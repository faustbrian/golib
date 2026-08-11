//go:build msk

package mskiam_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	kafka "github.com/faustbrian/golib/pkg/kafka"
	mskiam "github.com/faustbrian/golib/pkg/kafka/adapters/mskiam"
)

const (
	mskModeProvisioned = "provisioned"
	mskModeServerless  = "serverless"

	mskTransactionsRequired    = "required"
	mskTransactionsUnsupported = "unsupported"
)

type mskCompatibilityConfig struct {
	mode                   string
	region                 string
	clusterARN             string
	kafkaVersion           string
	brokers                []string
	dataTopic              string
	transactionSourceTopic string
	transactionOutputTopic string
	groupID                string
	transactionalID        string
	runID                  string
	transactionExpectation string
	transactionCategory    string
	timeout                time.Duration
}

func TestAmazonMSKCompatibility(t *testing.T) {
	config := loadMSKCompatibilityConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), config.timeout)
	defer cancel()

	provider, err := mskiam.New(ctx, mskiam.Config{
		Region:       config.region,
		TokenTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("construct MSK IAM provider: %v", err)
	}
	security := kafka.ClientSecurity{
		Authentication:    kafka.NewOAuthBearerAuthentication(provider),
		CredentialTimeout: 10 * time.Second,
	}

	reportMSKRuntime(t, config)
	inspectMSKControlPlane(t, ctx, config)
	inspectMSKCluster(t, ctx, config, security)
	delivery, values := exerciseMSKProducerModes(t, ctx, config, security)
	exerciseMSKConsumerSettlement(t, ctx, config, security, values)
	exerciseMSKReplay(t, ctx, config, security, delivery)
	exerciseMSKTransactions(t, ctx, config, security)
}

type mskControlPlaneCluster struct {
	ClusterARN  string                 `json:"ClusterArn"`
	ClusterType string                 `json:"ClusterType"`
	State       string                 `json:"State"`
	Provisioned *mskProvisionedProfile `json:"Provisioned"`
	Serverless  *mskServerlessProfile  `json:"Serverless"`
}

type mskProvisionedProfile struct {
	CurrentBrokerSoftwareInfo struct {
		KafkaVersion string `json:"KafkaVersion"`
	} `json:"CurrentBrokerSoftwareInfo"`
	ClientAuthentication mskControlPlaneAuthentication `json:"ClientAuthentication"`
}

type mskServerlessProfile struct {
	KafkaVersion         string                        `json:"KafkaVersion"`
	ClientAuthentication mskControlPlaneAuthentication `json:"ClientAuthentication"`
}

type mskControlPlaneAuthentication struct {
	SASL struct {
		IAM struct {
			Enabled bool `json:"Enabled"`
		} `json:"Iam"`
	} `json:"Sasl"`
}

type mskDescribeClusterResponse struct {
	ClusterInfo *mskControlPlaneCluster `json:"ClusterInfo"`
}

type mskBootstrapBrokersResponse struct {
	PrivateIAM         string `json:"BootstrapBrokerStringSaslIam"`
	PublicIAM          string `json:"BootstrapBrokerStringPublicSaslIam"`
	VPCConnectivityIAM string `json:"BootstrapBrokerStringVpcConnectivitySaslIam"`
}

type boundedMSKCommandOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (output *boundedMSKCommandOutput) Write(value []byte) (int, error) {
	if len(value) > output.limit-output.buffer.Len() {
		return 0, errors.New("AWS CLI output exceeds compatibility limit")
	}

	return output.buffer.Write(value)
}

func runMSKAWSCLI(
	ctx context.Context,
	operation string,
	arguments ...string,
) ([]byte, error) {
	output := &boundedMSKCommandOutput{limit: 1 << 20}
	command := exec.CommandContext(ctx, "aws", arguments...)
	command.Stdout = output
	command.Stderr = output
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("AWS CLI %s failed: %w", operation, err)
	}

	return append([]byte(nil), output.buffer.Bytes()...), nil
}

func inspectMSKControlPlane(
	t *testing.T,
	ctx context.Context,
	config mskCompatibilityConfig,
) {
	t.Helper()
	versionOutput, err := runMSKAWSCLI(ctx, "version", "--version")
	if err != nil {
		t.Fatal(err)
	}
	cliVersion := strings.TrimSpace(string(versionOutput))
	if !validMSKIdentifier(cliVersion, 512) {
		t.Fatal("AWS CLI returned invalid bounded version output")
	}
	t.Logf("aws_cli=%s", cliVersion)

	describeOutput, err := runMSKAWSCLI(
		ctx,
		"describe-cluster-v2",
		"kafka",
		"describe-cluster-v2",
		"--cluster-arn",
		config.clusterARN,
		"--region",
		config.region,
		"--output",
		"json",
		"--no-cli-pager",
	)
	if err != nil {
		t.Fatal(err)
	}
	bootstrapOutput, err := runMSKAWSCLI(
		ctx,
		"get-bootstrap-brokers",
		"kafka",
		"get-bootstrap-brokers",
		"--cluster-arn",
		config.clusterARN,
		"--region",
		config.region,
		"--output",
		"json",
		"--no-cli-pager",
	)
	if err != nil {
		t.Fatal(err)
	}
	var described mskDescribeClusterResponse
	if err := json.Unmarshal(describeOutput, &described); err != nil {
		t.Fatal("AWS CLI returned malformed bounded cluster metadata")
	}
	var bootstrap mskBootstrapBrokersResponse
	if err := json.Unmarshal(bootstrapOutput, &bootstrap); err != nil {
		t.Fatal("AWS CLI returned malformed bounded bootstrap metadata")
	}
	if err := validateMSKControlPlane(config, described, bootstrap); err != nil {
		t.Fatal(err)
	}
}

func validateMSKControlPlane(
	config mskCompatibilityConfig,
	described mskDescribeClusterResponse,
	bootstrap mskBootstrapBrokersResponse,
) error {
	cluster := described.ClusterInfo
	if cluster == nil || cluster.ClusterARN != config.clusterARN ||
		cluster.State != "ACTIVE" ||
		strings.ToLower(cluster.ClusterType) != config.mode {
		return errors.New("MSK control-plane cluster identity, type, or state differs")
	}
	var kafkaVersion string
	var iamEnabled bool
	switch config.mode {
	case mskModeProvisioned:
		if cluster.Provisioned == nil || cluster.Serverless != nil {
			return errors.New("MSK control plane omitted the provisioned profile")
		}
		kafkaVersion = cluster.Provisioned.CurrentBrokerSoftwareInfo.KafkaVersion
		iamEnabled = cluster.Provisioned.ClientAuthentication.SASL.IAM.Enabled
	case mskModeServerless:
		if cluster.Serverless == nil || cluster.Provisioned != nil {
			return errors.New("MSK control plane omitted the serverless profile")
		}
		kafkaVersion = cluster.Serverless.KafkaVersion
		iamEnabled = cluster.Serverless.ClientAuthentication.SASL.IAM.Enabled
	default:
		return errors.New("MSK control-plane mode is invalid")
	}
	if kafkaVersion != config.kafkaVersion || !iamEnabled {
		return errors.New("MSK control-plane Kafka version or IAM profile differs")
	}
	for _, candidate := range []string{
		bootstrap.PrivateIAM,
		bootstrap.PublicIAM,
		bootstrap.VPCConnectivityIAM,
	} {
		if sameMSKBrokerSet(config.brokers, strings.Split(candidate, ",")) {
			return nil
		}
	}

	return errors.New("MSK control-plane IAM bootstrap brokers differ")
}

func sameMSKBrokerSet(left []string, right []string) bool {
	if len(left) == 0 || len(left) != len(right) {
		return false
	}
	set := make(map[string]struct{}, len(left))
	for _, broker := range left {
		if broker == "" {
			return false
		}
		set[broker] = struct{}{}
	}
	if len(set) != len(left) {
		return false
	}
	seenRight := make(map[string]struct{}, len(right))
	for _, broker := range right {
		if _, exists := set[broker]; !exists {
			return false
		}
		if _, duplicate := seenRight[broker]; duplicate {
			return false
		}
		seenRight[broker] = struct{}{}
	}

	return true
}

func loadMSKCompatibilityConfig(t *testing.T) mskCompatibilityConfig {
	t.Helper()
	required := func(name string) string {
		t.Helper()
		value, exists := os.LookupEnv(name)
		if !exists || value == "" || value != strings.TrimSpace(value) {
			t.Fatalf("%s must be set to one non-whitespace value", name)
		}

		return value
	}
	config := mskCompatibilityConfig{
		mode:                   required("GOLIB_MSK_MODE"),
		region:                 required("GOLIB_MSK_REGION"),
		clusterARN:             required("GOLIB_MSK_CLUSTER_ARN"),
		kafkaVersion:           required("GOLIB_MSK_KAFKA_VERSION"),
		dataTopic:              required("GOLIB_MSK_DATA_TOPIC"),
		transactionSourceTopic: required("GOLIB_MSK_TRANSACTION_SOURCE_TOPIC"),
		transactionOutputTopic: required("GOLIB_MSK_TRANSACTION_OUTPUT_TOPIC"),
		groupID:                required("GOLIB_MSK_GROUP_ID"),
		transactionalID:        required("GOLIB_MSK_TRANSACTIONAL_ID"),
		runID:                  required("GOLIB_MSK_RUN_ID"),
		transactionExpectation: required("GOLIB_MSK_TRANSACTIONS"),
	}
	brokers := strings.Split(required("GOLIB_MSK_BROKERS"), ",")
	seenBrokers := make(map[string]struct{}, len(brokers))
	for _, broker := range brokers {
		if broker == "" || broker != strings.TrimSpace(broker) {
			t.Fatal("GOLIB_MSK_BROKERS contains an empty or padded address")
		}
		if _, duplicate := seenBrokers[broker]; duplicate {
			t.Fatal("GOLIB_MSK_BROKERS contains a duplicate address")
		}
		seenBrokers[broker] = struct{}{}
	}
	config.brokers = brokers
	timeout, err := time.ParseDuration(required("GOLIB_MSK_TIMEOUT"))
	if err != nil || timeout < time.Minute || timeout > 30*time.Minute {
		t.Fatal("GOLIB_MSK_TIMEOUT must be a duration from 1m through 30m")
	}
	config.timeout = timeout

	if config.mode != mskModeProvisioned && config.mode != mskModeServerless {
		t.Fatal("GOLIB_MSK_MODE must be provisioned or serverless")
	}
	if config.transactionExpectation != mskTransactionsRequired &&
		config.transactionExpectation != mskTransactionsUnsupported {
		t.Fatal("GOLIB_MSK_TRANSACTIONS must be required or unsupported")
	}
	config.transactionCategory = os.Getenv("GOLIB_MSK_TRANSACTION_ERROR_CATEGORY")
	if config.transactionExpectation == mskTransactionsUnsupported {
		if config.transactionCategory != kafka.ErrorAuthorization.String() &&
			config.transactionCategory != kafka.ErrorPermanent.String() {
			t.Fatal("unsupported transactions require an expected authorization or permanent category")
		}
	} else if config.transactionCategory != "" {
		t.Fatal("GOLIB_MSK_TRANSACTION_ERROR_CATEGORY must be unset when transactions are required")
	}
	if !validMSKRunID(config.runID) {
		t.Fatal("GOLIB_MSK_RUN_ID must be 1-64 printable non-whitespace bytes")
	}
	if !strings.Contains(config.groupID, config.runID) ||
		!strings.Contains(config.transactionalID, config.runID) {
		t.Fatal("group and transactional IDs must contain GOLIB_MSK_RUN_ID")
	}
	if config.dataTopic == config.transactionSourceTopic ||
		config.dataTopic == config.transactionOutputTopic ||
		config.transactionSourceTopic == config.transactionOutputTopic {
		t.Fatal("data and transaction topics must be distinct")
	}
	for name, value := range map[string]string{
		"GOLIB_MSK_DATA_TOPIC":               config.dataTopic,
		"GOLIB_MSK_TRANSACTION_SOURCE_TOPIC": config.transactionSourceTopic,
		"GOLIB_MSK_TRANSACTION_OUTPUT_TOPIC": config.transactionOutputTopic,
		"GOLIB_MSK_GROUP_ID":                 config.groupID,
		"GOLIB_MSK_TRANSACTIONAL_ID":         config.transactionalID,
	} {
		if !validMSKIdentifier(value, 249) {
			t.Fatalf("%s is not valid bounded Kafka text", name)
		}
	}
	if !validMSKIdentifier(config.groupID+"-transaction", 255) ||
		!validMSKIdentifier(config.transactionalID+"-processor", 255) {
		t.Fatal("derived transaction group or owner identity exceeds its Kafka bound")
	}
	if !validMSKIdentifier(config.clusterARN, 2_048) ||
		!validMSKIdentifier(config.kafkaVersion, 64) {
		t.Fatal("cluster ARN or Kafka version is invalid bounded text")
	}
	if err := (mskiam.Config{Region: config.region}).Validate(); err != nil {
		t.Fatalf("GOLIB_MSK_REGION is invalid: %v", err)
	}

	return config
}

func validMSKRunID(value string) bool {
	return validMSKIdentifier(value, 64) &&
		strings.IndexFunc(value, unicode.IsSpace) == -1
}

func validMSKIdentifier(value string, limit int) bool {
	return value != "" && len(value) <= limit && utf8.ValidString(value) &&
		value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, unicode.IsControl) == -1
}

func reportMSKRuntime(t *testing.T, config mskCompatibilityConfig) {
	t.Helper()
	versions := make(map[string]string)
	if info, ok := debug.ReadBuildInfo(); ok {
		t.Logf("go_version=%s", info.GoVersion)
		versions[info.Main.Path] = info.Main.Version
		for _, dependency := range info.Deps {
			switch dependency.Path {
			case "github.com/faustbrian/golib/pkg/kafka",
				"github.com/faustbrian/golib/pkg/kafka/adapters/mskiam",
				"github.com/aws/aws-msk-iam-sasl-signer-go",
				"github.com/aws/aws-sdk-go-v2",
				"github.com/twmb/franz-go",
				"github.com/twmb/franz-go/pkg/kadm":
				versions[dependency.Path] = dependency.Version
			}
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "GOOS", "GOARCH", "vcs.revision", "vcs.modified":
				t.Logf("build %s=%s", setting.Key, setting.Value)
			}
		}
	}
	paths := make([]string, 0, len(versions))
	for path := range versions {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	t.Logf(
		"MSK runtime mode=%s region=%s cluster_arn=%s kafka_version=%s run_id=%s",
		config.mode,
		config.region,
		config.clusterARN,
		config.kafkaVersion,
		config.runID,
	)
	for _, path := range paths {
		t.Logf("module %s=%s", path, versions[path])
	}
}

func inspectMSKCluster(
	t *testing.T,
	ctx context.Context,
	config mskCompatibilityConfig,
	security kafka.ClientSecurity,
) {
	t.Helper()
	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:        config.brokers,
		ClientID:       "golib-msk-inspector-" + config.runID,
		Security:       security,
		DialTimeout:    15 * time.Second,
		RequestTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("construct MSK inspector: %v", err)
	}
	defer closeMSKInspector(t, inspector)
	cluster, err := inspector.Cluster(ctx)
	if err != nil {
		t.Fatalf("inspect MSK cluster: %v", err)
	}
	if len(cluster.Brokers) == 0 {
		t.Fatal("MSK cluster inspection returned no brokers")
	}
	t.Logf(
		"MSK broker metadata cluster_id=%s cluster_id_visible=%t broker_count=%d controller_visible=%t",
		cluster.ID,
		cluster.IDVisible,
		len(cluster.Brokers),
		cluster.ControllerVisible,
	)
	topics, err := inspector.Topics(
		ctx,
		config.dataTopic,
		config.transactionSourceTopic,
		config.transactionOutputTopic,
	)
	if err != nil {
		t.Fatalf("inspect MSK topics: %v", err)
	}
	if len(topics) != 3 {
		t.Fatalf("MSK topic inspection count = %d, want 3", len(topics))
	}
	for _, topic := range topics {
		if len(topic.Partitions) == 0 {
			t.Fatalf("MSK topic %q returned no partitions", topic.Name)
		}
	}
}

func exerciseMSKProducerModes(
	t *testing.T,
	ctx context.Context,
	config mskCompatibilityConfig,
	security kafka.ClientSecurity,
) (kafka.DeliveryResult, []string) {
	t.Helper()
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:            config.brokers,
		ClientID:           "golib-msk-producer-" + config.runID,
		AllowedTopics:      []string{config.dataTopic},
		KeyPolicy:          kafka.KeyRequired,
		MaxBufferedRecords: 32,
		MaxBufferedBytes:   1 << 20,
		MaxBatchRecords:    8,
		MaxBatchBytes:      1 << 20,
		DeliveryTimeout:    30 * time.Second,
		RequestTimeout:     15 * time.Second,
		DialTimeout:        15 * time.Second,
		Linger:             5 * time.Millisecond,
		Security:           security,
	})
	if err != nil {
		t.Fatalf("construct MSK producer: %v", err)
	}
	defer closeMSKProducer(t, producer)
	key := []byte("golib-msk-" + config.runID)
	values := []string{"sync", "batch-1", "batch-2", "async"}
	delivery := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic: config.dataTopic,
		Key:   key,
		Value: []byte(values[0]),
	})
	if delivery.Err != nil {
		t.Fatalf("MSK synchronous delivery: %v", delivery.Err)
	}
	batch, err := producer.PublishBatch(ctx, []kafka.ProducerRecord{
		{Topic: config.dataTopic, Key: key, Value: []byte(values[1])},
		{Topic: config.dataTopic, Key: key, Value: []byte(values[2])},
	})
	if err != nil || len(batch) != 2 || batch[0].Err != nil || batch[1].Err != nil {
		t.Fatalf("MSK batch delivery count/error = %d/%v", len(batch), err)
	}
	resultChannel, err := producer.PublishAsync(ctx, kafka.ProducerRecord{
		Topic: config.dataTopic,
		Key:   key,
		Value: []byte(values[3]),
	})
	if err != nil {
		t.Fatalf("admit MSK asynchronous delivery: %v", err)
	}
	select {
	case result := <-resultChannel:
		if result.Err != nil {
			t.Fatalf("MSK asynchronous delivery: %v", result.Err)
		}
	case <-ctx.Done():
		t.Fatalf("wait for MSK asynchronous delivery: %v", context.Cause(ctx))
	}

	return delivery, values
}

func exerciseMSKConsumerSettlement(
	t *testing.T,
	ctx context.Context,
	config mskCompatibilityConfig,
	security kafka.ClientSecurity,
	values []string,
) {
	t.Helper()
	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:                      config.brokers,
		ClientID:                     "golib-msk-consumer-" + config.runID,
		GroupID:                      config.groupID,
		Topics:                       []string{config.dataTopic},
		ResetOffset:                  kafka.OffsetEarliest,
		BalancePolicy:                kafka.BalanceCooperativeSticky,
		MaxPollRecords:               100,
		MaxConcurrentFetches:         2,
		MaxConcurrentHandlers:        2,
		FetchMaxBytes:                4 << 20,
		FetchMaxPartitionBytes:       1 << 20,
		BrokerMaxReadBytes:           8 << 20,
		MaxDecompressedBatchBytes:    2 << 20,
		MaxBufferedDecompressedBytes: 8 << 20,
		HandlerTimeout:               30 * time.Second,
		CommitTimeout:                30 * time.Second,
		ShutdownTimeout:              30 * time.Second,
		DialTimeout:                  15 * time.Second,
		Security:                     security,
	})
	if err != nil {
		t.Fatalf("construct MSK consumer: %v", err)
	}
	defer closeMSKConsumer(t, consumer)
	wantKey := "golib-msk-" + config.runID
	consumed := make([]string, 0, len(values))
	for len(consumed) < len(values) {
		_, runErr := consumer.RunOnce(ctx, kafka.HandlerFunc(func(
			_ context.Context,
			message kafka.ConsumedMessage,
		) error {
			if string(message.Key) == wantKey {
				consumed = append(consumed, string(message.Value))
			}

			return nil
		}))
		if runErr != nil {
			t.Fatalf("consume and settle MSK records: %v", runErr)
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("wait for MSK records: %v", err)
		}
	}
	if !slices.Equal(consumed, values) {
		t.Fatalf("MSK consumed values = %q, want %q", consumed, values)
	}
}

func exerciseMSKReplay(
	t *testing.T,
	ctx context.Context,
	config mskCompatibilityConfig,
	security kafka.ClientSecurity,
	delivery kafka.DeliveryResult,
) {
	t.Helper()
	reader, err := kafka.NewReplayReader(kafka.ReplayConfig{
		Brokers:  config.brokers,
		ClientID: "golib-msk-replay-" + config.runID,
		Ranges: []kafka.ReplayRange{{
			Topic:       config.dataTopic,
			Partition:   delivery.Partition,
			StartOffset: delivery.Offset,
			EndOffset:   delivery.Offset + 1,
		}},
		SideEffects:                  kafka.ReplaySideEffectsAllowed,
		Security:                     security,
		MaxPollRecords:               1,
		MaxConcurrentFetches:         1,
		MaxConcurrentHandlers:        1,
		FetchMaxBytes:                1 << 20,
		FetchMaxPartitionBytes:       1 << 20,
		BrokerMaxReadBytes:           2 << 20,
		MaxDecompressedBatchBytes:    1 << 20,
		MaxBufferedDecompressedBytes: 2 << 20,
		PlanningTimeout:              30 * time.Second,
		ProgressTimeout:              30 * time.Second,
		HandlerTimeout:               30 * time.Second,
		ShutdownTimeout:              30 * time.Second,
		DialTimeout:                  15 * time.Second,
	})
	if err != nil {
		t.Fatalf("construct MSK replay reader: %v", err)
	}
	defer closeMSKReplayReader(t, reader)
	plan, err := reader.PlanAgainstBroker(ctx)
	if err != nil || plan.TotalRemaining != 1 {
		t.Fatalf("plan MSK replay: remaining=%d error=%v", plan.TotalRemaining, err)
	}
	var replayed int
	result, err := reader.Replay(ctx, kafka.ReplayHandlerFunc(func(
		_ context.Context,
		record kafka.ReplayRecord,
	) error {
		if record.Offset != delivery.Offset ||
			record.Partition != delivery.Partition ||
			record.Topic != config.dataTopic {
			return errors.New("MSK replay returned unexpected coordinates")
		}
		replayed++

		return nil
	}))
	if err != nil || replayed != 1 || result.Processed != 1 ||
		result.IncompleteRanges != 0 {
		t.Fatalf("execute MSK replay: result=%#v replayed=%d error=%v", result, replayed, err)
	}
}

func exerciseMSKTransactions(
	t *testing.T,
	ctx context.Context,
	config mskCompatibilityConfig,
	security kafka.ClientSecurity,
) {
	t.Helper()
	transactionCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:               config.brokers,
		ClientID:              "golib-msk-transaction-" + config.runID,
		AllowedTopics:         []string{config.transactionSourceTopic},
		TransactionalID:       config.transactionalID,
		TransactionTimeout:    60 * time.Second,
		TransactionEndTimeout: 30 * time.Second,
		DeliveryTimeout:       30 * time.Second,
		RequestTimeout:        15 * time.Second,
		DialTimeout:           15 * time.Second,
		ShutdownTimeout:       30 * time.Second,
		Security:              security,
	})
	if err != nil {
		t.Fatalf("construct MSK transactional producer: %v", err)
	}
	defer closeMSKProducer(t, producer)
	if config.transactionExpectation == mskTransactionsRequired {
		abortCause := errors.New("abort MSK compatibility transaction")
		abortErr := producer.RunTransaction(transactionCtx, func(
			transaction kafka.Transaction,
		) error {
			if err := transaction.Publish(transactionCtx, kafka.ProducerRecord{
				Topic: config.transactionSourceTopic,
				Key:   []byte("golib-msk-transaction-abort-" + config.runID),
				Value: []byte("aborted"),
			}); err != nil {
				return err
			}

			return abortCause
		})
		if !errors.Is(abortErr, abortCause) {
			t.Fatalf("abort MSK transaction: %v", abortErr)
		}
	}
	transactionErr := producer.RunTransaction(transactionCtx, func(
		transaction kafka.Transaction,
	) error {
		return transaction.Publish(transactionCtx, kafka.ProducerRecord{
			Topic: config.transactionSourceTopic,
			Key:   []byte("golib-msk-transaction-" + config.runID),
			Value: []byte("committed"),
		})
	})
	if config.transactionExpectation == mskTransactionsUnsupported {
		if transactionErr == nil {
			t.Fatal("MSK transactions succeeded but were declared unsupported")
		}
		var classified *kafka.TransactionError
		if !errors.As(transactionErr, &classified) ||
			classified.Category().String() != config.transactionCategory ||
			!classified.OutcomeKnown() {
			t.Fatalf(
				"MSK transaction rejection category/outcome = %s/%t, want %s/known: %v",
				classifiedCategory(classified),
				classified != nil && classified.OutcomeKnown(),
				config.transactionCategory,
				transactionErr,
			)
		}
		t.Logf(
			"MSK transaction profile rejected as declared: operation=%s category=%s outcome_known=true",
			classified.Operation(),
			classified.Category(),
		)

		return
	}
	if transactionErr != nil {
		t.Fatalf("commit MSK transaction: %v", transactionErr)
	}

	processor, err := kafka.NewTransactionProcessor(kafka.TransactionProcessorConfig{
		Connection: kafka.TransactionConnectionConfig{
			Brokers:     config.brokers,
			ClientID:    "golib-msk-processor-" + config.runID,
			DialTimeout: 15 * time.Second,
			Security:    security,
		},
		Group: kafka.TransactionGroupConfig{
			GroupID:              config.groupID + "-transaction",
			Topics:               []string{config.transactionSourceTopic},
			ResetOffset:          kafka.OffsetEarliest,
			BalancePolicy:        kafka.BalanceCooperativeSticky,
			MaxPollRecords:       10,
			MaxConcurrentFetches: 1,
			ProcessingTimeout:    30 * time.Second,
		},
		Output: kafka.TransactionOutputConfig{
			AllowedTopics:         []string{config.transactionOutputTopic},
			TransactionalID:       config.transactionalID + "-processor",
			TransactionTimeout:    60 * time.Second,
			TransactionEndTimeout: 30 * time.Second,
			DeliveryTimeout:       30 * time.Second,
			RequestTimeout:        15 * time.Second,
		},
		ShutdownTimeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("construct MSK transaction processor: %v", err)
	}
	defer closeMSKTransactionProcessor(t, processor)
	result, err := processor.RunOnce(transactionCtx, kafka.TransactionHandlerFunc(func(
		ctx context.Context,
		record kafka.ConsumedRecord,
		transaction kafka.Transaction,
	) error {
		return transaction.Publish(ctx, kafka.ProducerRecord{
			Topic: config.transactionOutputTopic,
			Key:   record.Key,
			Value: []byte("transformed"),
		})
	}))
	if err != nil || !result.Committed || result.Processed != 1 || result.Published != 1 {
		t.Fatalf("MSK consume-transform-produce result=%#v error=%v", result, err)
	}
}

func classifiedCategory(err *kafka.TransactionError) kafka.ErrorCategory {
	if err == nil {
		return kafka.ErrorUnknown
	}

	return err.Category()
}

func closeMSKProducer(t *testing.T, producer *kafka.Producer) {
	t.Helper()
	if err := producer.Close(); err != nil {
		t.Errorf("close MSK producer: %v", err)
	}
}

func closeMSKConsumer(t *testing.T, consumer *kafka.Consumer) {
	t.Helper()
	if err := consumer.Close(); err != nil {
		t.Errorf("close MSK consumer: %v", err)
	}
}

func closeMSKInspector(t *testing.T, inspector *kafka.Inspector) {
	t.Helper()
	if err := inspector.Close(); err != nil {
		t.Errorf("close MSK inspector: %v", err)
	}
}

func closeMSKReplayReader(t *testing.T, reader *kafka.ReplayReader) {
	t.Helper()
	if err := reader.Close(); err != nil {
		t.Errorf("close MSK replay reader: %v", err)
	}
}

func closeMSKTransactionProcessor(
	t *testing.T,
	processor *kafka.TransactionProcessor,
) {
	t.Helper()
	if err := processor.Close(); err != nil {
		t.Errorf("close MSK transaction processor: %v", err)
	}
}

func TestMSKCompatibilityConfigRejectsUnboundedInputs(t *testing.T) {
	if validMSKRunID("") || validMSKRunID(strings.Repeat("a", 65)) ||
		validMSKRunID("has space") || validMSKIdentifier("\xff", 64) {
		t.Fatal("MSK compatibility identifiers accepted invalid input")
	}
	if !validMSKRunID("run-20260811") ||
		!validMSKIdentifier("events.compatibility.v1", 249) {
		t.Fatal("MSK compatibility identifiers rejected valid input")
	}
}

func TestMSKControlPlaneValidation(t *testing.T) {
	config := mskCompatibilityConfig{
		mode:         mskModeProvisioned,
		clusterARN:   "arn:aws:kafka:eu-north-1:123456789012:cluster/test/id",
		kafkaVersion: "4.1.x",
		brokers:      []string{"b-1.test:9098", "b-2.test:9098"},
	}
	provisionedDescription := testMSKDescribeResponse(config)
	bootstrap := mskBootstrapBrokersResponse{
		PrivateIAM: "b-2.test:9098,b-1.test:9098",
	}
	if err := validateMSKControlPlane(config, provisionedDescription, bootstrap); err != nil {
		t.Fatalf("validate provisioned control plane: %v", err)
	}

	serverless := config
	serverless.mode = mskModeServerless
	serverlessDescription := testMSKDescribeResponse(serverless)
	if err := validateMSKControlPlane(serverless, serverlessDescription, bootstrap); err != nil {
		t.Fatalf("validate serverless control plane: %v", err)
	}

	invalid := []struct {
		name      string
		config    mskCompatibilityConfig
		described mskDescribeClusterResponse
		bootstrap mskBootstrapBrokersResponse
	}{
		{
			name: "wrong cluster type",
			config: func() mskCompatibilityConfig {
				value := config
				value.mode = mskModeServerless
				return value
			}(),
			described: provisionedDescription,
			bootstrap: bootstrap,
		},
		{
			name:      "duplicate bootstrap",
			config:    config,
			described: testMSKDescribeResponse(config),
			bootstrap: mskBootstrapBrokersResponse{
				PrivateIAM: "b-1.test:9098,b-1.test:9098",
			},
		},
	}
	for _, testCase := range invalid {
		t.Run(testCase.name, func(t *testing.T) {
			if err := validateMSKControlPlane(
				testCase.config,
				testCase.described,
				testCase.bootstrap,
			); err == nil {
				t.Fatal("invalid MSK control-plane evidence was accepted")
			}
		})
	}
}

func testMSKDescribeResponse(
	config mskCompatibilityConfig,
) mskDescribeClusterResponse {
	cluster := &mskControlPlaneCluster{
		ClusterARN:  config.clusterARN,
		ClusterType: strings.ToUpper(config.mode),
		State:       "ACTIVE",
	}
	if config.mode == mskModeProvisioned {
		cluster.Provisioned = &mskProvisionedProfile{}
		cluster.Provisioned.CurrentBrokerSoftwareInfo.KafkaVersion = config.kafkaVersion
		cluster.Provisioned.ClientAuthentication.SASL.IAM.Enabled = true
	} else {
		cluster.Serverless = &mskServerlessProfile{KafkaVersion: config.kafkaVersion}
		cluster.Serverless.ClientAuthentication.SASL.IAM.Enabled = true
	}

	return mskDescribeClusterResponse{ClusterInfo: cluster}
}
