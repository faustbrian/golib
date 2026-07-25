// Package bigfloat provides immutable arbitrary-precision binary floating-point
// values. It is intended for inexact algorithms, not money or exact quantities.
package bigfloat

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	gomath "github.com/faustbrian/golib/pkg/math"
)

// Context makes binary precision, rounding, traps, and resource limits explicit.
type Context struct {
	Precision uint
	Rounding  gomath.RoundingMode
	Traps     gomath.Condition
	Limits    gomath.Limits
}

// Result contains a rounded Float, its direction, and raised conditions.
type Result struct {
	Value      Float
	Accuracy   big.Accuracy
	Conditions gomath.Condition
}

// Float is an immutable arbitrary-precision binary floating-point value. The
// zero value is numeric zero with no operation precision; contexts remain
// mandatory at construction and operation boundaries.
type Float struct {
	value     big.Float
	precision uint
	rounding  gomath.RoundingMode
}

// NewInt64 constructs an exact integer-valued Float.
func NewInt64(value int64, operation Context) (Result, error) {
	mode, limits, err := operation.validate()
	if err != nil {
		return Result{}, err
	}
	result := new(big.Float).SetPrec(operation.Precision).SetMode(mode).SetInt64(value)

	return operation.finish(result, result.Acc(), limits)
}

// FromBig constructs a Float by defensively copying and rounding value to the
// explicit operation context.
func FromBig(value *big.Float, operation Context) (Result, error) {
	if value == nil {
		return Result{}, fmt.Errorf("%w: nil big.Float", gomath.ErrInvalidArgument)
	}
	mode, limits, err := operation.validate()
	if err != nil {
		return Result{}, err
	}
	if err := checkFloatInputLimits(value, limits); err != nil {
		return Result{}, err
	}
	result := new(big.Float).SetPrec(operation.Precision).SetMode(mode).Set(value)

	return operation.finish(result, result.Acc(), limits)
}

// FromRat constructs a Float from an exact rational and reports any rounding.
func FromRat(value *big.Rat, operation Context) (Result, error) {
	if value == nil {
		return Result{}, fmt.Errorf("%w: nil big.Rat", gomath.ErrInvalidArgument)
	}
	mode, limits, err := operation.validate()
	if err != nil {
		return Result{}, err
	}
	if value.Num().BitLen() > limits.MaxIntermediateBits || value.Denom().BitLen() > limits.MaxIntermediateBits {
		return Result{}, fmt.Errorf("%w: rational source bits", gomath.ErrLimitExceeded)
	}
	result := new(big.Float).SetPrec(operation.Precision).SetMode(mode).SetRat(value)

	return operation.finish(result, result.Acc(), limits)
}

// Parse parses a strict finite or infinite big.Float form in an explicit base.
func Parse(text string, base int, operation Context) (Result, error) {
	mode, limits, err := operation.validate()
	if err != nil {
		return Result{}, err
	}
	if text == "" || strings.TrimSpace(text) != text || len(text) > limits.MaxInputDigits {
		return Result{}, gomath.ErrInvalidSyntax
	}
	if base != 0 && base != 2 && base != 10 && base != 16 {
		return Result{}, fmt.Errorf("%w: float base", gomath.ErrInvalidArgument)
	}
	value, _, err := big.ParseFloat(text, base, operation.Precision, mode)
	if err != nil {
		return Result{}, fmt.Errorf("%w: binary float", gomath.ErrInvalidSyntax)
	}

	return operation.finish(value, value.Acc(), limits)
}

// Big returns a mutable copy of f.
func (f Float) Big() *big.Float { return new(big.Float).Copy(&f.value) }

// Precision returns the construction precision in bits.
func (f Float) Precision() uint { return f.precision }

// Rounding returns the construction rounding mode.
func (f Float) Rounding() gomath.RoundingMode { return f.rounding }

// String returns a canonical decimal representation that round-trips to the
// stored binary floating-point value at its precision.
func (f Float) String() string { return f.value.Text('g', roundTripDecimalDigits(f.precision)) }

// IsInf reports whether f is positive or negative infinity.
func (f Float) IsInf() bool { return f.value.IsInf() }

// Sign returns -1, 0, or +1.
func (f Float) Sign() int { return f.value.Sign() }

// Signbit reports whether f has a negative sign, including negative zero.
func (f Float) Signbit() bool { return f.value.Signbit() }

// Cmp compares f and other numerically.
func (f Float) Cmp(other Float) int { return f.value.Cmp(&other.value) }

// Equal reports numeric equality.
func (f Float) Equal(other Float) bool { return f.Cmp(other) == 0 }

// Neg returns -f while preserving representation policy.
func (f Float) Neg() Float {
	value := new(big.Float).Copy(&f.value)
	value.Neg(value)

	return fromBig(value, f.precision, f.rounding)
}

// Abs returns |f| while preserving representation policy.
func (f Float) Abs() Float {
	value := new(big.Float).Copy(&f.value)
	value.Abs(value)

	return fromBig(value, f.precision, f.rounding)
}

// Int returns the exact integral value or ErrConversion.
func (f Float) Int() (*big.Int, error) {
	if f.IsInf() {
		return nil, gomath.ErrConversion
	}
	value, accuracy := f.value.Int(nil)
	if accuracy != big.Exact {
		return nil, gomath.ErrConversion
	}

	return new(big.Int).Set(value), nil
}

// Rat returns the exact rational represented by a finite Float.
func (f Float) Rat() (*big.Rat, error) {
	if f.IsInf() {
		return nil, gomath.ErrConversion
	}
	value, _ := f.value.Rat(nil)

	return new(big.Rat).Set(value), nil
}

// MarshalText returns the canonical round-trip-safe decimal representation.
func (f Float) MarshalText() ([]byte, error) { return []byte(f.String()), nil }

// MarshalJSON encodes f as a string to avoid JSON-number narrowing.
func (f Float) MarshalJSON() ([]byte, error) { return json.Marshal(f.String()) }

// Add performs context-rounded addition.
func (c Context) Add(ctx context.Context, left, right Float) (Result, error) {
	return c.binary(ctx, left, right, (*big.Float).Add)
}

// Sub performs context-rounded subtraction.
func (c Context) Sub(ctx context.Context, left, right Float) (Result, error) {
	return c.binary(ctx, left, right, (*big.Float).Sub)
}

// Mul performs context-rounded multiplication.
func (c Context) Mul(ctx context.Context, left, right Float) (Result, error) {
	return c.binary(ctx, left, right, (*big.Float).Mul)
}

// Quo performs context-rounded division and reports zero divisors.
func (c Context) Quo(ctx context.Context, numerator, denominator Float) (Result, error) {
	mode, limits, err := c.validateContext(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := checkFloatInputLimits(&numerator.value, limits); err != nil {
		return Result{}, err
	}
	if err := checkFloatInputLimits(&denominator.value, limits); err != nil {
		return Result{}, err
	}
	if denominator.Sign() == 0 {
		if numerator.Sign() == 0 {
			result := Result{Conditions: gomath.ConditionInvalidOperation}

			return result, fmt.Errorf("%w: zero divided by zero", gomath.ErrDomain)
		}
		value := new(big.Float).SetPrec(c.Precision).SetMode(mode)
		value.SetInf(numerator.value.Signbit() != denominator.value.Signbit())
		result := Result{
			Value:      fromBig(value, c.Precision, c.Rounding),
			Accuracy:   big.Exact,
			Conditions: gomath.ConditionDivisionByZero,
		}
		if c.Traps.Has(gomath.ConditionDivisionByZero) {
			return result, fmt.Errorf("%w: division_by_zero", gomath.ErrTrappedCondition)
		}

		return result, nil
	}

	return c.binaryWithMode(numerator, denominator, mode, limits, (*big.Float).Quo)
}

// Sqrt performs a context-rounded square root.
func (c Context) Sqrt(ctx context.Context, value Float) (Result, error) {
	mode, limits, err := c.validateContext(ctx)
	if err != nil {
		return Result{}, err
	}
	if err := checkFloatInputLimits(&value.value, limits); err != nil {
		return Result{}, err
	}
	if value.Sign() < 0 {
		result := Result{Conditions: gomath.ConditionInvalidOperation}

		return result, fmt.Errorf("%w: square root of negative float", gomath.ErrDomain)
	}
	result := new(big.Float).SetPrec(c.Precision).SetMode(mode).Sqrt(&value.value)

	return c.finish(result, result.Acc(), limits)
}

func (c Context) binary(ctx context.Context, left, right Float, operation func(*big.Float, *big.Float, *big.Float) *big.Float) (Result, error) {
	mode, limits, err := c.validateContext(ctx)
	if err != nil {
		return Result{}, err
	}

	return c.binaryWithMode(left, right, mode, limits, operation)
}

func (c Context) binaryWithMode(left, right Float, mode big.RoundingMode, limits gomath.Limits, operation func(*big.Float, *big.Float, *big.Float) *big.Float) (Result, error) {
	if err := checkFloatInputLimits(&left.value, limits); err != nil {
		return Result{}, err
	}
	if err := checkFloatInputLimits(&right.value, limits); err != nil {
		return Result{}, err
	}
	result := new(big.Float).SetPrec(c.Precision).SetMode(mode)
	operation(result, &left.value, &right.value)

	return c.finish(result, result.Acc(), limits)
}

func (c Context) finish(value *big.Float, accuracy big.Accuracy, limits gomath.Limits) (Result, error) {
	if err := checkValueLimits(value, limits); err != nil {
		return Result{}, err
	}
	conditions := gomath.Condition(0)
	if accuracy != big.Exact {
		conditions = gomath.ConditionRounded | gomath.ConditionInexact
	}
	result := Result{
		Value:      fromBig(value, c.Precision, c.Rounding),
		Accuracy:   accuracy,
		Conditions: conditions,
	}
	if trapped := conditions & c.Traps; trapped != 0 {
		return result, fmt.Errorf("%w: %s", gomath.ErrTrappedCondition, trapped)
	}

	return result, nil
}

func (c Context) validate() (big.RoundingMode, gomath.Limits, error) {
	limits := c.Limits
	if limits == (gomath.Limits{}) {
		limits = gomath.DefaultLimits()
	}
	if err := limits.Validate(); err != nil {
		return 0, gomath.Limits{}, err
	}
	if c.Precision == 0 || c.Precision > uint(limits.MaxPrecision) {
		return 0, gomath.Limits{}, fmt.Errorf("%w: binary precision", gomath.ErrInvalidArgument)
	}
	if c.Precision > uint(limits.MaxIntermediateBits) {
		return 0, gomath.Limits{}, fmt.Errorf("%w: binary precision", gomath.ErrLimitExceeded)
	}
	mode, ok := roundingMode(c.Rounding)
	if !ok {
		return 0, gomath.Limits{}, fmt.Errorf("%w: binary rounding mode", gomath.ErrInvalidArgument)
	}

	return mode, limits, nil
}

func checkValueLimits(value *big.Float, limits gomath.Limits) error {
	if err := checkFloatInputLimits(value, limits); err != nil {
		return err
	}
	if value.IsInf() {
		return nil
	}
	exponent := value.MantExp(nil)
	if maximumRenderedDigits(value.Prec(), exponent) <= limits.MaxOutputDigits {
		return nil
	}
	if renderedDigits(value.Text('g', roundTripDecimalDigits(value.Prec()))) > limits.MaxOutputDigits {
		return fmt.Errorf("%w: binary output digits", gomath.ErrLimitExceeded)
	}

	return nil
}

func checkFloatInputLimits(value *big.Float, limits gomath.Limits) error {
	if value.Prec() > uint(limits.MaxIntermediateBits) {
		return fmt.Errorf("%w: binary value precision", gomath.ErrLimitExceeded)
	}
	if value.IsInf() {
		return nil
	}
	exponent := value.MantExp(nil)
	maximumExponent := int(limits.MaxExponentMagnitude)
	if exponent < -maximumExponent || exponent > maximumExponent {
		return fmt.Errorf("%w: binary exponent", gomath.ErrLimitExceeded)
	}

	return nil
}

func maximumRenderedDigits(precision uint, exponent int) int {
	return roundTripDecimalDigits(precision) + integerDigits(exponent)
}

func roundTripDecimalDigits(precision uint) int {
	return int((uint64(precision)*30_103+99_999)/100_000) + 1
}

func integerDigits(value int) int {
	magnitude := uint64(value)
	if value < 0 {
		magnitude = uint64(-(value + 1)) + 1
	}
	digits := 1
	for magnitude >= 10 {
		magnitude /= 10
		digits++
	}

	return digits
}

func renderedDigits(text string) int {
	digits := 0
	for _, character := range text {
		if character >= '0' && character <= '9' {
			digits++
		}
	}

	return digits
}

func (c Context) validateContext(ctx context.Context) (big.RoundingMode, gomath.Limits, error) {
	if ctx == nil {
		return 0, gomath.Limits{}, fmt.Errorf("%w: nil context", gomath.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return 0, gomath.Limits{}, err
	}

	return c.validate()
}

func roundingMode(mode gomath.RoundingMode) (big.RoundingMode, bool) {
	switch mode {
	case gomath.RoundHalfEven:
		return big.ToNearestEven, true
	case gomath.RoundHalfUp:
		return big.ToNearestAway, true
	case gomath.RoundDown:
		return big.ToZero, true
	case gomath.RoundUp:
		return big.AwayFromZero, true
	case gomath.RoundCeiling:
		return big.ToPositiveInf, true
	case gomath.RoundFloor:
		return big.ToNegativeInf, true
	default:
		return 0, false
	}
}

func fromBig(value *big.Float, precision uint, rounding gomath.RoundingMode) Float {
	var result Float
	result.value.Copy(value)
	result.precision = precision
	result.rounding = rounding

	return result
}
