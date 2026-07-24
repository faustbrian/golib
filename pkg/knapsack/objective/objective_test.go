package objective_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/objective"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

type objectiveCallback struct {
	comparison int
	components []knapsack.ScoreComponent
	err        error
	panicIn    string
}

func (*objectiveCallback) Valid() bool { return true }

func (o *objectiveCallback) ComparePlans(context.Context, knapsack.NormalizedRequest, knapsack.Plan, knapsack.Plan) (int, error) {
	if o.panicIn == "compare" {
		panic("compare panic")
	}
	return o.comparison, o.err
}

func (o *objectiveCallback) Components(context.Context, knapsack.NormalizedRequest, knapsack.Plan) ([]knapsack.ScoreComponent, error) {
	if o.panicIn == "components" {
		panic("components panic")
	}
	return o.components, o.err
}

func testResolution() knapsack.Resolution {
	return knapsack.Resolution{Length: measurement.MustNew(decimal.New(1), measurement.Metre), Mass: measurement.MustNew(decimal.New(1), measurement.Kilogram)}
}

func TestLexicographicPrecedence(t *testing.T) {
	t.Parallel()

	goal, err := objective.New(
		objective.Minimize(objective.ContainerCount),
		objective.Minimize(objective.UnusedVolume),
	)
	if err != nil {
		t.Fatal(err)
	}

	// A lower-priority density improvement cannot override box count.
	left := objective.Score{Values: []int64{1, 100}}
	right := objective.Score{Values: []int64{2, 0}}
	comparison, err := goal.Compare(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if comparison >= 0 {
		t.Fatalf("comparison = %d, want left preferred", comparison)
	}
}

func TestObjectiveRejectsInvalidScoresAndDuplicateCriteria(t *testing.T) {
	t.Parallel()

	if _, err := objective.New(
		objective.Minimize(objective.ContainerCount),
		objective.Minimize(objective.ContainerCount),
	); err == nil {
		t.Fatal("duplicate criterion accepted")
	}
	goal, _ := objective.New(objective.Minimize(objective.ContainerCount))
	if _, err := goal.Compare(objective.Score{}, objective.Score{}); err == nil {
		t.Fatal("invalid score accepted")
	}
}

func TestObjectiveRejectsExcessCriteriaBeforeAllocation(t *testing.T) {
	valid := []objective.Criterion{
		objective.Minimize(objective.ContainerCount),
		objective.Minimize(objective.TotalPackagingCost),
		objective.Minimize(objective.UnusedVolume),
		objective.Minimize(objective.UnusedWeight),
		objective.Minimize(objective.WeightImbalance),
		objective.Minimize(objective.MaximumUsedHeight),
		objective.Maximize(objective.PackedPriority),
	}
	if _, err := objective.New(valid...); err != nil {
		t.Fatal(err)
	}
	criteria := make([]objective.Criterion, 1<<20)
	copy(criteria, valid)
	if allocations := testing.AllocsPerRun(10, func() {
		_, _ = objective.New(criteria...)
	}); allocations != 0 {
		t.Fatalf("excess criteria allocated %.0f objects", allocations)
	}
}

func TestObjectiveEntriesRejectNilContext(t *testing.T) {
	goal, err := objective.New(objective.Minimize(objective.ContainerCount))
	if err != nil {
		t.Fatal(err)
	}
	var ctx context.Context
	for name, invoke := range map[string]func() error{
		"compare": func() error {
			_, invokeErr := goal.ComparePlans(ctx, knapsack.NormalizedRequest{}, knapsack.Plan{}, knapsack.Plan{})
			return invokeErr
		},
		"components": func() error {
			_, invokeErr := goal.Components(ctx, knapsack.NormalizedRequest{}, knapsack.Plan{})
			return invokeErr
		},
		"safe compare": func() error {
			_, invokeErr := objective.SafeCompare(ctx, goal, knapsack.NormalizedRequest{}, knapsack.Plan{}, knapsack.Plan{})
			return invokeErr
		},
		"safe components": func() error {
			_, invokeErr := objective.SafeComponents(ctx, goal, knapsack.NormalizedRequest{}, knapsack.Plan{})
			return invokeErr
		},
	} {
		if invokeErr := invoke(); !errors.Is(invokeErr, knapsack.ErrInvalidOptions) {
			t.Fatalf("%s error = %v", name, invokeErr)
		}
	}
}

func TestCanonicalTieBreak(t *testing.T) {
	t.Parallel()

	goal, _ := objective.New(objective.Maximize(objective.PackedPriority))
	comparison, err := goal.Compare(
		objective.Score{Values: []int64{10}, TieBreak: "a"},
		objective.Score{Values: []int64{10}, TieBreak: "b"},
	)
	if err != nil || comparison >= 0 {
		t.Fatalf("comparison = %d, err = %v", comparison, err)
	}
}

func TestObjectivePlanInterfaceHonorsCancellationAndScoreFailures(t *testing.T) {
	t.Parallel()
	goal, _ := objective.New(objective.Maximize(objective.PackedPriority))
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items:      []knapsack.NormalizedItem{{ID: "item", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1, Orientations: []geometry.Orientation{geometry.OrientationXYZ}, Priority: 2}},
		Containers: []knapsack.NormalizedContainer{{ID: "box", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, MaxContentWeight: 1, Stock: knapsack.UnlimitedStock()}},
		Resolution: testResolution(), Limits: knapsack.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	packed := planWithPlacements([]knapsack.Placement{{ItemID: "item"}})
	empty := planWithPlacements(nil)
	if comparison, compareErr := goal.ComparePlans(context.Background(), request, packed, empty); compareErr != nil || comparison >= 0 {
		t.Fatalf("comparison=%d error=%v", comparison, compareErr)
	}
	if components, componentErr := goal.Components(context.Background(), request, packed); componentErr != nil || components[0].Direction != "max" {
		t.Fatalf("components=%+v error=%v", components, componentErr)
	}
	invalid := planWithPlacements([]knapsack.Placement{{ItemID: "unknown"}})
	if _, compareErr := goal.ComparePlans(context.Background(), request, invalid, packed); !errors.Is(compareErr, objective.ErrInvalidObjective) {
		t.Fatalf("left score error = %v", compareErr)
	}
	if _, compareErr := goal.ComparePlans(context.Background(), request, packed, invalid); !errors.Is(compareErr, objective.ErrInvalidObjective) {
		t.Fatalf("right score error = %v", compareErr)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, compareErr := goal.ComparePlans(cancelled, request, packed, empty); !errors.Is(compareErr, context.Canceled) {
		t.Fatalf("cancelled compare error = %v", compareErr)
	}
	if _, componentErr := goal.Components(cancelled, request, packed); !errors.Is(componentErr, context.Canceled) {
		t.Fatalf("cancelled components error = %v", componentErr)
	}
}

func TestObjectiveScoresPlanMetricsExactly(t *testing.T) {
	t.Parallel()
	goal, err := objective.New(
		objective.Minimize(objective.ContainerCount),
		objective.Minimize(objective.UnusedVolume),
		objective.Minimize(objective.UnusedWeight),
		objective.Minimize(objective.WeightImbalance),
		objective.Minimize(objective.MaximumUsedHeight),
		objective.Maximize(objective.PackedPriority),
	)
	if err != nil {
		t.Fatal(err)
	}
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{Items: []knapsack.NormalizedItem{{ID: "a", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 2, Orientations: []geometry.Orientation{geometry.OrientationXYZ}, Priority: 7}, {ID: "b", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1, Orientations: []geometry.Orientation{geometry.OrientationXYZ}, Priority: 5}}, Containers: []knapsack.NormalizedContainer{{ID: "box", Dimensions: geometry.Dimensions{X: 2, Y: 1, Z: 2}, MaxContentWeight: 5, Stock: knapsack.UnlimitedStock()}}, Resolution: testResolution(), Limits: knapsack.DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{Containers: []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}, Placements: []knapsack.Placement{{ItemID: "a", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 2}, {ItemID: "b", ContainerID: "box#1", Origin: geometry.Point{Z: 1}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1}}, Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted, Statistics: knapsack.Statistics{PackedItems: 2, ContainerCount: 1, ItemWeight: 3, ItemVolume: 2, ContainerVolume: 4, RemainingWeight: 2, RemainingVolume: 2}})
	score, components, err := goal.ScorePlan(request, plan)
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{1, 2, 2, 0, 2, 12}
	if len(score.Values) != len(want) {
		t.Fatalf("score = %+v", score)
	}
	for index := range want {
		if score.Values[index] != want[index] {
			t.Fatalf("score[%d] = %d, want %d", index, score.Values[index], want[index])
		}
	}
	if len(components) != len(want) {
		t.Fatalf("components = %+v", components)
	}
}

func TestObjectiveDefinitionAndSafeCallbackBoundaries(t *testing.T) {
	t.Parallel()
	if _, err := objective.New(); !errors.Is(err, objective.ErrInvalidObjective) {
		t.Fatalf("empty objective error = %v", err)
	}
	if _, err := objective.New(objective.Criterion{Metric: "invalid", Direction: objective.Min}); !errors.Is(err, objective.ErrInvalidObjective) {
		t.Fatalf("metric error = %v", err)
	}
	goal, _ := objective.New(objective.Maximize(objective.PackedPriority))
	criteria := goal.Criteria()
	criteria[0].Metric = objective.ContainerCount
	if goal.Criteria()[0].Metric != objective.PackedPriority {
		t.Fatal("criteria aliases caller state")
	}
	left, _ := knapsack.NewPlan(knapsack.PlanSpec{Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted})
	right, _ := knapsack.NewPlan(knapsack.PlanSpec{Status: knapsack.StatusBestKnown, Termination: knapsack.TerminationCompleted})
	validComponents := []knapsack.ScoreComponent{{Name: "custom", Direction: "min", Unit: "count", Value: "1"}}
	callback := &objectiveCallback{components: validComponents}
	if _, err := objective.SafeCompare(context.Background(), nil, knapsack.NormalizedRequest{}, left, right); !errors.Is(err, objective.ErrInvalidObjective) {
		t.Fatalf("nil comparison error = %v", err)
	}
	if comparison, err := objective.SafeCompare(context.Background(), callback, knapsack.NormalizedRequest{}, left, right); err != nil || comparison == 0 {
		t.Fatalf("comparison=%d error=%v", comparison, err)
	}
	callback.comparison = 2
	if _, err := objective.SafeCompare(context.Background(), callback, knapsack.NormalizedRequest{}, left, right); !errors.Is(err, objective.ErrInvalidObjective) {
		t.Fatalf("comparison error = %v", err)
	}
	callback.comparison, callback.panicIn = 0, "compare"
	if _, err := objective.SafeCompare(context.Background(), callback, knapsack.NormalizedRequest{}, left, right); !errors.Is(err, objective.ErrCallbackPanic) {
		t.Fatalf("panic error = %v", err)
	}
	callback.panicIn = "components"
	if _, err := objective.SafeComponents(context.Background(), callback, knapsack.NormalizedRequest{}, left); !errors.Is(err, objective.ErrCallbackPanic) {
		t.Fatalf("component panic error = %v", err)
	}
	callback.panicIn, callback.components = "", nil
	callback.err = errors.New("callback error")
	if _, err := objective.SafeCompare(context.Background(), callback, knapsack.NormalizedRequest{}, left, right); err == nil || err.Error() != "callback error" {
		t.Fatalf("callback compare error = %v", err)
	}
	if _, err := objective.SafeComponents(context.Background(), callback, knapsack.NormalizedRequest{}, left); err == nil || err.Error() != "callback error" {
		t.Fatalf("callback component error = %v", err)
	}
	callback.err = nil
	if _, err := objective.SafeComponents(context.Background(), callback, knapsack.NormalizedRequest{}, left); !errors.Is(err, objective.ErrInvalidObjective) {
		t.Fatalf("empty components error = %v", err)
	}
	callback.components = []knapsack.ScoreComponent{{Name: "custom", Direction: "sideways", Unit: "count", Value: "1"}}
	if _, err := objective.SafeComponents(context.Background(), callback, knapsack.NormalizedRequest{}, left); !errors.Is(err, objective.ErrInvalidObjective) {
		t.Fatalf("invalid components error = %v", err)
	}
	callback.components = []knapsack.ScoreComponent{{Name: strings.Repeat("x", 1025), Direction: "min", Unit: "count", Value: "1"}}
	if _, err := objective.SafeComponents(context.Background(), callback, knapsack.NormalizedRequest{}, left); !errors.Is(err, objective.ErrInvalidObjective) {
		t.Fatalf("oversized components error = %v", err)
	}
	callback.components = make([]knapsack.ScoreComponent, 33)
	if _, err := objective.SafeComponents(context.Background(), callback, knapsack.NormalizedRequest{}, left); !errors.Is(err, objective.ErrInvalidObjective) {
		t.Fatalf("component count error = %v", err)
	}
	var typedNil *objectiveCallback
	if _, err := objective.SafeComponents(context.Background(), typedNil, knapsack.NormalizedRequest{}, left); !errors.Is(err, objective.ErrInvalidObjective) {
		t.Fatalf("typed nil error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := objective.SafeCompare(ctx, callback, knapsack.NormalizedRequest{}, left, right); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel comparison error = %v", err)
	}
	if _, err := objective.SafeComponents(ctx, callback, knapsack.NormalizedRequest{}, left); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}

func TestObjectiveRejectsUnrepresentablePlanScores(t *testing.T) {
	t.Parallel()
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: []knapsack.NormalizedItem{
			{ID: "a", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1, Orientations: []geometry.Orientation{geometry.OrientationXYZ}, Priority: math.MaxInt64},
			{ID: "b", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1, Orientations: []geometry.Orientation{geometry.OrientationXYZ}, Priority: 1},
		},
		Containers: []knapsack.NormalizedContainer{{ID: "box", Dimensions: geometry.Dimensions{X: 2, Y: 1, Z: 1}, MaxContentWeight: math.MaxInt64, Stock: knapsack.UnlimitedStock()}},
		Resolution: testResolution(), Limits: knapsack.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	goal, _ := objective.New(objective.Maximize(objective.PackedPriority))
	tests := []struct {
		name string
		plan knapsack.Plan
		goal objective.Objective
	}{
		{"priority overflow", planWithPlacements([]knapsack.Placement{{ItemID: "a"}, {ItemID: "b"}}), goal},
		{"unknown item", planWithPlacements([]knapsack.Placement{{ItemID: "unknown"}}), goal},
		{"height overflow", planWithPlacements([]knapsack.Placement{{ItemID: "a", Origin: geometry.Point{Z: math.MaxInt64}, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}}}), goal},
		{"weight overflow", planWithPlacements([]knapsack.Placement{{ItemID: "a", ContainerID: "box", Weight: math.MaxInt64}, {ItemID: "b", ContainerID: "box", Weight: 1}}), goal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := test.goal.ScorePlan(request, test.plan); !errors.Is(err, objective.ErrInvalidObjective) {
				t.Fatalf("error = %v", err)
			}
		})
	}
	costGoal, _ := objective.New(objective.Minimize(objective.TotalPackagingCost))
	if _, _, err := costGoal.ScorePlan(request, planWithPlacements(nil)); !errors.Is(err, objective.ErrInvalidObjective) {
		t.Fatalf("cost error = %v", err)
	}
	if _, _, err := (objective.Objective{}).ScorePlan(request, planWithPlacements(nil)); !errors.Is(err, objective.ErrInvalidObjective) {
		t.Fatalf("zero objective error = %v", err)
	}
}

func TestObjectiveRejectsWeightImbalanceOverflow(t *testing.T) {
	t.Parallel()

	goal, _ := objective.New(objective.Minimize(objective.WeightImbalance))
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: []knapsack.NormalizedItem{
			{ID: "a", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1, Orientations: []geometry.Orientation{geometry.OrientationXYZ}},
			{ID: "b", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1, Orientations: []geometry.Orientation{geometry.OrientationXYZ}},
		},
		Containers: []knapsack.NormalizedContainer{{ID: "box", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, MaxContentWeight: 1, Stock: knapsack.UnlimitedStock()}},
		Resolution: testResolution(), Limits: knapsack.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := knapsack.NewPlan(knapsack.PlanSpec{
		Containers: []knapsack.ContainerInstance{{ID: "left"}, {ID: "right"}},
		Placements: []knapsack.Placement{
			{ItemID: "a", ContainerID: "left", Weight: math.MinInt64},
			{ItemID: "b", ContainerID: "right", Weight: math.MaxInt64},
		},
		Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := goal.ScorePlan(request, plan); !errors.Is(err, objective.ErrInvalidObjective) {
		t.Fatalf("error = %v", err)
	}
}

func planWithPlacements(placements []knapsack.Placement) knapsack.Plan {
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{Placements: placements, Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted})
	return plan
}
