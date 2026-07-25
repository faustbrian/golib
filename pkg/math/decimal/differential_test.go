package decimal_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/cockroachdb/apd/v3"
	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
	shopspring "github.com/shopspring/decimal"
)

func TestDecimalDifferentialAgainstAPDAndShopspring(t *testing.T) {
	t.Parallel()

	operation := decimal.Context{
		Precision: 12, MinExponent: -100, MaxExponent: 100,
		Rounding: decimal.HalfEven,
	}
	apdContext := apd.Context{
		Precision: 12, MinExponent: -100, MaxExponent: 100,
		Rounding: apd.RoundHalfEven,
		Traps:    0,
	}

	for index := int64(-30); index <= 30; index++ {
		leftText := fmt.Sprintf("%d.%03d", index, (index*index+97)%1000)
		rightFraction := ((index*7+91)%100 + 100) % 100
		rightText := fmt.Sprintf("%d.%02d", index+31, rightFraction)
		left := decimal.MustParse(leftText)
		right := decimal.MustParse(rightText)
		apdLeft, _, _ := apd.NewFromString(leftText)
		apdRight, _, _ := apd.NewFromString(rightText)
		shopLeft, _ := shopspring.NewFromString(leftText)
		shopRight, _ := shopspring.NewFromString(rightText)

		checkAPDOperation(t, "add", operation, &apdContext, left, right, apdLeft, apdRight)
		checkAPDOperation(t, "subtract", operation, &apdContext, left, right, apdLeft, apdRight)
		checkAPDOperation(t, "multiply", operation, &apdContext, left, right, apdLeft, apdRight)
		if !right.IsZero() {
			checkAPDOperation(t, "divide", operation, &apdContext, left, right, apdLeft, apdRight)
		}
		checkAPDQuantize(t, operation, &apdContext, left, apdLeft, int32((index+30)%5)-2)

		exactAdd, err := left.AddExact(context.Background(), right, gomath.DefaultLimits())
		if err != nil || exactAdd.BigRat().Cmp(shopLeft.Add(shopRight).Rat()) != 0 {
			t.Fatalf("shopspring add mismatch for %s and %s: %s, %v", left, right, exactAdd, err)
		}
		exactMul, err := left.MulExact(context.Background(), right, gomath.DefaultLimits())
		if err != nil || exactMul.BigRat().Cmp(shopLeft.Mul(shopRight).Rat()) != 0 {
			t.Fatalf("shopspring multiply mismatch for %s and %s: %s, %v", left, right, exactMul, err)
		}
		exactSub, err := left.SubExact(context.Background(), right, gomath.DefaultLimits())
		if err != nil || exactSub.BigRat().Cmp(shopLeft.Sub(shopRight).Rat()) != 0 {
			t.Fatalf("shopspring subtract mismatch for %s and %s: %s, %v", left, right, exactSub, err)
		}
	}
}

func checkAPDOperation(
	t *testing.T,
	name string,
	operation decimal.Context,
	apdContext *apd.Context,
	left decimal.Decimal,
	right decimal.Decimal,
	apdLeft *apd.Decimal,
	apdRight *apd.Decimal,
) {
	t.Helper()
	var got decimal.Result
	var gotErr error
	want := new(apd.Decimal)
	var apdConditions apd.Condition
	var apdErr error
	switch name {
	case "add":
		got, gotErr = operation.Add(context.Background(), left, right)
		apdConditions, apdErr = apdContext.Add(want, apdLeft, apdRight)
	case "multiply":
		got, gotErr = operation.Mul(context.Background(), left, right)
		apdConditions, apdErr = apdContext.Mul(want, apdLeft, apdRight)
	case "subtract":
		got, gotErr = operation.Sub(context.Background(), left, right)
		apdConditions, apdErr = apdContext.Sub(want, apdLeft, apdRight)
	case "divide":
		got, gotErr = operation.Quo(context.Background(), left, right)
		apdConditions, apdErr = apdContext.Quo(want, apdLeft, apdRight)
	}
	if gotErr != nil || apdErr != nil {
		t.Fatalf("%s errors: math=%v apd=%v", name, gotErr, apdErr)
	}
	wantValue, err := decimal.ParseWithOptions(want.Text('f'), decimal.ParseOptions{
		AllowLeadingZeros: true,
		Limits:            gomath.DefaultLimits(),
	})
	if err != nil {
		t.Fatalf("parse apd result %s: %v", want, err)
	}
	if !got.Value.Equal(wantValue) {
		t.Fatalf("%s mismatch for %s and %s: got %s, apd %s", name, left, right, got.Value, want)
	}
	wantConditions := sharedConditions(apdConditions)
	if got.Conditions != wantConditions {
		t.Fatalf("%s conditions: got %s, apd %s", name, got.Conditions, apdConditions)
	}
}

func checkAPDQuantize(
	t *testing.T,
	operation decimal.Context,
	apdContext *apd.Context,
	value decimal.Decimal,
	apdValue *apd.Decimal,
	scale int32,
) {
	t.Helper()
	got, gotErr := value.Quantize(context.Background(), scale, decimal.HalfEven, gomath.DefaultLimits())
	want := new(apd.Decimal)
	apdConditions, apdErr := apdContext.Quantize(want, apdValue, -scale)
	if gotErr != nil || apdErr != nil {
		t.Fatalf("quantize errors at scale %d: math=%v apd=%v", scale, gotErr, apdErr)
	}
	wantValue, err := decimal.ParseWithOptions(want.Text('f'), decimal.ParseOptions{
		AllowLeadingZeros: true,
		Limits:            gomath.DefaultLimits(),
	})
	if err != nil {
		t.Fatalf("parse apd quantize result %s: %v", want, err)
	}
	if !got.Value.Equal(wantValue) || got.Conditions != sharedConditions(apdConditions) {
		t.Fatalf(
			"quantize mismatch for %s at scale %d: got %s [%s], apd %s [%s]",
			value, scale, got.Value, got.Conditions, want, apdConditions,
		)
	}
}

func sharedConditions(conditions apd.Condition) gomath.Condition {
	result := gomath.Condition(0)
	if conditions.Rounded() {
		result |= gomath.ConditionRounded
	}
	if conditions.Inexact() {
		result |= gomath.ConditionInexact
	}

	return result
}
