package decimal_test

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
)

func TestParserBoundsRenderedOutput(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	limits.MaxOutputDigits = 8
	_, err := decimal.ParseWithOptions("1e-20", decimal.ParseOptions{
		AllowExponent: true,
		Limits:        limits,
	})
	if !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("ParseWithOptions() error = %v, want ErrLimitExceeded", err)
	}
}

func TestJSONDecoderRejectsTrailingData(t *testing.T) {
	t.Parallel()

	var value decimal.Decimal
	if err := value.UnmarshalJSON([]byte(`"1" true`)); !errors.Is(err, gomath.ErrInvalidSyntax) {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
}

func TestPositiveExponentZeroJSONRoundTripsCanonically(t *testing.T) {
	t.Parallel()

	value, err := decimal.FromBig(big.NewInt(0), 1, gomath.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `"0"` {
		t.Fatalf("MarshalJSON() = %s, want canonical zero", data)
	}
	var decoded decimal.Decimal
	if err := json.Unmarshal(data, &decoded); err != nil || !decoded.Equal(value) {
		t.Fatalf("JSON round trip = %s, %v", decoded, err)
	}
}

func TestContextReportsExponentConditions(t *testing.T) {
	t.Parallel()

	overflow := decimal.Context{
		Precision: 2, MinExponent: -2, MaxExponent: 2,
		Rounding: decimal.HalfEven,
	}
	overflowResult, err := overflow.Apply(context.Background(), decimal.MustParse("9999"))
	if err != nil || !overflowResult.Conditions.Has(gomath.ConditionOverflow) {
		t.Fatalf("overflow = %s, %s, %v", overflowResult.Value, overflowResult.Conditions, err)
	}

	underflow := decimal.Context{
		Precision: 2, MinExponent: -2, MaxExponent: 2,
		Rounding: decimal.HalfEven,
	}
	underflowResult, err := underflow.Apply(context.Background(), decimal.MustParse("0.00001"))
	if err != nil || !underflowResult.Conditions.Has(gomath.ConditionUnderflow) ||
		!underflowResult.Conditions.Has(gomath.ConditionSubnormal) {
		t.Fatalf("underflow = %s, %s, %v", underflowResult.Value, underflowResult.Conditions, err)
	}
}

func TestEveryRoundingModeHandlesPositiveAndNegativeTies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mode     gomath.RoundingMode
		positive string
		negative string
	}{
		{gomath.RoundHalfEven, "2", "-2"},
		{gomath.RoundHalfUp, "3", "-3"},
		{gomath.RoundHalfDown, "2", "-2"},
		{gomath.RoundDown, "2", "-2"},
		{gomath.RoundUp, "3", "-3"},
		{gomath.RoundCeiling, "3", "-2"},
		{gomath.RoundFloor, "2", "-3"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.mode.String(), func(t *testing.T) {
			t.Parallel()
			positive, err := decimal.MustParse("2.5").Quantize(
				context.Background(), 0, test.mode, gomath.DefaultLimits(),
			)
			if err != nil || positive.Value.String() != test.positive {
				t.Fatalf("positive tie = %s, %v", positive.Value, err)
			}
			negative, err := decimal.MustParse("-2.5").Quantize(
				context.Background(), 0, test.mode, gomath.DefaultLimits(),
			)
			if err != nil || negative.Value.String() != test.negative {
				t.Fatalf("negative tie = %s, %v", negative.Value, err)
			}
		})
	}
}

func TestDecimalOperationsRejectOversizedOperandsBeforeShortcuts(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	broad, err := decimal.FromBig(new(big.Int).Lsh(big.NewInt(1), 64), 0, limits)
	if err != nil {
		t.Fatal(err)
	}
	tiny := limits
	tiny.MaxIntermediateBits = 1
	operation := decimal.Context{
		Precision: 1, MinExponent: -100, MaxExponent: 100,
		Rounding: decimal.HalfEven, Limits: tiny,
	}
	checks := map[string]func() error{
		"exact quotient": func() error {
			_, err := decimal.New(0).QuoExact(context.Background(), broad, tiny)
			return err
		},
		"context quotient": func() error {
			_, err := operation.Quo(context.Background(), decimal.New(0), broad)
			return err
		},
		"context application": func() error {
			_, err := operation.Apply(context.Background(), broad)
			return err
		},
		"identity quantize": func() error {
			_, err := broad.Quantize(context.Background(), 0, decimal.HalfEven, tiny)
			return err
		},
	}
	for name, check := range checks {
		if err := check(); !errors.Is(err, gomath.ErrLimitExceeded) {
			t.Fatalf("%s error = %v, want ErrLimitExceeded", name, err)
		}
	}
}
