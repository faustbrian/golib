package gorabbitstream_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/adapters/gorabbitstream"
	"github.com/faustbrian/golib/pkg/outbox/relay"
	"github.com/faustbrian/golib/pkg/rabbitstream"
)

func TestPublisherConstructionRequiresClientOneTargetAndValidLimits(t *testing.T) {
	t.Parallel()

	if publisher, err := gorabbitstream.New(nil, gorabbitstream.Config{Stream: "events"}); publisher != nil || !errors.Is(err, gorabbitstream.ErrClientRequired) {
		t.Fatalf("nil client publisher/error = %#v/%v", publisher, err)
	}
	client := &recordingClient{}
	for name, config := range map[string]gorabbitstream.Config{
		"no target":   {},
		"two targets": {Stream: "events", SuperStream: "events"},
		"invalid limits": {
			Stream: "events", Limits: rabbitstream.Limits{MaxStreamNameBytes: 1},
		},
	} {
		t.Run(name, func(t *testing.T) {
			publisher, err := gorabbitstream.New(client, config)
			if publisher != nil || !errors.Is(err, gorabbitstream.ErrInvalidConfig) {
				t.Fatalf("New() publisher/error = %#v/%v", publisher, err)
			}
		})
	}
}

func TestPublisherMapsEnvelopeToOwnedConfirmedStreamMessage(t *testing.T) {
	t.Parallel()

	client := &recordingClient{result: rabbitstream.DeliveryResult{State: rabbitstream.DeliveryConfirmed}}
	publisher, err := gorabbitstream.New(client, gorabbitstream.Config{Stream: "tracking.events"})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("event payload")
	metadata := map[string]string{
		"es.content_type": "application/vnd.tracking+json",
		"correlation-id":  "tracking-123",
		"traceparent":     "00-00000000000000000000000000000001-0000000000000001-01",
		"tracestate":      "vendor=value",
		"z-domain":        "last",
		"a-domain":        "first",
	}
	envelope := outbox.Envelope{
		ID: "event-123", Topic: "tracking.events", Payload: payload,
		PayloadVersion: 7, Metadata: metadata, OrderingKey: "tracked-item-123",
		IdempotencyKey: "command-123", CreatedAt: time.Unix(1_700_000_000, 123).UTC(),
	}

	if err := publisher.Publish(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
	want := rabbitstream.Message{
		Stream: "tracking.events", RoutingKey: "tracked-item-123",
		Timestamp: envelope.CreatedAt, ContentType: "application/vnd.tracking+json",
		MessageID: "event-123", CorrelationID: "tracking-123", Payload: []byte("event payload"),
		Headers: []rabbitstream.MetadataEntry{
			{Key: "traceparent", Value: []byte(metadata["traceparent"])},
			{Key: "tracestate", Value: []byte(metadata["tracestate"])},
		},
		Properties: []rabbitstream.MetadataEntry{
			{Key: "schema-version", Value: []byte("7")},
			{Key: "idempotency-key", Value: []byte("command-123")},
			{Key: "a-domain", Value: []byte("first")},
			{Key: "es.content_type", Value: []byte("application/vnd.tracking+json")},
			{Key: "z-domain", Value: []byte("last")},
		},
	}
	if !reflect.DeepEqual(client.message, want) {
		t.Fatalf("stream message = %#v, want %#v", client.message, want)
	}
	payload[0] = 'X'
	metadata["a-domain"] = "changed"
	if string(client.message.Payload) != "event payload" ||
		string(client.message.Properties[2].Value) != "first" {
		t.Fatalf("publisher retained caller-owned data: %#v", client.message)
	}
}

func TestPublisherMapsSuperStreamAndStableRoutingFallbacks(t *testing.T) {
	t.Parallel()

	for name, envelope := range map[string]outbox.Envelope{
		"idempotency key": {
			ID: "event-1", Topic: "tracking.events", PayloadVersion: 1,
			IdempotencyKey: "command-1",
		},
		"event ID": {ID: "event-2", Topic: "tracking.events", PayloadVersion: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := &recordingClient{result: rabbitstream.DeliveryResult{State: rabbitstream.DeliveryConfirmed}}
			publisher, err := gorabbitstream.New(client, gorabbitstream.Config{SuperStream: "tracking.events"})
			if err != nil {
				t.Fatal(err)
			}
			if err := publisher.Publish(t.Context(), envelope); err != nil {
				t.Fatal(err)
			}
			wantKey := envelope.IdempotencyKey
			if wantKey == "" {
				wantKey = envelope.ID
			}
			if client.message.SuperStream != "tracking.events" || client.message.Stream != "" ||
				client.message.RoutingKey != wantKey {
				t.Fatalf("target/routing = %#v", client.message)
			}
		})
	}
}

func TestPublisherRejectsInvalidEnvelopeBeforeClientAdmission(t *testing.T) {
	t.Parallel()

	client := &recordingClient{}
	publisher, err := gorabbitstream.New(client, gorabbitstream.Config{Stream: "tracking.events"})
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]outbox.Envelope{
		"missing event ID":       {Topic: "tracking.events", PayloadVersion: 1},
		"wrong target":           {ID: "event-1", Topic: "other.events", PayloadVersion: 1},
		"missing schema version": {ID: "event-1", Topic: "tracking.events"},
		"reserved metadata": {
			ID: "event-1", Topic: "tracking.events", PayloadVersion: 1,
			Metadata: map[string]string{"schema-version": "forged"},
		},
	}
	for name, envelope := range tests {
		t.Run(name, func(t *testing.T) {
			err := publisher.Publish(t.Context(), envelope)
			if !errors.Is(err, gorabbitstream.ErrInvalidEnvelope) {
				t.Fatalf("Publish() error = %v", err)
			}
		})
	}
	if client.calls != 0 {
		t.Fatalf("client calls = %d", client.calls)
	}
}

func TestPublisherRequiresDefiniteConfirmationAndClassifiesRelayFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		result     rabbitstream.DeliveryResult
		publishErr error
		want       error
		class      relay.ErrorClass
	}{
		{
			name: "ambiguous", result: rabbitstream.DeliveryResult{State: rabbitstream.DeliveryAmbiguous},
			publishErr: &rabbitstream.OperationError{Operation: rabbitstream.OperationPublish, Category: rabbitstream.CategoryPublishAmbiguous},
			want:       rabbitstream.ErrPublishAmbiguous, class: relay.ErrorTransient,
		},
		{
			name: "oversized", result: rabbitstream.DeliveryResult{State: rabbitstream.DeliveryNotSent},
			publishErr: &rabbitstream.OperationError{Operation: rabbitstream.OperationPublish, Category: rabbitstream.CategoryMessageTooLarge},
			want:       rabbitstream.ErrMessageTooLarge, class: relay.ErrorPermanent,
		},
		{
			name: "rejected without client error", result: rabbitstream.DeliveryResult{State: rabbitstream.DeliveryRejected},
			want: rabbitstream.ErrBrokerRejected, class: relay.ErrorPermanent,
		},
		{
			name: "unconfirmed success", result: rabbitstream.DeliveryResult{State: rabbitstream.DeliveryNotSent},
			class: relay.ErrorTransient,
		},
		{
			name: "ambiguous without client error", result: rabbitstream.DeliveryResult{State: rabbitstream.DeliveryAmbiguous},
			want: rabbitstream.ErrPublishAmbiguous, class: relay.ErrorTransient,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := &recordingClient{result: test.result, err: test.publishErr}
			publisher, err := gorabbitstream.New(client, gorabbitstream.Config{Stream: "tracking.events"})
			if err != nil {
				t.Fatal(err)
			}
			publishErr := publisher.Publish(t.Context(), outbox.Envelope{
				ID: "event-1", Topic: "tracking.events", PayloadVersion: 1,
			})
			if publishErr == nil {
				t.Fatal("Publish() succeeded without a confirmation")
			}
			if test.want != nil && !errors.Is(publishErr, test.want) {
				t.Fatalf("Publish() error = %v, want %v", publishErr, test.want)
			}
			if class := gorabbitstream.ClassifyError(publishErr); class != test.class {
				t.Fatalf("ClassifyError() = %v, want %v", class, test.class)
			}
		})
	}
}

func TestClassifyErrorDistinguishesPermanentInputFailuresFromTransientOutcomes(t *testing.T) {
	t.Parallel()

	for name, err := range map[string]error{
		"invalid envelope":      gorabbitstream.ErrInvalidEnvelope,
		"invalid configuration": rabbitstream.ErrInvalidConfiguration,
		"validation":            rabbitstream.ErrValidation,
		"oversized message":     rabbitstream.ErrMessageTooLarge,
		"broker rejection":      rabbitstream.ErrBrokerRejected,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if class := gorabbitstream.ClassifyError(err); class != relay.ErrorPermanent {
				t.Fatalf("ClassifyError() = %v, want %v", class, relay.ErrorPermanent)
			}
		})
	}

	for name, err := range map[string]error{
		"cancellation":      context.Canceled,
		"ambiguous publish": rabbitstream.ErrPublishAmbiguous,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if class := gorabbitstream.ClassifyError(err); class != relay.ErrorTransient {
				t.Fatalf("ClassifyError() = %v, want %v", class, relay.ErrorTransient)
			}
		})
	}
}

func TestPublisherEnforcesConfiguredMessageBoundsBeforeClientAdmission(t *testing.T) {
	t.Parallel()

	limits := rabbitstream.DefaultLimits()
	limits.MaxPayloadBytes = 4
	client := &recordingClient{}
	publisher, err := gorabbitstream.New(client, gorabbitstream.Config{
		Stream: "tracking.events", Limits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	publishErr := publisher.Publish(t.Context(), outbox.Envelope{
		ID: "event-1", Topic: "tracking.events", PayloadVersion: 1,
		Payload: []byte(strings.Repeat("x", 5)),
	})
	if !errors.Is(publishErr, gorabbitstream.ErrInvalidEnvelope) ||
		!errors.Is(publishErr, rabbitstream.ErrMessageTooLarge) || client.calls != 0 {
		t.Fatalf("Publish() error/calls = %v/%d", publishErr, client.calls)
	}
}

func TestPublisherContainsClientPanicAsAmbiguousAndRejectsNilContext(t *testing.T) {
	t.Parallel()

	client := &panickingClient{}
	publisher, err := gorabbitstream.New(client, gorabbitstream.Config{Stream: "tracking.events"})
	if err != nil {
		t.Fatal(err)
	}
	envelope := outbox.Envelope{ID: "event-secret", Topic: "tracking.events", PayloadVersion: 1}
	panicErr := publisher.Publish(t.Context(), envelope)
	if !errors.Is(panicErr, rabbitstream.ErrPublishAmbiguous) ||
		gorabbitstream.ClassifyError(panicErr) != relay.ErrorTransient {
		t.Fatalf("panic error/class = %v/%v", panicErr, gorabbitstream.ClassifyError(panicErr))
	}
	if panicErr.Error() != "outbox/gorabbitstream: publish failed" {
		t.Fatalf("panic diagnostic = %q", panicErr)
	}

	recording := &recordingClient{}
	publisher, err = gorabbitstream.New(recording, gorabbitstream.Config{Stream: "tracking.events"})
	if err != nil {
		t.Fatal(err)
	}
	var nilContext context.Context
	if err := publisher.Publish(nilContext, envelope); !errors.Is(err, gorabbitstream.ErrContextRequired) {
		t.Fatalf("nil context error = %v", err)
	}
	if recording.calls != 0 {
		t.Fatalf("nil context client calls = %d", recording.calls)
	}
}

type recordingClient struct {
	message rabbitstream.Message
	result  rabbitstream.DeliveryResult
	err     error
	calls   int
}

func (client *recordingClient) Publish(_ context.Context, message rabbitstream.Message) (rabbitstream.DeliveryResult, error) {
	client.calls++
	client.message = message

	return client.result, client.err
}

type panickingClient struct{}

func (*panickingClient) Publish(context.Context, rabbitstream.Message) (rabbitstream.DeliveryResult, error) {
	panic("payload and credential secret")
}
