package scheduler_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
)

func TestConcurrentReplicasDispatchOnePhysicalOccurrence(t *testing.T) {
	t.Parallel()

	oldSchedule, _ := scheduler.NewSchedule(
		"report", "reports.generate", scheduler.Daily(),
		scheduler.WithVersion("old"),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithOneServer(time.Minute),
	)
	newSchedule, _ := scheduler.NewSchedule(
		"report", "reports.generate", scheduler.Hourly(),
		scheduler.WithVersion("new"),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithOneServer(time.Minute),
	)
	oldRegistry, _ := scheduler.Compile(oldSchedule)
	newRegistry, _ := scheduler.Compile(newSchedule)
	leasing := memory.New()
	var calls atomic.Int64
	executor := executorFunc(func(context.Context, scheduler.Context) error {
		calls.Add(1)
		return nil
	})
	const replicas = 32
	runners := make([]*scheduler.Runner, 0, replicas)
	for index := range replicas {
		registry := oldRegistry
		if index%2 == 1 {
			registry = newRegistry
		}
		runner, err := scheduler.NewRunner(
			registry,
			leasing,
			executor,
			scheduler.WithOwner(fmt.Sprintf("replica-%02d", index)),
		)
		if err != nil {
			t.Fatalf("NewRunner() error = %v", err)
		}
		runners = append(runners, runner)
	}
	through := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	after := through.Add(-time.Hour)
	start := make(chan struct{})
	errors := make(chan error, replicas)
	var wait sync.WaitGroup
	for _, runner := range runners {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			errors <- runner.Tick(context.Background(), after, through)
		}()
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("Tick() error = %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("dispatch calls = %d, want 1", got)
	}
}

func TestConcurrentReplicasRespectActiveTaskOverlap(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"billing", "billing.close", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithoutOverlap(scheduler.OverlapSkip, time.Minute),
		scheduler.WithRunTimeout(time.Minute),
	)
	registry, _ := scheduler.Compile(schedule)
	leasing := memory.New()
	release := make(chan struct{})
	started := make(chan struct{})
	var calls atomic.Int64
	executor := executorFunc(func(context.Context, scheduler.Context) error {
		calls.Add(1)
		close(started)
		<-release
		return nil
	})
	const replicas = 32
	runners := make([]*scheduler.Runner, 0, replicas)
	for index := range replicas {
		runner, err := scheduler.NewRunner(
			registry,
			leasing,
			executor,
			scheduler.WithOwner(fmt.Sprintf("replica-%02d", index)),
		)
		if err != nil {
			t.Fatalf("NewRunner() error = %v", err)
		}
		runners = append(runners, runner)
	}
	through := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	start := make(chan struct{})
	done := make(chan error, replicas)
	for _, runner := range runners {
		go func() {
			<-start
			done <- runner.Tick(context.Background(), through.Add(-time.Minute), through)
		}()
	}
	close(start)
	for range replicas - 1 {
		select {
		case err := <-done:
			if err != nil {
				close(release)
				t.Fatalf("Tick() error = %v", err)
			}
		case <-time.After(time.Second):
			close(release)
			t.Fatal("overlap contender did not finish")
		}
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("overlap winner did not start")
	}
	if got := calls.Load(); got != 1 {
		close(release)
		t.Fatalf("active executions = %d, want 1", got)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("winning Tick() error = %v", err)
	}
}

func TestExpiredPreDispatchOwnerIsTakenOverWithHigherFence(t *testing.T) {
	t.Parallel()

	schedule, _ := scheduler.NewSchedule(
		"recovery", "task.recovery", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithOneServer(time.Minute),
	)
	registry, _ := scheduler.Compile(schedule)
	through := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	due, err := registry.Due("recovery", through.Add(-time.Minute), through)
	if err != nil || len(due) != 1 {
		t.Fatalf("Due() = %v, %v", due, err)
	}
	leasing := memory.New()
	stale, err := leasing.Acquire(
		context.Background(),
		"occurrence:"+due[0].IdempotencyKey,
		"crashed-replica",
		time.Minute,
		through.Add(-2*time.Minute),
	)
	if err != nil {
		t.Fatalf("Acquire(stale) error = %v", err)
	}
	var executed scheduler.Context
	runner, _ := scheduler.NewRunner(
		registry,
		leasing,
		executorFunc(func(_ context.Context, scheduled scheduler.Context) error {
			executed = scheduled
			return nil
		}),
		scheduler.WithOwner("replacement-replica"),
	)
	if err := runner.Tick(context.Background(), through.Add(-time.Minute), through); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if executed.Owner != "replacement-replica" || executed.Fencing <= stale.FencingToken {
		t.Fatalf("replacement context = owner %q fence %d, stale fence %d", executed.Owner, executed.Fencing, stale.FencingToken)
	}
}
