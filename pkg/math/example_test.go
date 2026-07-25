package gomath_test

import (
	"context"
	"fmt"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/math/integer"
)

func Example_integer() {
	value, _ := integer.Parse("ff", integer.ParseOptions{
		Base: 16, Limits: gomath.DefaultLimits(),
	})
	result, _ := value.Mul(context.Background(), integer.New(2), gomath.DefaultLimits())
	fmt.Println(result)
	// Output: 510
}

func Example_decimal() {
	price := decimal.MustParse("19.995")
	result, _ := price.Quantize(
		context.Background(), 2, gomath.RoundHalfEven, gomath.DefaultLimits(),
	)
	fmt.Println(result.Value, result.Conditions)
	// Output: 20.00 rounded,inexact
}
