package concurrencylimit_test

import (
	"context"
	"testing"
	"time"

	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
)

func BenchmarkAcquireComplete(b *testing.B) {
	limiter, err := concurrencylimit.New(concurrencylimit.Config{
		MinLimit: 1, MaxLimit: 1, InitialLimit: 1, Algorithm: concurrencylimit.NewFixedAlgorithm(),
	})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		permit, acquireErr := limiter.Acquire(context.Background())
		if acquireErr != nil {
			b.Fatal(acquireErr)
		}
		if completeErr := permit.Complete(concurrencylimit.OutcomeSuccess); completeErr != nil {
			b.Fatal(completeErr)
		}
	}
}

func BenchmarkAlgorithmWindows(b *testing.B) {
	algorithms := map[string]concurrencylimit.Algorithm{
		"fixed": concurrencylimit.NewFixedAlgorithm(),
	}
	aimd, _ := concurrencylimit.NewAIMDAlgorithm(concurrencylimit.AIMDConfig{Increase: 1, DecreaseFactor: 0.9})
	vegas, _ := concurrencylimit.NewVegasAlgorithm(concurrencylimit.VegasConfig{Alpha: 2, Beta: 4, Increase: 1, Decrease: 1})
	gradient, _ := concurrencylimit.NewGradient2Algorithm(concurrencylimit.Gradient2Config{LongWindow: 600, Smoothing: 0.2, Tolerance: 1.5, MinGradient: 0.5, QueueSize: 4})
	algorithms["aimd"] = aimd
	algorithms["vegas"] = vegas
	algorithms["gradient2"] = gradient
	window := concurrencylimit.Window{CurrentLimit: 20, Samples: 50, MaxInFlight: 20, RecentLatency: 10 * time.Millisecond, BaselineLatency: 8 * time.Millisecond, Throughput: 2000}
	for name, algorithm := range algorithms {
		b.Run(name, func(b *testing.B) {
			algorithm.Reset(20)
			b.ReportAllocs()
			for b.Loop() {
				_ = algorithm.Update(window)
			}
		})
	}
}
