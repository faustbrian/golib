package verify_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/knapsack"
	packingjson "github.com/faustbrian/golib/pkg/knapsack/encoding"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/objective"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

type cancelAfterChecks struct{ remaining int }

func (*cancelAfterChecks) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelAfterChecks) Done() <-chan struct{}       { return nil }
func (*cancelAfterChecks) Value(any) any               { return nil }
func (c *cancelAfterChecks) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func q(value int64, unit measurement.Unit) measurement.Quantity {
	return measurement.MustNew(decimal.New(value), unit)
}

func request(t *testing.T) knapsack.NormalizedRequest {
	t.Helper()
	dims := knapsack.PhysicalDimensions{X: q(2, measurement.Metre), Y: q(2, measurement.Metre), Z: q(2, measurement.Metre)}
	itemA, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "a", Dimensions: dims, Weight: q(2, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	itemB, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "b", Dimensions: dims, Weight: q(2, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
	container, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: knapsack.PhysicalDimensions{X: q(4, measurement.Metre), Y: q(2, measurement.Metre), Z: q(2, measurement.Metre)}, MaxContentWeight: q(4, measurement.Kilogram), Stock: knapsack.FiniteStock(1)})
	r, err := knapsack.NewRequest([]knapsack.Item{itemA, itemB}, []knapsack.ContainerType{container}, knapsack.Resolution{Length: q(1, measurement.Metre), Mass: q(1, measurement.Kilogram)}, knapsack.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	return r.Normalized()
}

func validPlan(t *testing.T) knapsack.Plan {
	t.Helper()
	plan, err := knapsack.NewPlan(knapsack.PlanSpec{
		Containers: []knapsack.ContainerInstance{{ID: "box#000001", TypeID: "box"}},
		Placements: []knapsack.Placement{
			{ItemID: "a", ContainerID: "box#000001", Origin: geometry.Point{}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 2, Y: 2, Z: 2}, Weight: 2},
			{ItemID: "b", ContainerID: "box#000001", Origin: geometry.Point{X: 2}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 2, Y: 2, Z: 2}, Weight: 2},
		},
		Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted,
		Statistics: knapsack.Statistics{PackedItems: 2, ContainerCount: 1, ItemWeight: 4, ItemVolume: 16, ContainerVolume: 16, RemainingWeight: 0, RemainingVolume: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func TestVerifyAcceptsCompleteFeasiblePlan(t *testing.T) {
	t.Parallel()
	result := verify.Plan(request(t), validPlan(t), verify.RequireAll())
	if !result.Valid() || len(result.Violations()) != 0 {
		t.Fatalf("verification failed: %+v", result.Violations())
	}
}

func TestVerifyRejectsOverlapAndAccountingDrift(t *testing.T) {
	t.Parallel()
	base := validPlan(t)
	spec := base.Spec()
	spec.Placements[1].Origin.X = 1
	spec.Statistics.ItemVolume--
	bad, _ := knapsack.NewPlan(spec)
	result := verify.Plan(request(t), bad, verify.RequireAll())
	if result.Valid() {
		t.Fatal("invalid plan accepted")
	}
	if !result.Has(verify.CodeOverlap) || !result.Has(verify.CodeAccounting) {
		t.Fatalf("violations = %+v", result.Violations())
	}
}

func TestVerifyRejectsMissingDuplicateBoundaryAndWeight(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*knapsack.PlanSpec)
		code   verify.Code
	}{
		{"missing", func(s *knapsack.PlanSpec) { s.Placements = s.Placements[:1]; s.UnpackedItemIDs = []string{"b"} }, verify.CodeMissingItem},
		{"duplicate", func(s *knapsack.PlanSpec) { s.Placements[1].ItemID = "a" }, verify.CodeDuplicateItem},
		{"boundary", func(s *knapsack.PlanSpec) { s.Placements[1].Origin.X = 3 }, verify.CodeOutsideContainer},
		{"weight", func(s *knapsack.PlanSpec) { s.Placements[1].Weight = 3 }, verify.CodeAlteredItem},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := validPlan(t).Spec()
			test.mutate(&spec)
			bad, err := knapsack.NewPlan(spec)
			if err != nil && !errors.Is(err, knapsack.ErrInvalidRequest) {
				t.Fatal(err)
			}
			result := verify.Plan(request(t), bad, verify.RequireAll())
			if !result.Has(test.code) {
				t.Fatalf("violations = %+v", result.Violations())
			}
		})
	}
}

func TestVerifyBoundsViolationDiagnostics(t *testing.T) {
	t.Parallel()
	request := request(t)
	limits := request.Limits()
	limits.MaxDiagnostics = 2
	request = request.WithLimits(limits)
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{
		Placements: []knapsack.Placement{
			{ItemID: "unknown-1"},
			{ItemID: "unknown-2"},
			{ItemID: "unknown-3"},
		},
		Status: knapsack.StatusBestKnown, Termination: knapsack.TerminationNoPlacement,
	})
	result := verify.Plan(request, plan, verify.AllowUnpacked())
	if len(result.Violations()) != 2 || !result.Truncated() {
		t.Fatalf("violations=%+v truncated=%v", result.Violations(), result.Truncated())
	}
}

func TestVerifyRecomputesConfiguredObjective(t *testing.T) {
	t.Parallel()

	request := request(t)
	goal, err := objective.New(objective.Minimize(objective.ContainerCount), objective.Minimize(objective.UnusedVolume))
	if err != nil {
		t.Fatal(err)
	}
	spec := validPlan(t).Spec()
	spec.Objective = []knapsack.ScoreComponent{
		{Name: "container_count", Direction: "min", Unit: "count", Value: "999"},
		{Name: "unused_volume", Direction: "min", Unit: "lattice^3", Value: "0"},
	}
	plan, err := knapsack.NewPlan(spec)
	if err != nil {
		t.Fatal(err)
	}
	result := verify.Plan(request, plan, verify.RequireAll().WithObjective(goal))
	if !result.Has(verify.CodeObjective) {
		t.Fatalf("altered objective accepted: %+v", result.Violations())
	}
}

func TestVerifyContextRejectsNilAndCancellation(t *testing.T) {
	t.Parallel()

	request := request(t)
	plan := validPlan(t)
	var nilContext context.Context
	if _, err := verify.PlanContext(nilContext, request, plan, verify.RequireAll()); !errors.Is(err, knapsack.ErrInvalidOptions) {
		t.Fatalf("nil context error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verify.PlanContext(ctx, request, plan, verify.RequireAll()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled context error = %v", err)
	}
}

func TestVerifyContextChecksCancellationThroughoutWork(t *testing.T) {
	request := request(t)
	plan := validPlan(t)
	cancelled := 0
	for checks := 1; checks <= 64; checks++ {
		_, err := verify.PlanContext(&cancelAfterChecks{remaining: checks}, request, plan, verify.RequireAll())
		if errors.Is(err, context.Canceled) {
			cancelled++
		}
	}
	if cancelled < 10 {
		t.Fatalf("only %d cancellation checkpoints observed", cancelled)
	}
}

func TestVerifyPlanJSONPerformsFreshStrictDecode(t *testing.T) {
	t.Parallel()

	request := request(t)
	input, err := packingjson.MarshalPlan(validPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	result, err := verify.PlanJSON(context.Background(), request, input, packingjson.DefaultLimits(), verify.RequireAll())
	if err != nil || !result.Valid() {
		t.Fatalf("result=%+v error=%v", result.Violations(), err)
	}
	input = append(input, []byte(` {}`)...)
	if _, err := verify.PlanJSON(context.Background(), request, input, packingjson.DefaultLimits(), verify.RequireAll()); !errors.Is(err, packingjson.ErrInvalidEncoding) {
		t.Fatalf("trailing JSON error = %v", err)
	}
}

func TestVerifyRejectsIncoherentProofStatus(t *testing.T) {
	t.Parallel()

	request := request(t)
	tests := []struct {
		name   string
		mutate func(*knapsack.PlanSpec)
	}{
		{"heuristic optimal", func(spec *knapsack.PlanSpec) {
			spec.Status = knapsack.StatusOptimal
			spec.Work.Solver = "heuristic"
		}},
		{"feasible with unpacked", func(spec *knapsack.PlanSpec) {
			spec.Status = knapsack.StatusFeasible
			spec.UnpackedItemIDs = []string{"a"}
		}},
		{"infeasible with placements", func(spec *knapsack.PlanSpec) {
			spec.Status = knapsack.StatusInfeasible
			spec.Work.Solver = "exact"
		}},
		{"completed budget exhaustion", func(spec *knapsack.PlanSpec) {
			spec.Status = knapsack.StatusBudgetExhausted
			spec.Termination = knapsack.TerminationCompleted
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := validPlan(t).Spec()
			test.mutate(&spec)
			plan, err := knapsack.NewPlan(spec)
			if err != nil {
				t.Fatal(err)
			}
			if result := verify.Plan(request, plan, verify.AllowUnpacked()); !result.Has(verify.CodeProofStatus) {
				t.Fatalf("status accepted: %s", plan.CanonicalString())
			}
		})
	}
}

func TestVerifyAcceptsCoherentProofStatusesAndRejectsZeroPlan(t *testing.T) {
	t.Parallel()

	request := request(t)
	optimalSpec := validPlan(t).Spec()
	optimalSpec.Status = knapsack.StatusOptimal
	optimalSpec.Work.Solver = "exact"
	optimal, _ := knapsack.NewPlan(optimalSpec)
	if result := verify.Plan(request, optimal, verify.RequireAll()); !result.Valid() {
		t.Fatalf("optimal violations = %+v", result.Violations())
	}
	budgetSpec := validPlan(t).Spec()
	budgetSpec.Status = knapsack.StatusBudgetExhausted
	budgetSpec.Termination = knapsack.TerminationNodeLimit
	budget, _ := knapsack.NewPlan(budgetSpec)
	if result := verify.Plan(request, budget, verify.RequireAll()); !result.Valid() {
		t.Fatalf("budget violations = %+v", result.Violations())
	}
	infeasible, _ := knapsack.NewPlan(knapsack.PlanSpec{
		UnpackedItemIDs: []string{"a", "b"}, Status: knapsack.StatusInfeasible,
		Termination: knapsack.TerminationCompleted, Work: knapsack.Work{Solver: "exact"},
	})
	if result := verify.Plan(request, infeasible, verify.AllowUnpacked()); !result.Valid() {
		t.Fatalf("infeasible violations = %+v", result.Violations())
	}
	if result := verify.Plan(request, knapsack.Plan{}, verify.AllowUnpacked()); !result.Has(verify.CodeProofStatus) {
		t.Fatalf("zero plan status accepted: %+v", result.Violations())
	}
}

func TestVerifyRejectsContainerIdentityStockAndUnpackedAmbiguity(t *testing.T) {
	t.Parallel()
	baseRequest := request(t)
	tests := []struct {
		name string
		spec knapsack.PlanSpec
		code verify.Code
	}{
		{"unknown type", knapsack.PlanSpec{Containers: []knapsack.ContainerInstance{{ID: "bad", TypeID: "unknown"}}, Status: knapsack.StatusBestKnown, Termination: knapsack.TerminationNoPlacement}, verify.CodeUnknownContainer},
		{"empty instance", knapsack.PlanSpec{Containers: []knapsack.ContainerInstance{{TypeID: "box"}}, Status: knapsack.StatusBestKnown, Termination: knapsack.TerminationNoPlacement}, verify.CodeUnknownContainer},
		{"duplicate instance", knapsack.PlanSpec{Containers: []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}, {ID: "box#1", TypeID: "box"}}, Status: knapsack.StatusBestKnown, Termination: knapsack.TerminationNoPlacement}, verify.CodeStock},
		{"stock excess", knapsack.PlanSpec{Containers: []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}, {ID: "box#2", TypeID: "box"}}, Status: knapsack.StatusBestKnown, Termination: knapsack.TerminationNoPlacement}, verify.CodeStock},
		{"unknown unpacked", knapsack.PlanSpec{UnpackedItemIDs: []string{"unknown"}, Status: knapsack.StatusBestKnown, Termination: knapsack.TerminationNoPlacement}, verify.CodeUnknownItem},
		{"duplicate unpacked", knapsack.PlanSpec{UnpackedItemIDs: []string{"a", "a", "b"}, Status: knapsack.StatusBestKnown, Termination: knapsack.TerminationNoPlacement}, verify.CodeDuplicateItem},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			plan, _ := knapsack.NewPlan(test.spec)
			if result := verify.Plan(baseRequest, plan, verify.AllowUnpacked()); !result.Has(test.code) {
				t.Fatalf("violations = %+v", result.Violations())
			}
		})
	}
	spec := validPlan(t).Spec()
	spec.UnpackedItemIDs = []string{"a"}
	plan, _ := knapsack.NewPlan(spec)
	if result := verify.Plan(baseRequest, plan, verify.AllowUnpacked()); !result.Has(verify.CodeDuplicateItem) {
		t.Fatalf("packed and unpacked violations = %+v", result.Violations())
	}
	spec = validPlan(t).Spec()
	spec.Placements[0].ContainerID = "unknown"
	plan, _ = knapsack.NewPlan(spec)
	if result := verify.Plan(baseRequest, plan, verify.RequireAll()); !result.Has(verify.CodeUnknownContainer) {
		t.Fatalf("unknown placement container violations = %+v", result.Violations())
	}
}

func TestVerifyRejectsOrientationReservedEligibilityAndItemCount(t *testing.T) {
	t.Parallel()
	maximumItems := uint32(1)
	base := request(t)
	items := base.Items()
	containers := base.Containers()
	containers[0].AllowedClasses = []string{"allowed"}
	containers[0].MaxItemCount = maximumItems
	reserved, _ := geometry.NewCuboid(geometry.Point{}, geometry.Dimensions{X: 1, Y: 1, Z: 1})
	containers[0].Reserved = []geometry.Cuboid{reserved}
	custom, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{Items: items, Containers: containers, Resolution: base.Resolution(), Limits: base.Limits()})
	if err != nil {
		t.Fatal(err)
	}
	spec := validPlan(t).Spec()
	plan, _ := knapsack.NewPlan(spec)
	result := verify.Plan(custom, plan, verify.RequireAll())
	for _, code := range []verify.Code{verify.CodeReservedOverlap, verify.CodeEligibility, verify.CodeStock} {
		if !result.Has(code) {
			t.Fatalf("missing %s in %+v", code, result.Violations())
		}
	}
	spec.Placements[0].Orientation = geometry.OrientationZYX
	plan, _ = knapsack.NewPlan(spec)
	if result := verify.Plan(custom, plan, verify.RequireAll()); !result.Has(verify.CodeForbiddenOrientation) || countCode(result, verify.CodeForbiddenOrientation) != 1 {
		t.Fatalf("orientation violations = %+v", result.Violations())
	}
	spec = validPlan(t).Spec()
	spec.Placements[1].Origin.X = -1
	plan, _ = knapsack.NewPlan(spec)
	if result := verify.Plan(custom, plan, verify.RequireAll()); !result.Has(verify.CodeOutsideContainer) {
		t.Fatalf("boundary violations = %+v", result.Violations())
	}
}

func countCode(result verify.Result, code verify.Code) int {
	count := 0
	for _, violation := range result.Violations() {
		if violation.Code == code {
			count++
		}
	}
	return count
}

func TestVerifyDetectsAccountingOverflow(t *testing.T) {
	t.Parallel()
	base := request(t)
	items := base.Items()[:1]
	containers := []knapsack.NormalizedContainer{
		{ID: "a", Dimensions: geometry.Dimensions{X: math.MaxInt64 / 2, Y: 1, Z: 1}, MaxContentWeight: math.MaxInt64, Stock: knapsack.UnlimitedStock()},
		{ID: "b", Dimensions: geometry.Dimensions{X: math.MaxInt64 / 2, Y: 1, Z: 1}, MaxContentWeight: math.MaxInt64, Stock: knapsack.UnlimitedStock()},
		{ID: "c", Dimensions: geometry.Dimensions{X: 2, Y: 1, Z: 1}, MaxContentWeight: 1, Stock: knapsack.UnlimitedStock()},
	}
	custom, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{Items: items, Containers: containers, Resolution: base.Resolution(), Limits: base.Limits()})
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{
		Containers:      []knapsack.ContainerInstance{{ID: "a#1", TypeID: "a"}, {ID: "b#1", TypeID: "b"}, {ID: "c#1", TypeID: "c"}},
		UnpackedItemIDs: []string{"a"}, Status: knapsack.StatusBestKnown, Termination: knapsack.TerminationNoPlacement,
	})
	if result := verify.Plan(custom, plan, verify.AllowUnpacked()); !result.Has(verify.CodeOverflow) {
		t.Fatalf("violations = %+v", result.Violations())
	}
}
