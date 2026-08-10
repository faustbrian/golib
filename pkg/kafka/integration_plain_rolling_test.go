//go:build interoperability

package kafka_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/testcontainers/testcontainers-go"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl"
	franzplain "github.com/twmb/franz-go/pkg/sasl/plain"
)

const (
	plainRollingOldUsername = "plain-rotation-old"
	plainRollingNewUsername = "plain-rotation-new"
)

type plainRollingCredential struct {
	username string
	password string
}

type plainRollingKafkaCluster struct {
	*apacheKafkaCluster
	pki             secureKafkaPKI
	storePassword   string
	oldPassword     string
	currentPassword string
}

func TestApacheKafkaPlainRollingCredentialRotationCompatibility(t *testing.T) {
	runKafkaBrokerIntegration(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	oldPassword := randomSecureKafkaCredential(t)
	newPassword := randomSecureKafkaCredential(t)
	cluster := startApacheKafkaPlainRollingCluster(
		t,
		ctx,
		oldPassword,
		newPassword,
	)
	cluster.assertRuntimeVersion(t, ctx, "4.3.1")

	brokers := cluster.brokers(t, ctx)
	topic := fmt.Sprintf("golib-plain-rolling-%d", time.Now().UnixNano())
	oldMechanism := franzplain.Auth{
		User: plainRollingOldUsername,
		Pass: oldPassword,
	}.AsMechanism()
	createPlainRollingTopic(
		t,
		ctx,
		brokers,
		cluster.serverTLSConfig(),
		oldMechanism,
		topic,
	)

	var current atomic.Value
	current.Store(plainRollingCredential{
		username: plainRollingOldUsername,
		password: oldPassword,
	})
	var currentCalls atomic.Int64
	provider := kafka.UsernamePasswordProviderFunc(func(
		context.Context,
	) (kafka.UsernamePassword, error) {
		credential := current.Load().(plainRollingCredential)
		if credential.username == plainRollingNewUsername {
			currentCalls.Add(1)
		}

		return kafka.UsernamePassword{
			Username: credential.username,
			Password: []byte(credential.password),
		}, nil
	})
	security := kafka.ClientSecurity{
		TLS:               cluster.serverTLSConfig(),
		Authentication:    kafka.NewPlainAuthentication(provider),
		CredentialTimeout: time.Second,
	}
	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:            brokers,
		ClientID:           "golib-plain-rolling-producer",
		AllowedTopics:      []string{topic},
		DeliveryTimeout:    15 * time.Second,
		RequestTimeout:     3 * time.Second,
		DialTimeout:        5 * time.Second,
		ShutdownTimeout:    25 * time.Second,
		MaxBufferedRecords: 30,
		MaxBufferedBytes:   1 << 20,
		MaxBatchRecords:    30,
		MaxBatchBytes:      1 << 20,
		Security:           security,
	})
	if err != nil {
		t.Fatalf("construct rolling PLAIN producer: %v", err)
	}
	t.Cleanup(func() { _ = producer.Close() })

	expected := map[int32][]string{0: {}, 1: {}, 2: {}}
	publishPlainRollingPartitionBatch(t, ctx, producer, topic, "before", expected)
	current.Store(plainRollingCredential{
		username: plainRollingNewUsername,
		password: newPassword,
	})
	waitForPlainRollingProviderRefresh(
		t,
		ctx,
		producer,
		topic,
		&currentCalls,
		expected,
	)

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:               brokers,
		ClientID:              "golib-plain-rolling-inspector",
		Security:              security,
		DialTimeout:           5 * time.Second,
		RequestTimeout:        5 * time.Second,
		MaxMetadataBrokers:    3,
		MaxMetadataPartitions: 3,
	})
	if err != nil {
		t.Fatalf("construct rolling PLAIN inspector: %v", err)
	}
	t.Cleanup(func() { _ = inspector.Close() })
	waitForApacheTopicState(t, ctx, inspector, topic, plainRollingTopicHealthy)
	assertPlainRollingDistinctPartitionLeaders(t, ctx, inspector, topic)

	for nodeID := int32(1); nodeID <= 3; nodeID++ {
		cluster.retireOldCredentialOnNode(t, ctx, nodeID)
		cluster.stopNode(t, ctx, nodeID)
		publishPlainRollingPartitionBatch(
			t,
			ctx,
			producer,
			topic,
			fmt.Sprintf("node-%d-down", nodeID),
			expected,
		)
		cluster.startNode(t, ctx, nodeID)
		waitForApacheTopicState(t, ctx, inspector, topic, plainRollingTopicHealthy)
		assertPlainRollingNodeAcceptsCurrentCredential(
			t,
			ctx,
			cluster,
			cluster.brokerEndpoint(t, ctx, nodeID),
		)
		publishPlainRollingPartitionBatch(
			t,
			ctx,
			producer,
			topic,
			fmt.Sprintf("node-%d-recovered", nodeID),
			expected,
		)
	}

	if err := producer.Close(); err != nil {
		t.Fatalf("close rolling PLAIN producer: %v", err)
	}
	assertPlainRollingValues(t, ctx, cluster, topic, expected)
	for _, broker := range brokers {
		assertSecureKafkaHealthFailure(
			t,
			ctx,
			broker,
			cluster.security(plainRollingOldUsername, oldPassword),
			[]string{oldPassword},
		)
	}
}

func startApacheKafkaPlainRollingCluster(
	t *testing.T,
	ctx context.Context,
	oldPassword string,
	newPassword string,
) *plainRollingKafkaCluster {
	t.Helper()

	dockerNetwork := newApacheKafkaNetwork(t, ctx)
	cleanupApacheKafkaNetwork(t, dockerNetwork)
	cluster := &plainRollingKafkaCluster{
		apacheKafkaCluster: &apacheKafkaCluster{
			nodes: make([]apacheKafkaNode, 0, 3),
		},
		pki:             newSecureKafkaPKI(t, "127.0.0.1"),
		storePassword:   randomSecureKafkaCredential(t),
		oldPassword:     oldPassword,
		currentPassword: newPassword,
	}
	for nodeID := int32(1); nodeID <= 3; nodeID++ {
		alias := fmt.Sprintf("kafka-%d", nodeID)
		request := testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        apacheKafkaImage,
				User:         "0",
				ExposedPorts: []string{apacheKafkaClientPort},
				Networks:     []string{dockerNetwork.Name},
				NetworkAliases: map[string][]string{
					dockerNetwork.Name: {alias},
				},
				Entrypoint: []string{"sh"},
				Cmd: []string{
					"-c",
					"while [ ! -f " + apacheKafkaReadyFile +
						" ]; do sleep 0.05; done; exec /bin/bash " +
						apacheKafkaStartFile,
				},
			},
		}
		container, err := testcontainers.GenericContainer(ctx, request)
		if container != nil {
			cleanupKafkaContainer(t, container)
		}
		if err != nil {
			t.Fatalf("create rolling PLAIN Kafka node %d: %v", nodeID, err)
		}
		cluster.nodes = append(cluster.nodes, apacheKafkaNode{
			id: nodeID, alias: alias, container: container,
		})
	}

	for _, node := range cluster.nodes {
		if err := node.container.Start(ctx); err != nil {
			t.Fatalf("start rolling PLAIN Kafka node %d: %v", node.id, err)
		}
	}
	for _, node := range cluster.nodes {
		endpoint := waitForApacheKafkaPortEndpoint(
			t,
			ctx,
			node,
			apacheKafkaClientPort,
		)
		cluster.configureNode(t, ctx, node, endpoint)
	}
	for _, node := range cluster.nodes {
		cluster.waitForNode(t, ctx, node)
	}
	cluster.observeFailureState(t)

	return cluster
}

func (cluster *plainRollingKafkaCluster) configureNode(
	t *testing.T,
	ctx context.Context,
	node apacheKafkaNode,
	endpoint string,
) {
	t.Helper()

	files := []struct {
		path string
		data []byte
		mode int64
	}{
		{path: "/tmp/ca.pem", data: cluster.pki.caPEM, mode: 0o644},
		{path: "/tmp/server.pem", data: cluster.pki.serverPEM, mode: 0o644},
		{path: "/tmp/server-key.pem", data: cluster.pki.serverKeyPEM, mode: 0o600},
		{
			path: "/tmp/store-password",
			data: []byte(cluster.storePassword),
			mode: 0o600,
		},
		{
			path: "/tmp/store.properties",
			data: []byte("password=" + cluster.storePassword + "\n"),
			mode: 0o600,
		},
		{
			path: "/tmp/plain.properties",
			data: cluster.plainProperties(cluster.oldPassword),
			mode: 0o600,
		},
		{
			path: "/tmp/server.properties",
			data: []byte(plainRollingKafkaServerProperties(node.id, node.alias, endpoint)),
			mode: 0o644,
		},
		{
			path: apacheKafkaStartFile,
			data: []byte(plainRollingKafkaStartScript()),
			mode: 0o755,
		},
		{path: apacheKafkaReadyFile, data: []byte("ready\n"), mode: 0o644},
	}
	for _, file := range files {
		copySecureKafkaFile(t, ctx, node.container, file.path, file.data, file.mode)
	}
}

func (cluster *plainRollingKafkaCluster) plainProperties(oldPassword string) []byte {
	return []byte(
		"old-password=" + oldPassword + "\n" +
			"new-password=" + cluster.currentPassword + "\n",
	)
}

func (cluster *plainRollingKafkaCluster) retireOldCredentialOnNode(
	t *testing.T,
	ctx context.Context,
	nodeID int32,
) {
	t.Helper()

	node := cluster.node(t, nodeID)
	copySecureKafkaFile(
		t,
		ctx,
		node.container,
		"/tmp/plain.properties",
		cluster.plainProperties(randomSecureKafkaCredential(t)),
		0o600,
	)
	exitCode, _, err := node.container.Exec(ctx, []string{
		"sh",
		"-c",
		"chown 1000:1000 /tmp/plain.properties && chmod 0600 /tmp/plain.properties",
	})
	if err != nil {
		t.Fatalf("set rolling PLAIN credential ownership on node %d: %v", nodeID, err)
	}
	if exitCode != 0 {
		t.Fatalf("set rolling PLAIN credential ownership on node %d: exit %d", nodeID, exitCode)
	}
}

func (cluster *plainRollingKafkaCluster) brokerEndpoint(
	t *testing.T,
	ctx context.Context,
	nodeID int32,
) string {
	t.Helper()

	node := cluster.node(t, nodeID)
	endpoint, err := node.container.PortEndpoint(ctx, apacheKafkaClientPort, "")
	if err != nil {
		t.Fatalf("resolve rolling PLAIN Kafka node %d endpoint: %v", nodeID, err)
	}

	return endpoint
}

func (cluster *plainRollingKafkaCluster) serverTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    cluster.pki.roots.Clone(),
	}
}

func (cluster *plainRollingKafkaCluster) security(
	username string,
	password string,
) kafka.ClientSecurity {
	return kafka.ClientSecurity{
		TLS: cluster.serverTLSConfig(),
		Authentication: kafka.NewPlainAuthentication(
			kafka.UsernamePasswordProviderFunc(func(
				context.Context,
			) (kafka.UsernamePassword, error) {
				return kafka.UsernamePassword{
					Username: username,
					Password: []byte(password),
				}, nil
			}),
		),
		CredentialTimeout: time.Second,
	}
}

func plainRollingKafkaServerProperties(
	nodeID int32,
	alias string,
	endpoint string,
) string {
	return fmt.Sprintf(
		"process.roles=broker,controller\n"+
			"node.id=%d\n"+
			"controller.quorum.voters=1@kafka-1:9093,2@kafka-2:9093,3@kafka-3:9093\n"+
			"controller.listener.names=CONTROLLER\n"+
			"listeners=SASL_SSL://:9092,INTERNAL://:19092,CONTROLLER://:9093\n"+
			"advertised.listeners=SASL_SSL://%s,INTERNAL://%s:19092\n"+
			"listener.security.protocol.map=SASL_SSL:SASL_SSL,"+
			"INTERNAL:PLAINTEXT,CONTROLLER:PLAINTEXT\n"+
			"inter.broker.listener.name=INTERNAL\n"+
			"log.dirs=/tmp/kraft-combined-logs\n"+
			"num.partitions=3\n"+
			"offsets.topic.replication.factor=3\n"+
			"transaction.state.log.replication.factor=3\n"+
			"transaction.state.log.min.isr=2\n"+
			"share.coordinator.state.topic.replication.factor=3\n"+
			"share.coordinator.state.topic.min.isr=2\n"+
			"default.replication.factor=3\n"+
			"min.insync.replicas=2\n"+
			"group.initial.rebalance.delay.ms=0\n"+
			"auto.create.topics.enable=false\n"+
			"config.providers=file\n"+
			"config.providers.file.class="+
			"org.apache.kafka.common.config.provider.FileConfigProvider\n"+
			"ssl.keystore.location=/tmp/server.p12\n"+
			"ssl.keystore.type=PKCS12\n"+
			"ssl.keystore.password=${file:/tmp/store.properties:password}\n"+
			"ssl.key.password=${file:/tmp/store.properties:password}\n"+
			"ssl.enabled.protocols=TLSv1.2,TLSv1.3\n"+
			"ssl.client.auth=none\n"+
			"sasl.enabled.mechanisms=PLAIN\n"+
			"listener.name.sasl_ssl.plain.connections.max.reauth.ms=2000\n"+
			"listener.name.sasl_ssl.plain.sasl.jaas.config="+
			"org.apache.kafka.common.security.plain.PlainLoginModule required "+
			"user_%s=\"${file:/tmp/plain.properties:old-password}\" "+
			"user_%s=\"${file:/tmp/plain.properties:new-password}\";\n",
		nodeID,
		endpoint,
		alias,
		plainRollingOldUsername,
		plainRollingNewUsername,
	)
}

func plainRollingKafkaStartScript() string {
	return "#!/bin/bash\n" +
		"set -euo pipefail\n" +
		"if [ \"$(id -u)\" -eq 0 ]; then\n" +
		"  for secret in /tmp/server-key.pem /tmp/store-password " +
		"/tmp/store.properties /tmp/plain.properties; do\n" +
		"    chown 1000:1000 \"$secret\"\n" +
		"    chmod 0600 \"$secret\"\n" +
		"  done\n" +
		"  exec su appuser -s /bin/bash -c " +
		"'exec /bin/bash " + apacheKafkaStartFile + "'\n" +
		"fi\n" +
		"umask 077\n" +
		"openssl pkcs12 -export -name broker " +
		"-inkey /tmp/server-key.pem -in /tmp/server.pem " +
		"-certfile /tmp/ca.pem -out /tmp/server.p12 " +
		"-passout file:/tmp/store-password >/dev/null 2>&1\n" +
		"/opt/kafka/bin/kafka-storage.sh format --ignore-formatted " +
		"--cluster-id " + apacheKafkaClusterID + " " +
		"--config /tmp/server.properties >/dev/null\n" +
		"shutdown() {\n" +
		"  if [ -s " + apacheKafkaPIDFile + " ]; then\n" +
		"    pid=\"$(cat " + apacheKafkaPIDFile + ")\"\n" +
		"    kill -TERM \"$pid\" 2>/dev/null || true\n" +
		"    wait \"$pid\" 2>/dev/null || true\n" +
		"  fi\n" +
		"  exit 0\n" +
		"}\n" +
		"trap shutdown TERM INT\n" +
		"set +e\n" +
		"while true; do\n" +
		"  while [ -f " + apacheKafkaStopFile + " ]; do sleep 0.05; done\n" +
		"  /opt/kafka/bin/kafka-server-start.sh /tmp/server.properties &\n" +
		"  pid=\"$!\"\n" +
		"  printf '%s\\n' \"$pid\" > " + apacheKafkaPIDFile + "\n" +
		"  wait \"$pid\"\n" +
		"  rm -f " + apacheKafkaPIDFile + "\n" +
		"  touch " + apacheKafkaStopFile + "\n" +
		"done\n"
}

func createPlainRollingTopic(
	t *testing.T,
	ctx context.Context,
	brokers []string,
	tlsConfig *tls.Config,
	mechanism sasl.Mechanism,
	topic string,
) {
	t.Helper()

	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ClientID("golib-plain-rolling-admin"),
		kgo.DialTLSConfig(tlsConfig),
		kgo.SASL(mechanism),
	)
	if err != nil {
		t.Fatalf("construct rolling PLAIN administrator: %v", err)
	}
	defer client.Close()
	responses, err := kadm.NewClient(client).CreateTopics(ctx, 3, 3, nil, topic)
	if err != nil {
		t.Fatalf("create rolling PLAIN topic: %v", err)
	}
	response, exists := responses[topic]
	if !exists || response.Err != nil {
		t.Fatalf("create rolling PLAIN topic %q: %#v", topic, response)
	}
}

func waitForPlainRollingProviderRefresh(
	t *testing.T,
	ctx context.Context,
	producer *kafka.Producer,
	topic string,
	calls *atomic.Int64,
	expected map[int32][]string,
) {
	t.Helper()

	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for attempt := 1; ; attempt++ {
		publishPlainRollingPartitionBatch(
			t,
			waitCtx,
			producer,
			topic,
			fmt.Sprintf("current-credential-%d", attempt),
			expected,
		)
		if calls.Load() >= 3 {
			return
		}
		select {
		case <-waitCtx.Done():
			t.Fatalf("rolling PLAIN provider calls = %d, want at least 3: %v", calls.Load(), context.Cause(waitCtx))
		case <-ticker.C:
		}
	}
}

func publishPlainRollingPartitionBatch(
	t *testing.T,
	ctx context.Context,
	producer *kafka.Producer,
	topic string,
	label string,
	expected map[int32][]string,
) {
	t.Helper()

	for partition := int32(0); partition < 3; partition++ {
		value := fmt.Sprintf("%s-partition-%d", label, partition)
		result := producer.PublishRecord(ctx, kafka.ProducerRecord{
			Topic:     topic,
			Partition: kafka.ExplicitPartition(partition),
			Key:       []byte(value),
			Value:     []byte(value),
		})
		if result.Err != nil || result.Partition != partition {
			t.Fatalf("rolling PLAIN delivery to partition %d = %#v", partition, result)
		}
		expected[partition] = append(expected[partition], value)
	}
}

func plainRollingTopicHealthy(state kafka.TopicState) bool {
	if len(state.Partitions) != 3 {
		return false
	}
	for _, partition := range state.Partitions {
		if partition.ReplicationFactor != 3 ||
			partition.InSyncReplicas != 3 ||
			len(partition.OfflineReplicaIDs) != 0 {
			return false
		}
	}

	return true
}

func assertPlainRollingDistinctPartitionLeaders(
	t *testing.T,
	ctx context.Context,
	inspector *kafka.Inspector,
	topic string,
) {
	t.Helper()

	states, err := inspector.Topics(ctx, topic)
	if err != nil || len(states) != 1 || len(states[0].Partitions) != 3 {
		t.Fatalf("inspect rolling PLAIN partition leaders: %#v, %v", states, err)
	}
	leaders := make(map[int32]struct{}, 3)
	for _, partition := range states[0].Partitions {
		leaders[partition.Leader] = struct{}{}
	}
	if len(leaders) != 3 {
		t.Fatalf("rolling PLAIN partition leaders = %v, want three brokers", leaders)
	}
}

func assertPlainRollingNodeAcceptsCurrentCredential(
	t *testing.T,
	ctx context.Context,
	cluster *plainRollingKafkaCluster,
	broker string,
) {
	t.Helper()

	waitCtx, cancelWait := context.WithTimeout(ctx, 30*time.Second)
	defer cancelWait()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		client, err := kgo.NewClient(
			kgo.SeedBrokers(broker),
			kgo.ClientID("golib-plain-rolling-node-check"),
			kgo.DialTimeout(time.Second),
			kgo.RequestTimeoutOverhead(time.Second),
			kgo.DialTLSConfig(cluster.serverTLSConfig()),
			kgo.SASL(franzplain.Auth{
				User: plainRollingNewUsername,
				Pass: cluster.currentPassword,
			}.AsMechanism()),
		)
		if err != nil {
			t.Fatalf("construct rolling PLAIN node check: %v", err)
		}
		pingCtx, cancelPing := context.WithTimeout(waitCtx, 2*time.Second)
		lastErr = client.Ping(pingCtx)
		cancelPing()
		client.Close()
		if lastErr == nil {
			return
		}

		select {
		case <-waitCtx.Done():
			t.Fatalf(
				"authenticate to recovered rolling PLAIN node: %v; last error = %v",
				context.Cause(waitCtx),
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func assertPlainRollingValues(
	t *testing.T,
	ctx context.Context,
	cluster *plainRollingKafkaCluster,
	topic string,
	expected map[int32][]string,
) {
	t.Helper()

	partitions := make(map[int32]kgo.Offset, len(expected))
	total := 0
	for partition, values := range expected {
		partitions[partition] = kgo.NewOffset().AtStart()
		total += len(values)
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cluster.brokers(t, ctx)...),
		kgo.ClientID("golib-plain-rolling-reader"),
		kgo.DialTLSConfig(cluster.serverTLSConfig()),
		kgo.SASL(franzplain.Auth{
			User: plainRollingNewUsername,
			Pass: cluster.currentPassword,
		}.AsMechanism()),
		kgo.ConsumePartitions(map[string]map[int32]kgo.Offset{topic: partitions}),
		kgo.FetchMaxWait(100*time.Millisecond),
	)
	if err != nil {
		t.Fatalf("construct rolling PLAIN reader: %v", err)
	}
	defer client.Close()
	actual := map[int32][]string{0: {}, 1: {}, 2: {}}
	for read := 0; read < total; {
		fetches := client.PollRecords(ctx, total-read)
		if err := fetches.Err(); err != nil {
			t.Fatalf("read rolling PLAIN records: %v", err)
		}
		for _, record := range fetches.Records() {
			actual[record.Partition] = append(actual[record.Partition], string(record.Value))
			read++
		}
	}
	for partition, want := range expected {
		got := actual[partition]
		if len(got) != len(want) {
			t.Fatalf("rolling PLAIN partition %d values = %d, want %d", partition, len(got), len(want))
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf(
					"rolling PLAIN partition %d value %d = %q, want %q",
					partition,
					index,
					got[index],
					want[index],
				)
			}
		}
	}
}
