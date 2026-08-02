package concurrencylimit

import (
	"context"
	"testing"
	"time"
)

func FuzzLimitHistoriesRemainFiniteAndBounded(fuzz *testing.F) {
	fuzz.Add([]byte{1, 2, 3, 4, 5})
	fuzz.Add([]byte{255, 0, 127, 64})
	fuzz.Fuzz(func(t *testing.T, history []byte) {
		algorithm, err := NewGradient2Algorithm(Gradient2Config{
			LongWindow: 10, Smoothing: 0.2, Tolerance: 1.5, MinGradient: 0.5, QueueSize: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		limiter := mustInternalLimiter(t, Config{
			MinLimit: 1, MaxLimit: 100, InitialLimit: 10, Algorithm: algorithm,
			Sampling: SamplingConfig{MaxIncrease: 3, MaxDecrease: 5},
		})
		for index, value := range history {
			snapshot := limiter.Snapshot()
			window := Window{
				CurrentLimit: snapshot.Limit, Samples: 20, MaxInFlight: int(value)%100 + 1,
				RecentLatency:   time.Duration(value+1) * time.Millisecond,
				BaselineLatency: time.Duration(value%20+1) * time.Millisecond,
			}
			if index%7 == 0 {
				window.Overloads = 1
			}
			limiter.mu.Lock()
			limiter.adapting = true
			limiter.mu.Unlock()
			_ = limiter.applyWindow(window, time.Unix(int64(index), 0))
			after := limiter.Snapshot()
			if after.Limit < 1 || after.Limit > 100 || !validAlgorithmState(after.Algorithm) {
				t.Fatalf("history index %d produced %+v", index, after)
			}
		}
	})
}

func FuzzPermitSequencesRecordAtMostOneOutcome(fuzz *testing.F) {
	fuzz.Add([]byte{0, 1, 2, 3, 4})
	fuzz.Fuzz(func(t *testing.T, sequence []byte) {
		limiter := mustInternalLimiter(t, Config{MinLimit: 1, MaxLimit: 8, InitialLimit: 8, Algorithm: NewFixedAlgorithm()})
		for _, value := range sequence {
			permit, err := limiter.Acquire(context.Background())
			if err != nil {
				continue
			}
			outcome := Outcome(value % 5)
			if err = permit.Complete(outcome); err != nil {
				t.Fatal(err)
			}
			if err = permit.Complete(outcome); err != ErrPermitCompleted {
				t.Fatalf("duplicate error = %v", err)
			}
		}
		if snapshot := limiter.Snapshot(); snapshot.InFlight != 0 {
			t.Fatalf("Snapshot() = %+v", snapshot)
		}
	})
}
