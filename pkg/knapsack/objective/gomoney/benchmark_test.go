package gomoney_test

import (
	"fmt"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/objective/gomoney"
	"github.com/faustbrian/golib/pkg/money"
)

func BenchmarkTotalSixtyFourContainers(b *testing.B) {
	euro, err := currency.Parse("EUR")
	if err != nil {
		b.Fatal(err)
	}
	moneyContext, err := money.DefaultContext(euro)
	if err != nil {
		b.Fatal(err)
	}
	unitCost, err := money.Parse("1.25", euro, moneyContext)
	if err != nil {
		b.Fatal(err)
	}
	costs, err := gomoney.New(map[string]money.Money{"box": unitCost})
	if err != nil {
		b.Fatal(err)
	}
	containers := make([]knapsack.ContainerInstance, 64)
	for index := range containers {
		containers[index] = knapsack.ContainerInstance{
			ID:     fmt.Sprintf("box-%d", index),
			TypeID: "box",
		}
	}
	plan, err := knapsack.NewPlan(knapsack.PlanSpec{
		Containers:  containers,
		Status:      knapsack.StatusFeasible,
		Termination: knapsack.TerminationCompleted,
	})
	if err != nil {
		b.Fatal(err)
	}

	var total money.Money
	b.ReportAllocs()
	for b.Loop() {
		total, err = costs.Total(plan)
		if err != nil {
			b.Fatal(err)
		}
	}
	if !total.Valid() {
		b.Fatal("benchmark produced invalid total")
	}
}
