package gomath_test

import (
	"strings"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
)

func TestConditionAndRoundingFormattingEdges(t *testing.T) {
	t.Parallel()

	if gomath.Condition(0).String() != "none" {
		t.Fatal("zero condition is not canonical")
	}
	all := gomath.ConditionRounded | gomath.ConditionInexact |
		gomath.ConditionOverflow | gomath.ConditionUnderflow |
		gomath.ConditionDivisionByZero | gomath.ConditionInvalidOperation |
		gomath.ConditionClamped | gomath.ConditionSubnormal | gomath.Condition(1<<15)
	if text := all.String(); !strings.Contains(text, "unknown(0x8000)") || !strings.Contains(text, "subnormal") {
		t.Fatalf("all conditions = %q", text)
	}
	invalid := gomath.RoundingMode(255)
	if invalid.Valid() || invalid.String() != "RoundingMode(255)" {
		t.Fatalf("invalid rounding mode = %s", invalid)
	}
	for mode := gomath.RoundHalfEven; mode <= gomath.RoundFloor; mode++ {
		if !mode.Valid() || mode.String() == "" {
			t.Fatalf("rounding mode %d is invalid", mode)
		}
	}
}

func TestEveryLimitMustBePositive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		zero     func(*gomath.Limits)
		negative func(*gomath.Limits)
	}{
		{func(l *gomath.Limits) { l.MaxInputDigits = 0 }, func(l *gomath.Limits) { l.MaxInputDigits = -1 }},
		{func(l *gomath.Limits) { l.MaxOutputDigits = 0 }, func(l *gomath.Limits) { l.MaxOutputDigits = -1 }},
		{func(l *gomath.Limits) { l.MaxExponentMagnitude = 0 }, func(l *gomath.Limits) { l.MaxExponentMagnitude = -1 }},
		{func(l *gomath.Limits) { l.MaxPrecision = 0 }, nil},
		{func(l *gomath.Limits) { l.MaxPowerExponent = 0 }, nil},
		{func(l *gomath.Limits) { l.MaxRootDegree = 0 }, nil},
		{func(l *gomath.Limits) { l.MaxRandomBits = 0 }, func(l *gomath.Limits) { l.MaxRandomBits = -1 }},
		{func(l *gomath.Limits) { l.MaxRandomAttempts = 0 }, func(l *gomath.Limits) { l.MaxRandomAttempts = -1 }},
		{func(l *gomath.Limits) { l.MaxIntermediateBits = 0 }, func(l *gomath.Limits) { l.MaxIntermediateBits = -1 }},
		{func(l *gomath.Limits) { l.MaxDecimalExpansion = 0 }, func(l *gomath.Limits) { l.MaxDecimalExpansion = -1 }},
		{func(l *gomath.Limits) { l.MaxDiagnosticBytes = 0 }, func(l *gomath.Limits) { l.MaxDiagnosticBytes = -1 }},
	}
	for index, invalid := range tests {
		for _, invalidate := range []func(*gomath.Limits){invalid.zero, invalid.negative} {
			if invalidate == nil {
				continue
			}
			limits := gomath.DefaultLimits()
			invalidate(&limits)
			if err := limits.Validate(); err == nil {
				t.Fatalf("invalid limits case %d passed", index)
			}
		}
	}
}
