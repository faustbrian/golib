package integer

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
)

func TestMutationParseBoundaries(t *testing.T) {
	limits := gomath.DefaultLimits()
	for _, test := range []struct {
		text string
		base int
		want string
	}{
		{"10", 2, "2"},
		{"z", 36, "35"},
	} {
		value, err := Parse(test.text, ParseOptions{Base: test.base, Limits: limits})
		if err != nil || value.String() != test.want {
			t.Fatalf("Parse(%q, base %d) = %s, %v", test.text, test.base, value, err)
		}
	}
	for _, base := range []int{1, 37} {
		if _, err := Parse("1", ParseOptions{Base: base, Limits: limits}); !errors.Is(err, gomath.ErrInvalidArgument) {
			t.Fatalf("Parse(base %d) error = %v", base, err)
		}
	}
	for _, input := range []string{" 1", "1 "} {
		if _, err := Parse(input, ParseOptions{Base: 10, Limits: limits}); !errors.Is(err, gomath.ErrInvalidSyntax) {
			t.Fatalf("Parse(%q) error = %v", input, err)
		}
	}
	for _, test := range []struct {
		input        string
		allowLeading bool
		want         string
		wantError    bool
	}{
		{"0", false, "0", false},
		{"00", false, "", true},
		{"00", true, "0", false},
		{"01", false, "", true},
		{"01", true, "1", false},
	} {
		value, err := Parse(test.input, ParseOptions{
			Base:              10,
			AllowLeadingZeros: test.allowLeading,
			Limits:            limits,
		})
		if (err != nil) != test.wantError || !test.wantError && value.String() != test.want {
			t.Fatalf("Parse(%q, leading=%v) = %s, %v", test.input, test.allowLeading, value, err)
		}
	}
}

func TestMutationArithmeticResourceBoundaries(t *testing.T) {
	ctx := context.Background()
	limits := gomath.DefaultLimits()

	multiplyLimits := limits
	multiplyLimits.MaxIntermediateBits = 3
	if _, err := New(3).Mul(ctx, New(3), multiplyLimits); err == nil || err.Error() != "math: resource limit exceeded: integer magnitude" {
		t.Fatalf("Mul exact preflight boundary error = %v", err)
	}
	if _, err := New(7).Mul(ctx, New(3), multiplyLimits); err == nil || err.Error() != "math: resource limit exceeded: multiplication" {
		t.Fatalf("Mul over preflight boundary error = %v", err)
	}

	powerLimits := limits
	powerLimits.MaxPowerExponent = 2
	if value, err := New(1).Pow(ctx, 2, powerLimits); err != nil || value.String() != "1" {
		t.Fatalf("Pow exact exponent boundary = %s, %v", value, err)
	}
	if _, err := New(1).Pow(ctx, 3, powerLimits); err == nil || err.Error() != "math: resource limit exceeded: power exponent" {
		t.Fatalf("Pow over exponent boundary error = %v", err)
	}
	powerLimits.MaxIntermediateBits = 4
	if _, err := New(7).Pow(ctx, 2, powerLimits); err == nil || err.Error() != "math: resource limit exceeded: integer magnitude" {
		t.Fatalf("Pow exact result preflight boundary error = %v", err)
	}
	if _, err := New(15).Pow(ctx, 2, powerLimits); err == nil || err.Error() != "math: resource limit exceeded: power result" {
		t.Fatalf("Pow over result preflight boundary error = %v", err)
	}

	rootLimits := limits
	rootLimits.MaxRootDegree = 2
	if value, err := New(1).Root(ctx, 2, rootLimits); err != nil || value.String() != "1" {
		t.Fatalf("Root exact degree boundary = %s, %v", value, err)
	}
	if _, err := New(1).Root(ctx, 3, rootLimits); err == nil || err.Error() != "math: resource limit exceeded: root degree" {
		t.Fatalf("Root over degree boundary error = %v", err)
	}

	lcmLimits := limits
	lcmLimits.MaxIntermediateBits = 3
	if value, err := LCM(ctx, New(3), New(2), lcmLimits); err != nil || value.String() != "6" {
		t.Fatalf("LCM exact preflight boundary = %s, %v", value, err)
	}
	if _, err := LCM(ctx, New(7), New(3), lcmLimits); err == nil || err.Error() != "math: resource limit exceeded: least common multiple" {
		t.Fatalf("LCM over preflight boundary error = %v", err)
	}
}

func TestMutationSelectionAndRandomBoundaries(t *testing.T) {
	if got := Min(New(1), New(1)); got.String() != "1" {
		t.Fatalf("Min equality = %s", got)
	}
	if got := Max(New(1), New(1)); got.String() != "1" {
		t.Fatalf("Max equality = %s", got)
	}
	if _, err := Clamp(New(1), New(1), New(1)); err != nil {
		t.Fatalf("Clamp equal interval error = %v", err)
	}

	limits := gomath.DefaultLimits()
	for _, operands := range [][2]Integer{{Zero(), New(5)}, {New(5), Zero()}} {
		value, err := LCM(context.Background(), operands[0], operands[1], limits)
		if err != nil || value.Sign() != 0 {
			t.Fatalf("LCM(%s,%s) = %s, %v", operands[0], operands[1], value, err)
		}
	}

	randomLimits := limits
	randomLimits.MaxRandomBits = 3
	value, err := Random(context.Background(), bytes.NewReader([]byte{0}), Zero(), New(4), randomLimits)
	if err != nil || value.Sign() != 0 {
		t.Fatalf("Random exact bit boundary = %s, %v", value, err)
	}
	if _, err := Random(context.Background(), bytes.NewReader([]byte{0}), Zero(), New(8), randomLimits); err == nil || err.Error() != "math: resource limit exceeded: random range" {
		t.Fatalf("Random over bit boundary error = %v", err)
	}

	if err := checkBits(3, gomath.Limits{MaxIntermediateBits: 3}); err != nil {
		t.Fatalf("checkBits exact boundary error = %v", err)
	}
	if err := checkBits(4, gomath.Limits{MaxIntermediateBits: 3}); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("checkBits over boundary error = %v", err)
	}
}

func TestMutationDigitValidationBoundaries(t *testing.T) {
	for _, test := range []struct {
		text       string
		base       int
		underscore bool
		want       string
		ok         bool
	}{
		{"0", 2, false, "0", true},
		{"1", 2, false, "1", true},
		{"2", 2, false, "", false},
		{"z", 36, false, "z", true},
		{"1_0", 2, true, "10", true},
		{"1_0", 2, false, "", false},
		{"", 10, false, "", false},
		{"_1", 10, true, "", false},
		{"1_", 10, true, "", false},
		{"1__0", 10, true, "", false},
	} {
		got, count, ok := validateDigits(test.text, test.base, test.underscore)
		if ok != test.ok || got != test.want || count != len(test.want) {
			t.Fatalf("validateDigits(%q,%d,%v) = %q, %d, %v", test.text, test.base, test.underscore, got, count, ok)
		}
	}
}

func TestMutationNthRootBoundaries(t *testing.T) {
	for _, test := range []struct {
		bits   int
		degree uint32
		want   int
	}{
		{1, 3, 2},
		{3, 3, 2},
		{4, 3, 3},
		{20, 3, 8},
	} {
		if got := rootUpperBoundShift(test.bits, test.degree); got != test.want {
			t.Fatalf("rootUpperBoundShift(%d,%d) = %d", test.bits, test.degree, got)
		}
	}

	limits := gomath.DefaultLimits()
	for _, test := range []struct {
		value, degree int64
		want          string
	}{
		{1, 3, "1"},
		{8, 3, "2"},
		{9, 3, "2"},
		{26, 3, "2"},
		{27, 3, "3"},
		{28, 3, "3"},
	} {
		root, err := nthRoot(context.Background(), big.NewInt(test.value), uint32(test.degree), limits)
		if err != nil || root.String() != test.want {
			t.Fatalf("nthRoot(%d,%d) = %s, %v", test.value, test.degree, root, err)
		}
	}

	bounded := limits
	bounded.MaxIntermediateBits = 4
	root, err := nthRoot(context.Background(), big.NewInt(15), 3, bounded)
	if err != nil || root.String() != "2" {
		t.Fatalf("nthRoot exact power-bit boundary = %s, %v", root, err)
	}
}
