package ruleenginemath_test

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemath "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomath"
)

func BenchmarkDecimalGreaterThan(b *testing.B) {
	operator := operatorByName(b, ruleenginemath.OpDecimalGreaterThan)
	left := ruleenginemath.Decimal(decimal.MustParse("100.0000000001"))
	right := ruleenginemath.Decimal(decimal.MustParse("100"))
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		matched, err := operator.Evaluate(ctx, left, right)
		if err != nil || !matched {
			b.Fatalf("Evaluate() = %t, %v", matched, err)
		}
	}
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
