package ruleenginemeasurement_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemeasurement "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomeasurement"
)

func BenchmarkQuantityGreaterThanWithConversion(b *testing.B) {
	operator := measurementOperatorByName(
		b,
		ruleenginemeasurement.OpQuantityGreaterThan,
	)
	left := ruleenginemeasurement.Quantity(
		measurement.MustNew(decimal.MustParse("1001"), measurement.Gram),
	)
	right := ruleenginemeasurement.Quantity(
		measurement.MustNew(decimal.MustParse("1"), measurement.Kilogram),
	)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		matched, err := operator.Evaluate(ctx, left, right)
		if err != nil || !matched {
			b.Fatalf("Evaluate() = %t, %v", matched, err)
		}
	}
}

func measurementOperatorByName(
	tb testing.TB,
	name ruleengine.OperatorName,
) ruleengine.Operator {
	tb.Helper()
	for _, operator := range ruleenginemeasurement.Operators() {
		if operator.Name() == name {
			return operator
		}
	}
	tb.Fatalf("operator %q is missing", name)
	panic("unreachable")
}
