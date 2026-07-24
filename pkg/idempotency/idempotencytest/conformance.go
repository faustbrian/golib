package idempotencytest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
)

// StoreFixture supplies an isolated backend and deterministic identities.
type StoreFixture struct {
	// Store is the backend instance under test.
	Store idempotency.Store
	// Key identifies the shared operation used by the conformance cases.
	Key idempotency.Key
	// Fingerprint identifies the shared business request used by the cases.
	Fingerprint idempotency.Fingerprint
	// Advance moves authoritative backend time when the fixture supports it.
	Advance func(time.Duration)
}

// StoreFactory constructs a fresh fixture for one conformance subtest.
type StoreFactory func(testing.TB) StoreFixture

// RunStoreConformance proves shared ownership, fencing, and replay behavior.
func RunStoreConformance(t *testing.T, factory StoreFactory) {
	t.Helper()

	t.Run("concurrent acquisition elects one owner", func(t *testing.T) {
		fixture := factory(t)
		const callers = 32
		start := make(chan struct{})
		results := make(chan idempotency.AcquireResult, callers)
		errorsFromStore := make(chan error, callers)
		var wait sync.WaitGroup
		for range callers {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				result, err := fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
					Key: fixture.Key, Fingerprint: fixture.Fingerprint, Lease: time.Minute,
				})
				results <- result
				errorsFromStore <- err
			}()
		}
		close(start)
		wait.Wait()
		close(results)
		close(errorsFromStore)

		for err := range errorsFromStore {
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
			case idempotency.OutcomeInProgress:
				inProgress++
			default:
				t.Fatalf("Acquire() outcome = %q", result.Outcome)
			}
		}
		if acquired != 1 || inProgress != callers-1 {
			t.Fatalf("outcomes: acquired=%d in_progress=%d", acquired, inProgress)
		}
	})

	t.Run("concurrent stale-owner takeover elects one replacement", func(t *testing.T) {
		fixture := factory(t)
		first := acquire(t, fixture)
		fixture.Advance(time.Minute)

		const callers = 32
		results := runConcurrently(callers, func(int) (idempotency.AcquireResult, error) {
			return fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
				Key: fixture.Key, Fingerprint: fixture.Fingerprint, Lease: time.Minute,
			})
		})
		takeovers := 0
		inProgress := 0
		for _, result := range results {
			mustNoError(t, result.err)
			switch result.value.Outcome {
			case idempotency.OutcomeStaleOwnerTakeover:
				takeovers++
				if result.value.Record.FencingToken != first.Record.FencingToken+1 {
					t.Fatalf("takeover fence = %d", result.value.Record.FencingToken)
				}
			case idempotency.OutcomeInProgress:
				inProgress++
			default:
				t.Fatalf("takeover outcome = %q", result.value.Outcome)
			}
		}
		if takeovers != 1 || inProgress != callers-1 {
			t.Fatalf("outcomes: takeover=%d in_progress=%d", takeovers, inProgress)
		}
	})

	t.Run("concurrent heartbeats preserve current ownership", func(t *testing.T) {
		fixture := factory(t)
		owner := acquire(t, fixture)

		const callers = 32
		results := runConcurrently(callers, func(int) (idempotency.Record, error) {
			return fixture.Store.Heartbeat(context.Background(), idempotency.HeartbeatRequest{
				Ownership: owner.Record.Ownership(), Lease: 2 * time.Minute,
			})
		})
		for _, result := range results {
			mustNoError(t, result.err)
			if result.value.State != idempotency.StateRunning ||
				result.value.OwnerToken != owner.Record.OwnerToken ||
				result.value.FencingToken != owner.Record.FencingToken {
				t.Fatalf("heartbeat record = %#v", result.value)
			}
		}
	})

	t.Run("concurrent completion permits one terminal transition", func(t *testing.T) {
		fixture := factory(t)
		owner := acquire(t, fixture)

		const callers = 32
		results := runConcurrently(callers, func(index int) (idempotency.Record, error) {
			return fixture.Store.Complete(context.Background(), idempotency.CompleteRequest{
				Ownership: owner.Record.Ownership(), Result: []byte{byte(index)},
			})
		})
		completed := 0
		rejected := 0
		var winner idempotency.Record
		for _, result := range results {
			if result.err == nil {
				completed++
				winner = result.value
				continue
			}
			var semantic *idempotency.Error
			if !errors.As(result.err, &semantic) ||
				semantic.Reason != idempotency.ReasonInvalidTransition {
				t.Fatalf("Complete() error = %v", result.err)
			}
			rejected++
		}
		if completed != 1 || rejected != callers-1 ||
			winner.State != idempotency.StateCompleted {
			t.Fatalf("outcomes: completed=%d rejected=%d winner=%#v", completed, rejected, winner)
		}
		inspected, err := fixture.Store.Inspect(context.Background(), fixture.Key)
		mustNoError(t, err)
		if inspected.State != idempotency.StateCompleted ||
			string(inspected.Result) != string(winner.Result) {
			t.Fatalf("Inspect() = %#v, winner = %#v", inspected, winner)
		}
	})

	t.Run("conflict takes precedence over replay", func(t *testing.T) {
		fixture := factory(t)
		owner := acquire(t, fixture)
		_, err := fixture.Store.Complete(context.Background(), idempotency.CompleteRequest{
			Ownership: owner.Record.Ownership(),
			Result:    []byte("created"),
			Metadata:  map[string]string{"content-type": "application/json"},
		})
		mustNoError(t, err)

		replay, err := fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: fixture.Key, Fingerprint: fixture.Fingerprint, Lease: time.Minute,
		})
		mustNoError(t, err)
		if replay.Outcome != idempotency.OutcomeReplayed || string(replay.Record.Result) != "created" {
			t.Fatalf("replay = %#v", replay)
		}

		other, err := idempotency.NewFingerprint("v1", []byte("different request"))
		mustNoError(t, err)
		conflict, err := fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: fixture.Key, Fingerprint: other, Lease: time.Minute,
		})
		mustNoError(t, err)
		if conflict.Outcome != idempotency.OutcomeConflict {
			t.Fatalf("conflict outcome = %q", conflict.Outcome)
		}
	})

	t.Run("takeover increments fence and rejects stale owner", func(t *testing.T) {
		fixture := factory(t)
		first := acquire(t, fixture)
		fixture.Advance(time.Minute)
		second, err := fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: fixture.Key, Fingerprint: fixture.Fingerprint, Lease: time.Minute,
		})
		mustNoError(t, err)
		if second.Outcome != idempotency.OutcomeStaleOwnerTakeover ||
			second.Record.FencingToken != first.Record.FencingToken+1 ||
			second.Record.Attempt != first.Record.Attempt+1 ||
			second.Record.OwnerToken == first.Record.OwnerToken {
			t.Fatalf("takeover = %#v after %#v", second, first)
		}
		_, err = fixture.Store.Complete(context.Background(), idempotency.CompleteRequest{
			Ownership: first.Record.Ownership(), Result: []byte("stale"),
		})
		mustReason(t, err, idempotency.ReasonStaleOwner)
		_, err = fixture.Store.Complete(context.Background(), idempotency.CompleteRequest{
			Ownership: second.Record.Ownership(), Result: []byte("current"),
		})
		mustNoError(t, err)
	})

	t.Run("takeover rejects every stale ownership mutation", func(t *testing.T) {
		fixture := factory(t)
		first := acquire(t, fixture)
		fixture.Advance(time.Minute)
		second, err := fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: fixture.Key, Fingerprint: fixture.Fingerprint, Lease: time.Minute,
		})
		mustNoError(t, err)
		if second.Outcome != idempotency.OutcomeStaleOwnerTakeover {
			t.Fatalf("takeover outcome = %q", second.Outcome)
		}

		assertOwnershipMutationsReason(
			t,
			fixture.Store,
			first.Record.Ownership(),
			idempotency.ReasonStaleOwner,
		)
		_, err = fixture.Store.Complete(context.Background(), idempotency.CompleteRequest{
			Ownership: second.Record.Ownership(), Result: []byte("current"),
		})
		mustNoError(t, err)
	})

	t.Run("heartbeat extends the exclusive lease boundary", func(t *testing.T) {
		fixture := factory(t)
		owner := acquire(t, fixture)
		fixture.Advance(30 * time.Second)
		_, err := fixture.Store.Heartbeat(context.Background(), idempotency.HeartbeatRequest{
			Ownership: owner.Record.Ownership(), Lease: 2 * time.Minute,
		})
		mustNoError(t, err)
		fixture.Advance(119 * time.Second)
		inProgress, err := fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: fixture.Key, Fingerprint: fixture.Fingerprint, Lease: time.Minute,
		})
		mustNoError(t, err)
		if inProgress.Outcome != idempotency.OutcomeInProgress {
			t.Fatalf("outcome before boundary = %q", inProgress.Outcome)
		}
		fixture.Advance(time.Second)
		takeover, err := fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: fixture.Key, Fingerprint: fixture.Fingerprint, Lease: time.Minute,
		})
		mustNoError(t, err)
		if takeover.Outcome != idempotency.OutcomeStaleOwnerTakeover {
			t.Fatalf("outcome at boundary = %q", takeover.Outcome)
		}
	})

	t.Run("terminal failure is replayed", func(t *testing.T) {
		fixture := factory(t)
		owner := acquire(t, fixture)
		_, err := fixture.Store.Fail(context.Background(), idempotency.FailRequest{
			Ownership: owner.Record.Ownership(), Result: []byte("declined"),
		})
		mustNoError(t, err)
		terminal, err := fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: fixture.Key, Fingerprint: fixture.Fingerprint, Lease: time.Minute,
		})
		mustNoError(t, err)
		if terminal.Outcome != idempotency.OutcomeTerminalFailure || string(terminal.Record.Result) != "declined" {
			t.Fatalf("terminal failure = %#v", terminal)
		}
	})

	t.Run("missing records reject every record operation", func(t *testing.T) {
		fixture := factory(t)
		ownership := idempotency.Ownership{
			Key: fixture.Key, OwnerToken: "missing-owner", FencingToken: 1,
		}
		_, err := fixture.Store.Inspect(context.Background(), fixture.Key)
		mustReason(t, err, idempotency.ReasonNotFound)
		assertOwnershipMutationsReason(
			t,
			fixture.Store,
			ownership,
			idempotency.ReasonNotFound,
		)
		_, err = fixture.Store.Expire(context.Background(), fixture.Key)
		mustReason(t, err, idempotency.ReasonNotFound)
	})

	t.Run("inactive and terminal states reject every mutation", func(t *testing.T) {
		states := map[string]func(testing.TB, StoreFixture, idempotency.Ownership){
			"completed": func(t testing.TB, fixture StoreFixture, ownership idempotency.Ownership) {
				_, err := fixture.Store.Complete(context.Background(), idempotency.CompleteRequest{
					Ownership: ownership, Result: []byte("completed"),
				})
				mustNoError(t, err)
			},
			"failed": func(t testing.TB, fixture StoreFixture, ownership idempotency.Ownership) {
				_, err := fixture.Store.Fail(context.Background(), idempotency.FailRequest{
					Ownership: ownership, Result: []byte("failed"),
				})
				mustNoError(t, err)
			},
			"abandoned": func(t testing.TB, fixture StoreFixture, ownership idempotency.Ownership) {
				_, err := fixture.Store.Release(context.Background(), ownership)
				mustNoError(t, err)
			},
			"expired": func(t testing.TB, fixture StoreFixture, _ idempotency.Ownership) {
				fixture.Advance(time.Minute)
				_, err := fixture.Store.Expire(context.Background(), fixture.Key)
				mustNoError(t, err)
			},
		}
		for name, enter := range states {
			t.Run(name, func(t *testing.T) {
				fixture := factory(t)
				owner := acquire(t, fixture)
				ownership := owner.Record.Ownership()
				enter(t, fixture, ownership)
				assertOwnershipMutationsReason(
					t,
					fixture.Store,
					ownership,
					idempotency.ReasonInvalidTransition,
				)
				_, err := fixture.Store.Expire(context.Background(), fixture.Key)
				mustReason(t, err, idempotency.ReasonInvalidTransition)
			})
		}
	})

	t.Run("live records reject explicit expiry", func(t *testing.T) {
		for _, heartbeat := range []bool{false, true} {
			name := "acquired"
			if heartbeat {
				name = "running"
			}
			t.Run(name, func(t *testing.T) {
				fixture := factory(t)
				owner := acquire(t, fixture)
				if heartbeat {
					_, err := fixture.Store.Heartbeat(context.Background(), idempotency.HeartbeatRequest{
						Ownership: owner.Record.Ownership(), Lease: time.Minute,
					})
					mustNoError(t, err)
				}
				_, err := fixture.Store.Expire(context.Background(), fixture.Key)
				mustReason(t, err, idempotency.ReasonInvalidTransition)
			})
		}
	})

	t.Run("fingerprint policy version changes conflict", func(t *testing.T) {
		fixture := factory(t)
		_ = acquire(t, fixture)
		other, err := idempotency.NewFingerprintFromSum("v2", fixture.Fingerprint.Sum())
		mustNoError(t, err)
		conflict, err := fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: fixture.Key, Fingerprint: other, Lease: time.Minute,
		})
		mustNoError(t, err)
		if conflict.Outcome != idempotency.OutcomeConflict {
			t.Fatalf("cross-version outcome = %q", conflict.Outcome)
		}
	})

	t.Run("release and explicit expiry preserve monotonic ownership", func(t *testing.T) {
		t.Run("release", func(t *testing.T) {
			fixture := factory(t)
			first := acquire(t, fixture)
			_, err := fixture.Store.Release(context.Background(), first.Record.Ownership())
			mustNoError(t, err)
			second, err := fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
				Key: fixture.Key, Fingerprint: fixture.Fingerprint, Lease: time.Minute,
			})
			mustNoError(t, err)
			if second.Outcome != idempotency.OutcomeAcquired || second.Record.FencingToken != 2 {
				t.Fatalf("acquire after release = %#v", second)
			}
		})

		t.Run("expire", func(t *testing.T) {
			fixture := factory(t)
			first := acquire(t, fixture)
			fixture.Advance(time.Minute)
			expired, err := fixture.Store.Expire(context.Background(), fixture.Key)
			mustNoError(t, err)
			if expired.State != idempotency.StateExpired {
				t.Fatalf("Expire() state = %q", expired.State)
			}
			second, err := fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
				Key: fixture.Key, Fingerprint: fixture.Fingerprint, Lease: time.Minute,
			})
			mustNoError(t, err)
			if second.Outcome != idempotency.OutcomeAcquired ||
				second.Record.FencingToken != first.Record.FencingToken+1 {
				t.Fatalf("acquire after expiry = %#v", second)
			}
		})
	})

	t.Run("invalid lease and replay data fail before mutation", func(t *testing.T) {
		fixture := factory(t)
		_, err := fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
			Key: fixture.Key, Fingerprint: fixture.Fingerprint,
		})
		mustReason(t, err, idempotency.ReasonInvalidLease)
		owner := acquire(t, fixture)
		_, err = fixture.Store.Heartbeat(context.Background(), idempotency.HeartbeatRequest{
			Ownership: owner.Record.Ownership(),
		})
		mustReason(t, err, idempotency.ReasonInvalidLease)
		_, err = fixture.Store.Heartbeat(context.Background(), idempotency.HeartbeatRequest{
			Ownership: owner.Record.Ownership(), Lease: idempotency.MaxLease + time.Nanosecond,
		})
		mustReason(t, err, idempotency.ReasonLimitExceeded)
		_, err = fixture.Store.Complete(context.Background(), idempotency.CompleteRequest{
			Ownership: owner.Record.Ownership(),
			Result:    make([]byte, idempotency.MaxResultBytes+1),
		})
		mustReason(t, err, idempotency.ReasonLimitExceeded)
		inspected, err := fixture.Store.Inspect(context.Background(), fixture.Key)
		mustNoError(t, err)
		if inspected.State != idempotency.StateAcquired {
			t.Fatalf("state after rejected result = %q", inspected.State)
		}
	})

	t.Run("canceled operations do not mutate records", func(t *testing.T) {
		t.Run("acquire", func(t *testing.T) {
			fixture := factory(t)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := fixture.Store.Acquire(ctx, idempotency.AcquireRequest{
				Key: fixture.Key, Fingerprint: fixture.Fingerprint, Lease: time.Minute,
			})
			mustCanceled(t, err)
			_, err = fixture.Store.Inspect(context.Background(), fixture.Key)
			mustReason(t, err, idempotency.ReasonNotFound)
		})

		operations := map[string]func(context.Context, StoreFixture, idempotency.Ownership) error{
			"inspect": func(ctx context.Context, fixture StoreFixture, _ idempotency.Ownership) error {
				_, err := fixture.Store.Inspect(ctx, fixture.Key)
				return err
			},
			"heartbeat": func(ctx context.Context, fixture StoreFixture, ownership idempotency.Ownership) error {
				_, err := fixture.Store.Heartbeat(ctx, idempotency.HeartbeatRequest{
					Ownership: ownership, Lease: time.Minute,
				})
				return err
			},
			"complete": func(ctx context.Context, fixture StoreFixture, ownership idempotency.Ownership) error {
				_, err := fixture.Store.Complete(ctx, idempotency.CompleteRequest{Ownership: ownership})
				return err
			},
			"fail": func(ctx context.Context, fixture StoreFixture, ownership idempotency.Ownership) error {
				_, err := fixture.Store.Fail(ctx, idempotency.FailRequest{Ownership: ownership})
				return err
			},
			"release": func(ctx context.Context, fixture StoreFixture, ownership idempotency.Ownership) error {
				_, err := fixture.Store.Release(ctx, ownership)
				return err
			},
			"expire": func(ctx context.Context, fixture StoreFixture, _ idempotency.Ownership) error {
				fixture.Advance(time.Minute)
				_, err := fixture.Store.Expire(ctx, fixture.Key)
				return err
			},
		}
		for name, operation := range operations {
			t.Run(name, func(t *testing.T) {
				fixture := factory(t)
				owner := acquire(t, fixture)
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				mustCanceled(t, operation(ctx, fixture, owner.Record.Ownership()))
				record, err := fixture.Store.Inspect(context.Background(), fixture.Key)
				mustNoError(t, err)
				if record.State != idempotency.StateAcquired {
					t.Fatalf("state after canceled operation = %q", record.State)
				}
			})
		}
	})
}

func assertOwnershipMutationsReason(
	t *testing.T,
	store idempotency.Store,
	ownership idempotency.Ownership,
	want idempotency.Reason,
) {
	t.Helper()
	operations := map[string]func() error{
		"heartbeat": func() error {
			_, err := store.Heartbeat(context.Background(), idempotency.HeartbeatRequest{
				Ownership: ownership, Lease: time.Minute,
			})
			return err
		},
		"complete": func() error {
			_, err := store.Complete(context.Background(), idempotency.CompleteRequest{
				Ownership: ownership, Result: []byte("result"),
			})
			return err
		},
		"fail": func() error {
			_, err := store.Fail(context.Background(), idempotency.FailRequest{
				Ownership: ownership, Result: []byte("failure"),
			})
			return err
		},
		"release": func() error {
			_, err := store.Release(context.Background(), ownership)
			return err
		},
	}
	for name, operation := range operations {
		t.Run(name, func(t *testing.T) {
			mustReason(t, operation(), want)
		})
	}
}

type concurrentResult[T any] struct {
	value T
	err   error
}

func runConcurrently[T any](
	callers int,
	call func(int) (T, error),
) []concurrentResult[T] {
	start := make(chan struct{})
	results := make(chan concurrentResult[T], callers)
	var wait sync.WaitGroup
	for index := range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			value, err := call(index)
			results <- concurrentResult[T]{value: value, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	collected := make([]concurrentResult[T], 0, callers)
	for result := range results {
		collected = append(collected, result)
	}
	return collected
}

func acquire(t testing.TB, fixture StoreFixture) idempotency.AcquireResult {
	t.Helper()
	result, err := fixture.Store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: fixture.Key, Fingerprint: fixture.Fingerprint, Lease: time.Minute,
	})
	mustNoError(t, err)
	if result.Outcome != idempotency.OutcomeAcquired {
		t.Fatalf("Acquire() outcome = %q, want acquired", result.Outcome)
	}
	return result
}

func mustNoError(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func mustReason(t testing.TB, err error, reason idempotency.Reason) {
	t.Helper()
	var semanticError *idempotency.Error
	if !errors.As(err, &semanticError) {
		t.Fatalf("error = %v, want *idempotency.Error", err)
	}
	if semanticError.Reason != reason {
		t.Fatalf("reason = %q, want %q", semanticError.Reason, reason)
	}
}

func mustCanceled(t testing.TB, err error) {
	t.Helper()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
