//go:build integration

package gokafka_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	gokafka "github.com/faustbrian/golib/pkg/event-sourcing/adapters/gokafka"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/testcontainers/testcontainers-go"
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	restartKafkaImage = "apache/kafka:4.3.1@" +
		"sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837"
	restartKafkaPort      = "9092/tcp"
	restartKafkaPIDFile   = "/tmp/gokafka-restart.pid"
	restartKafkaStopFile  = "/tmp/gokafka-restart.stop"
	restartKafkaRunFile   = "/tmp/gokafka-restart.sh"
	restartKafkaReadyFile = "/tmp/gokafka-restart.ready"
)

func TestKafkaBrokerRestartPreservesAmbiguousAtLeastOnceDelivery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	broker := startRestartableKafkaBroker(t, ctx)
	brokers := []string{broker.endpoint}
	topic := fmt.Sprintf("event-sourcing-restart-%d", time.Now().UnixNano())
	createIntegrationTopic(t, ctx, brokers, topic)
	codec := integrationCodec(t, topic)
	producer := integrationProducer(t, brokers, topic)
	acknowledgementLost := errors.New("injected acknowledgement loss")
	ambiguous := &restartAfterAcknowledgementPublisher{
		publisher: producer,
		broker:    broker,
		failure:   acknowledgementLost,
	}
	dispatcher, err := gokafka.NewDispatcher(ambiguous, codec)
	if err != nil {
		t.Fatalf("construct restart dispatcher: %v", err)
	}
	deliveries := integrationDeliveries(t)[:2]
	if err := dispatcher.Dispatch(ctx, deliveries[:1]); !errors.Is(
		err,
		acknowledgementLost,
	) {
		t.Fatalf("ambiguous dispatch error = %v", err)
	}
	if err := broker.waitUnavailable(ctx); err != nil {
		t.Fatalf("prove Kafka broker stopped: %v", err)
	}
	if err := broker.start(ctx); err != nil {
		t.Fatalf("restart Kafka broker: %v", err)
	}
	if err := dispatcher.Dispatch(ctx, deliveries[:1]); err != nil {
		t.Fatalf("retry ambiguous dispatch after restart: %v", err)
	}
	if err := dispatcher.Dispatch(ctx, deliveries[1:]); err != nil {
		t.Fatalf("dispatch following delivery after restart: %v", err)
	}

	groupID := topic + ".group"
	effects := make(map[string]struct{}, 2)
	attempts := make([]string, 0, 4)
	consumer := newRestartIntegrationConsumer(t, brokers, topic, groupID)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(
			context.Background(),
			30*time.Second,
		)
		defer cleanupCancel()
		if err := broker.start(cleanupCtx); err != nil {
			t.Errorf("restore Kafka broker for consumer cleanup: %v", err)

			return
		}
		if err := consumer.Shutdown(cleanupCtx); err != nil {
			t.Errorf("shutdown restart consumer: %v", err)
		}
	})
	firstHandler, err := gokafka.NewRecordHandler(
		codec,
		gokafka.DeliveryConsumerFunc(func(
			_ context.Context,
			delivery eventsourcing.Delivery,
		) error {
			identity := delivery.Message().ID().String()
			attempts = append(attempts, identity)
			effects[identity] = struct{}{}

			return broker.stop(ctx)
		}),
	)
	if err != nil {
		t.Fatalf("construct pre-restart handler: %v", err)
	}
	result, err := consumer.RunOnce(ctx, firstHandler)
	if err == nil || result != (kafka.PollResult{Polled: 1, Processed: 1}) {
		t.Fatalf("ambiguous offset result/error = %#v/%v", result, err)
	}
	if err := broker.waitUnavailable(ctx); err != nil {
		t.Fatalf("prove Kafka broker stopped before commit: %v", err)
	}
	if err := broker.start(ctx); err != nil {
		t.Fatalf("restart Kafka broker after offset ambiguity: %v", err)
	}
	if err := consumer.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown pre-restart consumer after broker recovery: %v", err)
	}

	replacement, err := gokafka.NewRecordHandler(
		codec,
		gokafka.DeliveryConsumerFunc(func(
			_ context.Context,
			delivery eventsourcing.Delivery,
		) error {
			identity := delivery.Message().ID().String()
			attempts = append(attempts, identity)
			effects[identity] = struct{}{}

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("construct post-restart handler: %v", err)
	}
	runIntegrationConsumer(t, ctx, brokers, topic, groupID, replacement, 3)
	if want := []string{"message-1", "message-1", "message-1", "message-2"}; !slices.Equal(attempts, want) {
		t.Fatalf("restart delivery attempts = %q, want %q", attempts, want)
	}
	if len(effects) != 2 {
		t.Fatalf("idempotent effects after restart = %d, want 2", len(effects))
	}
	assertGroupCommitted(t, ctx, brokers, topic, groupID, 3)
}

type restartAfterAcknowledgementPublisher struct {
	publisher gokafka.Publisher
	broker    *restartableKafkaBroker
	failure   error
	failed    bool
}

func (publisher *restartAfterAcknowledgementPublisher) Publish(
	ctx context.Context,
	message kafka.Message,
) error {
	if err := publisher.publisher.Publish(ctx, message); err != nil {
		return err
	}
	if publisher.failed {
		return nil
	}
	publisher.failed = true
	if err := publisher.broker.stop(ctx); err != nil {
		return fmt.Errorf("stop broker after acknowledged append: %w", err)
	}

	return publisher.failure
}

type restartableKafkaBroker struct {
	container testcontainers.Container
	endpoint  string
}

func startRestartableKafkaBroker(
	t *testing.T,
	ctx context.Context,
) *restartableKafkaBroker {
	t.Helper()

	request := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        restartKafkaImage,
			ExposedPorts: []string{restartKafkaPort},
			Env: map[string]string{
				"CLUSTER_ID":                                     "4L6g3nShT-eMCtK--X86sw",
				"KAFKA_NODE_ID":                                  "1",
				"KAFKA_PROCESS_ROLES":                            "broker,controller",
				"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP":           "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT",
				"KAFKA_CONTROLLER_QUORUM_VOTERS":                 "1@localhost:9093",
				"KAFKA_LISTENERS":                                "PLAINTEXT://:9092,CONTROLLER://:9093",
				"KAFKA_INTER_BROKER_LISTENER_NAME":               "PLAINTEXT",
				"KAFKA_CONTROLLER_LISTENER_NAMES":                "CONTROLLER",
				"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":         "1",
				"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":            "1",
				"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1",
				"KAFKA_DEFAULT_REPLICATION_FACTOR":               "1",
				"KAFKA_MIN_INSYNC_REPLICAS":                      "1",
				"KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS":         "0",
				"KAFKA_AUTO_CREATE_TOPICS_ENABLE":                "false",
				"KAFKA_LOG_DIRS":                                 "/tmp/kraft-combined-logs",
			},
			Entrypoint: []string{"sh"},
			Cmd: []string{
				"-c",
				"while [ ! -f " + restartKafkaReadyFile +
					" ]; do sleep 0.05; done; exec /bin/bash " +
					restartKafkaRunFile,
			},
		},
	}
	container, err := testcontainers.GenericContainer(ctx, request)
	if container != nil {
		cleanupKafkaContainer(t, container)
	}
	if err != nil {
		t.Fatalf("create restartable Kafka broker: %v", err)
	}
	if err := container.Start(ctx); err != nil {
		t.Fatalf("start restartable Kafka container: %v", err)
	}
	endpoint, err := container.PortEndpoint(ctx, restartKafkaPort, "")
	if err != nil {
		t.Fatalf("resolve restartable Kafka endpoint: %v", err)
	}
	script := restartKafkaRunLoopScript(endpoint)
	if err := container.CopyToContainer(ctx, []byte(script), restartKafkaRunFile, 0o755); err != nil {
		t.Fatalf("configure restartable Kafka broker: %v", err)
	}
	if err := container.CopyToContainer(ctx, []byte("ready\n"), restartKafkaReadyFile, 0o644); err != nil {
		t.Fatalf("release restartable Kafka broker: %v", err)
	}
	broker := &restartableKafkaBroker{container: container, endpoint: endpoint}
	if err := broker.waitAvailable(ctx); err != nil {
		t.Fatalf("wait for restartable Kafka broker: %v", err)
	}

	return broker
}

func restartKafkaRunLoopScript(endpoint string) string {
	return fmt.Sprintf(`#!/bin/bash
export KAFKA_ADVERTISED_LISTENERS='PLAINTEXT://%s'
shutdown() {
  if [ -s %s ]; then
    pid="$(cat %s)"
    kill -TERM "$pid" 2>/dev/null
    wait "$pid" 2>/dev/null
  fi
  exit 0
}
trap shutdown TERM INT
while true; do
  while [ -f %s ]; do sleep 0.05; done
  /etc/kafka/docker/run &
  pid="$!"
  printf '%%s\n' "$pid" > %s
  wait "$pid"
  rm -f %s
  touch %s
done
`, endpoint, restartKafkaPIDFile, restartKafkaPIDFile, restartKafkaStopFile,
		restartKafkaPIDFile, restartKafkaPIDFile, restartKafkaStopFile)
}

func (broker *restartableKafkaBroker) stop(ctx context.Context) error {
	stopScript := fmt.Sprintf(`
touch %[1]s
if [ ! -s %[2]s ]; then exit 0; fi
pid="$(cat %[2]s)"
kill -TERM "$pid" 2>/dev/null || :
remaining=150
while kill -0 "$pid" 2>/dev/null && [ "$remaining" -gt 0 ]; do
  sleep 0.1
  remaining=$((remaining - 1))
done
if kill -0 "$pid" 2>/dev/null; then
  kill -KILL "$pid" 2>/dev/null || :
fi
while kill -0 "$pid" 2>/dev/null; do sleep 0.05; done
rm -f %[2]s
`, restartKafkaStopFile, restartKafkaPIDFile)
	exitCode, _, err := broker.container.Exec(ctx, []string{
		"sh",
		"-c",
		stopScript,
	})
	if err != nil {
		return fmt.Errorf("execute Kafka stop: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("execute Kafka stop: exit %d", exitCode)
	}

	return nil
}

func (broker *restartableKafkaBroker) start(ctx context.Context) error {
	exitCode, _, err := broker.container.Exec(
		ctx,
		[]string{"rm", "-f", restartKafkaStopFile},
	)
	if err != nil {
		return fmt.Errorf("release Kafka restart: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("release Kafka restart: exit %d", exitCode)
	}

	return broker.waitAvailable(ctx)
}

func (broker *restartableKafkaBroker) waitAvailable(ctx context.Context) error {
	return broker.waitForPingState(ctx, true)
}

func (broker *restartableKafkaBroker) waitUnavailable(ctx context.Context) error {
	return broker.waitForPingState(ctx, false)
}

func (broker *restartableKafkaBroker) waitForPingState(
	ctx context.Context,
	wantAvailable bool,
) error {
	waitCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		client, err := kgo.NewClient(
			kgo.SeedBrokers(broker.endpoint),
			kgo.DialTimeout(500*time.Millisecond),
			kgo.RequestTimeoutOverhead(500*time.Millisecond),
		)
		if err != nil {
			return fmt.Errorf("construct Kafka readiness client: %w", err)
		}
		pingCtx, pingCancel := context.WithTimeout(waitCtx, time.Second)
		pingErr := client.Ping(pingCtx)
		pingCancel()
		client.Close()
		if (pingErr == nil) == wantAvailable {
			return nil
		}
		lastErr = pingErr

		select {
		case <-waitCtx.Done():
			state := "available"
			if !wantAvailable {
				state = "unavailable"
			}

			return fmt.Errorf("wait for Kafka to become %s: %w; last ping: %v",
				state, context.Cause(waitCtx), lastErr)
		case <-ticker.C:
		}
	}
}

func newRestartIntegrationConsumer(
	t *testing.T,
	brokers []string,
	topic string,
	groupID string,
) *kafka.Consumer {
	t.Helper()

	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:           brokers,
		ClientID:          groupID + ".restart",
		GroupID:           groupID,
		Topics:            []string{topic},
		ResetOffset:       kafka.OffsetEarliest,
		MaxPollRecords:    1,
		FetchMaxWait:      100 * time.Millisecond,
		SessionTimeout:    10 * time.Second,
		RebalanceTimeout:  30 * time.Second,
		HeartbeatInterval: time.Second,
		HandlerTimeout:    20 * time.Second,
		CommitTimeout:     2 * time.Second,
		DialTimeout:       10 * time.Second,
		Security:          kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct restart consumer: %v", err)
	}

	return consumer
}
