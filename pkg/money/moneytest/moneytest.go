// Package moneytest provides currency fixtures and conservation assertions for
// consumers of money.
package moneytest

import (
	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/money"
)

// TestingT is the subset of testing.TB used by assertions.
type TestingT interface {
	Helper()
	Fatalf(format string, arguments ...any)
}

// CurrencyFixture represents an authoritative currency edge and an explicit
// usable monetary context.
type CurrencyFixture struct {
	Code    currency.Code
	Context money.Context
}

// CurrencyFixtures returns independent fixtures for ordinary, zero-unit,
// three-unit, and historic currencies.
func CurrencyFixtures() []CurrencyFixture {
	euro, _ := currency.Parse("EUR")
	yen, _ := currency.Parse("JPY")
	dinar, _ := currency.Parse("BHD")
	markka, _ := currency.ParseWithOptions("FIM", currency.ParseOptions{AllowHistoric: true})
	euroContext, _ := money.DefaultContext(euro)
	yenContext, _ := money.DefaultContext(yen)
	dinarContext, _ := money.DefaultContext(dinar)
	historicContext, _ := money.CustomContext(2)

	return []CurrencyFixture{
		{Code: euro, Context: euroContext},
		{Code: yen, Context: yenContext},
		{Code: dinar, Context: dinarContext},
		{Code: markka, Context: historicContext},
	}
}

// MustParse constructs Money or fails the current test.
func MustParse(t TestingT, input string, code currency.Code, context money.Context) money.Money {
	t.Helper()
	value, err := money.Parse(input, code, context)
	if err != nil {
		t.Fatalf("money.Parse(%q, %s) error = %v", input, code, err)
	}

	return value
}

// AssertEqual fails unless two values have identical currency, context, and
// numeric value.
func AssertEqual(t TestingT, got, want money.Money) {
	t.Helper()
	equal, err := got.Equal(want)
	if err != nil || !equal {
		t.Fatalf("money values differ: got %s, want %s, error %v", got, want, err)
	}
}

// AssertAllocationConserved fails unless all parts exactly sum to total.
func AssertAllocationConserved(t TestingT, total money.Money, allocation money.AllocationResult) {
	t.Helper()
	sum, err := allocation.Sum()
	if err != nil {
		t.Fatalf("allocation sum error = %v", err)
	}
	AssertEqual(t, sum, total)
}

// AssertTaxConserved fails unless net plus tax equals gross.
func AssertTaxConserved(t TestingT, result money.TaxResult) {
	t.Helper()
	total, err := result.Net().Add(result.Tax())
	if err != nil {
		t.Fatalf("tax sum error = %v", err)
	}
	AssertEqual(t, total, result.Gross())
}

// AssertDiscountConserved fails unless final plus discount equals original.
func AssertDiscountConserved(t TestingT, result money.DiscountResult) {
	t.Helper()
	total, err := result.Final().Add(result.Discount())
	if err != nil {
		t.Fatalf("discount sum error = %v", err)
	}
	AssertEqual(t, total, result.Original())
}
