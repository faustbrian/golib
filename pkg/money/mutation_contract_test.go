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
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func TestInclusivePublicBoundsRemainUsable(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	maximumContext, err := CustomContext(MaxScale)
	if err != nil {
		t.Fatalf("CustomContext(MaxScale) error = %v", err)
	}
	maximumCash, err := CashContext(MaxScale, MaxCashStep)
	if err != nil {
		t.Fatalf("CashContext(MaxScale, MaxCashStep) error = %v", err)
	}
	if maximumContext.Scale() != MaxScale || maximumCash.CashStep() != MaxCashStep {
		t.Fatal("maximum contexts did not retain their inclusive bounds")
	}

	value, _ := Parse("0", euro, maximumContext)
	split, err := value.EqualSplit(context.Background(), MaxAllocationParts)
	if err != nil || len(split.Parts()) != MaxAllocationParts {
		t.Fatalf("EqualSplit(MaxAllocationParts) parts = %d, error = %v", len(split.Parts()), err)
	}
	ratios := make([]integer.Integer, MaxAllocationParts)
	for index := range ratios {
		ratios[index] = integer.New(1)
	}
	allocation, err := value.Allocate(context.Background(), ratios)
	if err != nil || len(allocation.Parts()) != MaxAllocationParts {
		t.Fatalf("Allocate(MaxAllocationParts) parts = %d, error = %v", len(allocation.Parts()), err)
	}
	maximumRatio, err := integer.Parse(strings.Repeat("9", MaxRatioDigits), integer.ParseOptions{
		Base:              10,
		AllowLeadingZeros: true,
		Limits:            arithmeticLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := value.Allocate(context.Background(), []integer.Integer{maximumRatio}); err != nil {
		t.Fatalf("Allocate(MaxRatioDigits) error = %v", err)
	}

	if got := mustAllocationRemainder(integer.New(MaxAllocationParts), MaxAllocationParts); got != MaxAllocationParts {
		t.Fatalf("mustAllocationRemainder(maximum) = %d", got)
	}
	assertInvariantPanic(t, func() { mustAllocationRemainder(integer.New(-1), MaxAllocationParts) })

	for _, maximum := range []string{"0", "-1"} {
		if _, err := ParseRateWithMaximum("0", maximum); !errors.Is(err, ErrInvalidRate) {
			t.Errorf("ParseRateWithMaximum(0, %s) error = %v", maximum, err)
		}
	}
	if rate, err := ParseRateWithMaximum("0", "1"); err != nil || !rate.IsZero() {
		t.Fatalf("ParseRateWithMaximum(0, 1) = %s, %v", rate, err)
	}
	if _, err := ParseDiscountRate("1"); err != nil {
		t.Fatalf("ParseDiscountRate(1) error = %v", err)
	}
	if _, err := ParseTaxRate("10"); err != nil {
		t.Fatalf("ParseTaxRate(10) error = %v", err)
	}
}

func TestMoneyBagOrderingUsesEveryIdentityField(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	euroDefault, _ := DefaultContext(euro)
	dollarDefault, _ := DefaultContext(dollar)
	customTwo, _ := CustomContext(2)
	customThree, _ := CustomContext(3)
	cashFive, _ := CashContext(2, 5)
	cashTen, _ := CashContext(2, 10)

	contexts := []Context{euroDefault, dollarDefault, customTwo, customThree, cashFive, cashTen}
	for index := 1; index < len(contexts); index++ {
		if !contextLess(contexts[index-1], contexts[index]) {
			t.Fatalf("context %d did not sort before context %d", index-1, index)
		}
		if contextLess(contexts[index], contexts[index-1]) {
			t.Fatalf("context %d sorted before context %d", index, index-1)
		}
	}
	if contextLess(customTwo, customTwo) {
		t.Fatal("equal contexts compared less")
	}

	values := []Money{
		mustParseMoney(t, "1.00", dollar, dollarDefault),
		mustParseMoney(t, "1.00", euro, cashTen),
		mustParseMoney(t, "1.00", euro, cashFive),
		mustParseMoney(t, "1.000", euro, customThree),
		mustParseMoney(t, "1.00", euro, customTwo),
		mustParseMoney(t, "1.00", euro, euroDefault),
	}
	bag, err := NewMoneyBag(values...)
	if err != nil {
		t.Fatal(err)
	}
	for index, got := range bag.Values() {
		want := values[len(values)-1-index]
		if got.currency != want.currency || got.context != want.context {
			t.Fatalf("bag order[%d] = %s/%#v, want %s/%#v", index, got.currency, got.context, want.currency, want.context)
		}
	}
}

func TestIdentityValidationRejectsEachMissingField(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	euroContext, _ := DefaultContext(euro)
	amount, _ := ParseAmount("1.00")
	if (Money{amount: amount, currency: euro}).Valid() {
		t.Fatal("money without context is valid")
	}
	if (Money{amount: amount, context: euroContext}).Valid() {
		t.Fatal("money without currency is valid")
	}
	if _, err := Parse("1.000000000000000000", euro, Context{kind: ContextCustom, scale: MaxScale}); err != nil {
		t.Fatalf("Parse(maximum context scale) error = %v", err)
	}

	rate, _ := ParseRate("1")
	observed := time.Unix(1, 0).UTC()
	for _, pair := range [][2]currency.Code{{currency.Code{}, dollar}, {euro, currency.Code{}}} {
		if _, err := NewExchangeRate(pair[0], pair[1], rate, observed, "test"); !errors.Is(err, ErrUnknownCurrency) {
			t.Errorf("NewExchangeRate(%s, %s) error = %v", pair[0], pair[1], err)
		}
	}
	if _, err := NewExchangeRate(euro, dollar, rate, observed, ""); !errors.Is(err, ErrInvalidRate) {
		t.Fatalf("NewExchangeRate(empty source) error = %v", err)
	}
	if _, err := NewExchangeRate(euro, dollar, rate, observed, strings.Repeat("x", MaxRateSourceBytes)); err != nil {
		t.Fatalf("NewExchangeRate(maximum source) error = %v", err)
	}

	custom, _ := CustomContext(2)
	source := mustParseMoney(t, "1.00", euro, euroContext)
	exchange, _ := NewExchangeRate(euro, dollar, rate, observed, "test")
	converted, err := Convert(context.Background(), source, exchange, custom, gomath.RoundHalfEven)
	if err != nil || converted.Converted().Currency() != dollar || converted.Converted().Context() != custom {
		t.Fatalf("Convert(custom target) = %s, %v", converted.Converted(), err)
	}
}

func TestAllocationPreservesTieOrderAndZeroSign(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	oneCent := mustParseMoney(t, "0.01", euro, monetaryContext)
	allocation, err := oneCent.Allocate(context.Background(), []integer.Integer{integer.New(1), integer.New(1)})
	if err != nil {
		t.Fatal(err)
	}
	parts := allocation.Parts()
	if parts[0].String() != "0.01 EUR" || parts[1].String() != "0.00 EUR" {
		t.Fatalf("tie allocation = [%s, %s]", parts[0], parts[1])
	}
	zero := mustParseMoney(t, "0.00", euro, monetaryContext)
	zeroAllocation, err := zero.Allocate(context.Background(), []integer.Integer{integer.New(1)})
	if err != nil || zeroAllocation.Parts()[0].String() != "0.00 EUR" {
		t.Fatalf("zero allocation = %v, %v", zeroAllocation.Parts(), err)
	}
}

func TestTerminatingScaleAcceptsExactLimitsAndRejectsBeyondThem(t *testing.T) {
	t.Parallel()

	for _, factor := range []int64{2, 5} {
		atLimit := rationalWithDenominator(t, new(big.Int).Exp(big.NewInt(factor), big.NewInt(int64(MaxScale)), nil))
		if scale, err := terminatingScale(atLimit); err != nil || scale != MaxScale {
			t.Fatalf("terminatingScale(%d^MaxScale) = %d, %v", factor, scale, err)
		}
		beyond := rationalWithDenominator(t, new(big.Int).Exp(big.NewInt(factor), big.NewInt(int64(MaxScale+1)), nil))
		if _, err := terminatingScale(beyond); !errors.Is(err, ErrPrecisionLoss) {
			t.Fatalf("terminatingScale(%d^(MaxScale+1)) error = %v", factor, err)
		}
	}
	equalFactors := rationalWithDenominator(t, big.NewInt(10))
	if scale, err := terminatingScale(equalFactors); err != nil || scale != 1 {
		t.Fatalf("terminatingScale(1/10) = %d, %v", scale, err)
	}
	repeating := rationalWithDenominator(t, big.NewInt(3))
	if _, err := terminatingScale(repeating); !errors.Is(err, ErrPrecisionLoss) {
		t.Fatalf("terminatingScale(1/3) error = %v", err)
	}
}

func mustParseMoney(t *testing.T, input string, code currency.Code, monetaryContext Context) Money {
	t.Helper()
	value, err := Parse(input, code, monetaryContext)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func rationalWithDenominator(t *testing.T, denominator *big.Int) rational.Rational {
	t.Helper()
	value, err := rational.NewChecked(big.NewInt(1), denominator, arithmeticLimits())
	if err != nil {
		t.Fatal(err)
	}
	return value
}
