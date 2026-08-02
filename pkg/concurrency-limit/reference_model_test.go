package concurrencylimit_test

import (
	"math"
	"math/rand"
	"testing"
	"time"

	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
)

func TestEveryAlgorithmUpdateMatchesDeterministicReference(t *testing.T) {
	t.Parallel()

	random := rand.New(rand.NewSource(0x5eed))
	t.Run("aimd", func(t *testing.T) {
		config := concurrencylimit.AIMDConfig{
			Increase: 3, DecreaseFactor: 0.7, LatencyLimit: 80 * time.Millisecond,
		}
		algorithm, err := concurrencylimit.NewAIMDAlgorithm(config)
		if err != nil {
			t.Fatal(err)
		}
		algorithm.Reset(16)
		limit := 16
		for update := range 512 {
			window := generatedReferenceWindow(random, limit, update)
			got := algorithm.Update(window)
			want := referenceAIMD(config, window)
			assertReferenceDecision(t, update, got, want)
			limit = clampReferenceLimit(got.Limit)
		}
	})

	t.Run("vegas", func(t *testing.T) {
		config := concurrencylimit.VegasConfig{Alpha: 2, Beta: 5, Increase: 2, Decrease: 3}
		algorithm, err := concurrencylimit.NewVegasAlgorithm(config)
		if err != nil {
			t.Fatal(err)
		}
		algorithm.Reset(16)
		limit := 16
		for update := range 512 {
			window := generatedReferenceWindow(random, limit, update)
			got := algorithm.Update(window)
			want := referenceVegas(config, window)
			assertReferenceDecision(t, update, got, want)
			limit = clampReferenceLimit(got.Limit)
		}
	})

	t.Run("gradient2", func(t *testing.T) {
		config := concurrencylimit.Gradient2Config{
			LongWindow: 20, Smoothing: 0.2, Tolerance: 1.5, MinGradient: 0.5, QueueSize: 2,
		}
		algorithm, err := concurrencylimit.NewGradient2Algorithm(config)
		if err != nil {
			t.Fatal(err)
		}
		algorithm.Reset(16)
		reference := gradient2Reference{estimate: 16}
		limit := 16
		for update := range 512 {
			window := generatedReferenceWindow(random, limit, update)
			got := algorithm.Update(window)
			want := reference.update(config, window)
			assertReferenceDecision(t, update, got, want)
			if math.Abs(got.State.Estimate-want.State.Estimate) > 1e-9 ||
				math.Abs(got.State.Gradient-want.State.Gradient) > 1e-12 {
				t.Fatalf("update %d state = %+v, want %+v", update, got.State, want.State)
			}
			limit = clampReferenceLimit(got.Limit)
		}
	})
}

func generatedReferenceWindow(random *rand.Rand, limit, update int) concurrencylimit.Window {
	baseline := time.Duration(5+random.Intn(20)) * time.Millisecond
	recent := baseline + time.Duration(random.Intn(80))*time.Millisecond
	throughput := 50 + random.Float64()*500
	previousThroughput := 50 + random.Float64()*500
	maxInFlight := random.Intn(max(limit+2, 2))
	previousMax := random.Intn(max(limit+2, 2))
	overloads := uint64(0)
	if update%17 == 0 {
		overloads = 1
	}
	return concurrencylimit.Window{
		CurrentLimit: limit, Samples: 32, MaxInFlight: maxInFlight,
		RecentLatency: recent, BaselineLatency: baseline,
		Throughput: throughput, PreviousThroughput: previousThroughput,
		PreviousMaxInFlight: previousMax, Overloads: overloads,
	}
}

func referenceAIMD(config concurrencylimit.AIMDConfig, window concurrencylimit.Window) concurrencylimit.Decision {
	next := window.CurrentLimit
	reason := "application-limited"
	if window.Overloads > 0 || window.RecentLatency > config.LatencyLimit {
		next = int(math.Floor(float64(window.CurrentLimit) * config.DecreaseFactor))
		reason = "overload"
	} else if window.MaxInFlight >= (window.CurrentLimit+1)/2 {
		next += config.Increase
		reason = "capacity"
	}
	return concurrencylimit.Decision{Limit: next, State: concurrencylimit.AlgorithmState{Reason: reason}}
}

func referenceVegas(config concurrencylimit.VegasConfig, window concurrencylimit.Window) concurrencylimit.Decision {
	next := window.CurrentLimit
	queue := math.Max(0, math.Ceil(float64(window.CurrentLimit)*(1-float64(window.BaselineLatency)/float64(window.RecentLatency))))
	reason := "target-queue"
	switch {
	case window.Overloads > 0:
		next -= config.Decrease
		reason = "overload"
	case window.PreviousMaxInFlight > 0 && window.MaxInFlight > window.PreviousMaxInFlight &&
		window.PreviousThroughput > 0 && window.Throughput <= window.PreviousThroughput:
		next -= config.Decrease
		reason = "throughput"
	case window.MaxInFlight < (window.CurrentLimit+1)/2:
		reason = "application-limited"
	case queue < float64(config.Alpha):
		next += config.Increase
		reason = "low-queue"
	case queue > float64(config.Beta):
		next -= config.Decrease
		reason = "high-queue"
	}
	return concurrencylimit.Decision{
		Limit: next,
		State: concurrencylimit.AlgorithmState{Reason: reason, QueueEstimate: queue},
	}
}

type gradient2Reference struct {
	longRTT  float64
	samples  int
	estimate float64
}

func (reference *gradient2Reference) update(
	config concurrencylimit.Gradient2Config,
	window concurrencylimit.Window,
) concurrencylimit.Decision {
	shortRTT := float64(window.RecentLatency)
	if reference.samples < 10 {
		reference.samples++
		reference.longRTT += (shortRTT - reference.longRTT) / float64(reference.samples)
	} else {
		alpha := 2 / float64(config.LongWindow+1)
		reference.longRTT += alpha * (shortRTT - reference.longRTT)
	}
	if shortRTT > 0 && reference.longRTT/shortRTT > 2 {
		reference.longRTT *= 0.95
	}
	if int(math.Floor(reference.estimate)) != window.CurrentLimit {
		reference.estimate = float64(window.CurrentLimit)
	}
	next := reference.estimate
	gradient := 1.0
	reason := "application-limited"
	if float64(window.MaxInFlight) >= reference.estimate/2 && shortRTT > 0 {
		gradient = math.Max(config.MinGradient, math.Min(1, config.Tolerance*reference.longRTT/shortRTT))
		target := reference.estimate*gradient + float64(config.QueueSize)
		next = reference.estimate*(1-config.Smoothing) + target*config.Smoothing
		reason = "latency-gradient"
	}
	if window.Overloads > 0 && next >= float64(window.CurrentLimit) {
		next = float64(window.CurrentLimit - 1)
		reason = "overload"
	} else if window.PreviousMaxInFlight > 0 && window.MaxInFlight > window.PreviousMaxInFlight &&
		window.PreviousThroughput > 0 && window.Throughput <= window.PreviousThroughput &&
		next >= float64(window.CurrentLimit) {
		next = float64(window.CurrentLimit - 1)
		reason = "throughput"
	}
	reference.estimate = next
	return concurrencylimit.Decision{
		Limit: int(math.Floor(next)),
		State: concurrencylimit.AlgorithmState{Reason: reason, Estimate: next, Gradient: gradient},
	}
}

func assertReferenceDecision(t *testing.T, update int, got, want concurrencylimit.Decision) {
	t.Helper()
	if got.Limit != want.Limit || got.State.Reason != want.State.Reason {
		t.Fatalf("update %d decision = %+v, want %+v", update, got, want)
	}
	if want.State.QueueEstimate != 0 && got.State.QueueEstimate != want.State.QueueEstimate {
		t.Fatalf("update %d queue estimate = %v, want %v", update, got.State.QueueEstimate, want.State.QueueEstimate)
	}
}

func clampReferenceLimit(limit int) int { return min(max(limit, 1), 64) }
