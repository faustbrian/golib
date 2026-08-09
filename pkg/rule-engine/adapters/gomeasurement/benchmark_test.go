package ruleenginemeasurement_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemeasurement "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomeasurement"
)

func BenchmarkQuantityComparison(b *testing.B) {
	operator := measurementOperatorByName(
		b,
		ruleenginemeasurement.OpQuantityGreaterThan,
	)
	leftQuantity := measurement.MustNew(decimal.MustParse("1001"), measurement.Gram)
	rightQuantity := measurement.MustNew(decimal.MustParse("1"), measurement.Kilogram)
	left := ruleenginemeasurement.Quantity(leftQuantity)
	right := ruleenginemeasurement.Quantity(rightQuantity)
	ctx := context.Background()

	b.Run("adapter", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			matched, err := operator.Evaluate(ctx, left, right)
			if err != nil || !matched {
				b.Fatalf("Evaluate() = %t, %v", matched, err)
			}
		}
	})
	b.Run("direct-measurement", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			comparison, err := leftQuantity.Compare(rightQuantity, measurement.ExactConversion())
			if err != nil || comparison <= 0 {
				b.Fatalf("Compare() = %d, %v", comparison, err)
			}
		}
	})
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
