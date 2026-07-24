package references_test

import (
	"encoding/json"
	"os/exec"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack/verify"
)

type comparisonAdapterOutput struct {
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

func TestGoComparisonAdapterCommonSubset(t *testing.T) {
	t.Parallel()

	encoded, err := exec.Command("go", "run", "./cmd/knapsack-compare").Output()
	if err != nil {
		t.Fatal(err)
	}
	var output comparisonAdapterOutput
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	if output.AdapterSchema != "v2" ||
		output.Implementation != "github.com/faustbrian/golib/pkg/knapsack" ||
		output.ImplementationVersion == "" || output.ImplementationRevision == "" ||
		output.RuntimeVersion == "" || output.Timing.SolveNanoseconds <= 0 ||
		output.Timing.ProcessStartupIncluded || output.Timing.AutoloadAndFixtureIncluded ||
		output.Timing.VerificationIncluded {
		t.Fatalf("invalid adapter provenance or timing disclosure: %+v", output)
	}
	request := commonRequest(t)
	bins := make([]referenceBin, len(output.Containers))
	for binIndex, container := range output.Containers {
		bins[binIndex] = referenceBin{id: container.ID, typeID: container.TypeID}
		for _, placement := range container.Placements {
			bins[binIndex].items = append(bins[binIndex].items, referenceItem{
				id: placement.ItemID,
				origin: [3]float64{
					float64(placement.X), float64(placement.Y), float64(placement.Z),
				},
				dimensions: [3]float64{
					float64(placement.Width), float64(placement.Length), float64(placement.Depth),
				},
				weight: float64(placement.Weight),
			})
		}
	}
	plan := referencePlan(t, request, bins)
	if result := verify.Plan(request, plan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("Go adapter returned an invalid plan: %+v", result.Violations())
	}
}
