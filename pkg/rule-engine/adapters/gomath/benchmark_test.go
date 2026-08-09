package ruleenginemath_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemath "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomath"
)

func BenchmarkDecimalComparison(b *testing.B) {
	left := ruleenginemath.Decimal(decimal.MustParse("100.0000000001"))
	right := ruleenginemath.Decimal(decimal.MustParse("100"))
	ctx := context.Background()

	b.Run("adapter", func(b *testing.B) {
		operator := operatorByName(b, ruleenginemath.OpDecimalGreaterThan)
		b.ReportAllocs()
		for b.Loop() {
			matched, err := operator.Evaluate(ctx, left, right)
			if err != nil || !matched {
				b.Fatalf("Evaluate() = %t, %v", matched, err)
			}
		}
	})

	b.Run("direct", func(b *testing.B) {
		left := decimal.MustParse("100.0000000001")
		right := decimal.MustParse("100")
		b.ReportAllocs()
		for b.Loop() {
			if left.Cmp(right) <= 0 {
				b.Fatal("direct comparison did not preserve ordering")
			}
		}
	})
}

func operatorByName(
	tb testing.TB,
	name ruleengine.OperatorName,
) ruleengine.Operator {
	tb.Helper()
	for _, operator := range ruleenginemath.Operators() {
		if operator.Name() == name {
			return operator
		}
	}
	tb.Fatalf("operator %q is missing", name)
	panic("unreachable")
}
