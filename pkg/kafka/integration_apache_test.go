//go:build integration

package kafka_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

const (
	apacheKafkaImage = "apache/kafka:4.3.1@" +
		"sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837"
	apacheKafkaClusterID  = "4L6g3nShT-eMCtK--X86sw"
	apacheKafkaClientPort = "9092/tcp"
	apacheKafkaStartFile  = "/tmp/golib-kafka-start.sh"
	apacheKafkaPIDFile    = "/tmp/golib-kafka.pid"
	apacheKafkaStopFile   = "/tmp/golib-kafka.stop"
	apacheKafkaSubnetPool = 4_096

	apacheKafkaProcessorChildMode = "GOLIB_KAFKA_PROCESSOR_CHILD"
	apacheKafkaProcessorBrokers   = "GOLIB_KAFKA_PROCESSOR_BROKERS"
	apacheKafkaProcessorSource    = "GOLIB_KAFKA_PROCESSOR_SOURCE"
	apacheKafkaProcessorOutput    = "GOLIB_KAFKA_PROCESSOR_OUTPUT"
	apacheKafkaProcessorGroup     = "GOLIB_KAFKA_PROCESSOR_GROUP"
	apacheKafkaProcessorTxnID     = "GOLIB_KAFKA_PROCESSOR_TRANSACTIONAL_ID"
	apacheKafkaProcessorBalance   = "GOLIB_KAFKA_PROCESSOR_BALANCE"
	apacheKafkaProcessorReady     = "golib-kafka-processor-output-acknowledged"
	apacheKafkaProcessorPartition = "golib-kafka-processor-source-partition"
	apacheKafkaProcessorAborted   = "golib-kafka-processor-rebalance-aborted"

	apacheKafkaProcessorModeTermination = "termination"
	apacheKafkaProcessorModeRebalance   = "rebalance"
	apacheKafkaProcessorBalanceEager    = "eager"
	apacheKafkaProcessorBalanceCoop     = "cooperative"
	apacheKafkaProcessorDiagnosticLimit = 32 << 10

	apacheKafkaConsumerChildMode      = "GOLIB_KAFKA_CONSUMER_CHILD"
	apacheKafkaConsumerBrokers        = "GOLIB_KAFKA_CONSUMER_BROKERS"
	apacheKafkaConsumerTopic          = "GOLIB_KAFKA_CONSUMER_TOPIC"
	apacheKafkaConsumerGroup          = "GOLIB_KAFKA_CONSUMER_GROUP"
	apacheKafkaConsumerClient         = "GOLIB_KAFKA_CONSUMER_CLIENT"
	apacheKafkaConsumerBalance        = "GOLIB_KAFKA_CONSUMER_BALANCE"
	apacheKafkaConsumerRack           = "GOLIB_KAFKA_CONSUMER_RACK"
	apacheKafkaConsumerFetchBroker    = "GOLIB_KAFKA_CONSUMER_FETCH_BROKER"
	apacheKafkaConsumerBalanceEager   = "eager"
	apacheKafkaConsumerBalanceMigrate = "eager-to-cooperative"
	apacheKafkaConsumerBalanceCoop    = "cooperative"
	apacheKafkaConsumerReady          = "golib-kafka-consumer-assigned"
	apacheKafkaConsumerRackFetch      = "golib-kafka-consumer-rack-fetch"
	apacheKafkaConsumerRackArmed      = "golib-kafka-consumer-rack-armed"
	apacheKafkaConsumerRackRecord     = "golib-kafka-consumer-rack-record"
	apacheKafkaConsumerStopped        = "golib-kafka-consumer-stopped"
)

type apacheKafkaProcessorDiagnostic struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (diagnostic *apacheKafkaProcessorDiagnostic) Write(
	value []byte,
) (int, error) {
	length := len(value)
	diagnostic.mu.Lock()
	defer diagnostic.mu.Unlock()

	remaining := apacheKafkaProcessorDiagnosticLimit - diagnostic.buffer.Len()
	if remaining <= 0 {
		return length, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
	}
	_, _ = diagnostic.buffer.Write(value)

	return length, nil
}

func (diagnostic *apacheKafkaProcessorDiagnostic) String() string {
	diagnostic.mu.Lock()
	defer diagnostic.mu.Unlock()

	return diagnostic.buffer.String()
}

func TestApacheKafkaTransactionProcessorChild(t *testing.T) {
	switch os.Getenv(apacheKafkaProcessorChildMode) {
	case "":
		t.Skip("subprocess helper")
	case apacheKafkaProcessorModeTermination:
		runApacheKafkaTerminationChild(t)
	case apacheKafkaProcessorModeRebalance:
		runApacheKafkaRebalanceChild(t)
	default:
		t.Fatal("unknown transaction processor subprocess mode")
	}
}

func TestApacheKafkaConsumerChild(t *testing.T) {
	if os.Getenv(apacheKafkaConsumerChildMode) == "" {
		t.Skip("subprocess helper")
	}

	runApacheKafkaConsumerChild(t)
}

func runApacheKafkaConsumerChild(t *testing.T) {
	t.Helper()

	var balancePolicy kafka.GroupBalancePolicy
	switch os.Getenv(apacheKafkaConsumerBalance) {
	case apacheKafkaConsumerBalanceEager:
		balancePolicy = kafka.BalanceEagerSticky
	case apacheKafkaConsumerBalanceMigrate:
		balancePolicy = kafka.BalanceEagerToCooperative
	case apacheKafkaConsumerBalanceCoop:
		balancePolicy = kafka.BalanceCooperativeSticky
	default:
		t.Fatal("unknown consumer child balance policy")
	}
	config := kafka.ConsumerConfig{
		Brokers:               strings.Split(os.Getenv(apacheKafkaConsumerBrokers), ","),
		ClientID:              os.Getenv(apacheKafkaConsumerClient),
		GroupID:               os.Getenv(apacheKafkaConsumerGroup),
		Rack:                  os.Getenv(apacheKafkaConsumerRack),
		Topics:                []string{os.Getenv(apacheKafkaConsumerTopic)},
		ResetOffset:           kafka.OffsetEarliest,
		BalancePolicy:         balancePolicy,
		MaxPollRecords:        1,
		MaxAssignedPartitions: 2,
		FetchMaxWait:          100 * time.Millisecond,
		SessionTimeout:        6 * time.Second,
		HeartbeatInterval:     2 * time.Second,
		RebalanceTimeout:      15 * time.Second,
		HandlerTimeout:        time.Second,
		CommitTimeout:         time.Second,
		ShutdownTimeout:       10 * time.Second,
		Security:              kafka.DevelopmentPlaintextSecurity(),
	}
	expectedFetchBroker := int32(0)
	rackFetch := make(chan struct{})
	rackRecord := make(chan struct{})
	var rackRecordOnce sync.Once
	var rackState struct {
		sync.Mutex
		armed           bool
		lastFetchBroker int32
	}
	if value := os.Getenv(apacheKafkaConsumerFetchBroker); value != "" {
		parsed, parseErr := strconv.ParseInt(value, 10, 32)
		if parseErr != nil || parsed < 1 || parsed > 3 || config.Rack == "" {
			t.Fatal("invalid rack-local consumer child configuration")
		}
		expectedFetchBroker = int32(parsed)
		expectedFetchAPIKey := new(kmsg.FetchRequest).Key()
		config.MaxConcurrentFetches = 1
		var observed sync.Once
		config.Observers = kafka.ObserverPolicy{
			Observers: []kafka.ObserverFunc{func(
				_ context.Context,
				observation kafka.Observation,
			) error {
				if observation.Kind == kafka.ObservationBrokerRequest &&
					observation.APIKeyKnown &&
					observation.APIKey == expectedFetchAPIKey &&
					observation.BrokerKnown &&
					observation.Succeeded {
					rackState.Lock()
					if rackState.armed {
						rackState.lastFetchBroker = observation.BrokerID
					} else if observation.BrokerID == expectedFetchBroker {
						observed.Do(func() { close(rackFetch) })
					}
					rackState.Unlock()
				}

				return nil
			}},
			FailureHandler: func(context.Context, kafka.ObservationFailure) {},
		}
	}
	consumer, err := kafka.NewConsumer(config)
	if err != nil {
		t.Fatalf("construct consumer child: %v", err)
	}
	defer func() {
		if closeErr := consumer.Close(); closeErr != nil {
			t.Errorf("close consumer child: %v", closeErr)
		}
	}()

	runCtx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()
	runResult := make(chan error, 1)
	go func() {
		runResult <- consumer.Run(runCtx, kafka.HandlerFunc(func(
			context.Context,
			kafka.ConsumedRecord,
		) error {
			if expectedFetchBroker != 0 {
				rackState.Lock()
				armed := rackState.armed
				fetchBroker := rackState.lastFetchBroker
				rackState.Unlock()
				if armed && fetchBroker != expectedFetchBroker {
					return fmt.Errorf(
						"record followed fetch from broker %d, want %d",
						fetchBroker,
						expectedFetchBroker,
					)
				}
				if armed {
					rackRecordOnce.Do(func() { close(rackRecord) })
				}
			}

			return nil
		}))
	}()
	waitForApacheKafkaConsumerAssignment(t, consumer, runResult)
	if expectedFetchBroker != 0 {
		select {
		case <-rackFetch:
			if _, err := fmt.Fprintf(
				os.Stdout,
				"%s:%d\n",
				apacheKafkaConsumerRackFetch,
				expectedFetchBroker,
			); err != nil {
				t.Fatalf("report consumer child rack fetch: %v", err)
			}
		case err := <-runResult:
			t.Fatalf("consumer child stopped before rack fetch: %v", err)
		case <-time.After(30 * time.Second):
			t.Fatalf(
				"consumer child did not fetch from rack-local broker %d",
				expectedFetchBroker,
			)
		}
		arm := []byte{0}
		if _, err := io.ReadFull(os.Stdin, arm); err != nil {
			t.Fatalf("arm consumer child rack proof: %v", err)
		}
		rackState.Lock()
		rackState.armed = true
		rackState.lastFetchBroker = 0
		rackState.Unlock()
		if _, err := fmt.Fprintln(os.Stdout, apacheKafkaConsumerRackArmed); err != nil {
			t.Fatalf("report armed consumer child rack proof: %v", err)
		}
	}
	if _, err := fmt.Fprintln(os.Stdout, apacheKafkaConsumerReady); err != nil {
		t.Fatalf("report consumer child assignment: %v", err)
	}
	if expectedFetchBroker != 0 {
		select {
		case <-rackRecord:
			if _, err := fmt.Fprintf(
				os.Stdout,
				"%s:%d\n",
				apacheKafkaConsumerRackRecord,
				expectedFetchBroker,
			); err != nil {
				t.Fatalf("report consumer child rack record: %v", err)
			}
		case err := <-runResult:
			t.Fatalf("consumer child stopped before rack record: %v", err)
		case <-time.After(30 * time.Second):
			t.Fatalf(
				"consumer child did not handle a record fetched from broker %d",
				expectedFetchBroker,
			)
		}
	}

	release := []byte{0}
	if _, err := io.ReadFull(os.Stdin, release); err != nil {
		t.Fatalf("wait for consumer child release: %v", err)
	}
	cancelRun()
	if err := <-runResult; err != nil {
		t.Fatalf("stop consumer child runner: %v", err)
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancelShutdown()
	if err := consumer.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown consumer child: %v", err)
	}
	if _, err := fmt.Fprintln(os.Stdout, apacheKafkaConsumerStopped); err != nil {
		t.Fatalf("report consumer child shutdown: %v", err)
	}
}

func waitForApacheKafkaConsumerAssignment(
	t *testing.T,
	consumer *kafka.Consumer,
	runResult <-chan error,
) {
	t.Helper()

	waitCtx, cancelWait := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelWait()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		assignment, err := consumer.Assignment()
		if err == nil && len(assignment.Partitions) > 0 {
			return
		}

		select {
		case runErr := <-runResult:
			t.Fatalf("consumer child exited before assignment: %v", runErr)
		case <-waitCtx.Done():
			t.Fatalf("wait for consumer child assignment: %v", context.Cause(waitCtx))
		case <-ticker.C:
		}
	}
}

func runApacheKafkaTerminationChild(t *testing.T) {
	t.Helper()

	processor := newApacheKafkaTerminationProcessor(
		t,
		strings.Split(os.Getenv(apacheKafkaProcessorBrokers), ","),
		os.Getenv(apacheKafkaProcessorSource),
		os.Getenv(apacheKafkaProcessorOutput),
		os.Getenv(apacheKafkaProcessorGroup),
		os.Getenv(apacheKafkaProcessorTxnID),
		"golib-apache-termination-child",
	)
	err := processor.Run(context.Background(), kafka.TransactionHandlerFunc(
		func(
			ctx context.Context,
			record kafka.ConsumedRecord,
			transaction kafka.Transaction,
		) error {
			if err := transaction.Publish(ctx, kafka.ProducerRecord{
				Topic: os.Getenv(apacheKafkaProcessorOutput),
				Key:   record.Key,
				Value: append([]byte("crashed-"), record.Value...),
			}); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(
				os.Stdout,
				apacheKafkaProcessorReady,
			); err != nil {
				return err
			}

			<-ctx.Done()

			return context.Cause(ctx)
		},
	))
	t.Fatalf("transaction processor child unexpectedly returned: %v", err)
}

func runApacheKafkaRebalanceChild(t *testing.T) {
	t.Helper()

	var balancePolicy kafka.GroupBalancePolicy
	switch os.Getenv(apacheKafkaProcessorBalance) {
	case apacheKafkaProcessorBalanceEager:
		balancePolicy = kafka.BalanceEagerSticky
	case apacheKafkaProcessorBalanceCoop:
		balancePolicy = kafka.BalanceCooperativeSticky
	default:
		t.Fatal("unknown transaction processor child balance policy")
	}
	processor := newApacheKafkaRebalanceProcessor(
		t,
		strings.Split(os.Getenv(apacheKafkaProcessorBrokers), ","),
		os.Getenv(apacheKafkaProcessorSource),
		os.Getenv(apacheKafkaProcessorOutput),
		os.Getenv(apacheKafkaProcessorGroup),
		os.Getenv(apacheKafkaProcessorTxnID),
		"golib-apache-rebalance-child",
		balancePolicy,
	)
	defer func() {
		if err := processor.Close(); err != nil {
			t.Errorf("close rebalance child processor: %v", err)
		}
	}()

	childCtx, cancelChild := context.WithTimeout(
		context.Background(),
		45*time.Second,
	)
	defer cancelChild()
	result, err := processor.RunOnce(
		childCtx,
		kafka.TransactionHandlerFunc(func(
			ctx context.Context,
			record kafka.ConsumedRecord,
			transaction kafka.Transaction,
		) error {
			if err := transaction.Publish(ctx, kafka.ProducerRecord{
				Topic: os.Getenv(apacheKafkaProcessorOutput),
				Key:   record.Key,
				Value: append([]byte("rebalanced-"), record.Value...),
			}); err != nil {
				return err
			}
			if _, err := fmt.Fprintln(
				os.Stdout,
				apacheKafkaProcessorReady,
			); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(
				os.Stdout,
				"%s=%d\n",
				apacheKafkaProcessorPartition,
				record.Partition,
			); err != nil {
				return err
			}

			release := []byte{0}
			if _, err := io.ReadFull(os.Stdin, release); err != nil {
				return err
			}

			return nil
		}),
	)
	if !errors.Is(err, kafka.ErrTransactionNotCommitted) ||
		result != (kafka.TransactionPollResult{
			Polled: 1, Processed: 1, Published: 1,
		}) {
		t.Fatalf("rebalance child result = (%#v, %v)", result, err)
	}
	if _, err := fmt.Fprintln(os.Stdout, apacheKafkaProcessorAborted); err != nil {
		t.Fatalf("report rebalance child abort: %v", err)
	}
}

func TestApacheKafkaCooperativeTransactionRebalance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cluster := startApacheKafkaCluster(t, ctx)
	cluster.observeFailureState(t)
	cluster.assertRuntimeVersion(t, ctx, "4.3.1")
	brokers := cluster.brokers(t, ctx)
	waitForApacheBrokerEndpoints(t, ctx, brokers)

	topic := fmt.Sprintf(
		"golib-apache-cooperative-rebalance-%d",
		time.Now().UnixNano(),
	)
	sourceTopic := topic + "-source"
	outputTopic := topic + "-output"
	createApacheKafkaTopic(t, ctx, brokers, sourceTopic, 2)
	createApacheKafkaTopic(t, ctx, brokers, outputTopic, 1)
	proveTransactionProcessorCooperativeRebalance(
		t,
		ctx,
		brokers,
		sourceTopic,
		outputTopic,
	)
}

func TestApacheKafkaConsumerRollingBalanceMigration(t *testing.T) {
	const (
		eagerClient       = "golib-apache-consumer-eager"
		migratingClient   = "golib-apache-consumer-migrating"
		cooperativeClient = "golib-apache-consumer-cooperative"
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cluster := startApacheKafkaCluster(t, ctx)
	cluster.observeFailureState(t)
	cluster.assertRuntimeVersion(t, ctx, "4.3.1")
	brokers := cluster.brokers(t, ctx)
	waitForApacheBrokerEndpoints(t, ctx, brokers)

	topic := fmt.Sprintf(
		"golib-apache-consumer-rolling-%d",
		time.Now().UnixNano(),
	)
	groupID := topic + "-group"
	createApacheKafkaTopic(t, ctx, brokers, topic, 2)

	eager := startApacheKafkaConsumerChild(
		t,
		ctx,
		brokers,
		topic,
		groupID,
		eagerClient,
		apacheKafkaConsumerBalanceEager,
	)
	waitForApacheKafkaConsumerGroupState(
		t, ctx, brokers, groupID, topic, "sticky", []string{eagerClient},
	)

	migrating := startApacheKafkaConsumerChild(
		t,
		ctx,
		brokers,
		topic,
		groupID,
		migratingClient,
		apacheKafkaConsumerBalanceMigrate,
	)
	waitForApacheKafkaConsumerGroupState(
		t,
		ctx,
		brokers,
		groupID,
		topic,
		"sticky",
		[]string{eagerClient, migratingClient},
	)
	eager.releaseConsumerAndWait(t)
	waitForApacheKafkaConsumerGroupState(
		t, ctx, brokers, groupID, topic, "sticky", []string{migratingClient},
	)

	cooperative := startApacheKafkaConsumerChild(
		t,
		ctx,
		brokers,
		topic,
		groupID,
		cooperativeClient,
		apacheKafkaConsumerBalanceCoop,
	)
	waitForApacheKafkaConsumerGroupState(
		t,
		ctx,
		brokers,
		groupID,
		topic,
		"cooperative-sticky",
		[]string{migratingClient, cooperativeClient},
	)
	migrating.releaseConsumerAndWait(t)
	waitForApacheKafkaConsumerGroupState(
		t,
		ctx,
		brokers,
		groupID,
		topic,
		"cooperative-sticky",
		[]string{cooperativeClient},
	)
	cooperative.releaseConsumerAndWait(t)
}

func TestApacheKafkaRackLocalConsumerCompatibility(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cluster := startApacheKafkaCluster(t, ctx)
	cluster.observeFailureState(t)
	cluster.assertRuntimeVersion(t, ctx, "4.3.1")
	brokers := cluster.brokers(t, ctx)
	waitForApacheBrokerEndpoints(t, ctx, brokers)

	topic := fmt.Sprintf("golib-apache-rack-local-%d", time.Now().UnixNano())
	createApacheKafkaTopic(t, ctx, brokers, topic, 1)
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "golib-apache-rack-local-producer",
		AllowedTopics: []string{topic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct rack-local producer: %v", err)
	}
	defer func() {
		if err := producer.Close(); err != nil {
			t.Errorf("close rack-local producer: %v", err)
		}
	}()
	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "golib-apache-rack-local-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct rack-local inspector: %v", err)
	}
	defer inspector.Close()

	proveApacheKafkaRackLocalFetch(
		t,
		ctx,
		brokers,
		producer,
		inspector,
		topic,
	)
}

func TestApacheKafkaCurrentMultiBrokerKRaftCompatibility(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	cluster := startApacheKafkaCluster(t, ctx)
	cluster.observeFailureState(t)
	cluster.assertRuntimeVersion(t, ctx, "4.3.1")
	brokers := cluster.brokers(t, ctx)
	waitForApacheBrokerEndpoints(t, ctx, brokers)

	topic := fmt.Sprintf("golib-apache-compatibility-%d", time.Now().UnixNano())
	fencingTransactionTopic := topic + "-transaction-fencing"
	warmTransactionTopic := topic + "-transaction-warm"
	responseLossProducerTopic := topic + "-producer-response-loss"
	responseLossTransactionTopic := topic + "-transaction-response-loss"
	responseLossTransactionProduceTopic :=
		topic + "-transaction-produce-response-loss"
	responseLossProcessorSourceTopic :=
		topic + "-transaction-processor-response-loss-source"
	responseLossProcessorOutputTopic :=
		topic + "-transaction-processor-response-loss-output"
	partialBatchAcceptedTopic := topic + "-batch-partial-accepted"
	partialBatchRejectedTopic := topic + "-batch-partial-rejected"
	recoveredTransactionTopic := topic + "-transaction-recovered"
	processorSourceTopic := topic + "-processor-source"
	processorOutputTopic := topic + "-processor-output"
	terminationSourceTopic := topic + "-termination-source"
	terminationOutputTopic := topic + "-termination-output"
	rebalanceSourceTopic := topic + "-rebalance-source"
	rebalanceOutputTopic := topic + "-rebalance-output"
	rebalanceTriggerTopic := topic + "-rebalance-trigger"
	createApacheKafkaTopic(t, ctx, brokers, topic, 3)
	createApacheKafkaTopic(t, ctx, brokers, fencingTransactionTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, warmTransactionTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, responseLossProducerTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, responseLossTransactionTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, responseLossTransactionProduceTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, responseLossProcessorSourceTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, responseLossProcessorOutputTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, partialBatchAcceptedTopic, 1)
	createApacheKafkaTopicWithConfigs(
		t,
		ctx,
		brokers,
		partialBatchRejectedTopic,
		1,
		map[string]*string{"max.message.bytes": kadm.StringPtr("512")},
	)
	createApacheKafkaTopic(t, ctx, brokers, recoveredTransactionTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, processorSourceTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, processorOutputTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, terminationSourceTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, terminationOutputTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, rebalanceSourceTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, rebalanceOutputTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, rebalanceTriggerTopic, 1)

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "golib-apache-cluster-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct Apache Kafka inspector: %v", err)
	}
	defer inspector.Close()

	clusterState, err := inspector.Cluster(ctx)
	if err != nil {
		t.Fatalf("inspect Apache Kafka cluster: %v", err)
	}
	if !clusterState.IDVisible ||
		clusterState.ID != apacheKafkaClusterID ||
		!clusterState.ControllerVisible ||
		len(clusterState.Brokers) != 3 {
		t.Fatalf("Apache Kafka cluster state = %#v", clusterState)
	}

	state := waitForApacheTopicState(t, ctx, inspector, topic, func(
		state kafka.TopicState,
	) bool {
		return len(state.Partitions) == 3 &&
			allPartitionsMatch(state, 3, 3)
	})
	if state.MinInSyncReplicas != 2 ||
		state.UncleanLeaderElectionEnabled {
		t.Fatalf("initial Apache Kafka topic state = %#v", state)
	}
	proveProducerBatchPartialDelivery(
		t,
		ctx,
		brokers,
		partialBatchAcceptedTopic,
		partialBatchRejectedTopic,
	)

	proveProducerTransactionVisibility(
		t,
		ctx,
		brokers,
		warmTransactionTopic,
	)
	waitForApacheTopicState(t, ctx, inspector, "__transaction_state", func(
		state kafka.TopicState,
	) bool {
		return len(state.Partitions) > 0 && allPartitionsMatch(state, 3, 3)
	})
	waitForApacheTopicState(t, ctx, inspector, responseLossProducerTopic, func(
		state kafka.TopicState,
	) bool {
		return len(state.Partitions) == 1 && allPartitionsMatch(state, 3, 3)
	})
	proveProducerCommitResponseLoss(
		t,
		ctx,
		brokers,
		responseLossTransactionTopic,
	)
	proveProducerResponseLoss(
		t,
		ctx,
		brokers,
		inspector,
		responseLossProducerTopic,
	)
	proveTransactionalProducerResponseLoss(
		t,
		ctx,
		brokers,
		responseLossTransactionProduceTopic,
	)
	proveTransactionProcessorProduceResponseLoss(
		t,
		ctx,
		brokers,
		responseLossProcessorSourceTopic,
		responseLossProcessorOutputTopic,
	)
	proveProducerFencing(t, ctx, brokers, fencingTransactionTopic)
	proveTransactionProcessorTerminationRecovery(
		t,
		ctx,
		brokers,
		terminationSourceTopic,
		terminationOutputTopic,
	)
	proveTransactionProcessorRebalance(
		t,
		ctx,
		brokers,
		rebalanceSourceTopic,
		rebalanceOutputTopic,
		rebalanceTriggerTopic,
	)

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "golib-apache-failure-producer",
		AllowedTopics: []string{topic, processorSourceTopic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct Apache Kafka producer: %v", err)
	}
	defer func() {
		if err := producer.Close(); err != nil {
			t.Errorf("close Apache Kafka producer: %v", err)
		}
	}()

	assertApacheKafkaDelivery(t, ctx, producer, topic, 0, "before-stop")
	stoppedNode := state.Partitions[0].Leader
	transactionState := waitForApacheTopicState(
		t,
		ctx,
		inspector,
		"__transaction_state",
		func(state kafka.TopicState) bool {
			return len(state.Partitions) > 0 && allPartitionsMatch(state, 3, 3)
		},
	)
	sourceTransactionalID := apacheTransactionalIDForLeader(
		t,
		transactionState,
		stoppedNode,
	)
	t.Logf("stopping Apache Kafka node %d", stoppedNode)
	cluster.stopNode(t, ctx, stoppedNode)

	state = waitForApacheTopicState(t, ctx, inspector, topic, func(
		state kafka.TopicState,
	) bool {
		return len(state.Partitions) == 3 &&
			allPartitionsMatch(state, 3, 2) &&
			state.Partitions[0].Leader != stoppedNode
	})
	assertApacheKafkaDelivery(t, ctx, producer, topic, 0, "during-stop")
	waitForApacheTopicState(t, ctx, inspector, processorSourceTopic, func(
		state kafka.TopicState,
	) bool {
		return len(state.Partitions) == 1 &&
			allPartitionsMatch(state, 3, 2)
	})
	waitForApacheTopicState(t, ctx, inspector, processorOutputTopic, func(
		state kafka.TopicState,
	) bool {
		return len(state.Partitions) == 1 &&
			allPartitionsMatch(state, 3, 2)
	})
	waitForApacheTopicState(t, ctx, inspector, "__transaction_state", func(
		state kafka.TopicState,
	) bool {
		return len(state.Partitions) > 0 &&
			allPartitionsMatch(state, 3, 2)
	})
	proveConsumeTransformProduce(
		t,
		ctx,
		brokers,
		producer,
		processorSourceTopic,
		processorOutputTopic,
		sourceTransactionalID,
	)
	waitForApacheTopicState(t, ctx, inspector, "__consumer_offsets", func(
		state kafka.TopicState,
	) bool {
		return len(state.Partitions) > 0 &&
			allPartitionsMatch(state, 3, 2)
	})

	cluster.startNode(t, ctx, stoppedNode)
	recoveredBrokers := cluster.brokers(t, ctx)
	for index := range brokers {
		if recoveredBrokers[index] != brokers[index] {
			t.Fatalf(
				"Apache Kafka node %d client port changed after restart",
				index+1,
			)
		}
	}
	waitForApacheBrokerEndpoints(t, ctx, brokers)
	waitForApacheTopicState(t, ctx, inspector, topic, func(
		state kafka.TopicState,
	) bool {
		return len(state.Partitions) == 3 &&
			allPartitionsMatch(state, 3, 3)
	})
	waitForApacheTopicState(t, ctx, inspector, "__transaction_state", func(
		state kafka.TopicState,
	) bool {
		return len(state.Partitions) > 0 && allPartitionsMatch(state, 3, 3)
	})
	assertApacheKafkaDelivery(t, ctx, producer, topic, 0, "after-recovery")

	proveProducerTransactionVisibility(
		t,
		ctx,
		brokers,
		recoveredTransactionTopic,
	)
}

func TestApacheKafkaTransactionTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cluster := startApacheKafkaCluster(t, ctx)
	cluster.observeFailureState(t)
	cluster.assertRuntimeVersion(t, ctx, "4.3.1")
	brokers := cluster.brokers(t, ctx)
	waitForApacheBrokerEndpoints(t, ctx, brokers)

	topic := fmt.Sprintf(
		"golib-apache-transaction-timeout-%d",
		time.Now().UnixNano(),
	)
	createApacheKafkaTopic(t, ctx, brokers, topic, 1)

	adminClient, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-apache-timeout-inspector"),
	)
	if err != nil {
		t.Fatalf("construct timeout inspector backend: %v", err)
	}
	defer adminClient.Close()
	admin := kadm.NewClient(adminClient)

	transactionalID := fmt.Sprintf(
		"golib-apache-transaction-timeout-%d",
		time.Now().UnixNano(),
	)
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:               brokers,
		ClientID:              "golib-apache-timeout-producer",
		AllowedTopics:         []string{topic},
		TransactionalID:       transactionalID,
		TransactionTimeout:    time.Second,
		TransactionEndTimeout: 3 * time.Second,
		Security:              kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct timeout producer: %v", err)
	}
	defer func() {
		if closeErr := producer.Close(); closeErr != nil {
			t.Errorf("close timeout producer: %v", closeErr)
		}
	}()

	err = producer.RunTransaction(ctx, func(
		transaction kafka.Transaction,
	) error {
		if publishErr := transaction.Publish(ctx, kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte("expired"),
			Value: []byte("expired"),
		}); publishErr != nil {
			return publishErr
		}
		return waitForApacheTransactionState(
			ctx,
			admin,
			transactionalID,
			"CompleteAbort",
		)
	})

	var transactionErr *kafka.TransactionError
	if !errors.As(err, &transactionErr) ||
		transactionErr.Operation() != kafka.TransactionOperationCommit ||
		transactionErr.Category() != kafka.ErrorAmbiguous ||
		transactionErr.Abortable() ||
		transactionErr.OutcomeKnown() ||
		!errors.Is(err, kafka.ErrTransactionOutcomeUnknown) ||
		!errors.Is(err, kerr.InvalidTxnState) {
		t.Fatalf("expired transaction error = %v", err)
	}

	assertNoApacheKafkaTransactionValues(
		t,
		brokers,
		topic,
		kgo.ReadCommitted(),
	)
	if values := consumeTransactionValues(
		t,
		brokers,
		topic,
		kgo.ReadUncommitted(),
		1,
	); len(values) != 1 || values[0] != "expired" {
		t.Fatalf("read-uncommitted expired values = %q", values)
	}
}

const (
	kafkaProduceAPIKey      = 0
	kafkaEndTxnAPIKey       = 26
	maxKafkaTestFrameBytes  = 100 << 20
	kafkaRequestHeaderBytes = 8
)

type kafkaResponseDropper struct {
	dialer net.Dialer
	apiKey uint16

	mu       sync.Mutex
	dropped  int
	disabled bool
}

func (dropper *kafkaResponseDropper) DialContext(
	ctx context.Context,
	network string,
	address string,
) (net.Conn, error) {
	connection, err := dropper.dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}

	return &kafkaResponseDroppingConn{
		Conn:          connection,
		dropper:       dropper,
		correlations:  make(map[int32]struct{}),
		requestBuffer: make([]byte, 0, 4<<10),
	}, nil
}

func (dropper *kafkaResponseDropper) recordDrop() bool {
	dropper.mu.Lock()
	defer dropper.mu.Unlock()
	if dropper.disabled {
		return false
	}
	dropper.dropped++

	return true
}

func (dropper *kafkaResponseDropper) disable() {
	dropper.mu.Lock()
	dropper.disabled = true
	dropper.mu.Unlock()
}

func (dropper *kafkaResponseDropper) droppedResponses() int {
	dropper.mu.Lock()
	defer dropper.mu.Unlock()

	return dropper.dropped
}

type kafkaResponseDroppingConn struct {
	net.Conn
	dropper *kafkaResponseDropper

	writeMu       sync.Mutex
	requestBuffer []byte

	correlationMu sync.Mutex
	correlations  map[int32]struct{}

	readMu       sync.Mutex
	responseData []byte
}

func (connection *kafkaResponseDroppingConn) Write(data []byte) (int, error) {
	connection.writeMu.Lock()
	defer connection.writeMu.Unlock()

	if err := connection.trackRequests(data); err != nil {
		return 0, err
	}
	written := 0
	for written < len(data) {
		count, err := connection.Conn.Write(data[written:])
		written += count
		if err != nil {
			return written, err
		}
		if count == 0 {
			return written, io.ErrNoProgress
		}
	}

	return written, nil
}

func (connection *kafkaResponseDroppingConn) trackRequests(data []byte) error {
	if len(connection.requestBuffer)+len(data) > maxKafkaTestFrameBytes+4 {
		return errors.New("kafka test request framing exceeded its bound")
	}
	connection.requestBuffer = append(connection.requestBuffer, data...)
	for len(connection.requestBuffer) >= 4 {
		frameBytes := int64(int32(binary.BigEndian.Uint32(
			connection.requestBuffer[:4],
		)))
		if frameBytes < kafkaRequestHeaderBytes ||
			frameBytes > maxKafkaTestFrameBytes {
			return errors.New("kafka test request framing is invalid")
		}
		totalBytes := frameBytes + 4
		if int64(len(connection.requestBuffer)) < totalBytes {
			return nil
		}
		frame := connection.requestBuffer[:totalBytes]
		if binary.BigEndian.Uint16(frame[4:6]) == connection.dropper.apiKey {
			correlation := int32(binary.BigEndian.Uint32(frame[8:12]))
			connection.correlationMu.Lock()
			connection.correlations[correlation] = struct{}{}
			connection.correlationMu.Unlock()
		}
		connection.requestBuffer = connection.requestBuffer[totalBytes:]
	}

	return nil
}

func (connection *kafkaResponseDroppingConn) Read(data []byte) (int, error) {
	connection.readMu.Lock()
	defer connection.readMu.Unlock()

	if len(data) == 0 {
		return 0, nil
	}
	for len(connection.responseData) == 0 {
		var sizeBytes [4]byte
		if _, err := io.ReadFull(connection.Conn, sizeBytes[:]); err != nil {
			return 0, err
		}
		frameBytes := int64(int32(binary.BigEndian.Uint32(sizeBytes[:])))
		if frameBytes < 4 || frameBytes > maxKafkaTestFrameBytes {
			return 0, errors.New("kafka test response framing is invalid")
		}
		frame := make([]byte, frameBytes+4)
		copy(frame, sizeBytes[:])
		if _, err := io.ReadFull(connection.Conn, frame[4:]); err != nil {
			return 0, err
		}
		correlation := int32(binary.BigEndian.Uint32(frame[4:8]))
		if connection.dropResponse(correlation) &&
			connection.dropper.recordDrop() {
			_ = connection.Conn.Close()

			return 0, io.EOF
		}
		connection.responseData = frame
	}
	count := copy(data, connection.responseData)
	connection.responseData = connection.responseData[count:]

	return count, nil
}

func (connection *kafkaResponseDroppingConn) dropResponse(correlation int32) bool {
	connection.correlationMu.Lock()
	defer connection.correlationMu.Unlock()

	if _, exists := connection.correlations[correlation]; !exists {
		return false
	}
	delete(connection.correlations, correlation)

	return true
}

func proveProducerCommitResponseLoss(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
) {
	t.Helper()

	dropper := &kafkaResponseDropper{apiKey: kafkaEndTxnAPIKey}
	producer, err := kafka.NewProducerWithDialerForTest(
		kafka.ProducerConfig{
			Brokers:               brokers,
			ClientID:              "golib-apache-response-loss-producer",
			AllowedTopics:         []string{topic},
			TransactionalID:       "golib-apache-response-loss-producer",
			TransactionEndTimeout: 2 * time.Second,
			Security:              kafka.DevelopmentPlaintextSecurity(),
		},
		dropper.DialContext,
	)
	if err != nil {
		t.Fatalf("construct response-loss producer: %v", err)
	}
	defer func() {
		if err := producer.Close(); err != nil {
			t.Errorf("close response-loss producer: %v", err)
		}
	}()

	err = producer.RunTransaction(ctx, func(transaction kafka.Transaction) error {
		return transaction.Publish(ctx, kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte("response-loss"),
			Value: []byte("response-loss"),
		})
	})
	var transactionErr *kafka.TransactionError
	if !errors.As(err, &transactionErr) ||
		!errors.Is(err, kafka.ErrTransactionOutcomeUnknown) ||
		transactionErr.Operation() != kafka.TransactionOperationCommit ||
		transactionErr.Category() != kafka.ErrorAmbiguous ||
		transactionErr.Abortable() ||
		transactionErr.OutcomeKnown() {
		t.Fatalf("response-loss transaction error = %v", err)
	}
	if dropper.droppedResponses() == 0 {
		t.Fatal("response-loss producer did not drop an EndTxn response")
	}
	values := consumeTransactionValues(
		t,
		brokers,
		topic,
		kgo.ReadCommitted(),
		1,
	)
	if len(values) != 1 || values[0] != "response-loss" {
		t.Fatalf("response-loss committed values = %q", values)
	}
}

func proveProducerResponseLoss(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	inspector *kafka.Inspector,
	topic string,
) {
	t.Helper()

	dropper := &kafkaResponseDropper{apiKey: kafkaProduceAPIKey}
	producer, err := kafka.NewProducerWithDialerForTest(
		kafka.ProducerConfig{
			Brokers:         brokers,
			ClientID:        "golib-apache-produce-response-loss",
			AllowedTopics:   []string{topic},
			DeliveryTimeout: time.Second,
			RequestTimeout:  100 * time.Millisecond,
			ShutdownTimeout: 2 * time.Second,
			Security:        kafka.DevelopmentPlaintextSecurity(),
		},
		dropper.DialContext,
	)
	if err != nil {
		t.Fatalf("construct produce-response-loss producer: %v", err)
	}
	defer func() {
		if err := producer.Close(); err != nil {
			t.Errorf("close produce-response-loss producer: %v", err)
		}
	}()

	startedAt := time.Now()
	result := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic:     topic,
		Partition: kafka.ExplicitPartition(0),
		Key:       []byte("produce-response-loss"),
		Value:     []byte("produce-response-loss"),
	})
	duration := time.Since(startedAt)
	var deliveryErr *kafka.DeliveryError
	if !errors.As(result.Err, &deliveryErr) {
		t.Fatalf("produce-response-loss error type = %T", result.Err)
	}
	timedOut := errors.Is(result.Err, kgo.ErrRecordTimeout) ||
		errors.Is(result.Err, context.DeadlineExceeded)
	if deliveryErr.Category() != kafka.ErrorAmbiguous || !timedOut {
		t.Fatalf(
			"produce-response-loss category=%s timeout=%t",
			deliveryErr.Category(),
			timedOut,
		)
	}
	if duration > 3*time.Second {
		t.Fatalf(
			"produce-response-loss duration = %s drops=%d",
			duration,
			dropper.droppedResponses(),
		)
	}
	if dropper.droppedResponses() == 0 {
		t.Fatal("produce-response-loss producer did not drop a Produce response")
	}
	values := consumeTransactionValues(
		t,
		brokers,
		topic,
		kgo.ReadCommitted(),
		1,
	)
	if len(values) != 1 || values[0] != "produce-response-loss" {
		t.Fatalf("produce-response-loss values = %q", values)
	}
	states, err := inspector.Topics(ctx, topic)
	if err != nil {
		t.Fatalf("inspect produce-response-loss offsets: %v", err)
	}
	if len(states) != 1 ||
		len(states[0].Partitions) != 1 ||
		states[0].Partitions[0].BeginningOffset != 0 ||
		states[0].Partitions[0].EndOffset != 1 {
		t.Fatalf("produce-response-loss topic state = %#v", states)
	}
}

func proveTransactionalProducerResponseLoss(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
) {
	t.Helper()

	dropper := &kafkaResponseDropper{apiKey: kafkaProduceAPIKey}
	producer, err := kafka.NewProducerWithDialerForTest(
		kafka.ProducerConfig{
			Brokers:               brokers,
			ClientID:              "golib-apache-transaction-produce-response-loss",
			AllowedTopics:         []string{topic},
			DeliveryTimeout:       time.Second,
			RequestTimeout:        100 * time.Millisecond,
			ShutdownTimeout:       2 * time.Second,
			TransactionalID:       "golib-apache-transaction-produce-response-loss",
			TransactionTimeout:    10 * time.Second,
			TransactionEndTimeout: time.Second,
			Security:              kafka.DevelopmentPlaintextSecurity(),
		},
		dropper.DialContext,
	)
	if err != nil {
		t.Fatalf("construct transaction-produce-response-loss producer: %v", err)
	}
	defer func() {
		if err := producer.Close(); err != nil {
			t.Errorf("close transaction-produce-response-loss producer: %v", err)
		}
	}()

	publishCtx, cancelPublish := context.WithTimeout(ctx, 2*time.Second)
	droppingStopped := make(chan struct{})
	go func() {
		<-publishCtx.Done()
		dropper.disable()
		close(droppingStopped)
	}()
	startedAt := time.Now()
	err = producer.RunTransaction(ctx, func(transaction kafka.Transaction) error {
		return transaction.Publish(publishCtx, kafka.ProducerRecord{
			Topic:     topic,
			Partition: kafka.ExplicitPartition(0),
			Key:       []byte("transaction-produce-response-loss"),
			Value:     []byte("transaction-produce-response-loss"),
		})
	})
	duration := time.Since(startedAt)
	cancelPublish()
	<-droppingStopped

	var deliveryErr *kafka.DeliveryError
	if !errors.As(err, &deliveryErr) ||
		deliveryErr.Category() != kafka.ErrorAmbiguous ||
		!errors.Is(err, context.DeadlineExceeded) {
		t.Errorf(
			"transaction-produce-response-loss error = %T %v",
			err,
			err,
		)
	}
	if duration > 3*time.Second {
		t.Errorf(
			"transaction-produce-response-loss duration = %s drops=%d",
			duration,
			dropper.droppedResponses(),
		)
	}
	if dropper.droppedResponses() == 0 {
		t.Error("transaction producer did not drop a Produce response")
	}
	if retryErr := producer.RunTransaction(
		ctx,
		func(kafka.Transaction) error { return nil },
	); !errors.Is(retryErr, kafka.ErrProducerFatal) {
		t.Errorf("transaction producer after response loss = %v", retryErr)
	}
	assertNoApacheKafkaTransactionValues(t, brokers, topic, kgo.ReadCommitted())
	values := consumeTransactionValues(
		t,
		brokers,
		topic,
		kgo.ReadUncommitted(),
		1,
	)
	if len(values) != 1 || values[0] != "transaction-produce-response-loss" {
		t.Fatalf("transaction-produce-response-loss values = %q", values)
	}
}

func proveTransactionProcessorProduceResponseLoss(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	sourceTopic string,
	outputTopic string,
) {
	t.Helper()

	const groupID = "golib-apache-processor-produce-response-loss"
	sourceProducer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "golib-apache-processor-response-loss-source",
		AllowedTopics: []string{sourceTopic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct processor response-loss source producer: %v", err)
	}
	if err := sourceProducer.Publish(ctx, kafka.ProducerRecord{
		Topic: sourceTopic,
		Key:   []byte("processor-response-loss"),
		Value: []byte("processor-response-loss"),
	}); err != nil {
		t.Fatalf("publish processor response-loss source: %v", err)
	}
	if err := sourceProducer.Close(); err != nil {
		t.Fatalf("close processor response-loss source producer: %v", err)
	}

	dropper := &kafkaResponseDropper{apiKey: kafkaProduceAPIKey}
	processor, err := kafka.NewTransactionProcessorWithDialerForTest(
		kafka.TransactionProcessorConfig{
			Connection: kafka.TransactionConnectionConfig{
				Brokers:  brokers,
				ClientID: "golib-apache-processor-produce-response-loss",
				Security: kafka.DevelopmentPlaintextSecurity(),
			},
			Group: kafka.TransactionGroupConfig{
				GroupID:           groupID,
				Topics:            []string{sourceTopic},
				ResetOffset:       kafka.OffsetEarliest,
				MaxPollRecords:    1,
				ProcessingTimeout: 4 * time.Second,
			},
			Output: kafka.TransactionOutputConfig{
				AllowedTopics:         []string{outputTopic},
				TransactionalID:       groupID,
				DeliveryTimeout:       time.Second,
				RequestTimeout:        100 * time.Millisecond,
				RetryBackoffMin:       100 * time.Millisecond,
				RetryBackoffMax:       100 * time.Millisecond,
				TransactionTimeout:    10 * time.Second,
				TransactionEndTimeout: time.Second,
			},
			ShutdownTimeout: 2 * time.Second,
		},
		dropper.DialContext,
	)
	if err != nil {
		t.Fatalf("construct processor response-loss client: %v", err)
	}

	var handlerStartedAt time.Time
	result, err := processor.RunOnce(
		ctx,
		kafka.TransactionHandlerFunc(func(
			handlerCtx context.Context,
			_ kafka.ConsumedRecord,
			transaction kafka.Transaction,
		) error {
			handlerStartedAt = time.Now()

			return transaction.Publish(handlerCtx, kafka.ProducerRecord{
				Topic: outputTopic,
				Key:   []byte("processor-response-loss"),
				Value: []byte("processor-response-loss"),
			})
		}),
	)
	duration := time.Since(handlerStartedAt)
	var deliveryErr *kafka.DeliveryError
	if result.Polled != 1 || result.Processed != 0 ||
		result.Published != 0 || result.Committed ||
		!errors.Is(err, kafka.ErrTransactionProcessorFatal) ||
		!errors.Is(err, context.DeadlineExceeded) ||
		!errors.As(err, &deliveryErr) ||
		deliveryErr.Category() != kafka.ErrorAmbiguous {
		t.Errorf("processor response-loss result/error = %#v/%v", result, err)
	}
	if duration > 3*time.Second {
		t.Errorf("processor response-loss duration = %s", duration)
	}
	if dropper.droppedResponses() == 0 {
		t.Error("transaction processor did not drop a Produce response")
	}
	if _, retryErr := processor.RunOnce(
		ctx,
		kafka.TransactionHandlerFunc(func(
			context.Context,
			kafka.ConsumedRecord,
			kafka.Transaction,
		) error {
			return nil
		}),
	); !errors.Is(retryErr, kafka.ErrTransactionProcessorFatal) {
		t.Errorf("transaction processor after response loss = %v", retryErr)
	}
	if err := processor.Close(); err != nil {
		t.Fatalf("close processor response-loss client: %v", err)
	}
	assertApacheKafkaGroupCommits(
		t,
		ctx,
		brokers,
		groupID,
		map[kafka.TopicPartition]int64{{Topic: sourceTopic, Partition: 0}: -1},
	)
	assertNoApacheKafkaTransactionValues(
		t,
		brokers,
		outputTopic,
		kgo.ReadCommitted(),
	)
	values := consumeTransactionValues(
		t,
		brokers,
		outputTopic,
		kgo.ReadUncommitted(),
		1,
	)
	if len(values) != 1 || values[0] != "processor-response-loss" {
		t.Fatalf("processor response-loss output values = %q", values)
	}
}

func proveProducerFencing(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
) {
	t.Helper()

	const transactionalID = "golib-apache-transaction-fencing"
	newProducer := func(clientID string) *kafka.Producer {
		producer, err := kafka.NewProducer(kafka.ProducerConfig{
			Brokers:         brokers,
			ClientID:        clientID,
			AllowedTopics:   []string{topic},
			TransactionalID: transactionalID,
			Security:        kafka.DevelopmentPlaintextSecurity(),
		})
		if err != nil {
			t.Fatalf("construct %s: %v", clientID, err)
		}
		t.Cleanup(func() {
			if err := producer.Close(); err != nil {
				t.Errorf("close %s: %v", clientID, err)
			}
		})

		return producer
	}

	original := newProducer("golib-apache-fenced-producer")
	replacement := newProducer("golib-apache-replacement-producer")
	originalStarted := make(chan struct{})
	releaseOriginal := make(chan struct{})
	originalResult := make(chan error, 1)
	released := false
	defer func() {
		if !released {
			close(releaseOriginal)
		}
	}()

	go func() {
		originalResult <- original.RunTransaction(
			ctx,
			func(transaction kafka.Transaction) error {
				if err := transaction.Publish(ctx, kafka.ProducerRecord{
					Topic: topic,
					Key:   []byte("original"),
					Value: []byte("original"),
				}); err != nil {
					return err
				}
				close(originalStarted)
				select {
				case <-releaseOriginal:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		)
	}()

	select {
	case <-originalStarted:
	case <-ctx.Done():
		t.Fatalf("wait for original transaction: %v", ctx.Err())
	}

	if err := replacement.RunTransaction(
		ctx,
		func(transaction kafka.Transaction) error {
			return transaction.Publish(ctx, kafka.ProducerRecord{
				Topic: topic,
				Key:   []byte("replacement"),
				Value: []byte("replacement"),
			})
		},
	); err != nil {
		t.Fatalf("replacement transaction: %v", err)
	}

	close(releaseOriginal)
	released = true
	var originalErr error
	select {
	case originalErr = <-originalResult:
	case <-ctx.Done():
		t.Fatalf("wait for fenced transaction: %v", ctx.Err())
	}

	var transactionErr *kafka.TransactionError
	if !errors.As(originalErr, &transactionErr) ||
		transactionErr.Operation() != kafka.TransactionOperationCommit ||
		transactionErr.Category() != kafka.ErrorFenced ||
		transactionErr.Abortable() ||
		!transactionErr.OutcomeKnown() ||
		(!errors.Is(originalErr, kerr.ProducerFenced) &&
			!errors.Is(originalErr, kerr.InvalidProducerEpoch)) {
		t.Fatalf("original transaction error = %v", originalErr)
	}

	values := consumeTransactionValues(
		t,
		brokers,
		topic,
		kgo.ReadCommitted(),
		1,
	)
	if len(values) != 1 || values[0] != "replacement" {
		t.Fatalf("read-committed fenced values = %q", values)
	}
}

func proveTransactionProcessorTerminationRecovery(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	sourceTopic string,
	outputTopic string,
) {
	t.Helper()

	const (
		groupID         = "golib-apache-termination-processor"
		transactionalID = "golib-apache-termination-processor"
	)
	recoveryCtx, cancelRecovery := context.WithTimeout(ctx, 45*time.Second)
	defer cancelRecovery()

	sourceProducer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "golib-apache-termination-source",
		AllowedTopics: []string{sourceTopic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct termination source producer: %v", err)
	}
	sourceProducerClosed := false
	defer func() {
		if !sourceProducerClosed {
			_ = sourceProducer.Close()
		}
	}()
	if err := sourceProducer.Publish(recoveryCtx, kafka.ProducerRecord{
		Topic: sourceTopic,
		Key:   []byte("terminated"),
		Value: []byte("terminated"),
	}); err != nil {
		t.Fatalf("publish termination source: %v", err)
	}
	if err := sourceProducer.Close(); err != nil {
		t.Fatalf("close termination source producer: %v", err)
	}
	sourceProducerClosed = true

	command := exec.CommandContext(
		recoveryCtx,
		os.Args[0],
		"-test.run=^TestApacheKafkaTransactionProcessorChild$",
	)
	command.Env = append(
		os.Environ(),
		apacheKafkaProcessorChildMode+"="+
			apacheKafkaProcessorModeTermination,
		apacheKafkaProcessorBrokers+"="+strings.Join(brokers, ","),
		apacheKafkaProcessorSource+"="+sourceTopic,
		apacheKafkaProcessorOutput+"="+outputTopic,
		apacheKafkaProcessorGroup+"="+groupID,
		apacheKafkaProcessorTxnID+"="+transactionalID,
	)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("open transaction processor child stdout: %v", err)
	}
	var stderr apacheKafkaProcessorDiagnostic
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start transaction processor child: %v", err)
	}
	childExited := false
	defer func() {
		if childExited {
			return
		}
		_ = command.Process.Kill()
		_ = command.Wait()
	}()

	ready := false
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		if scanner.Text() == apacheKafkaProcessorReady {
			ready = true

			break
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read transaction processor child stdout: %v", err)
	}
	if !ready {
		t.Fatalf(
			"transaction processor child exited before output acknowledgement: %s",
			strings.TrimSpace(stderr.String()),
		)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("terminate transaction processor child: %v", err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("terminated transaction processor child exited successfully")
	}
	childExited = true

	processor := newApacheKafkaTerminationProcessor(
		t,
		brokers,
		sourceTopic,
		outputTopic,
		groupID,
		transactionalID,
		"golib-apache-termination-replacement",
	)
	defer func() {
		if err := processor.Close(); err != nil {
			t.Errorf("close replacement transaction processor: %v", err)
		}
	}()

	var result kafka.TransactionPollResult
	for result.Polled == 0 {
		result, err = processor.RunOnce(
			recoveryCtx,
			kafka.TransactionHandlerFunc(func(
				ctx context.Context,
				record kafka.ConsumedRecord,
				transaction kafka.Transaction,
			) error {
				return transaction.Publish(ctx, kafka.ProducerRecord{
					Topic: outputTopic,
					Key:   record.Key,
					Value: append([]byte("recovered-"), record.Value...),
				})
			}),
		)
		if err != nil {
			t.Fatalf("replacement transaction processor: %v", err)
		}
		if cause := context.Cause(recoveryCtx); cause != nil {
			t.Fatalf("wait for replacement source record: %v", cause)
		}
	}
	if result != (kafka.TransactionPollResult{
		Polled: 1, Processed: 1, Published: 1, Committed: true,
	}) {
		t.Fatalf("replacement transaction result = %#v", result)
	}

	assertPartitionCommits(
		t,
		recoveryCtx,
		brokers,
		sourceTopic,
		groupID,
		map[int32]int64{0: 1},
	)
	if values := consumeTransactionValues(
		t,
		brokers,
		outputTopic,
		kgo.ReadCommitted(),
		1,
	); len(values) != 1 || values[0] != "recovered-terminated" {
		t.Fatalf("read-committed termination values = %q", values)
	}
	if values := consumeTransactionValues(
		t,
		brokers,
		outputTopic,
		kgo.ReadUncommitted(),
		2,
	); len(values) != 2 ||
		values[0] != "crashed-terminated" ||
		values[1] != "recovered-terminated" {
		t.Fatalf("read-uncommitted termination values = %q", values)
	}
}

type apacheKafkaProcessorChildProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  apacheKafkaProcessorDiagnostic
	exited  bool
}

func startApacheKafkaConsumerChild(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
	clientID string,
	balance string,
) *apacheKafkaProcessorChildProcess {
	return startApacheKafkaConsumerChildWithRack(
		t, ctx, brokers, topic, groupID, clientID, balance, "", 0,
	)
}

func startApacheKafkaRackConsumerChild(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
	clientID string,
	rack string,
	fetchBroker int32,
) *apacheKafkaProcessorChildProcess {
	return startApacheKafkaConsumerChildWithRack(
		t,
		ctx,
		brokers,
		topic,
		groupID,
		clientID,
		apacheKafkaConsumerBalanceCoop,
		rack,
		fetchBroker,
	)
}

func startApacheKafkaConsumerChildWithRack(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	groupID string,
	clientID string,
	balance string,
	rack string,
	fetchBroker int32,
) *apacheKafkaProcessorChildProcess {
	t.Helper()

	command := exec.CommandContext(
		ctx,
		os.Args[0],
		"-test.run=^TestApacheKafkaConsumerChild$",
	)
	fetchBrokerValue := ""
	if fetchBroker != 0 {
		fetchBrokerValue = strconv.FormatInt(int64(fetchBroker), 10)
	}
	command.Env = append(
		os.Environ(),
		apacheKafkaConsumerChildMode+"=1",
		apacheKafkaConsumerBrokers+"="+strings.Join(brokers, ","),
		apacheKafkaConsumerTopic+"="+topic,
		apacheKafkaConsumerGroup+"="+groupID,
		apacheKafkaConsumerClient+"="+clientID,
		apacheKafkaConsumerBalance+"="+balance,
		apacheKafkaConsumerRack+"="+rack,
		apacheKafkaConsumerFetchBroker+"="+fetchBrokerValue,
	)
	readyMarker := apacheKafkaConsumerReady
	if fetchBroker != 0 {
		readyMarker = fmt.Sprintf(
			"%s:%d",
			apacheKafkaConsumerRackFetch,
			fetchBroker,
		)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("open consumer child stdin: %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		t.Fatalf("open consumer child stdout: %v", err)
	}
	child := &apacheKafkaProcessorChildProcess{
		command: command,
		stdin:   stdin,
		scanner: bufio.NewScanner(stdout),
	}
	command.Stderr = &child.stderr
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		t.Fatalf("start consumer child: %v", err)
	}
	t.Cleanup(child.stop)
	waitForApacheKafkaProcessorMarker(
		t,
		child.scanner,
		readyMarker,
		&child.stderr,
	)
	if fetchBroker != 0 {
		if _, err := child.stdin.Write([]byte{1}); err != nil {
			t.Fatalf("arm rack-local consumer child: %v", err)
		}
		waitForApacheKafkaProcessorMarker(
			t,
			child.scanner,
			apacheKafkaConsumerRackArmed,
			&child.stderr,
		)
	}

	return child
}

func (child *apacheKafkaProcessorChildProcess) releaseConsumerAndWait(
	t *testing.T,
) {
	t.Helper()

	if _, err := child.stdin.Write([]byte{1}); err != nil {
		t.Fatalf("release consumer child: %v", err)
	}
	if err := child.stdin.Close(); err != nil {
		t.Fatalf("close consumer child stdin: %v", err)
	}
	waitForApacheKafkaProcessorMarker(
		t,
		child.scanner,
		apacheKafkaConsumerStopped,
		&child.stderr,
	)
	if err := child.command.Wait(); err != nil {
		t.Fatalf(
			"wait for consumer child: %v: %s",
			err,
			strings.TrimSpace(child.stderr.String()),
		)
	}
	child.exited = true
}

func startApacheKafkaRebalanceChild(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	sourceTopic string,
	outputTopic string,
	groupID string,
	transactionalID string,
	balance string,
) (*apacheKafkaProcessorChildProcess, int32) {
	t.Helper()

	command := exec.CommandContext(
		ctx,
		os.Args[0],
		"-test.run=^TestApacheKafkaTransactionProcessorChild$",
	)
	command.Env = append(
		os.Environ(),
		apacheKafkaProcessorChildMode+"="+apacheKafkaProcessorModeRebalance,
		apacheKafkaProcessorBrokers+"="+strings.Join(brokers, ","),
		apacheKafkaProcessorSource+"="+sourceTopic,
		apacheKafkaProcessorOutput+"="+outputTopic,
		apacheKafkaProcessorGroup+"="+groupID,
		apacheKafkaProcessorTxnID+"="+transactionalID,
		apacheKafkaProcessorBalance+"="+balance,
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("open rebalance child stdin: %v", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("open rebalance child stdout: %v", err)
	}
	child := &apacheKafkaProcessorChildProcess{
		command: command,
		stdin:   stdin,
		scanner: bufio.NewScanner(stdout),
	}
	command.Stderr = &child.stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start rebalance child: %v", err)
	}
	t.Cleanup(child.stop)

	waitForApacheKafkaProcessorMarker(
		t,
		child.scanner,
		apacheKafkaProcessorReady,
		&child.stderr,
	)
	partitionText := waitForApacheKafkaProcessorMarkerPrefix(
		t,
		child.scanner,
		apacheKafkaProcessorPartition+"=",
		&child.stderr,
	)
	partition, err := strconv.ParseInt(partitionText, 10, 32)
	if err != nil || partition < 0 {
		t.Fatalf("rebalance child source partition = %q", partitionText)
	}

	return child, int32(partition)
}

func (child *apacheKafkaProcessorChildProcess) releaseAndWait(t *testing.T) {
	t.Helper()

	if _, err := child.stdin.Write([]byte{1}); err != nil {
		t.Fatalf("release rebalance child: %v", err)
	}
	if err := child.stdin.Close(); err != nil {
		t.Fatalf("close rebalance child stdin: %v", err)
	}
	waitForApacheKafkaProcessorMarker(
		t,
		child.scanner,
		apacheKafkaProcessorAborted,
		&child.stderr,
	)
	if err := child.command.Wait(); err != nil {
		t.Fatalf(
			"wait for rebalance child: %v: %s",
			err,
			strings.TrimSpace(child.stderr.String()),
		)
	}
	child.exited = true
}

func (child *apacheKafkaProcessorChildProcess) stop() {
	_ = child.stdin.Close()
	if child.exited {
		return
	}
	_ = child.command.Process.Kill()
	_ = child.command.Wait()
}

func proveTransactionProcessorRebalance(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	sourceTopic string,
	outputTopic string,
	triggerTopic string,
) {
	t.Helper()

	const (
		groupID    = "golib-apache-rebalance-processor"
		childID    = "golib-apache-rebalance-child"
		triggerID  = "golib-apache-rebalance-trigger"
		recoveryID = "golib-apache-rebalance-recovery"
	)
	rebalanceCtx, cancelRebalance := context.WithTimeout(ctx, 45*time.Second)
	defer cancelRebalance()

	sourceProducer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "golib-apache-rebalance-source",
		AllowedTopics: []string{sourceTopic, triggerTopic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct rebalance source producer: %v", err)
	}
	sourceProducerClosed := false
	defer func() {
		if !sourceProducerClosed {
			_ = sourceProducer.Close()
		}
	}()
	if err := sourceProducer.Publish(rebalanceCtx, kafka.ProducerRecord{
		Topic: sourceTopic,
		Key:   []byte("rebalance"),
		Value: []byte("rebalance"),
	}); err != nil {
		t.Fatalf("publish rebalance source: %v", err)
	}
	if err := sourceProducer.Publish(rebalanceCtx, kafka.ProducerRecord{
		Topic: triggerTopic,
		Key:   []byte("trigger"),
		Value: []byte("trigger"),
	}); err != nil {
		t.Fatalf("publish rebalance trigger: %v", err)
	}
	if err := sourceProducer.Close(); err != nil {
		t.Fatalf("close rebalance source producer: %v", err)
	}
	sourceProducerClosed = true

	child, _ := startApacheKafkaRebalanceChild(
		t,
		rebalanceCtx,
		brokers,
		sourceTopic,
		outputTopic,
		groupID,
		childID,
		apacheKafkaProcessorBalanceEager,
	)

	trigger := newApacheKafkaRebalanceProcessor(
		t,
		brokers,
		triggerTopic,
		outputTopic,
		groupID,
		triggerID,
		"golib-apache-rebalance-trigger",
		kafka.BalanceEagerSticky,
	)
	triggerClosed := false
	defer func() {
		if !triggerClosed {
			_ = trigger.Close()
		}
	}()

	type runResult struct {
		result kafka.TransactionPollResult
		err    error
	}
	triggerResult := make(chan runResult, 1)
	go func() {
		result, err := trigger.RunOnce(
			rebalanceCtx,
			kafka.TransactionHandlerFunc(func(
				context.Context,
				kafka.ConsumedRecord,
				kafka.Transaction,
			) error {
				return nil
			}),
		)
		triggerResult <- runResult{result: result, err: err}
	}()

	waitForApacheKafkaGroupMembers(
		t,
		rebalanceCtx,
		brokers,
		groupID,
		2,
	)
	var triggerRun runResult
	select {
	case triggerRun = <-triggerResult:
	case <-rebalanceCtx.Done():
		t.Fatalf(
			"wait for rebalance trigger: %v",
			context.Cause(rebalanceCtx),
		)
	}
	if triggerRun.err != nil ||
		triggerRun.result != (kafka.TransactionPollResult{
			Polled: 1, Processed: 1, Committed: true,
		}) {
		t.Fatalf(
			"rebalance trigger result = (%#v, %v)",
			triggerRun.result,
			triggerRun.err,
		)
	}

	child.releaseAndWait(t)

	if err := trigger.Close(); err != nil {
		t.Fatalf("close rebalance trigger: %v", err)
	}
	triggerClosed = true
	waitForApacheKafkaGroupMembers(
		t,
		rebalanceCtx,
		brokers,
		groupID,
		0,
	)

	recovery := newApacheKafkaRebalanceProcessor(
		t,
		brokers,
		sourceTopic,
		outputTopic,
		groupID,
		recoveryID,
		"golib-apache-rebalance-recovery",
		kafka.BalanceEagerSticky,
	)
	defer func() {
		if err := recovery.Close(); err != nil {
			t.Errorf("close rebalance recovery: %v", err)
		}
	}()
	var recoveryRun runResult
	for recoveryRun.result.Polled == 0 &&
		recoveryRun.err == nil &&
		context.Cause(rebalanceCtx) == nil {
		recoveryRun.result, recoveryRun.err = recovery.RunOnce(
			rebalanceCtx,
			kafka.TransactionHandlerFunc(func(
				ctx context.Context,
				record kafka.ConsumedRecord,
				transaction kafka.Transaction,
			) error {
				return transaction.Publish(ctx, kafka.ProducerRecord{
					Topic: outputTopic,
					Key:   record.Key,
					Value: append([]byte("committed-"), record.Value...),
				})
			}),
		)
	}
	if recoveryRun.err != nil ||
		recoveryRun.result != (kafka.TransactionPollResult{
			Polled: 1, Processed: 1, Published: 1, Committed: true,
		}) {
		t.Fatalf(
			"rebalance recovery result = (%#v, %v)",
			recoveryRun.result,
			recoveryRun.err,
		)
	}

	assertApacheKafkaGroupCommits(
		t,
		rebalanceCtx,
		brokers,
		groupID,
		map[kafka.TopicPartition]int64{
			{Topic: sourceTopic, Partition: 0}:  1,
			{Topic: triggerTopic, Partition: 0}: 1,
		},
	)
	if values := consumeTransactionValues(
		t,
		brokers,
		outputTopic,
		kgo.ReadCommitted(),
		1,
	); len(values) != 1 || values[0] != "committed-rebalance" {
		t.Fatalf("read-committed rebalance values = %q", values)
	}
	if values := consumeTransactionValues(
		t,
		brokers,
		outputTopic,
		kgo.ReadUncommitted(),
		2,
	); len(values) != 2 ||
		values[0] != "rebalanced-rebalance" ||
		values[1] != "committed-rebalance" {
		t.Fatalf("read-uncommitted rebalance values = %q", values)
	}
}

func proveTransactionProcessorCooperativeRebalance(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	sourceTopic string,
	outputTopic string,
) {
	t.Helper()

	const (
		groupID       = "golib-apache-cooperative-processor"
		childID       = "golib-apache-cooperative-child"
		replacementID = "golib-apache-cooperative-replacement"
		recoveryID    = "golib-apache-cooperative-recovery"
	)
	cooperativeCtx, cancelCooperative := context.WithTimeout(
		ctx,
		45*time.Second,
	)
	defer cancelCooperative()

	sourceProducer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "golib-apache-cooperative-source",
		AllowedTopics: []string{sourceTopic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct cooperative source producer: %v", err)
	}
	sourceProducerClosed := false
	defer func() {
		if !sourceProducerClosed {
			_ = sourceProducer.Close()
		}
	}()
	for partition := int32(0); partition < 2; partition++ {
		value := fmt.Sprintf("p%d", partition)
		if result := sourceProducer.PublishRecord(
			cooperativeCtx,
			kafka.ProducerRecord{
				Topic:     sourceTopic,
				Partition: kafka.ExplicitPartition(partition),
				Key:       []byte(value),
				Value:     []byte(value),
			},
		); result.Err != nil {
			t.Fatalf(
				"publish cooperative source partition %d: %v",
				partition,
				result.Err,
			)
		}
	}
	if err := sourceProducer.Close(); err != nil {
		t.Fatalf("close cooperative source producer: %v", err)
	}
	sourceProducerClosed = true

	child, childPartition := startApacheKafkaRebalanceChild(
		t,
		cooperativeCtx,
		brokers,
		sourceTopic,
		outputTopic,
		groupID,
		childID,
		apacheKafkaProcessorBalanceCoop,
	)
	if childPartition < 0 || childPartition > 1 {
		t.Fatalf("cooperative child partition = %d", childPartition)
	}
	var replacementPartition int32

	replacement := newApacheKafkaRebalanceProcessor(
		t,
		brokers,
		sourceTopic,
		outputTopic,
		groupID,
		replacementID,
		"golib-apache-cooperative-replacement",
		kafka.BalanceCooperativeSticky,
	)
	replacementClosed := false
	defer func() {
		if !replacementClosed {
			_ = replacement.Close()
		}
	}()

	type runResult struct {
		result kafka.TransactionPollResult
		err    error
	}
	replacementResult := make(chan runResult, 1)
	go func() {
		var run runResult
		for run.result.Polled == 0 &&
			run.err == nil &&
			context.Cause(cooperativeCtx) == nil {
			run.result, run.err = replacement.RunOnce(
				cooperativeCtx,
				kafka.TransactionHandlerFunc(func(
					ctx context.Context,
					record kafka.ConsumedRecord,
					transaction kafka.Transaction,
				) error {
					replacementPartition = record.Partition

					return transaction.Publish(ctx, kafka.ProducerRecord{
						Topic: outputTopic,
						Key:   record.Key,
						Value: append(
							[]byte("cooperative-"),
							record.Value...,
						),
					})
				}),
			)
		}
		replacementResult <- run
	}()

	waitForApacheKafkaGroupMembers(
		t,
		cooperativeCtx,
		brokers,
		groupID,
		2,
	)
	var replacementRun runResult
	select {
	case replacementRun = <-replacementResult:
	case <-cooperativeCtx.Done():
		t.Fatalf(
			"wait for cooperative replacement: %v",
			context.Cause(cooperativeCtx),
		)
	}
	if replacementRun.err != nil ||
		replacementRun.result != (kafka.TransactionPollResult{
			Polled: 1, Processed: 1, Published: 1, Committed: true,
		}) {
		t.Fatalf(
			"cooperative replacement result = (%#v, %v)",
			replacementRun.result,
			replacementRun.err,
		)
	}
	if replacementPartition < 0 || replacementPartition > 1 {
		t.Fatalf("cooperative replacement partition = %d", replacementPartition)
	}
	remainingPartition := int32(1) - replacementPartition

	child.releaseAndWait(t)
	if err := replacement.Close(); err != nil {
		t.Fatalf("close cooperative replacement: %v", err)
	}
	replacementClosed = true
	waitForApacheKafkaGroupMembers(
		t,
		cooperativeCtx,
		brokers,
		groupID,
		0,
	)

	recovery := newApacheKafkaRebalanceProcessor(
		t,
		brokers,
		sourceTopic,
		outputTopic,
		groupID,
		recoveryID,
		"golib-apache-cooperative-recovery",
		kafka.BalanceCooperativeSticky,
	)
	defer func() {
		if err := recovery.Close(); err != nil {
			t.Errorf("close cooperative recovery: %v", err)
		}
	}()
	var recoveryRun runResult
	for recoveryRun.result.Polled == 0 &&
		recoveryRun.err == nil &&
		context.Cause(cooperativeCtx) == nil {
		recoveryRun.result, recoveryRun.err = recovery.RunOnce(
			cooperativeCtx,
			kafka.TransactionHandlerFunc(func(
				ctx context.Context,
				record kafka.ConsumedRecord,
				transaction kafka.Transaction,
			) error {
				if record.Partition != remainingPartition {
					return fmt.Errorf(
						"recovery partition = %d, want %d",
						record.Partition,
						remainingPartition,
					)
				}

				return transaction.Publish(ctx, kafka.ProducerRecord{
					Topic: outputTopic,
					Key:   record.Key,
					Value: append([]byte("recovered-"), record.Value...),
				})
			}),
		)
	}
	if recoveryRun.err != nil ||
		recoveryRun.result != (kafka.TransactionPollResult{
			Polled: 1, Processed: 1, Published: 1, Committed: true,
		}) {
		t.Fatalf(
			"cooperative recovery result = (%#v, %v)",
			recoveryRun.result,
			recoveryRun.err,
		)
	}

	assertApacheKafkaGroupCommits(
		t,
		cooperativeCtx,
		brokers,
		groupID,
		map[kafka.TopicPartition]int64{
			{Topic: sourceTopic, Partition: 0}: 1,
			{Topic: sourceTopic, Partition: 1}: 1,
		},
	)
	childValue := fmt.Sprintf("p%d", childPartition)
	replacementValue := fmt.Sprintf("p%d", replacementPartition)
	remainingValue := fmt.Sprintf("p%d", remainingPartition)
	if values := consumeTransactionValues(
		t,
		brokers,
		outputTopic,
		kgo.ReadCommitted(),
		2,
	); len(values) != 2 ||
		values[0] != "cooperative-"+replacementValue ||
		values[1] != "recovered-"+remainingValue {
		t.Fatalf("read-committed cooperative values = %q", values)
	}
	if values := consumeTransactionValues(
		t,
		brokers,
		outputTopic,
		kgo.ReadUncommitted(),
		3,
	); len(values) != 3 ||
		values[0] != "rebalanced-"+childValue ||
		values[1] != "cooperative-"+replacementValue ||
		values[2] != "recovered-"+remainingValue {
		t.Fatalf("read-uncommitted cooperative values = %q", values)
	}
}

func waitForApacheKafkaProcessorMarker(
	t *testing.T,
	scanner *bufio.Scanner,
	marker string,
	stderr *apacheKafkaProcessorDiagnostic,
) {
	t.Helper()

	for scanner.Scan() {
		if scanner.Text() == marker {
			return
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read transaction processor child stdout: %v", err)
	}
	t.Fatalf(
		"transaction processor child exited before %q: %s",
		marker,
		strings.TrimSpace(stderr.String()),
	)
}

func waitForApacheKafkaProcessorMarkerPrefix(
	t *testing.T,
	scanner *bufio.Scanner,
	prefix string,
	stderr *apacheKafkaProcessorDiagnostic,
) string {
	t.Helper()

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read transaction processor child stdout: %v", err)
	}
	t.Fatalf(
		"transaction processor child exited before prefix %q: %s",
		prefix,
		strings.TrimSpace(stderr.String()),
	)

	return ""
}

func waitForApacheKafkaGroupMembers(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	groupID string,
	count int,
) {
	t.Helper()

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "golib-apache-rebalance-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct rebalance inspector: %v", err)
	}
	defer inspector.Close()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastGroups []kafka.ConsumerGroupState
	var lastErr error
	for {
		groups, err := inspector.ConsumerGroupLag(ctx, groupID)
		stateMatches := len(groups) == 1 &&
			(groups[0].State == "Stable" ||
				(count == 0 && groups[0].State == "Empty"))
		if err == nil &&
			stateMatches &&
			len(groups[0].Members) == count {
			return
		}
		lastGroups = groups
		lastErr = err

		select {
		case <-ctx.Done():
			t.Fatalf(
				"wait for %d rebalance members: %v; state = %#v; "+
					"last error = %v",
				count,
				context.Cause(ctx),
				lastGroups,
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func waitForApacheKafkaConsumerGroupState(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	groupID string,
	topic string,
	protocol string,
	clientIDs []string,
) {
	t.Helper()

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "golib-apache-consumer-rolling-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct consumer rolling inspector: %v", err)
	}
	defer inspector.Close()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastGroups []kafka.ConsumerGroupState
	var lastErr error
	for {
		groups, err := inspector.ConsumerGroupLag(ctx, groupID)
		if err == nil && len(groups) == 1 &&
			apacheKafkaConsumerGroupStateMatches(
				groups[0], topic, protocol, clientIDs,
			) {
			return
		}
		lastGroups = groups
		lastErr = err

		select {
		case <-ctx.Done():
			t.Fatalf(
				"wait for consumer rolling group protocol %q with %d members: "+
					"%v; state = %#v; last error = %v",
				protocol,
				len(clientIDs),
				context.Cause(ctx),
				lastGroups,
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func apacheKafkaConsumerGroupStateMatches(
	group kafka.ConsumerGroupState,
	topic string,
	protocol string,
	clientIDs []string,
) bool {
	members := len(clientIDs)
	if group.State != "Stable" ||
		group.ProtocolType != "consumer" ||
		group.Protocol != protocol ||
		members < 1 ||
		members > 2 ||
		len(group.Members) != members ||
		len(group.Partitions) != 2 {
		return false
	}
	wantedClients := make(map[string]bool, members)
	for _, clientID := range clientIDs {
		if clientID == "" || wantedClients[clientID] {
			return false
		}
		wantedClients[clientID] = true
	}
	assigned := map[int32]bool{}
	for _, member := range group.Members {
		if !wantedClients[member.ClientID] || len(member.Assignments) != 2/members {
			return false
		}
		delete(wantedClients, member.ClientID)
		for _, assignment := range member.Assignments {
			if assignment.Topic != topic ||
				assignment.Partition < 0 ||
				assignment.Partition > 1 ||
				assigned[assignment.Partition] {
				return false
			}
			assigned[assignment.Partition] = true
		}
	}

	return len(wantedClients) == 0 && assigned[0] && assigned[1]
}

func assertApacheKafkaGroupCommits(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	groupID string,
	want map[kafka.TopicPartition]int64,
) {
	t.Helper()

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "golib-apache-rebalance-commit-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct rebalance commit inspector: %v", err)
	}
	defer inspector.Close()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastGroups []kafka.ConsumerGroupState
	var lastErr error
	for {
		groups, err := inspector.ConsumerGroupLag(ctx, groupID)
		if err == nil && apacheKafkaGroupCommitsMatch(groups, want) {
			return
		}
		lastGroups = groups
		lastErr = err

		select {
		case <-ctx.Done():
			t.Fatalf(
				"wait for rebalance commits: %v; state = %#v; "+
					"last error = %v",
				context.Cause(ctx),
				lastGroups,
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func apacheKafkaGroupCommitsMatch(
	groups []kafka.ConsumerGroupState,
	want map[kafka.TopicPartition]int64,
) bool {
	if len(groups) != 1 || len(groups[0].Partitions) != len(want) {
		return false
	}
	for _, partition := range groups[0].Partitions {
		offset, exists := want[kafka.TopicPartition{
			Topic: partition.Topic, Partition: partition.Partition,
		}]
		if !exists || partition.CommittedOffset != offset {
			return false
		}
	}

	return true
}

func waitForApacheTransactionState(
	ctx context.Context,
	admin *kadm.Client,
	transactionalID string,
	want string,
) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var last kadm.DescribedTransaction
	var lastErr error
	for {
		described, err := admin.DescribeTransactions(ctx, transactionalID)
		if err == nil {
			last, err = described.On(transactionalID, nil)
		}
		if err == nil && last.State == want {
			return nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return fmt.Errorf(
				"wait for transaction %q state %q: %v; "+
					"last state = %q; last error = %v",
				transactionalID,
				want,
				context.Cause(ctx),
				last.State,
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func assertNoApacheKafkaTransactionValues(
	t *testing.T,
	brokers []string,
	topic string,
	isolation kgo.IsolationLevel,
) {
	t.Helper()

	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-apache-transaction-empty-reader"),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{
			topic: {0: kgo.NewOffset().AtStart()},
		}),
		kgo.FetchIsolationLevel(isolation),
		kgo.DialTimeout(10*time.Second),
	)
	if err != nil {
		t.Fatalf("construct empty transaction reader: %v", err)
	}
	defer client.Close()

	readCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	fetches := client.PollRecords(readCtx, 1)
	if records := fetches.Records(); len(records) != 0 {
		t.Fatalf("unexpected transaction records = %#v", records)
	}
	for _, fetchErr := range fetches.Errors() {
		if !errors.Is(fetchErr.Err, context.DeadlineExceeded) &&
			!errors.Is(fetchErr.Err, context.Canceled) {
			t.Fatalf("read empty transaction records: %v", fetchErr.Err)
		}
	}
}

func newApacheKafkaTerminationProcessor(
	t *testing.T,
	brokers []string,
	sourceTopic string,
	outputTopic string,
	groupID string,
	transactionalID string,
	clientID string,
) *kafka.TransactionProcessor {
	t.Helper()

	processor, err := kafka.NewTransactionProcessor(
		kafka.TransactionProcessorConfig{
			Connection: kafka.TransactionConnectionConfig{
				Brokers:  brokers,
				ClientID: clientID,
				Security: kafka.DevelopmentPlaintextSecurity(),
			},
			Group: kafka.TransactionGroupConfig{
				GroupID:           groupID,
				Topics:            []string{sourceTopic},
				ResetOffset:       kafka.OffsetEarliest,
				MaxPollRecords:    1,
				SessionTimeout:    6 * time.Second,
				HeartbeatInterval: 2 * time.Second,
				ProcessingTimeout: 30 * time.Second,
			},
			Output: kafka.TransactionOutputConfig{
				AllowedTopics:         []string{outputTopic},
				TransactionalID:       transactionalID,
				TransactionTimeout:    45 * time.Second,
				TransactionEndTimeout: 2 * time.Second,
			},
		},
	)
	if err != nil {
		t.Fatalf("construct %s: %v", clientID, err)
	}

	return processor
}

func newApacheKafkaRebalanceProcessor(
	t *testing.T,
	brokers []string,
	sourceTopic string,
	outputTopic string,
	groupID string,
	transactionalID string,
	clientID string,
	balancePolicy kafka.GroupBalancePolicy,
) *kafka.TransactionProcessor {
	t.Helper()

	processor, err := kafka.NewTransactionProcessor(
		kafka.TransactionProcessorConfig{
			Connection: kafka.TransactionConnectionConfig{
				Brokers:  brokers,
				ClientID: clientID,
				Security: kafka.DevelopmentPlaintextSecurity(),
			},
			Group: kafka.TransactionGroupConfig{
				GroupID:           groupID,
				Topics:            []string{sourceTopic},
				ResetOffset:       kafka.OffsetEarliest,
				BalancePolicy:     balancePolicy,
				MaxPollRecords:    1,
				SessionTimeout:    6 * time.Second,
				HeartbeatInterval: 2 * time.Second,
				ProcessingTimeout: 30 * time.Second,
			},
			Output: kafka.TransactionOutputConfig{
				AllowedTopics:         []string{outputTopic},
				TransactionalID:       transactionalID,
				TransactionTimeout:    45 * time.Second,
				TransactionEndTimeout: 2 * time.Second,
			},
		},
	)
	if err != nil {
		t.Fatalf("construct %s: %v", clientID, err)
	}

	return processor
}

type apacheKafkaCluster struct {
	nodes []apacheKafkaNode
}

type apacheKafkaNode struct {
	id        int32
	alias     string
	container testcontainers.Container
}

func (cluster *apacheKafkaCluster) observeFailureState(t *testing.T) {
	t.Helper()

	t.Cleanup(func() {
		if !t.Failed() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, node := range cluster.nodes {
			state, err := node.container.State(ctx)
			if err != nil {
				t.Logf("inspect failed Apache Kafka node %d: %v", node.id, err)

				continue
			}
			t.Logf(
				"failed Apache Kafka node %d running=%t status=%s exit=%d",
				node.id,
				state.Running,
				state.Status,
				state.ExitCode,
			)
		}
	})
}

func startApacheKafkaCluster(
	t *testing.T,
	ctx context.Context,
) *apacheKafkaCluster {
	t.Helper()

	dockerNetwork := newApacheKafkaNetwork(t, ctx)
	testcontainers.CleanupNetwork(t, dockerNetwork)

	cluster := &apacheKafkaCluster{nodes: make([]apacheKafkaNode, 0, 3)}
	for nodeID := int32(1); nodeID <= 3; nodeID++ {
		alias := fmt.Sprintf("kafka-%d", nodeID)
		request := testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        apacheKafkaImage,
				ExposedPorts: []string{apacheKafkaClientPort},
				Env:          apacheKafkaEnvironment(nodeID),
				Networks:     []string{dockerNetwork.Name},
				NetworkAliases: map[string][]string{
					dockerNetwork.Name: {alias},
				},
				Entrypoint: []string{"sh"},
				Cmd: []string{
					"-c",
					"while [ ! -f " + apacheKafkaStartFile +
						" ]; do sleep 0.05; done; exec /bin/bash " +
						apacheKafkaStartFile,
				},
			},
		}
		container, err := testcontainers.GenericContainer(ctx, request)
		if container != nil {
			testcontainers.CleanupContainer(t, container)
		}
		if err != nil {
			t.Fatalf("create Apache Kafka node %d: %v", nodeID, err)
		}
		cluster.nodes = append(cluster.nodes, apacheKafkaNode{
			id: nodeID, alias: alias, container: container,
		})
	}

	for _, node := range cluster.nodes {
		if err := node.container.Start(ctx); err != nil {
			t.Fatalf("start Apache Kafka node %d: %v", node.id, err)
		}
	}
	for _, node := range cluster.nodes {
		endpoint, err := node.container.PortEndpoint(
			ctx,
			apacheKafkaClientPort,
			"",
		)
		if err != nil {
			t.Fatalf("resolve Apache Kafka node %d endpoint: %v", node.id, err)
		}
		script := fmt.Sprintf(
			"#!/bin/bash\n"+
				"export KAFKA_ADVERTISED_LISTENERS="+
				"'PLAINTEXT://%s:19092,PLAINTEXT_HOST://%s'\n"+
				"shutdown() {\n"+
				"  if [ -s %[3]s ]; then\n"+
				"    pid=\"$(cat %[3]s)\"\n"+
				"    kill -TERM \"$pid\" 2>/dev/null\n"+
				"    wait \"$pid\" 2>/dev/null\n"+
				"  fi\n"+
				"  exit 0\n"+
				"}\n"+
				"trap shutdown TERM INT\n"+
				"while true; do\n"+
				"  while [ -f %[4]s ]; do sleep 0.05; done\n"+
				"  /etc/kafka/docker/run &\n"+
				"  pid=\"$!\"\n"+
				"  printf '%%s\\n' \"$pid\" > %[3]s\n"+
				"  wait \"$pid\"\n"+
				"  rm -f %[3]s\n"+
				"  touch %[4]s\n"+
				"done\n",
			node.alias,
			endpoint,
			apacheKafkaPIDFile,
			apacheKafkaStopFile,
		)
		if err := node.container.CopyToContainer(
			ctx,
			[]byte(script),
			apacheKafkaStartFile,
			0o755,
		); err != nil {
			t.Fatalf("configure Apache Kafka node %d: %v", node.id, err)
		}
	}
	for _, node := range cluster.nodes {
		cluster.waitForNode(t, ctx, node)
	}

	return cluster
}

func newApacheKafkaNetwork(
	t *testing.T,
	ctx context.Context,
) *testcontainers.DockerNetwork {
	t.Helper()

	start := int(time.Now().UnixNano() % apacheKafkaSubnetPool)
	var lastErr error
	for attempt := range apacheKafkaSubnetPool {
		index := (start + attempt) % apacheKafkaSubnetPool
		prefix := netip.PrefixFrom(
			netip.AddrFrom4([4]byte{
				10,
				253,
				byte(index / 16),
				byte(index%16) * 16,
			}),
			28,
		)
		dockerNetwork, err := network.New(
			ctx,
			network.WithIPAM(&dockernetwork.IPAM{
				Driver: "default",
				Config: []dockernetwork.IPAMConfig{{Subnet: prefix}},
			}),
		)
		if err == nil {
			return dockerNetwork
		}
		if context.Cause(ctx) != nil ||
			!strings.Contains(err.Error(), "Pool overlaps") {
			t.Fatalf("create Apache Kafka network: %v", err)
		}
		lastErr = err
	}

	t.Fatalf(
		"create Apache Kafka network from dedicated subnet pool: %v",
		lastErr,
	)

	return nil
}

func apacheKafkaEnvironment(nodeID int32) map[string]string {
	return map[string]string{
		"KAFKA_NODE_ID":                                          fmt.Sprint(nodeID),
		"KAFKA_BROKER_RACK":                                      fmt.Sprintf("rack-%d", nodeID),
		"KAFKA_REPLICA_SELECTOR_CLASS":                           "org.apache.kafka.common.replica.RackAwareReplicaSelector",
		"KAFKA_PROCESS_ROLES":                                    "broker,controller",
		"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP":                   "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT,PLAINTEXT_HOST:PLAINTEXT",
		"KAFKA_CONTROLLER_QUORUM_VOTERS":                         "1@kafka-1:9093,2@kafka-2:9093,3@kafka-3:9093",
		"KAFKA_LISTENERS":                                        "PLAINTEXT://:19092,CONTROLLER://:9093,PLAINTEXT_HOST://:9092",
		"KAFKA_INTER_BROKER_LISTENER_NAME":                       "PLAINTEXT",
		"KAFKA_CONTROLLER_LISTENER_NAMES":                        "CONTROLLER",
		"CLUSTER_ID":                                             apacheKafkaClusterID,
		"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":                 "3",
		"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":                    "2",
		"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR":         "3",
		"KAFKA_SHARE_COORDINATOR_STATE_TOPIC_REPLICATION_FACTOR": "3",
		"KAFKA_SHARE_COORDINATOR_STATE_TOPIC_MIN_ISR":            "2",
		"KAFKA_DEFAULT_REPLICATION_FACTOR":                       "3",
		"KAFKA_MIN_INSYNC_REPLICAS":                              "2",
		"KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS":                 "0",
		"KAFKA_AUTO_CREATE_TOPICS_ENABLE":                        "false",
		"KAFKA_LOG_DIRS":                                         "/tmp/kraft-combined-logs",
	}
}

func (cluster *apacheKafkaCluster) brokers(
	t *testing.T,
	ctx context.Context,
) []string {
	t.Helper()

	brokers := make([]string, 0, len(cluster.nodes))
	for _, node := range cluster.nodes {
		endpoint, err := node.container.PortEndpoint(
			ctx,
			apacheKafkaClientPort,
			"",
		)
		if err != nil {
			t.Fatalf("resolve Apache Kafka node %d endpoint: %v", node.id, err)
		}
		brokers = append(brokers, endpoint)
	}

	return brokers
}

func (cluster *apacheKafkaCluster) assertRuntimeVersion(
	t *testing.T,
	ctx context.Context,
	want string,
) {
	t.Helper()

	exitCode, output, err := cluster.nodes[0].container.Exec(
		ctx,
		[]string{"/opt/kafka/bin/kafka-topics.sh", "--version"},
		tcexec.Multiplexed(),
	)
	if err != nil {
		t.Fatalf("query Apache Kafka runtime version: %v", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(output, 256))
	if readErr != nil {
		t.Fatalf("read Apache Kafka runtime version: %v", readErr)
	}
	if exitCode != 0 || strings.TrimSpace(string(data)) != want {
		t.Fatalf(
			"Apache Kafka runtime version = %q, exit %d; want %q",
			strings.TrimSpace(string(data)),
			exitCode,
			want,
		)
	}
}

func (cluster *apacheKafkaCluster) stopNode(
	t *testing.T,
	ctx context.Context,
	nodeID int32,
) {
	t.Helper()

	node := cluster.node(t, nodeID)
	exitCode, _, err := node.container.Exec(ctx, []string{
		"sh",
		"-c",
		"touch " + apacheKafkaStopFile + "; " +
			"if [ -s " + apacheKafkaPIDFile + " ]; then " +
			"kill -TERM \"$(cat " + apacheKafkaPIDFile + ")\"; fi; " +
			"while [ -f " + apacheKafkaPIDFile + " ]; do sleep 0.05; done",
	})
	if err != nil {
		t.Fatalf("stop Apache Kafka node %d: %v", nodeID, err)
	}
	if exitCode != 0 {
		t.Fatalf("stop Apache Kafka node %d: exit %d", nodeID, exitCode)
	}
}

func (cluster *apacheKafkaCluster) startNode(
	t *testing.T,
	ctx context.Context,
	nodeID int32,
) {
	t.Helper()

	node := cluster.node(t, nodeID)
	exitCode, _, err := node.container.Exec(
		ctx,
		[]string{"rm", "-f", apacheKafkaStopFile},
	)
	if err != nil {
		t.Fatalf("restart Apache Kafka node %d: %v", nodeID, err)
	}
	if exitCode != 0 {
		t.Fatalf("restart Apache Kafka node %d: exit %d", nodeID, exitCode)
	}
	cluster.waitForNode(t, ctx, node)
}

func (cluster *apacheKafkaCluster) node(
	t *testing.T,
	nodeID int32,
) apacheKafkaNode {
	t.Helper()

	for _, node := range cluster.nodes {
		if node.id == nodeID {
			return node
		}
	}
	t.Fatalf("Apache Kafka node %d is not part of the fixture", nodeID)

	return apacheKafkaNode{}
}

func (cluster *apacheKafkaCluster) waitForNode(
	t *testing.T,
	ctx context.Context,
	node apacheKafkaNode,
) {
	t.Helper()

	if err := wait.ForListeningPort(apacheKafkaClientPort).
		WithStartupTimeout(2*time.Minute).
		WithPollInterval(100*time.Millisecond).
		WaitUntilReady(ctx, node.container); err != nil {
		t.Fatalf("wait for Apache Kafka node %d: %v", node.id, err)
	}
}

func waitForApacheTopicState(
	t *testing.T,
	ctx context.Context,
	inspector *kafka.Inspector,
	topic string,
	accept func(kafka.TopicState) bool,
) kafka.TopicState {
	t.Helper()

	waitCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var lastState kafka.TopicState
	var lastErr error
	for {
		states, err := inspector.Topics(waitCtx, topic)
		if err == nil && len(states) == 1 {
			lastState = states[0]
			if accept(lastState) {
				return lastState
			}
		} else {
			lastErr = err
		}

		select {
		case <-waitCtx.Done():
			t.Fatalf(
				"wait for Apache Kafka topic state: %v; last state = %#v; "+
					"last error = %v",
				context.Cause(waitCtx),
				lastState,
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func waitForApacheBrokerEndpoints(
	t *testing.T,
	ctx context.Context,
	brokers []string,
) {
	t.Helper()

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	lastFailedBroker := -1
	for {
		allReachable := true
		for index, broker := range brokers {
			client, err := kgo.NewClient(
				kgo.SeedBrokers(broker),
				kgo.ClientID(fmt.Sprintf("golib-apache-readiness-%d", index+1)),
				kgo.DialTimeout(time.Second),
				kgo.RequestTimeoutOverhead(time.Second),
			)
			if err != nil {
				t.Fatalf("construct Apache Kafka readiness client: %v", err)
			}
			pingCtx, pingCancel := context.WithTimeout(waitCtx, 2*time.Second)
			err = client.Ping(pingCtx)
			pingCancel()
			client.Close()
			if err != nil {
				allReachable = false
				lastFailedBroker = index

				break
			}
		}
		if allReachable {
			return
		}

		select {
		case <-waitCtx.Done():
			t.Fatalf(
				"wait for every Apache Kafka client endpoint: %v; "+
					"last failed broker index = %d",
				context.Cause(waitCtx),
				lastFailedBroker,
			)
		case <-ticker.C:
		}
	}
}

func allPartitionsMatch(
	state kafka.TopicState,
	replicationFactor int,
	inSyncReplicas int,
) bool {
	for _, partition := range state.Partitions {
		if partition.ReplicationFactor != replicationFactor ||
			partition.InSyncReplicas != inSyncReplicas {
			return false
		}
	}

	return true
}

func apacheTransactionalIDForLeader(
	t *testing.T,
	state kafka.TopicState,
	leader int32,
) string {
	t.Helper()

	leaders := make(map[int32]int32, len(state.Partitions))
	for _, partition := range state.Partitions {
		leaders[partition.Partition] = partition.Leader
	}
	for candidate := range 10_000 {
		transactionalID := fmt.Sprintf(
			"golib-apache-coordinator-failover-%d",
			candidate,
		)
		partition := apacheTransactionCoordinatorPartition(
			transactionalID,
			len(state.Partitions),
		)
		if leaders[partition] == leader {
			return transactionalID
		}
	}

	t.Fatalf("no transactional ID maps to Apache Kafka broker %d", leader)

	return ""
}

func apacheTransactionCoordinatorPartition(
	transactionalID string,
	partitionCount int,
) int32 {
	var hash int32
	for _, character := range transactionalID {
		hash = 31*hash + int32(character)
	}

	return int32((uint32(hash) & math.MaxInt32) % uint32(partitionCount))
}

func proveApacheKafkaRackLocalFetch(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	producer *kafka.Producer,
	inspector *kafka.Inspector,
	topic string,
) {
	t.Helper()

	state := waitForApacheTopicState(t, ctx, inspector, topic, func(
		state kafka.TopicState,
	) bool {
		return len(state.Partitions) == 1 && allPartitionsMatch(state, 3, 3)
	})
	partition := state.Partitions[0]
	follower := int32(0)
	for _, replica := range partition.Replicas {
		if replica != partition.Leader {
			follower = replica

			break
		}
	}
	if follower == 0 {
		t.Fatalf("rack-local topic has no follower: %#v", partition)
	}

	groupID := fmt.Sprintf("golib-apache-rack-local-%d", time.Now().UnixNano())
	child := startApacheKafkaRackConsumerChild(
		t,
		ctx,
		brokers,
		topic,
		groupID,
		"golib-apache-rack-local-consumer",
		fmt.Sprintf("rack-%d", follower),
		follower,
	)
	assertApacheKafkaDelivery(t, ctx, producer, topic, 0, "rack-local")
	waitForApacheKafkaProcessorMarker(
		t,
		child.scanner,
		fmt.Sprintf("%s:%d", apacheKafkaConsumerRackRecord, follower),
		&child.stderr,
	)
	assertPartitionCommits(
		t,
		ctx,
		brokers,
		topic,
		groupID,
		map[int32]int64{0: 1},
	)
	child.releaseConsumerAndWait(t)
}

func assertApacheKafkaDelivery(
	t *testing.T,
	ctx context.Context,
	producer *kafka.Producer,
	topic string,
	partition int32,
	value string,
) {
	t.Helper()

	result := producer.PublishRecord(ctx, kafka.ProducerRecord{
		Topic:     topic,
		Partition: kafka.ExplicitPartition(partition),
		Key:       []byte("aggregate-1"),
		Value:     []byte(value),
	})
	if result.Err != nil || result.Partition != partition {
		t.Fatalf("Apache Kafka delivery = %#v", result)
	}
}

func proveProducerBatchPartialDelivery(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	acceptedTopic string,
	rejectedTopic string,
) {
	t.Helper()

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:                brokers,
		ClientID:               "golib-apache-partial-batch-producer",
		AllowedTopics:          []string{acceptedTopic, rejectedTopic},
		CompressionPreferences: []kafka.CompressionCodec{kafka.CompressionNone},
		Security:               kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct partial-batch producer: %v", err)
	}
	defer func() {
		if closeErr := producer.Close(); closeErr != nil {
			t.Errorf("close partial-batch producer: %v", closeErr)
		}
	}()

	results, err := producer.PublishBatch(ctx, []kafka.ProducerRecord{
		{
			Topic:     acceptedTopic,
			Partition: kafka.ExplicitPartition(0),
			Key:       []byte("accepted"),
			Value:     []byte("accepted"),
		},
		{
			Topic:     rejectedTopic,
			Partition: kafka.ExplicitPartition(0),
			Key:       []byte("rejected"),
			Value:     bytes.Repeat([]byte{0xa5}, 2<<10),
		},
	})
	var deliveryErr *kafka.DeliveryError
	if !errors.Is(err, kafka.ErrBatchDeliveryFailed) ||
		!errors.Is(err, kerr.MessageTooLarge) || len(results) != 2 ||
		results[0].Topic != acceptedTopic || results[0].Partition != 0 ||
		results[0].Offset != 0 || results[0].Timestamp.IsZero() ||
		results[0].Err != nil || results[1].Topic != rejectedTopic ||
		results[1].Partition != 0 ||
		!errors.As(results[1].Err, &deliveryErr) ||
		deliveryErr.Category() != kafka.ErrorOversized {
		t.Fatalf("partial batch results/error = %#v/%v", results, err)
	}
	if values := consumeTransactionValues(
		t,
		brokers,
		acceptedTopic,
		kgo.ReadUncommitted(),
		1,
	); !slices.Equal(values, []string{"accepted"}) {
		t.Fatalf("partial batch accepted values = %#v", values)
	}
	assertNoApacheKafkaTransactionValues(
		t,
		brokers,
		rejectedTopic,
		kgo.ReadUncommitted(),
	)
}

func createApacheKafkaTopic(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	partitions int32,
) {
	t.Helper()
	createApacheKafkaTopicWithConfigs(t, ctx, brokers, topic, partitions, nil)
}

func createApacheKafkaTopicWithConfigs(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	partitions int32,
	additional map[string]*string,
) {
	t.Helper()

	configs := map[string]*string{
		"min.insync.replicas":            kadm.StringPtr("2"),
		"unclean.leader.election.enable": kadm.StringPtr("false"),
	}
	for key, value := range additional {
		configs[key] = value
	}

	createIntegrationTopicWithReplication(
		t,
		ctx,
		brokers,
		topic,
		partitions,
		3,
		configs,
	)
}
