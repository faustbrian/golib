package main

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/failsafe-go/failsafe-go/adaptivelimiter"
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

func TestComparativeCampaignsUseCommonSuccessfulCompletionSemantics(t *testing.T) {
	t.Parallel()

	for _, candidate := range campaigns() {
		random := rand.New(rand.NewSource(candidate.seed))
		for window := range candidate.windows {
			demand, capacity, rtt := candidate.model(random, window)
			if demand < 0 || capacity < 1 || rtt < 0 {
				t.Fatalf("%s window %d invalid common observation = %d/%d/%s", candidate.name, window, demand, capacity, rtt)
			}
		}
	}
}

func TestFailsafeDriverLearnsFromOneAggregateSamplePerWindow(t *testing.T) {
	t.Parallel()

	permits := make([]*recordingPermit, 8)
	values := make([]adaptivelimiter.Permit, len(permits))
	for index := range permits {
		permits[index] = &recordingPermit{}
		values[index] = permits[index]
	}
	finishFailsafeAggregate(values)
	for index, permit := range permits {
		wantRecords := 0
		wantDrops := 1
		if index == len(permits)-1 {
			wantRecords = 1
			wantDrops = 0
		}
		if permit.records != wantRecords || permit.drops != wantDrops {
			t.Fatalf("permit %d records/drops = %d/%d, want %d/%d", index, permit.records, permit.drops, wantRecords, wantDrops)
		}
	}
}

type recordingPermit struct{ records, drops int }

func (permit *recordingPermit) Record() { permit.records++ }
func (permit *recordingPermit) Drop()   { permit.drops++ }
