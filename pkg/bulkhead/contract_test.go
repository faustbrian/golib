package bulkhead_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/bulkhead"
)

func TestConfigurationRejectsUnboundedAndTypedNilInputs(t *testing.T) {
	var nilClock *testClock
	var nilObserver *recordingObserver
	tests := []struct {
		name   string
		config bulkhead.Config
	}{
		{name: "empty resource", config: bulkhead.Config{Capacity: 1}},
		{name: "unsafe resource", config: bulkhead.Config{Resource: "secret value", Capacity: 1}},
		{name: "long resource", config: bulkhead.Config{Resource: strings.Repeat("a", bulkhead.MaxResourceBytes+1), Capacity: 1}},
		{name: "zero capacity", config: bulkhead.Config{Resource: "database"}},
		{name: "long revision", config: bulkhead.Config{Resource: "database", Capacity: 1, PolicyRevision: strings.Repeat("a", bulkhead.MaxResourceBytes+1)}},
		{name: "unsafe revision", config: bulkhead.Config{Resource: "database", Capacity: 1, PolicyRevision: "revision value"}},
		{name: "typed nil clock", config: bulkhead.Config{Resource: "database", Capacity: 1, Clock: nilClock}},
		{name: "typed nil observer", config: bulkhead.Config{Resource: "database", Capacity: 1, Observer: nilObserver}},
		{name: "zero queue", config: bulkhead.Config{Resource: "database", Capacity: 1, Admission: bulkhead.Wait{MaxWait: time.Second}}},
		{name: "unbounded queue", config: bulkhead.Config{Resource: "database", Capacity: 1, Admission: bulkhead.Wait{MaxQueued: bulkhead.MaxQueueSize + 1, MaxWait: time.Second}}},
		{name: "zero wait", config: bulkhead.Config{Resource: "database", Capacity: 1, Admission: bulkhead.Wait{MaxQueued: 1}}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if _, err := bulkhead.New(test.config); !errors.Is(err, bulkhead.ErrInvalidConfig) {
				t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestConfigurationAcceptsDocumentedUpperBounds(t *testing.T) {
	resource := strings.Repeat("r", bulkhead.MaxResourceBytes)
	policy, err := bulkhead.New(bulkhead.Config{
		Resource:       resource,
		Capacity:       1,
		PolicyRevision: strings.Repeat("p", bulkhead.MaxResourceBytes),
		Admission: bulkhead.Wait{
			MaxQueued: bulkhead.MaxQueueSize,
			MaxWait:   time.Nanosecond,
		},
	})
	if err != nil {
		t.Fatalf("New(maximum bounds) error = %v", err)
	}
	if got := policy.Snapshot(); got.Resource != resource || got.Capacity != 1 {
		t.Fatalf("Snapshot() = %+v", got)
	}
}

func TestAdmissionErrorsAndSnapshotReasonsRemainDistinct(t *testing.T) {
	policy := mustPolicy(t, bulkhead.Config{
		Resource:  "database",
		Capacity:  1,
		Admission: bulkhead.RejectImmediately{},
	})
	//lint:ignore SA1012 Public boundary must reject a nil context safely.
	if _, err := policy.Acquire(nil, 1); !errors.Is(err, bulkhead.ErrCallerCanceled) { //nolint:staticcheck // Explicit nil rejection.
		t.Fatalf("Acquire(nil) error = %v", err)
	}
	if _, err := policy.Acquire(context.Background(), 0); !errors.Is(err, bulkhead.ErrInvalidWeight) {
		t.Fatalf("Acquire(zero) error = %v", err)
	}
	if _, err := policy.Acquire(context.Background(), 2); !errors.Is(err, bulkhead.ErrInvalidWeight) {
		t.Fatalf("Acquire(oversize) error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := policy.Acquire(canceled, 1); !errors.Is(err, context.Canceled) ||
		!errors.Is(err, bulkhead.ErrCallerCanceled) {
		t.Fatalf("Acquire(canceled) error = %v", err)
	}

	snapshot := policy.Snapshot()
	if snapshot.RejectionCounts[bulkhead.RejectionCaller] != 2 ||
		snapshot.RejectionCounts[bulkhead.RejectionWeight] != 2 || snapshot.Cancellations != 2 {
		t.Fatalf("Snapshot().RejectionCounts = %+v", snapshot.RejectionCounts)
	}
}

func TestWeightedHeadOfLineIsStrictAndCancellationUnblocksNext(t *testing.T) {
	policy := mustPolicy(t, bulkhead.Config{
		Resource:  "database",
		Capacity:  3,
		Admission: bulkhead.Wait{MaxQueued: 2, MaxWait: 100 * time.Millisecond},
	})
	holder, err := policy.Acquire(context.Background(), 2)
	if err != nil {
		t.Fatalf("holder Acquire() error = %v", err)
	}
	firstContext, cancelFirst := context.WithCancel(context.Background())
	first := acquireWeightAsync(policy, firstContext, 2)
	waitForQueueDepth(t, policy, 1)
	second := acquireWeightAsync(policy, context.Background(), 1)
	waitForQueueDepth(t, policy, 2)

	select {
	case result := <-second:
		if result.err == nil {
			_ = result.permit.Release()
		}
		t.Fatalf("lighter waiter bypassed weighted queue head: %v", result.err)
	default:
	}
	cancelFirst()
	if result := receiveAcquire(t, first); !errors.Is(result.err, context.Canceled) {
		t.Fatalf("canceled head error = %v", result.err)
	}
	secondPermit := receivePermit(t, second)
	if secondPermit.Weight() != 1 || secondPermit.Resource() != "database" {
		t.Fatalf("second permit = weight %d resource %q", secondPermit.Weight(), secondPermit.Resource())
	}
	_ = secondPermit.Release()
	_ = holder.Release()
}

func TestObserverFailurePanicAndReentrancyDoNotAlterAdmission(t *testing.T) {
	var policy *bulkhead.Bulkhead
	observer := &recordingObserver{failure: errors.New("telemetry unavailable")}
	observer.callback = func() {
		_ = policy.Snapshot()
	}
	var err error
	policy, err = bulkhead.New(bulkhead.Config{
		Resource:       "database",
		Capacity:       1,
		Admission:      bulkhead.RejectImmediately{},
		PolicyRevision: "revision-7",
		Observer:       observer,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	permit, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	_ = permit.Release()
	events := observer.Events()
	if len(events) != 2 || events[0].Kind != bulkhead.EventAdmitted ||
		events[0].PolicyRevision != "revision-7" || events[0].At.IsZero() {
		t.Fatalf("observer events = %+v", events)
	}

	panicPolicy := mustPolicy(t, bulkhead.Config{
		Resource: "payments",
		Capacity: 1,
		Observer: bulkhead.ObserveFunc(func(bulkhead.Event) error {
			panic("observer failure")
		}),
	})
	panicPermit, err := panicPolicy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("Acquire() with panicking observer error = %v", err)
	}
	if err := panicPermit.Release(); err != nil {
		t.Fatalf("Release() with panicking observer error = %v", err)
	}
}

func TestObserverClassifiesCallerCancellationSeparatelyFromRejection(t *testing.T) {
	observer := &recordingObserver{}
	policy := mustPolicy(t, bulkhead.Config{Resource: "database", Capacity: 1, Observer: observer})
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := policy.Acquire(canceled, 1); !errors.Is(err, bulkhead.ErrCallerCanceled) {
		t.Fatalf("Acquire(canceled) error = %v", err)
	}
	events := observer.Events()
	if len(events) != 1 || events[0].Kind != bulkhead.EventCanceled ||
		events[0].Reason != bulkhead.RejectionCaller {
		t.Fatalf("cancellation events = %+v", events)
	}
}

func TestUncooperativeExecutionRetainsCapacityUntilReturn(t *testing.T) {
	policy := mustPolicy(t, bulkhead.Config{Resource: "database", Capacity: 1})
	started := make(chan struct{})
	finish := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	completed := make(chan error, 1)
	go func() {
		_, _, err := bulkhead.Execute(ctx, policy, 1, func(context.Context) (struct{}, error) {
			close(started)
			<-finish
			return struct{}{}, nil
		})
		completed <- err
	}()
	<-started
	cancel()
	if _, err := policy.Acquire(context.Background(), 1); !errors.Is(err, bulkhead.ErrRejected) {
		t.Fatalf("Acquire() while uncooperative work runs error = %v", err)
	}
	close(finish)
	if err := <-completed; err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got := policy.Snapshot().ActiveWeight; got != 0 {
		t.Fatalf("ActiveWeight after operation return = %d", got)
	}
}

func TestExecutionReportsSeparateDurations(t *testing.T) {
	clock := &testClock{now: time.Unix(100, 0)}
	policy := mustPolicy(t, bulkhead.Config{Resource: "database", Capacity: 1, Clock: clock})
	value, result, err := bulkhead.Execute(context.Background(), policy, 1, func(context.Context) (int, error) {
		clock.Advance(7 * time.Millisecond)
		return 42, nil
	})
	if err != nil || value != 42 {
		t.Fatalf("Execute() = %d, %v", value, err)
	}
	if result.WaitDuration != 0 || result.ExecutionDuration != 7*time.Millisecond {
		t.Fatalf("Execute() result = %+v", result)
	}
	snapshot := policy.Snapshot()
	if snapshot.Executions != 1 || snapshot.TotalExecutionDuration != 7*time.Millisecond {
		t.Fatalf("Snapshot() = %+v", snapshot)
	}
	_, _, err = bulkhead.Execute(context.Background(), policy, 1, func(context.Context) (int, error) {
		clock.Advance(3 * time.Millisecond)
		return 7, nil
	})
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if snapshot = policy.Snapshot(); snapshot.Executions != 2 ||
		snapshot.TotalExecutionDuration != 10*time.Millisecond {
		t.Fatalf("Snapshot() after second execution = %+v", snapshot)
	}
}

func TestMaximumWaitUsesAbsoluteClockDeadlineDespiteDelayedTimer(t *testing.T) {
	clock := &delayedClock{now: time.Unix(100, 0)}
	policy := mustPolicy(t, bulkhead.Config{
		Resource:  "database",
		Capacity:  1,
		Admission: bulkhead.Wait{MaxQueued: 1, MaxWait: 100 * time.Millisecond},
		Clock:     clock,
	})
	holder, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("holder Acquire() error = %v", err)
	}
	waiter := acquireAsync(policy, context.Background())
	waitForQueueDepth(t, policy, 1)
	clock.Advance(2 * time.Second)
	if err := holder.Release(); err != nil {
		t.Fatalf("holder Release() error = %v", err)
	}
	if result := receiveAcquire(t, waiter); !errors.Is(result.err, bulkhead.ErrWaitTimeout) {
		t.Fatalf("delayed timer waiter error = %v, want ErrWaitTimeout", result.err)
	}
}

func TestSynchronousObserverLatencyConsumesMaximumWait(t *testing.T) {
	clock := &delayedClock{now: time.Unix(100, 0)}
	var policy *bulkhead.Bulkhead
	observer := bulkhead.ObserveFunc(func(event bulkhead.Event) error {
		if event.Kind == bulkhead.EventQueued {
			clock.Advance(time.Millisecond)
		}
		return nil
	})
	var err error
	policy, err = bulkhead.New(bulkhead.Config{
		Resource:  "database",
		Capacity:  1,
		Admission: bulkhead.Wait{MaxQueued: 1, MaxWait: time.Millisecond},
		Clock:     clock,
		Observer:  observer,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	holder, err := policy.Acquire(context.Background(), 1)
	if err != nil {
		t.Fatalf("holder Acquire() error = %v", err)
	}
	defer func() { _ = holder.Release() }()
	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := policy.Acquire(context.Background(), 1); !errors.Is(err, bulkhead.ErrWaitTimeout) {
			t.Fatalf("Acquire() attempt %d error = %v, want ErrWaitTimeout", attempt, err)
		}
	}
	if snapshot := policy.Snapshot(); snapshot.TotalWaitDuration != 2*time.Millisecond ||
		snapshot.Rejections != 2 || snapshot.RejectionCounts[bulkhead.RejectionTimeout] != 2 {
		t.Fatalf("Snapshot() after exact-deadline timeout = %+v", snapshot)
	}
	if durations := clock.TimerDurations(); len(durations) != 0 {
		t.Fatalf("NewTimer() durations = %v, want none", durations)
	}
}

func acquireWeightAsync(policy *bulkhead.Bulkhead, ctx context.Context, weight int64) <-chan acquireResult {
	result := make(chan acquireResult, 1)
	go func() {
		permit, err := policy.Acquire(ctx, weight)
		result <- acquireResult{permit: permit, err: err}
	}()
	return result
}

type recordingObserver struct {
	mu       sync.Mutex
	events   []bulkhead.Event
	failure  error
	callback func()
}

func (observer *recordingObserver) Observe(event bulkhead.Event) error {
	if observer.callback != nil {
		observer.callback()
	}
	observer.mu.Lock()
	observer.events = append(observer.events, event)
	observer.mu.Unlock()
	return observer.failure
}

func (observer *recordingObserver) Events() []bulkhead.Event {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	return append([]bulkhead.Event(nil), observer.events...)
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (*testClock) NewTimer(duration time.Duration) bulkhead.Timer {
	return testTimer{Timer: time.NewTimer(duration)}
}

func (clock *testClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

type testTimer struct{ *time.Timer }

func (timer testTimer) C() <-chan time.Time { return timer.Timer.C }

type delayedClock struct {
	mu             sync.Mutex
	now            time.Time
	timerDurations []time.Duration
}

func (clock *delayedClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *delayedClock) NewTimer(duration time.Duration) bulkhead.Timer {
	clock.mu.Lock()
	clock.timerDurations = append(clock.timerDurations, duration)
	clock.mu.Unlock()
	channel := make(chan time.Time)
	if duration <= 0 {
		close(channel)
	}
	return delayedTimer{channel: channel}
}

func (clock *delayedClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

func (clock *delayedClock) TimerDurations() []time.Duration {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return append([]time.Duration(nil), clock.timerDurations...)
}

type delayedTimer struct {
	channel <-chan time.Time
}

func (timer delayedTimer) C() <-chan time.Time { return timer.channel }

func (delayedTimer) Stop() bool { return true }
