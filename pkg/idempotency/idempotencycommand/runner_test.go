package idempotencycommand_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencycommand"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
)

func TestRunnerExecutesOnceAndReplaysResult(t *testing.T) {
	runner, _ := fixture(t)
	request := commandRequest(t, "row-42", "payload")
	var calls atomic.Int64
	handler := func(ctx context.Context) ([]byte, map[string]string, error) {
		calls.Add(1)
		ownership, found := idempotency.OwnershipFromContext(ctx)
		if !found || ownership.FencingToken != 1 {
			t.Fatalf("handler ownership = %#v, %t", ownership, found)
		}
		return []byte("created:42"), map[string]string{"kind": "widget"}, nil
	}
	first, err := runner.Run(context.Background(), request, handler)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	second, err := runner.Run(context.Background(), request, handler)
	if err != nil {
		t.Fatalf("Run() replay error = %v", err)
	}
	if calls.Load() != 1 || first.Outcome != idempotency.OutcomeAcquired ||
		second.Outcome != idempotency.OutcomeReplayed || !second.Replayed ||
		string(second.Result) != "created:42" || second.Metadata["kind"] != "widget" {
		t.Fatalf("calls = %d, results = %#v, %#v", calls.Load(), first, second)
	}
}

func TestRunnerReleasesFailedCommandForRetry(t *testing.T) {
	runner, _ := fixture(t)
	request := commandRequest(t, "row-43", "payload")
	handlerErr := errors.New("retry row")
	var calls atomic.Int64
	handler := func(context.Context) ([]byte, map[string]string, error) {
		if calls.Add(1) == 1 {
			return nil, nil, handlerErr
		}
		return []byte("ok"), nil, nil
	}
	if _, err := runner.Run(context.Background(), request, handler); !errors.Is(err, handlerErr) {
		t.Fatalf("Run() first error = %v", err)
	}
	if _, err := runner.Run(context.Background(), request, handler); err != nil {
		t.Fatalf("Run() retry error = %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("handler calls = %d", calls.Load())
	}
}

func TestRunnerReturnsConflictAndInProgress(t *testing.T) {
	runner, store := fixture(t)
	request := commandRequest(t, "row-44", "payload")
	key, err := idempotency.NewKey(
		request.Namespace, request.Tenant, request.Name, request.Caller, request.SourceID,
	)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	if _, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: request.Fingerprint, Lease: time.Minute,
	}); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	handler := func(context.Context) ([]byte, map[string]string, error) {
		t.Fatal("handler executed")
		return nil, nil, nil
	}
	if _, err := runner.Run(context.Background(), request, handler); !errors.Is(err, idempotencycommand.ErrInProgress) {
		t.Fatalf("Run() in-progress error = %v", err)
	}
	request.Fingerprint, _ = idempotency.NewFingerprint("command-v1", []byte("different"))
	if _, err := runner.Run(context.Background(), request, handler); !errors.Is(err, idempotencycommand.ErrConflict) {
		t.Fatalf("Run() conflict error = %v", err)
	}
}

func TestNewValidatesConfiguration(t *testing.T) {
	service := mustService(t, mustStore(t))
	valid := idempotencycommand.Options{Service: service, Lease: time.Minute}
	tests := map[string]idempotencycommand.Options{
		"service":            {},
		"lease zero":         {Service: service},
		"lease too long":     {Service: service, Lease: idempotency.MaxLease + 1},
		"transition timeout": withTimeout(valid, -1),
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := idempotencycommand.New(options); err == nil {
				t.Fatal("New() error = nil")
			}
		})
	}
	if _, err := idempotencycommand.New(valid); err != nil {
		t.Fatalf("New() default timeout error = %v", err)
	}
}

func TestRunnerValidatesHandlerAndKey(t *testing.T) {
	runner, _ := fixture(t)
	request := commandRequest(t, "source", "payload")
	if _, err := runner.Run(context.Background(), request, nil); err == nil {
		t.Fatal("Run() nil handler error = nil")
	}
	request.SourceID = ""
	if _, err := runner.Run(context.Background(), request, func(context.Context) ([]byte, map[string]string, error) {
		return nil, nil, nil
	}); err == nil {
		t.Fatal("Run() invalid key error = nil")
	}
}

func TestRunnerReturnsPersistedTerminalFailure(t *testing.T) {
	runner, store := fixture(t)
	request := commandRequest(t, "terminal", "payload")
	key, err := idempotency.NewKey(
		request.Namespace, request.Tenant, request.Name, request.Caller, request.SourceID,
	)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	acquired, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: request.Fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if _, err := store.Fail(context.Background(), idempotency.FailRequest{
		Ownership: acquired.Record.Ownership(), Result: []byte("rejected"),
		Metadata: map[string]string{"reason": "invalid"},
	}); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	result, err := runner.Run(context.Background(), request, func(context.Context) ([]byte, map[string]string, error) {
		t.Fatal("handler executed")
		return nil, nil, nil
	})
	if !errors.Is(err, idempotencycommand.ErrTerminalFailure) ||
		result.Outcome != idempotency.OutcomeTerminalFailure || !result.Replayed ||
		string(result.Result) != "rejected" || result.Metadata["reason"] != "invalid" {
		t.Fatalf("Run() = %#v, %v", result, err)
	}
}

func TestRunnerPropagatesStorageFailures(t *testing.T) {
	request := commandRequest(t, "storage", "payload")
	key, err := idempotency.NewKey(
		request.Namespace, request.Tenant, request.Name, request.Caller, request.SourceID,
	)
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	record := idempotency.Record{
		Key: key, Fingerprint: request.Fingerprint, State: idempotency.StateAcquired,
		OwnerToken: "owner", FencingToken: 1,
	}
	backendErr := errors.New("backend failed")
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
			runner := mustRunner(t, store)
			if _, err := runner.Run(context.Background(), request, func(context.Context) ([]byte, map[string]string, error) {
				return []byte("ok"), nil, nil
			}); !errors.Is(err, backendErr) {
				t.Fatalf("Run() error = %v", err)
			}
		})
	}
}

func TestRunnerUsesFreshReleaseContextAndJoinsFailures(t *testing.T) {
	base := mustStore(t)
	var releaseContextError error
	store := &storeOverride{Store: base}
	store.release = func(ctx context.Context, ownership idempotency.Ownership) (idempotency.Record, error) {
		releaseContextError = ctx.Err()
		return base.Release(ctx, ownership)
	}
	runner := mustRunner(t, store)
	ctx, cancel := context.WithCancel(context.Background())
	handlerErr := errors.New("handler failed")
	_, err := runner.Run(ctx, commandRequest(t, "canceled", "payload"), func(context.Context) ([]byte, map[string]string, error) {
		cancel()
		return nil, nil, handlerErr
	})
	if !errors.Is(err, handlerErr) || releaseContextError != nil {
		t.Fatalf("Run() error = %v, release context error = %v", err, releaseContextError)
	}

	releaseErr := errors.New("release failed")
	store = &storeOverride{Store: mustStore(t)}
	store.release = func(context.Context, idempotency.Ownership) (idempotency.Record, error) {
		return idempotency.Record{}, releaseErr
	}
	runner = mustRunner(t, store)
	_, err = runner.Run(context.Background(), commandRequest(t, "joined", "payload"), func(context.Context) ([]byte, map[string]string, error) {
		return nil, nil, handlerErr
	})
	if !errors.Is(err, handlerErr) || !errors.Is(err, releaseErr) {
		t.Fatalf("Run() joined error = %v", err)
	}
}

func TestRunnerReleasesAndPropagatesHandlerPanic(t *testing.T) {
	runner, store := fixture(t)
	request := commandRequest(t, "panic", "payload")
	defer func() {
		if recover() != "command panic" {
			t.Fatal("panic was not propagated")
		}
		key, err := idempotency.NewKey(
			request.Namespace, request.Tenant, request.Name, request.Caller, request.SourceID,
		)
		if err != nil {
			t.Fatalf("NewKey() error = %v", err)
		}
		record, err := store.Inspect(context.Background(), key)
		if err != nil || record.State != idempotency.StateAbandoned {
			t.Fatalf("Inspect() = %#v, %v", record, err)
		}
	}()
	_, _ = runner.Run(context.Background(), request, func(context.Context) ([]byte, map[string]string, error) {
		panic("command panic")
	})
}

func fixture(t *testing.T) (*idempotencycommand.Runner, *memory.Store) {
	t.Helper()
	store, err := memory.New(memory.Options{
		Clock:       idempotencytest.NewClock(time.Unix(1_700_000_000, 0).UTC()),
		OwnerTokens: idempotencytest.NewTokenSource("command-owner").Next,
	})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	service, err := idempotency.NewService(store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	runner, err := idempotencycommand.New(idempotencycommand.Options{
		Service: service, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("idempotencycommand.New() error = %v", err)
	}
	return runner, store
}

func commandRequest(t *testing.T, sourceID, payload string) idempotencycommand.Request {
	t.Helper()
	fingerprint, err := idempotency.NewFingerprint("command-v1", []byte(payload))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	return idempotencycommand.Request{
		Namespace: "imports", Tenant: "tenant", Name: "widgets.import",
		Caller: "nightly-import", SourceID: sourceID, Fingerprint: fingerprint,
	}
}

func mustStore(t *testing.T) *memory.Store {
	t.Helper()
	store, err := memory.New(memory.Options{
		Clock:       idempotencytest.NewClock(time.Unix(1_700_000_000, 0).UTC()),
		OwnerTokens: idempotencytest.NewTokenSource("command-owner").Next,
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

func mustRunner(t *testing.T, store idempotency.Store) *idempotencycommand.Runner {
	t.Helper()
	runner, err := idempotencycommand.New(idempotencycommand.Options{
		Service: mustService(t, store), Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return runner
}

func withTimeout(options idempotencycommand.Options, timeout time.Duration) idempotencycommand.Options {
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
