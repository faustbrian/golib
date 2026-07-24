package solver_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/constraint"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/objective"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

type rejectAll struct{}

func (rejectAll) Check(context.Context, constraint.PlacementView) constraint.Decision {
	return constraint.Reject("application_rejection", "application rejected placement")
}

type panicOnPlacement struct{}

func (panicOnPlacement) Check(context.Context, constraint.PlacementView) constraint.Decision {
	panic("callback failure")
}

func quantity(value int64, unit measurement.Unit) measurement.Quantity {
	return measurement.MustNew(decimal.New(value), unit)
}

func TestHeuristicPackAllIsDeterministicAndVerified(t *testing.T) {
	t.Parallel()
	dims := knapsack.PhysicalDimensions{X: quantity(2, measurement.Metre), Y: quantity(2, measurement.Metre), Z: quantity(2, measurement.Metre)}
	items := make([]knapsack.Item, 3)
	for index, id := range []string{"c", "a", "b"} {
		items[index], _ = knapsack.NewItem(knapsack.ItemSpec{ID: id, Dimensions: dims, Weight: quantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	}
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: knapsack.PhysicalDimensions{X: quantity(4, measurement.Metre), Y: quantity(4, measurement.Metre), Z: quantity(2, measurement.Metre)}, MaxContentWeight: quantity(10, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	request, err := knapsack.NewRequest(items, []knapsack.ContainerType{box}, knapsack.Resolution{Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}

	first, err := solver.Heuristic{}.PackAll(context.Background(), request.Normalized(), solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := solver.Heuristic{}.PackAll(context.Background(), request.Normalized(), solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first.CanonicalString() != second.CanonicalString() {
		t.Fatal("result is nondeterministic")
	}
	if result := verify.Plan(request.Normalized(), first, verify.RequireAll()); !result.Valid() {
		t.Fatalf("invalid solver plan: %+v", result.Violations())
	}
	if first.Status() != knapsack.StatusFeasible || len(first.UnpackedItemIDs()) != 0 {
		t.Fatalf("status=%s unpacked=%v", first.Status(), first.UnpackedItemIDs())
	}
}

func TestSolversHonorContainerCenterOfGravityBounds(t *testing.T) {
	t.Parallel()

	item, err := knapsack.NewItem(knapsack.ItemSpec{
		ID: "item", Dimensions: knapsack.PhysicalDimensions{
			X: quantity(1, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre),
		}, Weight: quantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ},
	})
	if err != nil {
		t.Fatal(err)
	}
	box, err := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
		ID: "box", InternalDimensions: knapsack.PhysicalDimensions{
			X: quantity(3, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre),
		}, MaxContentWeight: quantity(1, measurement.Kilogram), Stock: knapsack.FiniteStock(1),
		CenterOfGravity: &knapsack.CenterOfGravityBounds{
			MinXPPM: 500_000, MaxXPPM: 500_000,
			MinYPPM: 500_000, MaxYPPM: 500_000,
			MinZPPM: 500_000, MaxZPPM: 500_000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := knapsack.NewRequest([]knapsack.Item{item}, []knapsack.ContainerType{box}, knapsack.Resolution{
		Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram),
	}, knapsack.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	for name, pack := range map[string]func() (knapsack.Plan, error){
		"heuristic": func() (knapsack.Plan, error) {
			return (solver.Heuristic{}).PackFixed(context.Background(), request.Normalized(), instances, solver.Options{})
		},
		"exact": func() (knapsack.Plan, error) {
			return (solver.Exact{}).PackFixed(context.Background(), request.Normalized(), instances, solver.Options{})
		},
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := pack()
			if err != nil {
				t.Fatal(err)
			}
			placements := plan.Placements()
			if len(placements) != 1 || placements[0].Origin.X != 1 {
				t.Fatalf("placements = %+v", placements)
			}
			if result := verify.Plan(request.Normalized(), plan, verify.RequireAll()); !result.Valid() {
				t.Fatalf("solver returned invalid plan: %+v", result.Violations())
			}
		})
	}
}

func TestHeuristicReturnsVerifiedPartialPlanWhenCenterOfGravityIsImpossible(t *testing.T) {
	t.Parallel()

	item, _ := knapsack.NewItem(knapsack.ItemSpec{
		ID: "item", Dimensions: knapsack.PhysicalDimensions{
			X: quantity(1, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre),
		}, Weight: quantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ},
	})
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
		ID: "box", InternalDimensions: knapsack.PhysicalDimensions{
			X: quantity(1, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre),
		}, MaxContentWeight: quantity(1, measurement.Kilogram), Stock: knapsack.FiniteStock(1),
		CenterOfGravity: &knapsack.CenterOfGravityBounds{},
	})
	request, _ := knapsack.NewRequest([]knapsack.Item{item}, []knapsack.ContainerType{box}, knapsack.Resolution{
		Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram),
	}, knapsack.DefaultLimits())
	plan, err := (solver.Heuristic{}).PackFixed(context.Background(), request.Normalized(),
		[]knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}, solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status() != knapsack.StatusBestKnown || len(plan.Placements()) != 0 ||
		!slices.Equal(plan.UnpackedItemIDs(), []string{"item"}) {
		t.Fatalf("unexpected partial plan: %s", plan.CanonicalString())
	}
	if result := verify.Plan(request.Normalized(), plan, verify.AllowUnpacked()); !result.Valid() {
		t.Fatalf("partial plan is invalid: %+v", result.Violations())
	}
}

func TestPackAllBoundsCenterOfGravityDiagnostics(t *testing.T) {
	t.Parallel()

	dimensions := knapsack.PhysicalDimensions{
		X: quantity(1, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre),
	}
	items := make([]knapsack.Item, 2)
	for index, id := range []string{"a", "b"} {
		items[index], _ = knapsack.NewItem(knapsack.ItemSpec{
			ID: id, Dimensions: dimensions, Weight: quantity(1, measurement.Kilogram),
			Orientations: []geometry.Orientation{geometry.OrientationXYZ},
		})
	}
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
		ID: "box", InternalDimensions: knapsack.PhysicalDimensions{
			X: quantity(2, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre),
		}, MaxContentWeight: quantity(2, measurement.Kilogram), Stock: knapsack.UnlimitedStock(),
		CenterOfGravity: &knapsack.CenterOfGravityBounds{},
	})
	limits := knapsack.DefaultLimits()
	limits.MaxDiagnostics = 1
	request, _ := knapsack.NewRequest(items, []knapsack.ContainerType{box}, knapsack.Resolution{
		Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram),
	}, limits)
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), request.Normalized(), solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Placements()) != 0 || !slices.Equal(plan.UnpackedItemIDs(), []string{"a", "b"}) || len(plan.Diagnostics()) != 1 {
		t.Fatalf("unexpected bounded partial plan: %s", plan.CanonicalString())
	}
	if result := verify.Plan(request.Normalized(), plan, verify.AllowUnpacked()); !result.Valid() {
		t.Fatalf("partial plan is invalid: %+v", result.Violations())
	}
}

func TestHeuristicHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := solver.Heuristic{}.PackAll(ctx, knapsack.NormalizedRequest{}, solver.Options{})
	if err == nil {
		t.Fatal("cancelled solve succeeded")
	}
}

func TestHeuristicPackFixedNeverInventsContainers(t *testing.T) {
	t.Parallel()
	dims := knapsack.PhysicalDimensions{X: quantity(2, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre)}
	items := make([]knapsack.Item, 2)
	for index, id := range []string{"a", "b"} {
		items[index], _ = knapsack.NewItem(knapsack.ItemSpec{ID: id, Dimensions: dims, Weight: quantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	}
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: dims, MaxContentWeight: quantity(2, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	request, _ := knapsack.NewRequest(items, []knapsack.ContainerType{box}, knapsack.Resolution{Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	instances := []knapsack.ContainerInstance{{ID: "supplied", TypeID: "box"}}
	plan, err := (solver.Heuristic{}).PackFixed(context.Background(), request.Normalized(), instances, solver.Options{AllowUnpacked: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Containers(); len(got) != 1 || got[0].ID != "supplied" {
		t.Fatalf("containers = %+v", got)
	}
	if len(plan.UnpackedItemIDs()) != 1 {
		t.Fatalf("unpacked = %v", plan.UnpackedItemIDs())
	}
}

func TestHeuristicAvoidsFragileSupportSurface(t *testing.T) {
	t.Parallel()
	dims := knapsack.PhysicalDimensions{X: quantity(1, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre)}
	fragile, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "a", Dimensions: dims, Weight: quantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}, FragileTop: true})
	ordinary, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "b", Dimensions: dims, Weight: quantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: knapsack.PhysicalDimensions{X: quantity(1, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(2, measurement.Metre)}, MaxContentWeight: quantity(2, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	request, _ := knapsack.NewRequest([]knapsack.Item{ordinary, fragile}, []knapsack.ContainerType{box}, knapsack.Resolution{Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), request.Normalized(), solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Containers()) != 2 {
		t.Fatalf("containers = %+v", plan.Containers())
	}
}

func TestHeuristicAppliesCustomConstraintAndConvertsPanic(t *testing.T) {
	t.Parallel()
	dims := knapsack.PhysicalDimensions{X: quantity(1, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre)}
	item, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "item", Dimensions: dims, Weight: quantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: dims, MaxContentWeight: quantity(1, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	request, _ := knapsack.NewRequest([]knapsack.Item{item}, []knapsack.ContainerType{box}, knapsack.Resolution{Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), request.Normalized(), solver.Options{AllowUnpacked: true, Constraints: []constraint.Placement{rejectAll{}}})
	if err != nil || len(plan.UnpackedItemIDs()) != 1 {
		t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
	}
	_, err = (solver.Heuristic{}).PackAll(context.Background(), request.Normalized(), solver.Options{Constraints: []constraint.Placement{panicOnPlacement{}}})
	if !errors.Is(err, constraint.ErrCallbackPanic) {
		t.Fatalf("panic error = %v", err)
	}
}

func TestHeuristicAvoidsTransitiveOverloadAndStackExcess(t *testing.T) {
	t.Parallel()
	dims := knapsack.PhysicalDimensions{X: quantity(1, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre)}
	maximum := quantity(1, measurement.Kilogram)
	items := make([]knapsack.Item, 3)
	items[0], _ = knapsack.NewItem(knapsack.ItemSpec{ID: "a", Dimensions: dims, Weight: quantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}, MaxSupportedWeight: &maximum, MaxStackCount: 1})
	for index, id := range []string{"b", "c"} {
		items[index+1], _ = knapsack.NewItem(knapsack.ItemSpec{ID: id, Dimensions: dims, Weight: quantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	}
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: knapsack.PhysicalDimensions{X: quantity(1, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(3, measurement.Metre)}, MaxContentWeight: quantity(3, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	request, _ := knapsack.NewRequest(items, []knapsack.ContainerType{box}, knapsack.Resolution{Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), request.Normalized(), solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Containers()) != 2 {
		t.Fatalf("containers = %+v", plan.Containers())
	}
}

func TestHeuristicChecksLargeSupportRatiosWithoutOverflow(t *testing.T) {
	t.Parallel()
	baseDimensions := geometry.Dimensions{X: 1_500_000_000, Y: 1_000_000_000, Z: 2}
	topDimensions := geometry.Dimensions{X: 3_000_000_000, Y: 1_000_000_000, Z: 1}
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: []knapsack.NormalizedItem{
			{ID: "a", Dimensions: baseDimensions, Weight: 1, Orientations: []geometry.Orientation{geometry.OrientationXYZ}},
			{ID: "b", Dimensions: topDimensions, Weight: 1, Orientations: []geometry.Orientation{geometry.OrientationXYZ}, MinimumSupportPPM: 2},
		},
		Containers: []knapsack.NormalizedContainer{{ID: "box", Dimensions: geometry.Dimensions{X: topDimensions.X, Y: topDimensions.Y, Z: 3}, MaxContentWeight: 2, Stock: knapsack.UnlimitedStock()}},
		Resolution: knapsack.Resolution{Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram)},
		Limits:     knapsack.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Containers()) != 1 {
		t.Fatalf("plan = %s", plan.CanonicalString())
	}
}

func TestFixedSolversRejectFiniteStockExcess(t *testing.T) {
	t.Parallel()
	request := exactRequest(t, 4, 1)
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}, {ID: "box#2", TypeID: "box"}}
	for name, solve := range map[string]func() error{
		"heuristic": func() error {
			_, err := (solver.Heuristic{}).PackFixed(context.Background(), request, instances, solver.Options{})
			return err
		},
		"exact": func() error {
			_, err := (solver.Exact{}).PackFixed(context.Background(), request, instances, solver.Options{})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := solve(); !errors.Is(err, knapsack.ErrInsufficientStock) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestHeuristicKeepsRequiredGroupTogetherOrUnpacked(t *testing.T) {
	t.Parallel()
	dims := knapsack.PhysicalDimensions{X: quantity(1, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre)}
	items := make([]knapsack.Item, 2)
	for index, id := range []string{"a", "b"} {
		items[index], _ = knapsack.NewItem(knapsack.ItemSpec{ID: id, Group: "linked", Dimensions: dims, Weight: quantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	}
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: dims, MaxContentWeight: quantity(1, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	request, _ := knapsack.NewRequest(items, []knapsack.ContainerType{box}, knapsack.Resolution{Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), request.Normalized(), solver.Options{AllowUnpacked: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Placements()) != 0 || len(plan.UnpackedItemIDs()) != 2 {
		t.Fatalf("plan = %s", plan.CanonicalString())
	}
}

func TestHeuristicHonorsHeightBeforeContainerCount(t *testing.T) {
	t.Parallel()
	dims := knapsack.PhysicalDimensions{X: quantity(1, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre)}
	items := make([]knapsack.Item, 2)
	for index, id := range []string{"a", "b"} {
		items[index], _ = knapsack.NewItem(knapsack.ItemSpec{ID: id, Dimensions: dims, Weight: quantity(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	}
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: knapsack.PhysicalDimensions{X: quantity(1, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(2, measurement.Metre)}, MaxContentWeight: quantity(2, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	request, _ := knapsack.NewRequest(items, []knapsack.ContainerType{box}, knapsack.Resolution{Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	goal, _ := objective.New(objective.Minimize(objective.MaximumUsedHeight), objective.Minimize(objective.ContainerCount))
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), request.Normalized(), solver.Options{Objective: goal})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Containers()) != 2 {
		t.Fatalf("plan = %s", plan.CanonicalString())
	}
	if got := plan.Objective(); len(got) != 2 || got[0].Name != string(objective.MaximumUsedHeight) {
		t.Fatalf("objective = %+v", got)
	}
}

func TestHeuristicCrossContainerRepackingRemovesFragmentation(t *testing.T) {
	t.Parallel()

	shapes := []knapsack.PhysicalDimensions{
		{X: quantity(3, measurement.Metre), Y: quantity(2, measurement.Metre), Z: quantity(1, measurement.Metre)},
		{X: quantity(2, measurement.Metre), Y: quantity(2, measurement.Metre), Z: quantity(1, measurement.Metre)},
		{X: quantity(2, measurement.Metre), Y: quantity(1, measurement.Metre), Z: quantity(1, measurement.Metre)},
	}
	items := make([]knapsack.Item, len(shapes))
	for index, dimensions := range shapes {
		items[index], _ = knapsack.NewItem(knapsack.ItemSpec{
			ID: fmt.Sprintf("item-%d", index), Dimensions: dimensions,
			Weight:       quantity(1, measurement.Kilogram),
			Orientations: []geometry.Orientation{geometry.OrientationXYZ, geometry.OrientationYXZ},
		})
	}
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
		ID: "box",
		InternalDimensions: knapsack.PhysicalDimensions{
			X: quantity(4, measurement.Metre), Y: quantity(3, measurement.Metre), Z: quantity(1, measurement.Metre),
		},
		MaxContentWeight: quantity(3, measurement.Kilogram), Stock: knapsack.UnlimitedStock(),
	})
	request, _ := knapsack.NewRequest(items, []knapsack.ContainerType{box}, knapsack.Resolution{
		Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram),
	}, knapsack.DefaultLimits())
	normalized := request.Normalized()
	limits := normalized.Limits()
	limits.MaxImprovementRounds = 0
	fragmented, err := (solver.Heuristic{}).PackAll(context.Background(), normalized.WithLimits(limits), solver.Options{})
	if err != nil || len(fragmented.Containers()) != 2 || fragmented.Work().ImprovementRounds != 0 {
		t.Fatalf("disabled repacking plan=%s error=%v", fragmented.CanonicalString(), err)
	}
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), normalized, solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Containers()) != 1 {
		t.Fatalf("cross-container repacking did not consolidate plan: %s", plan.CanonicalString())
	}
	if plan.Work().ImprovementRounds != 1 || plan.Work().Strategy != "deterministic_extreme_point_repack" {
		t.Fatalf("repacking work was not reported: %+v", plan.Work())
	}
	if result := verify.Plan(normalized, plan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("repacked plan invalid: %+v", result.Violations())
	}
}
