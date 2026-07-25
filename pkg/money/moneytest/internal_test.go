package moneytest

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/money"
)

func TestAssertionsReportEveryFailedLaw(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := money.DefaultContext(euro)
	one, _ := money.Parse("1.00", euro, monetaryContext)
	two, _ := money.Parse("2.00", euro, monetaryContext)
	taxRate, _ := money.ParseTaxRate("0.24")
	discountRate, _ := money.ParseDiscountRate("0.10")
	tax, _ := money.AddTax(context.Background(), one, taxRate, gomath.RoundHalfEven)
	discount, _ := money.ApplyDiscount(context.Background(), one, discountRate, gomath.RoundHalfEven)

	AssertTaxConserved(t, tax)
	AssertDiscountConserved(t, discount)
	assertFails(t, func(fake TestingT) { MustParse(fake, "bad", euro, monetaryContext) })
	assertFails(t, func(fake TestingT) { AssertEqual(fake, one, two) })
	assertFails(t, func(fake TestingT) { AssertAllocationConserved(fake, one, money.AllocationResult{}) })
	assertFails(t, func(fake TestingT) { AssertTaxConserved(fake, money.TaxResult{}) })
	assertFails(t, func(fake TestingT) { AssertDiscountConserved(fake, money.DiscountResult{}) })
}

func assertFails(t *testing.T, assertion func(TestingT)) {
	t.Helper()
	deferred := false
	func() {
		defer func() {
			deferred = recover() != nil
		}()
		assertion(panicTestingT{})
	}()
	if !deferred {
		t.Fatal("assertion did not report failure")
	}
}

type panicTestingT struct{}

func (panicTestingT) Helper() {}
func (panicTestingT) Fatalf(string, ...any) {
	panic("fatal assertion")
}
