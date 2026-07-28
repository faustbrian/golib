//go:build integration

package kafka_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"
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
)

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
	recoveredTransactionTopic := topic + "-transaction-recovered"
	createApacheKafkaTopic(t, ctx, brokers, topic, 3)
	createApacheKafkaTopic(t, ctx, brokers, fencingTransactionTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, warmTransactionTopic, 1)
	createApacheKafkaTopic(t, ctx, brokers, recoveredTransactionTopic, 1)

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
	proveProducerFencing(t, ctx, brokers, fencingTransactionTopic)

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       brokers,
		ClientID:      "golib-apache-failure-producer",
		AllowedTopics: []string{topic},
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

func createApacheKafkaTopic(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	topic string,
	partitions int32,
) {
	t.Helper()

	createIntegrationTopicWithReplication(
		t,
		ctx,
		brokers,
		topic,
		partitions,
		3,
		map[string]*string{
			"min.insync.replicas":            kadm.StringPtr("2"),
			"unclean.leader.election.enable": kadm.StringPtr("false"),
		},
	)
}
