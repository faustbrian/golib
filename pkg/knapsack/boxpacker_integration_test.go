package knapsack_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

type boxPackerOutput struct {
	AdapterSchema          string `json:"adapter_schema"`
	Implementation         string `json:"implementation"`
	ImplementationVersion  string `json:"implementation_version"`
	ImplementationRevision string `json:"implementation_revision"`
	RuntimeVersion         string `json:"runtime_version"`
	Timing                 struct {
		ProcessStartupIncluded     bool  `json:"process_startup_included"`
		AutoloadAndFixtureIncluded bool  `json:"autoload_and_fixture_setup_included"`
		VerificationIncluded       bool  `json:"verification_included"`
		SolveNanoseconds           int64 `json:"solve_nanoseconds"`
	} `json:"timing"`
	Containers []struct {
		ID         string `json:"id"`
		TypeID     string `json:"type_id"`
		Placements []struct {
			ItemID string `json:"item_id"`
			X      int64  `json:"x"`
			Y      int64  `json:"y"`
			Z      int64  `json:"z"`
			Width  int64  `json:"width"`
			Length int64  `json:"length"`
			Depth  int64  `json:"depth"`
			Weight int64  `json:"weight"`
		} `json:"placements"`
	} `json:"containers"`
}

func TestBoxPackerCommonSubset(t *testing.T) {
	if os.Getenv("BOXPACKER_INTEGRATION") != "1" {
		t.Skip("run through make reference-integration")
	}
	request := boxPackerRequest(t)
	command := exec.Command("php", "integration/boxpacker/compare.php")
	encoded, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var output boxPackerOutput
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	if output.AdapterSchema != "v2" || output.Implementation != "dvdoug/BoxPacker" ||
		output.ImplementationVersion != "4.2.0" ||
		output.ImplementationRevision != "4fa822e71109095212a499572822c07bdb7228eb" ||
		output.RuntimeVersion == "" || output.Timing.SolveNanoseconds <= 0 ||
		output.Timing.ProcessStartupIncluded || output.Timing.AutoloadAndFixtureIncluded ||
		output.Timing.VerificationIncluded {
		t.Fatalf("invalid adapter provenance or timing disclosure: %+v", output)
	}
	plan := boxPackerPlan(t, request, output)
	if result := verify.Plan(request, plan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("BoxPacker returned an invalid common-subset plan: %+v", result.Violations())
	}
	goPlan, err := (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Statistics() != goPlan.Statistics() {
		t.Fatalf("semantic result differs: BoxPacker=%+v knapsack=%+v", plan.Statistics(), goPlan.Statistics())
	}
}

func boxPackerRequest(t *testing.T) knapsack.NormalizedRequest {
	t.Helper()
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
			MaxContentWeight: 2, Stock: knapsack.UnlimitedStock(),
		}},
		Resolution: knapsack.Resolution{
			Length: measurement.MustNew(decimal.New(1), measurement.Millimetre),
			Mass:   measurement.MustNew(decimal.New(1), measurement.Gram),
		},
		Limits: knapsack.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func boxPackerPlan(t *testing.T, request knapsack.NormalizedRequest, output boxPackerOutput) knapsack.Plan {
	t.Helper()
	spec := knapsack.PlanSpec{Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted}
	for _, container := range output.Containers {
		spec.Containers = append(spec.Containers, knapsack.ContainerInstance{ID: container.ID, TypeID: container.TypeID})
		for _, placement := range container.Placements {
			dimensions := geometry.Dimensions{X: placement.Width, Y: placement.Length, Z: placement.Depth}
			orientation := geometry.OrientationXYZ
			for _, candidate := range request.Items()[0].Orientations {
				if oriented, err := candidate.Apply(geometry.Dimensions{X: 2, Y: 1, Z: 1}); err == nil && oriented == dimensions {
					orientation = candidate
					break
				}
			}
			spec.Placements = append(spec.Placements, knapsack.Placement{
				ItemID: placement.ItemID, ContainerID: container.ID,
				Origin:      geometry.Point{X: placement.X, Y: placement.Y, Z: placement.Z},
				Orientation: orientation, Dimensions: dimensions, Weight: placement.Weight,
			})
		}
	}
	spec.Statistics = knapsack.Statistics{
		PackedItems: uint32(len(spec.Placements)), ContainerCount: uint32(len(spec.Containers)),
		ItemWeight: 2, ItemVolume: 4, ContainerVolume: int64(4 * len(spec.Containers)),
		RemainingWeight: int64(2*len(spec.Containers) - 2),
		RemainingVolume: int64(4*len(spec.Containers) - 4),
	}
	plan, err := knapsack.NewPlan(spec)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
