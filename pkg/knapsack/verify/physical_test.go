package verify_test

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/constraint"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/measurement"
)

type verifierReject struct{}

func (verifierReject) Check(context.Context, constraint.PlacementView) constraint.Decision {
	return constraint.Reject("policy", "placement is forbidden")
}

type verifierPanic struct{}

func (verifierPanic) Check(context.Context, constraint.PlacementView) constraint.Decision {
	panic("broken policy")
}

func normalizedRequest(t *testing.T, specs []knapsack.ItemSpec, containerSpec knapsack.ContainerTypeSpec) knapsack.NormalizedRequest {
	t.Helper()
	items := make([]knapsack.Item, len(specs))
	for index, spec := range specs {
		var err error
		items[index], err = knapsack.NewItem(spec)
		if err != nil {
			t.Fatal(err)
		}
	}
	container, err := knapsack.NewContainerType(containerSpec)
	if err != nil {
		t.Fatal(err)
	}
	request, err := knapsack.NewRequest(items, []knapsack.ContainerType{container}, knapsack.Resolution{Length: q(1, measurement.Metre), Mass: q(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return request.Normalized()
}

func planWithStatistics(t *testing.T, request knapsack.NormalizedRequest, containers []knapsack.ContainerInstance, placements []knapsack.Placement, unpacked []string) knapsack.Plan {
	t.Helper()
	types := make(map[string]knapsack.NormalizedContainer)
	for _, container := range request.Containers() {
		types[container.ID] = container
	}
	stats := knapsack.Statistics{PackedItems: uint32(len(placements)), ContainerCount: uint32(len(containers))}
	weights := make(map[string]int64)
	for _, placement := range placements {
		volume, _ := placement.Dimensions.Volume()
		stats.ItemVolume += volume
		stats.ItemWeight += placement.Weight
		weights[placement.ContainerID] += placement.Weight
	}
	for _, instance := range containers {
		info := types[instance.TypeID]
		volume, _ := info.Dimensions.Volume()
		stats.ContainerVolume += volume
		stats.RemainingWeight += info.MaxContentWeight - weights[instance.ID]
	}
	stats.RemainingVolume = stats.ContainerVolume - stats.ItemVolume
	plan, err := knapsack.NewPlan(knapsack.PlanSpec{Containers: containers, Placements: placements, UnpackedItemIDs: unpacked, Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted, Statistics: stats})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func unitSpec(id string) knapsack.ItemSpec {
	return knapsack.ItemSpec{ID: id, Dimensions: knapsack.PhysicalDimensions{X: q(1, measurement.Metre), Y: q(1, measurement.Metre), Z: q(1, measurement.Metre)}, Weight: q(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}}
}
func containerSpec(x, z int64, stock uint32) knapsack.ContainerTypeSpec {
	return knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: knapsack.PhysicalDimensions{X: q(x, measurement.Metre), Y: q(1, measurement.Metre), Z: q(z, measurement.Metre)}, MaxContentWeight: q(10, measurement.Kilogram), Stock: knapsack.FiniteStock(stock)}
}

func TestVerifyAccountsForSuppliedEmptyContainers(t *testing.T) {
	t.Parallel()
	request := normalizedRequest(t, []knapsack.ItemSpec{unitSpec("item")}, containerSpec(1, 1, 2))
	containers := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}, {ID: "box#2", TypeID: "box"}}
	placements := []knapsack.Placement{{ItemID: "item", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1}}
	result := verify.Plan(request, planWithStatistics(t, request, containers, placements, nil), verify.RequireAll())
	if !result.Valid() {
		t.Fatalf("violations = %+v", result.Violations())
	}
}

func TestVerifyRejectsEligibilityGroupingAndUnknownUnpackedItem(t *testing.T) {
	t.Parallel()
	left, right := unitSpec("left"), unitSpec("right")
	left.Group, right.Group = "linked", "linked"
	left.Attributes = map[string]string{"class": "forbidden"}
	box := containerSpec(1, 1, 2)
	box.AllowedClasses = []string{"allowed"}
	request := normalizedRequest(t, []knapsack.ItemSpec{left, right}, box)
	containers := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}, {ID: "box#2", TypeID: "box"}}
	placements := []knapsack.Placement{{ItemID: "left", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1}, {ItemID: "right", ContainerID: "box#2", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1}}
	plan := planWithStatistics(t, request, containers, placements, []string{"ghost"})
	result := verify.Plan(request, plan, verify.AllowUnpacked())
	for _, code := range []verify.Code{verify.CodeEligibility, verify.CodeGrouping, verify.CodeUnknownItem} {
		if !result.Has(code) {
			t.Fatalf("missing %s in %+v", code, result.Violations())
		}
	}
}

func TestVerifyChecksSupportRelationshipsAndExactRatioBoundary(t *testing.T) {
	t.Parallel()
	base := unitSpec("base")
	top := unitSpec("top")
	top.Dimensions.X = q(2, measurement.Metre)
	top.MinimumSupportPPM = 500_000
	request := normalizedRequest(t, []knapsack.ItemSpec{base, top}, containerSpec(2, 2, 1))
	containers := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	placements := []knapsack.Placement{{ItemID: "base", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1}, {ItemID: "top", ContainerID: "box#1", Origin: geometry.Point{Z: 1}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 2, Y: 1, Z: 1}, Weight: 1, SupporterIDs: []string{"base"}}}
	result := verify.Plan(request, planWithStatistics(t, request, containers, placements, nil), verify.RequireAll())
	if !result.Valid() {
		t.Fatalf("exact support boundary rejected: %+v", result.Violations())
	}
	placements[1].SupporterIDs = nil
	result = verify.Plan(request, planWithStatistics(t, request, containers, placements, nil), verify.RequireAll())
	if !result.Has(verify.CodeSupportRelationship) {
		t.Fatalf("missing support relationship violation: %+v", result.Violations())
	}

	top.MinimumSupportPPM = 500_001
	request = normalizedRequest(t, []knapsack.ItemSpec{base, top}, containerSpec(2, 2, 1))
	placements[1].SupporterIDs = []string{"base"}
	result = verify.Plan(request, planWithStatistics(t, request, containers, placements, nil), verify.RequireAll())
	if !result.Has(verify.CodeUnsupported) {
		t.Fatalf("support below boundary accepted: %+v", result.Violations())
	}
}

func TestVerifyChecksExactCenterOfGravityBoundaries(t *testing.T) {
	t.Parallel()

	item := unitSpec("item")
	box := containerSpec(4, 2, 1)
	box.InternalDimensions.Y = q(2, measurement.Metre)
	box.CenterOfGravity = &knapsack.CenterOfGravityBounds{
		MinXPPM: 125_000, MaxXPPM: 125_000,
		MinYPPM: 250_000, MaxYPPM: 250_000,
		MinZPPM: 250_000, MaxZPPM: 250_000,
	}
	request := normalizedRequest(t, []knapsack.ItemSpec{item}, box)
	containers := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	placements := []knapsack.Placement{{
		ItemID: "item", ContainerID: "box#1", Orientation: geometry.OrientationXYZ,
		Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1,
	}}
	plan := planWithStatistics(t, request, containers, placements, nil)
	if result := verify.Plan(request, plan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("exact center-of-gravity boundary rejected: %+v", result.Violations())
	}

	box.CenterOfGravity.MinXPPM++
	box.CenterOfGravity.MaxXPPM++
	request = normalizedRequest(t, []knapsack.ItemSpec{item}, box)
	plan = planWithStatistics(t, request, containers, placements, nil)
	if result := verify.Plan(request, plan, verify.RequireAll()); !result.Has(verify.CodeCenterOfGravity) {
		t.Fatalf("out-of-bounds center of gravity accepted: %+v", result.Violations())
	}
}

func TestVerifyCenterOfGravityUsesOverflowSafeMoments(t *testing.T) {
	t.Parallel()

	base := request(t)
	items := []knapsack.NormalizedItem{{
		ID: "item", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1},
		Weight: math.MaxInt64, Orientations: []geometry.Orientation{geometry.OrientationXYZ},
	}}
	containers := []knapsack.NormalizedContainer{{
		ID: "box", Dimensions: geometry.Dimensions{X: math.MaxInt64, Y: 1, Z: 1},
		MaxContentWeight: math.MaxInt64, Stock: knapsack.FiniteStock(1),
		CenterOfGravity: &knapsack.CenterOfGravityBounds{
			MinXPPM: 999_999, MaxXPPM: 1_000_000,
			MinYPPM: 500_000, MaxYPPM: 500_000,
			MinZPPM: 500_000, MaxZPPM: 500_000,
		},
	}}
	custom, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: items, Containers: containers, Resolution: base.Resolution(), Limits: base.Limits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	placements := []knapsack.Placement{{
		ItemID: "item", ContainerID: "box#1", Origin: geometry.Point{X: math.MaxInt64 - 1},
		Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1},
		Weight: math.MaxInt64,
	}}
	plan := planWithStatistics(t, custom, instances, placements, nil)
	if result := verify.Plan(custom, plan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("overflow-safe center of gravity rejected: %+v", result.Violations())
	}
}

func TestVerifyUsesSupportAreaUnionWithoutDoubleCounting(t *testing.T) {
	t.Parallel()

	left, duplicate, top := unitSpec("left"), unitSpec("duplicate"), unitSpec("top")
	top.Dimensions.X = q(2, measurement.Metre)
	top.MinimumSupportPPM = 1_000_000
	request := normalizedRequest(t, []knapsack.ItemSpec{left, duplicate, top}, containerSpec(2, 2, 1))
	containers := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	placements := []knapsack.Placement{
		{ItemID: "left", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1},
		{ItemID: "duplicate", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1},
		{ItemID: "top", ContainerID: "box#1", Origin: geometry.Point{Z: 1}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 2, Y: 1, Z: 1}, Weight: 1, SupporterIDs: []string{"duplicate", "left"}},
	}
	result := verify.Plan(request, planWithStatistics(t, request, containers, placements, nil), verify.RequireAll())
	if !result.Has(verify.CodeUnsupported) {
		t.Fatalf("overlapping supporters were double counted: %+v", result.Violations())
	}
}

func TestVerifyPropagatesLoadAndEnforcesStackLimit(t *testing.T) {
	t.Parallel()
	base, middle, top := unitSpec("base"), unitSpec("middle"), unitSpec("top")
	maximum := q(1, measurement.Kilogram)
	base.MaxSupportedWeight = &maximum
	base.MaxStackCount = 1
	request := normalizedRequest(t, []knapsack.ItemSpec{base, middle, top}, containerSpec(1, 3, 1))
	containers := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	placements := []knapsack.Placement{{ItemID: "base", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1}, {ItemID: "middle", ContainerID: "box#1", Origin: geometry.Point{Z: 1}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1, SupporterIDs: []string{"base"}}, {ItemID: "top", ContainerID: "box#1", Origin: geometry.Point{Z: 2}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1, SupporterIDs: []string{"middle"}}}
	result := verify.Plan(request, planWithStatistics(t, request, containers, placements, nil), verify.RequireAll())
	if !result.Has(verify.CodeLoadBearing) || !result.Has(verify.CodeStackLimit) {
		t.Fatalf("violations = %+v", result.Violations())
	}
}

func TestVerifyRejectsWeightFragilityAndIncompatibilityFromInput(t *testing.T) {
	t.Parallel()
	base, top := unitSpec("base"), unitSpec("top")
	base.FragileTop = true
	base.Group = "fragile"
	top.IncompatibleGroups = []string{"fragile"}
	top.Weight = q(2, measurement.Kilogram)
	box := containerSpec(1, 2, 1)
	box.MaxContentWeight = q(2, measurement.Kilogram)
	tare, gross := q(1, measurement.Kilogram), q(2, measurement.Kilogram)
	box.TareWeight, box.MaxGrossWeight = &tare, &gross
	request := normalizedRequest(t, []knapsack.ItemSpec{base, top}, box)
	containers := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	placements := []knapsack.Placement{
		{ItemID: "base", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1},
		{ItemID: "top", ContainerID: "box#1", Origin: geometry.Point{Z: 1}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 2, SupporterIDs: []string{"base"}},
	}
	result := verify.Plan(request, planWithStatistics(t, request, containers, placements, nil), verify.RequireAll())
	for _, code := range []verify.Code{verify.CodeOverweight, verify.CodeFragile, verify.CodeIncompatible} {
		if !result.Has(code) {
			t.Fatalf("missing %s in %+v", code, result.Violations())
		}
	}
}

func TestVerifyReplaysCustomPlacementConstraints(t *testing.T) {
	t.Parallel()
	request := normalizedRequest(t, []knapsack.ItemSpec{unitSpec("item")}, containerSpec(1, 1, 1))
	containers := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	placements := []knapsack.Placement{{ItemID: "item", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1}}
	plan := planWithStatistics(t, request, containers, placements, nil)

	result := verify.Plan(request, plan, verify.RequireAll().WithConstraints(verifierReject{}))
	if !result.Has(verify.CodeConstraint) || result.Err() != nil {
		t.Fatalf("violations=%+v error=%v", result.Violations(), result.Err())
	}

	result = verify.Plan(request, plan, verify.RequireAll().WithConstraints(verifierPanic{}))
	if !result.Has(verify.CodeConstraint) || !errors.Is(result.Err(), constraint.ErrCallbackPanic) {
		t.Fatalf("violations=%+v error=%v", result.Violations(), result.Err())
	}
}

func TestVerifyDetectsPackedWeightAccumulationOverflow(t *testing.T) {
	t.Parallel()
	base := request(t)
	items := []knapsack.NormalizedItem{
		{ID: "a", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: math.MaxInt64, Orientations: []geometry.Orientation{geometry.OrientationXYZ}},
		{ID: "b", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1, Orientations: []geometry.Orientation{geometry.OrientationXYZ}},
	}
	containers := []knapsack.NormalizedContainer{{ID: "box", Dimensions: geometry.Dimensions{X: 2, Y: 1, Z: 1}, MaxContentWeight: math.MaxInt64, Stock: knapsack.UnlimitedStock()}}
	custom, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{Items: items, Containers: containers, Resolution: base.Resolution(), Limits: base.Limits()})
	if err != nil {
		t.Fatal(err)
	}
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	placements := []knapsack.Placement{
		{ItemID: "a", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: math.MaxInt64},
		{ItemID: "b", ContainerID: "box#1", Origin: geometry.Point{X: 1}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1},
	}
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{Containers: instances, Placements: placements, Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted})
	if result := verify.Plan(custom, plan, verify.RequireAll()); !result.Has(verify.CodeOverflow) {
		t.Fatalf("violations = %+v", result.Violations())
	}
}

func TestVerifyDetectsPackedVolumeAccumulationOverflow(t *testing.T) {
	t.Parallel()
	base := request(t)
	items := []knapsack.NormalizedItem{
		{ID: "a", Dimensions: geometry.Dimensions{X: math.MaxInt64, Y: 1, Z: 1}, Weight: 1, Orientations: []geometry.Orientation{geometry.OrientationXYZ}},
		{ID: "b", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1, Orientations: []geometry.Orientation{geometry.OrientationXYZ}},
	}
	containers := []knapsack.NormalizedContainer{
		{ID: "large", Dimensions: items[0].Dimensions, MaxContentWeight: 1, Stock: knapsack.UnlimitedStock()},
		{ID: "small", Dimensions: items[1].Dimensions, MaxContentWeight: 1, Stock: knapsack.UnlimitedStock()},
	}
	custom, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{Items: items, Containers: containers, Resolution: base.Resolution(), Limits: base.Limits()})
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{
		Containers: []knapsack.ContainerInstance{{ID: "large#1", TypeID: "large"}, {ID: "small#1", TypeID: "small"}},
		Placements: []knapsack.Placement{
			{ItemID: "a", ContainerID: "large#1", Orientation: geometry.OrientationXYZ, Dimensions: items[0].Dimensions, Weight: 1},
			{ItemID: "b", ContainerID: "small#1", Orientation: geometry.OrientationXYZ, Dimensions: items[1].Dimensions, Weight: 1},
		},
		Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted,
	})
	if result := verify.Plan(custom, plan, verify.RequireAll()); !result.Has(verify.CodeOverflow) {
		t.Fatalf("violations = %+v", result.Violations())
	}
}

func TestVerifyBoundsDiagnosticsForZeroValueRequest(t *testing.T) {
	t.Parallel()
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{Placements: []knapsack.Placement{{ItemID: "unknown"}, {ItemID: "also-unknown"}}, Status: knapsack.StatusBestKnown, Termination: knapsack.TerminationNoPlacement})
	result := verify.Plan(knapsack.NormalizedRequest{}, plan, verify.AllowUnpacked())
	if len(result.Violations()) != 1 || !result.Truncated() {
		t.Fatalf("violations=%+v truncated=%v", result.Violations(), result.Truncated())
	}
}
