package goqueue

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	webhook "github.com/faustbrian/golib/pkg/webhook"
)

func TestEnqueueUsesBoundedCanonicalDeliveryBytes(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	adapter, err := New(Config{Queue: queue, MaxMessageBytes: 2048})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	endpoint, _ := url.Parse("https://example.com/hook")
	delivery := webhook.DeliveryRequest{
		Endpoint: endpoint, Body: []byte("body"), EventID: "event", IdempotencyKey: "event",
	}
	if err := adapter.Enqueue(context.Background(), delivery); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if len(queue.options) != 1 || queue.options[0].RetryCount == nil || *queue.options[0].RetryCount != 0 {
		t.Fatalf("queue options = %#v", queue.options)
	}
	decoded, err := webhook.UnmarshalDeliveryRequest(queue.message.Bytes(), 2048)
	if err != nil {
		t.Fatalf("UnmarshalDeliveryRequest() error = %v", err)
	}
	if decoded.EventID != "event" || string(decoded.Body) != "body" {
		t.Fatalf("decoded delivery = %#v", decoded)
	}
}

func TestEnqueueChecksCancellationAndMessageLimit(t *testing.T) {
	t.Parallel()

	queue := &recordingQueue{}
	adapter, err := New(Config{Queue: queue, MaxMessageBytes: 32})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := adapter.Enqueue(ctx, webhook.DeliveryRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Enqueue() cancellation error = %v", err)
	}
	endpoint, _ := url.Parse("https://example.com/hook")
	if err := adapter.Enqueue(context.Background(), webhook.DeliveryRequest{Endpoint: endpoint, Body: []byte("large body"), EventID: "event"}); !errors.Is(err, webhook.ErrDeliveryEncoding) {
		t.Fatalf("Enqueue() limit error = %v", err)
	}
	if queue.calls != 0 {
		t.Fatalf("queue calls = %d", queue.calls)
	}
}

func TestEnqueueAndHandleSurfaceDependencyErrors(t *testing.T) {
	t.Parallel()

	queueErr := errors.New("queue offline")
	adapter, _ := New(Config{Queue: &recordingQueue{err: queueErr}, MaxMessageBytes: 2048})
	endpoint, _ := url.Parse("https://example.com/hook")
	err := adapter.Enqueue(context.Background(), webhook.DeliveryRequest{Endpoint: endpoint, Body: []byte("body"), EventID: "event"})
	if !errors.Is(err, queueErr) {
		t.Fatalf("Enqueue() error = %v", err)
	}
	deliverer := newDeliverer(t, &countingDoer{})
	if _, err := Handle(context.Background(), deliverer, []byte("{"), 2048); !errors.Is(err, webhook.ErrDeliveryEncoding) {
		t.Fatalf("Handle() decode error = %v", err)
	}
}

func TestHandleUsesSingleDeliveryAttempt(t *testing.T) {
	t.Parallel()

	doer := &countingDoer{err: errors.New("network failed")}
	deliverer := newDeliverer(t, doer)
	endpoint, _ := url.Parse("https://example.com/hook")
	encoded, err := webhook.MarshalDeliveryRequest(webhook.DeliveryRequest{
		Endpoint: endpoint, Body: []byte("body"), EventID: "event", IdempotencyKey: "event",
	}, 2048)
	if err != nil {
		t.Fatalf("MarshalDeliveryRequest() error = %v", err)
	}

	_, err = Handle(context.Background(), deliverer, encoded, 2048)
	if !errors.Is(err, webhook.ErrDeliveryFailed) || doer.calls != 1 {
		t.Fatalf("Handle() error = %v, calls = %d", err, doer.calls)
	}
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	t.Parallel()

	if _, err := New(Config{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
	}
	if _, err := Handle(context.Background(), nil, nil, 1); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Handle() error = %v, want ErrInvalidConfig", err)
	}
}

type recordingQueue struct {
	message core.QueuedMessage
	options []job.AllowOption
	calls   int
	err     error
}

func (q *recordingQueue) Queue(message core.QueuedMessage, options ...job.AllowOption) error {
	q.calls++
	q.message = message
	q.options = options

	return q.err
}

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
		IDGenerator: func() (string, error) { return "id", nil }, MaxRequestBytes: 2048,
		MaxResponseBytes: 2048, MaxFanOut: 4,
		HeaderLimits: webhook.HeaderLimits{MaxSignatures: 1, MaxBytes: 256},
	})
	if err != nil {
		t.Fatalf("NewDeliverer() error = %v", err)
	}

	return deliverer
}
