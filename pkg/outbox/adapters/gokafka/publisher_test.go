package gokafka_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gokafka"
)

func TestPublisherMapsEnvelopeToKafkaMessage(t *testing.T) {
	t.Parallel()

	client := &recordingClient{}
	publisher, err := gokafka.New(client)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	envelope := outbox.Envelope{
		ID:             "event-1",
		Topic:          "track.tracking-event.v1",
		Payload:        []byte(`{"event_id":"event-1"}`),
		PayloadVersion: 7,
		OrderingKey:    "tracked-item-1",
		IdempotencyKey: "event-1",
		Attempts:       2,
		AvailableAt:    time.Unix(1, 0).UTC(),
		CreatedAt:      time.Unix(1, 0).UTC(),
	}

	if err := publisher.Publish(context.Background(), envelope); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if client.calls != 1 {
		t.Fatalf("Publish() calls = %d, want 1", client.calls)
	}
	message := client.message
	if message.Topic != envelope.Topic ||
		string(message.Key) != envelope.OrderingKey ||
		string(message.Value) != string(envelope.Payload) {
		t.Fatalf("Publish() message = %#v", message)
	}
	wantHeaders := []kafka.Header{
		{Key: "content-type", Value: []byte("application/json")},
		{Key: "event-id", Value: []byte(envelope.ID)},
		{Key: "schema-version", Value: []byte(strconv.Itoa(int(envelope.PayloadVersion)))},
		{Key: "idempotency-key", Value: []byte(envelope.IdempotencyKey)},
	}
	if len(message.Headers) != len(wantHeaders) {
		t.Fatalf("Publish() headers = %#v, want %#v", message.Headers, wantHeaders)
	}
	for index, header := range message.Headers {
		if header.Key != wantHeaders[index].Key ||
			string(header.Value) != string(wantHeaders[index].Value) {
			t.Fatalf("Publish() header %d = %#v, want %#v", index, header, wantHeaders[index])
		}
	}
}

func TestPublisherUsesStablePartitionKeyFallbacks(t *testing.T) {
	t.Parallel()

	idempotencyValue := "idempotency-1"
	tests := []struct {
		name        string
		envelope    outbox.Envelope
		wantKey     string
		wantHeaders int
	}{
		{
			name: "idempotency key",
			envelope: outbox.Envelope{
				ID: "event-1", Topic: "events", PayloadVersion: 1,
				IdempotencyKey: idempotencyValue,
			},
			wantKey:     idempotencyValue,
			wantHeaders: 4,
		},
		{
			name: "event ID",
			envelope: outbox.Envelope{
				ID: "event-2", Topic: "events", PayloadVersion: 1,
			},
			wantKey:     "event-2",
			wantHeaders: 3,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := &recordingClient{}
			publisher, err := gokafka.New(client)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if err := publisher.Publish(context.Background(), test.envelope); err != nil {
				t.Fatalf("Publish() error = %v", err)
			}
			if string(client.message.Key) != test.wantKey ||
				len(client.message.Headers) != test.wantHeaders {
				t.Fatalf("message = %#v", client.message)
			}
		})
	}
}

func TestPublisherRequiresClientAndPreservesFailures(t *testing.T) {
	t.Parallel()

	if publisher, err := gokafka.New(nil); publisher != nil ||
		!errors.Is(err, gokafka.ErrClientRequired) {
		t.Fatalf("New(nil) publisher/error = %#v/%v", publisher, err)
	}

	publishErr := errors.New("delivery failed")
	healthErr := errors.New("broker unavailable")
	client := &recordingClient{publishErr: publishErr, healthErr: healthErr}
	publisher, err := gokafka.New(client)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := publisher.Publish(context.Background(), outbox.Envelope{
		ID: "event-1", Topic: "events", PayloadVersion: 1,
	}); !errors.Is(err, publishErr) {
		t.Fatalf("Publish() error = %v, want %v", err, publishErr)
	}
	if err := publisher.Health(context.Background()); !errors.Is(err, healthErr) {
		t.Fatalf("Health() error = %v, want %v", err, healthErr)
	}
}

func TestPublisherRejectsEnvelopeWithoutRequiredRoutingIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		envelope outbox.Envelope
	}{
		{
			name: "event ID",
			envelope: outbox.Envelope{
				Topic: "events", PayloadVersion: 1, OrderingKey: "aggregate-1",
			},
		},
		{
			name: "topic",
			envelope: outbox.Envelope{
				ID: "event-1", PayloadVersion: 1, OrderingKey: "aggregate-1",
			},
		},
		{
			name: "payload version",
			envelope: outbox.Envelope{
				ID: "event-1", Topic: "events", OrderingKey: "aggregate-1",
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := &recordingClient{}
			publisher, err := gokafka.New(client)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			err = publisher.Publish(context.Background(), test.envelope)
			if !errors.Is(err, gokafka.ErrInvalidEnvelope) || client.calls != 0 {
				t.Fatalf("Publish() error/calls = %v/%d", err, client.calls)
			}
		})
	}
}

type recordingClient struct {
	message    kafka.Message
	publishErr error
	healthErr  error
	calls      int
}

func (client *recordingClient) Publish(_ context.Context, message kafka.Message) error {
	client.calls++
	client.message = message

	return client.publishErr
}

func (client *recordingClient) Health(context.Context) error {
	return client.healthErr
}
