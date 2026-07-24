// Package integer provides immutable arbitrary-precision signed integers.
package integer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strings"

	gomath "github.com/faustbrian/golib/pkg/math"
)

// Integer is an immutable arbitrary-precision signed integer. Its zero value
// represents zero.
type Integer struct{ n big.Int }

// ParseOptions controls the strict integer grammar. Base must be in [2, 36].
type ParseOptions struct {
	Base              int
	AllowUnderscores  bool
	AllowLeadingZeros bool
	AllowWhitespace   bool
	RejectSign        bool
	Limits            gomath.Limits
}

// Zero returns the canonical integer zero.
func Zero() Integer { return Integer{} }

// New constructs an Integer from a signed machine integer.
func New(value int64) Integer {
	var result Integer
	result.n.SetInt64(value)

	return result
}

// NewUnsigned constructs an Integer from an unsigned machine integer.
func NewUnsigned(value uint64) Integer {
	var result Integer
	result.n.SetUint64(value)

	return result
}

// FromBig constructs an Integer by defensively copying value.
func FromBig(value *big.Int, limits gomath.Limits) (Integer, error) {
	if value == nil {
		return Integer{}, fmt.Errorf("%w: nil big.Int", gomath.ErrInvalidArgument)
	}
	if err := validateLimits(limits); err != nil {
		return Integer{}, err
	}
	if err := checkBits(value.BitLen(), limits); err != nil {
		return Integer{}, err
	}

	return fromBig(value), nil
}

// Parse parses text without trimming it or inferring a base.
func Parse(text string, options ParseOptions) (Integer, error) {
	if err := validateLimits(options.Limits); err != nil {
		return Integer{}, err
	}
	if options.Base < 2 || options.Base > 36 {
		return Integer{}, fmt.Errorf("%w: base must be between 2 and 36", gomath.ErrInvalidArgument)
	}
	if text == "" || (!options.AllowWhitespace && strings.TrimSpace(text) != text) {
		return Integer{}, gomath.ErrInvalidSyntax
	}
	if options.AllowWhitespace {
		text = strings.TrimSpace(text)
	}
	if text == "" {
		return Integer{}, gomath.ErrInvalidSyntax
	}

	digits := text
	if digits[0] == '+' || digits[0] == '-' {
		if options.RejectSign || len(digits) == 1 {
			return Integer{}, gomath.ErrInvalidSyntax
		}
		digits = digits[1:]
	}
	clean, count, ok := validateDigits(digits, options.Base, options.AllowUnderscores)
	if !ok {
		return Integer{}, gomath.ErrInvalidSyntax
	}
	if count > options.Limits.MaxInputDigits {
		return Integer{}, fmt.Errorf("%w: input digits", gomath.ErrLimitExceeded)
	}
	if !options.AllowLeadingZeros && count > 1 && clean[0] == '0' {
		return Integer{}, gomath.ErrInvalidSyntax
	}
	if text[0] == '-' {
		clean = "-" + clean
	}
	value, _ := new(big.Int).SetString(clean, options.Base)
	if err := checkBits(value.BitLen(), options.Limits); err != nil {
		return Integer{}, err
	}

	return fromBig(value), nil
}

// Big returns a mutable copy of the value.
func (i Integer) Big() *big.Int { return new(big.Int).Set(&i.n) }

// String returns the canonical base-10 representation.
func (i Integer) String() string { return i.n.String() }

// MarshalText returns the canonical base-10 representation.
func (i Integer) MarshalText() ([]byte, error) { return []byte(i.String()), nil }

// MarshalJSON encodes the integer as a JSON string to prevent precision loss.
func (i Integer) MarshalJSON() ([]byte, error) { return json.Marshal(i.String()) }

// Sign returns -1, 0, or +1.
func (i Integer) Sign() int { return i.n.Sign() }

// Cmp compares i and other numerically.
func (i Integer) Cmp(other Integer) int { return i.n.Cmp(&other.n) }

// Equal reports numeric equality.
func (i Integer) Equal(other Integer) bool { return i.Cmp(other) == 0 }

// Neg returns -i.
func (i Integer) Neg() Integer { return fromBig(new(big.Int).Neg(&i.n)) }

// Abs returns the absolute value of i.
func (i Integer) Abs() Integer { return fromBig(new(big.Int).Abs(&i.n)) }

// Add returns i + other.
func (i Integer) Add(ctx context.Context, other Integer, limits gomath.Limits) (Integer, error) {
	return binary(ctx, &i.n, &other.n, limits, (*big.Int).Add)
}

// Sub returns i - other.
func (i Integer) Sub(ctx context.Context, other Integer, limits gomath.Limits) (Integer, error) {
	return binary(ctx, &i.n, &other.n, limits, (*big.Int).Sub)
}

// Mul returns i * other.
func (i Integer) Mul(ctx context.Context, other Integer, limits gomath.Limits) (Integer, error) {
	if err := validateContext(ctx, limits); err != nil {
		return Integer{}, err
	}
	if err := checkIntegerOperands(limits, &i.n, &other.n); err != nil {
		return Integer{}, err
	}
	if i.n.BitLen()+other.n.BitLen() > limits.MaxIntermediateBits+1 {
		return Integer{}, fmt.Errorf("%w: multiplication", gomath.ErrLimitExceeded)
	}

	return checked(new(big.Int).Mul(&i.n, &other.n), limits)
}

// Quo returns the quotient truncated toward zero under explicit resource
// bounds.
func (i Integer) Quo(ctx context.Context, divisor Integer, limits gomath.Limits) (Integer, error) {
	quotient, _, err := i.QuoRem(ctx, divisor, limits)

	return quotient, err
}

// QuoRem returns a quotient truncated toward zero and a remainder with i's sign
// under explicit resource bounds.
func (i Integer) QuoRem(ctx context.Context, divisor Integer, limits gomath.Limits) (Integer, Integer, error) {
	if err := validateContext(ctx, limits); err != nil {
		return Integer{}, Integer{}, err
	}
	if err := checkIntegerOperands(limits, &i.n, &divisor.n); err != nil {
		return Integer{}, Integer{}, err
	}
	if divisor.Sign() == 0 {
		return Integer{}, Integer{}, gomath.ErrDivisionByZero
	}
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(&i.n, &divisor.n, remainder)

	return fromBig(quotient), fromBig(remainder), nil
}

// Mod returns the Euclidean modulus in [0, modulus). Modulus must be positive
// and work is performed under explicit resource bounds.
func (i Integer) Mod(ctx context.Context, modulus Integer, limits gomath.Limits) (Integer, error) {
	if err := validateContext(ctx, limits); err != nil {
		return Integer{}, err
	}
	if err := checkIntegerOperands(limits, &i.n, &modulus.n); err != nil {
		return Integer{}, err
	}
	if modulus.Sign() <= 0 {
		if modulus.Sign() == 0 {
			return Integer{}, gomath.ErrDivisionByZero
		}
		return Integer{}, fmt.Errorf("%w: modulus must be positive", gomath.ErrDomain)
	}

	return fromBig(new(big.Int).Mod(&i.n, &modulus.n)), nil
}

// Pow returns i raised to a nonnegative exponent.
func (i Integer) Pow(ctx context.Context, exponent uint64, limits gomath.Limits) (Integer, error) {
	if err := validateContext(ctx, limits); err != nil {
		return Integer{}, err
	}
	if err := checkIntegerOperands(limits, &i.n); err != nil {
		return Integer{}, err
	}
	if exponent > limits.MaxPowerExponent {
		return Integer{}, fmt.Errorf("%w: power exponent", gomath.ErrLimitExceeded)
	}
	if exponent != 0 && uint64(i.n.BitLen()) > uint64(limits.MaxIntermediateBits)/exponent+1 {
		return Integer{}, fmt.Errorf("%w: power result", gomath.ErrLimitExceeded)
	}
	result := new(big.Int).Exp(&i.n, new(big.Int).SetUint64(exponent), nil)

	return checked(result, limits)
}

// Root returns the integer nth root truncated toward zero.
func (i Integer) Root(ctx context.Context, degree uint32, limits gomath.Limits) (Integer, error) {
	if err := validateContext(ctx, limits); err != nil {
		return Integer{}, err
	}
	if err := checkIntegerOperands(limits, &i.n); err != nil {
		return Integer{}, err
	}
	if degree == 0 {
		return Integer{}, fmt.Errorf("%w: root degree is zero", gomath.ErrDomain)
	}
	if degree > limits.MaxRootDegree {
		return Integer{}, fmt.Errorf("%w: root degree", gomath.ErrLimitExceeded)
	}
	negative := i.Sign() < 0
	if negative && degree%2 == 0 {
		return Integer{}, fmt.Errorf("%w: even root of a negative integer", gomath.ErrDomain)
	}
	abs := new(big.Int).Abs(&i.n)
	if degree == 1 {
		return i, nil
	}
	if degree == 2 {
		result := new(big.Int).Sqrt(abs)
		return fromBig(result), nil
	}
	root, err := nthRoot(ctx, abs, degree, limits)
	if err != nil {
		return Integer{}, err
	}
	if negative {
		root.Neg(root)
	}

	return fromBig(root), nil
}

// Min returns the lesser operand.
func Min(left, right Integer) Integer {
	if left.Cmp(right) <= 0 {
		return left
	}

	return right
}

// Max returns the greater operand.
func Max(left, right Integer) Integer {
	if left.Cmp(right) >= 0 {
		return left
	}

	return right
}

// Clamp restricts value to the inclusive interval [minimum, maximum].
func Clamp(value, minimum, maximum Integer) (Integer, error) {
	if minimum.Cmp(maximum) > 0 {
		return Integer{}, fmt.Errorf("%w: inverted clamp interval", gomath.ErrInvalidArgument)
	}

	return Max(minimum, Min(value, maximum)), nil
}

// GCD returns the nonnegative greatest common divisor.
func GCD(ctx context.Context, left, right Integer, limits gomath.Limits) (Integer, error) {
	if err := validateContext(ctx, limits); err != nil {
		return Integer{}, err
	}
	if err := checkIntegerOperands(limits, &left.n, &right.n); err != nil {
		return Integer{}, err
	}

	return checked(new(big.Int).GCD(nil, nil, &left.n, &right.n), limits)
}

// LCM returns the nonnegative least common multiple.
func LCM(ctx context.Context, left, right Integer, limits gomath.Limits) (Integer, error) {
	if err := validateContext(ctx, limits); err != nil {
		return Integer{}, err
	}
	if err := checkIntegerOperands(limits, &left.n, &right.n); err != nil {
		return Integer{}, err
	}
	if left.Sign() == 0 || right.Sign() == 0 {
		return Integer{}, nil
	}
	gcd := new(big.Int).GCD(nil, nil, &left.n, &right.n)
	result := new(big.Int).Quo(new(big.Int).Abs(&left.n), gcd)
	if result.BitLen()+right.n.BitLen() > limits.MaxIntermediateBits+1 {
		return Integer{}, fmt.Errorf("%w: least common multiple", gomath.ErrLimitExceeded)
	}
	result.Mul(result, new(big.Int).Abs(&right.n))

	return checked(result, limits)
}

// Random samples uniformly from [minimum, maximum) using only source.
func Random(ctx context.Context, source io.Reader, minimum, maximum Integer, limits gomath.Limits) (Integer, error) {
	if err := validateContext(ctx, limits); err != nil {
		return Integer{}, err
	}
	if err := checkIntegerOperands(limits, &minimum.n, &maximum.n); err != nil {
		return Integer{}, err
	}
	if source == nil {
		return Integer{}, fmt.Errorf("%w: nil random source", gomath.ErrInvalidArgument)
	}
	rangeSize := new(big.Int).Sub(&maximum.n, &minimum.n)
	if rangeSize.Sign() <= 0 {
		return Integer{}, fmt.Errorf("%w: empty random interval", gomath.ErrInvalidArgument)
	}
	if rangeSize.BitLen() > limits.MaxRandomBits {
		return Integer{}, fmt.Errorf("%w: random range", gomath.ErrLimitExceeded)
	}
	byteCount := (rangeSize.BitLen() + 7) / 8
	space := new(big.Int).Lsh(big.NewInt(1), uint(byteCount*8))
	cutoff := new(big.Int).Sub(space, new(big.Int).Mod(space, rangeSize))
	buffer := make([]byte, byteCount)
	for range limits.MaxRandomAttempts {
		if err := ctx.Err(); err != nil {
			return Integer{}, err
		}
		if _, err := io.ReadFull(source, buffer); err != nil {
			return Integer{}, fmt.Errorf("random source: %w", err)
		}
		candidate := new(big.Int).SetBytes(buffer)
		if candidate.Cmp(cutoff) < 0 {
			candidate.Mod(candidate, rangeSize).Add(candidate, &minimum.n)

			return fromBig(candidate), nil
		}
	}

	return Integer{}, fmt.Errorf("%w: random rejection attempts", gomath.ErrLimitExceeded)
}

// Int64 returns an exact signed machine integer or ErrConversion.
func (i Integer) Int64() (int64, error) {
	if !i.n.IsInt64() {
		return 0, gomath.ErrConversion
	}

	return i.n.Int64(), nil
}

// Uint64 returns an exact unsigned machine integer or ErrConversion.
func (i Integer) Uint64() (uint64, error) {
	if !i.n.IsUint64() {
		return 0, gomath.ErrConversion
	}

	return i.n.Uint64(), nil
}

func fromBig(value *big.Int) Integer {
	var result Integer
	result.n.Set(value)

	return result
}

func binary(ctx context.Context, left, right *big.Int, limits gomath.Limits, operation func(*big.Int, *big.Int, *big.Int) *big.Int) (Integer, error) {
	if err := validateContext(ctx, limits); err != nil {
		return Integer{}, err
	}
	if err := checkIntegerOperands(limits, left, right); err != nil {
		return Integer{}, err
	}

	return checked(operation(new(big.Int), left, right), limits)
}

func checked(value *big.Int, limits gomath.Limits) (Integer, error) {
	if err := checkBits(value.BitLen(), limits); err != nil {
		return Integer{}, err
	}

	return fromBig(value), nil
}

func checkBits(bits int, limits gomath.Limits) error {
	if bits > limits.MaxIntermediateBits {
		return fmt.Errorf("%w: integer magnitude", gomath.ErrLimitExceeded)
	}

	return nil
}

func checkIntegerOperands(limits gomath.Limits, values ...*big.Int) error {
	for _, value := range values {
		if err := checkBits(value.BitLen(), limits); err != nil {
			return err
		}
	}

	return nil
}

func validateLimits(limits gomath.Limits) error { return limits.Validate() }

func validateContext(ctx context.Context, limits gomath.Limits) error {
	if ctx == nil {
		return fmt.Errorf("%w: nil context", gomath.ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	return validateLimits(limits)
}

func validateDigits(text string, base int, allowUnderscores bool) (string, int, bool) {
	var builder strings.Builder
	builder.Grow(len(text))
	count := 0
	previousUnderscore := false
	for index := 0; index < len(text); index++ {
		character := text[index]
		if character == '_' {
			if !allowUnderscores || count == 0 || previousUnderscore || index == len(text)-1 {
				return "", 0, false
			}
			previousUnderscore = true
			continue
		}
		value := digitValue(character)
		if value < 0 || value >= base {
			return "", 0, false
		}
		builder.WriteByte(character)
		count++
		previousUnderscore = false
	}

	return builder.String(), count, count > 0
}

func digitValue(character byte) int {
	if character >= '0' && character <= '9' {
		return int(character - '0')
	}
	if character >= 'a' && character <= 'z' {
		return int(character-'a') + 10
	}
	if character >= 'A' && character <= 'Z' {
		return int(character-'A') + 10
	}

	return -1
}

func nthRoot(ctx context.Context, value *big.Int, degree uint32, limits gomath.Limits) (*big.Int, error) {
	if value.Sign() == 0 {
		return new(big.Int), nil
	}
	low := new(big.Int)
	high := new(big.Int).Lsh(big.NewInt(1), uint((value.BitLen()+int(degree)-1)/int(degree)+1))
	one := big.NewInt(1)
	for new(big.Int).Sub(high, low).Cmp(one) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		mid := new(big.Int).Rsh(new(big.Int).Add(low, high), 1)
		power := new(big.Int).Exp(mid, new(big.Int).SetUint64(uint64(degree)), nil)
		if power.BitLen() > limits.MaxIntermediateBits {
			high = mid
			continue
		}
		if power.Cmp(value) <= 0 {
			low = mid
		} else {
			high = mid
		}
	}

	return low, nil
}
