package money_test

import (
	"context"
	"fmt"
	"time"

	"github.com/faustbrian/golib/pkg/international/currency"
	gomath "github.com/faustbrian/golib/pkg/math"
	"github.com/faustbrian/golib/pkg/math/integer"
	"github.com/faustbrian/golib/pkg/money"
)

func ExampleMoney_Add() {
	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := money.DefaultContext(euro)
	left, _ := money.Parse("12.30", euro, monetaryContext)
	right, _ := money.Parse("0.45", euro, monetaryContext)
	total, _ := left.Add(right)

	fmt.Println(total)
	// Output: 12.75 EUR
}

func ExampleMoney_Allocate() {
	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := money.DefaultContext(euro)
	total, _ := money.Parse("10.00", euro, monetaryContext)
	result, _ := total.Allocate(context.Background(), []integer.Integer{
		integer.New(1), integer.New(2), integer.New(3),
	})

	for _, part := range result.Parts() {
		fmt.Println(part)
	}
	// Output:
	// 1.67 EUR
	// 3.33 EUR
	// 5.00 EUR
}

func ExampleConvert() {
	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	euroContext, _ := money.DefaultContext(euro)
	dollarContext, _ := money.DefaultContext(dollar)
	value, _ := money.Parse("10.00", euro, euroContext)
	exact, _ := money.ParseRate("1.1")
	rate, _ := money.NewExchangeRate(
		euro,
		dollar,
		exact,
		time.Date(2026, 7, 19, 6, 0, 0, 0, time.UTC),
		"central-bank-daily",
	)
	result, _ := money.Convert(
		context.Background(),
		value,
		rate,
		dollarContext,
		gomath.RoundHalfEven,
	)

	fmt.Println(result.Converted())
	// Output: 11.00 USD
}
