// Package decimal provides immutable finite base-10 decimals with explicit
// precision, rounding, exponent, trap, condition, and resource policies.
package decimal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	gomath "github.com/faustbrian/golib/pkg/math"
)

var (
	ErrInvalid        = gomath.ErrInvalidSyntax
	ErrDivisionByZero = gomath.ErrDivisionByZero
	ErrNonTerminating = gomath.ErrConversion
	ErrLimit          = gomath.ErrLimitExceeded
)

// RoundingMode is the shared math rounding contract.
type RoundingMode = gomath.RoundingMode

const (
	HalfEven = gomath.RoundHalfEven
	HalfUp   = gomath.RoundHalfUp
	HalfDown = gomath.RoundHalfDown
	Down     = gomath.RoundDown
	Up       = gomath.RoundUp
	Ceiling  = gomath.RoundCeiling
	Floor    = gomath.RoundFloor
)

// ParseOptions controls the strict finite-decimal grammar.
type ParseOptions struct {
	AllowExponent     bool
	AllowPlus         bool
	AllowUnderscores  bool
	AllowLeadingZeros bool
	AllowWhitespace   bool
	Limits            gomath.Limits
}

// Context defines finite-precision decimal arithmetic. MinExponent and
// MaxExponent bound the adjusted exponent of normal results.
type Context struct {
	Precision   uint32
	MinExponent int32
	MaxExponent int32
	Rounding    RoundingMode
	Traps       gomath.Condition
	Limits      gomath.Limits
}

// Result contains a decimal value and every condition raised while producing it.
type Result struct {
	Value      Decimal
	Conditions gomath.Condition
}

// Decimal is an immutable coefficient multiplied by 10^exponent. Its zero
// value is canonical numeric zero.
type Decimal struct {
	coefficient big.Int
	exponent    int32
}

// New constructs an integral Decimal.
func New(value int64) Decimal {
	var coefficient big.Int
	coefficient.SetInt64(value)

	return fromBig(&coefficient, 0)
}

// FromBig constructs a Decimal by defensively copying coefficient.
func FromBig(coefficient *big.Int, exponent int32, limits gomath.Limits) (Decimal, error) {
	if coefficient == nil {
		return Decimal{}, fmt.Errorf("%w: nil coefficient", gomath.ErrInvalidArgument)
	}
	if err := limits.Validate(); err != nil {
		return Decimal{}, err
	}
	if exponentMagnitude(exponent) > uint32(limits.MaxExponentMagnitude) {
		return Decimal{}, fmt.Errorf("%w: decimal exponent", gomath.ErrLimitExceeded)
	}
	if decimalDigits(coefficient) > limits.MaxInputDigits || coefficient.BitLen() > limits.MaxIntermediateBits {
		return Decimal{}, fmt.Errorf("%w: decimal coefficient", gomath.ErrLimitExceeded)
	}
	if outputDigits(coefficient, exponent) > limits.MaxOutputDigits {
		return Decimal{}, fmt.Errorf("%w: decimal output", gomath.ErrLimitExceeded)
	}

	return fromBig(coefficient, exponent), nil
}

// Parse parses a strict non-exponent decimal with default limits.
func Parse(input string) (Decimal, error) {
	return ParseWithOptions(input, ParseOptions{Limits: gomath.DefaultLimits()})
}

// ParseWithOptions parses a decimal according to an explicit grammar and limits.
func ParseWithOptions(input string, options ParseOptions) (Decimal, error) {
	if err := options.Limits.Validate(); err != nil {
		return Decimal{}, err
	}
	if input == "" || (!options.AllowWhitespace && strings.TrimSpace(input) != input) {
		return Decimal{}, ErrInvalid
	}
	if options.AllowWhitespace {
		input = strings.TrimSpace(input)
	}
	if input == "" {
		return Decimal{}, ErrInvalid
	}

	negative := false
	switch input[0] {
	case '-':
		negative = true
		input = input[1:]
	case '+':
		if !options.AllowPlus {
			return Decimal{}, ErrInvalid
		}
		input = input[1:]
	}
	if input == "" {
		return Decimal{}, ErrInvalid
	}

	mantissa, parsedExponent, err := splitExponent(input, options)
	if err != nil {
		return Decimal{}, err
	}
	parts := strings.Split(mantissa, ".")
	if len(parts) > 2 || parts[0] == "" || len(parts) == 2 && parts[1] == "" {
		return Decimal{}, ErrInvalid
	}
	integerDigits, integerCount, ok := cleanDigits(parts[0], options.AllowUnderscores)
	if !ok || !options.AllowLeadingZeros && integerCount > 1 && integerDigits[0] == '0' {
		return Decimal{}, ErrInvalid
	}
	fractionDigits := ""
	fractionCount := 0
	if len(parts) == 2 {
		fractionDigits, fractionCount, ok = cleanDigits(parts[1], options.AllowUnderscores)
		if !ok {
			return Decimal{}, ErrInvalid
		}
	}
	if integerCount+fractionCount > options.Limits.MaxInputDigits {
		return Decimal{}, fmt.Errorf("%w: decimal input digits", ErrLimit)
	}
	exponent64 := int64(parsedExponent) - int64(fractionCount)
	if exponent64 < -int64(options.Limits.MaxExponentMagnitude) || exponent64 > int64(options.Limits.MaxExponentMagnitude) {
		return Decimal{}, fmt.Errorf("%w: decimal exponent", ErrLimit)
	}
	var coefficient big.Int
	coefficient.SetString(integerDigits+fractionDigits, 10)
	if negative {
		coefficient.Neg(&coefficient)
	}

	return FromBig(&coefficient, int32(exponent64), options.Limits)
}

// MustParse parses a trusted constant and panics on invalid input.
func MustParse(input string) Decimal {
	value, err := Parse(input)
	if err != nil {
		panic(err)
	}

	return value
}

// Coefficient returns a mutable copy of the coefficient.
func (d Decimal) Coefficient() *big.Int { return new(big.Int).Set(&d.coefficient) }

// Exponent returns the base-10 exponent.
func (d Decimal) Exponent() int32 { return d.exponent }

// Scale returns the number of fractional places, or a negative value when the
// representation has a positive exponent.
func (d Decimal) Scale() int32 { return -d.exponent }

// String returns the exact non-exponent representation.
func (d Decimal) String() string {
	negative := d.coefficient.Sign() < 0
	digits := new(big.Int).Abs(&d.coefficient).String()
	if d.exponent >= 0 {
		digits += strings.Repeat("0", int(d.exponent))
	} else if int(-d.exponent) >= len(digits) {
		digits = "0." + strings.Repeat("0", int(-d.exponent)-len(digits)) + digits
	} else {
		point := len(digits) + int(d.exponent)
		digits = digits[:point] + "." + digits[point:]
	}
	if negative {
		return "-" + digits
	}

	return digits
}

// BigRat returns the exact value as a mutable rational copy.
func (d Decimal) BigRat() *big.Rat {
	if d.exponent >= 0 {
		return new(big.Rat).SetInt(new(big.Int).Mul(&d.coefficient, pow10(uint32(d.exponent))))
	}

	return new(big.Rat).SetFrac(&d.coefficient, pow10(uint32(-d.exponent)))
}

// Sign returns -1, 0, or +1.
func (d Decimal) Sign() int { return d.coefficient.Sign() }

// IsZero reports numeric zero.
func (d Decimal) IsZero() bool { return d.Sign() == 0 }

// Neg returns -d while retaining its exponent.
func (d Decimal) Neg() Decimal { return fromBig(new(big.Int).Neg(&d.coefficient), d.exponent) }

// Abs returns |d| while retaining its exponent.
func (d Decimal) Abs() Decimal { return fromBig(new(big.Int).Abs(&d.coefficient), d.exponent) }

// Cmp compares decimals numerically without converting through binary floating point.
func (d Decimal) Cmp(other Decimal) int {
	if d.Sign() != other.Sign() {
		return compareInts(d.Sign(), other.Sign())
	}
	if d.Sign() == 0 {
		return 0
	}
	dAdjusted := int64(decimalDigits(&d.coefficient)) + int64(d.exponent)
	oAdjusted := int64(decimalDigits(&other.coefficient)) + int64(other.exponent)
	if dAdjusted != oAdjusted {
		comparison := compareInt64(dAdjusted, oAdjusted)
		if d.Sign() < 0 {
			return -comparison
		}
		return comparison
	}
	left, right, _ := align(d, other)

	return left.Cmp(right)
}

// Equal reports numeric equality.
func (d Decimal) Equal(other Decimal) bool { return d.Cmp(other) == 0 }

// SameRepresentation reports equal coefficients and exponents.
func (d Decimal) SameRepresentation(other Decimal) bool {
	return d.exponent == other.exponent && d.coefficient.Cmp(&other.coefficient) == 0
}

// AddExact returns the exact sum under explicit resource bounds.
func (d Decimal) AddExact(ctx context.Context, other Decimal, limits gomath.Limits) (Decimal, error) {
	return exactAdd(ctx, d, other, limits, false)
}

// SubExact returns the exact difference under explicit resource bounds.
func (d Decimal) SubExact(ctx context.Context, other Decimal, limits gomath.Limits) (Decimal, error) {
	return exactAdd(ctx, d, other, limits, true)
}

// MulExact returns the exact product under explicit resource bounds.
func (d Decimal) MulExact(ctx context.Context, other Decimal, limits gomath.Limits) (Decimal, error) {
	if err := validateWork(ctx, limits); err != nil {
		return Decimal{}, err
	}
	if err := checkDecimalOperands(limits, d, other); err != nil {
		return Decimal{}, err
	}
	exponent := int64(d.exponent) + int64(other.exponent)
	if exponent < -int64(limits.MaxExponentMagnitude) || exponent > int64(limits.MaxExponentMagnitude) {
		return Decimal{}, fmt.Errorf("%w: product exponent", ErrLimit)
	}
	if d.coefficient.BitLen()+other.coefficient.BitLen() > limits.MaxIntermediateBits+1 {
		return Decimal{}, fmt.Errorf("%w: product coefficient", ErrLimit)
	}

	return FromBig(new(big.Int).Mul(&d.coefficient, &other.coefficient), int32(exponent), limits)
}

// QuoExact returns an exact terminating base-10 quotient.
func (d Decimal) QuoExact(ctx context.Context, other Decimal, limits gomath.Limits) (Decimal, error) {
	if err := validateWork(ctx, limits); err != nil {
		return Decimal{}, err
	}
	if err := checkDecimalOperands(limits, d, other); err != nil {
		return Decimal{}, err
	}
	if other.IsZero() {
		return Decimal{}, ErrDivisionByZero
	}
	numerator := new(big.Int).Set(&d.coefficient)
	denominator := new(big.Int).Set(&other.coefficient)
	if denominator.Sign() < 0 {
		numerator.Neg(numerator)
		denominator.Neg(denominator)
	}
	gcd := new(big.Int).GCD(nil, nil, new(big.Int).Abs(numerator), denominator)
	numerator.Quo(numerator, gcd)
	denominator.Quo(denominator, gcd)
	twos := removeFactor(denominator, 2)
	fives := removeFactor(denominator, 5)
	if denominator.Cmp(big.NewInt(1)) != 0 {
		return Decimal{}, ErrNonTerminating
	}
	scale := max(twos, fives)
	if twos < scale {
		scaled, err := multiplyPowerLimited(numerator, 2, scale-twos, limits)
		if err != nil {
			return Decimal{}, err
		}
		numerator = scaled
	}
	if fives < scale {
		scaled, err := multiplyPowerLimited(numerator, 5, scale-fives, limits)
		if err != nil {
			return Decimal{}, err
		}
		numerator = scaled
	}
	exponent := int64(d.exponent) - int64(other.exponent) - int64(scale)
	if exponent < -int64(limits.MaxExponentMagnitude) || exponent > int64(limits.MaxExponentMagnitude) {
		return Decimal{}, fmt.Errorf("%w: quotient exponent", ErrLimit)
	}

	return FromBig(numerator, int32(exponent), limits)
}

// Add performs context-rounded addition.
func (c Context) Add(ctx context.Context, left, right Decimal) (Result, error) {
	limits, err := c.validate(ctx)
	if err != nil {
		return Result{}, err
	}
	exact, err := left.AddExact(ctx, right, limits)
	if err != nil {
		return Result{}, err
	}
	return c.apply(exact, limits)
}

// Sub performs context-rounded subtraction.
func (c Context) Sub(ctx context.Context, left, right Decimal) (Result, error) {
	limits, err := c.validate(ctx)
	if err != nil {
		return Result{}, err
	}
	exact, err := left.SubExact(ctx, right, limits)
	if err != nil {
		return Result{}, err
	}

	return c.apply(exact, limits)
}

// Mul performs context-rounded multiplication.
func (c Context) Mul(ctx context.Context, left, right Decimal) (Result, error) {
	limits, err := c.validate(ctx)
	if err != nil {
		return Result{}, err
	}
	exact, err := left.MulExact(ctx, right, limits)
	if err != nil {
		return Result{}, err
	}

	return c.apply(exact, limits)
}

// Quo performs context-rounded division.
func (c Context) Quo(ctx context.Context, numerator, denominator Decimal) (Result, error) {
	limits, err := c.validate(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := checkDecimalOperands(limits, numerator, denominator); err != nil {
		return Result{}, err
	}
	if denominator.IsZero() {
		result := Result{Conditions: gomath.ConditionDivisionByZero}

		return result, ErrDivisionByZero
	}
	if numerator.IsZero() {
		return c.apply(Decimal{}, limits)
	}
	result, conditions, err := divide(
		numerator, denominator, c.Precision, c.Rounding, limits,
		decimalDigits(&numerator.coefficient) > int(c.Precision) ||
			decimalDigits(&denominator.coefficient) > int(c.Precision),
	)
	if err != nil {
		return Result{}, err
	}

	return c.finish(result, conditions, limits)
}

// Apply rounds value to the context without another arithmetic operation.
func (c Context) Apply(ctx context.Context, value Decimal) (Result, error) {
	limits, err := c.validate(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := checkDecimalOperands(limits, value); err != nil {
		return Result{}, err
	}

	return c.apply(value, limits)
}

// Quantize rounds or pads d to an explicit number of fractional decimal
// places. A negative scale rounds to positions left of the decimal point.
func (d Decimal) Quantize(
	ctx context.Context,
	scale int32,
	mode RoundingMode,
	limits gomath.Limits,
) (Result, error) {
	if err := validateWork(ctx, limits); err != nil {
		return Result{}, err
	}
	if err := checkDecimalOperands(limits, d); err != nil {
		return Result{}, err
	}
	if !mode.Valid() {
		return Result{}, fmt.Errorf("%w: rounding mode", gomath.ErrInvalidArgument)
	}
	targetExponent64 := -int64(scale)
	if targetExponent64 < -int64(limits.MaxExponentMagnitude) ||
		targetExponent64 > int64(limits.MaxExponentMagnitude) {
		return Result{}, fmt.Errorf("%w: quantize exponent", ErrLimit)
	}
	targetExponent := int32(targetExponent64)
	if d.IsZero() {
		value, err := FromBig(new(big.Int), targetExponent, limits)

		return Result{Value: value}, err
	}
	if d.exponent == targetExponent {
		return Result{Value: d}, nil
	}
	if d.exponent > targetExponent {
		shift := uint32(d.exponent - targetExponent)
		if shift > uint32(limits.MaxExponentMagnitude) {
			return Result{}, fmt.Errorf("%w: quantize padding", ErrLimit)
		}
		coefficient, err := scaleCoefficient(&d.coefficient, shift, limits)
		if err != nil {
			return Result{}, err
		}
		value, err := FromBig(coefficient, targetExponent, limits)

		return Result{Value: value}, err
	}

	drop := uint32(targetExponent - d.exponent)
	if drop > uint32(limits.MaxExponentMagnitude) {
		return Result{}, fmt.Errorf("%w: quantize rounding", ErrLimit)
	}
	coefficient, conditions := roundCoefficient(&d.coefficient, drop, mode)
	value, err := FromBig(coefficient, targetExponent, limits)
	if err != nil {
		return Result{}, err
	}

	return Result{Value: value, Conditions: conditions}, nil
}

// QuantizedQuo divides numerator by denominator and rounds exactly once to an
// explicit fractional scale.
func QuantizedQuo(
	ctx context.Context,
	numerator Decimal,
	denominator Decimal,
	scale int32,
	mode RoundingMode,
	limits gomath.Limits,
) (Result, error) {
	if err := validateWork(ctx, limits); err != nil {
		return Result{}, err
	}
	if err := checkDecimalOperands(limits, numerator, denominator); err != nil {
		return Result{}, err
	}
	if !mode.Valid() {
		return Result{}, fmt.Errorf("%w: rounding mode", gomath.ErrInvalidArgument)
	}
	if denominator.IsZero() {
		return Result{Conditions: gomath.ConditionDivisionByZero}, ErrDivisionByZero
	}
	targetExponent64 := -int64(scale)
	if targetExponent64 < -int64(limits.MaxExponentMagnitude) ||
		targetExponent64 > int64(limits.MaxExponentMagnitude) {
		return Result{}, fmt.Errorf("%w: quotient exponent", ErrLimit)
	}
	if numerator.IsZero() {
		value, err := FromBig(new(big.Int), int32(targetExponent64), limits)

		return Result{Value: value}, err
	}

	n := new(big.Int).Abs(&numerator.coefficient)
	d := new(big.Int).Abs(&denominator.coefficient)
	shift := int64(numerator.exponent) - int64(denominator.exponent) - targetExponent64
	if shift > int64(limits.MaxExponentMagnitude) || shift < -int64(limits.MaxExponentMagnitude) {
		return Result{}, fmt.Errorf("%w: quotient scale", ErrLimit)
	}
	var err error
	if shift >= 0 {
		n, err = scaleCoefficient(n, uint32(shift), limits)
	} else {
		d, err = scaleCoefficient(d, uint32(-shift), limits)
	}
	if err != nil {
		return Result{}, err
	}

	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(n, d, remainder)
	conditions := gomath.Condition(0)
	sign := numerator.Sign() * denominator.Sign()
	if remainder.Sign() != 0 {
		conditions = gomath.ConditionRounded | gomath.ConditionInexact
		if shouldIncrement(quotient, remainder, d, sign, mode) {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	if sign < 0 {
		quotient.Neg(quotient)
	}
	value, err := FromBig(quotient, int32(targetExponent64), limits)
	if err != nil {
		return Result{}, err
	}

	return Result{Value: value, Conditions: conditions}, nil
}

// Clamp restricts d to the inclusive numeric interval.
func (d Decimal) Clamp(minimum, maximum Decimal) (Decimal, error) {
	if minimum.Cmp(maximum) > 0 {
		return Decimal{}, fmt.Errorf("%w: minimum exceeds maximum", ErrInvalid)
	}
	if d.Cmp(minimum) < 0 {
		return minimum, nil
	}
	if d.Cmp(maximum) > 0 {
		return maximum, nil
	}

	return d, nil
}

// MarshalText returns a canonical exact numeric representation.
func (d Decimal) MarshalText() ([]byte, error) { return []byte(d.canonicalText()), nil }

// UnmarshalText replaces d with a strict parsed value.
func (d *Decimal) UnmarshalText(text []byte) error {
	if d == nil {
		return fmt.Errorf("%w: nil Decimal receiver", gomath.ErrInvalidArgument)
	}
	value, err := Parse(string(text))
	if err != nil {
		return err
	}
	*d = value

	return nil
}

// MarshalJSON encodes d as a JSON string to prevent precision loss.
func (d Decimal) MarshalJSON() ([]byte, error) { return json.Marshal(d.canonicalText()) }

// UnmarshalJSON accepts only a JSON string containing a strict decimal.
func (d *Decimal) UnmarshalJSON(data []byte) error {
	if d == nil {
		return fmt.Errorf("%w: nil Decimal receiver", gomath.ErrInvalidArgument)
	}
	if len(data) == 0 || data[0] != '"' {
		return ErrInvalid
	}
	var text string
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&text); err != nil {
		return fmt.Errorf("%w: malformed JSON string", ErrInvalid)
	}
	if decoder.More() {
		return ErrInvalid
	}

	return d.UnmarshalText([]byte(text))
}

func (d Decimal) canonicalText() string {
	if d.IsZero() && d.exponent > 0 {
		return "0"
	}

	return d.String()
}

func (c Context) validate(ctx context.Context) (gomath.Limits, error) {
	if ctx == nil {
		return gomath.Limits{}, fmt.Errorf("%w: nil context", gomath.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return gomath.Limits{}, err
	}
	limits := c.Limits
	if limits == (gomath.Limits{}) {
		limits = gomath.DefaultLimits()
	}
	if err := limits.Validate(); err != nil {
		return gomath.Limits{}, err
	}
	if c.Precision == 0 || c.Precision > limits.MaxPrecision {
		return gomath.Limits{}, fmt.Errorf("%w: decimal precision", gomath.ErrInvalidArgument)
	}
	if uint64(c.Precision) > uint64(limits.MaxIntermediateBits) {
		return gomath.Limits{}, fmt.Errorf("%w: decimal precision", ErrLimit)
	}
	if !c.Rounding.Valid() || c.MinExponent > c.MaxExponent ||
		exponentMagnitude(c.MinExponent) > uint32(limits.MaxExponentMagnitude) ||
		exponentMagnitude(c.MaxExponent) > uint32(limits.MaxExponentMagnitude) {
		return gomath.Limits{}, fmt.Errorf("%w: decimal context", gomath.ErrInvalidArgument)
	}

	return limits, nil
}

func (c Context) apply(value Decimal, limits gomath.Limits) (Result, error) {
	conditions := gomath.Condition(0)
	digits := decimalDigits(&value.coefficient)
	if value.Sign() != 0 && digits > int(c.Precision) {
		drop := uint32(digits - int(c.Precision))
		coefficient, rounded := roundCoefficient(&value.coefficient, drop, c.Rounding)
		conditions |= rounded
		value = fromBig(coefficient, value.exponent+int32(drop))
		if decimalDigits(&value.coefficient) > int(c.Precision) {
			value = fromBig(new(big.Int).Quo(&value.coefficient, big.NewInt(10)), value.exponent+1)
		}
	}

	return c.finish(value, conditions, limits)
}

func (c Context) finish(value Decimal, conditions gomath.Condition, limits gomath.Limits) (Result, error) {
	if value.Sign() != 0 {
		adjusted := int64(value.exponent) + int64(decimalDigits(&value.coefficient)) - 1
		if adjusted > int64(c.MaxExponent) {
			conditions |= gomath.ConditionOverflow | gomath.ConditionRounded | gomath.ConditionInexact
			coefficient := new(big.Int).Sub(pow10(c.Precision), big.NewInt(1))
			if value.Sign() < 0 {
				coefficient.Neg(coefficient)
			}
			value = fromBig(coefficient, c.MaxExponent-int32(c.Precision)+1)
		} else if adjusted < int64(c.MinExponent) {
			conditions |= gomath.ConditionSubnormal
			minimumExponent := c.MinExponent - int32(c.Precision) + 1
			if value.exponent < minimumExponent {
				drop := uint32(minimumExponent - value.exponent)
				coefficient, rounded := roundCoefficient(&value.coefficient, drop, c.Rounding)
				conditions |= rounded
				if rounded.Has(gomath.ConditionInexact) {
					conditions |= gomath.ConditionUnderflow
				}
				value = fromBig(coefficient, minimumExponent)
			}
		}
	}
	if outputDigits(&value.coefficient, value.exponent) > limits.MaxOutputDigits {
		return Result{}, fmt.Errorf("%w: decimal output", ErrLimit)
	}
	result := Result{Value: value, Conditions: conditions}
	if trapped := conditions & c.Traps; trapped != 0 {
		return result, fmt.Errorf("%w: %s", gomath.ErrTrappedCondition, trapped)
	}

	return result, nil
}

func exactAdd(ctx context.Context, left, right Decimal, limits gomath.Limits, subtract bool) (Decimal, error) {
	if err := validateWork(ctx, limits); err != nil {
		return Decimal{}, err
	}
	if err := checkDecimalOperands(limits, left, right); err != nil {
		return Decimal{}, err
	}
	difference := exponentDifference(left.exponent, right.exponent)
	if difference > uint32(limits.MaxExponentMagnitude) {
		return Decimal{}, fmt.Errorf("%w: exponent alignment", ErrLimit)
	}
	exponent := min(left.exponent, right.exponent)
	l, err := scaleCoefficient(&left.coefficient, uint32(left.exponent-exponent), limits)
	if err != nil {
		return Decimal{}, fmt.Errorf("%w: aligned coefficient", err)
	}
	r, err := scaleCoefficient(&right.coefficient, uint32(right.exponent-exponent), limits)
	if err != nil {
		return Decimal{}, fmt.Errorf("%w: aligned coefficient", err)
	}
	result := new(big.Int)
	if subtract {
		result.Sub(l, r)
	} else {
		result.Add(l, r)
	}

	return FromBig(result, exponent, limits)
}

func checkDecimalOperands(limits gomath.Limits, values ...Decimal) error {
	for _, value := range values {
		if value.coefficient.BitLen() > limits.MaxIntermediateBits ||
			exponentMagnitude(value.exponent) > uint32(limits.MaxExponentMagnitude) {
			return fmt.Errorf("%w: decimal operand", ErrLimit)
		}
	}

	return nil
}

func divide(
	numerator Decimal,
	denominator Decimal,
	precision uint32,
	mode RoundingMode,
	limits gomath.Limits,
	preservePrecision bool,
) (Decimal, gomath.Condition, error) {
	n := new(big.Int).Abs(&numerator.coefficient)
	d := new(big.Int).Abs(&denominator.coefficient)
	exponentShift := int64(numerator.exponent) - int64(denominator.exponent)
	adjusted := int64(decimalDigits(n)) - int64(decimalDigits(d)) + exponentShift
	if compareRatioPower10(n, d, adjusted-exponentShift) < 0 {
		adjusted--
	}
	resultExponent := adjusted - int64(precision) + 1
	scale := exponentShift - resultExponent
	if scale >= 0 {
		if scale > int64(limits.MaxExponentMagnitude) {
			return Decimal{}, 0, fmt.Errorf("%w: division scale", ErrLimit)
		}
		var err error
		n, err = scaleCoefficient(n, uint32(scale), limits)
		if err != nil {
			return Decimal{}, 0, err
		}
	} else {
		if -scale > int64(limits.MaxExponentMagnitude) {
			return Decimal{}, 0, fmt.Errorf("%w: division scale", ErrLimit)
		}
		var err error
		d, err = scaleCoefficient(d, uint32(-scale), limits)
		if err != nil {
			return Decimal{}, 0, err
		}
	}
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(n, d, remainder)
	conditions := gomath.Condition(0)
	if remainder.Sign() != 0 {
		conditions = gomath.ConditionRounded | gomath.ConditionInexact
		if shouldIncrement(quotient, remainder, d, numerator.Sign()*denominator.Sign(), mode) {
			quotient.Add(quotient, big.NewInt(1))
		}
		if decimalDigits(quotient) > int(precision) {
			quotient.Quo(quotient, big.NewInt(10))
			resultExponent++
		}
	} else if !preservePrecision {
		for quotient.Sign() != 0 && new(big.Int).Mod(quotient, big.NewInt(10)).Sign() == 0 {
			quotient.Quo(quotient, big.NewInt(10))
			resultExponent++
		}
	} else {
		conditions |= gomath.ConditionRounded
	}
	if numerator.Sign()*denominator.Sign() < 0 {
		quotient.Neg(quotient)
	}
	return fromBig(quotient, int32(resultExponent)), conditions, nil
}

func scaleCoefficient(coefficient *big.Int, shift uint32, limits gomath.Limits) (*big.Int, error) {
	if coefficient.Sign() == 0 || shift == 0 {
		return new(big.Int).Set(coefficient), nil
	}
	growth := uint64(shift) * 3_321_928_094 / 1_000_000_000
	if uint64(coefficient.BitLen())+growth > uint64(limits.MaxIntermediateBits) {
		return nil, fmt.Errorf("%w: scaled coefficient", ErrLimit)
	}
	result := new(big.Int).Mul(coefficient, pow10(shift))
	if result.BitLen() > limits.MaxIntermediateBits {
		return nil, fmt.Errorf("%w: scaled coefficient", ErrLimit)
	}

	return result, nil
}

func multiplyPowerLimited(coefficient *big.Int, base int64, exponent uint32, limits gomath.Limits) (*big.Int, error) {
	growthPerBillion := uint64(1_000_000_000)
	if base == 5 {
		growthPerBillion = 2_321_928_094
	}
	growth := uint64(exponent) * growthPerBillion / 1_000_000_000
	if uint64(coefficient.BitLen())+growth > uint64(limits.MaxIntermediateBits) {
		return nil, fmt.Errorf("%w: quotient coefficient", ErrLimit)
	}
	power := new(big.Int).Exp(big.NewInt(base), new(big.Int).SetUint64(uint64(exponent)), nil)
	result := new(big.Int).Mul(coefficient, power)
	if result.BitLen() > limits.MaxIntermediateBits {
		return nil, fmt.Errorf("%w: quotient coefficient", ErrLimit)
	}

	return result, nil
}

func roundCoefficient(coefficient *big.Int, drop uint32, mode RoundingMode) (*big.Int, gomath.Condition) {
	divisor := pow10(drop)
	abs := new(big.Int).Abs(coefficient)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(abs, divisor, remainder)
	conditions := gomath.ConditionRounded
	if remainder.Sign() != 0 {
		conditions |= gomath.ConditionInexact
		if shouldIncrement(quotient, remainder, divisor, coefficient.Sign(), mode) {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	if coefficient.Sign() < 0 {
		quotient.Neg(quotient)
	}

	return quotient, conditions
}

func shouldIncrement(quotient, remainder, divisor *big.Int, sign int, mode RoundingMode) bool {
	switch mode {
	case Down:
		return false
	case Up:
		return true
	case Ceiling:
		return sign > 0
	case Floor:
		return sign < 0
	}
	comparison := new(big.Int).Lsh(new(big.Int).Set(remainder), 1).Cmp(divisor)
	if comparison > 0 {
		return true
	}
	if comparison < 0 {
		return false
	}
	if mode == HalfUp {
		return true
	}
	if mode == HalfDown {
		return false
	}

	return quotient.Bit(0) == 1
}

func validateWork(ctx context.Context, limits gomath.Limits) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", gomath.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	return limits.Validate()
}

func fromBig(coefficient *big.Int, exponent int32) Decimal {
	var result Decimal
	result.coefficient.Set(coefficient)
	result.exponent = exponent

	return result
}

func align(left, right Decimal) (*big.Int, *big.Int, int32) {
	exponent := min(left.exponent, right.exponent)
	l := new(big.Int).Mul(&left.coefficient, pow10(uint32(left.exponent-exponent)))
	r := new(big.Int).Mul(&right.coefficient, pow10(uint32(right.exponent-exponent)))

	return l, r, exponent
}

func pow10(exponent uint32) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), new(big.Int).SetUint64(uint64(exponent)), nil)
}

func removeFactor(value *big.Int, factor int64) uint32 {
	count := uint32(0)
	divisor := big.NewInt(factor)
	for {
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(value, divisor, remainder)
		if remainder.Sign() != 0 {
			return count
		}
		value.Set(quotient)
		count++
	}
}

func splitExponent(input string, options ParseOptions) (string, int32, error) {
	index := strings.IndexAny(input, "eE")
	if index < 0 {
		return input, 0, nil
	}
	if !options.AllowExponent || strings.ContainsAny(input[index+1:], "eE") {
		return "", 0, ErrInvalid
	}
	mantissa, exponentText := input[:index], input[index+1:]
	if mantissa == "" || exponentText == "" {
		return "", 0, ErrInvalid
	}
	exponent, err := strconv.ParseInt(exponentText, 10, 32)
	if err != nil {
		if errorsIsRange(err) {
			return "", 0, fmt.Errorf("%w: decimal exponent", ErrLimit)
		}
		return "", 0, ErrInvalid
	}
	if exponent < -int64(options.Limits.MaxExponentMagnitude) || exponent > int64(options.Limits.MaxExponentMagnitude) {
		return "", 0, fmt.Errorf("%w: decimal exponent", ErrLimit)
	}

	return mantissa, int32(exponent), nil
}

func cleanDigits(input string, allowUnderscores bool) (string, int, bool) {
	var builder strings.Builder
	previousUnderscore := false
	for index := 0; index < len(input); index++ {
		character := input[index]
		if character == '_' {
			if !allowUnderscores || index == 0 || index == len(input)-1 || previousUnderscore {
				return "", 0, false
			}
			previousUnderscore = true
			continue
		}
		if character < '0' || character > '9' {
			return "", 0, false
		}
		builder.WriteByte(character)
		previousUnderscore = false
	}

	return builder.String(), builder.Len(), builder.Len() > 0
}

func errorsIsRange(err error) bool {
	var numberError *strconv.NumError

	return errors.As(err, &numberError) && numberError.Err == strconv.ErrRange
}

func decimalDigits(value *big.Int) int { return len(new(big.Int).Abs(value).String()) }

func outputDigits(coefficient *big.Int, exponent int32) int {
	if exponent > 0 {
		return decimalDigits(coefficient) + int(exponent)
	}
	if exponent < 0 && int64(-exponent) >= int64(decimalDigits(coefficient)) {
		return int(-exponent) + 1
	}

	return decimalDigits(coefficient)
}

func exponentMagnitude(exponent int32) uint32 {
	if exponent >= 0 {
		return uint32(exponent)
	}

	return uint32(-(int64(exponent)))
}

func exponentDifference(left, right int32) uint32 {
	difference := int64(left) - int64(right)
	if difference < 0 {
		difference = -difference
	}

	return uint32(difference)
}

func compareRatioPower10(numerator, denominator *big.Int, exponent int64) int {
	if exponent >= 0 {
		return numerator.Cmp(new(big.Int).Mul(denominator, pow10(uint32(exponent))))
	}

	return new(big.Int).Mul(numerator, pow10(uint32(-exponent))).Cmp(denominator)
}

func compareInts(left, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}

	return 0
}

func compareInt64(left, right int64) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}

	return 0
}
