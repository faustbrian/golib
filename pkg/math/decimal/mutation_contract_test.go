package decimal

import (
	"context"
	"errors"
	"math/big"
	"testing"

	gomath "github.com/faustbrian/golib/pkg/math"
)

func TestAMutationConstructionAndParsingContracts(t *testing.T) {
	limits := gomath.DefaultLimits()

	exponentLimits := limits
	exponentLimits.MaxExponentMagnitude = 2
	for _, exponent := range []int32{-2, 2} {
		value, err := FromBig(big.NewInt(1), exponent, exponentLimits)
		if err != nil || value.Exponent() != exponent {
			t.Fatalf("FromBig(exponent %d) = %v, %v", exponent, value, err)
		}
	}
	for _, exponent := range []int32{-3, 3} {
		if _, err := FromBig(big.NewInt(1), exponent, exponentLimits); !errors.Is(err, ErrLimit) {
			t.Fatalf("FromBig(exponent %d) error = %v", exponent, err)
		}
	}

	digitLimits := limits
	digitLimits.MaxInputDigits = 3
	if value, err := FromBig(big.NewInt(999), 0, digitLimits); err != nil || value.String() != "999" {
		t.Fatalf("FromBig(exact input digits) = %v, %v", value, err)
	}
	if _, err := FromBig(big.NewInt(1_000), 0, digitLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("FromBig(over input digits) error = %v", err)
	}

	bitLimits := limits
	bitLimits.MaxIntermediateBits = 8
	if value, err := FromBig(big.NewInt(255), 0, bitLimits); err != nil || value.String() != "255" {
		t.Fatalf("FromBig(exact bits) = %v, %v", value, err)
	}
	if _, err := FromBig(big.NewInt(256), 0, bitLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("FromBig(over bits) error = %v", err)
	}

	outputLimits := limits
	outputLimits.MaxOutputDigits = 3
	if value, err := FromBig(big.NewInt(1), 2, outputLimits); err != nil || value.String() != "100" {
		t.Fatalf("FromBig(exact output) = %v, %v", value, err)
	}
	if _, err := FromBig(big.NewInt(1), 3, outputLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("FromBig(over output) error = %v", err)
	}

	parseLimits := limits
	parseLimits.MaxInputDigits = 3
	options := ParseOptions{
		AllowExponent: true, AllowPlus: true, AllowUnderscores: true,
		AllowLeadingZeros: true, AllowWhitespace: true, Limits: parseLimits,
	}
	if value, err := ParseWithOptions(" +1.2_3 ", options); err != nil || value.String() != "1.23" {
		t.Fatalf("ParseWithOptions(exact digits) = %v, %v", value, err)
	}
	if _, err := ParseWithOptions("1.234", options); !errors.Is(err, ErrLimit) {
		t.Fatalf("ParseWithOptions(over digits) error = %v", err)
	}
	if _, err := ParseWithOptions("12.34", options); err == nil || err.Error() != "math: resource limit exceeded: decimal input digits" {
		t.Fatalf("ParseWithOptions(input digit preflight) error = %v", err)
	}
	options.Limits = exponentLimits
	for input, want := range map[string]string{"1e2": "100", "1e-2": "0.01"} {
		value, err := ParseWithOptions(input, options)
		if err != nil || value.String() != want {
			t.Fatalf("ParseWithOptions(%q) = %v, %v", input, value, err)
		}
	}
	for _, input := range []string{"1e3", "1e-3"} {
		if _, err := ParseWithOptions(input, options); !errors.Is(err, ErrLimit) {
			t.Fatalf("ParseWithOptions(%q) error = %v", input, err)
		}
	}
	strict := ParseOptions{Limits: limits}
	for _, input := range []string{"", " 1", "1 ", "+1", "01", "1_0"} {
		if _, err := ParseWithOptions(input, strict); !errors.Is(err, ErrInvalid) {
			t.Fatalf("strict ParseWithOptions(%q) error = %v", input, err)
		}
	}
}

func TestAMutationRepresentationAndComparisonContracts(t *testing.T) {
	values := []struct {
		coefficient int64
		exponent    int32
		text        string
		rational    *big.Rat
	}{
		{-12, 0, "-12", big.NewRat(-12, 1)},
		{12, 1, "120", big.NewRat(120, 1)},
		{12, -1, "1.2", big.NewRat(6, 5)},
		{12, -2, "0.12", big.NewRat(3, 25)},
		{12, -3, "0.012", big.NewRat(3, 250)},
	}
	for _, test := range values {
		value := fromBig(big.NewInt(test.coefficient), test.exponent)
		if got := value.String(); got != test.text {
			t.Fatalf("String(%d,%d) = %q", test.coefficient, test.exponent, got)
		}
		if got := value.BigRat(); got.Cmp(test.rational) != 0 {
			t.Fatalf("BigRat(%s) = %s", value, got)
		}
	}

	comparisons := []struct {
		left, right Decimal
		want        int
	}{
		{New(-1), Decimal{}, -1},
		{Decimal{}, New(-1), 1},
		{Decimal{}, Decimal{}, 0},
		{MustParse("9"), MustParse("10"), -1},
		{MustParse("10"), MustParse("9"), 1},
		{MustParse("-9"), MustParse("-10"), 1},
		{MustParse("-10"), MustParse("-9"), -1},
		{MustParse("1.0"), MustParse("1.00"), 0},
	}
	for _, test := range comparisons {
		if got := test.left.Cmp(test.right); got != test.want {
			t.Fatalf("Cmp(%s,%s) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
	if !MustParse("1.0").SameRepresentation(MustParse("1.0")) {
		t.Fatal("identical representations differ")
	}
	if MustParse("1.0").SameRepresentation(MustParse("1.00")) {
		t.Fatal("different exponents have the same representation")
	}
	if MustParse("1.0").SameRepresentation(MustParse("2.0")) {
		t.Fatal("different coefficients have the same representation")
	}
}

func TestAMutationExactArithmeticContracts(t *testing.T) {
	ctx := context.Background()
	limits := gomath.DefaultLimits()

	for _, test := range []struct {
		left, right Decimal
		operation   string
		want        string
	}{
		{MustParse("1.2"), MustParse("0.03"), "add", "1.23"},
		{MustParse("1.2"), MustParse("0.03"), "subtract", "1.17"},
		{MustParse("1.2"), MustParse("0.03"), "multiply", "0.036"},
	} {
		var value Decimal
		var err error
		switch test.operation {
		case "add":
			value, err = test.left.AddExact(ctx, test.right, limits)
		case "subtract":
			value, err = test.left.SubExact(ctx, test.right, limits)
		case "multiply":
			value, err = test.left.MulExact(ctx, test.right, limits)
		}
		if err != nil || value.String() != test.want {
			t.Fatalf("%s = %v, %v", test.operation, value, err)
		}
	}

	exponentLimits := limits
	exponentLimits.MaxExponentMagnitude = 2
	if value, err := fromBig(big.NewInt(1), 1).MulExact(ctx, fromBig(big.NewInt(1), 1), exponentLimits); err != nil || value.Exponent() != 2 {
		t.Fatalf("MulExact(exact exponent) = %v, %v", value, err)
	}
	for _, exponents := range [][2]int32{{2, 1}, {-2, -1}} {
		if _, err := fromBig(big.NewInt(1), exponents[0]).MulExact(ctx, fromBig(big.NewInt(1), exponents[1]), exponentLimits); !errors.Is(err, ErrLimit) {
			t.Fatalf("MulExact(exponents %v) error = %v", exponents, err)
		}
	}
	bitLimits := limits
	bitLimits.MaxIntermediateBits = 2
	if value, err := New(3).MulExact(ctx, New(1), bitLimits); err != nil || value.String() != "3" {
		t.Fatalf("MulExact(exact preflight bits) = %v, %v", value, err)
	}

	for _, test := range []struct {
		numerator, denominator int64
		want                   string
	}{
		{1, 40, "0.025"},
		{1, 250, "0.004"},
		{-1, 40, "-0.025"},
		{1, -40, "-0.025"},
		{3, 6, "0.5"},
	} {
		value, err := New(test.numerator).QuoExact(ctx, New(test.denominator), limits)
		if err != nil || value.String() != test.want {
			t.Fatalf("QuoExact(%d,%d) = %v, %v", test.numerator, test.denominator, value, err)
		}
	}
	if value, err := fromBig(big.NewInt(1), 2).QuoExact(ctx, New(1), exponentLimits); err != nil || value.Exponent() != 2 {
		t.Fatalf("QuoExact(exact exponent) = %v, %v", value, err)
	}
	if _, err := fromBig(big.NewInt(1), 2).QuoExact(ctx, fromBig(big.NewInt(1), -1), exponentLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("QuoExact(over exponent) error = %v", err)
	}
}

func TestAMutationQuantizeAndDivisionContracts(t *testing.T) {
	ctx := context.Background()
	limits := gomath.DefaultLimits()

	quantizeCases := []struct {
		input string
		scale int32
		mode  RoundingMode
		value string
		flags gomath.Condition
	}{
		{"0", 2, HalfEven, "0.00", 0},
		{"1.20", 2, HalfEven, "1.20", 0},
		{"1.2", 2, HalfEven, "1.20", 0},
		{"1.24", 1, HalfEven, "1.2", gomath.ConditionRounded | gomath.ConditionInexact},
		{"1.25", 1, HalfEven, "1.2", gomath.ConditionRounded | gomath.ConditionInexact},
		{"1.35", 1, HalfEven, "1.4", gomath.ConditionRounded | gomath.ConditionInexact},
		{"-1.25", 1, HalfUp, "-1.3", gomath.ConditionRounded | gomath.ConditionInexact},
		{"1.20", 1, HalfEven, "1.2", gomath.ConditionRounded},
	}
	for _, test := range quantizeCases {
		result, err := MustParse(test.input).Quantize(ctx, test.scale, test.mode, limits)
		if err != nil || result.Value.String() != test.value || result.Conditions != test.flags {
			t.Fatalf("Quantize(%s,%d,%s) = %s [%s], %v", test.input, test.scale, test.mode, result.Value, result.Conditions, err)
		}
	}

	exponentLimits := limits
	exponentLimits.MaxExponentMagnitude = 2
	for _, scale := range []int32{-2, 2} {
		if _, err := New(1).Quantize(ctx, scale, HalfEven, exponentLimits); err != nil {
			t.Fatalf("Quantize(exact scale %d) error = %v", scale, err)
		}
	}
	for _, scale := range []int32{-3, 3} {
		if _, err := New(1).Quantize(ctx, scale, HalfEven, exponentLimits); !errors.Is(err, ErrLimit) {
			t.Fatalf("Quantize(over scale %d) error = %v", scale, err)
		}
	}

	divisionCases := []struct {
		numerator, denominator int64
		scale                  int32
		mode                   RoundingMode
		value                  string
		flags                  gomath.Condition
	}{
		{0, 3, 2, HalfEven, "0.00", 0},
		{1, 4, 2, HalfEven, "0.25", 0},
		{1, 8, 2, HalfEven, "0.12", gomath.ConditionRounded | gomath.ConditionInexact},
		{3, 8, 2, HalfEven, "0.38", gomath.ConditionRounded | gomath.ConditionInexact},
		{-1, 8, 2, HalfUp, "-0.13", gomath.ConditionRounded | gomath.ConditionInexact},
		{1, -8, 2, HalfUp, "-0.13", gomath.ConditionRounded | gomath.ConditionInexact},
	}
	for _, test := range divisionCases {
		result, err := QuantizedQuo(ctx, New(test.numerator), New(test.denominator), test.scale, test.mode, limits)
		if err != nil || result.Value.String() != test.value || result.Conditions != test.flags {
			t.Fatalf("QuantizedQuo(%d,%d) = %s [%s], %v", test.numerator, test.denominator, result.Value, result.Conditions, err)
		}
	}
}

func TestAMutationContextBoundaryContracts(t *testing.T) {
	ctx := context.Background()
	limits := gomath.DefaultLimits()
	operation := Context{
		Precision: 2, MinExponent: -2, MaxExponent: 2,
		Rounding: HalfEven, Limits: limits,
	}

	for input, want := range map[string]string{
		"9.9":   "9.9",
		"9.99":  "10",
		"-9.99": "-10",
	} {
		result, err := operation.Apply(ctx, MustParse(input))
		if err != nil || result.Value.String() != want {
			t.Fatalf("Apply(%s) = %s [%s], %v", input, result.Value, result.Conditions, err)
		}
	}

	overflow := operation
	overflow.MaxExponent = 0
	for input, want := range map[string]string{"999": "9.9", "-999": "-9.9"} {
		result, err := overflow.Apply(ctx, MustParse(input))
		wantFlags := gomath.ConditionOverflow | gomath.ConditionRounded | gomath.ConditionInexact
		if err != nil || result.Value.String() != want || result.Conditions != wantFlags {
			t.Fatalf("overflow Apply(%s) = %s [%s], %v", input, result.Value, result.Conditions, err)
		}
	}

	for input, want := range map[string]string{"0.001": "0.001", "0.0001": "0.000"} {
		result, err := operation.Apply(ctx, MustParse(input))
		if err != nil || result.Value.String() != want || !result.Conditions.Has(gomath.ConditionSubnormal) {
			t.Fatalf("subnormal Apply(%s) = %s [%s], %v", input, result.Value, result.Conditions, err)
		}
	}

	precisionLimits := limits
	precisionLimits.MaxPrecision = 2
	precisionLimits.MaxIntermediateBits = 2
	for _, precision := range []uint32{1, 2} {
		candidate := operation
		candidate.Precision = precision
		candidate.Limits = precisionLimits
		if _, err := candidate.Apply(ctx, New(1)); err != nil {
			t.Fatalf("Apply(precision %d) error = %v", precision, err)
		}
	}
	for _, precision := range []uint32{0, 3} {
		candidate := operation
		candidate.Precision = precision
		candidate.Limits = precisionLimits
		if _, err := candidate.Apply(ctx, New(1)); err == nil {
			t.Fatalf("Apply(precision %d) succeeded", precision)
		}
	}
}

func TestAMutationHelperContracts(t *testing.T) {
	limits := gomath.DefaultLimits()

	for _, test := range []struct {
		coefficient int64
		shift       uint32
		want        string
	}{
		{0, 3, "0"},
		{7, 0, "7"},
		{7, 1, "70"},
		{-7, 2, "-700"},
	} {
		result, err := scaleCoefficient(big.NewInt(test.coefficient), test.shift, limits)
		if err != nil || result.String() != test.want {
			t.Fatalf("scaleCoefficient(%d,%d) = %v, %v", test.coefficient, test.shift, result, err)
		}
	}
	boundary := limits
	boundary.MaxIntermediateBits = 7
	if result, err := scaleCoefficient(big.NewInt(1), 2, boundary); err != nil || result.String() != "100" {
		t.Fatalf("scaleCoefficient(exact bits) = %v, %v", result, err)
	}
	boundary.MaxIntermediateBits = 6
	if _, err := scaleCoefficient(big.NewInt(1), 2, boundary); !errors.Is(err, ErrLimit) {
		t.Fatalf("scaleCoefficient(over bits) error = %v", err)
	}

	for _, test := range []struct {
		coefficient, base int64
		exponent          uint32
		want              string
	}{
		{3, 2, 3, "24"},
		{3, 5, 2, "75"},
	} {
		result, err := multiplyPowerLimited(big.NewInt(test.coefficient), test.base, test.exponent, limits)
		if err != nil || result.String() != test.want {
			t.Fatalf("multiplyPowerLimited(%d,%d,%d) = %v, %v", test.coefficient, test.base, test.exponent, result, err)
		}
	}

	roundCases := []struct {
		coefficient int64
		drop        uint32
		mode        RoundingMode
		want        string
		flags       gomath.Condition
	}{
		{20, 1, HalfEven, "2", gomath.ConditionRounded},
		{24, 1, HalfEven, "2", gomath.ConditionRounded | gomath.ConditionInexact},
		{25, 1, HalfEven, "2", gomath.ConditionRounded | gomath.ConditionInexact},
		{35, 1, HalfEven, "4", gomath.ConditionRounded | gomath.ConditionInexact},
		{-25, 1, HalfUp, "-3", gomath.ConditionRounded | gomath.ConditionInexact},
	}
	for _, test := range roundCases {
		result, flags := roundCoefficient(big.NewInt(test.coefficient), test.drop, test.mode)
		if result.String() != test.want || flags != test.flags {
			t.Fatalf("roundCoefficient(%d) = %s [%s]", test.coefficient, result, flags)
		}
	}

	incrementCases := []struct {
		quotient, remainder, divisor int64
		sign                         int
		mode                         RoundingMode
		want                         bool
	}{
		{2, 1, 10, 1, Down, false},
		{2, 1, 10, 1, Up, true},
		{2, 1, 10, 1, Ceiling, true},
		{2, 1, 10, -1, Ceiling, false},
		{2, 1, 10, 1, Floor, false},
		{2, 1, 10, -1, Floor, true},
		{2, 6, 10, 1, HalfEven, true},
		{2, 4, 10, 1, HalfEven, false},
		{2, 5, 10, 1, HalfUp, true},
		{2, 5, 10, 1, HalfDown, false},
		{2, 5, 10, 1, HalfEven, false},
		{3, 5, 10, 1, HalfEven, true},
	}
	for _, test := range incrementCases {
		got := shouldIncrement(
			big.NewInt(test.quotient), big.NewInt(test.remainder),
			big.NewInt(test.divisor), test.sign, test.mode,
		)
		if got != test.want {
			t.Fatalf("shouldIncrement(%+v) = %v", test, got)
		}
	}

	for _, test := range []struct {
		value, factor int64
		count         uint32
		remaining     string
	}{
		{40, 2, 3, "5"},
		{125, 5, 3, "1"},
		{7, 2, 0, "7"},
	} {
		value := big.NewInt(test.value)
		if count := removeFactor(value, test.factor); count != test.count || value.String() != test.remaining {
			t.Fatalf("removeFactor(%d,%d) = %d, %s", test.value, test.factor, count, value)
		}
	}
}

func TestAMutationPrimitiveBoundaryContracts(t *testing.T) {
	for _, test := range []struct {
		value int64
		want  uint64
	}{
		{value: -1 << 63, want: 1 << 63},
		{value: -2, want: 2},
		{value: -1, want: 1},
		{value: 0, want: 0},
		{value: 1, want: 1},
		{value: 2, want: 2},
	} {
		if got := integerMagnitude(test.value); got != test.want {
			t.Fatalf("integerMagnitude(%d) = %d", test.value, got)
		}
	}
	for _, test := range []struct {
		exponent int32
		want     uint32
	}{
		{-2, 2}, {-1, 1}, {0, 0}, {1, 1}, {2, 2},
	} {
		if got := exponentMagnitude(test.exponent); got != test.want {
			t.Fatalf("exponentMagnitude(%d) = %d", test.exponent, got)
		}
	}
	for _, test := range []struct {
		left, right int32
		want        uint32
	}{
		{-2, 1, 3}, {1, -2, 3}, {1, 1, 0},
	} {
		if got := exponentDifference(test.left, test.right); got != test.want {
			t.Fatalf("exponentDifference(%d,%d) = %d", test.left, test.right, got)
		}
	}
	for _, test := range []struct {
		coefficient int64
		exponent    int32
		want        int
	}{
		{12, 1, 3}, {12, 0, 2}, {12, -1, 2}, {12, -2, 3}, {12, -3, 4},
	} {
		if got := outputDigits(big.NewInt(test.coefficient), test.exponent); got != test.want {
			t.Fatalf("outputDigits(%d,%d) = %d", test.coefficient, test.exponent, got)
		}
	}
	for _, test := range []struct {
		numerator, denominator int64
		exponent               int64
		want                   int
	}{
		{10, 1, 1, 0},
		{9, 1, 1, -1},
		{11, 1, 1, 1},
		{1, 10, -1, 0},
		{1, 11, -1, -1},
		{1, 9, -1, 1},
	} {
		if got := compareRatioPower10(big.NewInt(test.numerator), big.NewInt(test.denominator), test.exponent); got != test.want {
			t.Fatalf("compareRatioPower10(%+v) = %d", test, got)
		}
	}

	options := ParseOptions{AllowExponent: true, Limits: gomath.DefaultLimits()}
	for input, wantExponent := range map[string]int32{"1": 0, "1e0": 0, "1e1": 1, "1e-1": -1} {
		mantissa, exponent, err := splitExponent(input, options)
		if err != nil || mantissa != "1" || exponent != wantExponent {
			t.Fatalf("splitExponent(%q) = %q, %d, %v", input, mantissa, exponent, err)
		}
	}
	for _, input := range []string{"e1", "1e", "1e1e2"} {
		if _, _, err := splitExponent(input, options); !errors.Is(err, ErrInvalid) {
			t.Fatalf("splitExponent(%q) error = %v", input, err)
		}
	}
	for input, want := range map[string]string{"0": "0", "123": "123", "1_2_3": "123"} {
		digits, count, err := cleanDigits(input, true, len(want))
		if err != nil || digits != want || count != len(want) {
			t.Fatalf("cleanDigits(%q) = %q, %d, %v", input, digits, count, err)
		}
	}
	for _, input := range []string{"", "_1", "1_", "1__2", "1a"} {
		if _, _, err := cleanDigits(input, true, len(input)); err == nil {
			t.Fatalf("cleanDigits(%q) succeeded", input)
		}
	}
	if _, _, err := cleanDigits("1_2", false, 2); err == nil {
		t.Fatal("cleanDigits accepted disabled underscores")
	}
	if _, _, err := cleanDigits("12", true, 1); !errors.Is(err, ErrLimit) {
		t.Fatalf("cleanDigits over limit error = %v", err)
	}
}

func TestAMutationPreflightAndExactBoundaryContracts(t *testing.T) {
	ctx := context.Background()
	limits := gomath.DefaultLimits()

	parseLimits := limits
	parseLimits.MaxExponentMagnitude = 2
	options := ParseOptions{
		AllowExponent: true,
		Limits:        parseLimits,
	}
	if _, err := ParseWithOptions("1.23e-1", options); err == nil || err.Error() != "math: resource limit exceeded: decimal exponent" {
		t.Fatalf("combined negative exponent error = %v", err)
	}

	exponentLimits := limits
	exponentLimits.MaxExponentMagnitude = 2
	for _, test := range []struct {
		left, right Decimal
		wantError   string
	}{
		{fromBig(big.NewInt(1), -2), fromBig(big.NewInt(1), 0), ""},
		{fromBig(big.NewInt(1), -2), fromBig(big.NewInt(1), -1), "math: resource limit exceeded: product exponent"},
		{fromBig(big.NewInt(1), 2), fromBig(big.NewInt(1), 1), "math: resource limit exceeded: product exponent"},
	} {
		value, err := test.left.MulExact(ctx, test.right, exponentLimits)
		if test.wantError == "" {
			if err != nil || value.Exponent() != -2 {
				t.Fatalf("MulExact negative boundary = %v, %v", value, err)
			}
		} else if err == nil || err.Error() != test.wantError {
			t.Fatalf("MulExact preflight error = %v, want %q", err, test.wantError)
		}
	}

	bitLimits := limits
	bitLimits.MaxIntermediateBits = 2
	if _, err := New(3).MulExact(ctx, New(3), bitLimits); err == nil || err.Error() != "math: resource limit exceeded: product coefficient" {
		t.Fatalf("MulExact coefficient preflight error = %v", err)
	}

	for _, test := range []struct {
		numerator, denominator Decimal
		wantError              string
	}{
		{fromBig(big.NewInt(1), -2), New(1), ""},
		{fromBig(big.NewInt(1), -2), fromBig(big.NewInt(1), 1), "math: resource limit exceeded: quotient exponent"},
		{fromBig(big.NewInt(1), 2), fromBig(big.NewInt(1), -1), "math: resource limit exceeded: quotient exponent"},
	} {
		value, err := test.numerator.QuoExact(ctx, test.denominator, exponentLimits)
		if test.wantError == "" {
			if err != nil || value.Exponent() != -2 {
				t.Fatalf("QuoExact negative boundary = %v, %v", value, err)
			}
		} else if err == nil || err.Error() != test.wantError {
			t.Fatalf("QuoExact preflight error = %v, want %q", err, test.wantError)
		}
	}

	if got := fromBig(big.NewInt(11), 0).Cmp(fromBig(big.NewInt(12), 0)); got != -1 {
		t.Fatalf("equal-adjusted Cmp = %d", got)
	}
	if got := fromBig(big.NewInt(-11), 0).Cmp(fromBig(big.NewInt(-12), 0)); got != 1 {
		t.Fatalf("negative equal-adjusted Cmp = %d", got)
	}
}

func TestAMutationContextAndClampEdgeContracts(t *testing.T) {
	ctx := context.Background()
	limits := gomath.DefaultLimits()
	operation := Context{
		Precision: 2, MinExponent: -2, MaxExponent: 2,
		Rounding: HalfEven, Limits: limits,
	}

	result, err := operation.Quo(ctx, MustParse("10"), MustParse("2"))
	if err != nil || result.Value.String() != "5" || result.Conditions != 0 {
		t.Fatalf("exact precision-boundary Quo = %s [%s], %v", result.Value, result.Conditions, err)
	}
	result, err = operation.Quo(ctx, MustParse("1"), MustParse("20"))
	if err != nil || result.Value.String() != "0.05" || result.Conditions != 0 {
		t.Fatalf("denominator precision-boundary Quo = %s [%s], %v", result.Value, result.Conditions, err)
	}
	result, err = operation.Apply(ctx, MustParse("9.99"))
	if err != nil || result.Value.String() != "10" || result.Conditions != gomath.ConditionRounded|gomath.ConditionInexact {
		t.Fatalf("rounded Apply = %s [%s], %v", result.Value, result.Conditions, err)
	}

	for _, exponent := range []int32{-2, 2} {
		value := fromBig(big.NewInt(1), exponent)
		result, err = value.Quantize(ctx, -exponent, HalfEven, limits)
		if err != nil || !result.Value.SameRepresentation(value) || result.Conditions != 0 {
			t.Fatalf("same-exponent Quantize(%d) = %s [%s], %v", exponent, result.Value, result.Conditions, err)
		}
	}

	for _, test := range []struct {
		value, minimum, maximum string
		want                    string
	}{
		{"1", "1", "2", "1"},
		{"2", "1", "2", "2"},
		{"0", "1", "2", "1"},
		{"3", "1", "2", "2"},
	} {
		got, clampErr := MustParse(test.value).Clamp(MustParse(test.minimum), MustParse(test.maximum))
		if clampErr != nil || got.String() != test.want {
			t.Fatalf("Clamp(%s,%s,%s) = %s, %v", test.value, test.minimum, test.maximum, got, clampErr)
		}
	}
	if _, err := New(1).Clamp(New(1), New(1)); err != nil {
		t.Fatalf("Clamp equal interval error = %v", err)
	}
	represented := MustParse("1.0")
	minimum := MustParse("1.00")
	got, err := represented.Clamp(minimum, New(2))
	if err != nil || !got.SameRepresentation(represented) {
		t.Fatalf("Clamp lower equality changed representation: %s, %v", got, err)
	}
	maximum := MustParse("2.00")
	represented = MustParse("2.0")
	got, err = represented.Clamp(New(1), maximum)
	if err != nil || !got.SameRepresentation(represented) {
		t.Fatalf("Clamp upper equality changed representation: %s, %v", got, err)
	}

	positiveZero := fromBig(big.NewInt(0), 2)
	negativeZero := fromBig(big.NewInt(0), -2)
	if got := positiveZero.canonicalText(); got != "0" {
		t.Fatalf("positive-exponent zero canonical text = %q", got)
	}
	if got := negativeZero.canonicalText(); got != "0.00" {
		t.Fatalf("negative-exponent zero canonical text = %q", got)
	}
}

func TestAMutationQuantizedQuotientScaleContracts(t *testing.T) {
	ctx := context.Background()
	limits := gomath.DefaultLimits()
	exponentLimits := limits
	exponentLimits.MaxExponentMagnitude = 2

	for _, test := range []struct {
		numerator, denominator Decimal
		scale                  int32
		want                   string
	}{
		{fromBig(big.NewInt(1), 0), fromBig(big.NewInt(1), -2), 0, "100"},
		{fromBig(big.NewInt(1), -2), fromBig(big.NewInt(1), 0), 0, "0"},
		{New(1), New(2), 0, "0"},
	} {
		result, err := QuantizedQuo(ctx, test.numerator, test.denominator, test.scale, HalfEven, exponentLimits)
		if err != nil || result.Value.String() != test.want {
			t.Fatalf("QuantizedQuo scale boundary = %s [%s], %v", result.Value, result.Conditions, err)
		}
	}

	for _, test := range []struct {
		numerator, denominator Decimal
		wantError              string
	}{
		{fromBig(big.NewInt(1), 2), fromBig(big.NewInt(1), -1), "math: resource limit exceeded: quotient scale"},
		{fromBig(big.NewInt(1), -2), fromBig(big.NewInt(1), 1), "math: resource limit exceeded: quotient scale"},
	} {
		if _, err := QuantizedQuo(ctx, test.numerator, test.denominator, 0, HalfEven, exponentLimits); err == nil || err.Error() != test.wantError {
			t.Fatalf("QuantizedQuo scale preflight error = %v", err)
		}
	}
}

func TestAMutationHelperLimitAndRoundingEdges(t *testing.T) {
	limits := gomath.DefaultLimits()

	for _, test := range []struct {
		coefficient int64
		shift       uint32
		bits        int
		wantError   bool
	}{
		{1, 1, 4, false},
		{1, 1, 3, true},
		{3, 1, 5, false},
		{3, 1, 4, true},
	} {
		bounded := limits
		bounded.MaxIntermediateBits = test.bits
		_, err := scaleCoefficient(big.NewInt(test.coefficient), test.shift, bounded)
		if (err != nil) != test.wantError {
			t.Fatalf("scaleCoefficient(%d,%d,bits=%d) error = %v", test.coefficient, test.shift, test.bits, err)
		}
	}

	for _, test := range []struct {
		coefficient, base int64
		exponent          uint32
		bits              int
		wantError         bool
	}{
		{1, 2, 2, 3, false},
		{1, 2, 2, 2, true},
		{1, 5, 1, 3, false},
		{1, 5, 1, 2, true},
	} {
		bounded := limits
		bounded.MaxIntermediateBits = test.bits
		_, err := multiplyPowerLimited(big.NewInt(test.coefficient), test.base, test.exponent, bounded)
		if (err != nil) != test.wantError {
			t.Fatalf("multiplyPowerLimited(%+v) error = %v", test, err)
		}
	}

	for _, test := range []struct {
		sign int
		mode RoundingMode
		want bool
	}{
		{0, Ceiling, false},
		{0, Floor, false},
		{1, Ceiling, true},
		{-1, Floor, true},
	} {
		if got := shouldIncrement(big.NewInt(1), big.NewInt(1), big.NewInt(3), test.sign, test.mode); got != test.want {
			t.Fatalf("shouldIncrement(sign=%d, mode=%s) = %v", test.sign, test.mode, got)
		}
	}
}

func TestAMutationInternalContextAndDivisionContracts(t *testing.T) {
	limits := gomath.DefaultLimits()
	base := Context{
		Precision: 2, MinExponent: -2, MaxExponent: 2,
		Rounding: HalfEven, Limits: limits,
	}
	for name, mutate := range map[string]func(*Context){
		"rounding":       func(candidate *Context) { candidate.Rounding = RoundingMode(255) },
		"exponent order": func(candidate *Context) { candidate.MinExponent = 3 },
		"minimum bound":  func(candidate *Context) { candidate.MinExponent = -3; candidate.Limits.MaxExponentMagnitude = 2 },
		"maximum bound":  func(candidate *Context) { candidate.MaxExponent = 3; candidate.Limits.MaxExponentMagnitude = 2 },
	} {
		candidate := base
		mutate(&candidate)
		if _, err := candidate.validate(context.Background()); err == nil {
			t.Fatalf("validate accepted invalid %s", name)
		}
	}
	equalExponentContext := base
	equalExponentContext.MinExponent = 0
	equalExponentContext.MaxExponent = 0
	if _, err := equalExponentContext.validate(context.Background()); err != nil {
		t.Fatalf("validate rejected equal exponent interval: %v", err)
	}

	incoming := gomath.ConditionDivisionByZero
	overflow, err := base.finish(fromBig(big.NewInt(9990), 0), incoming, limits)
	if err != nil || !overflow.Conditions.Has(incoming|gomath.ConditionOverflow|gomath.ConditionRounded|gomath.ConditionInexact) {
		t.Fatalf("finish overflow = %s [%s], %v", overflow.Value, overflow.Conditions, err)
	}
	subnormal, err := base.finish(fromBig(big.NewInt(1), -3), incoming, limits)
	if err != nil || !subnormal.Conditions.Has(incoming|gomath.ConditionSubnormal) {
		t.Fatalf("finish subnormal = %s [%s], %v", subnormal.Value, subnormal.Conditions, err)
	}
	exactSubnormal, err := base.finish(fromBig(big.NewInt(1), -3), 0, limits)
	if err != nil || exactSubnormal.Conditions != gomath.ConditionSubnormal {
		t.Fatalf("finish exact subnormal = %s [%s], %v", exactSubnormal.Value, exactSubnormal.Conditions, err)
	}

	outputLimits := limits
	outputLimits.MaxOutputDigits = 3
	outputContext := base
	outputContext.MaxExponent = 10
	if _, err := outputContext.finish(fromBig(big.NewInt(1), 2), 0, outputLimits); err != nil {
		t.Fatalf("finish exact output boundary error = %v", err)
	}
	if _, err := outputContext.finish(fromBig(big.NewInt(1), 3), 0, outputLimits); !errors.Is(err, ErrLimit) {
		t.Fatalf("finish output overflow error = %v", err)
	}

	for _, exponent := range []int64{-2, 2} {
		got, err := checkedExponent(exponent, 2)
		if err != nil || int64(got) != exponent {
			t.Fatalf("checkedExponent(%d) = %d, %v", exponent, got, err)
		}
	}
	for _, exponent := range []int64{-3, 3} {
		if _, err := checkedExponent(exponent, 2); !errors.Is(err, ErrLimit) {
			t.Fatalf("checkedExponent(%d) error = %v", exponent, err)
		}
	}

	for _, test := range []struct {
		numerator, denominator Decimal
		precision              uint32
		preserve               bool
		want                   string
		conditions             gomath.Condition
	}{
		{New(1), New(2), 2, false, "0.5", 0},
		{New(1), New(20), 2, false, "0.05", 0},
		{New(1), New(20), 2, true, "0.050", gomath.ConditionRounded},
		{New(10), New(1), 2, false, "10", 0},
		{New(2), New(3), 1, false, "0.7", gomath.ConditionRounded | gomath.ConditionInexact},
		{New(-2), New(3), 1, false, "-0.7", gomath.ConditionRounded | gomath.ConditionInexact},
	} {
		value, conditions, divideErr := divide(test.numerator, test.denominator, test.precision, HalfEven, limits, test.preserve)
		if divideErr != nil || value.String() != test.want || conditions != test.conditions {
			t.Fatalf("divide(%s,%s,p=%d,preserve=%v) = %s [%s], %v", test.numerator, test.denominator, test.precision, test.preserve, value, conditions, divideErr)
		}
	}
	preserved, preservedConditions, preservedErr := divide(New(10), New(1), 2, HalfEven, limits, true)
	if preservedErr != nil || !preserved.SameRepresentation(MustParse("10")) || preservedConditions != gomath.ConditionRounded {
		t.Fatalf("preserved exact power ratio = %s (exponent %d) [%s], %v", preserved, preserved.Exponent(), preservedConditions, preservedErr)
	}

	divisionScaleLimits := limits
	divisionScaleLimits.MaxExponentMagnitude = 2
	for _, test := range []struct {
		numerator, denominator Decimal
		precision              uint32
		want                   string
	}{
		{New(1), New(2), 2, "0.5"},
		{New(100), New(1), 1, "100"},
	} {
		value, _, divideErr := divide(test.numerator, test.denominator, test.precision, HalfEven, divisionScaleLimits, false)
		if divideErr != nil || value.String() != test.want {
			t.Fatalf("divide scale boundary (%s/%s) = %s, %v", test.numerator, test.denominator, value, divideErr)
		}
	}
	for _, mode := range []RoundingMode{Ceiling, Floor} {
		value, _, divideErr := divide(New(-2), New(3), 1, mode, limits, false)
		want := "-0.6"
		if mode == Floor {
			want = "-0.7"
		}
		if divideErr != nil || value.String() != want {
			t.Fatalf("negative divide(%s) = %s, %v", mode, value, divideErr)
		}
	}

	alignmentLimits := limits
	alignmentLimits.MaxExponentMagnitude = 2
	value, addErr := fromBig(big.NewInt(1), -2).AddExact(context.Background(), New(1), alignmentLimits)
	if addErr != nil || value.String() != "1.01" {
		t.Fatalf("AddExact alignment boundary = %s, %v", value, addErr)
	}
	if _, addErr := fromBig(big.NewInt(1), -2).AddExact(context.Background(), fromBig(big.NewInt(1), 1), alignmentLimits); addErr == nil || addErr.Error() != "math: resource limit exceeded: exponent alignment" {
		t.Fatalf("AddExact alignment preflight error = %v", addErr)
	}

	for _, mode := range []RoundingMode{Ceiling, Floor} {
		result, quantizeErr := QuantizedQuo(context.Background(), New(-1), New(8), 2, mode, limits)
		want := "-0.12"
		if mode == Floor {
			want = "-0.13"
		}
		if quantizeErr != nil || result.Value.String() != want {
			t.Fatalf("negative QuantizedQuo(%s) = %s, %v", mode, result.Value, quantizeErr)
		}
	}

	for _, test := range []struct {
		exponent       uint32
		bitsPerBillion uint64
		want           uint64
	}{
		{0, 3_321_928_094, 0},
		{1, 3_321_928_094, 3},
		{2, 3_321_928_094, 6},
		{1, 2_321_928_094, 2},
		{2, 2_321_928_094, 4},
	} {
		if got := estimatedBitGrowth(test.exponent, test.bitsPerBillion); got != test.want {
			t.Fatalf("estimatedBitGrowth(%+v) = %d", test, got)
		}
	}
}
