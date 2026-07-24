package rational_test

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func TestRationalNormalizesAndDefensivelyCopies(t *testing.T) {
	t.Parallel()

	source := big.NewRat(2, 4)
	value, err := rational.FromBig(source, gomath.DefaultLimits())
	if err != nil {
		t.Fatalf("FromBig() error = %v", err)
	}
	source.SetInt64(9)
	numerator := value.Numerator()
	numerator.SetInt64(7)

	if got := value.String(); got != "1/2" {
		t.Fatalf("value changed through alias: got %s", got)
	}
	if value.Denominator().String() != "2" {
		t.Fatal("denominator accessor returned the wrong value")
	}
}

func TestRationalExactArithmetic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	limits := gomath.DefaultLimits()
	a := mustNew(t, 2, 3)
	b := mustNew(t, -5, 7)

	assertOperation(t, "-1/21", func() (rational.Rational, error) { return a.Add(ctx, b, limits) })
	assertOperation(t, "29/21", func() (rational.Rational, error) { return a.Sub(ctx, b, limits) })
	assertOperation(t, "-10/21", func() (rational.Rational, error) { return a.Mul(ctx, b, limits) })
	assertOperation(t, "-14/15", func() (rational.Rational, error) { return a.Quo(ctx, b, limits) })
	assertOperation(t, "8/27", func() (rational.Rational, error) { return a.Pow(ctx, 3, limits) })
	assertOperation(t, "9/4", func() (rational.Rational, error) { return a.Pow(ctx, -2, limits) })

	if a.Sign() != 1 || a.Cmp(b) <= 0 || a.Equal(b) {
		t.Fatal("comparison contract failed")
	}
	if a.Neg().String() != "-2/3" || b.Abs().String() != "5/7" {
		t.Fatal("unary arithmetic contract failed")
	}
	if rational.Min(a, b).String() != "-5/7" || rational.Max(a, b).String() != "2/3" {
		t.Fatal("min/max contract failed")
	}
}

func TestRationalConversionAndSerialization(t *testing.T) {
	t.Parallel()

	value := mustNew(t, 1, 8)
	text, conditions, err := value.Decimal(2, gomath.RoundHalfEven, gomath.DefaultLimits())
	if err != nil || text != "0.12" || conditions != gomath.ConditionRounded|gomath.ConditionInexact {
		t.Fatalf("Decimal() = %q, %s, %v", text, conditions, err)
	}
	exact, conditions, err := value.Decimal(3, gomath.RoundHalfEven, gomath.DefaultLimits())
	if err != nil || exact != "0.125" || conditions != 0 {
		t.Fatalf("exact Decimal() = %q, %s, %v", exact, conditions, err)
	}

	data, err := json.Marshal(value)
	if err != nil || string(data) != `"1/8"` {
		t.Fatalf("MarshalJSON() = %s, %v", data, err)
	}
	parsed, err := rational.Parse("-10/15", gomath.DefaultLimits())
	if err != nil || parsed.String() != "-2/3" {
		t.Fatalf("Parse() = %s, %v", parsed, err)
	}
	if _, err := rational.Parse("1 / 2", gomath.DefaultLimits()); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("Parse() error = %v", err)
	}
}

func TestRationalRejectsUndefinedOrUnboundedWork(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	if _, err := rational.NewChecked(big.NewInt(1), big.NewInt(0), limits); !errors.Is(err, gomath.ErrDivisionByZero) {
		t.Fatalf("NewChecked() error = %v", err)
	}
	value := mustNew(t, 1, 2)
	if _, err := value.Quo(context.Background(), rational.Zero(), limits); !errors.Is(err, gomath.ErrDivisionByZero) {
		t.Fatalf("Quo() error = %v", err)
	}
	third := mustNew(t, 1, 3)
	if _, _, err := third.Decimal(limits.MaxDecimalExpansion+1, gomath.RoundDown, limits); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Decimal() error = %v", err)
	}
	twoThirds := mustNew(t, 2, 3)
	if _, err := twoThirds.Pow(context.Background(), int64(limits.MaxPowerExponent)+1, limits); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Pow() error = %v", err)
	}
}

func TestRationalDecimalHonorsIntermediateBitLimit(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	limits.MaxIntermediateBits = 16
	limits.MaxDecimalExpansion = 1_000
	limits.MaxOutputDigits = 1_000
	value := mustNew(t, 1, 3)
	if _, _, err := value.Decimal(100, gomath.RoundHalfEven, limits); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Decimal() error = %v, want ErrLimitExceeded", err)
	}
}

func TestRationalPowerPreflightsIntermediateSize(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	limits.MaxIntermediateBits = 16
	limits.MaxPowerExponent = 1_000
	value := mustNew(t, 256, 3)
	if _, err := value.Pow(context.Background(), 10, limits); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Pow() error = %v, want ErrLimitExceeded", err)
	}
	if got, err := mustNew(t, 1, 1).Pow(context.Background(), 1_000, limits); err != nil || got.String() != "1" {
		t.Fatalf("1^1000 = %s, %v", got, err)
	}
	if got, err := value.Pow(context.Background(), 0, limits); err != nil || got.String() != "1" {
		t.Fatalf("value^0 = %s, %v", got, err)
	}
}

func TestRationalMatchesBigRat(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	for numerator := int64(-9); numerator <= 9; numerator++ {
		for denominator := int64(1); denominator <= 9; denominator++ {
			value := mustNew(t, numerator, denominator)
			other := mustNew(t, denominator, 11)
			got, err := value.Add(context.Background(), other, limits)
			if err != nil {
				t.Fatalf("Add() error = %v", err)
			}
			want := new(big.Rat).Add(big.NewRat(numerator, denominator), big.NewRat(denominator, 11))
			if got.Big().Cmp(want) != 0 {
				t.Fatalf("%s + %s = %s, want %s", value, other, got, want)
			}
		}
	}
}

func mustNew(t *testing.T, numerator, denominator int64) rational.Rational {
	t.Helper()
	value, err := rational.New(numerator, denominator)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return value
}

func assertOperation(t *testing.T, want string, operation func() (rational.Rational, error)) {
	t.Helper()
	value, err := operation()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := value.String(); got != want {
		t.Fatalf("value = %s, want %s", got, want)
	}
}
