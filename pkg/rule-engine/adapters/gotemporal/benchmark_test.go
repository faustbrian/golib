package ruleenginetemporal_test

import (
	"context"
	"testing"
	"time"

	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginetemporal "github.com/faustbrian/golib/pkg/rule-engine/adapters/gotemporal"
	temporal "github.com/faustbrian/golib/pkg/temporal"
	"github.com/faustbrian/golib/pkg/temporal/instant"
)

func BenchmarkPeriodContainsInstant(b *testing.B) {
	operator := temporalOperatorByName(
		b,
		ruleenginetemporal.OpPeriodContains,
	)
	start := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	period, err := instant.New(
		start,
		start.Add(time.Hour),
		temporal.ClosedOpen,
	)
	if err != nil {
		b.Fatal(err)
	}
	left := ruleenginetemporal.Period(period)
	right := ruleenginetemporal.Instant(start.Add(time.Minute))
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		matched, evaluateErr := operator.Evaluate(ctx, left, right)
		if evaluateErr != nil || !matched {
			b.Fatalf("Evaluate() = %t, %v", matched, evaluateErr)
		}
	}
}

func temporalOperatorByName(
	tb testing.TB,
	name ruleengine.OperatorName,
) ruleengine.Operator {
	tb.Helper()
	for _, operator := range ruleenginetemporal.Operators() {
		if operator.Name() == name {
			return operator
		}
	}
	tb.Fatalf("operator %q is missing", name)
	panic("unreachable")
}
