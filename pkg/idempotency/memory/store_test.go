package memory_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/memory"
)

func TestConcurrentAcquireElectsOneOwner(t *testing.T) {
	t.Parallel()

	store, key, fingerprint, _ := fixture(t)
	const callers = 32

	start := make(chan struct{})
	results := make(chan idempotency.AcquireResult, callers)
	errors := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
				Key:         key,
				Fingerprint: fingerprint,
				Lease:       time.Minute,
			})
			results <- result
			errors <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
	}

	acquired := 0
	inProgress := 0
	for result := range results {
		switch result.Outcome {
		case idempotency.OutcomeAcquired:
			acquired++
			if result.Record.FencingToken != 1 || result.Record.Attempt != 1 {
				t.Fatalf("first owner record = %#v", result.Record)
			}
		case idempotency.OutcomeInProgress:
			inProgress++
		default:
			t.Fatalf("Acquire() outcome = %q", result.Outcome)
		}
	}
	if acquired != 1 || inProgress != callers-1 {
		t.Fatalf("outcomes: acquired=%d in_progress=%d", acquired, inProgress)
	}
}

func TestAcquireConflictsBeforeReplaying(t *testing.T) {
	t.Parallel()

	store, key, fingerprint, _ := fixture(t)
	owner := acquire(t, store, key, fingerprint)
	completed, err := store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: owner.Record.Ownership(),
		Result:    []byte("created"),
		Metadata:  map[string]string{"content-type": "application/json"},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if completed.State != idempotency.StateCompleted {
		t.Fatalf("Complete() state = %q", completed.State)
	}

	replay, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() replay error = %v", err)
	}
	if replay.Outcome != idempotency.OutcomeReplayed ||
		string(replay.Record.Result) != "created" ||
		replay.Record.Metadata["content-type"] != "application/json" {
		t.Fatalf("Acquire() replay = %#v", replay)
	}

	other, err := idempotency.NewFingerprint("v1", []byte("different"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	conflict, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: other, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() conflict error = %v", err)
	}
	if conflict.Outcome != idempotency.OutcomeConflict {
		t.Fatalf("Acquire() conflict outcome = %q", conflict.Outcome)
	}
}

func TestExpiredLeaseTakeoverFencesPreviousOwner(t *testing.T) {
	t.Parallel()

	store, key, fingerprint, clock := fixture(t)
	first := acquire(t, store, key, fingerprint)
	clock.Advance(time.Minute)

	second, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() takeover error = %v", err)
	}
	if second.Outcome != idempotency.OutcomeStaleOwnerTakeover ||
		second.Record.FencingToken != 2 || second.Record.Attempt != 2 ||
		second.Record.OwnerToken == first.Record.OwnerToken {
		t.Fatalf("Acquire() takeover = %#v", second)
	}

	_, err = store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: first.Record.Ownership(), Result: []byte("stale"),
	})
	assertReason(t, err, idempotency.ReasonStaleOwner)

	completed, err := store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: second.Record.Ownership(), Result: []byte("current"),
	})
	if err != nil {
		t.Fatalf("Complete() current owner error = %v", err)
	}
	if string(completed.Result) != "current" {
		t.Fatalf("Complete() result = %q", completed.Result)
	}
}

func TestHeartbeatExtendsLeaseAndRejectsLateOwner(t *testing.T) {
	t.Parallel()

	store, key, fingerprint, clock := fixture(t)
	owner := acquire(t, store, key, fingerprint)
	clock.Advance(30 * time.Second)

	record, err := store.Heartbeat(context.Background(), idempotency.HeartbeatRequest{
		Ownership: owner.Record.Ownership(),
		Lease:     2 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Heartbeat() error = %v", err)
	}
	if !record.LeaseExpiresAt.Equal(clock.Now().Add(2*time.Minute)) ||
		!record.HeartbeatAt.Equal(clock.Now()) || record.State != idempotency.StateRunning {
		t.Fatalf("Heartbeat() record = %#v", record)
	}

	clock.Advance(2 * time.Minute)
	_, err = store.Heartbeat(context.Background(), idempotency.HeartbeatRequest{
		Ownership: owner.Record.Ownership(), Lease: time.Minute,
	})
	assertReason(t, err, idempotency.ReasonLeaseExpired)
}

func TestFailReleaseExpireAndInspectTransitions(t *testing.T) {
	t.Parallel()

	t.Run("fail is terminal and replayed as terminal failure", func(t *testing.T) {
		t.Parallel()
		store, key, fingerprint, _ := fixture(t)
		owner := acquire(t, store, key, fingerprint)

		failed, err := store.Fail(context.Background(), idempotency.FailRequest{
			Ownership: owner.Record.Ownership(),
			Result:    []byte("declined"),
		})
		if err != nil || failed.State != idempotency.StateFailed {
			t.Fatalf("Fail() = %#v, %v", failed, err)
		}
		result, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: key, Fingerprint: fingerprint, Lease: time.Minute,
		})
		if err != nil || result.Outcome != idempotency.OutcomeTerminalFailure {
			t.Fatalf("Acquire() after fail = %#v, %v", result, err)
		}
	})

	t.Run("release abandons ownership and permits a new attempt", func(t *testing.T) {
		t.Parallel()
		store, key, fingerprint, _ := fixture(t)
		owner := acquire(t, store, key, fingerprint)

		abandoned, err := store.Release(context.Background(), owner.Record.Ownership())
		if err != nil || abandoned.State != idempotency.StateAbandoned {
			t.Fatalf("Release() = %#v, %v", abandoned, err)
		}
		result, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: key, Fingerprint: fingerprint, Lease: time.Minute,
		})
		if err != nil || result.Outcome != idempotency.OutcomeAcquired ||
			result.Record.Attempt != 2 || result.Record.FencingToken != 2 {
			t.Fatalf("Acquire() after release = %#v, %v", result, err)
		}
	})

	t.Run("expire marks elapsed active records", func(t *testing.T) {
		t.Parallel()
		store, key, fingerprint, clock := fixture(t)
		owner := acquire(t, store, key, fingerprint)
		clock.Advance(time.Minute)

		expired, err := store.Expire(context.Background(), key)
		if err != nil || expired.State != idempotency.StateExpired {
			t.Fatalf("Expire() = %#v, %v", expired, err)
		}
		inspected, err := store.Inspect(context.Background(), key)
		if err != nil || inspected.State != idempotency.StateExpired ||
			inspected.OwnerToken != owner.Record.OwnerToken {
			t.Fatalf("Inspect() = %#v, %v", inspected, err)
		}
	})
}

func TestOperationsRejectMissingStaleAndTerminalOwnership(t *testing.T) {
	t.Parallel()

	store, key, fingerprint, _ := fixture(t)
	missingKey, err := idempotency.NewKey("billing", "tenant", "charge", "api", "missing")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	_, err = store.Inspect(context.Background(), missingKey)
	assertReason(t, err, idempotency.ReasonNotFound)
	_, err = store.Expire(context.Background(), missingKey)
	assertReason(t, err, idempotency.ReasonNotFound)
	_, err = store.Fail(context.Background(), idempotency.FailRequest{
		Ownership: idempotency.Ownership{Key: missingKey},
	})
	assertReason(t, err, idempotency.ReasonNotFound)

	owner := acquire(t, store, key, fingerprint)
	stale := owner.Record.Ownership()
	stale.OwnerToken = "wrong-owner"
	_, err = store.Fail(context.Background(), idempotency.FailRequest{Ownership: stale})
	assertReason(t, err, idempotency.ReasonStaleOwner)

	_, err = store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: owner.Record.Ownership(),
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	_, err = store.Release(context.Background(), owner.Record.Ownership())
	assertReason(t, err, idempotency.ReasonInvalidTransition)
}

func TestAcquireFailsClosedWhenOwnerTokenCannotBeCreated(t *testing.T) {
	t.Parallel()

	key, err := idempotency.NewKey("billing", "tenant", "charge", "api", "request")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("request"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	clock := &fakeClock{now: time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)}
	store, err := memory.New(memory.Options{
		Clock: clock,
		OwnerTokens: func() (string, error) {
			return "", errors.New("entropy unavailable")
		},
	})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}

	_, err = store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	assertReason(t, err, idempotency.ReasonUnavailable)
}

func TestStoreBoundsOwnerTokensAndRecordCount(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("request"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	firstKey, err := idempotency.NewKey("memory", "tenant", "bound", "caller", "first")
	if err != nil {
		t.Fatalf("NewKey(first) error = %v", err)
	}
	secondKey, err := idempotency.NewKey("memory", "tenant", "bound", "caller", "second")
	if err != nil {
		t.Fatalf("NewKey(second) error = %v", err)
	}

	t.Run("owner token", func(t *testing.T) {
		store, err := memory.New(memory.Options{
			Clock: clock,
			OwnerTokens: func() (string, error) {
				return strings.Repeat("o", idempotency.MaxOwnerTokenBytes+1), nil
			},
			MaxRecords: 1,
		})
		if err != nil {
			t.Fatalf("memory.New() error = %v", err)
		}
		_, err = store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: firstKey, Fingerprint: fingerprint, Lease: time.Minute,
		})
		assertReason(t, err, idempotency.ReasonUnavailable)
		_, err = store.Inspect(context.Background(), firstKey)
		assertReason(t, err, idempotency.ReasonNotFound)
	})

	t.Run("record capacity", func(t *testing.T) {
		tokens := &tokenSource{}
		store, err := memory.New(memory.Options{
			Clock: clock, OwnerTokens: tokens.Next, MaxRecords: 1,
		})
		if err != nil {
			t.Fatalf("memory.New() error = %v", err)
		}
		first, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: firstKey, Fingerprint: fingerprint, Lease: time.Minute,
		})
		if err != nil || first.Outcome != idempotency.OutcomeAcquired {
			t.Fatalf("Acquire(first) = %#v, %v", first, err)
		}
		_, err = store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: secondKey, Fingerprint: fingerprint, Lease: time.Minute,
		})
		assertReason(t, err, idempotency.ReasonLimitExceeded)
		_, err = store.Inspect(context.Background(), secondKey)
		assertReason(t, err, idempotency.ReasonNotFound)
		retry, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: firstKey, Fingerprint: fingerprint, Lease: time.Minute,
		})
		if err != nil || retry.Outcome != idempotency.OutcomeInProgress {
			t.Fatalf("Acquire(existing) = %#v, %v", retry, err)
		}
	})
}

func TestStoreRejectsInvalidConfigurationAndInputs(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)}
	tokens := (&tokenSource{}).Next

	_, err := memory.New(memory.Options{OwnerTokens: tokens})
	assertReason(t, err, idempotency.ReasonInvalidConfiguration)
	_, err = memory.New(memory.Options{Clock: clock})
	assertReason(t, err, idempotency.ReasonInvalidConfiguration)
	_, err = memory.New(memory.Options{
		Clock: clock, OwnerTokens: tokens, MaxRecords: -1,
	})
	assertReason(t, err, idempotency.ReasonInvalidConfiguration)
	_, err = memory.New(memory.Options{
		Clock: clock, OwnerTokens: tokens, MaxRecords: memory.MaxRecordCapacity + 1,
	})
	assertReason(t, err, idempotency.ReasonInvalidConfiguration)

	store, key, fingerprint, _ := fixture(t)
	_, err = store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint,
	})
	assertReason(t, err, idempotency.ReasonInvalidLease)
	_, err = store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: idempotency.MaxLease + time.Nanosecond,
	})
	assertReason(t, err, idempotency.ReasonLimitExceeded)
}

func TestStoreBoundsResultsAndMetadata(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		result   []byte
		metadata map[string]string
	}{
		"result": {
			result: make([]byte, idempotency.MaxResultBytes+1),
		},
		"metadata entries": {
			metadata: func() map[string]string {
				metadata := make(map[string]string, idempotency.MaxMetadataEntries+1)
				for index := range idempotency.MaxMetadataEntries + 1 {
					metadata[fmt.Sprintf("key-%d", index)] = "value"
				}
				return metadata
			}(),
		},
		"metadata key": {
			metadata: map[string]string{string(make([]byte, idempotency.MaxMetadataKeyBytes+1)): "value"},
		},
		"metadata value": {
			metadata: map[string]string{"key": string(make([]byte, idempotency.MaxMetadataValueBytes+1))},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store, key, fingerprint, _ := fixture(t)
			owner := acquire(t, store, key, fingerprint)
			_, err := store.Complete(context.Background(), idempotency.CompleteRequest{
				Ownership: owner.Record.Ownership(),
				Result:    test.result,
				Metadata:  test.metadata,
			})
			assertReason(t, err, idempotency.ReasonLimitExceeded)
		})
	}

	t.Run("failed result", func(t *testing.T) {
		t.Parallel()

		store, key, fingerprint, _ := fixture(t)
		owner := acquire(t, store, key, fingerprint)
		_, err := store.Fail(context.Background(), idempotency.FailRequest{
			Ownership: owner.Record.Ownership(),
			Result:    make([]byte, idempotency.MaxResultBytes+1),
		})
		assertReason(t, err, idempotency.ReasonLimitExceeded)
	})
}

func TestExpireRejectsLiveAndTerminalRecords(t *testing.T) {
	t.Parallel()

	store, key, fingerprint, _ := fixture(t)
	owner := acquire(t, store, key, fingerprint)
	_, err := store.Expire(context.Background(), key)
	assertReason(t, err, idempotency.ReasonInvalidTransition)

	_, err = store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: owner.Record.Ownership(),
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	_, err = store.Expire(context.Background(), key)
	assertReason(t, err, idempotency.ReasonInvalidTransition)
}

func TestRecordsDoNotExposeMutableStoredData(t *testing.T) {
	t.Parallel()

	store, key, fingerprint, _ := fixture(t)
	owner := acquire(t, store, key, fingerprint)
	result := []byte("created")
	metadata := map[string]string{"content-type": "application/json"}
	completed, err := store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: owner.Record.Ownership(), Result: result, Metadata: metadata,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	result[0] = 'X'
	metadata["content-type"] = "mutated"
	completed.Result[1] = 'X'
	completed.Metadata["content-type"] = "also-mutated"

	inspected, err := store.Inspect(context.Background(), key)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if string(inspected.Result) != "created" ||
		inspected.Metadata["content-type"] != "application/json" {
		t.Fatalf("stored data was mutated: %#v", inspected)
	}
}

func TestCanceledOperationDoesNotAcquire(t *testing.T) {
	t.Parallel()

	store, key, fingerprint, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.Acquire(ctx, idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire() error = %v, want context.Canceled", err)
	}
	_, err = store.Inspect(context.Background(), key)
	assertReason(t, err, idempotency.ReasonNotFound)
}

func fixture(t *testing.T) (*memory.Store, idempotency.Key, idempotency.Fingerprint, *fakeClock) {
	t.Helper()

	key, err := idempotency.NewKey("billing", "tenant", "charge", "api", "request")
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("request"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	clock := &fakeClock{now: time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)}
	tokens := &tokenSource{}
	store, err := memory.New(memory.Options{Clock: clock, OwnerTokens: tokens.Next})
	if err != nil {
		t.Fatalf("memory.New() error = %v", err)
	}
	return store, key, fingerprint, clock
}

func acquire(t *testing.T, store *memory.Store, key idempotency.Key, fingerprint idempotency.Fingerprint) idempotency.AcquireResult {
	t.Helper()

	result, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if result.Outcome != idempotency.OutcomeAcquired {
		t.Fatalf("Acquire() outcome = %q", result.Outcome)
	}
	return result
}

func assertReason(t *testing.T, err error, want idempotency.Reason) {
	t.Helper()

	var semanticError *idempotency.Error
	if !errors.As(err, &semanticError) {
		t.Fatalf("error = %v, want *idempotency.Error", err)
	}
	if semanticError.Reason != want {
		t.Fatalf("reason = %q, want %q", semanticError.Reason, want)
	}
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(duration time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(duration)
}

type tokenSource struct {
	mu   sync.Mutex
	next uint64
}

func (s *tokenSource) Next() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	return fmt.Sprintf("owner-%d", s.next), nil
}
