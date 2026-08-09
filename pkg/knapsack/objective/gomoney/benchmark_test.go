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

func BenchmarkComparisonOverhead(b *testing.B) {
	euro, err := currency.Parse("EUR")
	if err != nil {
		b.Fatal(err)
	}
	moneyContext, err := money.DefaultContext(euro)
	if err != nil {
		b.Fatal(err)
	}
	small, err := money.Parse("0.60", euro, moneyContext)
	if err != nil {
		b.Fatal(err)
	}
	large, err := money.Parse("1.50", euro, moneyContext)
	if err != nil {
		b.Fatal(err)
	}
	costs, err := gomoney.New(map[string]money.Money{"small": small, "large": large})
	if err != nil {
		b.Fatal(err)
	}
	left := benchmarkPlan(b, "small", "small")
	right := benchmarkPlan(b, "large")

	b.Run("objective", func(b *testing.B) {
		var comparison int
		b.ReportAllocs()
		for b.Loop() {
			comparison, err = costs.Compare(left, right)
			if err != nil {
				b.Fatal(err)
			}
		}
		if comparison >= 0 {
			b.Fatal("unexpected objective ordering")
		}
	})

	b.Run("direct_money", func(b *testing.B) {
		leftTotal, addErr := small.Add(small)
		if addErr != nil {
			b.Fatal(addErr)
		}
		var comparison int
		b.ReportAllocs()
		for b.Loop() {
			comparison, err = leftTotal.Compare(large)
			if err != nil {
				b.Fatal(err)
			}
		}
		if comparison >= 0 {
			b.Fatal("unexpected direct ordering")
		}
	})
}

func BenchmarkLookupOverhead(b *testing.B) {
	one := mustEuro(b, "1.25")
	zero, err := one.Sub(one)
	if err != nil {
		b.Fatal(err)
	}
	costs, err := gomoney.New(map[string]money.Money{"box": one})
	if err != nil {
		b.Fatal(err)
	}
	plan := benchmarkPlan(b, "box")

	b.Run("objective", func(b *testing.B) {
		var total money.Money
		b.ReportAllocs()
		for b.Loop() {
			total, err = costs.Total(plan)
			if err != nil {
				b.Fatal(err)
			}
		}
		if !total.Valid() {
			b.Fatal("unexpected invalid total")
		}
	})

	b.Run("direct_money", func(b *testing.B) {
		var total money.Money
		b.ReportAllocs()
		for b.Loop() {
			total, err = zero.Add(one)
			if err != nil {
				b.Fatal(err)
			}
		}
		if !total.Valid() {
			b.Fatal("unexpected invalid total")
		}
	})
}

func benchmarkPlan(b *testing.B, typeIDs ...string) knapsack.Plan {
	b.Helper()
	containers := make([]knapsack.ContainerInstance, len(typeIDs))
	for index, typeID := range typeIDs {
		containers[index] = knapsack.ContainerInstance{ID: fmt.Sprintf("box-%d", index), TypeID: typeID}
	}
	plan, err := knapsack.NewPlan(knapsack.PlanSpec{
		Containers: containers, Status: knapsack.StatusFeasible,
		Termination: knapsack.TerminationCompleted,
	})
	if err != nil {
		b.Fatal(err)
	}
	return plan
}
