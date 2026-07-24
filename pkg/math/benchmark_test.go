package gomath_test

import (
	"context"
	"math/big"
	"testing"

	"github.com/cockroachdb/apd/v3"
	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/bigfloat"
	"github.com/faustbrian/golib/pkg/math/decimal"
	mathencoding "github.com/faustbrian/golib/pkg/math/encoding"
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/math/rational"
	shopspring "github.com/shopspring/decimal"
)

func BenchmarkIntegerAdd(b *testing.B) {
	left := integer.New(123456789)
	right := integer.New(987654321)
	limits := gomath.DefaultLimits()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := left.Add(context.Background(), right, limits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBigIntAdd(b *testing.B) {
	left := big.NewInt(123456789)
	right := big.NewInt(987654321)
	b.ReportAllocs()
	for b.Loop() {
		new(big.Int).Add(left, right)
	}
}

func BenchmarkDecimalAdd(b *testing.B) {
	left := decimal.MustParse("123456789.1234")
	right := decimal.MustParse("987654321.9876")
	operation := decimal.Context{
		Precision: 18, MinExponent: -100, MaxExponent: 100,
		Rounding: decimal.HalfEven,
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := operation.Add(context.Background(), left, right); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAPDAdd(b *testing.B) {
	left, _, _ := apd.NewFromString("123456789.1234")
	right, _, _ := apd.NewFromString("987654321.9876")
	operation := apd.Context{
		Precision: 18, MinExponent: -100, MaxExponent: 100,
		Rounding: apd.RoundHalfEven,
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := operation.Add(new(apd.Decimal), left, right); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkShopspringAdd(b *testing.B) {
	left := shopspring.RequireFromString("123456789.1234")
	right := shopspring.RequireFromString("987654321.9876")
	b.ReportAllocs()
	for b.Loop() {
		_ = left.Add(right)
	}
}

func BenchmarkBoundedIntegerPower(b *testing.B) {
	value := integer.New(3)
	limits := gomath.DefaultLimits()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := value.Pow(context.Background(), 1_024, limits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBoundedIntegerRoot(b *testing.B) {
	value, err := integer.FromBig(new(big.Int).Lsh(big.NewInt(1), 4_096), gomath.DefaultLimits())
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := value.Root(context.Background(), 17, gomath.DefaultLimits()); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecimalDivision(b *testing.B) {
	numerator := decimal.MustParse("12345678901234567890.123456789")
	denominator := decimal.MustParse("7.000000001")
	operation := decimal.Context{
		Precision: 34, MinExponent: -1_000, MaxExponent: 1_000,
		Rounding: decimal.HalfEven,
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := operation.Quo(context.Background(), numerator, denominator); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecimalFormatting(b *testing.B) {
	value := decimal.MustParse("12345678901234567890.12345678901234567890")
	b.ReportAllocs()
	for b.Loop() {
		_ = value.String()
	}
}

func BenchmarkRationalNormalization(b *testing.B) {
	factor := new(big.Int).Lsh(big.NewInt(1), 2_048)
	numerator := new(big.Int).Mul(big.NewInt(22), factor)
	denominator := new(big.Int).Mul(big.NewInt(7), factor)
	limits := gomath.DefaultLimits()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := rational.NewChecked(numerator, denominator, limits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRationalDecimalExpansion(b *testing.B) {
	value, err := rational.New(1, 7)
	if err != nil {
		b.Fatal(err)
	}
	limits := gomath.DefaultLimits()
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := value.Decimal(1_000, gomath.RoundHalfEven, limits); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBigFloatSquareRootAndConversion(b *testing.B) {
	operation := bigfloat.Context{
		Precision: 4_096, Rounding: gomath.RoundHalfEven,
		Limits: gomath.DefaultLimits(),
	}
	value, err := bigfloat.FromRat(big.NewRat(2, 1), operation)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		result, err := operation.Sqrt(context.Background(), value.Value)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := result.Value.Rat(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBinaryConversionRoundTrip(b *testing.B) {
	limits := gomath.DefaultLimits()
	value := decimal.MustParse("12345678901234567890.12345678901234567890")
	b.ReportAllocs()
	for b.Loop() {
		data, err := mathencoding.MarshalDecimal(value)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := mathencoding.UnmarshalDecimal(data, limits); err != nil {
			b.Fatal(err)
		}
	}
}
