package workflow_test

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

const (
	workflowSoakMinimumDuration = 48 * time.Hour
	workflowSoakBatchInterval   = 250 * time.Millisecond
	workflowSoakReplayCount     = 128
	workflowSoakWorkCount       = 64
	workflowSoakConcurrency     = 8
	workflowSoakHeapLimit       = 128 << 20
	workflowSoakGoroutineLimit  = 2
)

func TestWorkflowMultiDaySoakKeepsReplayAndWorkerResourcesBounded(t *testing.T) {
	durationText := os.Getenv("WORKFLOW_SOAK_DURATION")
	if durationText == "" {
		t.Skip("set WORKFLOW_SOAK_DURATION to run the explicit soak audit")
	}
	duration, err := time.ParseDuration(durationText)
	if err != nil || duration <= 0 {
		t.Fatalf("WORKFLOW_SOAK_DURATION = %q, want a positive Go duration", durationText)
	}
	if duration < workflowSoakMinimumDuration && os.Getenv("WORKFLOW_SOAK_ALLOW_SHORT") != "1" {
		t.Fatalf("WORKFLOW_SOAK_DURATION = %s, want at least %s", duration, workflowSoakMinimumDuration)
	}

	first := mustDefinition(t, "soak.workflow", "1")
	second := mustDefinition(t, "soak.workflow", "2")
	registry, err := workflow.CompileDefinitions(first, second)
	if err != nil {
		t.Fatalf("compile soak definitions: %v", err)
	}
	definitions := []workflow.Definition{first, second}

	runtime.GC()
	baselineHeap, baselineGoroutines := workflowSoakResources()
	peakHeap, peakGoroutines := baselineHeap, baselineGoroutines
	startedAt := time.Now()
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	ticker := time.NewTicker(workflowSoakBatchInterval)
	defer ticker.Stop()
	nextReport := startedAt.Add(time.Hour)
	var batches uint64

	for {
		workflowSoakBatch(t, registry, definitions, batches)
		batches++
		select {
		case <-deadline.C:
			runtime.GC()
			heap, goroutines := workflowSoakResources()
			peakHeap = max(peakHeap, heap)
			peakGoroutines = max(peakGoroutines, goroutines)
			workflowAssertSoakResources(t, baselineHeap, baselineGoroutines, heap, goroutines)
			t.Logf(
				"workflow_soak_result duration=%s batches=%d replayed_instances=%d completed_work=%d baseline_heap_bytes=%d peak_heap_bytes=%d baseline_goroutines=%d peak_goroutines=%d go=%s os=%s arch=%s",
				time.Since(startedAt).Round(time.Millisecond), batches,
				batches*workflowSoakReplayCount, batches*workflowSoakWorkCount,
				baselineHeap, peakHeap, baselineGoroutines, peakGoroutines,
				runtime.Version(), runtime.GOOS, runtime.GOARCH,
			)
			return
		case now := <-ticker.C:
			if now.Before(nextReport) {
				continue
			}
			runtime.GC()
			heap, goroutines := workflowSoakResources()
			peakHeap = max(peakHeap, heap)
			peakGoroutines = max(peakGoroutines, goroutines)
			workflowAssertSoakResources(t, baselineHeap, baselineGoroutines, heap, goroutines)
			t.Logf(
				"workflow_soak_checkpoint elapsed=%s batches=%d heap_bytes=%d goroutines=%d",
				time.Since(startedAt).Round(time.Second), batches, heap, goroutines,
			)
			nextReport = now.Add(time.Hour)
		}
	}
}

func workflowSoakBatch(
	t *testing.T,
	registry *workflow.Registry,
	definitions []workflow.Definition,
	batch uint64,
) {
	t.Helper()
	baseTime := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	for index := range workflowSoakReplayCount {
		generation := batch*workflowSoakReplayCount + uint64(index)
		current := definitions[generation%uint64(len(definitions))]
		next := definitions[(generation+1)%uint64(len(definitions))]
		instanceID := fmt.Sprintf("soak-instance-%d", generation)
		successorID := fmt.Sprintf("soak-instance-%d", generation+1)
		occurredAt := baseTime.Add(time.Duration(generation*2) * time.Nanosecond)
		events := []workflow.HistoryEvent{
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 1, InstanceID: instanceID, Kind: workflow.EventInstanceStarted,
				OccurredAt: occurredAt, Definition: current.Reference(),
			}),
			mustHistoryEvent(t, workflow.HistoryEventSpec{
				Sequence: 2, InstanceID: instanceID, Kind: workflow.EventContinuedAsNew,
				OccurredAt: occurredAt.Add(time.Nanosecond), Definition: next.Reference(),
				SuccessorID: successorID,
			}),
		}
		instance, err := workflow.Replay(registry, events)
		if err != nil || instance.Status() != workflow.StatusContinuedAsNew ||
			instance.Sequence() != 2 || instance.SuccessorID() != successorID {
			t.Fatalf("soak replay generation %d = %#v, %v", generation, instance, err)
		}
		replayed, err := workflow.Replay(registry, events)
		if err != nil || replayed.SnapshotDigest() != instance.SnapshotDigest() {
			t.Fatalf("soak deterministic replay generation %d = %#v, %v", generation, replayed, err)
		}
	}

	now := baseTime.Add(time.Duration(batch) * time.Second)
	leases := make([]workflow.WorkLease, workflowSoakWorkCount)
	for index := range leases {
		leases[index] = mustWorkerLease(
			t,
			now,
			fmt.Sprintf("soak-work-%d-%d", batch, index),
			fmt.Sprintf("tenant-%d", index%7),
		)
	}
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	store := &workerStore{claims: [][]workflow.WorkLease{leases}}
	processor := &countingProcessor{cancel: cancel, target: len(leases)}
	worker, err := workflow.NewWorker(workflow.WorkerConfig{
		Store: store, Processor: processor, Clock: hardeningClock{now: now}, Owner: "worker-1",
		MaxConcurrent: workflowSoakConcurrency, ClaimLimit: workflowSoakConcurrency,
		LeaseDuration: time.Minute, RenewEvery: 20 * time.Second,
		PollInterval: time.Millisecond, FinalizeTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("construct soak worker batch %d: %v", batch, err)
	}
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("run soak worker batch %d: %v", batch, err)
	}
	if processor.active != 0 || processor.maximum > workflowSoakConcurrency ||
		processor.completed != workflowSoakWorkCount || len(store.completions) != workflowSoakWorkCount {
		t.Fatalf(
			"soak worker batch %d = active %d maximum %d completed %d durable %d",
			batch, processor.active, processor.maximum, processor.completed, len(store.completions),
		)
	}
}

func workflowSoakResources() (uint64, int) {
	var statistics runtime.MemStats
	runtime.ReadMemStats(&statistics)
	return statistics.HeapAlloc, runtime.NumGoroutine()
}

func workflowAssertSoakResources(
	t *testing.T,
	baselineHeap uint64,
	baselineGoroutines int,
	heap uint64,
	goroutines int,
) {
	t.Helper()
	if heap > baselineHeap+workflowSoakHeapLimit {
		t.Fatalf(
			"soak retained heap = %d bytes, baseline %d limit %d",
			heap, baselineHeap, workflowSoakHeapLimit,
		)
	}
	if goroutines > baselineGoroutines+workflowSoakGoroutineLimit {
		t.Fatalf(
			"soak retained goroutines = %d, baseline %d limit %d",
			goroutines, baselineGoroutines, workflowSoakGoroutineLimit,
		)
	}
}
