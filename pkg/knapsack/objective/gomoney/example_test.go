package gomoney_test

import (
	"fmt"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/knapsack/objective/gomoney"
	"github.com/faustbrian/golib/pkg/money"
)

func ExampleNew() {
	euro, _ := currency.Parse("EUR")
	moneyContext, _ := money.DefaultContext(euro)
	small, _ := money.Parse("0.60", euro, moneyContext)
	large, _ := money.Parse("1.50", euro, moneyContext)
	costs, _ := gomoney.New(map[string]money.Money{
		"small": small,
		"large": large,
	})

	total, _ := costs.Total(mustPlanForExample("small", "small"))
	fmt.Println(total)

	// Output:
	// 1.20 EUR
}

func ExampleNewWithPolicy() {
	euro, _ := currency.Parse("EUR")
	moneyContext, _ := money.DefaultContext(euro)
	credit, _ := money.Parse("-0.25", euro, moneyContext)
	policy := gomoney.DefaultPolicy()
	policy.AllowNegativeCosts = true
	costs, _ := gomoney.NewWithPolicy(
		map[string]money.Money{"reusable": credit},
		policy,
	)

	total, _ := costs.Total(mustPlanForExample("reusable"))
	fmt.Println(total)

	// Output:
	// -0.25 EUR
}
