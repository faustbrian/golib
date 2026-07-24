package gooutbox

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	webhook "github.com/faustbrian/golib/pkg/webhook"
)

func TestBuildMapsDeliveryToOutboxEnvelope(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	builder, err := outbox.NewEnvelopeBuilder(
		outbox.WithClock(func() time.Time { return now }),
		outbox.WithIDGenerator(func() (string, error) { return "outbox-1", nil }),
	)
	if err != nil {
		t.Fatalf("NewEnvelopeBuilder() error = %v", err)
	}
	endpoint, _ := url.Parse("https://example.com/hook")
	delivery := webhook.DeliveryRequest{
		Endpoint: endpoint, Body: []byte("body"), EventID: "event", IdempotencyKey: "idem",
		Metadata: map[string]string{"tenant": "one"},
	}
	envelope, err := Build(builder, "webhook-delivery", delivery, 4096)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if envelope.ID != "outbox-1" || envelope.Topic != "webhook-delivery" ||
		envelope.OrderingKey != "event" || envelope.IdempotencyKey != "idem" {
		t.Fatalf("envelope = %#v", envelope)
	}
	decoded, err := webhook.UnmarshalDeliveryRequest(envelope.Payload, 4096)
	if err != nil || decoded.EventID != "event" {
		t.Fatalf("decoded payload = %#v, error = %v", decoded, err)
	}
}

func TestPublisherPerformsSingleAttemptForRelay(t *testing.T) {
	t.Parallel()

	doer := &countingDoer{err: errors.New("network failed")}
	deliverer := newDeliverer(t, doer)
	publisher, err := NewPublisher(deliverer, 4096)
	if err != nil {
		t.Fatalf("NewPublisher() error = %v", err)
	}
	endpoint, _ := url.Parse("https://example.com/hook")
	payload, err := webhook.MarshalDeliveryRequest(webhook.DeliveryRequest{
		Endpoint: endpoint, Body: []byte("body"), EventID: "event", IdempotencyKey: "idem",
	}, 4096)
	if err != nil {
		t.Fatalf("MarshalDeliveryRequest() error = %v", err)
	}

	err = publisher.Publish(context.Background(), outbox.Envelope{Payload: payload})
	if !errors.Is(err, webhook.ErrDeliveryFailed) || doer.calls != 1 {
		t.Fatalf("Publish() error = %v, calls = %d", err, doer.calls)
	}
}

func TestAdapterValidatesConfigurationAndPayload(t *testing.T) {
	t.Parallel()

	if _, err := NewPublisher(nil, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("NewPublisher() error = %v, want ErrInvalidConfig", err)
	}
	if _, err := Build(nil, "", webhook.DeliveryRequest{}, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Build() error = %v, want ErrInvalidConfig", err)
	}
	publisher := &Publisher{maxBytes: 1}
	if err := publisher.Publish(context.Background(), outbox.Envelope{Payload: []byte("too large")}); !errors.Is(err, webhook.ErrDeliveryEncoding) {
		t.Fatalf("Publish() payload error = %v", err)
	}
	builder, _ := outbox.NewEnvelopeBuilder()
	endpoint, _ := url.Parse("https://example.com/hook")
	if _, err := Build(builder, "topic", webhook.DeliveryRequest{Endpoint: endpoint, Body: []byte("body"), EventID: "event"}, 1); !errors.Is(err, webhook.ErrDeliveryEncoding) {
		t.Fatalf("Build() encoding error = %v", err)
	}
	buildErr := errors.New("ID unavailable")
	failingBuilder, _ := outbox.NewEnvelopeBuilder(outbox.WithIDGenerator(func() (string, error) { return "", buildErr }))
	if _, err := Build(failingBuilder, "topic", webhook.DeliveryRequest{Endpoint: endpoint, Body: []byte("body"), EventID: "event"}, 4096); !errors.Is(err, buildErr) {
		t.Fatalf("Build() builder error = %v", err)
	}
	successDeliverer := newDeliverer(t, httpDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
	}))
	successPublisher, _ := NewPublisher(successDeliverer, 4096)
	payload, _ := webhook.MarshalDeliveryRequest(webhook.DeliveryRequest{Endpoint: endpoint, Body: []byte("body"), EventID: "event"}, 4096)
	if err := successPublisher.Publish(context.Background(), outbox.Envelope{Payload: payload}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
}

type httpDoerFunc func(*http.Request) (*http.Response, error)

func (f httpDoerFunc) Do(request *http.Request) (*http.Response, error) { return f(request) }

type countingDoer struct {
	calls int
	err   error
}

func (d *countingDoer) Do(*http.Request) (*http.Response, error) {
	d.calls++

	return nil, d.err
}

func newDeliverer(t *testing.T, doer webhook.HTTPDoer) *webhook.Deliverer {
	t.Helper()

	now := time.Unix(1_700_000_000, 0)
	signer, err := webhook.NewSigner(webhook.SignerConfig{
		Algorithm: webhook.SHA256,
		Keys:      []webhook.SigningKey{{ID: "key", Secret: []byte("secret")}},
		Clock:     func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	deliverer, err := webhook.NewDeliverer(webhook.DeliveryConfig{
		Client: doer, Signer: signer,
		EndpointPolicy: webhook.EndpointPolicyFunc(func(context.Context, *url.URL) error { return nil }),
		Retry:          webhook.RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond, MaxDelay: time.Second},
		Clock:          func() time.Time { return now }, Sleep: func(context.Context, time.Duration) error { return nil },
		IDGenerator: func() (string, error) { return "id", nil }, MaxRequestBytes: 4096,
		MaxResponseBytes: 4096, MaxFanOut: 4,
		HeaderLimits: webhook.HeaderLimits{MaxSignatures: 1, MaxBytes: 256},
	})
	if err != nil {
		t.Fatalf("NewDeliverer() error = %v", err)
	}

	return deliverer
}
