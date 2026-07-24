package encoding_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	packingjson "github.com/faustbrian/golib/pkg/knapsack/encoding"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

func TestPlanCanonicalRoundTrip(t *testing.T) {
	t.Parallel()
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{
		Containers: []knapsack.ContainerInstance{{ID: "box#000001", TypeID: "box"}},
		Placements: []knapsack.Placement{{ItemID: "item", ContainerID: "box#000001", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1}},
		Status:     knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted,
		Statistics: knapsack.Statistics{PackedItems: 1, ContainerCount: 1, ItemWeight: 1, ItemVolume: 1, ContainerVolume: 1},
	})
	encoded, err := packingjson.MarshalPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := packingjson.UnmarshalPlan(encoded, packingjson.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	reencoded, _ := packingjson.MarshalPlan(decoded)
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("non-canonical round trip\n%s\n%s", encoded, reencoded)
	}
}

func TestNormalizedRequestCanonicalRoundTrip(t *testing.T) {
	t.Parallel()
	q := func(value int64, unit measurement.Unit) measurement.Quantity {
		return measurement.MustNew(decimal.New(value), unit)
	}
	dims := knapsack.PhysicalDimensions{X: q(1, measurement.Metre), Y: q(2, measurement.Metre), Z: q(3, measurement.Metre)}
	item, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "item", Dimensions: dims, Weight: q(2, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}, Attributes: map[string]string{"class": "ordinary"}})
	box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: dims, MaxContentWeight: q(3, measurement.Kilogram), Stock: knapsack.FiniteStock(1)})
	request, _ := knapsack.NewRequest([]knapsack.Item{item}, []knapsack.ContainerType{box}, knapsack.Resolution{Length: q(1, measurement.Metre), Mass: q(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	encoded, err := packingjson.MarshalRequest(request.Normalized())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := packingjson.UnmarshalRequest(encoded, packingjson.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	reencoded, _ := packingjson.MarshalRequest(decoded)
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("non-canonical request round trip\n%s\n%s", encoded, reencoded)
	}
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{Containers: []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}, Placements: []knapsack.Placement{{ItemID: "item", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 2, Z: 3}, Weight: 2}}, Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted, Statistics: knapsack.Statistics{PackedItems: 1, ContainerCount: 1, ItemWeight: 2, ItemVolume: 6, ContainerVolume: 6, RemainingWeight: 1}})
	planJSON, _ := packingjson.MarshalPlan(plan)
	decodedPlan, err := packingjson.UnmarshalPlan(planJSON, packingjson.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if result := verify.Plan(decoded, decodedPlan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("freshly decoded plan failed verification: %+v", result.Violations())
	}
}

func TestStrictDecodeRejectsUnknownDuplicateAndOversizedInput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		limits packingjson.Limits
		target error
	}{
		{"unknown", `{"version":"v1","plan":{"status":"feasible","termination":"completed"},"extra":true}`, packingjson.DefaultLimits(), packingjson.ErrInvalidEncoding},
		{"duplicate", `{"version":"v1","version":"v1","plan":{"status":"feasible","termination":"completed"}}`, packingjson.DefaultLimits(), packingjson.ErrDuplicateKey},
		{"oversized", `{"version":"v1","plan":{"status":"feasible","termination":"completed"}}`, packingjson.Limits{MaxBytes: 8, MaxDepth: 8, MaxCollection: 8}, packingjson.ErrEncodingLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := packingjson.UnmarshalPlan([]byte(test.input), test.limits); !errors.Is(err, test.target) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestStrictDecodeRejectsInvalidUTF8(t *testing.T) {
	t.Parallel()

	input := append([]byte(`{"version":"v1","plan":{"status":"feasible","termination":"completed","diagnostics":[{"code":"x","message":"`), 0xff)
	input = append(input, []byte(`"}]}}`)...)
	if _, err := packingjson.UnmarshalPlan(input, packingjson.DefaultLimits()); !errors.Is(err, packingjson.ErrInvalidEncoding) {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeRejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()
	_, err := packingjson.UnmarshalPlan([]byte(`{"version":"v2","plan":{"status":"feasible","termination":"completed"}}`), packingjson.DefaultLimits())
	if !errors.Is(err, packingjson.ErrUnsupportedVersion) {
		t.Fatalf("error = %v", err)
	}
}

func TestRequestRoundTripPreservesCenterOfGravityBounds(t *testing.T) {
	t.Parallel()

	request, _ := compatibilityArtifacts(t)
	spec := knapsack.NormalizedSpec{
		Items: request.Items(), Containers: request.Containers(),
		Resolution: request.Resolution(), Limits: request.Limits(),
	}
	spec.Containers[0].CenterOfGravity = &knapsack.CenterOfGravityBounds{
		MinXPPM: 100_000, MaxXPPM: 900_000,
		MinYPPM: 200_000, MaxYPPM: 800_000,
		MinZPPM: 300_000, MaxZPPM: 700_000,
	}
	request, err := knapsack.NewNormalizedRequest(spec)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := packingjson.MarshalRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := packingjson.UnmarshalRequest(encoded, packingjson.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Containers()[0].CenterOfGravity; got == nil || *got != *spec.Containers[0].CenterOfGravity {
		t.Fatalf("decoded center-of-gravity bounds = %+v", got)
	}
}

func TestRequestRoundTripPreservesUnlimitedStockGrossWeightAndReservedSpace(t *testing.T) {
	t.Parallel()
	q := func(value int64, unit measurement.Unit) measurement.Quantity {
		return measurement.MustNew(decimal.New(value), unit)
	}
	dimensions := knapsack.PhysicalDimensions{X: q(4, measurement.Metre), Y: q(4, measurement.Metre), Z: q(4, measurement.Metre)}
	item, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "item", Dimensions: dimensions, Weight: q(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	tare, gross := q(1, measurement.Kilogram), q(3, measurement.Kilogram)
	container, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{
		ID: "box", InternalDimensions: dimensions, MaxContentWeight: q(2, measurement.Kilogram),
		TareWeight: &tare, MaxGrossWeight: &gross, Stock: knapsack.UnlimitedStock(),
		Reserved: []knapsack.ReservedRegion{{Origin: geometry.Point{X: 1}, Dimensions: knapsack.PhysicalDimensions{X: q(1, measurement.Metre), Y: q(1, measurement.Metre), Z: q(1, measurement.Metre)}}},
	})
	request, err := knapsack.NewRequest([]knapsack.Item{item}, []knapsack.ContainerType{container}, knapsack.Resolution{Length: q(1, measurement.Metre), Mass: q(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := packingjson.MarshalRequest(request.Normalized())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := packingjson.UnmarshalRequest(encoded, packingjson.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	got := decoded.Containers()[0]
	if !got.Stock.Unlimited() || !got.HasGrossWeight || len(got.Reserved) != 1 {
		t.Fatalf("container = %+v", got)
	}
}

func TestStrictDecoderRejectsMalformedResourceAndSemanticInputs(t *testing.T) {
	t.Parallel()
	defaultLimits := packingjson.DefaultLimits()
	tests := []struct {
		name   string
		input  string
		limits packingjson.Limits
		target error
	}{
		{"invalid limits", `{}`, packingjson.Limits{}, packingjson.ErrEncodingLimit},
		{"empty", ``, defaultLimits, packingjson.ErrInvalidEncoding},
		{"malformed", `{`, defaultLimits, packingjson.ErrInvalidEncoding},
		{"malformed object key", `{"`, defaultLimits, packingjson.ErrInvalidEncoding},
		{"missing object value", `{"a":`, defaultLimits, packingjson.ErrInvalidEncoding},
		{"missing object close", `{"a":1`, defaultLimits, packingjson.ErrInvalidEncoding},
		{"missing array close", `[1`, defaultLimits, packingjson.ErrInvalidEncoding},
		{"malformed array value", `["`, defaultLimits, packingjson.ErrInvalidEncoding},
		{"nested missing close", `{"a":[1`, defaultLimits, packingjson.ErrInvalidEncoding},
		{"trailing", `{"version":"v1","plan":{"status":"feasible","termination":"completed"}} {}`, defaultLimits, packingjson.ErrInvalidEncoding},
		{"invalid trailing token", `{"version":"v1","plan":{"status":"feasible","termination":"completed"}} x`, defaultLimits, packingjson.ErrInvalidEncoding},
		{"depth", `[[[]]]`, packingjson.Limits{MaxBytes: 100, MaxDepth: 2, MaxCollection: 10}, packingjson.ErrEncodingLimit},
		{"object collection", `{"a":1,"b":2}`, packingjson.Limits{MaxBytes: 100, MaxDepth: 10, MaxCollection: 1}, packingjson.ErrEncodingLimit},
		{"array collection", `[1,2]`, packingjson.Limits{MaxBytes: 100, MaxDepth: 10, MaxCollection: 1}, packingjson.ErrEncodingLimit},
		{"invalid plan", `{"version":"v1","plan":{"status":"unknown","termination":"completed"}}`, defaultLimits, packingjson.ErrInvalidEncoding},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := packingjson.UnmarshalPlan([]byte(test.input), test.limits); !errors.Is(err, test.target) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRequestDecoderRejectsVersionQuantityGeometryAndRequestErrors(t *testing.T) {
	t.Parallel()
	q := func(value int64, unit measurement.Unit) measurement.Quantity {
		return measurement.MustNew(decimal.New(value), unit)
	}
	dimensions := knapsack.PhysicalDimensions{X: q(2, measurement.Metre), Y: q(2, measurement.Metre), Z: q(2, measurement.Metre)}
	item, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "item", Dimensions: dimensions, Weight: q(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	container, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: dimensions, MaxContentWeight: q(2, measurement.Kilogram), Stock: knapsack.FiniteStock(1)})
	request, _ := knapsack.NewRequest([]knapsack.Item{item}, []knapsack.ContainerType{container}, knapsack.Resolution{Length: q(1, measurement.Metre), Mass: q(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	encoded, _ := packingjson.MarshalRequest(request.Normalized())
	var envelope map[string]any
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(map[string]any){
		"version": func(value map[string]any) { value["version"] = "v2" },
		"amount": func(value map[string]any) {
			resolution, _ := value["resolution"].(map[string]any)
			length, _ := resolution["length"].(map[string]any)
			length["amount"] = "bad"
		},
		"unit": func(value map[string]any) {
			resolution, _ := value["resolution"].(map[string]any)
			mass, _ := resolution["mass"].(map[string]any)
			mass["unit"] = "invalid"
		},
		"request": func(value map[string]any) { value["items"] = []any{} },
		"reserved": func(value map[string]any) {
			containers, _ := value["containers"].([]any)
			container, _ := containers[0].(map[string]any)
			container["reserved"] = []any{map[string]any{"origin": map[string]any{"X": 2.0}, "dimensions": map[string]any{"X": 1.0, "Y": 1.0, "Z": 1.0}}}
		},
		"reserved coordinate": func(value map[string]any) {
			containers, _ := value["containers"].([]any)
			container, _ := containers[0].(map[string]any)
			container["reserved"] = []any{map[string]any{"origin": map[string]any{"X": -1.0}, "dimensions": map[string]any{"X": 1.0, "Y": 1.0, "Z": 1.0}}}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			copyInput, _ := json.Marshal(envelope)
			var changed map[string]any
			_ = json.Unmarshal(copyInput, &changed)
			mutate(changed)
			input, _ := json.Marshal(changed)
			if _, err := packingjson.UnmarshalRequest(input, packingjson.DefaultLimits()); err == nil {
				t.Fatal("invalid request decoded")
			}
		})
	}
	if _, err := packingjson.UnmarshalRequest(encoded, packingjson.Limits{}); !errors.Is(err, packingjson.ErrEncodingLimit) {
		t.Fatalf("request limits error = %v", err)
	}
	if _, err := packingjson.UnmarshalRequest([]byte(`{"version":1}`), packingjson.DefaultLimits()); !errors.Is(err, packingjson.ErrInvalidEncoding) {
		t.Fatalf("request type error = %v", err)
	}
	if _, err := packingjson.UnmarshalRequest([]byte(`{`), packingjson.DefaultLimits()); !errors.Is(err, packingjson.ErrInvalidEncoding) {
		t.Fatalf("request JSON error = %v", err)
	}
}
