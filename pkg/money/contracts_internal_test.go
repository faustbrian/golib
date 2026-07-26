package money

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/international/currency"
	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func TestMoneyZeroValuesAndOperationErrorsAreExplicit(t *testing.T) {
	t.Parallel()

	zero := Money{}
	if zero.String() != "" || zero.Valid() || zero.IsZero() || zero.Sign() != 0 {
		t.Fatal("Money zero-value contract failed")
	}
	if _, err := zero.Add(zero); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("zero.Add() error = %v", err)
	}
	if _, err := zero.Sub(zero); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("zero.Sub() error = %v", err)
	}
	if _, err := zero.Compare(zero); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("zero.Compare() error = %v", err)
	}
	if _, err := zero.Neg(); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("zero.Neg() error = %v", err)
	}
	if _, err := zero.Abs(); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("zero.Abs() error = %v", err)
	}
	if _, err := zero.MinorUnits(); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("zero.MinorUnits() error = %v", err)
	}
	if _, err := zero.Mul(context.Background(), Rate{}); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("zero.Mul() error = %v", err)
	}
	if _, err := zero.Quo(context.Background(), Rate{}); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("zero.Quo() error = %v", err)
	}
}

func TestAmountAndMoneyArithmeticEnforceOutputBounds(t *testing.T) {
	t.Parallel()

	tooManyDigits := decimal.MustParse(strings.Repeat("9", MaxAmountDigits+1))
	if _, err := AmountFromDecimal(tooManyDigits); !errors.Is(err, ErrAmountLimit) {
		t.Errorf("AmountFromDecimal(digits) error = %v", err)
	}
	positiveExponent, err := decimal.FromBig(big.NewInt(1), 1, gomath.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AmountFromDecimal(positiveExponent); !errors.Is(err, ErrAmountLimit) {
		t.Errorf("AmountFromDecimal(exponent) error = %v", err)
	}

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := CustomContext(0)
	maximum, _ := Parse(strings.Repeat("9", MaxAmountDigits), euro, monetaryContext)
	one, _ := Parse("1", euro, monetaryContext)
	if _, err := maximum.Add(one); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Errorf("Add(over limit) error = %v", err)
	}
	minimum, _ := Parse("-"+strings.Repeat("9", MaxAmountDigits), euro, monetaryContext)
	if _, err := minimum.Sub(one); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Errorf("Sub(over limit) error = %v", err)
	}
}

func TestAllocationRejectsInvalidWorkAndKeepsBagAliasesPrivate(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	euroContext, _ := DefaultContext(euro)
	dollarContext, _ := DefaultContext(dollar)
	value, _ := Parse("1.00", euro, euroContext)
	dollars, _ := Parse("1.00", dollar, dollarContext)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := (AllocationResult{}).Sum(); !errors.Is(err, ErrInvalidAllocation) {
		t.Errorf("empty Sum() error = %v", err)
	}
	if _, err := (AllocationResult{parts: []Money{value, dollars}}).Sum(); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("mismatched Sum() error = %v", err)
	}
	for _, count := range []int{-1, MaxAllocationParts + 1} {
		if _, err := value.EqualSplit(context.Background(), count); !errors.Is(err, ErrInvalidAllocation) {
			t.Errorf("EqualSplit(%d) error = %v", count, err)
		}
	}
	automatic, _ := Parse("1.2", euro, AutomaticContext())
	if _, err := automatic.EqualSplit(context.Background(), 2); !errors.Is(err, ErrInvalidContext) {
		t.Errorf("automatic EqualSplit() error = %v", err)
	}
	if _, err := automatic.Allocate(context.Background(), []integer.Integer{integer.New(1)}); !errors.Is(err, ErrInvalidContext) {
		t.Errorf("automatic Allocate() error = %v", err)
	}
	//lint:ignore SA1012 intentional nil-context contract test
	if _, err := value.EqualSplit(nil, 1); !errors.Is(err, ErrInvalidAllocation) { //nolint:staticcheck // intentional nil contract
		t.Errorf("EqualSplit(nil) error = %v", err)
	}
	if _, err := value.EqualSplit(canceled, 1); !errors.Is(err, context.Canceled) {
		t.Errorf("EqualSplit(canceled) error = %v", err)
	}
	if _, err := (Money{}).EqualSplit(context.Background(), 1); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("zero.EqualSplit() error = %v", err)
	}

	invalidRatios := [][]integer.Integer{
		nil,
		{integer.New(-1)},
		{mustLargeInteger(t, MaxRatioDigits+1)},
	}
	for _, ratios := range invalidRatios {
		if _, err := value.Allocate(context.Background(), ratios); !errors.Is(err, ErrInvalidAllocation) {
			t.Errorf("Allocate(%v) error = %v", ratios, err)
		}
	}
	//lint:ignore SA1012 intentional nil-context contract test
	if _, err := value.Allocate(nil, []integer.Integer{integer.New(1)}); !errors.Is(err, ErrInvalidAllocation) { //nolint:staticcheck // intentional nil contract
		t.Errorf("Allocate(nil) error = %v", err)
	}
	if _, err := value.Allocate(canceled, []integer.Integer{integer.New(1)}); !errors.Is(err, context.Canceled) {
		t.Errorf("Allocate(canceled) error = %v", err)
	}
	if _, err := (Money{}).Allocate(context.Background(), []integer.Integer{integer.New(1)}); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("zero.Allocate() error = %v", err)
	}

	if _, err := NewMoneyBag(value, Money{}); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("NewMoneyBag(invalid) error = %v", err)
	}
	bag, _ := NewMoneyBag(value)
	values := bag.Values()
	values[0] = dollars
	if got, _ := bag.Get(euro, euroContext); got.String() != "1.00 EUR" {
		t.Fatal("MoneyBag.Values exposed an alias")
	}
	if _, ok := bag.Get(dollar, dollarContext); ok {
		t.Fatal("MoneyBag.Get found absent value")
	}
	full := MoneyBag{values: make([]Money, MaxMoneyBagEntries)}
	if _, err := full.Add(value); !errors.Is(err, ErrMoneyBagLimit) {
		t.Errorf("full bag Add() error = %v", err)
	}
	contexts := []Context{
		{kind: ContextDefault, scale: 2, currency: euro},
		{kind: ContextCustom, scale: 2},
		{kind: ContextCustom, scale: 3},
		{kind: ContextCash, scale: 2, cashStep: 5},
		{kind: ContextCash, scale: 2, cashStep: 10},
		{kind: ContextDefault, scale: 2, currency: dollar},
	}
	for left := range contexts {
		for right := range contexts {
			_ = contextLess(contexts[left], contexts[right])
		}
	}
	customZero, _ := CustomContext(0)
	whole, err := FromMinorUnits(integer.New(-1), euro, customZero)
	if err != nil || whole.String() != "-1 EUR" {
		t.Fatalf("scale-zero FromMinorUnits() = %s, %v", whole, err)
	}
}

func TestRateRationalAndConversionErrorContracts(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	euroContext, _ := DefaultContext(euro)
	dollarContext, _ := DefaultContext(dollar)
	value, _ := Parse("1.00", euro, euroContext)
	rate, _ := ParseRate("1/3")
	zeroRate, _ := ParseRate("0")
	if rate.String() != "1/3" || rate.Rational().String() != "1/3" {
		t.Fatal("Rate accessors failed")
	}
	if _, err := value.Mul(context.Background(), Rate{}); !errors.Is(err, ErrInvalidRate) {
		t.Errorf("Mul(invalid rate) error = %v", err)
	}
	//lint:ignore SA1012 intentional nil-context contract test
	if _, err := value.Mul(nil, rate); err == nil { //nolint:staticcheck // intentional nil contract
		t.Error("Mul(nil context) succeeded")
	}
	if _, err := value.Quo(context.Background(), zeroRate); !errors.Is(err, ErrInvalidRate) {
		t.Errorf("Quo(zero rate) error = %v", err)
	}
	//lint:ignore SA1012 intentional nil-context contract test
	if _, err := value.Quo(nil, rate); err == nil { //nolint:staticcheck // intentional nil contract
		t.Error("Quo(nil context) succeeded")
	}
	zero, _ := Parse("0.00", euro, euroContext)
	if _, err := value.Ratio(context.Background(), zero); !errors.Is(err, gomath.ErrDivisionByZero) {
		t.Errorf("Ratio(zero) error = %v", err)
	}
	//lint:ignore SA1012 intentional nil-context contract test
	if _, err := value.Ratio(nil, value); err == nil { //nolint:staticcheck // intentional nil contract
		t.Error("Ratio(nil context) succeeded")
	}

	exact, _ := value.Mul(context.Background(), rate)
	if exact.Currency() != euro || exact.Rational().String() != "1/3" || exact.String() != "1/3 EUR" {
		t.Fatal("RationalMoney accessors failed")
	}
	if (RationalMoney{}).String() != "" || (Ratio{}).String() != "" || (Ratio{}).Rational().Sign() != 0 {
		t.Fatal("rational zero-value contract failed")
	}
	if _, _, err := (RationalMoney{}).Round(euroContext, gomath.RoundHalfEven); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("zero RationalMoney.Round() error = %v", err)
	}
	if _, _, err := exact.Round(dollarContext, gomath.RoundHalfEven); !errors.Is(err, ErrContextMismatch) {
		t.Errorf("Round(context mismatch) error = %v", err)
	}
	if _, _, err := exact.Round(euroContext, gomath.RoundingMode(255)); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("Round(mode) error = %v", err)
	}
	tooPrecise, _ := rational.NewChecked(big.NewInt(1), new(big.Int).Lsh(big.NewInt(1), uint(MaxScale+1)), arithmeticLimits())
	if _, err := terminatingScale(tooPrecise); !errors.Is(err, ErrPrecisionLoss) {
		t.Errorf("terminatingScale(over limit) error = %v", err)
	}
	oneFifth, _ := rational.New(1, 5)
	if scale, err := terminatingScale(oneFifth); err != nil || scale != 1 {
		t.Fatalf("terminatingScale(1/5) = %d, %v", scale, err)
	}
	invalidCash := Context{kind: ContextCash, scale: 2}
	if _, _, err := (RationalMoney{amount: rational.Zero(), currency: euro}).Round(invalidCash, gomath.RoundHalfEven); err == nil {
		t.Error("Round(invalid cash) succeeded")
	}
	hugeInteger := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(MaxAmountDigits+1)), nil)
	huge, _ := rational.FromBig(new(big.Rat).SetInt(hugeInteger), gomath.DefaultLimits())
	customZero, _ := CustomContext(0)
	if _, _, err := (RationalMoney{amount: huge, currency: euro}).Round(customZero, gomath.RoundHalfEven); err == nil {
		t.Error("Round(oversized output) succeeded")
	}
	if _, _, err := roundedDecimal(rational.Zero(), Context{kind: ContextCash, scale: 255, cashStep: 1}, gomath.RoundHalfEven); err == nil {
		t.Error("roundedDecimal(oversized scale) succeeded")
	}

	observed := time.Now().UTC()
	validExchange, _ := NewExchangeRate(euro, dollar, rate, observed, "test")
	if validExchange.Base() != euro || validExchange.Quote() != dollar || validExchange.Exact().String() != "1/3" {
		t.Fatal("ExchangeRate accessors failed")
	}
	result, _ := Convert(context.Background(), value, validExchange, dollarContext, gomath.RoundHalfEven)
	if result.Source().String() != value.String() || result.Rounding().Conditions() == 0 {
		t.Fatal("ConversionResult accessors failed")
	}
	invalidExchanges := []struct {
		base, quote currency.Code
		rate        Rate
		at          time.Time
		source      string
	}{
		{base: currency.Code{}, quote: dollar, rate: rate, at: observed, source: "test"},
		{base: euro, quote: euro, rate: rate, at: observed, source: "test"},
		{base: euro, quote: dollar, rate: Rate{}, at: observed, source: "test"},
		{base: euro, quote: dollar, rate: zeroRate, at: observed, source: "test"},
		{base: euro, quote: dollar, rate: rate, source: "test"},
		{base: euro, quote: dollar, rate: rate, at: observed, source: " bad"},
		{base: euro, quote: dollar, rate: rate, at: observed, source: "bad\nsource"},
		{base: euro, quote: dollar, rate: rate, at: observed, source: strings.Repeat("x", MaxRateSourceBytes+1)},
	}
	for _, test := range invalidExchanges {
		if _, err := NewExchangeRate(test.base, test.quote, test.rate, test.at, test.source); err == nil {
			t.Errorf("NewExchangeRate(%s, %s, %q) succeeded", test.base, test.quote, test.source)
		}
	}
	if _, err := Convert(context.Background(), Money{}, validExchange, dollarContext, gomath.RoundHalfEven); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("Convert(zero) error = %v", err)
	}
	if _, err := Convert(context.Background(), value, validExchange, AutomaticContext(), gomath.RoundHalfEven); !errors.Is(err, ErrInvalidContext) {
		t.Errorf("Convert(automatic) error = %v", err)
	}
	if _, err := Convert(context.Background(), value, validExchange, euroContext, gomath.RoundHalfEven); !errors.Is(err, ErrContextMismatch) {
		t.Errorf("Convert(context mismatch) error = %v", err)
	}
	//lint:ignore SA1012 intentional nil-context contract test
	if _, err := Convert(nil, value, validExchange, dollarContext, gomath.RoundHalfEven); err == nil { //nolint:staticcheck // intentional nil contract
		t.Error("Convert(nil context) succeeded")
	}
	if _, err := Convert(context.Background(), value, validExchange, dollarContext, gomath.RoundingMode(255)); err == nil {
		t.Error("Convert(invalid mode) succeeded")
	}
}

func TestTaxAndDiscountAccessorsAndInvalidInputs(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	value, _ := Parse("1.00", euro, monetaryContext)
	taxRate, _ := ParseTaxRate("0.24")
	discountRate, _ := ParseDiscountRate("0.10")
	if taxRate.Rate().String() != "6/25" || discountRate.Rate().String() != "1/10" {
		t.Fatal("specialized rate accessors failed")
	}
	tax, _ := AddTax(context.Background(), value, taxRate, gomath.RoundHalfEven)
	if tax.Rounding().Conditions() != 0 {
		t.Fatal("exact tax unexpectedly rounded")
	}
	if _, err := AddTax(context.Background(), value, TaxRate{}, gomath.RoundHalfEven); !errors.Is(err, ErrInvalidRate) {
		t.Errorf("AddTax(invalid rate) error = %v", err)
	}
	//lint:ignore SA1012 intentional nil-context contract test
	if _, err := AddTax(nil, value, taxRate, gomath.RoundHalfEven); err == nil { //nolint:staticcheck // intentional nil contract
		t.Error("AddTax(nil context) succeeded")
	}
	if _, err := AddTax(context.Background(), Money{}, taxRate, gomath.RoundHalfEven); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("AddTax(zero money) error = %v", err)
	}
	if _, err := AddTax(context.Background(), value, taxRate, gomath.RoundingMode(255)); err == nil {
		t.Error("AddTax(invalid mode) succeeded")
	}
	if _, err := ExtractTax(context.Background(), Money{}, taxRate, gomath.RoundHalfEven); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("ExtractTax(zero) error = %v", err)
	}
	if _, err := ExtractTax(context.Background(), value, TaxRate{}, gomath.RoundHalfEven); !errors.Is(err, ErrInvalidRate) {
		t.Errorf("ExtractTax(invalid rate) error = %v", err)
	}
	//lint:ignore SA1012 intentional nil-context contract test
	if _, err := ExtractTax(nil, value, taxRate, gomath.RoundHalfEven); err == nil { //nolint:staticcheck // intentional nil contract
		t.Error("ExtractTax(nil context) succeeded")
	}
	if _, err := ExtractTax(context.Background(), value, taxRate, gomath.RoundingMode(255)); err == nil {
		t.Error("ExtractTax(invalid mode) succeeded")
	}
	if _, err := ApplyDiscount(context.Background(), value, DiscountRate{}, gomath.RoundHalfEven); !errors.Is(err, ErrInvalidRate) {
		t.Errorf("ApplyDiscount(invalid rate) error = %v", err)
	}
	//lint:ignore SA1012 intentional nil-context contract test
	if _, err := ApplyDiscount(nil, value, discountRate, gomath.RoundHalfEven); err == nil { //nolint:staticcheck // intentional nil contract
		t.Error("ApplyDiscount(nil context) succeeded")
	}
	if _, err := ApplyDiscount(context.Background(), Money{}, discountRate, gomath.RoundHalfEven); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("ApplyDiscount(zero money) error = %v", err)
	}
	if _, err := ApplyDiscount(context.Background(), value, discountRate, gomath.RoundingMode(255)); err == nil {
		t.Error("ApplyDiscount(invalid mode) succeeded")
	}
}

func TestCancellationPropagatesFromEveryBoundedArithmeticStage(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	euroContext, _ := DefaultContext(euro)
	dollarContext, _ := DefaultContext(dollar)
	value, _ := Parse("10.01", euro, euroContext)
	rate, _ := ParseRate("1/3")
	taxRate, _ := ParseTaxRate("0.24")
	discountRate, _ := ParseDiscountRate("1/3")
	exchange, _ := NewExchangeRate(euro, dollar, rate, time.Now().UTC(), "test")
	ratios := []integer.Integer{integer.New(1), integer.New(2), integer.New(3)}

	for failAt := 1; failAt <= 30; failAt++ {
		ctx := &stagedCancellation{failAt: failAt}
		_, _ = value.EqualSplit(ctx, 3)
		ctx = &stagedCancellation{failAt: failAt}
		_, _ = value.Allocate(ctx, ratios)
		ctx = &stagedCancellation{failAt: failAt}
		_, _ = value.Mul(ctx, rate)
		ctx = &stagedCancellation{failAt: failAt}
		_, _ = value.Quo(ctx, rate)
		ctx = &stagedCancellation{failAt: failAt}
		_, _ = value.Ratio(ctx, value)
		ctx = &stagedCancellation{failAt: failAt}
		_, _ = AddTax(ctx, value, taxRate, gomath.RoundHalfEven)
		ctx = &stagedCancellation{failAt: failAt}
		_, _ = ExtractTax(ctx, value, taxRate, gomath.RoundHalfEven)
		ctx = &stagedCancellation{failAt: failAt}
		_, _ = ApplyDiscount(ctx, value, discountRate, gomath.RoundHalfEven)
		ctx = &stagedCancellation{failAt: failAt}
		_, _ = Convert(ctx, value, exchange, dollarContext, gomath.RoundHalfEven)
	}
}

func TestCorruptInternalValuesFailClosedAtEveryNumericBoundary(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	monetaryContext, _ := CustomContext(0)
	dollarContext, _ := DefaultContext(dollar)
	rate, _ := ParseRate("1")
	hugeDecimal := decimal.MustParse(strings.Repeat("9", 3_000))
	corrupt := Money{
		amount:   Amount{value: hugeDecimal},
		currency: euro,
		context:  monetaryContext,
	}

	if _, err := corrupt.EqualSplit(context.Background(), 2); err == nil {
		t.Error("EqualSplit(corrupt) succeeded")
	}
	if _, err := corrupt.Allocate(context.Background(), []integer.Integer{integer.New(1)}); err == nil {
		t.Error("Allocate(corrupt) succeeded")
	}
	if _, err := corrupt.Mul(context.Background(), rate); err == nil {
		t.Error("Mul(corrupt) succeeded")
	}
	if _, err := corrupt.Quo(context.Background(), rate); err == nil {
		t.Error("Quo(corrupt) succeeded")
	}
	valid, _ := Parse("1", euro, monetaryContext)
	if _, err := corrupt.Ratio(context.Background(), valid); err == nil {
		t.Error("Ratio(corrupt left) succeeded")
	}
	if _, err := valid.Ratio(context.Background(), corrupt); err == nil {
		t.Error("Ratio(corrupt right) succeeded")
	}
	exchange, _ := NewExchangeRate(euro, dollar, rate, time.Now().UTC(), "test")
	if _, err := Convert(context.Background(), corrupt, exchange, dollarContext, gomath.RoundHalfEven); err == nil {
		t.Error("Convert(corrupt) succeeded")
	}
	taxRate, _ := ParseTaxRate("1")
	if _, err := ExtractTax(context.Background(), corrupt, taxRate, gomath.RoundHalfEven); err == nil {
		t.Error("ExtractTax(corrupt) succeeded")
	}

	maximum, _ := Parse(strings.Repeat("9", MaxAmountDigits), euro, monetaryContext)
	one, _ := Parse("1", euro, monetaryContext)
	bag := MoneyBag{values: []Money{maximum}}
	if _, err := bag.Add(one); err == nil {
		t.Error("MoneyBag.Add(over limit) succeeded")
	}
	if _, err := AddTax(context.Background(), maximum, taxRate, gomath.RoundHalfEven); err == nil {
		t.Error("AddTax(over limit) succeeded")
	}
	cash, _ := CashContext(2, 5)
	oneThird, _ := rational.New(1, 3)
	if _, _, err := roundedDecimal(oneThird, cash, gomath.RoundingMode(255)); err == nil {
		t.Error("roundedDecimal(invalid mode) succeeded")
	}
}

func TestInternalInvariantAssertionsFailClosed(t *testing.T) {
	t.Parallel()

	if got := mustInvariant(42, nil); got != 42 {
		t.Fatalf("mustInvariant() = %d", got)
	}
	assertInvariantPanic(t, func() { mustInvariant(0, errors.New("dependency failure")) })
	assertInvariantPanic(t, func() { mustAllocationRemainder(integer.New(2), 1) })
	assertInvariantPanic(t, func() { mustAllocationRemainder(mustLargeInteger(t, MaxAmountDigits+1), 1) })
}

func assertInvariantPanic(t *testing.T, operation func()) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != invariantViolation {
			t.Fatalf("panic = %#v, want %q", recovered, invariantViolation)
		}
	}()
	operation()
}

func mustLargeInteger(t *testing.T, digits int) integer.Integer {
	t.Helper()
	value, err := integer.Parse(strings.Repeat("9", digits), integer.ParseOptions{
		Base:              10,
		AllowLeadingZeros: true,
		Limits:            gomath.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return value
}

type stagedCancellation struct {
	calls  int
	failAt int
}

func (staged *stagedCancellation) Deadline() (time.Time, bool) { return time.Time{}, false }
func (staged *stagedCancellation) Done() <-chan struct{}       { return nil }
func (staged *stagedCancellation) Value(any) any               { return nil }
func (staged *stagedCancellation) Err() error {
	staged.calls++
	if staged.calls >= staged.failAt {
		return context.Canceled
	}

	return nil
}
