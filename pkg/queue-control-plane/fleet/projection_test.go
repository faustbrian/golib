package fleet

import (
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
)

func TestProjectionBoundsQueueCardinality(t *testing.T) {
	t.Parallel()

	projection := NewProjection(1)

	projection.Observe(queue.Event{
		Kind:       queue.EventEnqueued,
		Backend:    "valkey",
		Queue:      "critical",
		OccurredAt: time.Unix(10, 0),
	})
	projection.Observe(queue.Event{
		Kind:       queue.EventHandlerSucceeded,
		Backend:    "valkey",
		Queue:      "critical",
		OccurredAt: time.Unix(11, 0),
		Duration:   25 * time.Millisecond,
	})
	projection.Observe(queue.Event{
		Kind:       queue.EventHandlerFailed,
		Backend:    "redis",
		Queue:      "bulk",
		OccurredAt: time.Unix(12, 0),
		Duration:   50 * time.Millisecond,
	})

	snapshot := projection.Snapshot()
	if len(snapshot.Queues) != 1 {
		t.Fatalf("len(Snapshot().Queues) = %d, want 1", len(snapshot.Queues))
	}

	critical := snapshot.Queues[0]
	if critical.Backend != "valkey" || critical.Queue != "critical" {
		t.Fatalf("Snapshot().Queues[0] = %q/%q, want valkey/critical", critical.Backend, critical.Queue)
	}
	if critical.Metrics.Enqueued != 1 || critical.Metrics.Succeeded != 1 {
		t.Fatalf("critical metrics = %+v, want one enqueue and success", critical.Metrics)
	}
	if critical.Metrics.Runtime != 25*time.Millisecond {
		t.Fatalf("critical runtime = %s, want 25ms", critical.Metrics.Runtime)
	}

	if snapshot.Overflow.Failed != 1 {
		t.Fatalf("overflow metrics = %+v, want one failure", snapshot.Overflow)
	}
	if snapshot.Overflow.Runtime != 50*time.Millisecond {
		t.Fatalf("overflow runtime = %s, want 50ms", snapshot.Overflow.Runtime)
	}
}

func TestProjectionSnapshotsQueuesDeterministically(t *testing.T) {
	t.Parallel()

	projection := NewProjection(3)
	for _, event := range []queue.Event{
		{Kind: queue.EventEnqueued, Backend: "valkey", Queue: "standard"},
		{Kind: queue.EventEnqueued, Backend: "redis", Queue: "critical"},
		{Kind: queue.EventEnqueued, Backend: "redis", Queue: "bulk"},
	} {
		projection.Observe(event)
	}

	snapshot := projection.Snapshot()
	want := []struct{ backend, queue string }{
		{backend: "redis", queue: "bulk"},
		{backend: "redis", queue: "critical"},
		{backend: "valkey", queue: "standard"},
	}
	for index, expected := range want {
		got := snapshot.Queues[index]
		if got.Backend != expected.backend || got.Queue != expected.queue {
			t.Fatalf(
				"Snapshot().Queues[%d] = %q/%q, want %s/%s",
				index,
				got.Backend,
				got.Queue,
				expected.backend,
				expected.queue,
			)
		}
	}
}

func TestProjectionAggregatesAllEventsWhenCapacityIsNegative(t *testing.T) {
	t.Parallel()

	projection := NewProjection(-1)
	projection.Observe(queue.Event{Kind: queue.EventEnqueued, Backend: "redis", Queue: "bulk"})

	snapshot := projection.Snapshot()
	if len(snapshot.Queues) != 0 {
		t.Fatalf("len(Snapshot().Queues) = %d, want 0", len(snapshot.Queues))
	}
	if snapshot.Overflow.Enqueued != 1 {
		t.Fatalf("Snapshot().Overflow.Enqueued = %d, want 1", snapshot.Overflow.Enqueued)
	}
}
