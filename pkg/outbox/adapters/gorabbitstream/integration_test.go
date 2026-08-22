//go:build integration

package gorabbitstream_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gorabbitstream"
	"github.com/faustbrian/golib/pkg/rabbitstream"
	rabbitmqadapter "github.com/faustbrian/golib/pkg/rabbitstream/rabbitmq"
	"github.com/rabbitmq/rabbitmq-stream-go-client/pkg/stream"
)

func TestConfirmedOutboxPublicationPreservesStableIdentityAcrossRelayRetry(t *testing.T) {
	connection, environment := integrationBroker(t)
	streamName := "codex-outbox-rabbitstream-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := environment.DeclareStream(streamName, stream.NewStreamOptions().SetMaxAge(time.Hour)); err != nil {
		t.Fatalf("declare disposable stream: %v", err)
	}
	t.Cleanup(func() {
		if err := environment.DeleteStream(streamName); err != nil {
			t.Errorf("delete disposable stream: %v", err)
		}
		if err := environment.Close(); err != nil {
			t.Errorf("close provisioning environment: %v", err)
		}
	})

	producer, err := rabbitmqadapter.OpenProducer(t.Context(), connection, rabbitstream.ProducerConfig{
		Stream: streamName,
	})
	if err != nil {
		t.Fatalf("open producer: %v", err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := producer.Close(closeCtx); err != nil {
			t.Errorf("close producer: %v", err)
		}
	})
	publisher, err := gorabbitstream.New(producer, gorabbitstream.Config{Stream: streamName})
	if err != nil {
		t.Fatalf("construct publisher: %v", err)
	}
	envelope := outbox.Envelope{
		ID: "event-123", Topic: streamName, OrderingKey: "tracked-item-123",
		IdempotencyKey: "command-123", PayloadVersion: 7,
		Payload: []byte("canonical payload"), CreatedAt: time.Now().UTC().Truncate(time.Millisecond),
		Metadata: map[string]string{
			"es.content_type": "application/octet-stream",
			"correlation-id":  "tracking-123",
			"traceparent":     "00-0123456789abcdef0123456789abcdef-0123456789abcdef-01",
		},
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := publisher.Publish(t.Context(), envelope); err != nil {
			t.Fatalf("confirmed publish attempt %d: %v", attempt+1, err)
		}
	}

	consumer, err := rabbitmqadapter.OpenConsumer(t.Context(), connection, rabbitstream.ConsumerConfig{
		Stream: streamName, ConsumerName: "outbox-consumer-" + strconv.FormatInt(time.Now().UnixNano(), 36),
		Start: rabbitstream.StartPosition{Kind: rabbitstream.OffsetStartBeginning},
	})
	if err != nil {
		t.Fatalf("open consumer: %v", err)
	}
	runCtx, cancelRun := context.WithCancel(t.Context())
	deliveries := make(chan rabbitstream.Message, 2)
	runDone := make(chan error, 1)
	go func() {
		runDone <- consumer.Run(runCtx, func(_ context.Context, message rabbitstream.Message) error {
			deliveries <- message.Retain()
			return nil
		})
	}()
	received := make([]rabbitstream.Message, 0, 2)
	for len(received) < 2 {
		select {
		case delivery := <-deliveries:
			received = append(received, delivery)
		case <-time.After(20 * time.Second):
			cancelRun()
			t.Fatal("timed out waiting for duplicate deliveries")
		}
	}
	cancelRun()
	if err := <-runDone; !errors.Is(err, rabbitstream.ErrCanceled) {
		t.Fatalf("consumer run: %v", err)
	}
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelClose()
	if err := consumer.Close(closeCtx); err != nil {
		t.Fatalf("close consumer: %v", err)
	}
	for index, delivery := range received {
		if delivery.MessageID != envelope.ID || delivery.RoutingKey != envelope.OrderingKey ||
			delivery.CorrelationID != "tracking-123" || delivery.ContentType != "application/octet-stream" ||
			string(delivery.Payload) != string(envelope.Payload) || !delivery.Timestamp.Equal(envelope.CreatedAt) {
			t.Fatalf("delivery %d = %#v", index, delivery)
		}
	}
	if received[0].Offset == received[1].Offset {
		t.Fatalf("relay retry was not observable as a duplicate: %#v", received)
	}
}

func integrationBroker(t *testing.T) (rabbitstream.ConnectionConfig, *stream.Environment) {
	t.Helper()
	host := requiredIntegrationEnv(t, "RABBITSTREAM_TEST_HOST")
	portText := requiredIntegrationEnv(t, "RABBITSTREAM_TEST_PORT")
	username := requiredIntegrationEnv(t, "RABBITSTREAM_TEST_USER")
	password := requiredIntegrationEnv(t, "RABBITSTREAM_TEST_PASSWORD")
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil || port == 0 {
		t.Fatal("RABBITSTREAM_TEST_PORT is invalid")
	}
	connection := rabbitstream.ConnectionConfig{
		Endpoints:      []rabbitstream.Endpoint{{Host: host, Port: uint16(port)}},
		Credentials:    rabbitstream.StaticCredentials(username, []byte(password)),
		Security:       rabbitstream.DevelopmentPlaintextSecurity(),
		ConnectTimeout: 10 * time.Second, RPCTimeout: 10 * time.Second,
		MaxReconnectAttempts: 2,
	}
	environment, err := stream.NewEnvironment(
		stream.NewEnvironmentOptions().SetHost(host).SetPort(int(port)).
			SetUser(username).SetPassword(password).SetRPCTimeout(10 * time.Second),
	)
	if err != nil {
		t.Fatalf("open provisioning environment: %v", err)
	}

	return connection, environment
}

func requiredIntegrationEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s is required", name)
	}

	return value
}
