package money

import (
	"context"
	"errors"
	"strings"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/currency"
	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/math/rational"
)

func TestEveryAuthoritativeCurrencyCanBeRepresentedExplicitly(t *testing.T) {
	t.Parallel()

	for _, record := range currency.DatasetRecords() {
		options := currency.ParseOptions{AllowHistoric: record.Status == international.StatusHistoric}
		code, err := currency.ParseWithOptions(record.ID, options)
		if err != nil {
			t.Errorf("ParseWithOptions(%s) error = %v", record.ID, err)
			continue
		}
		monetaryContext, contextErr := DefaultContext(code)
		if _, hasMinorUnits := code.MinorUnits(); !hasMinorUnits {
			if !errors.Is(contextErr, ErrMinorUnitsUnavailable) {
				t.Errorf("DefaultContext(%s) error = %v", code, contextErr)
			}
			monetaryContext, _ = CustomContext(2)
		} else if contextErr != nil {
			t.Errorf("DefaultContext(%s) error = %v", code, contextErr)
			continue
		}
		value, err := Parse("0", code, monetaryContext)
		if err != nil || value.Currency() != code {
			t.Errorf("Parse(0, %s) = %s, %v", code, value, err)
		}
	}
}

func TestAllocationConservationPropertyAcrossSignsAndCounts(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	largeRatio := mustLargeInteger(t, MaxRatioDigits)
	ratioVectors := [][]integer.Integer{
		{integer.New(1)},
		{integer.New(1), integer.New(1), integer.New(1)},
		{integer.New(1), integer.New(2), integer.New(5), integer.New(8)},
		{integer.New(13), integer.New(7), integer.New(13), integer.New(7)},
		{integer.New(1), largeRatio},
	}
	for units := int64(-200); units <= 200; units++ {
		total, _ := FromMinorUnits(integer.New(units), euro, monetaryContext)
		for count := 1; count <= 12; count++ {
			allocation, err := total.EqualSplit(context.Background(), count)
			if err != nil {
				t.Fatalf("EqualSplit(%d, %d) error = %v", units, count, err)
			}
			assertAllocationEquals(t, allocation, total)
		}
		for _, ratios := range ratioVectors {
			allocation, err := total.Allocate(context.Background(), ratios)
			if err != nil {
				t.Fatalf("Allocate(%d, %v) error = %v", units, ratios, err)
			}
			assertAllocationEquals(t, allocation, total)
		}
	}
}

func TestTaxDiscountAndCashMatricesConserveForEveryRoundingMode(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := DefaultContext(euro)
	taxRate, _ := ParseTaxRate("0.24")
	discountRate, _ := ParseDiscountRate("1/3")
	modes := []gomath.RoundingMode{
		gomath.RoundHalfEven,
		gomath.RoundHalfUp,
		gomath.RoundHalfDown,
		gomath.RoundDown,
		gomath.RoundUp,
		gomath.RoundCeiling,
		gomath.RoundFloor,
	}
	for units := int64(-25); units <= 25; units++ {
		value, _ := FromMinorUnits(integer.New(units), euro, monetaryContext)
		for _, mode := range modes {
			tax, err := AddTax(context.Background(), value, taxRate, mode)
			if err != nil {
				t.Fatalf("AddTax(%d, %s) error = %v", units, mode, err)
			}
			assertSum(t, tax.Net(), tax.Tax(), tax.Gross())
			extracted, err := ExtractTax(context.Background(), tax.Gross(), taxRate, mode)
			if err != nil {
				t.Fatalf("ExtractTax(%d, %s) error = %v", units, mode, err)
			}
			assertSum(t, extracted.Net(), extracted.Tax(), extracted.Gross())
			discount, err := ApplyDiscount(context.Background(), value, discountRate, mode)
			if err != nil {
				t.Fatalf("ApplyDiscount(%d, %s) error = %v", units, mode, err)
			}
			assertSum(t, discount.Final(), discount.Discount(), discount.Original())

			discountAfterTax, err := ApplyDiscount(context.Background(), tax.Gross(), discountRate, mode)
			if err != nil {
				t.Fatalf("discount-after-tax(%d, %s) error = %v", units, mode, err)
			}
			assertSum(
				t,
				discountAfterTax.Final(),
				discountAfterTax.Discount(),
				discountAfterTax.Original(),
			)

			taxAfterDiscount, err := AddTax(context.Background(), discount.Final(), taxRate, mode)
			if err != nil {
				t.Fatalf("tax-after-discount(%d, %s) error = %v", units, mode, err)
			}
			assertSum(t, taxAfterDiscount.Net(), taxAfterDiscount.Tax(), taxAfterDiscount.Gross())
		}
	}
}

func TestCashRoundingMatrixAcrossStepsSignsAndModes(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	modes := []gomath.RoundingMode{
		gomath.RoundHalfEven,
		gomath.RoundHalfUp,
		gomath.RoundHalfDown,
		gomath.RoundDown,
		gomath.RoundUp,
		gomath.RoundCeiling,
		gomath.RoundFloor,
	}
	for _, step := range []uint64{1, 5, 10, 25} {
		cashContext, _ := CashContext(2, step)
		for numerator := int64(-205); numerator <= 205; numerator++ {
			exact, _ := rational.New(numerator, 200)
			value := RationalMoney{amount: exact, currency: euro}
			for _, mode := range modes {
				rounded, _, err := value.Round(cashContext, mode)
				if err != nil {
					t.Fatalf("Round(%d/200, step %d, %s) error = %v", numerator, step, mode, err)
				}
				units, err := rounded.MinorUnits()
				if err != nil {
					t.Fatal(err)
				}
				_, remainder, err := units.QuoRem(
					context.Background(), integer.New(int64(step)), arithmeticLimits(),
				)
				if err != nil || remainder.Sign() != 0 {
					t.Fatalf("Round(%d/200, step %d, %s) = %s", numerator, step, mode, rounded)
				}
			}
		}
	}

	cashContext, _ := CashContext(2, 5)
	ties := []struct {
		mode               gomath.RoundingMode
		positive, negative string
	}{
		{gomath.RoundHalfEven, "1.00", "-1.00"},
		{gomath.RoundHalfUp, "1.05", "-1.05"},
		{gomath.RoundHalfDown, "1.00", "-1.00"},
		{gomath.RoundDown, "1.00", "-1.00"},
		{gomath.RoundUp, "1.05", "-1.05"},
		{gomath.RoundCeiling, "1.05", "-1.00"},
		{gomath.RoundFloor, "1.00", "-1.05"},
	}
	for _, test := range ties {
		for input, want := range map[string]string{"1.025": test.positive, "-1.025": test.negative} {
			amount, _ := ParseAmount(input)
			exact, _ := rationalFromAmount(amount)
			got, _, err := (RationalMoney{amount: exact, currency: euro}).Round(cashContext, test.mode)
			if err != nil || got.Amount().String() != want {
				t.Errorf("Round(%s, %s) = %s, %v; want %s", input, test.mode, got, err, want)
			}
		}
	}
}

func TestResourceAndIdentityBoundariesRejectHostileInputs(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	euroContext, _ := DefaultContext(euro)
	dollarContext, _ := DefaultContext(dollar)

	if _, err := Parse("1", currency.Code{}, euroContext); !errors.Is(err, ErrUnknownCurrency) {
		t.Errorf("zero currency error = %v", err)
	}
	if _, err := Parse("1", euro, Context{}); !errors.Is(err, ErrInvalidContext) {
		t.Errorf("zero context error = %v", err)
	}
	if _, err := Parse(strings.Repeat("9", MaxAmountDigits+1), euro, euroContext); !errors.Is(err, gomath.ErrLimitExceeded) {
		t.Errorf("oversized amount error = %v", err)
	}
	if _, err := Parse("1.00", dollar, euroContext); !errors.Is(err, ErrContextMismatch) {
		t.Errorf("currency-bound context error = %v", err)
	}
	if _, err := FromMinorUnits(integer.New(1), euro, AutomaticContext()); !errors.Is(err, ErrInvalidContext) {
		t.Errorf("automatic minor units error = %v", err)
	}
	if _, err := ParseRate("-1"); !errors.Is(err, ErrInvalidRate) {
		t.Errorf("negative rate error = %v", err)
	}
	if _, err := ParseRate("1000001"); !errors.Is(err, ErrInvalidRate) {
		t.Errorf("excessive rate error = %v", err)
	}
	if _, err := ParseTaxRate("11"); !errors.Is(err, ErrInvalidRate) {
		t.Errorf("excessive tax error = %v", err)
	}
	if _, err := ParseTaxRate("bad"); !errors.Is(err, ErrInvalidRate) {
		t.Errorf("invalid tax error = %v", err)
	}
	if _, err := ParseDiscountRate("bad"); !errors.Is(err, ErrInvalidRate) {
		t.Errorf("invalid discount error = %v", err)
	}

	euros, _ := Parse("1.00", euro, euroContext)
	dollars, _ := Parse("1.00", dollar, dollarContext)
	if _, err := euros.Ratio(context.Background(), dollars); !errors.Is(err, ErrCurrencyMismatch) {
		t.Errorf("cross-currency ratio error = %v", err)
	}
	if _, err := euros.Ratio(context.Background(), Money{}); !errors.Is(err, ErrInvalidMoney) {
		t.Errorf("invalid ratio error = %v", err)
	}
}

func assertAllocationEquals(t *testing.T, allocation AllocationResult, total Money) {
	t.Helper()
	sum, err := allocation.Sum()
	if err != nil {
		t.Fatalf("allocation.Sum() error = %v", err)
	}
	equal, err := sum.Equal(total)
	if err != nil || !equal {
		t.Fatalf("allocation sum = %s, want %s", sum, total)
	}
}

func assertSum(t *testing.T, left, right, total Money) {
	t.Helper()
	sum, err := left.Add(right)
	if err != nil {
		t.Fatalf("sum error = %v", err)
	}
	equal, err := sum.Equal(total)
	if err != nil || !equal {
		t.Fatalf("%s + %s = %s, want %s", left, right, sum, total)
	}
}
