package concurrencylimit_test

import (
	"math"
	"testing"
	"time"

	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
)

func TestAIMDReferenceEquation(t *testing.T) {
	t.Parallel()

	algorithm, err := concurrencylimit.NewAIMDAlgorithm(concurrencylimit.AIMDConfig{
		Increase:       1,
		DecreaseFactor: 0.5,
		LatencyLimit:   100 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewAIMDAlgorithm() error = %v", err)
	}
	algorithm.Reset(10)

	increased := algorithm.Update(concurrencylimit.Window{
		CurrentLimit:  10,
		MaxInFlight:   5,
		RecentLatency: 50 * time.Millisecond,
	})
	if increased.Limit != 11 {
		t.Fatalf("increase limit = %d, want 11", increased.Limit)
	}

	decreased := algorithm.Update(concurrencylimit.Window{
		CurrentLimit:  10,
		MaxInFlight:   10,
		RecentLatency: 101 * time.Millisecond,
	})
	if decreased.Limit != 5 {
		t.Fatalf("decrease limit = %d, want 5", decreased.Limit)
	}
}

func TestVegasReferenceQueueEquationAndThroughputSignal(t *testing.T) {
	t.Parallel()

	algorithm, err := concurrencylimit.NewVegasAlgorithm(concurrencylimit.VegasConfig{
		Alpha: 2, Beta: 4, Increase: 1, Decrease: 1,
	})
	if err != nil {
		t.Fatalf("NewVegasAlgorithm() error = %v", err)
	}
	algorithm.Reset(10)

	lowQueue := algorithm.Update(concurrencylimit.Window{
		CurrentLimit: 10, MaxInFlight: 10,
		BaselineLatency: 90 * time.Millisecond, RecentLatency: 100 * time.Millisecond,
	})
	if lowQueue.Limit != 11 || lowQueue.State.QueueEstimate != 1 {
		t.Fatalf("low queue decision = %+v", lowQueue)
	}
	highQueue := algorithm.Update(concurrencylimit.Window{
		CurrentLimit: 10, MaxInFlight: 10,
		BaselineLatency: 40 * time.Millisecond, RecentLatency: 100 * time.Millisecond,
	})
	if highQueue.Limit != 9 || highQueue.State.QueueEstimate != 6 {
		t.Fatalf("high queue decision = %+v", highQueue)
	}
	plateau := algorithm.Update(concurrencylimit.Window{
		CurrentLimit: 10, MaxInFlight: 10, PreviousMaxInFlight: 8,
		BaselineLatency: 90 * time.Millisecond, RecentLatency: 100 * time.Millisecond,
		Throughput: 80, PreviousThroughput: 100,
	})
	if plateau.Limit != 9 || plateau.State.Reason != "throughput" {
		t.Fatalf("throughput plateau decision = %+v", plateau)
	}
}

func TestGradient2ReferenceEquation(t *testing.T) {
	t.Parallel()

	algorithm, err := concurrencylimit.NewGradient2Algorithm(concurrencylimit.Gradient2Config{
		LongWindow: 10, Smoothing: 0.2, Tolerance: 1, MinGradient: 0.5, QueueSize: 0,
	})
	if err != nil {
		t.Fatalf("NewGradient2Algorithm() error = %v", err)
	}
	algorithm.Reset(10)
	first := algorithm.Update(concurrencylimit.Window{
		CurrentLimit: 10, MaxInFlight: 10, RecentLatency: 100 * time.Millisecond,
	})
	if first.Limit != 10 || first.State.Gradient != 1 {
		t.Fatalf("initial decision = %+v", first)
	}
	second := algorithm.Update(concurrencylimit.Window{
		CurrentLimit: 10, MaxInFlight: 10, RecentLatency: 200 * time.Millisecond,
	})
	if second.Limit != 9 || second.State.Gradient <= 0.5 || second.State.Gradient >= 1 {
		t.Fatalf("loaded decision = %+v", second)
	}
}

func TestGradient2MatchesNetflixWarmupAndFractionalEstimate(t *testing.T) {
	t.Parallel()

	algorithm, err := concurrencylimit.NewGradient2Algorithm(concurrencylimit.Gradient2Config{
		LongWindow: 10, Smoothing: 0.2, Tolerance: 1, MinGradient: 0.5, QueueSize: 0,
	})
	if err != nil {
		t.Fatalf("NewGradient2Algorithm() error = %v", err)
	}
	algorithm.Reset(10)

	windows := []concurrencylimit.Window{
		{CurrentLimit: 10, MaxInFlight: 10, RecentLatency: 100 * time.Millisecond},
		{CurrentLimit: 10, MaxInFlight: 10, RecentLatency: 200 * time.Millisecond},
		{CurrentLimit: 9, MaxInFlight: 9, RecentLatency: 200 * time.Millisecond},
	}
	wantLimits := []int{10, 9, 9}
	wantEstimates := []float64{10, 9.5, 9.183333333333334}
	for index, window := range windows {
		decision := algorithm.Update(window)
		if decision.Limit != wantLimits[index] {
			t.Fatalf("update %d limit = %d, want %d", index, decision.Limit, wantLimits[index])
		}
		if math.Abs(decision.State.Estimate-wantEstimates[index]) > 1e-9 {
			t.Fatalf("update %d estimate = %.12f, want %.12f", index, decision.State.Estimate, wantEstimates[index])
		}
	}
}

func TestDefaultAlgorithmIsConservativeVegasProfile(t *testing.T) {
	t.Parallel()

	algorithm := concurrencylimit.NewDefaultAlgorithm()
	algorithm.Reset(10)
	decision := algorithm.Update(concurrencylimit.Window{
		CurrentLimit: 10, MaxInFlight: 10,
		BaselineLatency: 90 * time.Millisecond, RecentLatency: 100 * time.Millisecond,
	})
	if algorithm.Name() != "vegas" || decision.Limit != 11 {
		t.Fatalf("default algorithm = %q, decision = %+v", algorithm.Name(), decision)
	}
}
