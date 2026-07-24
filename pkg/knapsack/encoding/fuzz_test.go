package encoding_test

import (
	"bytes"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	packingjson "github.com/faustbrian/golib/pkg/knapsack/encoding"
)

func FuzzPlanDecode(f *testing.F) {
	f.Add([]byte(`{"version":"v1","plan":{"status":"feasible","termination":"completed"}}`))
	f.Add([]byte(`{"version":"v1","version":"v1"}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 1<<20 {
			return
		}
		first, firstErr := packingjson.UnmarshalPlan(input, packingjson.DefaultLimits())
		second, secondErr := packingjson.UnmarshalPlan(input, packingjson.DefaultLimits())
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatal("decode result is nondeterministic")
		}
		if firstErr == nil {
			encoded, err := packingjson.MarshalPlan(first)
			if err != nil {
				t.Fatal(err)
			}
			replayed, err := packingjson.UnmarshalPlan(encoded, packingjson.DefaultLimits())
			if err != nil || !bytes.Equal(encoded, mustMarshalPlan(t, replayed)) || first.CanonicalString() != second.CanonicalString() {
				t.Fatalf("canonical plan replay failed: %v", err)
			}
		}
	})
}

func FuzzRequestDecode(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"version":"v1","version":"v1"}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 1<<20 {
			return
		}
		request, err := packingjson.UnmarshalRequest(input, packingjson.DefaultLimits())
		if err != nil {
			return
		}
		encoded, err := packingjson.MarshalRequest(request)
		if err != nil {
			t.Fatal(err)
		}
		replayed, err := packingjson.UnmarshalRequest(encoded, packingjson.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		reencoded, err := packingjson.MarshalRequest(replayed)
		if err != nil || !bytes.Equal(encoded, reencoded) {
			t.Fatalf("canonical request replay failed: %v", err)
		}
	})
}

func mustMarshalPlan(t *testing.T, plan knapsack.Plan) []byte {
	t.Helper()
	encoded, err := packingjson.MarshalPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
