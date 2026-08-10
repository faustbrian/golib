//go:build interoperability

package gotelemetry_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/kafka/adapters/gotelemetry"
	"go.opentelemetry.io/otel/trace"
)

const integrationKafkaImage = "apache/kafka:4.3.1@" +
	"sha256:77e3df9054047a88b520d0cc46e16696d3b22022e1d580aeccd2632df6532837"

func TestTraceContextPropagationAcrossApacheKafka(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	broker, container := startIntegrationKafka(t, ctx)
	topic := fmt.Sprintf("golib-gotelemetry-%d", time.Now().UnixNano())
	createIntegrationTopic(t, ctx, container, topic)

	policy, err := gotelemetry.NewTraceContextPropagation(
		kafka.DefaultMessageLimits(),
	)
	if err != nil {
		t.Fatalf("construct trace-context policy: %v", err)
	}
	traceState, err := trace.ParseTraceState("vendor=value")
	if err != nil {
		t.Fatalf("parse test trace state: %v", err)
	}
	want := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{17, 18, 19, 20, 21, 22, 23, 24},
		TraceFlags: trace.FlagsSampled,
		TraceState: traceState,
	})
	outbound, err := policy.Inject(
		trace.ContextWithSpanContext(ctx, want),
		kafka.ProducerRecord{
			Topic: topic,
			Key:   []byte("order-42"),
			Value: []byte("created"),
			Headers: []kafka.Header{{
				Key: "content-type", Value: []byte("application/octet-stream"),
			}},
		},
	)
	if err != nil {
		t.Fatalf("inject trace context: %v", err)
	}

	producer, err := kafka.NewProducer(kafka.ProducerConfig{
		Brokers:       []string{broker},
		ClientID:      "golib-gotelemetry-integration-producer",
		AllowedTopics: []string{topic},
		Security:      kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct producer: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := producer.Close(); closeErr != nil {
			t.Errorf("close producer: %v", closeErr)
		}
	})
	if err := producer.Publish(ctx, outbound); err != nil {
		t.Fatalf("publish propagated record: %v", err)
	}

	consumer, err := kafka.NewConsumer(kafka.ConsumerConfig{
		Brokers:        []string{broker},
		ClientID:       "golib-gotelemetry-integration-consumer",
		GroupID:        "golib-gotelemetry-integration-v1",
		Topics:         []string{topic},
		ResetOffset:    kafka.OffsetEarliest,
		MaxPollRecords: 1,
		Security:       kafka.DevelopmentPlaintextSecurity(),
	})
	if err != nil {
		t.Fatalf("construct consumer: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := consumer.Close(); closeErr != nil {
			t.Errorf("close consumer: %v", closeErr)
		}
	})

	consumed := false
	for !consumed {
		result, runErr := consumer.RunOnce(ctx, kafka.HandlerFunc(func(
			handlerCtx context.Context,
			record kafka.ConsumedRecord,
		) error {
			extracted, extractErr := policy.Extract(handlerCtx, record)
			if extractErr != nil {
				return extractErr
			}
			got := trace.SpanContextFromContext(extracted)
			if got.TraceID() != want.TraceID() ||
				got.SpanID() != want.SpanID() ||
				got.TraceFlags() != want.TraceFlags() ||
				got.TraceState().String() != want.TraceState().String() ||
				!got.IsRemote() {
				return fmt.Errorf(
					"extracted span context = %v, want remote %v",
					got,
					want,
				)
			}
			if string(record.Key) != "order-42" ||
				string(record.Value) != "created" ||
				!hasHeader(record.Headers, "content-type", "application/octet-stream") {
				return errors.New(
					"consumed record did not preserve expected fixture data",
				)
			}
			consumed = true

			return nil
		}))
		if runErr != nil {
			t.Fatalf("consume propagated record: %v", runErr)
		}
		if consumed && (result.Processed != 1 || result.Committed != 1) {
			t.Fatalf("consume result = %#v", result)
		}
		if ctx.Err() != nil {
			t.Fatalf("wait for propagated record: %v", ctx.Err())
		}
	}
}

func startIntegrationKafka(
	t *testing.T,
	ctx context.Context,
) (string, string) {
	t.Helper()

	port := reserveIntegrationPort(t)
	container := fmt.Sprintf(
		"golib-gotelemetry-%d-%d",
		os.Getpid(),
		time.Now().UnixNano(),
	)
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
		integrationKafkaImage,
	}
	if output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("start pinned Apache Kafka container: %v: %s", err, boundedOutput(output))
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if output, err := exec.CommandContext(
			cleanupCtx,
			"docker",
			"rm",
			"--force",
			container,
		).CombinedOutput(); err != nil {
			t.Errorf("remove Apache Kafka container: %v: %s", err, boundedOutput(output))
		}
	})

	waitForIntegrationKafka(t, ctx, container)
	versionOutput := runDockerExec(
		t,
		ctx,
		container,
		"/opt/kafka/bin/kafka-broker-api-versions.sh",
		"--version",
	)
	fields := strings.Fields(versionOutput)
	if len(fields) == 0 || fields[0] != "4.3.1" {
		t.Fatalf("Apache Kafka runtime version = %q, want 4.3.1", versionOutput)
	}

	return "127.0.0.1:" + strconv.Itoa(port), container
}

func waitForIntegrationKafka(t *testing.T, ctx context.Context, container string) {
	t.Helper()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	var lastOutput string
	for {
		command := exec.CommandContext(
			ctx,
			"docker",
			"exec",
			container,
			"/opt/kafka/bin/kafka-broker-api-versions.sh",
			"--bootstrap-server",
			"localhost:19092",
		)
		output, err := command.CombinedOutput()
		if err == nil {
			return
		}
		lastOutput = boundedOutput(output)
		select {
		case <-ctx.Done():
			t.Fatalf("wait for Apache Kafka: %v: %s", ctx.Err(), lastOutput)
		case <-ticker.C:
		}
	}
}

func createIntegrationTopic(
	t *testing.T,
	ctx context.Context,
	container string,
	topic string,
) {
	t.Helper()

	runDockerExec(
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

func runDockerExec(
	t *testing.T,
	ctx context.Context,
	container string,
	args ...string,
) string {
	t.Helper()

	commandArgs := append([]string{"exec", container}, args...)
	output, err := exec.CommandContext(ctx, "docker", commandArgs...).CombinedOutput()
	if err != nil {
		t.Fatalf("execute Apache Kafka fixture command: %v: %s", err, boundedOutput(output))
	}

	return strings.TrimSpace(string(output))
}

func reserveIntegrationPort(t *testing.T) int {
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

func hasHeader(headers []kafka.Header, key string, value string) bool {
	for _, header := range headers {
		if header.Key == key && string(header.Value) == value {
			return true
		}
	}

	return false
}

func boundedOutput(output []byte) string {
	const maximum = 4 << 10
	if len(output) > maximum {
		output = output[len(output)-maximum:]
	}

	return strings.TrimSpace(string(output))
}
