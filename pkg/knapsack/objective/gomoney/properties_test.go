package gomoney_test

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/knapsack/objective/gomoney"
	"github.com/faustbrian/golib/pkg/money"
)

func TestTotalsAndOrderingMatchDirectExactMoneyArithmetic(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	moneyContext, _ := money.CustomContext(2)
	zero, _ := money.Parse("0", euro, moneyContext)
	random := rand.New(rand.NewPCG(0x6d6f6e6579, 0x6b6e61707361636b))

	for iteration := range 100 {
		entries := make([]gomoney.Entry, 4)
		directCosts := make(map[string]money.Money, len(entries))
		for index := range entries {
			typeID := fmt.Sprintf("type-%d", index)
			minorUnits := random.Int64N(10_000)
			cost, err := money.Parse(
				fmt.Sprintf("%d.%02d", minorUnits/100, minorUnits%100),
				euro,
				moneyContext,
			)
			if err != nil {
				t.Fatal(err)
			}
			entries[index] = gomoney.Entry{TypeID: typeID, Cost: cost}
			directCosts[typeID] = cost
		}

		forward := make(map[string]money.Money, len(entries))
		reverse := make(map[string]money.Money, len(entries))
		for index, entry := range entries {
			forward[entry.TypeID] = entry.Cost
			reversed := entries[len(entries)-1-index]
			reverse[reversed.TypeID] = reversed.Cost
		}
		forwardCosts, err := gomoney.New(forward)
		if err != nil {
			t.Fatal(err)
		}
		reverseCosts, err := gomoney.New(reverse)
		if err != nil {
			t.Fatal(err)
		}

		leftIDs := randomTypeIDs(random, iteration%7, len(entries))
		rightIDs := randomTypeIDs(random, (iteration*3)%7, len(entries))
		left := mustPlan(t, leftIDs...)
		right := mustPlan(t, rightIDs...)

		leftWant := directTotal(t, zero, directCosts, leftIDs)
		leftForward, err := forwardCosts.Total(left)
		if err != nil {
			t.Fatal(err)
		}
		leftReverse, err := reverseCosts.Total(left)
		if err != nil {
			t.Fatal(err)
		}
		if equal, _ := leftForward.Equal(leftWant); !equal {
			t.Fatalf("iteration %d total = %s, want %s", iteration, leftForward, leftWant)
		}
		if equal, _ := leftReverse.Equal(leftForward); !equal {
			t.Fatalf("iteration %d map order changed total: %s != %s", iteration, leftReverse, leftForward)
		}

		rightWant := directTotal(t, zero, directCosts, rightIDs)
		want, err := leftWant.Compare(rightWant)
		if err != nil {
			t.Fatal(err)
		}
		if want == 0 {
			want = strings.Compare(left.CanonicalString(), right.CanonicalString())
		}
		got, err := forwardCosts.Compare(left, right)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("iteration %d comparison = %d, want %d", iteration, got, want)
		}
	}
}

func TestCostsAreSafeForConcurrentSolverUse(t *testing.T) {
	t.Parallel()

	one := mustEuro(t, "1.00")
	costs, err := gomoney.New(map[string]money.Money{"box": one})
	if err != nil {
		t.Fatal(err)
	}
	plan := mustPlan(t, "box", "box")

	const workers = 16
	errorsFound := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Go(func() {
			for range 100 {
				total, totalErr := costs.Total(plan)
				if totalErr != nil {
					errorsFound <- totalErr
					return
				}
				if total.String() != "2.00 EUR" {
					errorsFound <- fmt.Errorf("concurrent total = %s", total.String())
					return
				}
			}
		})
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Error(err)
	}
}

func randomTypeIDs(random *rand.Rand, count, typeCount int) []string {
	result := make([]string, count)
	for index := range result {
		result[index] = fmt.Sprintf("type-%d", random.IntN(typeCount))
	}
	return result
}

func directTotal(t *testing.T, zero money.Money, costs map[string]money.Money, typeIDs []string) money.Money {
	t.Helper()
	total := zero
	for _, typeID := range typeIDs {
		var err error
		total, err = total.Add(costs[typeID])
		if err != nil {
			t.Fatal(err)
		}
	}
	return total
}
