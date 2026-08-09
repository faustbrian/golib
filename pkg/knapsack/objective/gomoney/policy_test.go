package gomoney_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/knapsack/objective/gomoney"
	"github.com/faustbrian/golib/pkg/money"
)

func TestCostsRejectNegativeValuesUnlessPolicyAllowsThem(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	moneyContext, _ := money.DefaultContext(euro)
	negative, _ := money.Parse("-1.25", euro, moneyContext)

	if _, err := gomoney.New(map[string]money.Money{"credit": negative}); !errors.Is(err, gomoney.ErrInvalidCosts) || !errors.Is(err, gomoney.ErrNegativeCost) {
		t.Fatalf("New(negative) error = %v", err)
	}

	policy := gomoney.DefaultPolicy()
	policy.AllowNegativeCosts = true
	costs, err := gomoney.NewWithPolicy(
		map[string]money.Money{"credit": negative},
		policy,
	)
	if err != nil {
		t.Fatalf("NewWithPolicy(negative) error = %v", err)
	}

	total, err := costs.Total(mustPlan(t, "credit"))
	if err != nil {
		t.Fatalf("Total(credit) error = %v", err)
	}
	if total.String() != "-1.25 EUR" {
		t.Fatalf("Total(credit) = %s", total.String())
	}
}

func TestEntryConstructionRejectsDuplicatesAndCopiesInput(t *testing.T) {
	t.Parallel()

	one := mustEuro(t, "1.00")
	two := mustEuro(t, "2.00")
	duplicate := []gomoney.Entry{{TypeID: "box", Cost: one}, {TypeID: "box", Cost: two}}
	if _, err := gomoney.NewFromEntries(duplicate, gomoney.DefaultPolicy()); !errors.Is(err, gomoney.ErrInvalidCosts) || !errors.Is(err, gomoney.ErrDuplicateTypeID) {
		t.Fatalf("NewFromEntries(duplicate) error = %v", err)
	}

	entries := []gomoney.Entry{{TypeID: "box", Cost: one}, {TypeID: "crate", Cost: two}}
	costs, err := gomoney.NewFromEntries(entries, gomoney.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	entries[0] = gomoney.Entry{TypeID: "missing", Cost: two}

	total, err := costs.Total(mustPlan(t, "box"))
	if err != nil {
		t.Fatalf("Total(box) after input mutation error = %v", err)
	}
	if total.String() != "1.00 EUR" {
		t.Fatalf("Total(box) after input mutation = %s", total.String())
	}

	mapping := map[string]money.Money{"box": one, "crate": two}
	mappedCosts, err := gomoney.New(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping["box"] = two
	delete(mapping, "crate")
	mappedTotal, err := mappedCosts.Total(mustPlan(t, "box", "crate"))
	if err != nil {
		t.Fatalf("Total(mapped costs) after input mutation error = %v", err)
	}
	if mappedTotal.String() != "3.00 EUR" {
		t.Fatalf("Total(mapped costs) after input mutation = %s", mappedTotal.String())
	}
}

func TestEntryConstructionEnforcesEveryCollectionBoundary(t *testing.T) {
	t.Parallel()

	zero := mustEuro(t, "0.00")
	one := mustEuro(t, "1.00")
	entries := []gomoney.Entry{{TypeID: "box", Cost: zero}, {TypeID: "crate", Cost: one}}

	for name, test := range map[string]struct {
		entries []gomoney.Entry
		limits  gomoney.Limits
	}{
		"empty entries":   {nil, gomoney.Limits{MaxTypes: 2, MaxIDBytes: 8}},
		"zero type limit": {entries[:1], gomoney.Limits{MaxIDBytes: 8}},
		"zero ID limit":   {entries[:1], gomoney.Limits{MaxTypes: 2}},
		"excess entries":  {entries, gomoney.Limits{MaxTypes: 1, MaxIDBytes: 8}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := gomoney.NewFromEntries(test.entries, gomoney.Policy{Limits: test.limits}); !errors.Is(err, gomoney.ErrInvalidCosts) {
				t.Fatalf("NewFromEntries() error = %v", err)
			}
		})
	}

	if _, err := gomoney.NewFromEntries(entries, gomoney.Policy{
		Limits: gomoney.Limits{MaxTypes: 2, MaxIDBytes: 5},
	}); err != nil {
		t.Fatalf("exact entry and ID limits rejected: %v", err)
	}
	if _, err := gomoney.New(map[string]money.Money{"free": zero}); err != nil {
		t.Fatalf("zero cost rejected: %v", err)
	}
}

func TestCostsRequireOneSupportedExactCurrencyContext(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	euroDefault, _ := money.DefaultContext(euro)
	dollarDefault, _ := money.DefaultContext(dollar)
	customTwo, _ := money.CustomContext(2)
	customThree, _ := money.CustomContext(3)
	cash, _ := money.CashContext(2, 5)
	automatic := money.AutomaticContext()

	euroCost, _ := money.Parse("1.00", euro, euroDefault)
	dollarCost, _ := money.Parse("1.00", dollar, dollarDefault)
	customCost, _ := money.Parse("1.00", euro, customTwo)
	customScaleCost, _ := money.Parse("1.000", euro, customThree)
	cashCost, _ := money.Parse("1.00", euro, cash)
	automaticCost, _ := money.Parse("1.00", euro, automatic)

	for name, test := range map[string]struct {
		values map[string]money.Money
		cause  error
	}{
		"mixed currencies":        {map[string]money.Money{"eur": euroCost, "usd": dollarCost}, money.ErrCurrencyMismatch},
		"mixed context kinds":     {map[string]money.Money{"default": euroCost, "custom": customCost}, money.ErrContextMismatch},
		"mixed scales":            {map[string]money.Money{"two": customCost, "three": customScaleCost}, money.ErrContextMismatch},
		"cash rounding context":   {map[string]money.Money{"cash": cashCost}, gomoney.ErrUnsupportedScale},
		"automatic scale context": {map[string]money.Money{"automatic": automaticCost}, gomoney.ErrUnsupportedScale},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := gomoney.New(test.values)
			if !errors.Is(err, gomoney.ErrInvalidCosts) || !errors.Is(err, test.cause) {
				t.Fatalf("New() error = %v, want invalid costs and %v", err, test.cause)
			}
		})
	}

	if _, err := gomoney.New(map[string]money.Money{"custom": customCost}); err != nil {
		t.Fatalf("New(fixed custom scale) error = %v", err)
	}
	if _, err := gomoney.New(map[string]money.Money{"invalid": {}}); !errors.Is(err, gomoney.ErrInvalidCosts) || !errors.Is(err, money.ErrInvalidMoney) {
		t.Fatalf("New(invalid money) error = %v", err)
	}
}

func TestValidationCauseIsIndependentOfEntryOrder(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	defaultContext, _ := money.DefaultContext(euro)
	automaticContext := money.AutomaticContext()
	negative, _ := money.Parse("-1.00", euro, defaultContext)
	automatic, _ := money.Parse("1.00", euro, automaticContext)
	forward := []gomoney.Entry{
		{TypeID: "z-negative", Cost: negative},
		{TypeID: "a-automatic", Cost: automatic},
	}
	reverse := []gomoney.Entry{forward[1], forward[0]}

	for _, entries := range [][]gomoney.Entry{forward, reverse} {
		_, err := gomoney.NewFromEntries(entries, gomoney.DefaultPolicy())
		if !errors.Is(err, gomoney.ErrUnsupportedScale) || errors.Is(err, gomoney.ErrNegativeCost) {
			t.Fatalf("NewFromEntries() error = %v, want only canonical first cause", err)
		}
	}
}

func TestEmptyPlanHasExactZeroTotal(t *testing.T) {
	t.Parallel()

	one := mustEuro(t, "1.00")
	costs, err := gomoney.New(map[string]money.Money{"box": one})
	if err != nil {
		t.Fatal(err)
	}
	total, err := costs.Total(mustPlan(t))
	if err != nil {
		t.Fatalf("Total(empty) error = %v", err)
	}
	if total.String() != "0.00 EUR" || total.Context() != one.Context() {
		t.Fatalf("Total(empty) = %s with context %#v", total.String(), total.Context())
	}
}

func TestTotalIsExactAcrossOrderSensitiveIntermediateMagnitudes(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	maximum, _ := money.Parse(strings.Repeat("9", money.MaxAmountDigits), euro, moneyContextZero(t))
	negative, err := maximum.Neg()
	if err != nil {
		t.Fatal(err)
	}
	costs, err := gomoney.NewWithPolicy(
		map[string]money.Money{"positive": maximum, "negative": negative},
		gomoney.Policy{Limits: gomoney.DefaultLimits(), AllowNegativeCosts: true},
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, plan := range []struct {
		name    string
		typeIDs []string
	}{
		{"positive first", []string{"positive", "positive", "negative"}},
		{"negative first", []string{"negative", "positive", "positive"}},
	} {
		t.Run(plan.name, func(t *testing.T) {
			total, totalErr := costs.Total(mustPlan(t, plan.typeIDs...))
			if totalErr != nil {
				t.Fatalf("Total() error = %v", totalErr)
			}
			equal, equalErr := total.Equal(maximum)
			if equalErr != nil || !equal {
				t.Fatalf("Total() = %s, want %s (%v)", total, maximum, equalErr)
			}
		})
	}
}

func mustEuro(t *testing.T, amount string) money.Money {
	t.Helper()
	code, err := currency.Parse("EUR")
	if err != nil {
		t.Fatal(err)
	}
	monetaryContext, err := money.DefaultContext(code)
	if err != nil {
		t.Fatal(err)
	}
	value, err := money.Parse(amount, code, monetaryContext)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
