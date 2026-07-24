package solver_test

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/objective"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

type preferMoreContainers struct{}

func (preferMoreContainers) Valid() bool { return true }

func (preferMoreContainers) ComparePlans(_ context.Context, _ knapsack.NormalizedRequest, left, right knapsack.Plan) (int, error) {
	leftCount, rightCount := len(left.Containers()), len(right.Containers())
	if leftCount > rightCount {
		return -1, nil
	}
	if leftCount < rightCount {
		return 1, nil
	}
	return 0, nil
}

func (preferMoreContainers) Components(_ context.Context, _ knapsack.NormalizedRequest, plan knapsack.Plan) ([]knapsack.ScoreComponent, error) {
	return []knapsack.ScoreComponent{{Name: "test_more_containers", Direction: "max", Unit: "count", Value: strconv.Itoa(len(plan.Containers()))}}, nil
}

type panickingObjective struct{ preferMoreContainers }

func (panickingObjective) ComparePlans(context.Context, knapsack.NormalizedRequest, knapsack.Plan, knapsack.Plan) (int, error) {
	panic("objective failure")
}

func exactQuantity(value int64, unit measurement.Unit) measurement.Quantity {
	return measurement.MustNew(decimal.New(value), unit)
}

func exactRequest(t *testing.T, boxX int64, stock uint32) knapsack.NormalizedRequest {
	t.Helper()
	itemDims := knapsack.PhysicalDimensions{X: exactQuantity(2, measurement.Metre), Y: exactQuantity(1, measurement.Metre), Z: exactQuantity(1, measurement.Metre)}
	items := make([]knapsack.Item, 2)
	for index, id := range []string{"b", "a"} {
		items[index], _ = knapsack.NewItem(knapsack.ItemSpec{ID: id, Dimensions: itemDims, Weight: exactQuantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ, geometry.OrientationYXZ}})
	}
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: knapsack.PhysicalDimensions{X: exactQuantity(boxX, measurement.Metre), Y: exactQuantity(1, measurement.Metre), Z: exactQuantity(1, measurement.Metre)}, MaxContentWeight: exactQuantity(2, measurement.Kilogram), Stock: knapsack.FiniteStock(stock)})
	limits := knapsack.DefaultLimits()
	limits.MaxSearchNodes = 10_000
	request, err := knapsack.NewRequest(items, []knapsack.ContainerType{box}, knapsack.Resolution{Length: exactQuantity(1, measurement.Metre), Mass: exactQuantity(1, measurement.Kilogram)}, limits)
	if err != nil {
		t.Fatal(err)
	}
	return request.Normalized()
}

func TestExactProvesOptimalFixedPacking(t *testing.T) {
	t.Parallel()
	request := exactRequest(t, 4, 1)
	plan, err := solver.Exact{}.PackFixed(context.Background(), request, []knapsack.ContainerInstance{{ID: "box#000001", TypeID: "box"}}, solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status() != knapsack.StatusOptimal {
		t.Fatalf("status = %s", plan.Status())
	}
	if result := verify.Plan(request, plan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("invalid plan: %+v", result.Violations())
	}
}

func TestExactProvesFixedContainersInfeasible(t *testing.T) {
	t.Parallel()
	request := exactRequest(t, 2, 1)
	plan, err := solver.Exact{}.PackFixed(context.Background(), request, []knapsack.ContainerInstance{{ID: "box#000001", TypeID: "box"}}, solver.Options{})
	if !errors.Is(err, knapsack.ErrProvenInfeasible) {
		t.Fatalf("error = %v", err)
	}
	if plan.Status() != knapsack.StatusInfeasible {
		t.Fatalf("status = %s", plan.Status())
	}
}

func TestExactProvesFiniteStockPackAllInfeasible(t *testing.T) {
	t.Parallel()
	request := exactRequest(t, 2, 1)
	plan, err := (solver.Exact{}).PackAll(context.Background(), request, solver.Options{})
	if !errors.Is(err, knapsack.ErrProvenInfeasible) || plan.Status() != knapsack.StatusInfeasible {
		t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
	}
}

func TestExactReportsNodeBudgetWithoutFalseProof(t *testing.T) {
	t.Parallel()
	request := exactRequest(t, 4, 1)
	limits := request.Limits()
	limits.MaxSearchNodes = 1
	request = request.WithLimits(limits)
	plan, err := solver.Exact{}.PackFixed(context.Background(), request, []knapsack.ContainerInstance{{ID: "box#000001", TypeID: "box"}}, solver.Options{})
	if !errors.Is(err, knapsack.ErrBudgetExhausted) || plan.Status() == knapsack.StatusInfeasible || plan.Status() == knapsack.StatusOptimal {
		t.Fatalf("status = %s, error = %v", plan.Status(), err)
	}
}

func TestExactPackAllAppliesNodeBudgetAcrossConfigurations(t *testing.T) {
	t.Parallel()
	dimensions := knapsack.PhysicalDimensions{X: exactQuantity(1, measurement.Metre), Y: exactQuantity(1, measurement.Metre), Z: exactQuantity(1, measurement.Metre)}
	item, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "item", Dimensions: dimensions, Weight: exactQuantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	containers := make([]knapsack.ContainerType, 2)
	for index, id := range []string{"a", "b"} {
		containers[index], _ = knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: id, InternalDimensions: dimensions, MaxContentWeight: exactQuantity(1, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	}
	limits := knapsack.DefaultLimits()
	limits.MaxSearchNodes = 2
	request, _ := knapsack.NewRequest([]knapsack.Item{item}, containers, knapsack.Resolution{Length: exactQuantity(1, measurement.Metre), Mass: exactQuantity(1, measurement.Kilogram)}, limits)
	plan, err := (solver.Exact{}).PackAll(context.Background(), request.Normalized(), solver.Options{})
	if !errors.Is(err, knapsack.ErrBudgetExhausted) || plan.Status() != knapsack.StatusBudgetExhausted {
		t.Fatalf("status=%s error=%v work=%+v", plan.Status(), err, plan.Work())
	}
}

func TestExactPackAllMinimizesContainerCount(t *testing.T) {
	t.Parallel()
	itemDims := knapsack.PhysicalDimensions{X: exactQuantity(2, measurement.Metre), Y: exactQuantity(1, measurement.Metre), Z: exactQuantity(1, measurement.Metre)}
	items := make([]knapsack.Item, 2)
	for index, id := range []string{"a", "b"} {
		items[index], _ = knapsack.NewItem(knapsack.ItemSpec{ID: id, Dimensions: itemDims, Weight: exactQuantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	}
	small, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "small", InternalDimensions: itemDims, MaxContentWeight: exactQuantity(2, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	large, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "large", InternalDimensions: knapsack.PhysicalDimensions{X: exactQuantity(4, measurement.Metre), Y: exactQuantity(1, measurement.Metre), Z: exactQuantity(1, measurement.Metre)}, MaxContentWeight: exactQuantity(2, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	request, _ := knapsack.NewRequest(items, []knapsack.ContainerType{small, large}, knapsack.Resolution{Length: exactQuantity(1, measurement.Metre), Mass: exactQuantity(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	plan, err := (solver.Exact{}).PackAll(context.Background(), request.Normalized(), solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status() != knapsack.StatusOptimal || len(plan.Containers()) != 1 || plan.Containers()[0].TypeID != "large" {
		t.Fatalf("status=%s containers=%+v", plan.Status(), plan.Containers())
	}
}

func TestExactPackAllHonorsExplicitPrimaryObjective(t *testing.T) {
	t.Parallel()
	itemDims := knapsack.PhysicalDimensions{X: exactQuantity(1, measurement.Metre), Y: exactQuantity(1, measurement.Metre), Z: exactQuantity(1, measurement.Metre)}
	item, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "item", Dimensions: itemDims, Weight: exactQuantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	small, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "z-small", InternalDimensions: itemDims, MaxContentWeight: exactQuantity(1, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	large, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "a-large", InternalDimensions: knapsack.PhysicalDimensions{X: exactQuantity(2, measurement.Metre), Y: exactQuantity(1, measurement.Metre), Z: exactQuantity(1, measurement.Metre)}, MaxContentWeight: exactQuantity(1, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	request, _ := knapsack.NewRequest([]knapsack.Item{item}, []knapsack.ContainerType{large, small}, knapsack.Resolution{Length: exactQuantity(1, measurement.Metre), Mass: exactQuantity(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	goal, _ := objective.New(objective.Minimize(objective.UnusedVolume), objective.Minimize(objective.ContainerCount))
	plan, err := (solver.Exact{}).PackAll(context.Background(), request.Normalized(), solver.Options{Objective: goal})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Containers()[0].TypeID != "z-small" {
		t.Fatalf("containers = %+v", plan.Containers())
	}
	if got := plan.Objective(); len(got) != 2 || got[0].Name != string(objective.UnusedVolume) {
		t.Fatalf("objective = %+v", got)
	}
}

func TestExactPackAllSupportsPanicSafePlanObjectives(t *testing.T) {
	t.Parallel()
	request := exactRequest(t, 4, 2)
	plan, err := (solver.Exact{}).PackAll(context.Background(), request, solver.Options{PlanObjective: preferMoreContainers{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Containers()) != 2 || len(plan.Objective()) != 1 {
		t.Fatalf("plan = %s", plan.CanonicalString())
	}
	failed, err := (solver.Exact{}).PackAll(context.Background(), request, solver.Options{PlanObjective: panickingObjective{}})
	if !errors.Is(err, objective.ErrCallbackPanic) {
		t.Fatalf("panic error = %v", err)
	}
	if failed.Status() != "" || len(failed.Placements()) != 0 {
		t.Fatalf("objective failure returned a proof-bearing plan: %s", failed.CanonicalString())
	}
}

func TestExactReportsBranchAndCandidateBudgetsPrecisely(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		mutate      func(*knapsack.Limits)
		termination knapsack.TerminationReason
	}{
		{"branches", func(l *knapsack.Limits) { l.MaxBranches = 1 }, knapsack.TerminationBranchLimit},
		{"candidates", func(l *knapsack.Limits) { l.MaxCandidatePlacements = 1 }, knapsack.TerminationCandidateLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := exactRequest(t, 4, 1)
			limits := request.Limits()
			test.mutate(&limits)
			request = request.WithLimits(limits)
			plan, err := (solver.Exact{}).PackFixed(context.Background(), request, []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}, solver.Options{})
			if !errors.Is(err, knapsack.ErrBudgetExhausted) || plan.Termination() != test.termination || plan.Status() != knapsack.StatusBudgetExhausted {
				t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
			}
		})
	}
}

func TestExactPackAllReportsBranchAndCandidateBudgetsPrecisely(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		mutate      func(*knapsack.Limits)
		termination knapsack.TerminationReason
	}{
		{"branches", func(l *knapsack.Limits) { l.MaxBranches = 1 }, knapsack.TerminationBranchLimit},
		{"candidates", func(l *knapsack.Limits) { l.MaxCandidatePlacements = 1 }, knapsack.TerminationCandidateLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := exactRequest(t, 4, 1)
			limits := request.Limits()
			test.mutate(&limits)
			request = request.WithLimits(limits)
			plan, err := (solver.Exact{}).PackAll(context.Background(), request, solver.Options{})
			if !errors.Is(err, knapsack.ErrBudgetExhausted) || plan.Termination() != test.termination || plan.Status() != knapsack.StatusBudgetExhausted {
				t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
			}
		})
	}
}

func TestSolverEntryPointsRejectInvalidContextsOptionsAndInstances(t *testing.T) {
	t.Parallel()
	request := exactRequest(t, 4, 1)
	instance := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, solve := range map[string]func() error{
		"exact all cancellation": func() error { _, err := (solver.Exact{}).PackAll(cancelled, request, solver.Options{}); return err },
		"exact fixed cancellation": func() error {
			_, err := (solver.Exact{}).PackFixed(cancelled, request, instance, solver.Options{})
			return err
		},
		"heuristic all cancellation": func() error { _, err := (solver.Heuristic{}).PackAll(cancelled, request, solver.Options{}); return err },
		"heuristic fixed cancellation": func() error {
			_, err := (solver.Heuristic{}).PackFixed(cancelled, request, instance, solver.Options{})
			return err
		},
	} {
		if err := solve(); !errors.Is(err, context.Canceled) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	goal, _ := objective.New(objective.Minimize(objective.ContainerCount))
	conflicting := solver.Options{Objective: goal, PlanObjective: preferMoreContainers{}}
	for name, solve := range map[string]func() error{
		"exact all": func() error {
			_, err := (solver.Exact{}).PackAll(context.Background(), request, conflicting)
			return err
		},
		"exact fixed": func() error {
			_, err := (solver.Exact{}).PackFixed(context.Background(), request, instance, conflicting)
			return err
		},
		"heuristic all": func() error {
			_, err := (solver.Heuristic{}).PackAll(context.Background(), request, conflicting)
			return err
		},
		"heuristic fixed": func() error {
			_, err := (solver.Heuristic{}).PackFixed(context.Background(), request, instance, conflicting)
			return err
		},
	} {
		if err := solve(); !errors.Is(err, knapsack.ErrInvalidOptions) {
			t.Fatalf("%s option error = %v", name, err)
		}
	}
	for _, instances := range [][]knapsack.ContainerInstance{
		nil,
		{{ID: "", TypeID: "box"}},
		{{ID: "box#1", TypeID: "unknown"}},
		{{ID: "box#1", TypeID: "box"}, {ID: "box#1", TypeID: "box"}},
	} {
		for name, solve := range map[string]func() error{
			"exact": func() error {
				_, err := (solver.Exact{}).PackFixed(context.Background(), request, instances, solver.Options{})
				return err
			},
			"heuristic": func() error {
				_, err := (solver.Heuristic{}).PackFixed(context.Background(), request, instances, solver.Options{})
				return err
			},
		} {
			if err := solve(); err == nil {
				t.Fatalf("%s accepted instances %+v", name, instances)
			}
		}
	}
}
