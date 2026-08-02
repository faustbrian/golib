package concurrencylimit_test

import (
	"testing"
	"time"

	concurrencylimit "github.com/faustbrian/golib/pkg/concurrency-limit"
)

func TestVegasSimulationConvergesAndRecoversWithReproducibleWorkloads(t *testing.T) {
	t.Parallel()

	algorithm, err := concurrencylimit.NewVegasAlgorithm(concurrencylimit.VegasConfig{
		Alpha: 2, Beta: 4, Increase: 1, Decrease: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	algorithm.Reset(2)
	limit := 2
	run := func(capacity, windows int) {
		for range windows {
			recent := 10 * time.Millisecond
			if limit > capacity {
				recent += time.Duration(limit-capacity) * 5 * time.Millisecond
			}
			decision := algorithm.Update(concurrencylimit.Window{
				CurrentLimit: limit, Samples: 50, MaxInFlight: limit,
				RecentLatency: recent, BaselineLatency: 10 * time.Millisecond,
				Throughput: float64(min(limit, capacity)) / recent.Seconds(),
			})
			limit = min(max(decision.Limit, 1), 64)
		}
	}

	run(12, 40)
	if limit < 9 || limit > 14 {
		t.Fatalf("steady capacity limit = %d, want near 12", limit)
	}
	run(5, 20)
	if limit > 8 {
		t.Fatalf("collapsed capacity limit = %d, want bounded reduction", limit)
	}
	run(18, 40)
	if limit < 14 || limit > 20 {
		t.Fatalf("recovered capacity limit = %d, want near 18", limit)
	}
}

func TestAlgorithmsRemainDeterministicAcrossNoisyWorkloadClasses(t *testing.T) {
	t.Parallel()

	type workload struct {
		name      string
		latencies []time.Duration
	}
	workloads := []workload{
		{"constant", []time.Duration{10, 10, 10, 10}},
		{"bursty", []time.Duration{10, 40, 10, 40}},
		{"bimodal", []time.Duration{5, 50, 5, 50}},
		{"heavy-tail", []time.Duration{5, 6, 7, 100}},
		{"periodic", []time.Duration{10, 20, 10, 20}},
		{"sparse", []time.Duration{10}},
	}
	for _, workload := range workloads {
		t.Run(workload.name, func(t *testing.T) {
			first := simulateGradient(t, workload.latencies)
			second := simulateGradient(t, workload.latencies)
			if first != second {
				t.Fatalf("deterministic runs differ: %+v != %+v", first, second)
			}
			if first.Limit < 1 || first.Limit > 32 {
				t.Fatalf("limit = %d, outside bounds", first.Limit)
			}
		})
	}
}

func simulateGradient(t *testing.T, latencies []time.Duration) concurrencylimit.Decision {
	t.Helper()
	algorithm, err := concurrencylimit.NewGradient2Algorithm(concurrencylimit.Gradient2Config{
		LongWindow: 10, Smoothing: 0.2, Tolerance: 1.5, MinGradient: 0.5, QueueSize: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	algorithm.Reset(8)
	decision := concurrencylimit.Decision{Limit: 8}
	for _, milliseconds := range latencies {
		decision = algorithm.Update(concurrencylimit.Window{
			CurrentLimit: decision.Limit, Samples: 20, MaxInFlight: decision.Limit,
			RecentLatency: milliseconds * time.Millisecond, BaselineLatency: 5 * time.Millisecond,
		})
		decision.Limit = min(max(decision.Limit, 1), 32)
	}
	return decision
}
