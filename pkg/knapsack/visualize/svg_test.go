package visualize_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/knapsack/visualize"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

func TestSVGConsumesOnlyVerifiedPlansAndEscapesLabels(t *testing.T) {
	t.Parallel()
	q := func(value int64, unit measurement.Unit) measurement.Quantity {
		return measurement.MustNew(decimal.New(value), unit)
	}
	dims := knapsack.PhysicalDimensions{X: q(1, measurement.Metre), Y: q(1, measurement.Metre), Z: q(1, measurement.Metre)}
	item, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "<item&>\"'\n", Dimensions: dims, Weight: q(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: dims, MaxContentWeight: q(1, measurement.Kilogram), Stock: knapsack.FiniteStock(1)})
	request, _ := knapsack.NewRequest([]knapsack.Item{item}, []knapsack.ContainerType{box}, knapsack.Resolution{Length: q(1, measurement.Metre), Mass: q(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{Containers: []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}, Placements: []knapsack.Placement{{ItemID: "<item&>\"'\n", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1}}, Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted, Statistics: knapsack.Statistics{PackedItems: 1, ContainerCount: 1, ItemWeight: 1, ItemVolume: 1, ContainerVolume: 1}})
	svg, err := visualize.SVG(request.Normalized(), plan, verify.RequireAll())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(svg, `<item&>`) || !strings.Contains(svg, "&lt;item&amp;&gt;&#34;&#39;\n") {
		t.Fatalf("unsafe SVG: %s", svg)
	}
	spec := plan.Spec()
	spec.Placements[0].Origin.X = 1
	bad, _ := knapsack.NewPlan(spec)
	if _, err := visualize.SVG(request.Normalized(), bad, verify.RequireAll()); err == nil {
		t.Fatal("invalid plan rendered")
	}
}

func TestSVGRejectsSceneCoordinateBudget(t *testing.T) {
	t.Parallel()
	q := func(value int64, unit measurement.Unit) measurement.Quantity {
		return measurement.MustNew(decimal.New(value), unit)
	}
	itemDimensions := knapsack.PhysicalDimensions{X: q(1, measurement.Metre), Y: q(1, measurement.Metre), Z: q(1, measurement.Metre)}
	item, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "item", Dimensions: itemDimensions, Weight: q(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	containerDimensions := knapsack.PhysicalDimensions{X: q(1_000_001, measurement.Metre), Y: q(1, measurement.Metre), Z: q(1, measurement.Metre)}
	container, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: containerDimensions, MaxContentWeight: q(1, measurement.Kilogram), Stock: knapsack.UnlimitedStock()})
	request, _ := knapsack.NewRequest([]knapsack.Item{item}, []knapsack.ContainerType{container}, knapsack.Resolution{Length: q(1, measurement.Metre), Mass: q(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{
		Containers: []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}, UnpackedItemIDs: []string{"item"},
		Status: knapsack.StatusBestKnown, Termination: knapsack.TerminationNoPlacement,
		Statistics: knapsack.Statistics{ContainerCount: 1, ContainerVolume: 1_000_001, RemainingVolume: 1_000_001, RemainingWeight: 1},
	})
	if _, err := visualize.SVG(request.Normalized(), plan, verify.AllowUnpacked()); !errors.Is(err, visualize.ErrRenderLimit) {
		t.Fatalf("error = %v", err)
	}
}

func TestSVGOffsetsMultipleContainersAndUsesMaximumDepth(t *testing.T) {
	t.Parallel()
	q := func(value int64, unit measurement.Unit) measurement.Quantity {
		return measurement.MustNew(decimal.New(value), unit)
	}
	dimensions := knapsack.PhysicalDimensions{X: q(1, measurement.Metre), Y: q(2, measurement.Metre), Z: q(1, measurement.Metre)}
	item, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "item", Dimensions: dimensions, Weight: q(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	container, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: dimensions, MaxContentWeight: q(1, measurement.Kilogram), Stock: knapsack.FiniteStock(2)})
	request, _ := knapsack.NewRequest([]knapsack.Item{item}, []knapsack.ContainerType{container}, knapsack.Resolution{Length: q(1, measurement.Metre), Mass: q(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{
		Containers: []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}, {ID: "box#2", TypeID: "box"}},
		Placements: []knapsack.Placement{{ItemID: "item", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 2, Z: 1}, Weight: 1}},
		Status:     knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted,
		Statistics: knapsack.Statistics{PackedItems: 1, ContainerCount: 2, ItemWeight: 1, ItemVolume: 2, ContainerVolume: 4, RemainingWeight: 1, RemainingVolume: 2},
	})
	svg, err := visualize.SVG(request.Normalized(), plan, verify.RequireAll())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(svg, `<g transform=`) != 2 || !strings.Contains(svg, `viewBox="0 0 32 22"`) {
		t.Fatalf("unexpected scene: %s", svg)
	}
}
