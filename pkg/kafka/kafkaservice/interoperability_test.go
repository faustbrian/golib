//go:build interoperability

package kafkaservice_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/kafka/kafkaservice"
)

const serviceKafkaImage = "apache/kafka:4.3.1@" +
	"sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837"

func TestLifecycleAdapterDrainsRootKafkaResources(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	broker, container := startServiceKafka(t, ctx)
	topic := fmt.Sprintf("golib-kafkaservice-%d", time.Now().UnixNano())
	createServiceTopic(t, ctx, container, topic)

	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		t.Fatalf("construct correlation factory: %v", err)
	}
	producerResource, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       []string{broker},
		ClientID:      "golib-kafkaservice-producer",
		AllowedTopics: []string{topic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct producer: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := producerResource.Close(); closeErr != nil {
			t.Errorf("close producer: %v", closeErr)
		}
	})
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*kafka.Producer]{
			Name:        "golib-kafkaservice-producer",
			Resource:    producerResource,
			Correlation: factory,
			Startup: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Health(ctx)
			},
			Readiness: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Health(ctx)
			},
			Publish: func(
				ctx context.Context,
				resource *kafka.Producer,
				record kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				result := resource.PublishRecord(ctx, record)

				return result, result.Err
			},
			Shutdown: func(ctx context.Context, resource *kafka.Producer) error {
				return resource.Shutdown(ctx)
			},
		},
	)
	if err != nil {
		t.Fatalf("construct producer adapter: %v", err)
	}
	producerComponent := producer.Component()
	if err = producerComponent.Start(ctx); err != nil {
		t.Fatalf("start producer adapter: %v", err)
	}
	readiness, ok := producer.Readiness()
	if !ok {
		t.Fatal("producer readiness is absent")
	}
	if err = readiness.Run(ctx); err != nil {
		t.Fatalf("check producer readiness: %v", err)
	}

	group := "golib-kafkaservice-consumer-v1"
	consumerResource, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:        []string{broker},
		ClientID:       "golib-kafkaservice-consumer",
		GroupID:        group,
		Topics:         []string{topic},
		ResetOffset:    kafka.OffsetEarliest,
		MaxPollRecords: 1,
		Security:       kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct consumer: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := consumerResource.Close(); closeErr != nil {
			t.Errorf("close consumer: %v", closeErr)
		}
	})
	firstHandled := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseSecond := make(chan struct{})
	var firstOnce sync.Once
	var secondOnce sync.Once
	var secondFinished atomic.Bool
	var attempts atomic.Int32
	consumer, err := kafkaservice.NewConsumer(
		kafkaservice.ConsumerOptions[*kafka.Consumer]{
			Name:        "golib-kafkaservice-consumer",
			Resource:    consumerResource,
			Correlation: factory,
			Handler: kafka.HandlerFunc(func(
				_ context.Context,
				record kafka.ConsumedRecord,
			) error {
				attempt := attempts.Add(1)
				if record.Topic != topic || int64(attempt-1) != record.Offset {
					return errors.New("unexpected consumed record metadata")
				}
				if record.Offset == 0 {
					firstOnce.Do(func() { close(firstHandled) })

					return nil
				}
				secondOnce.Do(func() { close(secondStarted) })
				select {
				case <-releaseSecond:
					secondFinished.Store(true)

					return nil
				case <-ctx.Done():
					return context.Cause(ctx)
				}
			}),
			Run: func(
				ctx context.Context,
				resource *kafka.Consumer,
				handler kafka.Handler,
			) error {
				return resource.Run(ctx, handler)
			},
			Shutdown: func(ctx context.Context, resource *kafka.Consumer) error {
				if !secondFinished.Load() {
					return errors.New("consumer shutdown preceded admitted handler")
				}

				return resource.Shutdown(ctx)
			},
		},
	)
	if err != nil {
		t.Fatalf("construct consumer adapter: %v", err)
	}
	plan := consumer.Plan()
	if err = plan.Components[0].Start(ctx); err != nil {
		t.Fatalf("start consumer adapter: %v", err)
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	runResult := make(chan error, 1)
	go func() {
		runResult <- plan.Tasks[0].Run(runCtx)
	}()

	parent, err := factory.Start()
	if err != nil {
		t.Fatalf("create parent correlation: %v", err)
	}
	_, firstDelivery, err := producer.Publish(
		correlation.WithValues(ctx, parent),
		kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte("lifecycle"),
			Value: []byte("drain"),
		},
	)
	if err != nil {
		t.Fatalf("publish through producer adapter: %v", err)
	}
	if firstDelivery.Topic != topic || firstDelivery.Partition != 0 ||
		firstDelivery.Offset != 0 {
		t.Fatalf("first delivery metadata = %#v", firstDelivery)
	}

	select {
	case <-firstHandled:
	case runErr := <-runResult:
		t.Fatalf("consumer task exited before first handling: %v", runErr)
	case <-ctx.Done():
		t.Fatalf("wait for first handler: %v", context.Cause(ctx))
	}
	waitForServiceCommit(t, ctx, broker, group, topic, 1, 0)
	_, secondDelivery, err := producer.Publish(
		correlation.WithValues(ctx, parent),
		kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte("lifecycle"),
			Value: []byte("redeliver"),
		},
	)
	if err != nil {
		t.Fatalf("publish in-flight cancellation record: %v", err)
	}
	if secondDelivery.Topic != topic || secondDelivery.Partition != 0 ||
		secondDelivery.Offset != 1 {
		t.Fatalf("second delivery metadata = %#v", secondDelivery)
	}
	select {
	case <-secondStarted:
	case runErr := <-runResult:
		t.Fatalf("consumer task exited before second handling: %v", runErr)
	case <-ctx.Done():
		t.Fatalf("wait for second handler: %v", context.Cause(ctx))
	}
	cancelRun()
	stopInvoked := make(chan struct{})
	stopResult := make(chan error, 1)
	go func() {
		close(stopInvoked)
		stopResult <- plan.Components[0].Stop(ctx)
	}()
	<-stopInvoked
	close(releaseSecond)
	if err = <-stopResult; err != nil {
		t.Fatalf("stop consumer adapter: %v", err)
	}
	if err = <-runResult; err != nil {
		t.Fatalf("run consumer adapter during cancellation: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("handler attempts = %d, want 2", attempts.Load())
	}
	waitForServiceCommit(t, ctx, broker, group, topic, 1, 1)

	replacement, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:        []string{broker},
		ClientID:       "golib-kafkaservice-replacement",
		GroupID:        group,
		Topics:         []string{topic},
		ResetOffset:    kafka.OffsetEarliest,
		MaxPollRecords: 1,
		Security:       kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct replacement consumer: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := replacement.Close(); closeErr != nil {
			t.Errorf("close replacement consumer: %v", closeErr)
		}
	})
	replacementResult, err := replacement.RunOnce(
		ctx,
		kafka.HandlerFunc(func(_ context.Context, record kafka.ConsumedRecord) error {
			if record.Topic != topic || record.Partition != 0 || record.Offset != 1 {
				return errors.New("replacement did not receive unsettled record")
			}

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("consume unsettled record with replacement: %v", err)
	}
	if replacementResult.Processed != 1 || replacementResult.Committed != 1 {
		t.Fatalf("replacement result = %#v", replacementResult)
	}
	if err = replacement.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown replacement consumer: %v", err)
	}
	waitForServiceCommit(t, ctx, broker, group, topic, 2, 0)

	if err = producerComponent.Stop(ctx); err != nil {
		t.Fatalf("stop producer adapter: %v", err)
	}
	if _, _, publishErr := producer.Publish(
		correlation.WithValues(ctx, parent),
		kafka.ProducerRecord{Topic: topic, Key: []byte("closed")},
	); !errors.Is(publishErr, kafkaservice.ErrUnavailable) {
		t.Fatalf("publish after stop error = %v", publishErr)
	}
}

func waitForServiceCommit(
	t *testing.T,
	ctx context.Context,
	broker string,
	group string,
	topic string,
	offset int64,
	lag int64,
) {
	t.Helper()

	inspector, err := kafka.NewInspector(kafka.InspectorConfig{
		Brokers:  []string{broker},
		ClientID: "golib-kafkaservice-inspector",
		Security: kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct commit inspector: %v", err)
	}
	defer inspector.Close()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var last []kafka.ConsumerGroupState
	var lastErr error
	for {
		last, lastErr = inspector.ConsumerGroupLag(ctx, group)
		if lastErr == nil && len(last) == 1 && len(last[0].Partitions) == 1 {
			partition := last[0].Partitions[0]
			if partition.Topic == topic && partition.Partition == 0 &&
				partition.CommittedOffset == offset && partition.Lag == lag {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf(
				"wait for committed source offset: %v; state = %#v; error = %v",
				context.Cause(ctx),
				last,
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func startServiceKafka(t *testing.T, ctx context.Context) (string, string) {
	t.Helper()

	port := reserveServicePort(t)
	container := fmt.Sprintf("golib-kafkaservice-%d-%d", os.Getpid(), time.Now().UnixNano())
	args := []string{
		"run", "--detach", "--rm", "--name", container,
		"--publish", "127.0.0.1:" + strconv.Itoa(port) + ":9092",
		"--env", "KAFKA_NODE_ID=1",
		"--env", "KAFKA_PROCESS_ROLES=broker,controller",
		"--env", "KAFKA_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT",
		"--env", "KAFKA_CONTROLLER_QUORUM_VOTERS=1@localhost:9093",
		"--env", "KAFKA_LISTENERS=INTERNAL://:19092,CONTROLLER://:9093,EXTERNAL://:9092",
		"--env", "KAFKA_ADVERTISED_LISTENERS=INTERNAL://localhost:19092,EXTERNAL://127.0.0.1:" + strconv.Itoa(port),
		"--env", "KAFKA_INTER_BROKER_LISTENER_NAME=INTERNAL",
		"--env", "KAFKA_CONTROLLER_LISTENER_NAMES=CONTROLLER",
		"--env", "CLUSTER_ID=4L6g3nShT-eMCtK--X86sw",
		"--env", "KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR=1",
		"--env", "KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR=1",
		"--env", "KAFKA_TRANSACTION_STATE_LOG_MIN_ISR=1",
		"--env", "KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS=0",
		"--env", "KAFKA_AUTO_CREATE_TOPICS_ENABLE=false",
		serviceKafkaImage,
	}
	if output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("start pinned Apache Kafka container: %v: %s", err, boundedServiceOutput(output))
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if output, err := exec.CommandContext(
			cleanupCtx,
			"docker",
			"rm",
			"--force",
			container,
		).CombinedOutput(); err != nil {
			t.Errorf("remove Apache Kafka container: %v: %s", err, boundedServiceOutput(output))
		}
	})

	waitForServiceKafka(t, ctx, container)
	version := runServiceDockerExec(
		t,
		ctx,
		container,
		"/opt/kafka/bin/kafka-broker-api-versions.sh",
		"--version",
	)
	fields := strings.Fields(version)
	if len(fields) == 0 || fields[0] != "4.3.1" {
		t.Fatalf("Apache Kafka runtime version = %q, want 4.3.1", version)
	}

	return "127.0.0.1:" + strconv.Itoa(port), container
}

func waitForServiceKafka(t *testing.T, ctx context.Context, container string) {
	t.Helper()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var lastOutput string
	for {
		output, err := exec.CommandContext(
			ctx,
			"docker",
			"exec",
			container,
			"/opt/kafka/bin/kafka-broker-api-versions.sh",
			"--bootstrap-server",
			"localhost:19092",
		).CombinedOutput()
		if err == nil {
			return
		}
		lastOutput = boundedServiceOutput(output)
		select {
		case <-ctx.Done():
			t.Fatalf("wait for Apache Kafka: %v: %s", context.Cause(ctx), lastOutput)
		case <-ticker.C:
		}
	}
}

func createServiceTopic(t *testing.T, ctx context.Context, container string, topic string) {
	t.Helper()

	runServiceDockerExec(
		t,
		ctx,
		container,
		"/opt/kafka/bin/kafka-topics.sh",
		"--bootstrap-server",
		"localhost:19092",
		"--create",
		"--topic",
		topic,
		"--partitions",
		"1",
		"--replication-factor",
		"1",
	)
}

func runServiceDockerExec(
	t *testing.T,
	ctx context.Context,
	container string,
	args ...string,
) string {
	t.Helper()

	commandArgs := append([]string{"exec", container}, args...)
	output, err := exec.CommandContext(ctx, "docker", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("execute Apache Kafka fixture command: %v: %s", err, boundedServiceOutput(output))
	}

	return strings.TrimSpace(string(output))
}

func reserveServicePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve Apache Kafka port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release Apache Kafka port reservation: %v", err)
	}

	return port
}

func boundedServiceOutput(output []byte) string {
	const maximum = 4 << 10
	if len(output) > maximum {
		output = output[len(output)-maximum:]
	}

	return strings.TrimSpace(string(output))
}
