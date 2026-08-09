package ruleenginetemporal_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginetemporal "github.com/faustbrian/golib/pkg/rule-engine/adapters/gotemporal"
	temporal "github.com/faustbrian/golib/pkg/temporal"
)

func TestOperatorsAreSafeForConcurrentReuse(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.July, 19, 10, 0, 0, 0, time.UTC)
	period := mustEncodedPeriod(
		t,
		mustExternalPeriod(t, base, base.Add(time.Hour), temporal.ClosedOpen),
	)
	equal := mustEncodedPeriod(t, mustExternalPeriod(t, base, base.Add(time.Hour), temporal.ClosedOpen))
	point := mustEncodedInstant(t, base.Add(time.Minute))
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		operator    ruleengine.Operator
		left, right ruleengine.Value
		want        bool
		ctx         context.Context
		wantErr     error
	}{
		{temporalOperatorByName(t, ruleenginetemporal.OpPeriodEqual), period, equal, true, context.Background(), nil},
		{temporalOperatorByName(t, ruleenginetemporal.OpPeriodBefore), period, equal, false, context.Background(), nil},
		{temporalOperatorByName(t, ruleenginetemporal.OpPeriodAfter), period, equal, false, context.Background(), nil},
		{temporalOperatorByName(t, ruleenginetemporal.OpPeriodOverlaps), period, equal, true, context.Background(), nil},
		{temporalOperatorByName(t, ruleenginetemporal.OpPeriodContainsPeriod), period, equal, true, context.Background(), nil},
		{temporalOperatorByName(t, ruleenginetemporal.OpPeriodContains), period, point, true, context.Background(), nil},
		{temporalOperatorByName(t, ruleenginetemporal.OpPeriodEqual), period, equal, false, canceled, context.Canceled},
	}

	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range 100 {
				test := cases[index%len(cases)]
				matched, err := test.operator.Evaluate(test.ctx, test.left, test.right)
				if !errors.Is(err, test.wantErr) || matched != test.want {
					t.Errorf("%s Evaluate() = %t, %v", test.operator.Name(), matched, err)
					return
				}
				operators := ruleenginetemporal.Operators()
				signatures := operators[index%len(operators)].Signatures()
				operators[0] = nil
				signatures[0] = ruleengine.Signature{}
			}
		}()
	}
	wait.Wait()
}
