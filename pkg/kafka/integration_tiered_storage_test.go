//go:build interoperability

package kafka_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/testcontainers/testcontainers-go"
	tcexec "github.com/testcontainers/testcontainers-go/exec"
	"github.com/twmb/franz-go/pkg/kadm"
)

const (
	apacheKafkaTieredStorageJarURL = "https://repo.maven.apache.org/maven2/" +
		"org/apache/kafka/kafka-storage/4.3.1/" +
		"kafka-storage-4.3.1-test.jar"
	apacheKafkaTieredStorageJarPath = "/opt/kafka/libs/" +
		"kafka-storage-4.3.1-test.jar"
	apacheKafkaTieredStorageJarDigest = "f3236936854c13815ffaa7f35a3f2d6861c125616cba5395b538b8680dded886"
	apacheKafkaTieredStorageJarSize   = 638_839
	apacheKafkaTieredStorageAlias     = "tiered-kafka"
	apacheKafkaTieredStorageTopic     = "golib-tiered-storage"
	apacheKafkaTieredStorageDir       = "/tmp/kafka-remote-storage"
	apacheKafkaTieredStorageLogDir    = "/tmp/kafka-logs"
)

func TestApacheKafkaTieredStorageReplayCompatibility(t *testing.T) {
	runKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	testJar := downloadApacheKafkaTieredStorageTestJar(t, ctx)
	node := startApacheKafkaTieredStorageNode(t, ctx, testJar)
	cluster := &apacheKafkaCluster{nodes: []apacheKafkaNode{node}}
	cluster.assertRuntimeVersion(t, ctx, "4.3.1")
	brokers := cluster.brokers(t, ctx)

	createIntegrationTopicWithReplication(
		t,
		ctx,
		brokers,
		apacheKafkaTieredStorageTopic,
		1,
		1,
		map[string]*string{
			"file.delete.delay.ms":           kadm.StringPtr("100"),
			"local.retention.bytes":          kadm.StringPtr("1"),
			"local.retention.ms":             kadm.StringPtr("-2"),
			"min.insync.replicas":            kadm.StringPtr("1"),
			"remote.log.copy.disable":        kadm.StringPtr("false"),
			"remote.storage.enable":          kadm.StringPtr("true"),
			"retention.bytes":                kadm.StringPtr("-1"),
			"retention.ms":                   kadm.StringPtr("-1"),
			"segment.bytes":                  kadm.StringPtr("1048576"),
			"segment.ms":                     kadm.StringPtr("500"),
			"unclean.leader.election.enable": kadm.StringPtr("false"),
		},
	)

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:                brokers,
		ClientID:               "golib-tiered-storage-producer",
		AllowedTopics:          []string{apacheKafkaTieredStorageTopic},
		KeyPolicy:              kafka.KeyRequired,
		CompressionPreferences: []kafka.CompressionCodec{kafka.CompressionNone},
		Security:               kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct tiered-storage producer: %v", err)
	}
	payloads := make([][]byte, 6)
	for index := range payloads {
		payloads[index] = bytes.Repeat([]byte{byte(0xa0 + index)}, 700<<10)
		result := producer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic:     apacheKafkaTieredStorageTopic,
			Partition: kafka.ExplicitPartition(0),
			Key:       []byte(fmt.Sprintf("tiered-%d", index)),
			Value:     payloads[index],
		})
		if result.Err != nil || result.Partition != 0 ||
			result.Offset != int64(index) {
			t.Fatalf("tiered-storage delivery %d = %#v", index, result)
		}
	}
	if err := producer.Close(); err != nil {
		t.Fatalf("close tiered-storage producer: %v", err)
	}

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  brokers,
		ClientID: "golib-tiered-storage-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct tiered-storage inspector: %v", err)
	}
	defer func() {
		if closeErr := inspector.Close(); closeErr != nil {
			t.Errorf("close tiered-storage inspector: %v", closeErr)
		}
	}()
	waitForApacheTieredStorageEviction(t, ctx, node)
	state := waitForApacheTopicState(
		t,
		ctx,
		inspector,
		apacheKafkaTieredStorageTopic,
		func(state kafka.TopicState) bool {
			return tieredStorageTopicStateMatches(state)
		},
	)
	if len(state.Partitions) != 1 ||
		state.Partitions[0].BeginningOffset != 0 ||
		state.Partitions[0].EndOffset != int64(len(payloads)) {
		t.Fatalf("tiered-storage topic offsets = %#v", state)
	}

	reader, err := kafka.NewReplayReader(kafka.ReplayConfig{
		Brokers:  brokers,
		ClientID: "golib-tiered-storage-replay",
		Ranges: []kafka.ReplayRange{{
			Topic: apacheKafkaTieredStorageTopic, Partition: 0,
			StartOffset: 0, EndOffset: 1,
		}},
		SideEffects:     kafka.ReplaySideEffectsAllowed,
		FetchMaxWait:    100 * time.Millisecond,
		ProgressTimeout: 30 * time.Second,
		Security:        kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct tiered-storage replay reader: %v", err)
	}
	wantDigest := sha256.Sum256(payloads[0])
	var replayed int
	var replayMatched bool
	result, replayErr := reader.Replay(ctx, kafka.ReplayHandlerFunc(func(
		_ context.Context,
		record kafka.ReplayRecord,
	) error {
		replayed++
		gotDigest := sha256.Sum256(record.Value)
		replayMatched = record.Topic == apacheKafkaTieredStorageTopic &&
			record.Partition == 0 && record.Offset == 0 &&
			gotDigest == wantDigest &&
			record.Metadata.Range.StartOffset == 0 &&
			record.Metadata.Range.EndOffset == 1 &&
			record.Metadata.EffectiveStartOffset == 0

		return nil
	}))
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatalf("close tiered-storage replay reader: %v", closeErr)
	}
	if replayErr != nil || replayed != 1 || !replayMatched ||
		result.Processed != 1 ||
		result.CompletedRanges != 1 || result.IncompleteRanges != 0 ||
		len(result.Ranges) != 1 || !result.Ranges[0].Complete ||
		result.Ranges[0].NextOffset != 1 {
		t.Fatalf("tiered-storage replay result/error = %#v/%v", result, replayErr)
	}
}

func downloadApacheKafkaTieredStorageTestJar(
	t *testing.T,
	ctx context.Context,
) []byte {
	t.Helper()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		apacheKafkaTieredStorageJarURL,
		nil,
	)
	if err != nil {
		t.Fatalf("construct tiered-storage test-jar request: %v", err)
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("download tiered-storage test jar: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("download tiered-storage test jar: status %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(
		response.Body,
		apacheKafkaTieredStorageJarSize+1,
	))
	if err != nil {
		t.Fatalf("read tiered-storage test jar: %v", err)
	}
	digest := sha256.Sum256(data)
	if len(data) != apacheKafkaTieredStorageJarSize ||
		fmt.Sprintf("%x", digest) != apacheKafkaTieredStorageJarDigest {
		t.Fatalf(
			"tiered-storage test-jar identity = size %d digest %x",
			len(data),
			digest,
		)
	}

	return data
}

func startApacheKafkaTieredStorageNode(
	t *testing.T,
	ctx context.Context,
	testJar []byte,
) apacheKafkaNode {
	t.Helper()

	dockerNetwork := newApacheKafkaNetwork(t, ctx)
	cleanupApacheKafkaNetwork(t, dockerNetwork)
	request := testcontainers.ContainerRequest{
		Image:        apacheKafkaImage,
		ExposedPorts: []string{apacheKafkaClientPort},
		Env:          apacheKafkaTieredStorageEnvironment(),
		Networks:     []string{dockerNetwork.Name},
		NetworkAliases: map[string][]string{
			dockerNetwork.Name: {apacheKafkaTieredStorageAlias},
		},
		Files: []testcontainers.ContainerFile{{
			Reader:            bytes.NewReader(testJar),
			ContainerFilePath: apacheKafkaTieredStorageJarPath,
			FileMode:          0o444,
		}},
		Entrypoint: []string{"sh"},
		Cmd: []string{
			"-c",
			"while [ ! -f " + apacheKafkaReadyFile +
				" ]; do sleep 0.05; done; exec /bin/bash " +
				apacheKafkaStartFile,
		},
	}
	container := createApacheKafkaContainer(t, ctx, request, "tiered", 1)
	node := apacheKafkaNode{
		id: 1, alias: apacheKafkaTieredStorageAlias, container: container,
	}
	observeApacheKafkaFailureState(t, []apacheKafkaNode{node})
	if err := container.Start(ctx); err != nil {
		t.Fatalf("start tiered-storage Apache Kafka node: %v", err)
	}
	endpoint := waitForApacheKafkaPortEndpoint(
		t,
		ctx,
		node,
		apacheKafkaClientPort,
	)
	configureApacheKafkaNode(
		t,
		ctx,
		node,
		apacheKafkaRunLoopScript(
			apacheKafkaTieredStorageAlias,
			endpoint,
			"PLAINTEXT",
		),
	)
	waitForApacheKafkaNodeWithTimeout(
		t,
		ctx,
		node,
		apacheKafkaClientPort,
		3*time.Minute,
	)

	return node
}

func apacheKafkaTieredStorageEnvironment() map[string]string {
	return map[string]string{
		"CLUSTER_ID":                                                     apacheKafkaClusterID,
		"KAFKA_AUTO_CREATE_TOPICS_ENABLE":                                "false",
		"KAFKA_CONTROLLER_LISTENER_NAMES":                                "CONTROLLER",
		"KAFKA_CONTROLLER_QUORUM_VOTERS":                                 "1@tiered-kafka:9093",
		"KAFKA_DEFAULT_REPLICATION_FACTOR":                               "1",
		"KAFKA_FILE_DELETE_DELAY_MS":                                     "100",
		"KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS":                         "0",
		"KAFKA_INTER_BROKER_LISTENER_NAME":                               "PLAINTEXT",
		"KAFKA_LISTENERS":                                                "PLAINTEXT://:19092,CONTROLLER://:9093,PLAINTEXT_HOST://:9092",
		"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP":                           "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT,PLAINTEXT_HOST:PLAINTEXT",
		"KAFKA_LOG_DIRS":                                                 apacheKafkaTieredStorageLogDir,
		"KAFKA_LOG_RETENTION_CHECK_INTERVAL_MS":                          "100",
		"KAFKA_MIN_INSYNC_REPLICAS":                                      "1",
		"KAFKA_NODE_ID":                                                  "1",
		"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":                         "1",
		"KAFKA_PROCESS_ROLES":                                            "broker,controller",
		"KAFKA_REMOTE_LOG_MANAGER_TASK_INTERVAL_MS":                      "100",
		"KAFKA_REMOTE_LOG_METADATA_MANAGER_IMPL_PREFIX":                  "rlmm.config.",
		"KAFKA_REMOTE_LOG_METADATA_MANAGER_LISTENER_NAME":                "PLAINTEXT",
		"KAFKA_REMOTE_LOG_STORAGE_MANAGER_CLASS_NAME":                    "org.apache.kafka.server.log.remote.storage.LocalTieredStorage",
		"KAFKA_REMOTE_LOG_STORAGE_MANAGER_CLASS_PATH":                    apacheKafkaTieredStorageJarPath,
		"KAFKA_REMOTE_LOG_STORAGE_MANAGER_IMPL_PREFIX":                   "rsm.config.",
		"KAFKA_REMOTE_LOG_STORAGE_SYSTEM_ENABLE":                         "true",
		"KAFKA_RLMM_CONFIG_REMOTE_LOG_METADATA_TOPIC_MIN_ISR":            "1",
		"KAFKA_RLMM_CONFIG_REMOTE_LOG_METADATA_TOPIC_NUM_PARTITIONS":     "1",
		"KAFKA_RLMM_CONFIG_REMOTE_LOG_METADATA_TOPIC_REPLICATION_FACTOR": "1",
		"KAFKA_RSM_CONFIG_DELETE_ON_CLOSE":                               "false",
		"KAFKA_RSM_CONFIG_DIR":                                           apacheKafkaTieredStorageDir,
		"KAFKA_SHARE_COORDINATOR_STATE_TOPIC_MIN_ISR":                    "1",
		"KAFKA_SHARE_COORDINATOR_STATE_TOPIC_REPLICATION_FACTOR":         "1",
		"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":                            "1",
		"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR":                 "1",
	}
}

func waitForApacheTieredStorageEviction(
	t *testing.T,
	ctx context.Context,
	node apacheKafkaNode,
) {
	t.Helper()

	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastState string
	var lastErr error
	for {
		exitCode, output, err := node.container.Exec(
			waitCtx,
			[]string{
				"sh",
				"-c",
				"remote=$(find " + apacheKafkaTieredStorageDir +
					" -type f -name '*.log' 2>/dev/null | wc -l); " +
					"if [ -e " + apacheKafkaTieredStorageLogDir + "/" +
					apacheKafkaTieredStorageTopic +
					"-0/00000000000000000000.log ]; then local=1; " +
					"else local=0; fi; printf '%s %s\\n' \"$remote\" \"$local\"",
			},
			tcexec.Multiplexed(),
		)
		if err == nil {
			data, readErr := io.ReadAll(io.LimitReader(output, 256))
			if readErr == nil {
				lastState = strings.TrimSpace(string(data))
				var remoteFiles, localBaseSegment int
				if exitCode == 0 {
					_, scanErr := fmt.Sscan(lastState, &remoteFiles, &localBaseSegment)
					if scanErr == nil && remoteFiles > 0 && localBaseSegment == 0 {
						return
					}
				}
			} else {
				lastErr = readErr
			}
		} else {
			lastErr = err
		}

		select {
		case <-waitCtx.Done():
			t.Fatalf(
				"wait for remote copy and local eviction: %v; state = %q; error = %v",
				context.Cause(waitCtx),
				lastState,
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func tieredStorageTopicStateMatches(state kafka.TopicState) bool {
	return state.Name == apacheKafkaTieredStorageTopic &&
		state.MinInSyncReplicas == 1 &&
		state.RetentionMilliseconds == -1 &&
		state.RetentionBytesPerPartition == -1 &&
		state.LocalRetentionMilliseconds == -2 &&
		state.LocalRetentionBytesPerPartition == 1 &&
		state.LocalRetentionVisible &&
		state.RemoteStorageEnabled &&
		state.RemoteStorageEnabledVisible &&
		!state.RemoteLogCopyDisabled &&
		state.RemoteLogCopyDisabledVisible &&
		len(state.Partitions) == 1 &&
		state.Partitions[0].BeginningOffset == 0 &&
		state.Partitions[0].EndOffset == 6
}
