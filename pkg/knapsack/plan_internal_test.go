package knapsack

import (
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack/geometry"
)

func TestPlanRejectsInvalidStateAndAcceptsEveryDocumentedStatus(t *testing.T) {
	t.Parallel()
	for _, status := range []Status{StatusFeasible, StatusOptimal, StatusBestKnown, StatusInfeasible, StatusBudgetExhausted} {
		if _, err := NewPlan(PlanSpec{Status: status, Termination: TerminationCompleted}); err != nil {
			t.Fatalf("status %q rejected: %v", status, err)
		}
	}
	for _, spec := range []PlanSpec{
		{Status: "unknown", Termination: TerminationCompleted},
		{Status: StatusFeasible},
		{Status: StatusFeasible, Termination: "unknown"},
	} {
		if _, err := NewPlan(spec); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("spec %+v error = %v", spec, err)
		}
	}
}

func TestPlanOwnsAllCollectionState(t *testing.T) {
	t.Parallel()
	spec := PlanSpec{
		Containers: []ContainerInstance{{ID: "box#1", TypeID: "box"}},
		Placements: []Placement{{
			ItemID: "item", ContainerID: "box#1", Orientation: geometry.OrientationXYZ,
			Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1,
			SupporterIDs: []string{"floor"}, Diagnostics: []Diagnostic{{Code: "placement", Message: "kept"}},
		}},
		UnpackedItemIDs: []string{"other"},
		Objective:       []ScoreComponent{{Name: "boxes", Direction: "min", Unit: "count", Value: "1"}},
		Status:          StatusBestKnown,
		Termination:     TerminationNoPlacement,
		Statistics:      Statistics{PackedItems: 1},
		Work:            Work{Solver: "test"},
		Diagnostics:     []Diagnostic{{Code: "plan", Message: "kept"}},
	}
	plan, err := NewPlan(spec)
	if err != nil {
		t.Fatal(err)
	}
	spec.Containers[0].ID = "mutated"
	spec.Placements[0].SupporterIDs[0] = "mutated"
	spec.Placements[0].Diagnostics[0].Message = "mutated"
	spec.UnpackedItemIDs[0] = "mutated"
	spec.Objective[0].Value = "999"
	spec.Diagnostics[0].Message = "mutated"

	containers, placements := plan.Containers(), plan.Placements()
	unpacked, score, diagnostics := plan.UnpackedItemIDs(), plan.Objective(), plan.Diagnostics()
	containers[0].ID = "caller"
	placements[0].SupporterIDs[0] = "caller"
	placements[0].Diagnostics[0].Message = "caller"
	unpacked[0], score[0].Value, diagnostics[0].Message = "caller", "caller", "caller"
	if current := plan.Spec(); current.Containers[0].ID != "box#1" || current.Placements[0].SupporterIDs[0] != "floor" || current.Placements[0].Diagnostics[0].Message != "kept" || current.UnpackedItemIDs[0] != "other" || current.Objective[0].Value != "1" || current.Diagnostics[0].Message != "kept" {
		t.Fatalf("plan state was aliased: %+v", current)
	}
	if plan.Status() != StatusBestKnown || plan.Termination() != TerminationNoPlacement || plan.Statistics().PackedItems != 1 || plan.Work().Solver != "test" || !strings.Contains(plan.CanonicalString(), `"status":"best_known"`) {
		t.Fatalf("plan accessors changed: %s", plan.CanonicalString())
	}
}

func TestPlanConstructionIsBoundedBeforeCloning(t *testing.T) {
	t.Parallel()

	limits := DefaultPlanLimits()
	limits.MaxPlacements = 1
	if _, err := NewPlanWithLimits(PlanSpec{
		Placements:  []Placement{{ItemID: "a"}, {ItemID: "b"}},
		Status:      StatusBestKnown,
		Termination: TerminationNoPlacement,
	}, limits); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("placement limit error = %v", err)
	}
	limits = DefaultPlanLimits()
	limits.MaxMetadataBytes = 4
	if _, err := NewPlanWithLimits(PlanSpec{
		Diagnostics: []Diagnostic{{Code: "code", Message: "oversized"}},
		Status:      StatusBestKnown,
		Termination: TerminationNoPlacement,
	}, limits); !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("metadata limit error = %v", err)
	}
	if _, err := NewPlanWithLimits(PlanSpec{}, PlanLimits{}); !errors.Is(err, ErrInvalidOptions) {
		t.Fatalf("invalid limits error = %v", err)
	}
}

func TestPlanLimitsCoverNestedCollectionsAndMetadata(t *testing.T) {
	t.Parallel()

	base := PlanSpec{
		Containers: []ContainerInstance{{ID: "instance", TypeID: "type"}},
		Placements: []Placement{{
			ItemID: "item", ContainerID: "instance", SupporterIDs: []string{"supporter"},
			Diagnostics: []Diagnostic{{Code: "placement", ItemID: "item", ContainerID: "instance", Message: "message"}},
		}},
		UnpackedItemIDs: []string{"unpacked"},
		Objective:       []ScoreComponent{{Name: "metric", Direction: "min", Unit: "count", Value: "1"}},
		Status:          StatusBestKnown,
		Termination:     TerminationNoPlacement,
		Diagnostics:     []Diagnostic{{Code: "plan", ItemID: "item", ContainerID: "instance", Message: "message"}},
	}
	if _, err := NewPlanWithLimits(base, DefaultPlanLimits()); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*PlanSpec, *PlanLimits)
	}{
		{"bytes", func(_ *PlanSpec, limits *PlanLimits) { limits.MaxBytes = 1 }},
		{"container ID", func(spec *PlanSpec, limits *PlanLimits) {
			spec.Containers[0].ID = strings.Repeat("x", int(limits.MaxIDBytes)+1)
		}},
		{"placement ID", func(spec *PlanSpec, limits *PlanLimits) {
			spec.Placements[0].ItemID = strings.Repeat("x", int(limits.MaxIDBytes)+1)
		}},
		{"supporter ID", func(spec *PlanSpec, limits *PlanLimits) {
			spec.Placements[0].SupporterIDs[0] = strings.Repeat("x", int(limits.MaxIDBytes)+1)
		}},
		{"placement diagnostic", func(spec *PlanSpec, limits *PlanLimits) {
			spec.Placements[0].Diagnostics[0].Message = strings.Repeat("x", int(limits.MaxMetadataBytes)+1)
		}},
		{"diagnostic count", func(_ *PlanSpec, limits *PlanLimits) { limits.MaxDiagnostics = 1 }},
		{"unpacked ID", func(spec *PlanSpec, limits *PlanLimits) {
			spec.UnpackedItemIDs[0] = strings.Repeat("x", int(limits.MaxIDBytes)+1)
		}},
		{"objective metadata", func(spec *PlanSpec, limits *PlanLimits) {
			spec.Objective[0].Name = strings.Repeat("x", int(limits.MaxMetadataBytes)+1)
		}},
		{"plan diagnostic", func(spec *PlanSpec, limits *PlanLimits) {
			spec.Diagnostics[0].Message = strings.Repeat("x", int(limits.MaxMetadataBytes)+1)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := clonePlanSpec(base)
			limits := DefaultPlanLimits()
			test.mutate(&spec, &limits)
			if planWithinLimits(spec, limits) {
				t.Fatal("invalid nested plan accepted")
			}
		})
	}
}
