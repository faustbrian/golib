package references_test

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
	"github.com/gedex/bp3d"
	"github.com/jcoruiz/gopackx"
	"github.com/jcoruiz/gopackx/pkg/model"
)

// TestCommonIntegralSubset compares only behavior shared by all three
// libraries: orthogonal integral cuboids, unrestricted rotation, unlimited
// copies of one container type, weight capacity, and pack-all semantics.
// Reference float models are evidence of behavioral agreement, never the
// authoritative feasibility check for knapsack.
func TestCommonIntegralSubset(t *testing.T) {
	t.Parallel()

	request := commonRequest(t)
	plan, err := (solver.Heuristic{}).PackAll(
		context.Background(), request, solver.Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result := verify.Plan(request, plan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("knapsack returned an invalid plan: %+v", result.Violations())
	}
	if len(plan.Containers()) != 1 || len(plan.Placements()) != 2 {
		t.Fatalf("knapsack plan = %s", plan.CanonicalString())
	}

	gopackxResult, err := gopackx.Pack(
		context.Background(),
		[]*model.Bin{model.NewBin("box", 4, 1, 1, 10)},
		[]*model.Item{
			model.NewItem("a", 2, 1, 1, 1),
			model.NewItem("b", 2, 1, 1, 1),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(gopackxResult.Bins) != 1 || len(gopackxResult.UnfittedItems) != 0 ||
		len(gopackxResult.Bins[0].Items) != 2 {
		t.Fatalf("gopackx result = %+v", gopackxResult.Stats)
	}
	gopackxPlan := referencePlan(t, request, []referenceBin{{
		id: "box#1", typeID: "box", items: gopackxPlacements(t, gopackxResult.Bins[0]),
	}})
	if result := verify.Plan(request, gopackxPlan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("gopackx emitted invalid placements: %+v", result.Violations())
	}

	bp3dPacker := bp3d.NewPacker()
	bp3dPacker.AddBin(bp3d.NewBin("box", 4, 1, 1, 10))
	bp3dPacker.AddItem(
		bp3d.NewItem("a", 2, 1, 1, 1),
		bp3d.NewItem("b", 2, 1, 1, 1),
	)
	if err := bp3dPacker.Pack(); err != nil {
		t.Fatal(err)
	}
	if len(bp3dPacker.Bins) != 1 || len(bp3dPacker.UnfitItems) != 0 ||
		len(bp3dPacker.Bins[0].Items) != 2 {
		t.Fatalf("bp3d bins=%d unfit=%d", len(bp3dPacker.Bins), len(bp3dPacker.UnfitItems))
	}
	bp3dPlan := referencePlan(t, request, []referenceBin{{
		id: "box#1", typeID: "box", items: bp3dPlacements(t, bp3dPacker.Bins[0]),
	}})
	if result := verify.Plan(request, bp3dPlan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("bp3d emitted invalid placements: %+v", result.Violations())
	}
	if gopackxPlan.Statistics() != plan.Statistics() || bp3dPlan.Statistics() != plan.Statistics() {
		t.Fatalf("common-subset statistics disagree: go=%+v gopackx=%+v bp3d=%+v", plan.Statistics(), gopackxPlan.Statistics(), bp3dPlan.Statistics())
	}
}

type referenceItem struct {
	id          string
	origin      [3]float64
	dimensions  [3]float64
	weight      float64
	orientation int
}

type referenceBin struct {
	id     string
	typeID string
	items  []referenceItem
}

func gopackxPlacements(t *testing.T, bin *model.Bin) []referenceItem {
	t.Helper()
	result := make([]referenceItem, len(bin.Items))
	for index, item := range bin.Items {
		result[index] = referenceItem{item.ID, item.Position, item.PlacedDim, item.Weight, int(item.RotationType)}
	}
	return result
}

func bp3dPlacements(t *testing.T, bin *bp3d.Bin) []referenceItem {
	t.Helper()
	result := make([]referenceItem, len(bin.Items))
	for index, item := range bin.Items {
		dimensions := item.GetDimension()
		result[index] = referenceItem{
			id:         item.Name,
			origin:     [3]float64{item.Position[0], item.Position[1], item.Position[2]},
			dimensions: [3]float64{dimensions[0], dimensions[1], dimensions[2]},
			weight:     item.Weight, orientation: int(item.RotationType),
		}
	}
	return result
}

func referencePlan(t *testing.T, request knapsack.NormalizedRequest, bins []referenceBin) knapsack.Plan {
	t.Helper()
	spec := knapsack.PlanSpec{Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted}
	containerTypes := make(map[string]knapsack.NormalizedContainer)
	for _, container := range request.Containers() {
		containerTypes[container.ID] = container
	}
	containerWeights := make(map[string]int64)
	for _, bin := range bins {
		spec.Containers = append(spec.Containers, knapsack.ContainerInstance{ID: bin.id, TypeID: bin.typeID})
		container, ok := containerTypes[bin.typeID]
		if !ok {
			t.Fatalf("reference returned unknown container type %q", bin.typeID)
		}
		volume, err := container.Dimensions.Volume()
		if err != nil {
			t.Fatal(err)
		}
		spec.Statistics.ContainerVolume += volume
		for _, item := range bin.items {
			placement := knapsack.Placement{
				ItemID: item.id, ContainerID: bin.id,
				Origin:      geometry.Point{X: integral(t, item.origin[0]), Y: integral(t, item.origin[1]), Z: integral(t, item.origin[2])},
				Orientation: referenceOrientation(t, item.orientation),
				Dimensions:  geometry.Dimensions{X: integral(t, item.dimensions[0]), Y: integral(t, item.dimensions[1]), Z: integral(t, item.dimensions[2])},
				Weight:      integral(t, item.weight),
			}
			spec.Placements = append(spec.Placements, placement)
			itemVolume, volumeErr := placement.Dimensions.Volume()
			if volumeErr != nil {
				t.Fatal(volumeErr)
			}
			spec.Statistics.ItemVolume += itemVolume
			spec.Statistics.ItemWeight += placement.Weight
			containerWeights[bin.id] += placement.Weight
		}
		spec.Statistics.RemainingWeight += container.MaxContentWeight - containerWeights[bin.id]
	}
	spec.Statistics.PackedItems = uint32(len(spec.Placements))
	spec.Statistics.ContainerCount = uint32(len(spec.Containers))
	spec.Statistics.RemainingVolume = spec.Statistics.ContainerVolume - spec.Statistics.ItemVolume
	plan, err := knapsack.NewPlan(spec)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func integral(t *testing.T, value float64) int64 {
	t.Helper()
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < 0 || value > math.MaxInt64 {
		t.Fatalf("reference emitted non-integral common-subset value %v", value)
	}
	return int64(value)
}

func referenceOrientation(t *testing.T, rotation int) geometry.Orientation {
	t.Helper()
	orientations := [...]geometry.Orientation{
		geometry.OrientationXYZ,
		geometry.OrientationYXZ,
		geometry.OrientationYZX,
		geometry.OrientationZYX,
		geometry.OrientationZXY,
		geometry.OrientationXZY,
	}
	if rotation < 0 || rotation >= len(orientations) {
		t.Fatal(fmt.Errorf("reference emitted invalid rotation %d", rotation))
	}
	return orientations[rotation]
}

func commonRequest(t *testing.T) knapsack.NormalizedRequest {
	t.Helper()

	quantity := func(value int64, unit measurement.Unit) measurement.Quantity {
		return measurement.MustNew(decimal.New(value), unit)
	}
	dimensions := geometry.Dimensions{X: 2, Y: 1, Z: 1}
	orientations, err := geometry.Orientations(dimensions)
	if err != nil {
		t.Fatal(err)
	}
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: []knapsack.NormalizedItem{
			{ID: "a", Dimensions: dimensions, Weight: 1, Orientations: orientations},
			{ID: "b", Dimensions: dimensions, Weight: 1, Orientations: orientations},
		},
		Containers: []knapsack.NormalizedContainer{{
			ID: "box", Dimensions: geometry.Dimensions{X: 4, Y: 1, Z: 1},
			MaxContentWeight: 10, Stock: knapsack.UnlimitedStock(),
		}},
		Resolution: knapsack.Resolution{
			Length: quantity(1, measurement.Metre),
			Mass:   quantity(1, measurement.Kilogram),
		},
		Limits: knapsack.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}

	return request
}
