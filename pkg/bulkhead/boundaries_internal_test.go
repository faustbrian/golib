package bulkhead

import (
	"context"
	"errors"
	"testing"
	"time"
)

type unsupportedAdmission struct{}

func (unsupportedAdmission) admissionPolicy() {}

type nilAdmission struct{}

func (*nilAdmission) admissionPolicy() {}

type unsupportedPartitioning struct{}

func (unsupportedPartitioning) partitioningPolicy() {}

type nilPartitioning struct{}

func (*nilPartitioning) partitioningPolicy() {}

func TestInternalPolicyValidationBoundaries(t *testing.T) {
	RejectImmediately{}.admissionPolicy()
	Wait{}.admissionPolicy()
	FixedPartitions{}.partitioningPolicy()
	var typedNilAdmission *nilAdmission
	var typedNilChannel chan int
	var typedNilFunction func()
	var typedNilMap map[string]int
	var typedNilSlice []int
	for _, value := range []any{typedNilChannel, typedNilFunction, typedNilMap, typedNilSlice} {
		if !nilLike(value) {
			t.Fatalf("nilLike(%T) = false", value)
		}
	}
	if _, err := New(Config{
		Resource: "database", Capacity: 1, Admission: typedNilAdmission,
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(typed nil admission) error = %v", err)
	}
	if _, err := New(Config{
		Resource: "database", Capacity: 1, Admission: unsupportedAdmission{},
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New(unsupported admission) error = %v", err)
	}

	var typedNilPartitioning *nilPartitioning
	for _, policy := range []PartitioningPolicy{
		typedNilPartitioning,
		unsupportedPartitioning{},
		FixedPartitions{},
		FixedPartitions{Maximum: MaxPartitions + 1},
	} {
		if _, err := NewRegistry(policy); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("NewRegistry(%T) error = %v", policy, err)
		}
	}
}

func TestInternalDefensiveBranches(t *testing.T) {
	if got := nonNegativeDuration(-time.Second); got != 0 {
		t.Fatalf("nonNegativeDuration(-1s) = %s", got)
	}
	if reason := rejectionReason(context.Canceled); reason != RejectionCaller {
		t.Fatalf("rejectionReason(context.Canceled) = %s", reason)
	}
	if _, _, err := executeAdmitted(
		context.Background(),
		policyForInternalTest(t),
		1,
		nil,
		func(context.Context) (int, error) { return 0, nil },
	); !errors.Is(err, errAdmissionInvariant) {
		t.Fatalf("executeAdmitted(nil permit) error = %v", err)
	}
	policy, err := New(Config{Resource: "database", Capacity: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	policy.mu.Lock()
	waiter := &waiter{}
	policy.removeWaiterLocked(waiter)
	policy.mu.Unlock()
	policy.mu.Lock()
	if policy.maybeDrainedLocked() {
		t.Fatal("open policy reported drained")
	}
	policy.mu.Unlock()
	activePolicy := policyForInternalTest(t)
	activePolicy.mu.Lock()
	activePolicy.draining = true
	activePolicy.active = 1
	if activePolicy.maybeDrainedLocked() {
		activePolicy.mu.Unlock()
		t.Fatal("active draining policy reported drained")
	}
	activePolicy.mu.Unlock()
	if activePolicy.Snapshot().Drained {
		t.Fatal("active draining snapshot reported drained")
	}
	queuedPolicy := policyForInternalTest(t)
	queuedPolicy.mu.Lock()
	queuedPolicy.draining = true
	queuedPolicy.queued = 1
	if queuedPolicy.maybeDrainedLocked() {
		queuedPolicy.mu.Unlock()
		t.Fatal("queued draining policy reported drained")
	}
	queuedPolicy.mu.Unlock()
	if queuedPolicy.Snapshot().Drained {
		t.Fatal("queued draining snapshot reported drained")
	}
	if err := policy.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	policy.mu.Lock()
	if policy.maybeDrainedLocked() {
		t.Fatal("already drained policy reported a new drain transition")
	}
	policy.mu.Unlock()
}

func policyForInternalTest(t *testing.T) *Bulkhead {
	t.Helper()
	policy, err := New(Config{Resource: "internal", Capacity: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return policy
}

func TestInternalQueueTransitionBranches(t *testing.T) {
	policy, err := New(Config{Resource: "database", Capacity: 2})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	holder, err := policy.Acquire(context.Background(), 2)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	policy.mu.Lock()
	blocked := &waiter{weight: 1, deadline: time.Now().Add(time.Hour), ready: make(chan struct{})}
	blocked.element = policy.waiters.PushBack(blocked)
	policy.queued++
	policy.grantWaitersLocked(time.Now())
	if blocked.done {
		t.Fatal("waiter granted without capacity")
	}
	blocked.done = true
	policy.grantWaitersLocked(time.Now())
	policy.removeWaiterLocked(blocked)
	policy.mu.Unlock()
	if err := holder.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	closing, err := New(Config{Resource: "cache", Capacity: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	closing.mu.Lock()
	alreadyDone := &waiter{done: true, ready: make(chan struct{})}
	alreadyDone.element = closing.waiters.PushBack(alreadyDone)
	live := &waiter{ready: make(chan struct{})}
	live.element = closing.waiters.PushBack(live)
	closing.queued = 2
	closing.mu.Unlock()
	if err := closing.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case <-live.ready:
	default:
		t.Fatal("Close() did not wake live waiter")
	}
	closing.mu.Lock()
	closing.removeWaiterLocked(alreadyDone)
	closing.removeWaiterLocked(live)
	closing.maybeDrainedLocked()
	closing.mu.Unlock()
}

func TestInternalGrantTransitionsPreserveExactAccounting(t *testing.T) {
	policy, err := New(Config{Resource: "queue-accounting", Capacity: 3})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	now := time.Now()
	policy.mu.Lock()
	done := &waiter{done: true, weight: 3, deadline: now.Add(time.Hour), ready: make(chan struct{})}
	done.element = policy.waiters.PushBack(done)
	expired := &waiter{weight: 2, deadline: now, ready: make(chan struct{})}
	expired.element = policy.waiters.PushBack(expired)
	live := &waiter{weight: 2, deadline: now.Add(time.Hour), ready: make(chan struct{})}
	live.element = policy.waiters.PushBack(live)
	policy.queued = 3
	policy.grantWaitersLocked(now)
	if !expired.done || expired.terminal != ErrWaitTimeout || live.done != true || live.granted != true ||
		policy.active != 2 || policy.admissions != 1 || policy.queued != 2 || live.element != nil {
		policy.mu.Unlock()
		t.Fatalf("grant state: expired=%+v live=%+v active=%d admissions=%d queued=%d",
			expired, live, policy.active, policy.admissions, policy.queued)
	}
	policy.removeWaiterLocked(done)
	policy.removeWaiterLocked(expired)
	policy.mu.Unlock()

	select {
	case <-expired.ready:
	default:
		t.Fatal("expired waiter was not awakened")
	}
	select {
	case <-live.ready:
	default:
		t.Fatal("admitted waiter was not awakened")
	}
}

func TestInternalAdmittedWaitDurationsAccumulate(t *testing.T) {
	clock := &internalTestClock{now: time.Unix(100, 0)}
	policy, err := New(Config{Resource: "wait-accounting", Capacity: 1, Clock: clock})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, waited := range []time.Duration{time.Millisecond, 2 * time.Millisecond} {
		start := clock.Now()
		clock.Advance(waited)
		permit, finishErr := policy.finishWait(&waiter{weight: 1, granted: true}, start, "", nil)
		if finishErr != nil || permit == nil || permit.waitDuration != waited {
			t.Fatalf("finishWait(%s) = %+v, %v", waited, permit, finishErr)
		}
	}
	if policy.totalWait != 3*time.Millisecond {
		t.Fatalf("totalWait = %s", policy.totalWait)
	}
}

type internalTestClock struct {
	now time.Time
}

func (clock *internalTestClock) Now() time.Time { return clock.now }

func (*internalTestClock) NewTimer(duration time.Duration) Timer {
	return systemTimer{Timer: time.NewTimer(duration)}
}

func (clock *internalTestClock) Advance(duration time.Duration) {
	clock.now = clock.now.Add(duration)
}
