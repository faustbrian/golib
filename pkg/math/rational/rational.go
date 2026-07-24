// Package rational provides immutable exact fractions.
package rational

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	gomath "github.com/faustbrian/golib/pkg/math"
)

// Rational is an immutable normalized fraction. Its zero value represents zero.
type Rational struct{ value big.Rat }

// Zero returns the canonical rational zero.
func Zero() Rational { return Rational{} }

// New constructs a normalized fraction from machine integers.
func New(numerator, denominator int64) (Rational, error) {
	return NewChecked(big.NewInt(numerator), big.NewInt(denominator), gomath.DefaultLimits())
}

// NewChecked constructs a normalized fraction and rejects a zero denominator.
func NewChecked(numerator, denominator *big.Int, limits gomath.Limits) (Rational, error) {
	if numerator == nil || denominator == nil {
		return Rational{}, fmt.Errorf("%w: nil fraction component", gomath.ErrInvalidArgument)
	}
	if denominator.Sign() == 0 {
		return Rational{}, gomath.ErrDivisionByZero
	}
	if err := limits.Validate(); err != nil {
		return Rational{}, err
	}
	if numerator.BitLen() > limits.MaxIntermediateBits || denominator.BitLen() > limits.MaxIntermediateBits {
		return Rational{}, fmt.Errorf("%w: rational magnitude", gomath.ErrLimitExceeded)
	}

	return fromBig(new(big.Rat).SetFrac(numerator, denominator)), nil
}

// FromBig constructs a Rational by defensively copying value.
func FromBig(value *big.Rat, limits gomath.Limits) (Rational, error) {
	if value == nil {
		return Rational{}, fmt.Errorf("%w: nil big.Rat", gomath.ErrInvalidArgument)
	}

	return NewChecked(value.Num(), value.Denom(), limits)
}

// Parse parses a canonical integer or numerator/denominator fraction.
func Parse(text string, limits gomath.Limits) (Rational, error) {
	if err := limits.Validate(); err != nil {
		return Rational{}, err
	}
	if text == "" || strings.TrimSpace(text) != text || strings.Count(text, "/") > 1 {
		return Rational{}, gomath.ErrInvalidSyntax
	}
	parts := strings.Split(text, "/")
	if len(parts) == 1 {
		parts = append(parts, "1")
	}
	if !validInteger(parts[0], limits.MaxInputDigits) || !validInteger(parts[1], limits.MaxInputDigits) {
		return Rational{}, gomath.ErrInvalidSyntax
	}
	numerator, _ := new(big.Int).SetString(parts[0], 10)
	denominator, _ := new(big.Int).SetString(parts[1], 10)

	return NewChecked(numerator, denominator, limits)
}

// Big returns a mutable copy of the normalized fraction.
func (r Rational) Big() *big.Rat { return new(big.Rat).Set(&r.value) }

// Numerator returns a mutable copy of the normalized numerator.
func (r Rational) Numerator() *big.Int { return new(big.Int).Set(r.value.Num()) }

// Denominator returns a mutable copy of the positive normalized denominator.
func (r Rational) Denominator() *big.Int { return new(big.Int).Set(r.value.Denom()) }

// String returns the canonical normalized fraction.
func (r Rational) String() string { return r.value.RatString() }

// MarshalText returns the canonical normalized fraction.
func (r Rational) MarshalText() ([]byte, error) { return []byte(r.String()), nil }

// MarshalJSON encodes the rational as a JSON string.
func (r Rational) MarshalJSON() ([]byte, error) { return json.Marshal(r.String()) }

// Sign returns -1, 0, or +1.
func (r Rational) Sign() int { return r.value.Sign() }

// Cmp compares r and other numerically.
func (r Rational) Cmp(other Rational) int { return r.value.Cmp(&other.value) }

// Equal reports numeric equality.
func (r Rational) Equal(other Rational) bool { return r.Cmp(other) == 0 }

// Neg returns -r.
func (r Rational) Neg() Rational { return fromBig(new(big.Rat).Neg(&r.value)) }

// Abs returns the absolute value of r.
func (r Rational) Abs() Rational { return fromBig(new(big.Rat).Abs(&r.value)) }

// Add returns r + other.
func (r Rational) Add(ctx context.Context, other Rational, limits gomath.Limits) (Rational, error) {
	return binary(ctx, &r.value, &other.value, limits, (*big.Rat).Add)
}

// Sub returns r - other.
func (r Rational) Sub(ctx context.Context, other Rational, limits gomath.Limits) (Rational, error) {
	return binary(ctx, &r.value, &other.value, limits, (*big.Rat).Sub)
}

// Mul returns r * other.
func (r Rational) Mul(ctx context.Context, other Rational, limits gomath.Limits) (Rational, error) {
	return binary(ctx, &r.value, &other.value, limits, (*big.Rat).Mul)
}

// Quo returns r / other.
func (r Rational) Quo(ctx context.Context, other Rational, limits gomath.Limits) (Rational, error) {
	if err := validateContext(ctx, limits); err != nil {
		return Rational{}, err
	}
	if err := checkRationalOperands(limits, &r.value, &other.value); err != nil {
		return Rational{}, err
	}
	if other.Sign() == 0 {
		return Rational{}, gomath.ErrDivisionByZero
	}

	return checked(new(big.Rat).Quo(&r.value, &other.value), limits)
}

// Pow returns r raised to an integer exponent.
func (r Rational) Pow(ctx context.Context, exponent int64, limits gomath.Limits) (Rational, error) {
	if err := validateContext(ctx, limits); err != nil {
		return Rational{}, err
	}
	if err := checkRationalOperands(limits, &r.value); err != nil {
		return Rational{}, err
	}
	magnitude := exponentMagnitude(exponent)
	if magnitude > limits.MaxPowerExponent {
		return Rational{}, fmt.Errorf("%w: power exponent", gomath.ErrLimitExceeded)
	}
	if exponent < 0 && r.Sign() == 0 {
		return Rational{}, gomath.ErrDivisionByZero
	}
	if powerExceedsBits(r.value.Num(), magnitude, limits.MaxIntermediateBits) ||
		powerExceedsBits(r.value.Denom(), magnitude, limits.MaxIntermediateBits) {
		return Rational{}, fmt.Errorf("%w: rational power", gomath.ErrLimitExceeded)
	}
	numerator := new(big.Int).Exp(r.value.Num(), new(big.Int).SetUint64(magnitude), nil)
	denominator := new(big.Int).Exp(r.value.Denom(), new(big.Int).SetUint64(magnitude), nil)
	if exponent < 0 {
		numerator, denominator = denominator, numerator
	}

	return checked(new(big.Rat).SetFrac(numerator, denominator), limits)
}

func powerExceedsBits(value *big.Int, exponent uint64, maximum int) bool {
	bits := value.BitLen()
	if exponent == 0 || bits <= 1 {
		return false
	}

	return uint64(bits-1) > (uint64(maximum)-1)/exponent
}

// Decimal returns a fixed-scale base-10 expansion and any rounding conditions.
func (r Rational) Decimal(scale int, mode gomath.RoundingMode, limits gomath.Limits) (string, gomath.Condition, error) {
	if err := limits.Validate(); err != nil {
		return "", 0, err
	}
	if scale < 0 || scale > limits.MaxDecimalExpansion {
		return "", 0, fmt.Errorf("%w: decimal expansion", gomath.ErrLimitExceeded)
	}
	if !mode.Valid() {
		return "", 0, fmt.Errorf("%w: rounding mode", gomath.ErrInvalidArgument)
	}
	if r.value.Num().BitLen() > limits.MaxIntermediateBits ||
		r.value.Denom().BitLen() > limits.MaxIntermediateBits ||
		uint64(r.value.Num().BitLen())+uint64(scale)*3 > uint64(limits.MaxIntermediateBits) {
		return "", 0, fmt.Errorf("%w: decimal expansion intermediate", gomath.ErrLimitExceeded)
	}
	absNumerator := new(big.Int).Abs(r.value.Num())
	scaled := new(big.Int).Mul(absNumerator, pow10(scale))
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(scaled, r.value.Denom(), remainder)
	conditions := gomath.Condition(0)
	if remainder.Sign() != 0 {
		conditions = gomath.ConditionRounded | gomath.ConditionInexact
		if shouldIncrement(quotient, remainder, r.value.Denom(), r.Sign(), mode) {
			quotient.Add(quotient, big.NewInt(1))
		}
	}
	text := quotient.String()
	if scale > 0 {
		if len(text) <= scale {
			text = strings.Repeat("0", scale-len(text)+1) + text
		}
		text = text[:len(text)-scale] + "." + text[len(text)-scale:]
	}
	if r.Sign() < 0 && quotient.Sign() != 0 {
		text = "-" + text
	}
	if len(text) > limits.MaxOutputDigits+2 {
		return "", 0, fmt.Errorf("%w: decimal output", gomath.ErrLimitExceeded)
	}

	return text, conditions, nil
}

// Min returns the lesser operand.
func Min(left, right Rational) Rational {
	if left.Cmp(right) <= 0 {
		return left
	}

	return right
}

// Max returns the greater operand.
func Max(left, right Rational) Rational {
	if left.Cmp(right) >= 0 {
		return left
	}

	return right
}

// Clamp restricts value to the inclusive interval [minimum, maximum].
func Clamp(value, minimum, maximum Rational) (Rational, error) {
	if minimum.Cmp(maximum) > 0 {
		return Rational{}, fmt.Errorf("%w: inverted clamp interval", gomath.ErrInvalidArgument)
	}

	return Max(minimum, Min(value, maximum)), nil
}

func fromBig(value *big.Rat) Rational {
	var result Rational
	result.value.Set(value)

	return result
}

func binary(ctx context.Context, left, right *big.Rat, limits gomath.Limits, operation func(*big.Rat, *big.Rat, *big.Rat) *big.Rat) (Rational, error) {
	if err := validateContext(ctx, limits); err != nil {
		return Rational{}, err
	}
	if err := checkRationalOperands(limits, left, right); err != nil {
		return Rational{}, err
	}

	return checked(operation(new(big.Rat), left, right), limits)
}

func checkRationalOperands(limits gomath.Limits, values ...*big.Rat) error {
	for _, value := range values {
		if value.Num().BitLen() > limits.MaxIntermediateBits || value.Denom().BitLen() > limits.MaxIntermediateBits {
			return fmt.Errorf("%w: rational operand", gomath.ErrLimitExceeded)
		}
	}

	return nil
}

func checked(value *big.Rat, limits gomath.Limits) (Rational, error) {
	if value.Num().BitLen() > limits.MaxIntermediateBits || value.Denom().BitLen() > limits.MaxIntermediateBits {
		return Rational{}, fmt.Errorf("%w: rational result", gomath.ErrLimitExceeded)
	}

	return fromBig(value), nil
}

func validateContext(ctx context.Context, limits gomath.Limits) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", gomath.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	return limits.Validate()
}

func validInteger(text string, maximumDigits int) bool {
	if text == "" {
		return false
	}
	if text[0] == '-' {
		text = text[1:]
	}
	if text == "" || len(text) > maximumDigits || (len(text) > 1 && text[0] == '0') {
		return false
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return false
		}
	}

	return true
}

func exponentMagnitude(exponent int64) uint64 {
	if exponent >= 0 {
		return uint64(exponent)
	}

	return uint64(-(exponent + 1)) + 1
}

func pow10(exponent int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)), nil)
}

func shouldIncrement(quotient, remainder, denominator *big.Int, sign int, mode gomath.RoundingMode) bool {
	switch mode {
	case gomath.RoundDown:
		return false
	case gomath.RoundUp:
		return true
	case gomath.RoundCeiling:
		return sign > 0
	case gomath.RoundFloor:
		return sign < 0
	}
	comparison := new(big.Int).Lsh(new(big.Int).Set(remainder), 1).Cmp(denominator)
	if comparison > 0 {
		return true
	}
	if comparison < 0 {
		return false
	}
	if mode == gomath.RoundHalfUp {
		return true
	}
	if mode == gomath.RoundHalfDown {
		return false
	}

	return quotient.Bit(0) == 1
}
