package ruleenginemath_test

import (
	"context"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
	ruleengine "github.com/faustbrian/golib/pkg/rule-engine"
	ruleenginemath "github.com/faustbrian/golib/pkg/rule-engine/adapters/gomath"
)

func BenchmarkDecimalComparison(b *testing.B) {
	const leftText = "100.0000000001"
	const rightText = "100"
	left := ruleenginemath.Decimal(decimal.MustParse(leftText))
	right := ruleenginemath.Decimal(decimal.MustParse(rightText))
	ctx := context.Background()
	limits := gomath.DefaultLimits()
	b.SetBytes(int64(len(leftText) + len(rightText)))

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

	b.Run("direct-parse-and-compare", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			left, err := decimal.ParseWithOptions(leftText, decimal.ParseOptions{Limits: limits})
			if err != nil {
				b.Fatal(err)
			}
			right, err := decimal.ParseWithOptions(rightText, decimal.ParseOptions{Limits: limits})
			if err != nil {
				b.Fatal(err)
			}
			if left.Cmp(right) <= 0 {
				b.Fatal("direct comparison did not preserve ordering")
			}
		}
	})

	b.Run("direct-preparsed-comparison", func(b *testing.B) {
		left := decimal.MustParse(leftText)
		right := decimal.MustParse(rightText)
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
