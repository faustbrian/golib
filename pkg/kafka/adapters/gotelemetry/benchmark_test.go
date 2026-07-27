package gotelemetry

import (
	"context"
	"testing"
	"time"

	kafka "github.com/faustbrian/golib/pkg/kafka"
)

func BenchmarkObserver(b *testing.B) {
	instrumentation, err := New(Config{Runtime: completeTestRuntime()})
	if err != nil {
		b.Fatalf("New() error = %v", err)
	}
	observer := instrumentation.Observer()
	observation := kafka.Observation{
		Kind:           kafka.ObservationProduceRecord,
		StartedAt:      time.Unix(1, 0),
		Duration:       time.Millisecond,
		Partition:      1,
		PartitionKnown: true,
		Offset:         42,
		OffsetKnown:    true,
		RecordCount:    1,
		RecordBytes:    128,
		Succeeded:      true,
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := observer(ctx, observation); err != nil {
			b.Fatal(err)
		}
	}
}
