package rational_test

import (
	"context"
	"errors"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func TestRationalConstructionParsingAndTextEdges(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	if _, err := rational.New(1, 0); !errors.Is(err, gomath.ErrDivisionByZero) {
		t.Fatalf("New(1, 0) error = %v", err)
	}
	if _, err := rational.NewChecked(nil, big.NewInt(1), limits); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("NewChecked(nil) error = %v", err)
	}
	invalid := limits
	invalid.MaxInputDigits = 0
	if _, err := rational.NewChecked(big.NewInt(1), big.NewInt(2), invalid); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("NewChecked(limits) error = %v", err)
	}
	tiny := limits
	tiny.MaxIntermediateBits = 1
	if _, err := rational.NewChecked(big.NewInt(4), big.NewInt(1), tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("NewChecked(magnitude) error = %v", err)
	}
	if _, err := rational.FromBig(nil, limits); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("FromBig(nil) error = %v", err)
	}

	integerValue, err := rational.Parse("42", limits)
	if err != nil || integerValue.String() != "42" {
		t.Fatalf("Parse(integer) = %s, %v", integerValue, err)
	}
	text, err := integerValue.MarshalText()
	if err != nil || string(text) != "42" {
		t.Fatalf("MarshalText() = %q, %v", text, err)
	}
	for _, input := range []string{"", "1/2/3", "+1/2", "01/2", "1/0", "1/a"} {
		if _, err := rational.Parse(input, limits); err == nil {
			t.Fatalf("Parse(%q) succeeded", input)
		}
	}
}

func TestRationalOperationAndRoundingEdges(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	zero := rational.Zero()
	if _, err := zero.Pow(context.Background(), -1, limits); !errors.Is(err, gomath.ErrDivisionByZero) {
		t.Fatalf("zero.Pow(-1) error = %v", err)
	}
	value := mustNew(t, -5, 8)
	tests := []struct {
		mode gomath.RoundingMode
		want string
	}{
		{gomath.RoundDown, "-0.6"},
		{gomath.RoundUp, "-0.7"},
		{gomath.RoundCeiling, "-0.6"},
		{gomath.RoundFloor, "-0.7"},
		{gomath.RoundHalfUp, "-0.6"},
		{gomath.RoundHalfDown, "-0.6"},
		{gomath.RoundHalfEven, "-0.6"},
	}
	for _, test := range tests {
		got, conditions, err := value.Decimal(1, test.mode, limits)
		if err != nil || got != test.want || !conditions.Has(gomath.ConditionInexact) {
			t.Fatalf("Decimal(%s) = %s, %s, %v", test.mode, got, conditions, err)
		}
	}
	if _, _, err := value.Decimal(1, gomath.RoundingMode(255), limits); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("Decimal(rounding) error = %v", err)
	}
	outputLimits := limits
	outputLimits.MaxOutputDigits = 1
	if _, _, err := mustNew(t, 1234, 1).Decimal(0, gomath.RoundDown, outputLimits); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Decimal(output) error = %v", err)
	}

	minimum := mustNew(t, -1, 1)
	maximum := mustNew(t, 1, 1)
	if got, _ := rational.Clamp(mustNew(t, -2, 1), minimum, maximum); !got.Equal(minimum) {
		t.Fatalf("Clamp(low) = %s", got)
	}
	if got, _ := rational.Clamp(mustNew(t, 2, 1), minimum, maximum); !got.Equal(maximum) {
		t.Fatalf("Clamp(high) = %s", got)
	}
	if got, _ := rational.Clamp(zero, minimum, maximum); !got.Equal(zero) {
		t.Fatalf("Clamp(middle) = %s", got)
	}
	if _, err := rational.Clamp(zero, maximum, minimum); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("Clamp(inverted) error = %v", err)
	}
}

func TestRationalContextAndResultLimits(t *testing.T) {
	t.Parallel()

	limits := gomath.DefaultLimits()
	one := mustNew(t, 1, 1)
	var nilContext context.Context
	if _, err := one.Add(nilContext, one, limits); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("Add(nil context) error = %v", err)
	}
	if _, err := one.Quo(nilContext, one, limits); !errors.Is(err, gomath.ErrInvalidArgument) {
		t.Fatalf("Quo(nil context) error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := one.Add(canceled, one, limits); !errors.Is(err, context.Canceled) {
		t.Fatalf("Add(canceled) error = %v", err)
	}
	tiny := limits
	tiny.MaxIntermediateBits = 1
	broad, err := rational.NewChecked(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1), limits)
	if err != nil {
		t.Fatal(err)
	}
	for name, operation := range map[string]func() error{
		"additive cancellation": func() error {
			_, err := broad.Add(context.Background(), broad.Neg(), tiny)
			return err
		},
		"zero product": func() error {
			_, err := broad.Mul(context.Background(), rational.Zero(), tiny)
			return err
		},
		"zero quotient": func() error {
			_, err := rational.Zero().Quo(context.Background(), broad, tiny)
			return err
		},
		"zero exponent": func() error {
			_, err := broad.Pow(context.Background(), 0, tiny)
			return err
		},
	} {
		if err := operation(); !errors.Is(err, gomath.ErrLimitExceeded) {
			t.Fatalf("%s error = %v, want ErrLimitExceeded", name, err)
		}
	}
	if _, err := one.Add(context.Background(), one, tiny); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Fatalf("Add(limit) error = %v", err)
	}
	if rational.Min(one, rational.Zero()).Sign() != 0 || rational.Max(rational.Zero(), one).Cmp(one) != 0 {
		t.Fatal("min/max alternate branches failed")
	}
}
