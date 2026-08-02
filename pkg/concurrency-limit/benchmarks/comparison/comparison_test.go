package comparison_test

import (
	"context"
	"testing"
	"time"

	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
	"github.com/faustbrian/golib/pkg/concurrency-limit/benchmarks/comparison/internal/netflix"
	"github.com/platinummonkey/go-concurrency-limits/core"
	platinumlimit "github.com/platinummonkey/go-concurrency-limits/limit"
)

const (
	minimumLimit = 1
	maximumLimit = 64
	initialLimit = 16
)

func TestPinnedImplementationsRemainBoundedOnIdenticalGradientTrace(t *testing.T) {
	local := mustLocalGradient(t)
	netflix := netflix.New(initialLimit, minimumLimit, maximumLimit)
	platinum := mustPlatinumGradient(t)
	localCurrent := initialLimit

	for update := range 240 {
		rtt := 10 * time.Millisecond
		if update >= 80 && update < 150 {
			rtt = 45 * time.Millisecond
		}
		window := concurrencylimit.Window{
			CurrentLimit:    localCurrent,
			Samples:         32,
			MaxInFlight:     localCurrent,
			RecentLatency:   rtt,
			BaselineLatency: 10 * time.Millisecond,
		}

		localDecision := local.Update(window)
		localCurrent = clamp(localDecision.Limit)
		netflix.Update(float64(rtt), window.MaxInFlight)
		platinum.OnSample(int64(update), int64(rtt), platinum.EstimatedLimit(), false)

		assertBounded(t, "local", update, localCurrent)
		assertBounded(t, "netflix-reference", update, netflix.Limit())
		assertBounded(t, "platinum", update, platinum.EstimatedLimit())
		if localCurrent != netflix.Limit() {
			t.Fatalf("update %d local limit = %d, Netflix reference = %d", update, localCurrent, netflix.Limit())
		}
	}
}

func TestFailsafePublicPermitPathRemainsWithinConfiguredBounds(t *testing.T) {
	limiter := newFailsafeLimiter()
	for sample := range 500 {
		permit, ok := limiter.TryAcquirePermit()
		if !ok {
			continue
		}
		if sample%29 == 0 {
			permit.Drop()
		} else {
			permit.Record()
		}
		assertBounded(t, "failsafe-go", sample, limiter.Limit())
	}
}

func BenchmarkGradientUpdate(b *testing.B) {
	b.Run("LocalGradient2", func(b *testing.B) {
		algorithm := mustLocalGradient(b)
		window := concurrencylimit.Window{
			CurrentLimit: initialLimit, Samples: 32, MaxInFlight: initialLimit,
			RecentLatency: 12 * time.Millisecond, BaselineLatency: 10 * time.Millisecond,
		}
		b.ReportAllocs()
		for range b.N {
			_ = algorithm.Update(window)
		}
	})

	b.Run("NetflixReferencePort", func(b *testing.B) {
		algorithm := netflix.New(initialLimit, minimumLimit, maximumLimit)
		b.ReportAllocs()
		for range b.N {
			algorithm.Update(float64(12*time.Millisecond), initialLimit)
		}
	})

	b.Run("PlatinumGradient2", func(b *testing.B) {
		algorithm := mustPlatinumGradient(b)
		b.ReportAllocs()
		for index := range b.N {
			algorithm.OnSample(int64(index), int64(12*time.Millisecond), initialLimit, false)
		}
	})
}

func BenchmarkPermitLifecycle(b *testing.B) {
	b.Run("Local", func(b *testing.B) {
		algorithm := mustLocalGradient(b)
		limiter, err := concurrencylimit.New(concurrencylimit.Config{
			MinLimit: minimumLimit, MaxLimit: maximumLimit, InitialLimit: initialLimit,
			Algorithm: algorithm,
			Sampling: concurrencylimit.SamplingConfig{
				MinDuration: time.Nanosecond, MaxDuration: time.Nanosecond,
				MinSamples: 1, Capacity: 1, Quantile: 0.9, BaselineSmoothing: 0.1,
				MaxIncrease: maximumLimit, MaxDecrease: maximumLimit,
			},
		})
		if err != nil {
			b.Fatal(err)
		}
		ctx := context.Background()
		b.ReportAllocs()
		for range b.N {
			permit, acquireErr := limiter.Acquire(ctx)
			if acquireErr != nil {
				b.Fatal(acquireErr)
			}
			if completeErr := permit.Complete(concurrencylimit.OutcomeSuccess); completeErr != nil {
				b.Fatal(completeErr)
			}
		}
	})

	b.Run("FailsafeGo", func(b *testing.B) {
		limiter := newFailsafeLimiter()
		b.ReportAllocs()
		for range b.N {
			permit, ok := limiter.TryAcquirePermit()
			if !ok {
				b.Fatal("TryAcquirePermit rejected an uncontended attempt")
			}
			permit.Record()
		}
	})
}

type testingTB interface {
	Helper()
	Fatal(args ...any)
}

func mustLocalGradient(tb testingTB) concurrencylimit.Algorithm {
	tb.Helper()
	algorithm, err := concurrencylimit.NewGradient2Algorithm(concurrencylimit.Gradient2Config{
		LongWindow: 20, Smoothing: 0.2, Tolerance: 1.5, MinGradient: 0.5, QueueSize: 4,
	})
	if err != nil {
		tb.Fatal(err)
	}
	algorithm.Reset(initialLimit)
	return algorithm
}

func mustPlatinumGradient(tb testingTB) *platinumlimit.Gradient2Limit {
	tb.Helper()
	algorithm, err := platinumlimit.NewGradient2Limit(
		"comparison", initialLimit, maximumLimit, minimumLimit,
		func(int) int { return 4 }, 0.2, 20,
		platinumlimit.NoopLimitLogger{}, core.EmptyMetricRegistryInstance,
	)
	if err != nil {
		tb.Fatal(err)
	}
	return algorithm
}

func newFailsafeLimiter() adaptivelimiter.AdaptiveLimiter[any] {
	return adaptivelimiter.NewBuilder[any]().
		WithLimits(minimumLimit, maximumLimit, initialLimit).
		WithRecentWindow(0, 0, 1).
		Build()
}

func assertBounded(t *testing.T, name string, update, limit int) {
	t.Helper()
	if limit < minimumLimit || limit > maximumLimit {
		t.Fatalf("%s update %d limit = %d, want [%d,%d]", name, update, limit, minimumLimit, maximumLimit)
	}
}

func clamp(limit int) int { return min(max(limit, minimumLimit), maximumLimit) }
