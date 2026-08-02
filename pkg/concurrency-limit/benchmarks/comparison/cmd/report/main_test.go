package main

import (
	"slices"
	"testing"
)

func TestCampaignsCoverRequiredWorkloadClasses(t *testing.T) {
	t.Parallel()

	workloads := campaigns()
	names := make([]string, 0, len(workloads))
	seeds := make(map[int64]struct{}, len(workloads))
	for _, candidate := range workloads {
		names = append(names, candidate.name)
		if candidate.seed == 0 {
			t.Fatalf("%s has no reproducible seed", candidate.name)
		}
		if _, exists := seeds[candidate.seed]; exists {
			t.Fatalf("seed %d is reused", candidate.seed)
		}
		seeds[candidate.seed] = struct{}{}
	}

	want := []string{
		"constant", "bursty", "ramp", "bimodal", "heavy-tail",
		"periodic", "sparse", "capacity-collapse", "class-shift",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("campaigns = %v, want %v", names, want)
	}
}
