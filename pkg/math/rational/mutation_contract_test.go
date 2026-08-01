package rational

import (
	"context"
	"errors"
	"math"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
)

func TestConstructionAndParseResourceBoundaries(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	limits.MaxIntermediateBits = 3

	for _, test := range []struct {
		name        string
		numerator   *big.Int
		denominator *big.Int
		wantError   bool
	}{
		{"numerator exact", big.NewInt(7), big.NewInt(1), false},
		{"numerator over", big.NewInt(8), big.NewInt(1), true},
		{"denominator exact", big.NewInt(1), big.NewInt(7), false},
		{"denominator over", big.NewInt(1), big.NewInt(8), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewChecked(test.numerator, test.denominator, limits)
			if test.wantError != errors.Is(err, gomath.ErrLimitExceeded) {
				t.Fatalf("NewChecked() error = %v, want limit error %t", err, test.wantError)
			}
		})
	}

	parseLimits := gomath.DefaultLimits()
	parseLimits.MaxInputDigits = 2
	for _, input := range []string{"", " 1", "1 ", "1/2/3", "123", "00"} {
		if _, err := Parse(input, parseLimits); !errors.Is(err, gomath.ErrInvalidSyntax) {
			t.Fatalf("Parse(%q) error = %v, want invalid syntax", input, err)
		}
	}
	for input, want := range map[string]string{"0": "0", "-0": "0", "99": "99", "-9": "-9", "1/7": "1/7"} {
		got, err := Parse(input, parseLimits)
		if err != nil || got.String() != want {
			t.Fatalf("Parse(%q) = %s, %v; want %s", input, got, err, want)
		}
	}
}

func TestPowerResourceBoundaries(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	limits.MaxIntermediateBits = 5
	limits.MaxPowerExponent = 2

	for input, want := range map[int64]string{0: "1", 2: "16", -1: "1/4"} {
		value, err := New(4, 1)
		if err != nil {
			t.Fatal(err)
		}
		got, powerErr := value.Pow(context.Background(), input, limits)
		if powerErr != nil || got.String() != want {
			t.Fatalf("4^%d = %s, %v; want %s", input, got, powerErr, want)
		}
	}
	if _, err := Zero().Pow(context.Background(), -1, limits); !errors.Is(err, gomath.ErrDivisionByZero) {
		t.Fatalf("0^-1 error = %v", err)
	}
	if got, err := Zero().Pow(context.Background(), 0, limits); err != nil || got.String() != "1" {
		t.Fatalf("0^0 = %s, %v", got, err)
	}
	if _, err := mustRational(t, 4, 1).Pow(context.Background(), 2, withIntermediateBits(limits, 4)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("numerator power error = %v", err)
	}
	if _, err := mustRational(t, 1, 4).Pow(context.Background(), 2, withIntermediateBits(limits, 4)); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("denominator power error = %v", err)
	}
	if _, err := mustRational(t, 2, 1).Pow(context.Background(), 3, limits); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("exponent error = %v", err)
	}

	if powerExceedsBits(big.NewInt(4), 2, 5) {
		t.Fatal("exact power bit boundary was rejected")
	}
	if !powerExceedsBits(big.NewInt(4), 2, 4) {
		t.Fatal("oversized power was accepted")
	}
	if powerExceedsBits(big.NewInt(4), 0, 1) || powerExceedsBits(big.NewInt(1), 1_000, 1) || powerExceedsBits(big.NewInt(0), 1, 1) {
		t.Fatal("zero exponent or unit magnitude was rejected")
	}
}

func TestDecimalResourceBoundaries(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	limits.MaxIntermediateBits = 16
	limits.MaxDecimalExpansion = 2
	limits.MaxOutputDigits = 8
	value := mustRational(t, 1, 4)
	for scale, want := range map[int]string{0: "0", 2: "0.25"} {
		got, _, err := value.Decimal(scale, gomath.RoundDown, limits)
		if err != nil || got != want {
			t.Fatalf("Decimal(%d) = %q, %v; want %q", scale, got, err, want)
		}
	}
	for _, scale := range []int{-1, 3} {
		if _, _, err := value.Decimal(scale, gomath.RoundDown, limits); !errors.Is(err, gomath.ErrLimitExceeded) {
			t.Fatalf("Decimal(%d) error = %v", scale, err)
		}
	}

	bitLimits := limits
	bitLimits.MaxIntermediateBits = 3
	for _, test := range []struct {
		value     Rational
		wantError bool
	}{
		{mustRational(t, 7, 1), false},
		{mustRational(t, 8, 1), true},
		{mustRational(t, 1, 7), false},
		{mustRational(t, 1, 8), true},
	} {
		_, _, err := test.value.Decimal(0, gomath.RoundDown, bitLimits)
		if test.wantError != errors.Is(err, gomath.ErrLimitExceeded) {
			t.Fatalf("Decimal(%s) error = %v, want limit error %t", test.value, err, test.wantError)
		}
	}

	growthLimits := limits
	growthLimits.MaxIntermediateBits = 4
	if _, _, err := mustRational(t, 1, 2).Decimal(1, gomath.RoundDown, growthLimits); err != nil {
		t.Fatalf("exact decimal growth boundary: %v", err)
	}
	growthLimits.MaxIntermediateBits = 3
	if _, _, err := mustRational(t, 1, 2).Decimal(1, gomath.RoundDown, growthLimits); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("decimal growth error = %v", err)
	}

	outputLimits := limits
	outputLimits.MaxOutputDigits = 2
	if got, _, err := mustRational(t, 123, 10).Decimal(1, gomath.RoundDown, outputLimits); err != nil || got != "12.3" {
		t.Fatalf("exact decimal output boundary = %q, %v", got, err)
	}
	if _, _, err := mustRational(t, 1234, 10).Decimal(1, gomath.RoundDown, outputLimits); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("decimal output error = %v", err)
	}
}

func TestComparisonAndInternalLimitBoundaries(t *testing.T) {
	t.Parallel()

	zero := Zero()
	one := mustRational(t, 1, 1)
	equal := mustRational(t, 2, 2)
	if !Min(one, equal).Equal(one) || !Max(one, equal).Equal(one) {
		t.Fatal("equal min/max changed the numeric result")
	}
	if got, err := Clamp(zero, one, one); err != nil || !got.Equal(one) {
		t.Fatalf("Clamp(equal interval) = %s, %v", got, err)
	}

	limits := gomath.DefaultLimits()
	limits.MaxIntermediateBits = 3
	for _, test := range []struct {
		name      string
		value     *big.Rat
		wantError bool
	}{
		{"numerator exact", big.NewRat(7, 1), false},
		{"numerator over", big.NewRat(8, 1), true},
		{"denominator exact", big.NewRat(1, 7), false},
		{"denominator over", big.NewRat(1, 8), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			operandErr := checkRationalOperands(limits, test.value)
			if test.wantError != errors.Is(operandErr, gomath.ErrLimitExceeded) {
				t.Fatalf("checkRationalOperands() error = %v, want limit error %t", operandErr, test.wantError)
			}
			_, resultErr := checked(test.value, limits)
			if test.wantError != errors.Is(resultErr, gomath.ErrLimitExceeded) {
				t.Fatalf("checked() error = %v, want limit error %t", resultErr, test.wantError)
			}
		})
	}
}

func TestIntegerExponentAndRoundingBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		text  string
		limit int
		want  bool
	}{
		{"0", 1, true},
		{"9", 1, true},
		{"-1", 1, true},
		{"", 1, false},
		{"-", 1, false},
		{"00", 2, false},
		{"10", 1, false},
		{"/", 1, false},
		{":", 1, false},
	} {
		if got := validInteger(test.text, test.limit); got != test.want {
			t.Fatalf("validInteger(%q, %d) = %t, want %t", test.text, test.limit, got, test.want)
		}
	}

	for value, want := range map[int64]uint64{
		math.MinInt64: uint64(1) << 63,
		-1:            1,
		0:             0,
		1:             1,
		math.MaxInt64: math.MaxInt64,
	} {
		if got := exponentMagnitude(value); got != want {
			t.Fatalf("exponentMagnitude(%d) = %d, want %d", value, got, want)
		}
		if got := isNegative(value); got != (value < 0) {
			t.Fatalf("isNegative(%d) = %t", value, got)
		}
	}
	if decimalExpansionBits(1, 2) != 7 {
		t.Fatal("decimal expansion growth accounting changed")
	}

	quotient, remainder, denominator := big.NewInt(1), big.NewInt(1), big.NewInt(3)
	for _, test := range []struct {
		sign int
		mode gomath.RoundingMode
		want bool
	}{
		{0, gomath.RoundCeiling, false},
		{1, gomath.RoundCeiling, true},
		{-1, gomath.RoundCeiling, false},
		{0, gomath.RoundFloor, false},
		{1, gomath.RoundFloor, false},
		{-1, gomath.RoundFloor, true},
	} {
		if got := shouldIncrement(quotient, remainder, denominator, test.sign, test.mode); got != test.want {
			t.Fatalf("shouldIncrement(sign=%d, mode=%s) = %t, want %t", test.sign, test.mode, got, test.want)
		}
	}
}

func mustRational(t *testing.T, numerator, denominator int64) Rational {
	t.Helper()

	value, err := New(numerator, denominator)
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func withIntermediateBits(limits gomath.Limits, bits int) gomath.Limits {
	limits.MaxIntermediateBits = bits

	return limits
}
