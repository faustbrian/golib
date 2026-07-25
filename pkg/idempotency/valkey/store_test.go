package valkey

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

func TestNewStoreValidatesAdapterOptions(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{}
	tokens := func() (string, error) { return "owner", nil }
	tests := map[string]Options{
		"prefix missing":    {Retention: time.Hour, OwnerTokens: tokens},
		"prefix hash open":  {Prefix: "bad{prefix", Retention: time.Hour, OwnerTokens: tokens},
		"prefix hash close": {Prefix: "bad}prefix", Retention: time.Hour, OwnerTokens: tokens},
		"prefix oversized":  {Prefix: strings.Repeat("p", MaxPrefixBytes+1), Retention: time.Hour, OwnerTokens: tokens},
		"retention missing": {Prefix: "idempotency", OwnerTokens: tokens},
		"retention too long": {
			Prefix: "idempotency", Retention: MaxRetention + time.Nanosecond, OwnerTokens: tokens,
		},
		"tokens missing": {Prefix: "idempotency", Retention: time.Hour},
	}
	for name, options := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := newStore(executor, options)
			assertStoreReason(t, err, idempotency.ReasonInvalidConfiguration)
		})
	}

	_, err := newStore(nil, Options{
		Prefix: "idempotency", Retention: time.Hour, OwnerTokens: tokens,
	})
	assertStoreReason(t, err, idempotency.ReasonInvalidConfiguration)
}

func TestAcquireExecutesAtomicScriptAndDecodesResult(t *testing.T) {
	t.Parallel()

	want := testRecord(t)
	executor := &stubExecutor{reply: acquireReply(t, idempotency.OutcomeAcquired, want)}
	store := mustStore(t, executor)

	result, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: want.Key, Fingerprint: want.Fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if result.Outcome != idempotency.OutcomeAcquired || result.Record.OwnerToken != want.OwnerToken {
		t.Fatalf("Acquire() = %#v", result)
	}
	call := executor.singleCall(t)
	if call.operation != operationAcquire || call.key != recordKey("idempotency", want.Key) {
		t.Fatalf("Acquire() call = %#v", call)
	}
	if call.args[5] != "deterministic-owner" || call.args[6] != "60000" || call.args[7] != "3600000" {
		t.Fatalf("Acquire() args = %#v", call.args)
	}
}

func TestAcquireValidatesLeaseAndOwnerTokenBeforeExecuting(t *testing.T) {
	t.Parallel()

	want := testRecord(t)
	tests := map[string]struct {
		lease  time.Duration
		tokens func() (string, error)
		reason idempotency.Reason
	}{
		"zero": {
			tokens: func() (string, error) { return "owner", nil },
			reason: idempotency.ReasonInvalidLease,
		},
		"too long": {
			lease:  idempotency.MaxLease + time.Nanosecond,
			tokens: func() (string, error) { return "owner", nil },
			reason: idempotency.ReasonLimitExceeded,
		},
		"token error": {
			lease:  time.Minute,
			tokens: func() (string, error) { return "", errors.New("entropy unavailable") },
			reason: idempotency.ReasonUnavailable,
		},
		"empty token": {
			lease:  time.Minute,
			tokens: func() (string, error) { return "", nil },
			reason: idempotency.ReasonUnavailable,
		},
		"oversized token": {
			lease: time.Minute,
			tokens: func() (string, error) {
				return strings.Repeat("o", idempotency.MaxOwnerTokenBytes+1), nil
			},
			reason: idempotency.ReasonUnavailable,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			executor := &stubExecutor{}
			store, err := newStore(executor, Options{
				Prefix: "idempotency", Retention: time.Hour, OwnerTokens: test.tokens,
			})
			if err != nil {
				t.Fatalf("newStore() error = %v", err)
			}
			_, err = store.Acquire(context.Background(), idempotency.AcquireRequest{
				Key: want.Key, Fingerprint: want.Fingerprint, Lease: test.lease,
			})
			assertStoreReason(t, err, test.reason)
			if len(executor.calls) != 0 {
				t.Fatal("Acquire() executed a script for invalid input")
			}
		})
	}
}

func TestAcquirePropagatesCancellationBackendAndSemanticFailures(t *testing.T) {
	t.Parallel()

	want := testRecord(t)
	t.Run("canceled", func(t *testing.T) {
		t.Parallel()
		executor := &stubExecutor{}
		store := mustStore(t, executor)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := store.Acquire(ctx, idempotency.AcquireRequest{
			Key: want.Key, Fingerprint: want.Fingerprint, Lease: time.Minute,
		})
		if !errors.Is(err, context.Canceled) || len(executor.calls) != 0 {
			t.Fatalf("Acquire() error = %v, calls = %d", err, len(executor.calls))
		}
	})

	t.Run("backend", func(t *testing.T) {
		t.Parallel()
		backendErr := errors.New("connection lost")
		store := mustStore(t, &stubExecutor{err: backendErr})
		_, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: want.Key, Fingerprint: want.Fingerprint, Lease: time.Minute,
		})
		if !errors.Is(err, backendErr) {
			t.Fatalf("Acquire() error = %v", err)
		}
	})

	t.Run("semantic", func(t *testing.T) {
		t.Parallel()
		store := mustStore(t, &stubExecutor{
			reply: []string{"error", string(idempotency.ReasonInvalidTransition)},
		})
		_, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: want.Key, Fingerprint: want.Fingerprint, Lease: time.Minute,
		})
		assertStoreReason(t, err, idempotency.ReasonInvalidTransition)
	})
}

func TestStoreRoutesEveryRecordOperation(t *testing.T) {
	t.Parallel()

	want := testRecord(t)
	operations := map[operation]func(*Store) (idempotency.Record, error){
		operationInspect: func(store *Store) (idempotency.Record, error) {
			return store.Inspect(context.Background(), want.Key)
		},
		operationHeartbeat: func(store *Store) (idempotency.Record, error) {
			return store.Heartbeat(context.Background(), idempotency.HeartbeatRequest{
				Ownership: want.Ownership(), Lease: time.Minute,
			})
		},
		operationComplete: func(store *Store) (idempotency.Record, error) {
			return store.Complete(context.Background(), idempotency.CompleteRequest{
				Ownership: want.Ownership(), Result: want.Result, Metadata: want.Metadata,
			})
		},
		operationFail: func(store *Store) (idempotency.Record, error) {
			return store.Fail(context.Background(), idempotency.FailRequest{
				Ownership: want.Ownership(), Result: want.Result, Metadata: want.Metadata,
			})
		},
		operationRelease: func(store *Store) (idempotency.Record, error) {
			return store.Release(context.Background(), want.Ownership())
		},
		operationExpire: func(store *Store) (idempotency.Record, error) {
			return store.Expire(context.Background(), want.Key)
		},
	}
	for expected, execute := range operations {
		t.Run(string(expected), func(t *testing.T) {
			t.Parallel()
			executor := &stubExecutor{reply: recordReply(t, want)}
			store := mustStore(t, executor)
			record, err := execute(store)
			if err != nil {
				t.Fatalf("operation error = %v", err)
			}
			if record.Key != want.Key || record.State != want.State {
				t.Fatalf("record = %#v", record)
			}
			if call := executor.singleCall(t); call.operation != expected {
				t.Fatalf("operation = %q, want %q", call.operation, expected)
			}
		})
	}
}

func TestStorePropagatesBackendAndSemanticFailures(t *testing.T) {
	t.Parallel()

	want := testRecord(t)
	backendErr := errors.New("connection lost")
	store := mustStore(t, &stubExecutor{err: backendErr})
	_, err := store.Inspect(context.Background(), want.Key)
	if !errors.Is(err, backendErr) {
		t.Fatalf("Inspect() error = %v", err)
	}

	store = mustStore(t, &stubExecutor{reply: []string{"error", string(idempotency.ReasonNotFound)}})
	_, err = store.Inspect(context.Background(), want.Key)
	assertStoreReason(t, err, idempotency.ReasonNotFound)

	store = mustStore(t, &stubExecutor{reply: []string{"error", "future_reason"}})
	_, err = store.Inspect(context.Background(), want.Key)
	assertStoreReason(t, err, idempotency.ReasonInvalidPayload)

	store = mustStore(t, &stubExecutor{reply: []string{"ok", fieldSchema}})
	_, err = store.Inspect(context.Background(), want.Key)
	assertStoreReason(t, err, idempotency.ReasonInvalidPayload)
}

func TestCompleteAndFailRejectOversizedDataBeforeExecuting(t *testing.T) {
	t.Parallel()

	want := testRecord(t)
	for name, execute := range map[string]func(*Store) error{
		"complete": func(store *Store) error {
			_, err := store.Complete(context.Background(), idempotency.CompleteRequest{
				Ownership: want.Ownership(), Result: make([]byte, idempotency.MaxResultBytes+1),
			})
			return err
		},
		"fail": func(store *Store) error {
			_, err := store.Fail(context.Background(), idempotency.FailRequest{
				Ownership: want.Ownership(),
				Metadata:  map[string]string{"key": strings.Repeat("v", idempotency.MaxMetadataValueBytes+1)},
			})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			executor := &stubExecutor{}
			store := mustStore(t, executor)
			assertStoreReason(t, execute(store), idempotency.ReasonLimitExceeded)
			if len(executor.calls) != 0 {
				t.Fatal("operation executed a script for oversized input")
			}
		})
	}
}

func TestCanceledStoreCallDoesNotExecuteScript(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{}
	store := mustStore(t, executor)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := store.Inspect(ctx, testKey(t, "canceled"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Inspect() error = %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatal("Inspect() executed after cancellation")
	}
}

func TestHeartbeatRejectsInvalidLeaseBeforeExecuting(t *testing.T) {
	t.Parallel()

	executor := &stubExecutor{}
	store := mustStore(t, executor)
	_, err := store.Heartbeat(context.Background(), idempotency.HeartbeatRequest{
		Ownership: testRecord(t).Ownership(),
	})
	assertStoreReason(t, err, idempotency.ReasonInvalidLease)
	if len(executor.calls) != 0 {
		t.Fatal("Heartbeat() executed for an invalid lease")
	}
}

func TestStoreCheckDelegatesBackendSafetyValidation(t *testing.T) {
	t.Parallel()

	t.Run("safe", func(t *testing.T) {
		t.Parallel()
		executor := &stubExecutor{}
		store := mustStore(t, executor)
		if err := store.Check(context.Background()); err != nil {
			t.Fatalf("Check() error = %v", err)
		}
		if executor.checkCalls != 1 {
			t.Fatalf("Check() calls = %d", executor.checkCalls)
		}
	})

	t.Run("unsafe", func(t *testing.T) {
		t.Parallel()
		unsafeErr := &idempotency.Error{
			Reason: idempotency.ReasonUnsafeBackend,
			Field:  "maxmemory_policy",
		}
		executor := &stubExecutor{checkErr: unsafeErr}
		store := mustStore(t, executor)
		err := store.Check(context.Background())
		if !errors.Is(err, unsafeErr) {
			t.Fatalf("Check() error = %v", err)
		}
	})

	t.Run("canceled", func(t *testing.T) {
		t.Parallel()
		executor := &stubExecutor{}
		store := mustStore(t, executor)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := store.Check(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Check() error = %v", err)
		}
		if executor.checkCalls != 0 {
			t.Fatal("Check() reached the backend after cancellation")
		}
	})
}

func mustStore(t *testing.T, executor scriptExecutor) *Store {
	t.Helper()
	store, err := newStore(executor, Options{
		Prefix:      "idempotency",
		Retention:   time.Hour,
		OwnerTokens: func() (string, error) { return "deterministic-owner", nil },
	})
	if err != nil {
		t.Fatalf("newStore() error = %v", err)
	}
	return store
}

func acquireReply(t *testing.T, outcome idempotency.Outcome, record idempotency.Record) []string {
	t.Helper()
	reply := []string{string(outcome)}
	return append(reply, flatFields(t, record)...)
}

func recordReply(t *testing.T, record idempotency.Record) []string {
	t.Helper()
	return append([]string{"ok"}, flatFields(t, record)...)
}

func flatFields(t *testing.T, record idempotency.Record) []string {
	t.Helper()
	fields, err := encodeRecord(record)
	if err != nil {
		t.Fatalf("encodeRecord() error = %v", err)
	}
	flat := make([]string, 0, len(fields)*2)
	for key, value := range fields {
		flat = append(flat, key, value)
	}
	return flat
}

type scriptCall struct {
	operation operation
	key       string
	args      []string
}

type stubExecutor struct {
	reply      []string
	err        error
	checkErr   error
	checkCalls int
	calls      []scriptCall
}

func (e *stubExecutor) Exec(_ context.Context, operation operation, key string, args []string) ([]string, error) {
	e.calls = append(e.calls, scriptCall{operation: operation, key: key, args: append([]string(nil), args...)})
	return append([]string(nil), e.reply...), e.err
}

func (e *stubExecutor) Check(context.Context) error {
	e.checkCalls++
	return e.checkErr
}

func (e *stubExecutor) singleCall(t *testing.T) scriptCall {
	t.Helper()
	if len(e.calls) != 1 {
		t.Fatalf("script calls = %d, want 1", len(e.calls))
	}
	return e.calls[0]
}

func assertStoreReason(t *testing.T, err error, reason idempotency.Reason) {
	t.Helper()
	var semanticError *idempotency.Error
	if !errors.As(err, &semanticError) {
		t.Fatalf("error = %v, want *idempotency.Error", err)
	}
	if semanticError.Reason != reason {
		t.Fatalf("reason = %q, want %q", semanticError.Reason, reason)
	}
}
