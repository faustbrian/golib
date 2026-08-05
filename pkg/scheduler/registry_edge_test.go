package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/lease"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
)

type clearCacheStore struct {
	inspectResults []struct {
		owned lease.Lease
		err   error
	}
	recoverErrors []error
	inspectIndex  int
	recoverIndex  int
}

func (*clearCacheStore) Acquire(context.Context, string, string, time.Duration, time.Time) (lease.Lease, error) {
	return lease.Lease{}, nil
}

func (*clearCacheStore) Heartbeat(context.Context, lease.Lease, time.Duration, time.Time) (lease.Lease, error) {
	return lease.Lease{}, nil
}

func (*clearCacheStore) Release(context.Context, lease.Lease) error { return nil }

func (store *clearCacheStore) Inspect(context.Context, string) (lease.Lease, error) {
	result := store.inspectResults[store.inspectIndex]
	store.inspectIndex++
	return result.owned, result.err
}

func (store *clearCacheStore) Recover(context.Context, string, uint64) error {
	index := store.recoverIndex
	store.recoverIndex++
	if index < len(store.recoverErrors) {
		return store.recoverErrors[index]
	}
	return nil
}

func (*clearCacheStore) Capabilities() lease.Capabilities { return lease.Capabilities{} }

func TestRegistryMissingDisabledAndEmptyRanges(t *testing.T) {
	t.Parallel()

	registry, _ := scheduler.Compile()
	if _, err := registry.Next("missing", time.Now()); !errors.Is(err, scheduler.ErrScheduleNotFound) {
		t.Fatalf("Next(missing) error = %v", err)
	}
	if _, err := registry.Due("missing", time.Time{}, time.Now()); !errors.Is(err, scheduler.ErrScheduleNotFound) {
		t.Fatalf("Due(missing) error = %v", err)
	}
	disabled, _ := scheduler.NewSchedule("disabled", "task", scheduler.Daily(), scheduler.WithEnabled(false))
	registry, _ = scheduler.Compile(disabled)
	now := time.Now()
	for _, through := range []time.Time{now, now.Add(-time.Second), now.Add(time.Hour)} {
		occurrences, err := registry.Due("disabled", now, through)
		if err != nil || len(occurrences) != 0 {
			t.Fatalf("Due(disabled) = %v, %v", occurrences, err)
		}
	}
}

func TestCompileRejectsRegistryBeyondResourceBudget(t *testing.T) {
	t.Parallel()

	schedules := make([]scheduler.Schedule, scheduler.MaxSchedules+1)
	if _, err := scheduler.Compile(schedules...); !errors.Is(err, scheduler.ErrResourceLimit) {
		t.Fatalf("Compile() error = %v, want ErrResourceLimit", err)
	}
}

func TestDueHonorsBoundsAndRejectsInvalidPolicy(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	bounded, _ := scheduler.NewSchedule(
		"bounded", "task", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunCatchUp, 10),
		scheduler.WithDateBounds(from.Add(2*time.Minute), from.Add(3*time.Minute)),
	)
	registry, _ := scheduler.Compile(bounded)
	occurrences, err := registry.Due("bounded", from, from.Add(5*time.Minute))
	if err != nil || len(occurrences) != 2 {
		t.Fatalf("Due(bounded) = %v, %v", occurrences, err)
	}

	invalid := bounded
	invalid.Name = "invalid"
	invalid.MissedRunPolicy = scheduler.MissedRunPolicy(255)
	registry, _ = scheduler.Compile(invalid)
	if _, err := registry.Due("invalid", from, from.Add(time.Minute)); !errors.Is(err, scheduler.ErrInvalidMissedRuns) {
		t.Fatalf("Due(invalid policy) error = %v", err)
	}
}

func TestDueRejectsUnboundedDowntimeScan(t *testing.T) {
	t.Parallel()

	from := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	schedule, _ := scheduler.NewSchedule(
		"catch-up", "task", scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunCatchUp, 1),
	)
	registry, _ := scheduler.Compile(schedule)
	_, err := registry.Due("catch-up", from, from.Add(10_001*time.Minute))
	if !errors.Is(err, scheduler.ErrOccurrenceLimit) {
		t.Fatalf("Due(long downtime) error = %v", err)
	}
}

func TestSkipPolicyHonorsDateBounds(t *testing.T) {
	t.Parallel()

	through := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	schedule, _ := scheduler.NewSchedule(
		"future", "task", scheduler.EveryMinute(),
		scheduler.WithDateBounds(through.Add(time.Minute), time.Time{}),
	)
	registry, _ := scheduler.Compile(schedule)
	occurrences, err := registry.Due("future", through.Add(-time.Minute), through)
	if err != nil || len(occurrences) != 0 {
		t.Fatalf("Due(future) = %v, %v", occurrences, err)
	}
}

func TestClearCacheHandlesEmptyMissingAndInvalidStores(t *testing.T) {
	t.Parallel()

	plain, _ := scheduler.NewSchedule("a-plain", "task", scheduler.EveryMinute())
	overlap, _ := scheduler.NewSchedule(
		"z-overlap", "task", scheduler.EveryMinute(), scheduler.WithoutOverlapping(),
	)
	registry, _ := scheduler.Compile(plain, overlap)
	store := memory.New()
	if cleared, err := registry.ClearCache(context.Background(), store); err != nil || cleared != 0 {
		t.Fatalf("ClearCache(empty) = %d, %v", cleared, err)
	}
	key := "task:" + overlap.CoordinationID
	if _, err := store.Acquire(context.Background(), key, "stopped-owner", time.Hour, time.Now()); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if cleared, err := registry.ClearCache(context.Background(), store); err != nil || cleared != 1 {
		t.Fatalf("ClearCache(present) = %d, %v", cleared, err)
	}
	if cleared, err := registry.ClearCache(context.Background(), nil); cleared != 0 || !errors.Is(err, lease.ErrInvalid) {
		t.Fatalf("ClearCache(nil) = %d, %v", cleared, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if cleared, err := registry.ClearCache(canceled, memory.New()); cleared != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("ClearCache(canceled) = %d, %v", cleared, err)
	}
}

func TestClearCacheReportsLeaseBackendFailures(t *testing.T) {
	t.Parallel()

	first, _ := scheduler.NewSchedule(
		"a-overlap", "task", scheduler.EveryMinute(), scheduler.WithoutOverlapping(),
	)
	second, _ := scheduler.NewSchedule(
		"b-overlap", "task", scheduler.EveryMinute(), scheduler.WithoutOverlapping(),
	)
	registry, _ := scheduler.Compile(first, second)
	backend := errors.New("backend")
	for name, store := range map[string]*clearCacheStore{
		"missing then present": {inspectResults: []struct {
			owned lease.Lease
			err   error
		}{{err: lease.ErrNotFound}, {owned: lease.Lease{FencingToken: 7}}}},
		"inspect failure then present": {inspectResults: []struct {
			owned lease.Lease
			err   error
		}{{err: backend}, {owned: lease.Lease{FencingToken: 7}}}},
		"recover failure then present": {inspectResults: []struct {
			owned lease.Lease
			err   error
		}{{owned: lease.Lease{FencingToken: 6}}, {owned: lease.Lease{FencingToken: 7}}}, recoverErrors: []error{backend, nil}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cleared, err := registry.ClearCache(context.Background(), store)
			wantError := name != "missing then present"
			if cleared != 1 || errors.Is(err, backend) != wantError {
				t.Fatalf("ClearCache() = %d, %v", cleared, err)
			}
		})
	}
}
