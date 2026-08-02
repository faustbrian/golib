package hedge_test

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
)

func FuzzPolicyValidationRejectsUnsafeBounds(fuzz *testing.F) {
	fuzz.Add(uint8(1), int64(time.Millisecond), int64(time.Second), true)
	fuzz.Add(uint8(0), int64(0), int64(0), false)
	fuzz.Fuzz(func(t *testing.T, hedges uint8, delayNanos, timeoutNanos int64, replaySafe bool) {
		config := validConfig()
		config.MaxHedges = uint(hedges)
		config.Delay = time.Duration(delayNanos)
		config.TotalTimeout = time.Duration(timeoutNanos)
		config.ReplaySafe = replaySafe
		policy, err := hedge.NewPolicy(config)
		if err == nil && (policy == nil || hedges == 0 || uint(hedges) > hedge.MaxHedges || delayNanos <= 0 || timeoutNanos <= 0 || !replaySafe) {
			t.Fatalf("unsafe policy accepted: hedges=%d delay=%d timeout=%d replay=%v", hedges, delayNanos, timeoutNanos, replaySafe)
		}
	})
}

func FuzzOutstandingBudgetNeverExceedsLimit(fuzz *testing.F) {
	fuzz.Add(uint8(3), uint8(9))
	fuzz.Fuzz(func(t *testing.T, limitByte, acquisitions uint8) {
		limit := uint(limitByte%32) + 1
		budget, err := hedge.NewOutstandingBudget(limit)
		if err != nil {
			t.Fatal(err)
		}
		permits := make([]hedge.Permit, 0, limit)
		for index := uint8(0); index < acquisitions; index++ {
			permit, admitted := budget.TryAcquire("fuzz")
			if admitted {
				permits = append(permits, permit)
			}
			if budget.Outstanding() > uint64(limit) {
				t.Fatalf("outstanding = %d, limit = %d", budget.Outstanding(), limit)
			}
		}
		for _, permit := range permits {
			permit.Release()
		}
	})
}
