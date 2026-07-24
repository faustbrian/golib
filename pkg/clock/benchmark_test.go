package clock_test

import (
	"context"
	"testing"
	"time"

	clock "github.com/faustbrian/golib/pkg/clock"
	"github.com/faustbrian/golib/pkg/clock/manual"
)

func BenchmarkSystemNow(b *testing.B) {
	system := clock.System{}
	b.ReportAllocs()
	for b.Loop() {
		_ = system.Now()
	}
}

func BenchmarkSystemMeasure(b *testing.B) {
	system := clock.System{}
	measure := system.Measure()
	b.ReportAllocs()
	for b.Loop() {
		_ = measure()
	}
}

func BenchmarkManualTimerFanout(b *testing.B) {
	for _, count := range []int{1, 100, 10_000} {
		b.Run(time.Duration(count).String(), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				c, _ := manual.New(time.Unix(1, 0), manual.WithLimits(manual.Limits{MaxActive: count, MaxWorkPerAdvance: count}))
				for range count {
					_, _ = c.NewTimer(time.Second)
				}
				waiter, _ := c.Advance(time.Second)
				_, _ = waiter.Wait(context.Background())
			}
		})
	}
}

func BenchmarkManualColdStart(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = manual.New(time.Unix(1, 0))
	}
}

func BenchmarkManualNowContended(b *testing.B) {
	c, _ := manual.New(time.Unix(1, 0))
	b.ReportAllocs()
	b.RunParallel(func(parallel *testing.PB) {
		for parallel.Next() {
			_ = c.Now()
		}
	})
}
