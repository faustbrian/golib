package money

import (
	stdcontext "context"
	"fmt"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/math/decimal"
)

// Money is an immutable exact decimal amount bound to one currency and one
// precision context. Its zero value is absent and cannot participate in
// arithmetic.
type Money struct {
	amount   Amount
	currency currency.Code
	context  Context
}

// Parse constructs Money from strict exact decimal text. It never converts
// through binary floating point. Fixed contexts reject represented fractional
// places beyond their scale, including trailing zeroes, rather than silently
// normalizing a context difference.
func Parse(input string, code currency.Code, context Context) (Money, error) {
	if code.IsZero() || code.Status() == international.StatusUnknown {
		return Money{}, ErrUnknownCurrency
	}
	if context.IsZero() || context.scale > MaxScale {
		return Money{}, ErrInvalidContext
	}
	if context.kind == ContextDefault && context.currency != code {
		return Money{}, ErrContextMismatch
	}

	amount, err := ParseAmount(input)
	if err != nil {
		return Money{}, fmt.Errorf("money: parse amount: %w", err)
	}

	resolved := context
	if context.kind == ContextAutomatic {
		resolved.scale = uint8(amount.Scale())
	} else {
		if amount.Scale() > int32(context.scale) {
			return Money{}, ErrPrecisionLoss
		}
		aligned := mustInvariant(alignAmountScale(amount, context.scale))
		amount = mustInvariant(AmountFromDecimal(aligned))
	}

	return Money{amount: amount, currency: code, context: resolved}, nil
}

// Amount returns the immutable exact decimal amount.
func (money Money) Amount() Amount { return money.amount }

// Currency returns the currency identity owned by international.
func (money Money) Currency() currency.Code { return money.currency }

// Context returns the resolved precision context.
func (money Money) Context() Context { return money.context }

// Valid reports whether money has a currency and context.
func (money Money) Valid() bool {
	return !money.currency.IsZero() && !money.context.IsZero()
}

// String returns a deterministic diagnostic representation. Locale display
// belongs to the optional format package.
func (money Money) String() string {
	if !money.Valid() {
		return ""
	}

	return money.amount.String() + " " + money.currency.String()
}

// Add returns the exact sum. Currency and resolved context must match.
func (money Money) Add(other Money) (Money, error) {
	if err := money.compatible(other); err != nil {
		return Money{}, err
	}
	amount, err := money.amount.add(other.amount)
	if err != nil {
		return Money{}, fmt.Errorf("money: add: %w", err)
	}

	return Money{
		amount:   amount,
		currency: money.currency,
		context:  money.context,
	}, nil
}

// Sub returns the exact difference. Currency and resolved context must match.
func (money Money) Sub(other Money) (Money, error) {
	if err := money.compatible(other); err != nil {
		return Money{}, err
	}
	amount, err := money.amount.sub(other.amount)
	if err != nil {
		return Money{}, fmt.Errorf("money: subtract: %w", err)
	}

	return Money{
		amount:   amount,
		currency: money.currency,
		context:  money.context,
	}, nil
}

func alignAmountScale(amount Amount, scale uint8) (decimal.Decimal, error) {
	result, err := amount.Decimal().Quantize(
		stdcontext.Background(),
		int32(scale),
		decimal.Down,
		arithmeticLimits(),
	)

	return result.Value, err
}

// Compare returns -1, 0, or +1 after verifying currency and context identity.
func (money Money) Compare(other Money) (int, error) {
	if err := money.compatible(other); err != nil {
		return 0, err
	}

	return money.amount.cmp(other.amount), nil
}

// Equal reports exact monetary equality after verifying currency and context
// identity. Mismatched values return an error instead of comparing false.
func (money Money) Equal(other Money) (bool, error) {
	comparison, err := money.Compare(other)
	return comparison == 0, err
}

// Neg returns the additive inverse without mutating money.
func (money Money) Neg() (Money, error) {
	if !money.Valid() {
		return Money{}, ErrInvalidMoney
	}

	return Money{amount: money.amount.neg(), currency: money.currency, context: money.context}, nil
}

// Abs returns the nonnegative magnitude without mutating money.
func (money Money) Abs() (Money, error) {
	if !money.Valid() {
		return Money{}, ErrInvalidMoney
	}

	return Money{amount: money.amount.abs(), currency: money.currency, context: money.context}, nil
}

// Sign returns the amount sign. Invalid zero-value Money reports zero; callers
// can distinguish it with Valid.
func (money Money) Sign() int { return money.amount.Sign() }

// IsZero reports whether a valid Money has numeric value zero.
func (money Money) IsZero() bool { return money.Valid() && money.amount.IsZero() }

func (money Money) compatible(other Money) error {
	if !money.Valid() || !other.Valid() {
		return ErrInvalidMoney
	}
	if money.currency != other.currency {
		return ErrCurrencyMismatch
	}
	if money.context != other.context {
		return ErrContextMismatch
	}

	return nil
}
