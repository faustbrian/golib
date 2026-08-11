package workflow_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func TestWorkerStressCompletesEveryLeaseWithinItsConcurrencyBound(t *testing.T) {
	t.Parallel()

	const (
		workCount  = 512
		concurrent = 32
	)
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	leases := make([]workflow.WorkLease, workCount)
	for index := range leases {
		leases[index] = mustWorkerLease(t, now, fmt.Sprintf("stress-work-%d", index), fmt.Sprintf("tenant-%d", index%7))
	}

	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	store := &workerStore{claims: [][]workflow.WorkLease{leases}}
	processor := &countingProcessor{cancel: cancel, target: workCount}
	worker, err := workflow.NewWorker(workflow.WorkerConfig{
		Store: store, Processor: processor, Clock: hardeningClock{now: now}, Owner: "worker-1",
		MaxConcurrent: concurrent, ClaimLimit: concurrent, LeaseDuration: time.Minute,
		RenewEvery: 20 * time.Second, PollInterval: time.Millisecond, FinalizeTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("construct stress worker: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	if err := receiveHardeningWithin(t, done); err != nil {
		t.Fatalf("run stress worker: %v", err)
	}
	if processor.maximum > concurrent || processor.completed != workCount || processor.active != 0 {
		t.Fatalf("processor state = active %d maximum %d completed %d", processor.active, processor.maximum, processor.completed)
	}
	if len(store.completions) != workCount {
		t.Fatalf("durable completions = %d, want %d", len(store.completions), workCount)
	}
	for _, limit := range store.claimLimits {
		if limit == 0 || limit > concurrent {
			t.Fatalf("claim limit = %d, want 1..%d", limit, concurrent)
		}
	}
}

func TestWorkerShutdownJoinsEveryOwnedProcessorGoroutine(t *testing.T) {
	t.Parallel()

	const concurrent = 32
	now := time.Date(2026, 8, 11, 10, 0, 0, 0, time.UTC)
	leases := make([]workflow.WorkLease, concurrent*2)
	for index := range leases {
		leases[index] = mustWorkerLease(t, now, fmt.Sprintf("shutdown-work-%d", index), "")
	}

	started := make(chan struct{}, concurrent)
	var returned atomic.Int32
	processor := processorFunc(func(ctx context.Context, _ workflow.WorkLease) (workflow.WorkDecision, error) {
		started <- struct{}{}
		<-ctx.Done()
		returned.Add(1)
		return workflow.WorkDecision{}, ctx.Err()
	})
	store := &workerStore{claims: [][]workflow.WorkLease{leases}}
	worker, err := workflow.NewWorker(workflow.WorkerConfig{
		Store: store, Processor: processor, Clock: hardeningClock{now: now}, Owner: "worker-1",
		MaxConcurrent: concurrent, ClaimLimit: concurrent, LeaseDuration: time.Minute,
		RenewEvery: 20 * time.Second, PollInterval: time.Millisecond, FinalizeTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("construct shutdown worker: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()
	for range concurrent {
		receiveHardeningWithin(t, started)
	}
	cancel()
	if err := receiveHardeningWithin(t, done); err != nil {
		t.Fatalf("stop shutdown worker: %v", err)
	}
	if returned.Load() != concurrent {
		t.Fatalf("returned processors = %d, want %d", returned.Load(), concurrent)
	}
	if len(store.completions) != 0 || len(store.failures) != 0 {
		t.Fatalf("shutdown finalized work = completions %d failures %d", len(store.completions), len(store.failures))
	}
}

func receiveHardeningWithin[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for hardening result")
		var zero T
		return zero
	}
}

type hardeningClock struct{ now time.Time }

func (clock hardeningClock) Now() time.Time { return clock.now }

func (hardeningClock) NewTimer(time.Duration) workflow.ClockTimer { return hardeningTimer{} }

type hardeningTimer struct{}

func (hardeningTimer) C() <-chan time.Time { return nil }

func (hardeningTimer) Stop() bool { return true }

func TestContinueAsNewSoakKeepsEachHistoryBoundedAndDeterministic(t *testing.T) {
	t.Parallel()

	const generations = 10_000
	first := mustDefinition(t, "soak.workflow", "1")
	second := mustDefinition(t, "soak.workflow", "2")
	registry, err := workflow.CompileDefinitions(first, second)
	if err != nil {
		t.Fatalf("compile soak definitions: %v", err)
	}
	definitions := []workflow.Definition{first, second}
	now := time.Date(2026, 8, 11, 11, 0, 0, 0, time.UTC)
	for generation := range generations {
		current := definitions[generation%len(definitions)]
		next := definitions[(generation+1)%len(definitions)]
		instanceID := fmt.Sprintf("soak-instance-%d", generation)
		successorID := fmt.Sprintf("soak-instance-%d", generation+1)
		occurredAt := now.Add(time.Duration(generation*2) * time.Nanosecond)
		events := []workflow.HistoryEvent{
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 1, InstanceID: instanceID, Kind: workflow.EventInstanceStarted,
				OccurredAt: occurredAt, Definition: current.Reference(),
			}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 2, InstanceID: instanceID, Kind: workflow.EventContinuedAsNew,
				OccurredAt: occurredAt.Add(time.Nanosecond), Definition: next.Reference(), SuccessorID: successorID,
			}),
		}
		instance, replayErr := workflow.Replay(registry, events)
		if replayErr != nil {
			t.Fatalf("replay generation %d: %v", generation, replayErr)
		}
		if instance.Status() != workflow.StatusContinuedAsNew || instance.Sequence() != 2 ||
			instance.Definition() != current.Reference() || events[1].Definition() != next.Reference() ||
			instance.SuccessorID() != successorID {
			t.Fatalf("generation %d state = %#v", generation, instance)
		}
		replayed, replayErr := workflow.Replay(registry, events)
		if replayErr != nil || replayed.SnapshotDigest() != instance.SnapshotDigest() {
			t.Fatalf("generation %d deterministic replay = %#v, %v", generation, replayed, replayErr)
		}
	}
}
