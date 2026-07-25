package idempotencyqueue_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencyqueue"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
)

func TestMiddlewareCompletesOnceAndDeduplicatesRedelivery(t *testing.T) {
	middleware, _ := fixture(t)
	message := taskMessage{id: "delivery-1", payload: []byte("payload")}
	var calls atomic.Int64
	handler := func(ctx context.Context, _ idempotencyqueue.Message) error {
		calls.Add(1)
		ownership, found := idempotency.OwnershipFromContext(ctx)
		if !found || ownership.FencingToken != 1 || ownership.OwnerToken == "" {
			t.Fatalf("handler ownership = %#v, %t", ownership, found)
		}
		return nil
	}
	if err := middleware.Handle(context.Background(), message, handler); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if err := middleware.Handle(context.Background(), message, handler); err != nil {
		t.Fatalf("Handle() redelivery error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d", calls.Load())
	}
}

func TestMiddlewareReleasesFailedHandlerForRedelivery(t *testing.T) {
	middleware, _ := fixture(t)
	message := taskMessage{id: "delivery-2", payload: []byte("payload")}
	handlerErr := errors.New("retry handler")
	var calls atomic.Int64
	handler := func(context.Context, idempotencyqueue.Message) error {
		if calls.Add(1) == 1 {
			return handlerErr
		}
		return nil
	}
	if err := middleware.Handle(context.Background(), message, handler); !errors.Is(err, handlerErr) {
		t.Fatalf("Handle() first error = %v", err)
	}
	if err := middleware.Handle(context.Background(), message, handler); err != nil {
		t.Fatalf("Handle() redelivery error = %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("handler calls = %d", calls.Load())
	}
}

func TestMiddlewareReturnsSettlementRelevantOutcomes(t *testing.T) {
	middleware, store := fixture(t)
	handler := func(context.Context, idempotencyqueue.Message) error {
		t.Fatal("handler executed")
		return nil
	}
	message := taskMessage{id: "delivery-3", payload: []byte("payload")}
	key, fingerprint := identity(t, message)
	if _, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	}); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := middleware.Handle(context.Background(), message, handler); !errors.Is(err, idempotencyqueue.ErrInProgress) {
		t.Fatalf("Handle() in-progress error = %v", err)
	}

	conflict := taskMessage{id: "delivery-3", payload: []byte("different")}
	if err := middleware.Handle(context.Background(), conflict, handler); !errors.Is(err, idempotencyqueue.ErrConflict) {
		t.Fatalf("Handle() conflict error = %v", err)
	}
}

func TestWrapReturnsGoQueueCompatibleTypedHandler(t *testing.T) {
	middleware, _ := fixture(t)
	var payload string
	handler := idempotencyqueue.Wrap(middleware, func(_ context.Context, message taskMessage) error {
		payload = string(message.Payload())
		return nil
	})
	if err := invokeTaskHandler(
		context.Background(),
		handler,
		taskMessage{id: "delivery-4", payload: []byte("wrapped")},
	); err != nil {
		t.Fatalf("wrapped handler error = %v", err)
	}
	if payload != "wrapped" {
		t.Fatalf("payload = %q", payload)
	}
}

func invokeTaskHandler(
	ctx context.Context,
	handler func(context.Context, taskMessage) error,
	message taskMessage,
) error {
	return handler(ctx, message)
}

func TestNewValidatesConfiguration(t *testing.T) {
	service := mustService(t, mustStore(t))
	key := validKey(t)
	fingerprint := validFingerprint(t)
	valid := idempotencyqueue.Options{
		Service: service, Lease: time.Minute, Key: key, Fingerprint: fingerprint,
	}
	tests := map[string]idempotencyqueue.Options{
		"service":    {Lease: time.Minute, Key: key, Fingerprint: fingerprint},
		"lease zero": {Service: service, Key: key, Fingerprint: fingerprint},
		"lease too long": {
			Service: service, Lease: idempotency.MaxLease + 1, Key: key, Fingerprint: fingerprint,
		},
		"key":                {Service: service, Lease: time.Minute, Fingerprint: fingerprint},
		"fingerprint":        {Service: service, Lease: time.Minute, Key: key},
		"transition timeout": withTimeout(valid, -1),
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := idempotencyqueue.New(options); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
	if _, err := idempotencyqueue.New(valid); err != nil {
		t.Fatalf("New() default timeout error = %v", err)
	}
}

func TestMiddlewareValidatesMessageAndIdentity(t *testing.T) {
	backendErr := errors.New("identity failed")
	message := taskMessage{id: "delivery", payload: []byte("payload")}
	tests := map[string]struct {
		message     idempotencyqueue.Message
		handler     idempotencyqueue.Handler
		key         idempotencyqueue.KeyFunc
		fingerprint idempotencyqueue.FingerprintFunc
	}{
		"message": {handler: validHandler, key: validKey(t), fingerprint: validFingerprint(t)},
		"handler": {message: message, key: validKey(t), fingerprint: validFingerprint(t)},
		"key": {
			message: message, handler: validHandler,
			key: func(context.Context, idempotencyqueue.Message) (idempotency.Key, error) {
				return idempotency.Key{}, backendErr
			},
			fingerprint: validFingerprint(t),
		},
		"fingerprint": {
			message: message, handler: validHandler, key: validKey(t),
			fingerprint: func(idempotencyqueue.Message) (idempotency.Fingerprint, error) {
				return idempotency.Fingerprint{}, backendErr
			},
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			middleware := mustMiddleware(t, mustStore(t), test.key, test.fingerprint)
			if err := middleware.Handle(context.Background(), test.message, test.handler); err == nil {
				t.Fatal("Handle() error = nil")
			}
		})
	}
}

func TestMiddlewareReturnsTerminalFailureWithoutExecuting(t *testing.T) {
	middleware, store := fixture(t)
	message := taskMessage{id: "terminal", payload: []byte("payload")}
	key, fingerprint := identity(t, message)
	acquired, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if _, err := store.Fail(context.Background(), idempotency.FailRequest{
		Ownership: acquired.Record.Ownership(), Result: []byte("terminal"),
	}); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	if err := middleware.Handle(context.Background(), message, func(context.Context, idempotencyqueue.Message) error {
		t.Fatal("handler executed")
		return nil
	}); !errors.Is(err, idempotencyqueue.ErrTerminalFailure) {
		t.Fatalf("Handle() error = %v", err)
	}
}

func TestMiddlewarePropagatesStorageFailures(t *testing.T) {
	backendErr := errors.New("backend failed")
	message := taskMessage{id: "storage", payload: []byte("payload")}
	key, fingerprint := identity(t, message)
	record := idempotency.Record{
		Key: key, Fingerprint: fingerprint, State: idempotency.StateAcquired,
		OwnerToken: "owner", FencingToken: 1,
	}
	tests := map[string]*storeOverride{
		"acquire": {
			Store: mustStore(t),
			acquire: func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
				return idempotency.AcquireResult{}, backendErr
			},
		},
		"complete": {
			Store: mustStore(t), acquire: acquired(record),
			complete: func(context.Context, idempotency.CompleteRequest) (idempotency.Record, error) {
				return idempotency.Record{}, backendErr
			},
		},
	}
	for name, store := range tests {
		t.Run(name, func(t *testing.T) {
			middleware := mustMiddleware(t, store, validKey(t), validFingerprint(t))
			if err := middleware.Handle(context.Background(), message, validHandler); !errors.Is(err, backendErr) {
				t.Fatalf("Handle() error = %v", err)
			}
		})
	}
}

func TestMiddlewareUsesFreshContextToReleaseAfterHandlerFailure(t *testing.T) {
	base := mustStore(t)
	var releaseContextError error
	store := &storeOverride{Store: base}
	store.release = func(ctx context.Context, ownership idempotency.Ownership) (idempotency.Record, error) {
		releaseContextError = ctx.Err()
		return base.Release(ctx, ownership)
	}
	middleware := mustMiddleware(t, store, validKey(t), validFingerprint(t))
	ctx, cancel := context.WithCancel(context.Background())
	handlerErr := errors.New("handler failed")
	err := middleware.Handle(ctx, taskMessage{id: "canceled", payload: []byte("payload")}, func(
		context.Context, idempotencyqueue.Message,
	) error {
		cancel()
		return handlerErr
	})
	if !errors.Is(err, handlerErr) || releaseContextError != nil {
		t.Fatalf("Handle() error = %v, release context error = %v", err, releaseContextError)
	}
}

func TestMiddlewareJoinsReleaseFailureAndPropagatesPanics(t *testing.T) {
	releaseErr := errors.New("release failed")
	store := &storeOverride{Store: mustStore(t)}
	store.release = func(context.Context, idempotency.Ownership) (idempotency.Record, error) {
		return idempotency.Record{}, releaseErr
	}
	middleware := mustMiddleware(t, store, validKey(t), validFingerprint(t))
	handlerErr := errors.New("handler failed")
	err := middleware.Handle(context.Background(), taskMessage{id: "join", payload: []byte("payload")}, func(
		context.Context, idempotencyqueue.Message,
	) error {
		return handlerErr
	})
	if !errors.Is(err, handlerErr) || !errors.Is(err, releaseErr) {
		t.Fatalf("Handle() joined error = %v", err)
	}

	defer func() {
		if recover() != "queue panic" {
			t.Fatal("panic was not propagated")
		}
	}()
	_ = middleware.Handle(context.Background(), taskMessage{id: "panic", payload: []byte("payload")}, func(
		context.Context, idempotencyqueue.Message,
	) error {
		panic("queue panic")
	})
}

func fixture(t *testing.T) (*idempotencyqueue.Middleware, *memory.Store) {
	t.Helper()
	store, err := memory.New(memory.Options{
		Clock:       idempotencytest.NewClock(time.Unix(1_700_000_000, 0).UTC()),
		OwnerTokens: idempotencytest.NewTokenSource("queue-owner").Next,
	})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	service, err := idempotency.NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	middleware, err := idempotencyqueue.New(idempotencyqueue.Options{
		Service: service, Lease: time.Minute,
		Key: func(_ context.Context, message idempotencyqueue.Message) (idempotency.Key, error) {
			key, _ := identity(t, message.(taskMessage))
			return key, nil
		},
		Fingerprint: func(message idempotencyqueue.Message) (idempotency.Fingerprint, error) {
			_, fingerprint := identity(t, message.(taskMessage))
			return fingerprint, nil
		},
	})
	if err != nil {
		t.Fatalf("idempotencyqueue.New() error = %v", err)
	}
	return middleware, store
}

func identity(t *testing.T, message taskMessage) (idempotency.Key, idempotency.Fingerprint) {
	t.Helper()
	key, err := idempotency.NewKey("queue", "tenant", "widgets.consume", "consumer", message.id)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("queue-v1", message.Payload())
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	return key, fingerprint
}

type taskMessage struct {
	id      string
	payload []byte
}

func (m taskMessage) Payload() []byte { return m.payload }

func mustStore(t *testing.T) *memory.Store {
	t.Helper()
	store, err := memory.New(memory.Options{
		Clock:       idempotencytest.NewClock(time.Unix(1_700_000_000, 0).UTC()),
		OwnerTokens: idempotencytest.NewTokenSource("queue-owner").Next,
	})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	return store
}

func mustService(t *testing.T, store idempotency.Store) *idempotency.Service {
	t.Helper()
	service, err := idempotency.NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return service
}

func mustMiddleware(
	t *testing.T,
	store idempotency.Store,
	key idempotencyqueue.KeyFunc,
	fingerprint idempotencyqueue.FingerprintFunc,
) *idempotencyqueue.Middleware {
	t.Helper()
	middleware, err := idempotencyqueue.New(idempotencyqueue.Options{
		Service: mustService(t, store), Lease: time.Minute,
		Key: key, Fingerprint: fingerprint,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return middleware
}

func validKey(t *testing.T) idempotencyqueue.KeyFunc {
	t.Helper()
	return func(_ context.Context, message idempotencyqueue.Message) (idempotency.Key, error) {
		key, _ := identity(t, message.(taskMessage))
		return key, nil
	}
}

func validFingerprint(t *testing.T) idempotencyqueue.FingerprintFunc {
	t.Helper()
	return func(message idempotencyqueue.Message) (idempotency.Fingerprint, error) {
		_, fingerprint := identity(t, message.(taskMessage))
		return fingerprint, nil
	}
}

func validHandler(context.Context, idempotencyqueue.Message) error { return nil }

func withTimeout(options idempotencyqueue.Options, timeout time.Duration) idempotencyqueue.Options {
	options.TransitionTimeout = timeout
	return options
}

func acquired(record idempotency.Record) func(
	context.Context,
	idempotency.AcquireRequest,
) (idempotency.AcquireResult, error) {
	return func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error) {
		return idempotency.AcquireResult{Outcome: idempotency.OutcomeAcquired, Record: record}, nil
	}
}

type storeOverride struct {
	idempotency.Store
	acquire  func(context.Context, idempotency.AcquireRequest) (idempotency.AcquireResult, error)
	complete func(context.Context, idempotency.CompleteRequest) (idempotency.Record, error)
	release  func(context.Context, idempotency.Ownership) (idempotency.Record, error)
}

func (s *storeOverride) Acquire(
	ctx context.Context,
	request idempotency.AcquireRequest,
) (idempotency.AcquireResult, error) {
	if s.acquire != nil {
		return s.acquire(ctx, request)
	}
	return s.Store.Acquire(ctx, request)
}

func (s *storeOverride) Complete(
	ctx context.Context,
	request idempotency.CompleteRequest,
) (idempotency.Record, error) {
	if s.complete != nil {
		return s.complete(ctx, request)
	}
	return s.Store.Complete(ctx, request)
}

func (s *storeOverride) Release(
	ctx context.Context,
	ownership idempotency.Ownership,
) (idempotency.Record, error) {
	if s.release != nil {
		return s.release(ctx, ownership)
	}
	return s.Store.Release(ctx, ownership)
}
