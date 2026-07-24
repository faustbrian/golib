package gomath_test

import (
	"context"
	"sync"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/bigfloat"
	"github.com/faustbrian/golib/pkg/math/decimal"
	mathencoding "github.com/faustbrian/golib/pkg/math/encoding"
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func TestValuesAndContextsAreSafeForConcurrentUse(t *testing.T) {
	limits := gomath.DefaultLimits()
	integerValue := integer.New(123)
	rationalValue, err := rational.New(22, 7)
	if err != nil {
		t.Fatal(err)
	}
	decimalValue := decimal.MustParse("123.4500")
	decimalContext := decimal.Context{Precision: 12, MinExponent: -100, MaxExponent: 100, Rounding: decimal.HalfEven, Limits: limits}
	floatContext := bigfloat.Context{Precision: 128, Rounding: gomath.RoundHalfEven, Limits: limits}
	floatResult, err := bigfloat.NewInt64(123, floatContext)
	if err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				ctx := context.Background()
				if _, err := integerValue.Add(ctx, integerValue, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := integerValue.Sub(ctx, integerValue, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := integerValue.Mul(ctx, integer.New(2), limits); err != nil {
					t.Error(err)
					return
				}
				if _, _, err := integerValue.QuoRem(ctx, integer.New(7), limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := integerValue.Mod(ctx, integer.New(7), limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := integerValue.Pow(ctx, 3, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := integerValue.Root(ctx, 3, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := integer.GCD(ctx, integerValue, integer.New(9), limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := integer.LCM(ctx, integerValue, integer.New(9), limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := rationalValue.Add(ctx, rationalValue, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := rationalValue.Sub(ctx, rationalValue, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := rationalValue.Mul(ctx, rationalValue, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := rationalValue.Quo(ctx, rationalValue, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := rationalValue.Pow(ctx, 3, limits); err != nil {
					t.Error(err)
					return
				}
				if _, _, err := rationalValue.Decimal(8, gomath.RoundHalfEven, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := decimalValue.AddExact(ctx, decimalValue, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := decimalValue.SubExact(ctx, decimalValue, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := decimalValue.MulExact(ctx, decimalValue, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := decimalValue.QuoExact(ctx, decimalValue, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := decimalContext.Add(ctx, decimalValue, decimalValue); err != nil {
					t.Error(err)
					return
				}
				if _, err := decimalContext.Sub(ctx, decimalValue, decimalValue); err != nil {
					t.Error(err)
					return
				}
				if _, err := decimalContext.Mul(ctx, decimalValue, decimalValue); err != nil {
					t.Error(err)
					return
				}
				if _, err := decimalContext.Quo(ctx, decimalValue, decimalValue); err != nil {
					t.Error(err)
					return
				}
				if _, err := decimalContext.Apply(ctx, decimalValue); err != nil {
					t.Error(err)
					return
				}
				if _, err := decimalValue.Quantize(ctx, 2, decimal.HalfEven, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := decimal.QuantizedQuo(ctx, decimalValue, decimalValue, 2, decimal.HalfEven, limits); err != nil {
					t.Error(err)
					return
				}
				if _, err := floatContext.Add(ctx, floatResult.Value, floatResult.Value); err != nil {
					t.Error(err)
					return
				}
				if _, err := floatContext.Sub(ctx, floatResult.Value, floatResult.Value); err != nil {
					t.Error(err)
					return
				}
				if _, err := floatContext.Mul(ctx, floatResult.Value, floatResult.Value); err != nil {
					t.Error(err)
					return
				}
				if _, err := floatContext.Quo(ctx, floatResult.Value, floatResult.Value); err != nil {
					t.Error(err)
					return
				}
				if _, err := floatContext.Sqrt(ctx, floatResult.Value); err != nil {
					t.Error(err)
					return
				}
				integerValue.Big().SetInt64(0)
				rationalValue.Big().SetInt64(0)
				rationalValue.Numerator().SetInt64(0)
				rationalValue.Denominator().SetInt64(1)
				decimalValue.Coefficient().SetInt64(0)
				floatResult.Value.Big().SetInt64(0)
				_, _ = integerValue.MarshalText()
				_, _ = rationalValue.MarshalJSON()
				_, _ = decimalValue.MarshalJSON()
				_, _ = floatResult.Value.MarshalText()
				_, _ = mathencoding.MarshalInteger(integerValue)
				_, _ = mathencoding.MarshalRational(rationalValue)
				_, _ = mathencoding.MarshalDecimal(decimalValue)
				_, _ = mathencoding.MarshalFloat(floatResult.Value)
			}
		}()
	}
	wait.Wait()
}
