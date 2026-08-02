package hedge_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
)

func TestCompositionAccountsEveryAttemptAndSharesHardAmplificationBound(t *testing.T) {
	t.Parallel()

	budget, err := hedge.NewOutstandingBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	config := validConfig()
	config.MaxHedges = 2
	config.Schedule = []time.Duration{time.Millisecond, time.Millisecond}
	config.Delay = 0
	config.Budget = budget
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	var breakerObservations atomic.Uint32
	var ratePermits atomic.Uint32
	factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(ctx context.Context) (string, error) {
			breakerObservations.Add(1) // breaker inside: one observation per attempt
			ratePermits.Add(1)         // rate limiter inside: one permit per attempt
			if info.Ordinal == 0 {
				<-ctx.Done()
				return "loser", ctx.Err()
			}
			return "winner", nil
		}, "pod", nil
	})
	value, report, gotErr := hedge.Do(context.Background(), policy, factory)
	if gotErr != nil || value != "winner" {
		t.Fatalf("Do() = (%q, %v)", value, gotErr)
	}
	if err := report.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if report.AttemptsStarted != 2 || report.HedgesStarted != 1 || breakerObservations.Load() != 2 || ratePermits.Load() != 2 {
		t.Fatalf("accounting report=%+v breaker=%d rate=%d", report, breakerObservations.Load(), ratePermits.Load())
	}
}
