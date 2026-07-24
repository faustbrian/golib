package money

import (
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
)

func TestMoneyBagCombinesOnlyIdenticalCurrencyAndContexts(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	euroContext, _ := DefaultContext(euro)
	dollarContext, _ := DefaultContext(dollar)
	custom, _ := CustomContext(3)
	euroOne, _ := Parse("1.00", euro, euroContext)
	euroTwo, _ := Parse("2.00", euro, euroContext)
	euroPrecise, _ := Parse("3.000", euro, custom)
	dollars, _ := Parse("4.00", dollar, dollarContext)

	original, err := NewMoneyBag(euroOne, dollars)
	if err != nil {
		t.Fatalf("NewMoneyBag() error = %v", err)
	}
	derived, err := original.Add(euroTwo)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	derived, err = derived.Add(euroPrecise)
	if err != nil {
		t.Fatalf("Add(custom) error = %v", err)
	}
	if len(original.Values()) != 2 || len(derived.Values()) != 3 {
		t.Fatalf("bag sizes = %d and %d", len(original.Values()), len(derived.Values()))
	}
	total, ok := derived.Get(euro, euroContext)
	if !ok || total.String() != "3.00 EUR" {
		t.Fatalf("Get(EUR default) = %s, %t", total, ok)
	}
	precise, ok := derived.Get(euro, custom)
	if !ok || precise.String() != "3.000 EUR" {
		t.Fatalf("Get(EUR custom) = %s, %t", precise, ok)
	}
}
