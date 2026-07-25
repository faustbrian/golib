package idempotencywebhook_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencywebhook"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
)

func TestProcessorDeduplicatesProviderDelivery(t *testing.T) {
	processor := fixture(t)
	delivery := providerDelivery{
		provider: "stripe", id: "evt_42", payload: []byte("event payload"),
	}
	var calls atomic.Int64
	handler := func(ctx context.Context, actual idempotencywebhook.Delivery) error {
		calls.Add(1)
		ownership, found := idempotency.OwnershipFromContext(ctx)
		actualDelivery := actual.(providerDelivery)
		if !found || ownership.FencingToken != 1 || actualDelivery.id != delivery.id {
			t.Fatalf("ownership = %#v, found = %t, delivery = %#v", ownership, found, actual)
		}
		return nil
	}
	if err := processor.Handle(context.Background(), delivery, handler); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if err := processor.Handle(context.Background(), delivery, handler); err != nil {
		t.Fatalf("Handle() replay error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d", calls.Load())
	}
}

func TestProcessorPreservesHandlerFailureAndTypedWrapper(t *testing.T) {
	processor := fixture(t)
	delivery := providerDelivery{provider: "github", id: "delivery-1", payload: []byte("push")}
	handlerErr := errors.New("retry webhook")
	wrapped := idempotencywebhook.Wrap(processor, func(
		context.Context, providerDelivery,
	) error {
		return handlerErr
	})
	if err := invokeProviderHandler(context.Background(), wrapped, delivery); !errors.Is(err, handlerErr) {
		t.Fatalf("wrapped handler error = %v", err)
	}
}

func invokeProviderHandler(
	ctx context.Context,
	handler func(context.Context, providerDelivery) error,
	delivery providerDelivery,
) error {
	return handler(ctx, delivery)
}

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	if _, err := idempotencywebhook.New(idempotencywebhook.Options{}); err == nil {
		t.Fatal("New() error = nil")
	}
}

func TestProcessorRejectsNilHandlers(t *testing.T) {
	processor := fixture(t)
	delivery := providerDelivery{provider: "github", id: "nil", payload: []byte("push")}
	if err := processor.Handle(context.Background(), delivery, nil); err == nil {
		t.Fatal("Handle() nil handler error = nil")
	}
	wrapped := idempotencywebhook.Wrap[providerDelivery](processor, nil)
	if err := wrapped(context.Background(), delivery); err == nil {
		t.Fatal("Wrap() nil handler error = nil")
	}
}

func fixture(t *testing.T) *idempotencywebhook.Processor {
	t.Helper()
	store, err := memory.New(memory.Options{
		Clock:       idempotencytest.NewClock(time.Unix(1_700_000_000, 0).UTC()),
		OwnerTokens: idempotencytest.NewTokenSource("webhook-owner").Next,
	})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	service, err := idempotency.NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	processor, err := idempotencywebhook.New(idempotencywebhook.Options{
		Service: service, Lease: time.Minute,
		Key: func(_ context.Context, delivery idempotencywebhook.Delivery) (idempotency.Key, error) {
			provider := delivery.(providerDelivery)
			return idempotency.NewKey(
				"webhook", "tenant", provider.provider, "endpoint", provider.id,
			)
		},
		Fingerprint: func(delivery idempotencywebhook.Delivery) (idempotency.Fingerprint, error) {
			return idempotency.NewFingerprint("webhook-v1", delivery.Payload())
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return processor
}

type providerDelivery struct {
	provider string
	id       string
	payload  []byte
}

func (d providerDelivery) Payload() []byte { return d.payload }
