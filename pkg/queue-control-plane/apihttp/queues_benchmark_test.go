package apihttp

import (
	"fmt"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func BenchmarkQueuePageTwoHundredMeasurements(b *testing.B) {
	observedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	items := make([]queue.QueueStatus, queue.MaxStatusPageSize)
	for index := range items {
		items[index] = queue.QueueStatus{
			Backend:    "valkey-streams",
			Queue:      fmt.Sprintf("queue-%03d", index),
			ObservedAt: observedAt,
			Metrics: queue.QueueMetrics{
				Depth:            queue.Measurement[int64]{Value: int64(index), Supported: true},
				Lag:              queue.Measurement[int64]{Value: int64(index), Supported: true},
				Pending:          queue.Measurement[int64]{Value: int64(index), Supported: true},
				OldestAge:        queue.Measurement[time.Duration]{Value: time.Minute, Supported: true},
				Throughput:       queue.Measurement[float64]{Value: 100.5, Supported: true},
				Runtime:          queue.Measurement[time.Duration]{Value: time.Second, Supported: true},
				Succeeded:        queue.Measurement[uint64]{Value: 1_000, Supported: true},
				Failed:           queue.Measurement[uint64]{Value: 10, Supported: true},
				Retried:          queue.Measurement[uint64]{Value: 5, Supported: true},
				Reclaimed:        queue.Measurement[uint64]{Value: 2, Supported: true},
				DeadLettered:     queue.Measurement[uint64]{Value: 1, Supported: true},
				SettlementErrors: queue.Measurement[uint64]{Supported: true},
			},
		}
	}
	page := queue.QueueStatusPage{Items: items, NextCursor: "next"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if converted := queuePage(page); len(converted.Queues) != queue.MaxStatusPageSize {
			b.Fatalf("queuePage() queues = %d", len(converted.Queues))
		}
	}
}
