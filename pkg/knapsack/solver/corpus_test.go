package solver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/measurement"
)

type corpusBoxType struct {
	ID         int      `json:"id"`
	Dimensions [3]int64 `json:"dimensions"`
	Quantity   int      `json:"quantity"`
}

type corpusFixture struct {
	Name                        string          `json:"name"`
	Source                      string          `json:"source"`
	SourceRevision              string          `json:"source_revision"`
	SourceSHA256                string          `json:"source_sha256"`
	License                     string          `json:"license"`
	LicenseSHA256               string          `json:"license_sha256"`
	Container                   [3]int64        `json:"container"`
	BoxTypes                    []corpusBoxType `json:"box_types"`
	ExpectedHeuristicUpperBound int             `json:"expected_heuristic_upper_bound"`
}

func TestDWaveSampleData1(t *testing.T) {
	t.Parallel()
	input, err := os.ReadFile("testdata/corpus/dwave-sample-data-1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture corpusFixture
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.SourceRevision != "c64e4974859fa4638588b4174d4c6bd31e0b91af" ||
		fixture.SourceSHA256 != "a1d2dcc3eb8424e25cd89d150bc5bc1ae7704c985d6fa19112913d9bb778d951" ||
		fixture.License != "Apache-2.0" ||
		fixture.LicenseSHA256 != "58d1e17ffe5109a7ae296caafcadfdbe6a7d176f0bc4ab01e12a689b0499d8bd" {
		t.Fatalf("unexpected corpus provenance: %+v", fixture)
	}
	items := make([]knapsack.NormalizedItem, 0)
	for _, boxType := range fixture.BoxTypes {
		dimensions := geometry.Dimensions{X: boxType.Dimensions[0], Y: boxType.Dimensions[1], Z: boxType.Dimensions[2]}
		orientations, err := geometry.Orientations(dimensions)
		if err != nil {
			t.Fatal(err)
		}
		for instance := 1; instance <= boxType.Quantity; instance++ {
			items = append(items, knapsack.NormalizedItem{
				ID:         fmt.Sprintf("type-%02d#%06d", boxType.ID, instance),
				Dimensions: dimensions, Weight: 1, Orientations: orientations,
			})
		}
	}
	limits := knapsack.DefaultLimits()
	limits.MaxCandidatePlacements = 20_000_000
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: items,
		Containers: []knapsack.NormalizedContainer{{
			ID: "dwave-sample-data-1", Dimensions: geometry.Dimensions{X: fixture.Container[0], Y: fixture.Container[1], Z: fixture.Container[2]},
			MaxContentWeight: int64(len(items)), Stock: knapsack.UnlimitedStock(),
		}},
		Resolution: knapsack.Resolution{Length: quantity(1, measurement.Metre), Mass: quantity(1, measurement.Kilogram)},
		Limits:     limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result := verify.Plan(request, plan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("invalid corpus plan: %+v", result.Violations())
	}
	if got := len(plan.Containers()); got != fixture.ExpectedHeuristicUpperBound {
		t.Fatalf("container count = %d, expected reproducible upper bound %d", got, fixture.ExpectedHeuristicUpperBound)
	}
}
