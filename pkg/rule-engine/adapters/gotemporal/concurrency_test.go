package ruleenginetemporal_test

import (
	"context"
	"sync"
	"testing"
	"time"

	ruleenginetemporal "github.com/faustbrian/golib/pkg/rule-engine/adapters/gotemporal"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

func TestOperatorsAreSafeForConcurrentReuse(t *testing.T) {
	t.Parallel()

	operator := temporalOperatorByName(t, ruleenginetemporal.OpPeriodContains)
	base := time.Date(2026, time.July, 19, 10, 0, 0, 0, time.UTC)
	period := mustEncodedPeriod(
		t,
		mustExternalPeriod(t, base, base.Add(time.Hour), temporal.ClosedOpen),
	)
	point := mustEncodedInstant(t, base.Add(time.Minute))

	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				matched, err := operator.Evaluate(context.Background(), period, point)
				if err != nil || !matched {
					t.Errorf("Evaluate() = %t, %v", matched, err)
					return
				}
			}
		}()
	}
	wait.Wait()
}
