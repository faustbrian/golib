package decimal

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"strconv"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
)

func TestStringExponentBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value Decimal
		want  string
	}{
		{fromBig(big.NewInt(12), 1), "120"},
		{fromBig(big.NewInt(12), 0), "12"},
		{fromBig(big.NewInt(12), -1), "1.2"},
		{fromBig(big.NewInt(12), -2), "0.12"},
		{fromBig(big.NewInt(12), -3), "0.012"},
	}
	for _, test := range tests {
		if got := test.value.String(); got != test.want {
			t.Errorf("String() = %q, want %q", got, test.want)
		}
	}
}

func TestDecimalAccessorsComparisonAndClampEdges(t *testing.T) {
	positive := fromBig(big.NewInt(12), 2)
	negative := fromBig(big.NewInt(-12), 2)
	zero := Decimal{}
	if positive.Scale() != -2 || positive.Abs().String() != "1200" || negative.Abs().String() != "1200" {
		t.Fatal("accessor or absolute value mismatch")
	}
	if positive.BigRat().Cmp(big.NewRat(1200, 1)) != 0 || MustParse("0.12").BigRat().Cmp(big.NewRat(3, 25)) != 0 {
		t.Fatal("exact rational conversion mismatch")
	}
	checks := []struct {
		left, right Decimal
		want        int
	}{
		{negative, positive, -1}, {positive, negative, 1}, {zero, zero, 0},
		{MustParse("10"), MustParse("9"), 1}, {MustParse("-10"), MustParse("-9"), -1},
		{MustParse("1.0"), MustParse("1.00"), 0},
	}
	for _, check := range checks {
		if got := check.left.Cmp(check.right); got != check.want {
			t.Fatalf("Cmp = %d, want %d", got, check.want)
		}
	}
	if _, err := positive.Clamp(positive, negative); err == nil {
		t.Fatal("expected reversed clamp error")
	}
	if got, _ := negative.Clamp(zero, positive); !got.Equal(zero) {
		t.Fatal("lower clamp mismatch")
	}
	if got, _ := positive.Clamp(negative, zero); !got.Equal(zero) {
		t.Fatal("upper clamp mismatch")
	}
	if got, _ := zero.Clamp(negative, positive); !got.Equal(zero) {
		t.Fatal("in-range clamp mismatch")
	}
	if text, err := positive.MarshalText(); err != nil || string(text) != "1200" {
		t.Fatal("text encoding mismatch")
	}
}

func TestDecimalConstructionAndParsingEdges(t *testing.T) {
	limits := gomath.DefaultLimits()
	bad := limits
	bad.MaxInputDigits = 0
	if _, err := FromBig(nil, 0, limits); err == nil {
		t.Fatal("expected nil coefficient error")
	}
	if _, err := FromBig(big.NewInt(1), 0, bad); err == nil {
		t.Fatal("expected invalid limits")
	}
	tiny := limits
	tiny.MaxExponentMagnitude = 1
	if _, err := FromBig(big.NewInt(1), 2, tiny); err == nil {
		t.Fatal("expected exponent limit")
	}
	tiny = limits
	tiny.MaxInputDigits = 1
	if _, err := FromBig(big.NewInt(12), 0, tiny); err == nil {
		t.Fatal("expected coefficient limit")
	}
	tiny = limits
	tiny.MaxOutputDigits = 1
	if _, err := FromBig(big.NewInt(1), 1, tiny); err == nil {
		t.Fatal("expected output limit")
	}

	opts := ParseOptions{AllowExponent: true, AllowPlus: true, AllowUnderscores: true, AllowLeadingZeros: true, AllowWhitespace: true, Limits: limits}
	if got, err := ParseWithOptions(" +01_2.3_0e+2 ", opts); err != nil || got.String() != "1230" {
		t.Fatalf("extended parse: %v %v", got, err)
	}
	invalid := []string{"", " ", "+", "-", ".1", "1.", "1..0", "1__0", "1e", "e1", "1e1e1", "1e+bad"}
	for _, input := range invalid {
		if _, err := ParseWithOptions(input, opts); err == nil {
			t.Fatalf("expected %q to fail", input)
		}
	}
	strict := ParseOptions{Limits: limits}
	for _, input := range []string{" 1", "+1", "01", "1_0", "1e1"} {
		if _, err := ParseWithOptions(input, strict); err == nil {
			t.Fatalf("expected strict %q to fail", input)
		}
	}
	tooSmall := limits
	tooSmall.MaxInputDigits = 1
	if _, err := ParseWithOptions("12", ParseOptions{AllowLeadingZeros: true, Limits: tooSmall}); err == nil {
		t.Fatal("expected input digit limit")
	}
	tooSmall = limits
	tooSmall.MaxExponentMagnitude = 1
	if _, err := ParseWithOptions("1e999999999999999999999", ParseOptions{AllowExponent: true, Limits: tooSmall}); err == nil {
		t.Fatal("expected exponent range limit")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("MustParse should panic")
			}
		}()
		MustParse("bad")
	}()
}

func TestDecimalExactArithmeticEdges(t *testing.T) {
	limits := gomath.DefaultLimits()
	ctx := context.Background()
	one, two, three := New(1), New(2), New(3)
	if got, err := one.MulExact(ctx, two, limits); err != nil || got.String() != "2" {
		t.Fatal("multiply mismatch")
	}
	if got, err := one.QuoExact(ctx, New(-2), limits); err != nil || got.String() != "-0.5" {
		t.Fatal("negative quotient mismatch")
	}
	if got, err := one.QuoExact(ctx, New(5), limits); err != nil || got.String() != "0.2" {
		t.Fatal("factor-five quotient mismatch")
	}
	if _, err := one.QuoExact(ctx, three, limits); !errors.Is(err, ErrNonTerminating) {
		t.Fatal("expected non-terminating quotient")
	}
	if _, err := one.QuoExact(ctx, Decimal{}, limits); !errors.Is(err, ErrDivisionByZero) {
		t.Fatal("expected zero divisor")
	}
	var nilContext context.Context
	if _, err := one.MulExact(nilContext, two, limits); err == nil {
		t.Fatal("expected nil context")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := one.QuoExact(cancelled, two, limits); !errors.Is(err, context.Canceled) {
		t.Fatal("expected cancellation")
	}
	tiny := limits
	tiny.MaxExponentMagnitude = 1
	if _, err := fromBig(big.NewInt(1), 1).MulExact(ctx, fromBig(big.NewInt(1), 1), tiny); err == nil {
		t.Fatal("expected product exponent limit")
	}
	if _, err := fromBig(big.NewInt(1), 1).AddExact(ctx, fromBig(big.NewInt(1), -1), tiny); err == nil {
		t.Fatal("expected alignment limit")
	}
}

func TestDecimalContextAndRoundingEdges(t *testing.T) {
	limits := gomath.DefaultLimits()
	base := Context{Precision: 2, MinExponent: -2, MaxExponent: 2, Rounding: HalfEven, Limits: limits}
	invalid := []Context{{Precision: 0}, {Precision: limits.MaxPrecision + 1, Limits: limits}, {Precision: 1, Rounding: RoundingMode(99), Limits: limits}, {Precision: 1, MinExponent: 2, MaxExponent: 1, Limits: limits}}
	for _, operation := range invalid {
		if _, err := operation.Apply(context.Background(), New(1)); err == nil {
			t.Fatal("expected invalid context")
		}
	}
	var nilContext context.Context
	if _, err := base.Apply(nilContext, New(1)); err == nil {
		t.Fatal("expected nil context")
	}
	if result, err := base.Apply(context.Background(), MustParse("9.99")); err != nil || result.Value.String() != "10" {
		t.Fatalf("carry rounding: %+v %v", result, err)
	}
	if result, err := base.Quo(context.Background(), Decimal{}, New(2)); err != nil || !result.Value.IsZero() {
		t.Fatal("zero quotient mismatch")
	}
	if result, err := base.Quo(context.Background(), New(1), Decimal{}); !errors.Is(err, ErrDivisionByZero) || !result.Conditions.Has(gomath.ConditionDivisionByZero) {
		t.Fatal("division condition mismatch")
	}
	trapped := base
	trapped.Traps = gomath.ConditionInexact
	if result, err := trapped.Quo(context.Background(), New(1), New(3)); err == nil || !result.Conditions.Has(gomath.ConditionInexact) {
		t.Fatal("expected trapped inexact")
	}
	over := base
	over.MaxExponent = 0
	if result, err := over.Apply(context.Background(), New(99)); err != nil || !result.Conditions.Has(gomath.ConditionOverflow) {
		t.Fatal("expected overflow")
	}
	under := base
	if result, err := under.Apply(context.Background(), fromBig(big.NewInt(1), -5)); err != nil || !result.Conditions.Has(gomath.ConditionUnderflow) {
		t.Fatal("expected underflow")
	}

	for _, mode := range []RoundingMode{Down, Up, Ceiling, Floor, HalfEven, HalfUp, HalfDown} {
		if _, err := MustParse("1.25").Quantize(context.Background(), 1, mode, limits); err != nil {
			t.Fatalf("quantize %v: %v", mode, err)
		}
	}
	if result, err := New(1).Quantize(context.Background(), 2, HalfEven, limits); err != nil || result.Value.String() != "1.00" {
		t.Fatal("padding mismatch")
	}
	if result, err := New(1).Quantize(context.Background(), 0, HalfEven, limits); err != nil || result.Conditions != 0 {
		t.Fatal("identity quantize mismatch")
	}
	if _, err := New(1).Quantize(context.Background(), 0, RoundingMode(99), limits); err == nil {
		t.Fatal("expected invalid rounding")
	}
	if _, err := New(1).Quantize(nilContext, 0, HalfEven, limits); err == nil {
		t.Fatal("expected nil context")
	}

	for _, mode := range []RoundingMode{Down, Up, Ceiling, Floor, HalfEven, HalfUp, HalfDown} {
		if _, err := QuantizedQuo(context.Background(), New(-1), New(8), 2, mode, limits); err != nil {
			t.Fatalf("quantized quotient %v: %v", mode, err)
		}
	}
	if _, err := QuantizedQuo(context.Background(), New(1), Decimal{}, 2, HalfEven, limits); !errors.Is(err, ErrDivisionByZero) {
		t.Fatal("expected zero divisor")
	}
	if _, err := QuantizedQuo(context.Background(), New(1), New(2), 2, RoundingMode(99), limits); err == nil {
		t.Fatal("expected invalid rounding")
	}
}

func TestEveryIntermediateResourcePreflight(t *testing.T) {
	ctx := context.Background()
	limits := gomath.DefaultLimits()

	productLimits := limits
	productLimits.MaxIntermediateBits = 3
	if _, err := New(7).MulExact(ctx, New(7), productLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("product preflight error = %v", err)
	}
	productContext := Context{
		Precision: 1, MinExponent: -2, MaxExponent: 2,
		Rounding: HalfEven, Limits: productLimits,
	}
	if _, err := productContext.Mul(ctx, New(7), New(7)); !errors.Is(err, ErrLimit) {
		t.Fatalf("context product preflight error = %v", err)
	}

	exponentLimits := limits
	exponentLimits.MaxExponentMagnitude = 1
	if _, err := fromBig(big.NewInt(1), 1).QuoExact(ctx, fromBig(big.NewInt(1), -1), exponentLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("exact quotient exponent error = %v", err)
	}
	quotientLimits := limits
	quotientLimits.MaxIntermediateBits = 12
	if _, err := fromBig(big.NewInt(2_047), 0).QuoExact(ctx, fromBig(big.NewInt(3_125), 0), quotientLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("exact quotient power-of-two error = %v", err)
	}
	if _, err := New(1).QuoExact(ctx, fromBig(big.NewInt(1_024), 0), quotientLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("exact quotient power-of-five error = %v", err)
	}
	boundaryQuotientLimits := limits
	boundaryQuotientLimits.MaxIntermediateBits = 5
	if _, err := New(7).QuoExact(ctx, New(2), boundaryQuotientLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("exact quotient boundary error = %v", err)
	}
	if _, err := fromBig(big.NewInt(1), 1).Quantize(ctx, 1, HalfEven, exponentLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("quantize padding error = %v", err)
	}
	if _, err := fromBig(big.NewInt(1), -1).Quantize(ctx, -1, HalfEven, exponentLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("quantize drop error = %v", err)
	}

	alignmentLimits := limits
	alignmentLimits.MaxIntermediateBits = 4
	alignmentLimits.MaxExponentMagnitude = 2
	alignmentContext := Context{
		Precision: 1, MinExponent: -2, MaxExponent: 2,
		Rounding: HalfEven, Limits: alignmentLimits,
	}
	high := fromBig(big.NewInt(1), 2)
	low := fromBig(big.NewInt(1), 0)
	if _, err := alignmentContext.Add(ctx, high, low); !errors.Is(err, ErrLimit) {
		t.Fatalf("context add alignment error = %v", err)
	}
	if _, err := alignmentContext.Sub(ctx, low, high); !errors.Is(err, ErrLimit) {
		t.Fatalf("context subtract alignment error = %v", err)
	}
	if _, err := high.Quantize(ctx, 0, HalfEven, alignmentLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("quantize coefficient error = %v", err)
	}
	if _, err := QuantizedQuo(ctx, high, New(1), 0, HalfEven, alignmentLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("quantized quotient coefficient error = %v", err)
	}
	zeroResult, err := QuantizedQuo(ctx, Decimal{}, New(2), 2, HalfEven, limits)
	if err != nil || !zeroResult.Value.IsZero() {
		t.Fatalf("quantized zero = %+v, %v", zeroResult, err)
	}

	divisionContext := Context{
		Precision: 4, MinExponent: -10, MaxExponent: 10,
		Rounding: HalfEven, Limits: alignmentLimits,
	}
	divisionContext.Limits.MaxExponentMagnitude = 10
	if _, err := divisionContext.Quo(ctx, New(9), New(1)); !errors.Is(err, ErrLimit) {
		t.Fatalf("context quotient intermediate error = %v", err)
	}
	if _, _, err := divide(New(1), New(1), 4, HalfEven, alignmentLimits, false); !errors.Is(err, ErrLimit) {
		t.Fatalf("positive division scale error = %v", err)
	}
	negativeScaleLimits := alignmentLimits
	negativeScaleLimits.MaxExponentMagnitude = 10
	if _, _, err := divide(fromBig(big.NewInt(9_999), 0), New(1), 1, HalfEven, negativeScaleLimits, false); !errors.Is(err, ErrLimit) {
		t.Fatalf("negative division scale error = %v", err)
	}

	boundaryLimits := limits
	boundaryLimits.MaxIntermediateBits = 6
	if _, err := fromBig(big.NewInt(7), 1).AddExact(ctx, New(1), boundaryLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("exact scaled coefficient error = %v", err)
	}
}

func TestDecimalSerializationAndHelpersEdges(t *testing.T) {
	var nilDecimal *Decimal
	if err := nilDecimal.UnmarshalText([]byte("1")); err == nil {
		t.Fatal("expected nil text receiver")
	}
	if err := nilDecimal.UnmarshalJSON([]byte(`"1"`)); err == nil {
		t.Fatal("expected nil JSON receiver")
	}
	var value Decimal
	for _, input := range [][]byte{nil, []byte("1"), []byte(`"bad"`), []byte(`"1" "2"`)} {
		if err := value.UnmarshalJSON(input); err == nil {
			t.Fatalf("expected JSON %q to fail", input)
		}
	}
	if err := value.UnmarshalJSON([]byte(`"`)); err == nil {
		t.Fatal("expected malformed JSON string")
	}
	if err := value.UnmarshalText([]byte("bad")); err == nil {
		t.Fatal("expected text failure")
	}
	encoded, _ := json.Marshal(New(12))
	if string(encoded) != `"12"` {
		t.Fatal("JSON must use strings")
	}
	if !errorsIsRange(&strconv.NumError{Err: strconv.ErrRange}) || errorsIsRange(errors.New("x")) {
		t.Fatal("range classification mismatch")
	}
	if compareInts(1, 0) != 1 || compareInts(0, 1) != -1 || compareInts(1, 1) != 0 {
		t.Fatal("int comparison mismatch")
	}
	if compareInt64(1, 0) != 1 || compareInt64(0, 1) != -1 || compareInt64(1, 1) != 0 {
		t.Fatal("int64 comparison mismatch")
	}
	if _, _, ok := cleanDigits("_1", true); ok {
		t.Fatal("leading underscore accepted")
	}
}

func TestDecimalResourceRejectionEdges(t *testing.T) {
	ctx := context.Background()
	limits := gomath.DefaultLimits()
	invalid := limits
	invalid.MaxInputDigits = 0
	if _, err := ParseWithOptions("1", ParseOptions{Limits: invalid}); err == nil {
		t.Fatal("expected invalid parser limits")
	}
	if _, err := ParseWithOptions("1.a", ParseOptions{AllowLeadingZeros: true, Limits: limits}); err == nil {
		t.Fatal("expected invalid fractional digits")
	}
	tinyExponent := limits
	tinyExponent.MaxExponentMagnitude = 1
	if _, err := ParseWithOptions("0.001", ParseOptions{AllowLeadingZeros: true, Limits: tinyExponent}); err == nil {
		t.Fatal("expected fractional exponent limit")
	}

	tinyBits := limits
	tinyBits.MaxIntermediateBits = 1
	large := fromBig(big.NewInt(7), 0)
	var nilContext context.Context
	if _, err := large.MulExact(ctx, large, tinyBits); err == nil {
		t.Fatal("expected product coefficient limit")
	}
	if _, err := large.AddExact(ctx, large, tinyBits); err == nil {
		t.Fatal("expected aligned coefficient limit")
	}
	if _, err := large.AddExact(nilContext, large, limits); err == nil {
		t.Fatal("expected exact-add context error")
	}
	if _, err := fromBig(big.NewInt(1), 2).QuoExact(ctx, New(1), tinyExponent); err == nil {
		t.Fatal("expected quotient exponent limit")
	}

	operation := Context{Precision: 2, MinExponent: -2, MaxExponent: 2, Rounding: HalfEven, Limits: limits}
	if _, err := operation.Add(nilContext, New(1), New(2)); err == nil {
		t.Fatal("expected Add validation error")
	}
	if _, err := operation.Sub(nilContext, New(1), New(2)); err == nil {
		t.Fatal("expected Sub validation error")
	}
	if _, err := operation.Mul(nilContext, New(1), New(2)); err == nil {
		t.Fatal("expected Mul validation error")
	}
	if _, err := operation.Quo(nilContext, New(1), New(2)); err == nil {
		t.Fatal("expected Quo validation error")
	}
	limitedOperation := operation
	limitedOperation.Limits = tinyBits
	if _, err := limitedOperation.Add(ctx, large, large); err == nil {
		t.Fatal("expected Add work limit")
	}
	if _, err := limitedOperation.Sub(ctx, large, large); err == nil {
		t.Fatal("expected Sub work limit")
	}
	if _, err := limitedOperation.Mul(ctx, large, large); err == nil {
		t.Fatal("expected Mul work limit")
	}
	if _, err := limitedOperation.Quo(ctx, fromBig(big.NewInt(1), 100), New(3)); err == nil {
		t.Fatal("expected Quo work limit")
	}

	if _, err := fromBig(big.NewInt(1), 2).Quantize(ctx, 1, HalfEven, tinyExponent); err == nil {
		t.Fatal("expected quantize padding limit")
	}
	if _, err := fromBig(big.NewInt(1), -2).Quantize(ctx, -1, HalfEven, tinyExponent); err == nil {
		t.Fatal("expected quantize rounding limit")
	}
	outputLimited := limits
	outputLimited.MaxOutputDigits = 1
	if _, err := MustParse("99.9").Quantize(ctx, 0, HalfEven, outputLimited); err == nil {
		t.Fatal("expected quantize output limit")
	}
	if _, err := New(1).Quantize(ctx, 2, HalfEven, tinyExponent); err == nil {
		t.Fatal("expected quantize exponent limit")
	}

	if _, err := QuantizedQuo(nilContext, New(1), New(2), 0, HalfEven, limits); err == nil {
		t.Fatal("expected quotient context error")
	}
	if _, err := QuantizedQuo(ctx, New(1), New(2), 2, HalfEven, tinyExponent); err == nil {
		t.Fatal("expected quotient exponent limit")
	}
	if _, err := QuantizedQuo(ctx, fromBig(big.NewInt(1), 1), New(2), 1, HalfEven, tinyExponent); err == nil {
		t.Fatal("expected quotient scale limit")
	}
	if _, err := QuantizedQuo(ctx, large, New(1), 0, HalfEven, tinyBits); err == nil {
		t.Fatal("expected quotient intermediate limit")
	}
	if _, err := QuantizedQuo(ctx, fromBig(big.NewInt(1), -1), New(2), 0, HalfEven, limits); err != nil {
		t.Fatalf("negative quotient shift: %v", err)
	}
	if _, _, err := divide(New(1), New(1), 10, HalfEven, tinyExponent, false); err == nil {
		t.Fatal("expected positive division scale limit")
	}
	if _, _, err := divide(New(1000), New(1), 1, HalfEven, tinyExponent, false); err == nil {
		t.Fatal("expected negative division scale limit")
	}
	if _, err := QuantizedQuo(ctx, New(99), New(1), 0, HalfEven, outputLimited); err == nil {
		t.Fatal("expected quotient output limit")
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := operation.validate(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal("expected context cancellation")
	}
	badOperation := operation
	badOperation.Limits = invalid
	if _, err := badOperation.validate(ctx); err == nil {
		t.Fatal("expected context limit validation")
	}
	negativeOverflow := operation
	negativeOverflow.MaxExponent = 0
	if result, err := negativeOverflow.Apply(ctx, New(-99)); err != nil || !result.Conditions.Has(gomath.ConditionOverflow) {
		t.Fatal("expected negative overflow")
	}
	if _, err := operation.finish(fromBig(big.NewInt(99), 0), 0, outputLimited); err == nil {
		t.Fatal("expected finish output limit")
	}
}
