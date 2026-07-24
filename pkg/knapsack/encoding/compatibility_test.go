package encoding_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	packingjson "github.com/faustbrian/golib/pkg/knapsack/encoding"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

func TestV1GoldenRequestAndPlanCompatibility(t *testing.T) {
	t.Parallel()
	request, plan := compatibilityArtifacts(t)
	assertGoldenCompatibility(t, "request.json", func() ([]byte, error) {
		return packingjson.MarshalRequest(request)
	}, func(input []byte) ([]byte, error) {
		decoded, err := packingjson.UnmarshalRequest(input, packingjson.DefaultLimits())
		if err != nil {
			return nil, err
		}

		return packingjson.MarshalRequest(decoded)
	})
	assertGoldenCompatibility(t, "plan.json", func() ([]byte, error) {
		return packingjson.MarshalPlan(plan)
	}, func(input []byte) ([]byte, error) {
		decoded, err := packingjson.UnmarshalPlan(input, packingjson.DefaultLimits())
		if err != nil {
			return nil, err
		}
		if result := verify.Plan(request, decoded, verify.RequireAll()); !result.Valid() {
			t.Fatalf("persisted v1 plan is invalid: %+v", result.Violations())
		}

		return packingjson.MarshalPlan(decoded)
	})
}

func TestUpdateV1GoldenCompatibility(t *testing.T) {
	if os.Getenv("UPDATE_GOLDEN") != "1" {
		t.Skip("set UPDATE_GOLDEN=1 to regenerate v1 compatibility fixtures")
	}
	request, plan := compatibilityArtifacts(t)
	if err := os.MkdirAll(filepath.Join("testdata", "v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, marshal := range map[string]func() ([]byte, error){
		"request.json": func() ([]byte, error) { return packingjson.MarshalRequest(request) },
		"plan.json":    func() ([]byte, error) { return packingjson.MarshalPlan(plan) },
	} {
		data, err := marshal()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join("testdata", "v1", name), append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func assertGoldenCompatibility(
	t *testing.T,
	name string,
	marshal func() ([]byte, error),
	decodeAndMarshal func([]byte) ([]byte, error),
) {
	t.Helper()
	want, err := os.ReadFile(filepath.Join("testdata", "v1", name))
	if err != nil {
		t.Fatal(err)
	}
	want = bytes.TrimSuffix(want, []byte{'\n'})
	current, err := marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, want) {
		t.Fatalf("v1 %s encoding changed without a schema version", name)
	}
	reencoded, err := decodeAndMarshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reencoded, want) {
		t.Fatalf("v1 %s no longer decodes canonically", name)
	}
}

func compatibilityArtifacts(t *testing.T) (knapsack.NormalizedRequest, knapsack.Plan) {
	t.Helper()
	quantity := func(value int64, unit measurement.Unit) measurement.Quantity {
		return measurement.MustNew(decimal.New(value), unit)
	}
	dimensions := knapsack.PhysicalDimensions{
		X: quantity(1, measurement.Metre),
		Y: quantity(2, measurement.Metre),
		Z: quantity(3, measurement.Metre),
	}
	item, err := knapsack.NewItem(knapsack.ItemSpec{
		ID: "item", Dimensions: dimensions, Weight: quantity(2, measurement.Kilogram),
		Orientations: []geometry.Orientation{geometry.OrientationXYZ},
		Attributes:   map[string]string{"class": "ordinary"},
	})
	if err != nil {
		t.Fatal(err)
	}
	container, err := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
		ID: "box", InternalDimensions: dimensions,
		MaxContentWeight: quantity(3, measurement.Kilogram), Stock: knapsack.FiniteStock(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	request, err := knapsack.NewRequest(
		[]knapsack.Item{item}, []knapsack.ContainerType{container},
		knapsack.Resolution{
			Length: quantity(1, measurement.Metre),
			Mass:   quantity(1, measurement.Kilogram),
		},
		knapsack.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := knapsack.NewPlan(knapsack.PlanSpec{
		Containers: []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}},
		Placements: []knapsack.Placement{{
			ItemID: "item", ContainerID: "box#1",
			Orientation: geometry.OrientationXYZ,
			Dimensions:  geometry.Dimensions{X: 1, Y: 2, Z: 3}, Weight: 2,
		}},
		UnpackedItemIDs: []string{}, Objective: []knapsack.ScoreComponent{},
		Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted,
		Statistics: knapsack.Statistics{
			PackedItems: 1, ContainerCount: 1, ItemWeight: 2,
			ItemVolume: 6, ContainerVolume: 6, RemainingWeight: 1,
		},
		Diagnostics: []knapsack.Diagnostic{},
	})
	if err != nil {
		t.Fatal(err)
	}

	return request.Normalized(), plan
}
