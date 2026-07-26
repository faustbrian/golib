package constraint_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/constraint"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
)

type mutatingConstraint struct{}

func (mutatingConstraint) Check(_ context.Context, view constraint.PlacementView) constraint.Decision {
	item := view.Item()
	item.Attributes["changed"] = "yes"
	placements := view.Placements()
	placements[0].ItemID = "changed"
	return constraint.Accept()
}

type panicConstraint struct{}

func (panicConstraint) Check(context.Context, constraint.PlacementView) constraint.Decision {
	panic("boom")
}

type pointerConstraint struct{}

func (*pointerConstraint) Check(context.Context, constraint.PlacementView) constraint.Decision {
	return constraint.Accept()
}

type mutatingCenterOfGravityConstraint struct{}

func (mutatingCenterOfGravityConstraint) Check(_ context.Context, view constraint.PlacementView) constraint.Decision {
	container := view.Container()
	container.CenterOfGravity.MinXPPM = 900_000

	return constraint.Accept()
}

func TestPlacementViewDefensivelyCopiesState(t *testing.T) {
	t.Parallel()
	maximum := int64(1)
	item := knapsack.NormalizedItem{
		ID: "candidate", SKU: "sku", Group: "group",
		Dimensions:         geometry.Dimensions{X: 1, Y: 1, Z: 1},
		Orientations:       []geometry.Orientation{geometry.OrientationXYZ},
		Attributes:         map[string]string{"original": "yes"},
		IncompatibleGroups: []string{"other"}, MaxSupportedWeight: &maximum,
	}
	diagnostic := knapsack.Diagnostic{Code: "code", ItemID: "item", ContainerID: "box", Message: "message"}
	placements := []knapsack.Placement{{
		ItemID: "existing", ContainerID: "box",
		SupporterIDs: []string{"supporter"}, Diagnostics: []knapsack.Diagnostic{diagnostic},
	}}
	container := knapsack.NormalizedContainer{ID: "box", AllowedClasses: []string{"class"}}
	candidate := knapsack.Placement{
		ItemID: "candidate", ContainerID: "box",
		SupporterIDs: []string{"supporter"}, Diagnostics: []knapsack.Diagnostic{diagnostic},
	}
	view, err := constraint.NewPlacementView(item, container, candidate, placements)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := constraint.Evaluate(context.Background(), mutatingConstraint{}, view)
	if err != nil || !decision.Accepted {
		t.Fatalf("decision=%+v error=%v", decision, err)
	}
	if item.Attributes["changed"] != "" || placements[0].ItemID != "existing" {
		t.Fatal("callback mutated caller state")
	}
}

func TestPlacementViewDefensivelyCopiesCenterOfGravity(t *testing.T) {
	t.Parallel()

	bounds := &knapsack.CenterOfGravityBounds{
		MinXPPM: 100_000, MaxXPPM: 900_000,
		MaxYPPM: 1_000_000, MaxZPPM: 1_000_000,
	}
	container := knapsack.NormalizedContainer{ID: "box", CenterOfGravity: bounds}
	view, err := constraint.NewPlacementView(knapsack.NormalizedItem{ID: "item"}, container, knapsack.Placement{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := constraint.Evaluate(context.Background(), mutatingCenterOfGravityConstraint{}, view)
	if err != nil || !decision.Accepted {
		t.Fatalf("decision=%+v error=%v", decision, err)
	}
	if bounds.MinXPPM != 100_000 {
		t.Fatalf("callback mutated caller bounds: %+v", bounds)
	}
	if got := view.Container().CenterOfGravity.MinXPPM; got != 100_000 {
		t.Fatalf("callback mutated immutable view bounds: %d", got)
	}
}

func TestPlacementViewRejectsUnboundedInput(t *testing.T) {
	t.Parallel()

	placements := make([]knapsack.Placement, 10_001)
	if _, err := constraint.NewPlacementView(
		knapsack.NormalizedItem{}, knapsack.NormalizedContainer{},
		knapsack.Placement{}, placements,
	); !errors.Is(err, constraint.ErrViewLimit) {
		t.Fatalf("placement count error = %v", err)
	}
	oversized := strings.Repeat("x", 16<<20)
	tests := []struct {
		name       string
		item       knapsack.NormalizedItem
		container  knapsack.NormalizedContainer
		candidate  knapsack.Placement
		placements []knapsack.Placement
	}{
		{name: "attribute", item: knapsack.NormalizedItem{Attributes: map[string]string{"oversized": oversized}}},
		{name: "incompatible group", item: knapsack.NormalizedItem{IncompatibleGroups: []string{oversized}}},
		{name: "allowed class", container: knapsack.NormalizedContainer{AllowedClasses: []string{oversized}}},
		{name: "candidate identity", candidate: knapsack.Placement{ItemID: oversized}},
		{name: "candidate supporter", candidate: knapsack.Placement{SupporterIDs: []string{oversized}}},
		{name: "candidate diagnostic", candidate: knapsack.Placement{Diagnostics: []knapsack.Diagnostic{{Message: oversized}}}},
		{name: "existing placement", placements: []knapsack.Placement{{Diagnostics: []knapsack.Diagnostic{{Message: oversized}}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := constraint.NewPlacementView(
				test.item, test.container, test.candidate, test.placements,
			); !errors.Is(err, constraint.ErrViewLimit) {
				t.Fatalf("view memory error = %v", err)
			}
		})
	}
}

func TestConstraintPanicBecomesStableError(t *testing.T) {
	t.Parallel()
	_, err := constraint.Evaluate(context.Background(), panicConstraint{}, constraint.PlacementView{})
	if !errors.Is(err, constraint.ErrCallbackPanic) {
		t.Fatalf("error = %v", err)
	}
}

func TestDecisionRequiresBoundedDiagnostic(t *testing.T) {
	t.Parallel()
	if _, err := constraint.ValidateDecision(constraint.Reject("", "")); !errors.Is(err, constraint.ErrInvalidDecision) {
		t.Fatalf("error = %v", err)
	}
	if _, err := constraint.ValidateDecision(constraint.Reject("code", string(make([]byte, 5000)))); !errors.Is(err, constraint.ErrInvalidDecision) {
		t.Fatalf("error = %v", err)
	}
}

func TestConstraintBoundaryRejectsInvalidCallbacksAndDecisions(t *testing.T) {
	t.Parallel()
	view, err := constraint.NewPlacementView(
		knapsack.NormalizedItem{ID: "item"},
		knapsack.NormalizedContainer{ID: "box"},
		knapsack.Placement{ItemID: "item"},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if view.Container().ID != "box" || view.Candidate().ItemID != "item" {
		t.Fatal("view accessors changed")
	}
	var nilContext context.Context
	if _, err := constraint.Evaluate(nilContext, mutatingConstraint{}, view); !errors.Is(err, constraint.ErrInvalidConstraint) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := constraint.Evaluate(context.Background(), nil, view); !errors.Is(err, constraint.ErrInvalidConstraint) {
		t.Fatalf("nil callback error = %v", err)
	}
	var typedNil *pointerConstraint
	if _, err := constraint.Evaluate(context.Background(), typedNil, view); !errors.Is(err, constraint.ErrInvalidConstraint) {
		t.Fatalf("typed nil error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := constraint.Evaluate(ctx, mutatingConstraint{}, view); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
	if _, err := constraint.ValidateDecision(constraint.Decision{Accepted: true, Code: "unexpected"}); !errors.Is(err, constraint.ErrInvalidDecision) {
		t.Fatalf("accepted diagnostic error = %v", err)
	}
	decision, err := constraint.ValidateDecision(constraint.Reject("rejected", "bounded message"))
	if err != nil || decision.Accepted {
		t.Fatalf("decision=%+v error=%v", decision, err)
	}
}

func TestCallbackListsRejectExcessAndTypedNilValues(t *testing.T) {
	t.Parallel()

	callbacks := make([]constraint.Placement, 33)
	for index := range callbacks {
		callbacks[index] = mutatingConstraint{}
	}
	if err := constraint.ValidateCallbacks(callbacks); !errors.Is(err, constraint.ErrInvalidConstraint) {
		t.Fatalf("excess callback error = %v", err)
	}
	var typedNil *pointerConstraint
	if err := constraint.ValidateCallbacks([]constraint.Placement{typedNil}); !errors.Is(err, constraint.ErrInvalidConstraint) {
		t.Fatalf("typed nil callback error = %v", err)
	}
	if err := constraint.ValidateCallbacks([]constraint.Placement{nil}); !errors.Is(err, constraint.ErrInvalidConstraint) {
		t.Fatalf("nil callback error = %v", err)
	}
	if err := constraint.ValidateCallbacks(callbacks[:32]); err != nil {
		t.Fatalf("bounded callbacks rejected: %v", err)
	}
}
