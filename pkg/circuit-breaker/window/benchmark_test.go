package window_test

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/circuit-breaker/window"
)

func BenchmarkCountRollover(b *testing.B) {
	rolling, err := window.NewCount(100)
	if err != nil {
		b.Fatalf("NewCount() error = %v", err)
	}
	record := window.Record{Class: window.Failure, Slow: true}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = rolling.Add(record)
	}
}

func BenchmarkTimeRollover(b *testing.B) {
	rolling, err := window.NewTime(time.Second, 60)
	if err != nil {
		b.Fatalf("NewTime() error = %v", err)
	}
	record := window.Record{Class: window.Success}
	now := time.Unix(100, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		_ = rolling.Add(now.Add(time.Duration(index)*time.Second), record)
	}
}

func BenchmarkTimeSnapshot(b *testing.B) {
	rolling, err := window.NewTime(time.Second, 60)
	if err != nil {
		b.Fatalf("NewTime() error = %v", err)
	}
	now := time.Unix(100, 0)
	for index := range 60 {
		_ = rolling.Add(now.Add(time.Duration(index)*time.Second), window.Record{Class: window.Success})
	}
	now = now.Add(59 * time.Second)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = rolling.Snapshot(now)
	}
}
