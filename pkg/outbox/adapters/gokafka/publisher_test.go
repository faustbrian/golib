package gokafka_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
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

func TestPublisherOwnsEveryMappedByteBeforePublish(t *testing.T) {
	t.Parallel()

	client := &recordingClient{}
	publisher, err := gokafka.New(client)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("owned payload")
	envelope := outbox.Envelope{
		ID: "event-1", Topic: "events", PayloadVersion: 1,
		Payload: payload, OrderingKey: "stream-1",
		Metadata: map[string]string{"traceparent": "trace-1"},
	}
	if err := publisher.Publish(t.Context(), envelope); err != nil {
		t.Fatal(err)
	}

	payload[0] = 'X'
	envelope.Payload[1] = 'Y'
	envelope.Metadata["traceparent"] = "changed"
	if got := string(client.message.Value); got != "owned payload" {
		t.Fatalf("retained payload = %q", got)
	}
	if got := string(headerValue(client.message.Headers, "traceparent")); got != "trace-1" {
		t.Fatalf("retained metadata = %q", got)
	}

	client.message.Key[0] = 'X'
	client.message.Value[0] = 'Z'
	client.message.Headers[0].Value[0] = 'X'
	if envelope.OrderingKey != "stream-1" || string(envelope.Payload) != "XYned payload" {
		t.Fatalf("client mutation changed envelope = %#v", envelope)
	}
}

func TestPublisherEnforcesOutboxAndKafkaLimitsBeforePublish(t *testing.T) {
	t.Parallel()

	limits := gokafka.Limits{
		Envelope: outbox.Limits{
			MaxIDBytes: 4, MaxTopicBytes: 8, MaxPayloadBytes: 4,
			MaxMetadataEntries: 2, MaxMetadataBytes: 12,
			MaxOrderingKeyBytes: 4, MaxIdempotencyKeyBytes: 4,
		},
		Kafka: kafka.MessageLimits{
			MaxTopicBytes: 8, MaxKeyBytes: 4, MaxValueBytes: 4,
			MaxHeaders: 5, MaxHeaderKeyBytes: 32,
			MaxHeaderValueBytes: 8, MaxHeaderBytes: 80,
		},
	}
	tests := []struct {
		name   string
		mutate func(*outbox.Envelope)
		want   error
	}{
		{"ID", func(value *outbox.Envelope) { value.ID = "12345" }, outbox.ErrIDTooLarge},
		{"topic", func(value *outbox.Envelope) { value.Topic = "123456789" }, outbox.ErrTopicTooLarge},
		{"payload", func(value *outbox.Envelope) { value.Payload = []byte("12345") }, outbox.ErrPayloadTooLarge},
		{"metadata entries", func(value *outbox.Envelope) { value.Metadata = map[string]string{"a": "1", "b": "2", "c": "3"} }, outbox.ErrMetadataEntriesTooLarge},
		{"metadata bytes", func(value *outbox.Envelope) { value.Metadata = map[string]string{"long": "123456789"} }, outbox.ErrMetadataTooLarge},
		{"ordering key", func(value *outbox.Envelope) { value.OrderingKey = "12345" }, outbox.ErrOrderingKeyTooLarge},
		{"idempotency key", func(value *outbox.Envelope) { value.IdempotencyKey = "12345" }, outbox.ErrIdempotencyKeyTooLarge},
		{"Kafka header count", func(value *outbox.Envelope) {
			value.Metadata = map[string]string{"a": "1", "b": "2"}
			value.IdempotencyKey = "idem"
		}, kafka.ErrTooManyHeaders},
		{"Kafka header value", func(value *outbox.Envelope) { value.Metadata = map[string]string{"a": "123456789"} }, kafka.ErrHeaderValueTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := &recordingClient{}
			publisher, err := gokafka.New(client, gokafka.WithLimits(limits))
			if err != nil {
				t.Fatal(err)
			}
			envelope := outbox.Envelope{ID: "id-1", Topic: "events", Payload: []byte("data"), PayloadVersion: 1, OrderingKey: "key1"}
			test.mutate(&envelope)
			err = publisher.Publish(t.Context(), envelope)
			if !errors.Is(err, test.want) || client.calls != 0 {
				t.Fatalf("Publish() error/calls = %v/%d, want %v/0", err, client.calls, test.want)
			}
		})
	}
}

func TestPublisherRejectsInvalidLimitsAndReservedMetadata(t *testing.T) {
	t.Parallel()

	client := &recordingClient{}
	invalid := gokafka.DefaultLimits()
	invalid.Kafka.MaxValueBytes = 0
	if publisher, err := gokafka.New(client, gokafka.WithLimits(invalid)); publisher != nil ||
		!errors.Is(err, kafka.ErrInvalidMessageLimits) {
		t.Fatalf("New() publisher/error = %#v/%v", publisher, err)
	}
	invalid = gokafka.DefaultLimits()
	invalid.Envelope.MaxPayloadBytes = 0
	if publisher, err := gokafka.New(client, gokafka.WithLimits(invalid)); publisher != nil ||
		!errors.Is(err, outbox.ErrInvalidLimits) {
		t.Fatalf("New() publisher/error = %#v/%v", publisher, err)
	}
	if publisher, err := gokafka.New(client, nil); publisher != nil ||
		!errors.Is(err, gokafka.ErrInvalidConfig) {
		t.Fatalf("New() publisher/error = %#v/%v", publisher, err)
	}

	publisher, err := gokafka.New(client)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"content-type", "event-id", "schema-version", "idempotency-key"} {
		err := publisher.Publish(t.Context(), outbox.Envelope{
			ID: "event-1", Topic: "events", PayloadVersion: 1,
			Metadata: map[string]string{key: "forged"},
		})
		if !errors.Is(err, gokafka.ErrReservedMetadata) {
			t.Fatalf("metadata %q error = %v", key, err)
		}
	}
	if client.calls != 0 {
		t.Fatalf("Publish() calls = %d, want 0", client.calls)
	}
}

func TestPublisherEnforcesEveryKafkaRecordBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		adjust func(*gokafka.Limits, *outbox.Envelope)
		want   error
	}{
		{"topic", func(limits *gokafka.Limits, _ *outbox.Envelope) { limits.Kafka.MaxTopicBytes = 5 }, kafka.ErrTopicTooLarge},
		{"key", func(limits *gokafka.Limits, _ *outbox.Envelope) { limits.Kafka.MaxKeyBytes = 3 }, kafka.ErrKeyTooLarge},
		{"value", func(limits *gokafka.Limits, _ *outbox.Envelope) { limits.Kafka.MaxValueBytes = 3 }, kafka.ErrValueTooLarge},
		{"header count", func(limits *gokafka.Limits, _ *outbox.Envelope) { limits.Kafka.MaxHeaders = 2 }, kafka.ErrTooManyHeaders},
		{"header key required", func(_ *gokafka.Limits, envelope *outbox.Envelope) { envelope.Metadata = map[string]string{"": "value"} }, kafka.ErrHeaderKeyRequired},
		{"header key", func(limits *gokafka.Limits, _ *outbox.Envelope) { limits.Kafka.MaxHeaderKeyBytes = 13 }, kafka.ErrHeaderKeyTooLarge},
		{"header value", func(limits *gokafka.Limits, _ *outbox.Envelope) { limits.Kafka.MaxHeaderValueBytes = 15 }, kafka.ErrHeaderValueTooLarge},
		{"aggregate headers", func(limits *gokafka.Limits, _ *outbox.Envelope) { limits.Kafka.MaxHeaderBytes = 54 }, kafka.ErrHeadersTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limits := gokafka.DefaultLimits()
			envelope := outbox.Envelope{
				ID: "id-1", Topic: "events", Payload: []byte("data"),
				PayloadVersion: 1, OrderingKey: "key1",
			}
			test.adjust(&limits, &envelope)
			client := &recordingClient{}
			publisher, err := gokafka.New(client, gokafka.WithLimits(limits))
			if err != nil {
				t.Fatal(err)
			}
			err = publisher.Publish(t.Context(), envelope)
			if !errors.Is(err, test.want) || client.calls != 0 {
				t.Fatalf("Publish() error/calls = %v/%d, want %v/0", err, client.calls, test.want)
			}
		})
	}
}

func TestPublisherAcceptsExactOutboxAndKafkaBoundaries(t *testing.T) {
	t.Parallel()

	limits := gokafka.Limits{
		Envelope: outbox.Limits{
			MaxIDBytes: 4, MaxTopicBytes: 6, MaxPayloadBytes: 4,
			MaxMetadataEntries: 2, MaxMetadataBytes: 12,
			MaxOrderingKeyBytes: 4, MaxIdempotencyKeyBytes: 4,
		},
		Kafka: kafka.MessageLimits{
			MaxTopicBytes: 6, MaxKeyBytes: 4, MaxValueBytes: 4,
			MaxHeaders: 6, MaxHeaderKeyBytes: 15,
			MaxHeaderValueBytes: 16, MaxHeaderBytes: 86,
		},
	}
	envelope := outbox.Envelope{
		ID: "id-1", Topic: "events", Payload: []byte("data"),
		PayloadVersion: 1, OrderingKey: "key1", IdempotencyKey: "idem",
		Metadata: map[string]string{"abc": "123", "def": "456"},
	}
	client := &recordingClient{}
	publisher, err := gokafka.New(client, gokafka.WithLimits(limits))
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(t.Context(), envelope); err != nil {
		t.Fatalf("Publish() exact boundary error = %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("Publish() calls = %d, want 1", client.calls)
	}

	limits.Kafka.MaxHeaderBytes--
	client = &recordingClient{}
	publisher, err = gokafka.New(client, gokafka.WithLimits(limits))
	if err != nil {
		t.Fatal(err)
	}
	if err := publisher.Publish(t.Context(), envelope); !errors.Is(err, kafka.ErrHeadersTooLarge) || client.calls != 0 {
		t.Fatalf("Publish() aggregate boundary error/calls = %v/%d", err, client.calls)
	}
}

func TestPublisherRejectsMetadataWhoseCombinedSizeExceedsLimit(t *testing.T) {
	t.Parallel()

	limits := gokafka.DefaultLimits()
	limits.Envelope.MaxMetadataBytes = 8
	client := &recordingClient{}
	publisher, err := gokafka.New(client, gokafka.WithLimits(limits))
	if err != nil {
		t.Fatal(err)
	}
	err = publisher.Publish(t.Context(), outbox.Envelope{
		ID: "event-1", Topic: "events", PayloadVersion: 1,
		Metadata: map[string]string{"a": "123", "b": "4567"},
	})
	if !errors.Is(err, outbox.ErrMetadataTooLarge) || client.calls != 0 {
		t.Fatalf("Publish() error/calls = %v/%d", err, client.calls)
	}
}

func TestPublisherEnforcesCumulativeMetadataByteBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		maximum  int
		metadata map[string]string
		wantErr  bool
	}{
		{
			name:     "exact remaining bytes",
			maximum:  4,
			metadata: map[string]string{"aa": "", "bb": ""},
		},
		{
			name:     "cumulative overflow begins at key",
			maximum:  7,
			metadata: map[string]string{"aa": "1", "bb": "2", "cc": "3"},
			wantErr:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limits := gokafka.DefaultLimits()
			limits.Envelope.MaxMetadataBytes = test.maximum
			client := &recordingClient{}
			publisher, err := gokafka.New(client, gokafka.WithLimits(limits))
			if err != nil {
				t.Fatal(err)
			}
			err = publisher.Publish(t.Context(), outbox.Envelope{
				ID: "event-1", Topic: "events", PayloadVersion: 1,
				Metadata: test.metadata,
			})
			if test.wantErr {
				if !errors.Is(err, outbox.ErrMetadataTooLarge) || client.calls != 0 {
					t.Fatalf("Publish() error/calls = %v/%d", err, client.calls)
				}
				return
			}
			if err != nil || client.calls != 1 {
				t.Fatalf("Publish() error/calls = %v/%d", err, client.calls)
			}
		})
	}
}

func TestPublisherEnforcesCumulativeHeaderKeyBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		maximum   int
		metadata  map[string]string
		wantError bool
	}{
		{
			name:     "key exactly consumes remaining bytes",
			maximum:  58,
			metadata: map[string]string{"abc": ""},
		},
		{
			name:      "second key exceeds remaining bytes",
			maximum:   64,
			metadata:  map[string]string{"aaaa": "12", "bbbb": "34"},
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			limits := gokafka.DefaultLimits()
			limits.Kafka.MaxHeaderBytes = test.maximum
			client := &recordingClient{}
			publisher, err := gokafka.New(client, gokafka.WithLimits(limits))
			if err != nil {
				t.Fatal(err)
			}
			err = publisher.Publish(t.Context(), outbox.Envelope{
				ID: "id-1", Topic: "events", PayloadVersion: 1,
				Metadata: test.metadata,
			})
			if test.wantError {
				if !errors.Is(err, kafka.ErrHeadersTooLarge) || client.calls != 0 {
					t.Fatalf("Publish() error/calls = %v/%d", err, client.calls)
				}
				return
			}
			if err != nil || client.calls != 1 {
				t.Fatalf("Publish() error/calls = %v/%d", err, client.calls)
			}
		})
	}
}

func TestPublisherRejectsNilContextBeforePublish(t *testing.T) {
	t.Parallel()

	client := &recordingClient{}
	publisher, err := gokafka.New(client)
	if err != nil {
		t.Fatal(err)
	}
	var ctx context.Context
	err = publisher.Publish(ctx, outbox.Envelope{
		ID: "event-1", Topic: "events", PayloadVersion: 1,
	})
	if !errors.Is(err, kafka.ErrContextRequired) || client.calls != 0 {
		t.Fatalf("Publish() error/calls = %v/%d", err, client.calls)
	}
}

func TestPublisherPreservesDeliveryCategoriesWithoutIdentityDisclosure(t *testing.T) {
	t.Parallel()

	categories := []kafka.ErrorCategory{
		kafka.ErrorPermanent,
		kafka.ErrorRetryable,
		kafka.ErrorAmbiguous,
	}
	for _, category := range categories {
		category := category
		t.Run(category.String(), func(t *testing.T) {
			t.Parallel()
			cause := categorizedError{category: category}
			publisher, err := gokafka.New(&recordingClient{publishErr: cause})
			if err != nil {
				t.Fatal(err)
			}
			err = publisher.Publish(t.Context(), outbox.Envelope{
				ID: "sensitive-event-identity", Topic: "events", PayloadVersion: 1,
			})
			var categorized interface{ Category() kafka.ErrorCategory }
			if !errors.Is(err, cause) || !errors.As(err, &categorized) ||
				categorized.Category() != category {
				t.Fatalf("Publish() error = %v", err)
			}
			if strings.Contains(err.Error(), "sensitive-event-identity") {
				t.Fatalf("Publish() error disclosed envelope identity: %v", err)
			}
		})
	}
}

func TestPublisherConvertsClientPanicToAmbiguousOutcome(t *testing.T) {
	t.Parallel()

	publisher, err := gokafka.New(panickingClient{})
	if err != nil {
		t.Fatal(err)
	}
	err = publisher.Publish(t.Context(), outbox.Envelope{
		ID: "event-1", Topic: "events", PayloadVersion: 1,
	})
	if err == nil {
		t.Fatal("Publish() error = nil, want ambiguous panic outcome")
	}
	var categorized interface{ Category() kafka.ErrorCategory }
	if !errors.Is(err, gokafka.ErrPublishPanic) ||
		!errors.As(err, &categorized) ||
		categorized.Category() != kafka.ErrorAmbiguous {
		t.Fatalf("Publish() error = %v", err)
	}
	if strings.Contains(err.Error(), "secret panic") {
		t.Fatalf("Publish() error disclosed panic value: %v", err)
	}
}

func TestPublisherPropagatesSortedEnvelopeMetadataAndContentType(t *testing.T) {
	t.Parallel()

	client := &recordingClient{}
	publisher, err := gokafka.New(client)
	if err != nil {
		t.Fatal(err)
	}
	envelope := outbox.Envelope{
		ID:             "event-1",
		Topic:          "events",
		PayloadVersion: 1,
		OrderingKey:    "aggregate-1",
		Metadata: map[string]string{
			"es.event_name":   "account.opened",
			"es.content_type": "application/protobuf",
			"es.correlation":  "correlation-1",
		},
	}
	if err := publisher.Publish(t.Context(), envelope); err != nil {
		t.Fatal(err)
	}

	want := []kafka.Header{
		{Key: "content-type", Value: []byte("application/protobuf")},
		{Key: "event-id", Value: []byte("event-1")},
		{Key: "schema-version", Value: []byte("1")},
		{Key: "es.content_type", Value: []byte("application/protobuf")},
		{Key: "es.correlation", Value: []byte("correlation-1")},
		{Key: "es.event_name", Value: []byte("account.opened")},
	}
	if !slices.EqualFunc(
		client.message.Headers,
		want,
		func(left, right kafka.Header) bool {
			return left.Key == right.Key &&
				string(left.Value) == string(right.Value)
		},
	) {
		t.Fatalf("headers = %#v, want %#v", client.message.Headers, want)
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

type categorizedError struct {
	category kafka.ErrorCategory
}

type panickingClient struct{}

func (panickingClient) Publish(context.Context, kafka.Message) error {
	panic("secret panic")
}

func (panickingClient) Health(context.Context) error { return nil }

func (err categorizedError) Error() string { return err.category.String() }

func (err categorizedError) Category() kafka.ErrorCategory { return err.category }

func headerValue(headers []kafka.Header, key string) []byte {
	for _, header := range headers {
		if header.Key == key {
			return bytes.Clone(header.Value)
		}
	}

	return nil
}

func (client *recordingClient) Publish(_ context.Context, message kafka.Message) error {
	client.calls++
	client.message = message

	return client.publishErr
}

func (client *recordingClient) Health(context.Context) error {
	return client.healthErr
}
