package money

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/currency"
	gomath "github.com/faustbrian/golib/pkg/math"
)

// MaxRateSourceBytes bounds persisted and diagnostic source metadata.
const MaxRateSourceBytes = 128

// ExchangeRate is an injected exact directed rate with required attribution.
// It contains no fetching or ambient live-FX behavior.
type ExchangeRate struct {
	base       currency.Code
	quote      currency.Code
	rate       Rate
	observedAt time.Time
	source     string
}

// NewExchangeRate validates a directed rate and its observation metadata.
func NewExchangeRate(base, quote currency.Code, rate Rate, observedAt time.Time, source string) (ExchangeRate, error) {
	if base.IsZero() || quote.IsZero() || base.Status() == international.StatusUnknown || quote.Status() == international.StatusUnknown {
		return ExchangeRate{}, ErrUnknownCurrency
	}
	if base == quote {
		return ExchangeRate{}, ErrCurrencyMismatch
	}
	if !rate.Valid() || rate.IsZero() {
		return ExchangeRate{}, ErrInvalidRate
	}
	if observedAt.IsZero() || !validRateSource(source) {
		return ExchangeRate{}, ErrInvalidRate
	}

	return ExchangeRate{
		base:       base,
		quote:      quote,
		rate:       rate,
		observedAt: observedAt,
		source:     source,
	}, nil
}

// Base returns the source currency.
func (rate ExchangeRate) Base() currency.Code { return rate.base }

// Quote returns the destination currency.
func (rate ExchangeRate) Quote() currency.Code { return rate.quote }

// Exact returns the exact directed multiplier.
func (rate ExchangeRate) Exact() Rate { return rate.rate }

// ObservedAt returns the rate observation timestamp.
func (rate ExchangeRate) ObservedAt() time.Time { return rate.observedAt }

// Source returns bounded attribution metadata.
func (rate ExchangeRate) Source() string { return rate.source }

// ConversionResult retains both values, exact rate metadata, and rounding.
type ConversionResult struct {
	source    Money
	converted Money
	rate      ExchangeRate
	rounding  RoundingResult
}

// Source returns the original monetary value.
func (result ConversionResult) Source() Money { return result.source }

// Converted returns the rounded quote-currency value.
func (result ConversionResult) Converted() Money { return result.converted }

// Rate returns exact conversion and attribution metadata.
func (result ConversionResult) Rate() ExchangeRate { return result.rate }

// Rounding returns conditions raised at the explicit quote boundary.
func (result ConversionResult) Rounding() RoundingResult { return result.rounding }

// Convert applies only the supplied directed exact rate and rounds explicitly
// into target. It never contacts a live FX service.
func Convert(ctx context.Context, source Money, rate ExchangeRate, target Context, mode gomath.RoundingMode) (ConversionResult, error) {
	if !source.Valid() {
		return ConversionResult{}, ErrInvalidMoney
	}
	if source.currency != rate.base {
		return ConversionResult{}, ErrCurrencyMismatch
	}
	if target.IsZero() || target.kind == ContextAutomatic {
		return ConversionResult{}, ErrInvalidContext
	}
	if target.kind == ContextDefault && target.currency != rate.quote {
		return ConversionResult{}, ErrContextMismatch
	}
	amount, err := rationalFromAmount(source.amount)
	if err != nil {
		return ConversionResult{}, err
	}
	converted, err := amount.Mul(ctx, rate.rate.value, arithmeticLimits())
	if err != nil {
		return ConversionResult{}, fmt.Errorf("money: convert: %w", err)
	}
	exact := RationalMoney{amount: converted, currency: rate.quote, source: source.context}
	value, rounding, err := exact.Round(target, mode)
	if err != nil {
		return ConversionResult{}, err
	}

	return ConversionResult{source: source, converted: value, rate: rate, rounding: rounding}, nil
}

func validRateSource(source string) bool {
	if source == "" || len(source) > MaxRateSourceBytes || !utf8.ValidString(source) || strings.TrimSpace(source) != source {
		return false
	}
	for _, character := range source {
		if unicode.IsControl(character) {
			return false
		}
	}

	return true
}
