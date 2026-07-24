package gomoney_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/objective/gomoney"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
	"github.com/faustbrian/golib/pkg/money"
)

func TestNewRejectsMoreThanDefaultCostTypes(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	moneyContext, _ := money.DefaultContext(euro)
	value, _ := money.Parse("1.00", euro, moneyContext)
	values := make(map[string]money.Money, 1_001)
	for index := range 1_001 {
		values[fmt.Sprintf("box-%04d", index)] = value
	}
	if _, err := gomoney.New(values); !errors.Is(err, gomoney.ErrInvalidCosts) {
		t.Fatalf("cost limit error = %v", err)
	}
	if _, err := gomoney.NewWithLimits(values, gomoney.Limits{MaxTypes: 1_001, MaxIDBytes: 16}); err != nil {
		t.Fatalf("explicit cost limits rejected: %v", err)
	}
	if _, err := gomoney.NewWithLimits(values, gomoney.Limits{}); !errors.Is(err, gomoney.ErrInvalidCosts) {
		t.Fatalf("invalid limits error = %v", err)
	}
	if _, err := gomoney.NewWithLimits(
		map[string]money.Money{"too-long": value},
		gomoney.Limits{MaxTypes: 1, MaxIDBytes: 3},
	); !errors.Is(err, gomoney.ErrInvalidCosts) {
		t.Fatalf("type ID limit error = %v", err)
	}
}

func TestCostObjectiveEntriesRejectNilContext(t *testing.T) {
	euro, _ := currency.Parse("EUR")
	moneyContext, _ := money.DefaultContext(euro)
	value, _ := money.Parse("1.00", euro, moneyContext)
	costs, err := gomoney.New(map[string]money.Money{"box": value})
	if err != nil {
		t.Fatal(err)
	}
	var ctx context.Context
	if _, err := costs.ComparePlans(ctx, knapsack.NormalizedRequest{}, knapsack.Plan{}, knapsack.Plan{}); !errors.Is(err, knapsack.ErrInvalidOptions) {
		t.Fatalf("compare error = %v", err)
	}
	if _, err := costs.Components(ctx, knapsack.NormalizedRequest{}, knapsack.Plan{}); !errors.Is(err, knapsack.ErrInvalidOptions) {
		t.Fatalf("components error = %v", err)
	}
}

func TestCostObjectiveRejectsInvalidAndUnpriceablePlans(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	dollar, _ := currency.Parse("USD")
	euroContext, _ := money.DefaultContext(euro)
	dollarContext, _ := money.DefaultContext(dollar)
	euroCost, _ := money.Parse("1.00", euro, euroContext)
	dollarCost, _ := money.Parse("1.00", dollar, dollarContext)

	for name, values := range map[string]map[string]money.Money{
		"empty":          {},
		"blank type ID":  {" ": euroCost},
		"invalid money":  {"box": {}},
		"mixed currency": {"box": euroCost, "crate": dollarCost},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := gomoney.New(values); !errors.Is(err, gomoney.ErrInvalidCosts) {
				t.Fatalf("New() error = %v, want ErrInvalidCosts", err)
			}
		})
	}

	costs, err := gomoney.New(map[string]money.Money{"box": euroCost})
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := costs.ComparePlans(canceled, knapsack.NormalizedRequest{}, knapsack.Plan{}, knapsack.Plan{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("ComparePlans(canceled) error = %v", err)
	}
	if _, err := costs.Components(canceled, knapsack.NormalizedRequest{}, knapsack.Plan{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Components(canceled) error = %v", err)
	}

	empty := mustPlan(t)
	missing := mustPlan(t, "missing")
	priced := mustPlan(t, "box")
	if _, err := costs.Total(empty); !errors.Is(err, gomoney.ErrInvalidCosts) {
		t.Fatalf("Total(empty) error = %v", err)
	}
	if _, err := costs.Total(missing); !errors.Is(err, gomoney.ErrMissingCost) {
		t.Fatalf("Total(missing) error = %v", err)
	}
	if _, err := costs.Components(context.Background(), knapsack.NormalizedRequest{}, missing); !errors.Is(err, gomoney.ErrMissingCost) {
		t.Fatalf("Components(missing) error = %v", err)
	}
	if _, err := costs.Compare(missing, priced); !errors.Is(err, gomoney.ErrMissingCost) {
		t.Fatalf("Compare(missing left) error = %v", err)
	}
	if _, err := costs.Compare(priced, missing); !errors.Is(err, gomoney.ErrMissingCost) {
		t.Fatalf("Compare(missing right) error = %v", err)
	}

	maximum, _ := money.Parse(strings.Repeat("9", money.MaxAmountDigits), euro, moneyContextZero(t))
	overflowing, err := gomoney.New(map[string]money.Money{"box": maximum})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := overflowing.Total(mustPlan(t, "box", "box")); err == nil {
		t.Fatal("Total(overflowing) error = nil")
	}
}

func mustPlan(t *testing.T, typeIDs ...string) knapsack.Plan {
	t.Helper()
	containers := make([]knapsack.ContainerInstance, len(typeIDs))
	for index, typeID := range typeIDs {
		containers[index] = knapsack.ContainerInstance{ID: fmt.Sprintf("box-%d", index), TypeID: typeID}
	}
	plan, err := knapsack.NewPlan(knapsack.PlanSpec{
		Containers: containers, Status: knapsack.StatusFeasible,
		Termination: knapsack.TerminationCompleted,
	})
	if err != nil {
		t.Fatalf("NewPlan() error = %v", err)
	}
	return plan
}

func moneyContextZero(t *testing.T) money.Context {
	t.Helper()
	monetaryContext, err := money.CustomContext(0)
	if err != nil {
		t.Fatalf("CustomContext(0) error = %v", err)
	}
	return monetaryContext
}

func TestExactPackagingCostComparison(t *testing.T) {
	t.Parallel()
	euro, _ := currency.Parse("EUR")
	moneyContext, _ := money.DefaultContext(euro)
	small, _ := money.Parse("0.60", euro, moneyContext)
	large, _ := money.Parse("1.50", euro, moneyContext)
	costs, err := gomoney.New(map[string]money.Money{"small": small, "large": large})
	if err != nil {
		t.Fatal(err)
	}
	twoSmall, _ := knapsack.NewPlan(knapsack.PlanSpec{Containers: []knapsack.ContainerInstance{{ID: "s1", TypeID: "small"}, {ID: "s2", TypeID: "small"}}, Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted})
	oneLarge, _ := knapsack.NewPlan(knapsack.PlanSpec{Containers: []knapsack.ContainerInstance{{ID: "l1", TypeID: "large"}}, Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted})
	comparison, err := costs.Compare(twoSmall, oneLarge)
	if err != nil {
		t.Fatal(err)
	}
	if comparison >= 0 {
		t.Fatal("exact cheaper multi-box plan was not preferred")
	}
}

func TestExactSolverUsesExactPackagingCost(t *testing.T) {
	t.Parallel()
	euro, _ := currency.Parse("EUR")
	moneyContext, _ := money.DefaultContext(euro)
	smallCost, _ := money.Parse("0.60", euro, moneyContext)
	largeCost, _ := money.Parse("1.50", euro, moneyContext)
	costs, _ := gomoney.New(map[string]money.Money{"small": smallCost, "large": largeCost})
	quantity := func(value int64, unit measurement.Unit) measurement.Quantity {
		return measurement.MustNew(decimal.New(value), unit)
	}
	dimensions := knapsack.PhysicalDimensions{X: quantity(2, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre)}
	items := make([]knapsack.Item, 2)
	for index, id := range []string{"a", "b"} {
		items[index], _ = knapsack.NewItem(knapsack.ItemSpec{ID: id, Dimensions: dimensions, Weight: quantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	}
	small, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "small", InternalDimensions: dimensions, MaxContentWeight: quantity(2, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	large, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "large", InternalDimensions: knapsack.PhysicalDimensions{X: quantity(4, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre)}, MaxContentWeight: quantity(2, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	request, err := knapsack.NewRequest(items, []knapsack.ContainerType{small, large}, knapsack.Resolution{Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (solver.Exact{}).PackAll(context.Background(), request.Normalized(), solver.Options{PlanObjective: costs})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Containers()) != 2 || plan.Containers()[0].TypeID != "small" || plan.Containers()[1].TypeID != "small" {
		t.Fatalf("containers = %+v", plan.Containers())
	}
	if result := verify.Plan(request.Normalized(), plan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("invalid plan: %+v", result.Violations())
	}
	if got := plan.Objective(); len(got) != 1 || got[0].Value != "1.20" || got[0].Unit != "EUR" {
		t.Fatalf("objective = %+v", got)
	}
}
