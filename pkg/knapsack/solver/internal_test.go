package solver

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/constraint"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/objective"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

type internalReject struct{}

func (internalReject) Check(context.Context, constraint.PlacementView) constraint.Decision {
	return constraint.Reject("test", "rejected")
}

type internalPanic struct{}

func (internalPanic) Check(context.Context, constraint.PlacementView) constraint.Decision {
	panic("test panic")
}

type internalCancel struct{ cancel context.CancelFunc }

func (c internalCancel) Check(context.Context, constraint.PlacementView) constraint.Decision {
	c.cancel()
	return constraint.Accept()
}

type changingConstraint struct{ calls int }

func (c *changingConstraint) Check(context.Context, constraint.PlacementView) constraint.Decision {
	c.calls++
	if c.calls > 1 {
		return constraint.Reject("changed", "constraint result changed during verification")
	}
	return constraint.Accept()
}

type waitForDeadline struct{}

func (waitForDeadline) Check(ctx context.Context, _ constraint.PlacementView) constraint.Decision {
	<-ctx.Done()
	return constraint.Accept()
}

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

type componentSequence struct{ calls int }

func (*componentSequence) Valid() bool { return true }
func (*componentSequence) ComparePlans(context.Context, knapsack.NormalizedRequest, knapsack.Plan, knapsack.Plan) (int, error) {
	return 0, nil
}

type changingObjective struct{ calls int }

func (*changingObjective) Valid() bool { return true }
func (*changingObjective) ComparePlans(context.Context, knapsack.NormalizedRequest, knapsack.Plan, knapsack.Plan) (int, error) {
	return 0, nil
}
func (o *changingObjective) Components(context.Context, knapsack.NormalizedRequest, knapsack.Plan) ([]knapsack.ScoreComponent, error) {
	o.calls++
	return []knapsack.ScoreComponent{{Name: "test", Direction: "min", Unit: "count", Value: strconv.Itoa(o.calls)}}, nil
}
func (o *componentSequence) Components(context.Context, knapsack.NormalizedRequest, knapsack.Plan) ([]knapsack.ScoreComponent, error) {
	o.calls++
	if o.calls > 1 {
		return nil, nil
	}
	return []knapsack.ScoreComponent{{Name: "test", Direction: "min", Unit: "count", Value: "1"}}, nil
}

type cancelingObjective struct{ cancel context.CancelFunc }

func (*cancelingObjective) Valid() bool { return true }
func (*cancelingObjective) ComparePlans(context.Context, knapsack.NormalizedRequest, knapsack.Plan, knapsack.Plan) (int, error) {
	return 0, nil
}

type countedObjective struct {
	calls    int
	failAt   int
	cancelAt int
	cancel   context.CancelFunc
}

type countedCompareObjective struct {
	calls    int
	failAt   int
	cancelAt int
	cancel   context.CancelFunc
}

func (*countedCompareObjective) Valid() bool { return true }
func (o *countedCompareObjective) ComparePlans(ctx context.Context, _ knapsack.NormalizedRequest, _, _ knapsack.Plan) (int, error) {
	o.calls++
	if o.calls == o.cancelAt {
		o.cancel()
		return 0, ctx.Err()
	}
	if o.calls == o.failAt {
		return 0, errors.New("counted comparison failure")
	}
	return 0, nil
}
func (*countedCompareObjective) Components(context.Context, knapsack.NormalizedRequest, knapsack.Plan) ([]knapsack.ScoreComponent, error) {
	return []knapsack.ScoreComponent{{Name: "test", Direction: "min", Unit: "count", Value: "1"}}, nil
}

func (*countedObjective) Valid() bool { return true }
func (*countedObjective) ComparePlans(context.Context, knapsack.NormalizedRequest, knapsack.Plan, knapsack.Plan) (int, error) {
	return 0, nil
}
func (o *countedObjective) Components(context.Context, knapsack.NormalizedRequest, knapsack.Plan) ([]knapsack.ScoreComponent, error) {
	o.calls++
	if o.calls == o.cancelAt {
		o.cancel()
	}
	if o.calls == o.failAt {
		return nil, errors.New("counted objective failure")
	}
	return []knapsack.ScoreComponent{{Name: "test", Direction: "min", Unit: "count", Value: "1"}}, nil
}
func (o *cancelingObjective) Components(context.Context, knapsack.NormalizedRequest, knapsack.Plan) ([]knapsack.ScoreComponent, error) {
	o.cancel()
	return []knapsack.ScoreComponent{{Name: "test", Direction: "min", Unit: "count", Value: "1"}}, nil
}

func baseInternalBin() *bin {
	return &bin{
		instance: knapsack.ContainerInstance{ID: "box#1", TypeID: "box"},
		info: knapsack.NormalizedContainer{
			ID: "box", Dimensions: geometry.Dimensions{X: 2, Y: 2, Z: 2},
			MaxContentWeight: 10, Stock: knapsack.UnlimitedStock(),
		},
		points: []geometry.Point{{}},
	}
}

func baseInternalItem() knapsack.NormalizedItem {
	return knapsack.NormalizedItem{
		ID: "item", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1},
		Weight: 1, Orientations: []geometry.Orientation{geometry.OrientationXYZ},
	}
}

func TestExactPlacementRejectsEveryFeasibilityBoundary(t *testing.T) {
	t.Parallel()
	item := baseInternalItem()
	if placement, ok := exactPlacement(item, baseInternalBin(), geometry.Point{}, geometry.OrientationXYZ); !ok || placement.ItemID != item.ID {
		t.Fatalf("valid placement=%+v ok=%v", placement, ok)
	}
	supportedBin := baseInternalBin()
	base := baseInternalItem()
	base.ID = "base"
	supportedBin.items = []knapsack.NormalizedItem{base}
	supportedBin.placements = []knapsack.Placement{{ItemID: "base", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}}}
	if placement, ok := exactPlacement(item, supportedBin, geometry.Point{Z: 1}, geometry.OrientationXYZ); !ok || len(placement.SupporterIDs) != 1 {
		t.Fatalf("supported placement=%+v ok=%v", placement, ok)
	}
	reserved, _ := geometry.NewCuboid(geometry.Point{}, geometry.Dimensions{X: 1, Y: 1, Z: 1})
	tests := []struct {
		name        string
		mutateItem  func(*knapsack.NormalizedItem)
		mutateBin   func(*bin)
		point       geometry.Point
		orientation geometry.Orientation
	}{
		{"item count", nil, func(b *bin) { b.info.MaxItemCount = 1; b.placements = []knapsack.Placement{{}} }, geometry.Point{}, geometry.OrientationXYZ},
		{"weight", nil, func(b *bin) { b.info.MaxContentWeight = 0 }, geometry.Point{}, geometry.OrientationXYZ},
		{"gross weight", nil, func(b *bin) { b.info.HasGrossWeight = true; b.info.MaxGrossWeight = 1; b.info.TareWeight = 1 }, geometry.Point{}, geometry.OrientationXYZ},
		{"class", nil, func(b *bin) { b.info.AllowedClasses = []string{"allowed"} }, geometry.Point{}, geometry.OrientationXYZ},
		{"orientation", nil, nil, geometry.Point{}, geometry.Orientation("invalid")},
		{"outside", nil, nil, geometry.Point{X: 2}, geometry.OrientationXYZ},
		{"coordinate overflow", nil, nil, geometry.Point{X: 9223372036854775807}, geometry.OrientationXYZ},
		{"reserved", nil, func(b *bin) { b.info.Reserved = []geometry.Cuboid{reserved} }, geometry.Point{}, geometry.OrientationXYZ},
		{"overlap", nil, func(b *bin) {
			b.placements = []knapsack.Placement{{ItemID: "other", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}}}
			b.items = []knapsack.NormalizedItem{baseInternalItem()}
		}, geometry.Point{}, geometry.OrientationXYZ},
		{"support", func(i *knapsack.NormalizedItem) { i.MinimumSupportPPM = 1_000_000 }, nil, geometry.Point{Z: 1}, geometry.OrientationXYZ},
		{"eligibility relation", func(i *knapsack.NormalizedItem) { i.IncompatibleGroups = []string{"blocked"} }, func(b *bin) {
			other := baseInternalItem()
			other.ID, other.Group = "other", "blocked"
			b.items = []knapsack.NormalizedItem{other}
			b.placements = []knapsack.Placement{{ItemID: "other", Origin: geometry.Point{X: 1}, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}}}
		}, geometry.Point{}, geometry.OrientationXYZ},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			candidate, target := item, baseInternalBin()
			if test.mutateItem != nil {
				test.mutateItem(&candidate)
			}
			if test.mutateBin != nil {
				test.mutateBin(target)
			}
			if placement, ok := exactPlacement(candidate, target, test.point, test.orientation); ok {
				t.Fatalf("accepted placement %+v", placement)
			}
		})
	}
}

func TestHeuristicPlacementScoringAndCallbackRejection(t *testing.T) {
	t.Parallel()
	item, target := baseInternalItem(), baseInternalBin()
	target.points = []geometry.Point{{X: 1}, {}}
	var candidates uint64
	accepted, err := tryPlace(context.Background(), item, target, &candidates, 10, nil)
	if err != nil || !accepted || target.placements[0].Origin != (geometry.Point{}) {
		t.Fatalf("accepted=%v error=%v placements=%+v", accepted, err, target.placements)
	}
	target = baseInternalBin()
	candidates = 0
	accepted, err = tryPlace(context.Background(), item, target, &candidates, 10, []constraint.Placement{internalReject{}})
	if err != nil || accepted {
		t.Fatalf("accepted=%v error=%v", accepted, err)
	}
	target = baseInternalBin()
	candidates = 0
	accepted, err = tryPlace(context.Background(), item, target, &candidates, 0, nil)
	if err != nil || accepted || candidates != 1 {
		t.Fatalf("accepted=%v candidates=%d error=%v", accepted, candidates, err)
	}
}

func TestHeuristicPlacementRejectsGrossGeometryReservedAndOverlap(t *testing.T) {
	t.Parallel()
	item := baseInternalItem()
	reserved, _ := geometry.NewCuboid(geometry.Point{}, geometry.Dimensions{X: 1, Y: 1, Z: 1})
	for _, mutate := range []func(*bin){
		func(b *bin) { b.info.HasGrossWeight, b.info.TareWeight, b.info.MaxGrossWeight = true, 1, 1 },
		func(b *bin) { b.points = []geometry.Point{{X: 9223372036854775807}} },
		func(b *bin) { b.info.Reserved = []geometry.Cuboid{reserved} },
		func(b *bin) {
			b.placements = []knapsack.Placement{{ItemID: "other", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}}}
			b.items = []knapsack.NormalizedItem{baseInternalItem()}
		},
	} {
		target := baseInternalBin()
		mutate(target)
		var candidates uint64
		accepted, err := tryPlace(context.Background(), item, target, &candidates, 10, nil)
		if err != nil || accepted {
			t.Fatalf("accepted=%v error=%v target=%+v", accepted, err, target)
		}
	}
	target := baseInternalBin()
	base := baseInternalItem()
	base.ID = "base"
	target.items = []knapsack.NormalizedItem{base}
	target.placements = []knapsack.Placement{{ItemID: "base", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}}}
	target.points = []geometry.Point{{Z: 1}}
	var candidates uint64
	accepted, err := tryPlace(context.Background(), item, target, &candidates, 10, nil)
	if err != nil || !accepted || len(target.placements[1].SupporterIDs) != 1 {
		t.Fatalf("supported accepted=%v error=%v placements=%+v", accepted, err, target.placements)
	}
}

func TestDeterministicComparatorsExerciseEveryTieBreak(t *testing.T) {
	t.Parallel()
	placements := []knapsack.Placement{
		{Origin: geometry.Point{}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}},
		{Origin: geometry.Point{X: 1}, Orientation: geometry.OrientationZYX, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}},
		{Origin: geometry.Point{}, Orientation: geometry.OrientationZYX, Dimensions: geometry.Dimensions{X: 2, Y: 2, Z: 2}},
	}
	if comparePlacement(placements[0], placements[1]) >= 0 || comparePlacement(placements[0], placements[2]) >= 0 {
		t.Fatal("placement ordering changed")
	}
	widthFirst := []knapsack.Placement{
		{Origin: geometry.Point{}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}},
		{Origin: geometry.Point{Z: 1}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}},
		{Origin: geometry.Point{X: 1}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}},
		{Origin: geometry.Point{Y: 1}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}},
		{Origin: geometry.Point{X: 1}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{Y: 1, X: 0, Z: 1}},
		{Origin: geometry.Point{}, Orientation: geometry.OrientationZYX, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}},
	}
	if comparePlacementWidthFirst(widthFirst[0], widthFirst[1]) >= 0 ||
		comparePlacementWidthFirst(widthFirst[0], widthFirst[2]) >= 0 ||
		comparePlacementWidthFirst(widthFirst[0], widthFirst[3]) >= 0 ||
		comparePlacementWidthFirst(widthFirst[0], widthFirst[4]) == 0 ||
		comparePlacementWidthFirst(widthFirst[0], widthFirst[5]) >= 0 {
		t.Fatal("width-first placement ordering changed")
	}
	if compareItems(knapsack.NormalizedItem{ID: "a", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 2}, knapsack.NormalizedItem{ID: "b", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1}) >= 0 {
		t.Fatal("item weight ordering changed")
	}
	if compareContainers(knapsack.NormalizedContainer{ID: "a", Priority: 1, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}}, knapsack.NormalizedContainer{ID: "b", Priority: 2, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}}) >= 0 {
		t.Fatal("container priority ordering changed")
	}
	for _, pair := range [][2]geometry.Point{{{}, {Z: 1}}, {{}, {Y: 1}}, {{}, {X: 1}}, {{X: 1}, {}}} {
		if comparison := comparePoints(pair[0], pair[1]); comparison == 0 {
			t.Fatalf("points tied: %+v", pair)
		}
	}
}

func TestInterruptedPlanReportsCancellationDeadlineAndBudget(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	for _, test := range []struct {
		cause       error
		termination knapsack.TerminationReason
	}{
		{context.Canceled, knapsack.TerminationCancelled},
		{context.DeadlineExceeded, knapsack.TerminationDeadline},
		{knapsack.ErrBudgetExhausted, knapsack.TerminationCandidateLimit},
	} {
		plan, err := interruptedPlan(request, nil, []string{"item"}, 1, 7, test.cause)
		if !errors.Is(err, test.cause) || plan.Termination() != test.termination || plan.Status() != knapsack.StatusBudgetExhausted {
			t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
		}
	}
}

func TestVariableSearchBudgetClassification(t *testing.T) {
	t.Parallel()

	limits := knapsack.DefaultLimits()
	for _, test := range []struct {
		configurations uint64
		work           knapsack.Work
		want           knapsack.TerminationReason
	}{
		{limits.MaxBranches + 1, knapsack.Work{}, knapsack.TerminationBranchLimit},
		{1, knapsack.Work{Branches: limits.MaxBranches - 1}, knapsack.TerminationBranchLimit},
		{1, knapsack.Work{Nodes: limits.MaxSearchNodes}, knapsack.TerminationNodeLimit},
		{1, knapsack.Work{CandidatePlacements: limits.MaxCandidatePlacements}, knapsack.TerminationCandidateLimit},
		{1, knapsack.Work{}, ""},
	} {
		if got := variableBudgetTermination(test.configurations, test.work, limits); got != test.want {
			t.Fatalf("termination = %q, want %q", got, test.want)
		}
	}

	for err, want := range map[error]knapsack.TerminationReason{
		context.DeadlineExceeded:          knapsack.TerminationDeadline,
		context.Canceled:                  knapsack.TerminationCancelled,
		knapsack.ErrMemoryBudgetExhausted: knapsack.TerminationMemoryLimit,
		knapsack.ErrBudgetExhausted:       knapsack.TerminationNodeLimit,
	} {
		if got := terminationForError(err); got != want {
			t.Fatalf("error %v termination = %q, want %q", err, got, want)
		}
	}
}

func TestExactPackAllClassifiesCancellationDuringEnumeration(t *testing.T) {
	t.Parallel()

	plan, err := (Exact{}).PackAll(&cancelAfterChecks{remaining: 2}, internalRequest(t), Options{})
	if !errors.Is(err, context.Canceled) || plan.Termination() != knapsack.TerminationCancelled {
		t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
	}
}

func TestExactPointsRejectMemoryAndCardinalityOverflow(t *testing.T) {
	t.Parallel()
	target := baseInternalBin()
	target.placements = []knapsack.Placement{{
		Origin:     geometry.Point{X: 1, Y: 1, Z: 1},
		Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1},
	}}
	if points, ok := exactPoints(target, 1_000); !ok || len(points) != 8 {
		t.Fatalf("points=%d ok=%v", len(points), ok)
	}
	if points, ok := exactPoints(target, 1); ok || points != nil {
		t.Fatalf("memory-limited points=%v ok=%v", points, ok)
	}
	reserved, _ := geometry.NewCuboid(geometry.Point{}, geometry.Dimensions{X: 1, Y: 1, Z: 1})
	target.info.Reserved = []geometry.Cuboid{reserved}
	if points, ok := exactPoints(target, 1_000); !ok || len(points) != 27 {
		t.Fatalf("reserved points=%d ok=%v", len(points), ok)
	}
	if _, ok := checkedProduct(^uint64(0), 2); ok {
		t.Fatal("overflowed Cartesian product accepted")
	}
}

func TestSolversRejectLimitsBelowNormalizedRequestMemory(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	limits := request.Limits()
	limits.MaxMemoryBytes = request.MemoryBytes() - 1
	request = request.WithLimits(limits)
	instance := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	for name, solve := range map[string]func() error{
		"heuristic fixed": func() error {
			_, err := (Heuristic{}).PackFixed(context.Background(), request, instance, Options{})
			return err
		},
		"heuristic all": func() error { _, err := (Heuristic{}).PackAll(context.Background(), request, Options{}); return err },
		"exact fixed": func() error {
			_, err := (Exact{}).PackFixed(context.Background(), request, instance, Options{})
			return err
		},
		"exact all": func() error { _, err := (Exact{}).PackAll(context.Background(), request, Options{}); return err },
	} {
		if err := solve(); !errors.Is(err, knapsack.ErrMemoryBudgetExhausted) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestSolversReserveWorkingMemoryBeforeSearch(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	if request.ItemCount() != 1 || request.ContainerTypeCount() != 1 {
		t.Fatalf("counts=%d/%d", request.ItemCount(), request.ContainerTypeCount())
	}
	limits := request.Limits()
	limits.MaxMemoryBytes = request.MemoryBytes() * 2
	request = request.WithLimits(limits)
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	for name, solve := range map[string]func() error{
		"heuristic fixed": func() error {
			_, err := (Heuristic{}).PackFixed(context.Background(), request, instances, Options{})
			return err
		},
		"heuristic all": func() error { _, err := (Heuristic{}).PackAll(context.Background(), request, Options{}); return err },
		"exact fixed": func() error {
			_, err := (Exact{}).PackFixed(context.Background(), request, instances, Options{})
			return err
		},
		"exact all": func() error { _, err := (Exact{}).PackAll(context.Background(), request, Options{}); return err },
	} {
		if err := solve(); !errors.Is(err, knapsack.ErrMemoryBudgetExhausted) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestExactSolverReportsPointMemoryExhaustion(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	limits := request.Limits()
	available, ok := workingMemoryAvailable(request, 1, true)
	if !ok {
		t.Fatal("default working memory rejected")
	}
	reserved := limits.MaxMemoryBytes - available
	limits.MaxMemoryBytes = reserved + 23
	request = request.WithLimits(limits)
	plan, err := (Exact{}).PackFixed(context.Background(), request, []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}, Options{})
	if !errors.Is(err, knapsack.ErrMemoryBudgetExhausted) || plan.Status() != knapsack.StatusBudgetExhausted || plan.Termination() != knapsack.TerminationMemoryLimit {
		t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
	}
}

func TestExactSearchHonorsConstraintRejectionPanicAndCancellation(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	plan, err := (Exact{}).PackFixed(context.Background(), request, instances, Options{Constraints: []constraint.Placement{internalReject{}}})
	if !errors.Is(err, knapsack.ErrProvenInfeasible) || plan.Status() != knapsack.StatusInfeasible {
		t.Fatalf("rejected plan=%s error=%v", plan.CanonicalString(), err)
	}
	if _, err = (Exact{}).PackFixed(context.Background(), request, instances, Options{Constraints: []constraint.Placement{internalPanic{}}}); !errors.Is(err, constraint.ErrCallbackPanic) {
		t.Fatalf("panic error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	plan, err = (Exact{}).PackFixed(ctx, request, instances, Options{Constraints: []constraint.Placement{internalCancel{cancel: cancel}}})
	if !errors.Is(err, context.Canceled) || plan.Termination() != knapsack.TerminationCancelled {
		t.Fatalf("cancelled plan=%s error=%v", plan.CanonicalString(), err)
	}
}

func TestSolversAndVerifierRejectOversizedConstraintViews(t *testing.T) {
	request := internalRequest(t)
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	plan, err := (Heuristic{}).PackFixed(context.Background(), request, instances, Options{})
	if err != nil {
		t.Fatal(err)
	}

	item := request.Items()[0]
	item.Attributes = map[string]string{"oversized": strings.Repeat("x", 16<<20)}
	request, err = knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items:      []knapsack.NormalizedItem{item},
		Containers: request.Containers(),
		Resolution: request.Resolution(),
		Limits:     request.Limits(),
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, solve := range map[string]func() error{
		"heuristic": func() error {
			_, solveErr := (Heuristic{}).PackFixed(context.Background(), request, instances, Options{Constraints: []constraint.Placement{internalReject{}}})
			return solveErr
		},
		"exact": func() error {
			_, solveErr := (Exact{}).PackFixed(context.Background(), request, instances, Options{Constraints: []constraint.Placement{internalReject{}}})
			return solveErr
		},
	} {
		if solveErr := solve(); !errors.Is(solveErr, constraint.ErrViewLimit) {
			t.Fatalf("%s error = %v", name, solveErr)
		}
	}

	result, err := verify.PlanContext(context.Background(), request, plan, verify.RequireAll().WithConstraints(internalReject{}))
	if !errors.Is(err, constraint.ErrViewLimit) || !result.Has(verify.CodeConstraint) {
		t.Fatalf("result=%+v error=%v", result, err)
	}
}

func TestSolversAndVerifierRejectUnboundedConstraintLists(t *testing.T) {
	request := internalRequest(t)
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	callbacks := make([]constraint.Placement, 33)
	for index := range callbacks {
		callbacks[index] = internalReject{}
	}
	for name, solve := range map[string]func() error{
		"heuristic fixed": func() error {
			_, err := (Heuristic{}).PackFixed(context.Background(), request, instances, Options{Constraints: callbacks})
			return err
		},
		"heuristic all": func() error {
			_, err := (Heuristic{}).PackAll(context.Background(), request, Options{Constraints: callbacks})
			return err
		},
		"exact fixed": func() error {
			_, err := (Exact{}).PackFixed(context.Background(), request, instances, Options{Constraints: callbacks})
			return err
		},
		"exact all": func() error {
			_, err := (Exact{}).PackAll(context.Background(), request, Options{Constraints: callbacks})
			return err
		},
	} {
		if err := solve(); !errors.Is(err, constraint.ErrInvalidConstraint) {
			t.Fatalf("%s error = %v", name, err)
		}
	}

	plan, err := (Heuristic{}).PackFixed(context.Background(), request, instances, Options{})
	if err != nil {
		t.Fatal(err)
	}
	result := verify.Plan(request, plan, verify.RequireAll().WithConstraints(callbacks...))
	if !errors.Is(result.Err(), constraint.ErrInvalidConstraint) {
		t.Fatalf("verifier error = %v", result.Err())
	}
	result = verify.Plan(request, plan, verify.RequireAll().WithConstraints(callbacks...).WithConstraints(internalReject{}))
	if result.Err() != nil || !result.Has(verify.CodeConstraint) {
		t.Fatalf("replaced verifier options result=%+v error=%v", result, result.Err())
	}
}

func TestPublicSolverEntriesRejectNilContext(t *testing.T) {
	request := internalRequest(t)
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	var ctx context.Context
	for name, solve := range map[string]func() error{
		"heuristic fixed": func() error { _, err := (Heuristic{}).PackFixed(ctx, request, instances, Options{}); return err },
		"heuristic all":   func() error { _, err := (Heuristic{}).PackAll(ctx, request, Options{}); return err },
		"exact fixed":     func() error { _, err := (Exact{}).PackFixed(ctx, request, instances, Options{}); return err },
		"exact all":       func() error { _, err := (Exact{}).PackAll(ctx, request, Options{}); return err },
	} {
		if err := solve(); !errors.Is(err, knapsack.ErrInvalidOptions) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestSolversRejectConstraintDisagreementDuringVerification(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	for name, solve := range map[string]func(constraint.Placement) error{
		"heuristic": func(callback constraint.Placement) error {
			_, err := (Heuristic{}).PackFixed(context.Background(), request, instances, Options{Constraints: []constraint.Placement{callback}})
			return err
		},
		"exact": func(callback constraint.Placement) error {
			_, err := (Exact{}).PackFixed(context.Background(), request, instances, Options{Constraints: []constraint.Placement{callback}})
			return err
		},
	} {
		if err := solve(&changingConstraint{}); !errors.Is(err, knapsack.ErrInternalInvariant) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestExactSearchReportsDeadlineTermination(t *testing.T) {
	t.Parallel()

	request := internalRequest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	plan, err := (Exact{}).PackFixed(ctx, request,
		[]knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}},
		Options{Constraints: []constraint.Placement{waitForDeadline{}}},
	)
	if !errors.Is(err, context.DeadlineExceeded) || plan.Termination() != knapsack.TerminationDeadline || plan.Work().Solver != "exact" {
		t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
	}
}

func TestExactBudgetReturnsTheBestVerifiedPlan(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	item := request.Items()[0]
	item.Orientations = []geometry.Orientation{geometry.OrientationXYZ, geometry.OrientationXZY}
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: []knapsack.NormalizedItem{item}, Containers: request.Containers(),
		Resolution: request.Resolution(), Limits: request.Limits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	limits := request.Limits()
	limits.MaxSearchNodes = 2
	request = request.WithLimits(limits)
	plan, err := (Exact{}).PackFixed(context.Background(), request, []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}, Options{})
	if !errors.Is(err, knapsack.ErrBudgetExhausted) || len(plan.Placements()) != 1 || plan.Status() != knapsack.StatusBudgetExhausted {
		t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
	}
}

func TestExactPackAllRetainsACompletePlanFoundBeforeExhaustion(t *testing.T) {
	t.Parallel()
	base := internalRequest(t)
	item := base.Items()[0]
	item.Orientations = []geometry.Orientation{geometry.OrientationXYZ, geometry.OrientationXZY}
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{Items: []knapsack.NormalizedItem{item}, Containers: base.Containers(), Resolution: base.Resolution(), Limits: base.Limits()})
	if err != nil {
		t.Fatal(err)
	}
	limits := request.Limits()
	limits.MaxSearchNodes = 2
	request = request.WithLimits(limits)
	plan, err := (Exact{}).PackAll(context.Background(), request, Options{})
	if !errors.Is(err, knapsack.ErrBudgetExhausted) || len(plan.Placements()) != 1 || plan.Status() != knapsack.StatusBudgetExhausted {
		t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
	}
}

func TestInternalOrderingAndGroupStateTransitions(t *testing.T) {
	t.Parallel()
	base := knapsack.Placement{Origin: geometry.Point{}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}}
	for _, candidate := range []knapsack.Placement{
		{Origin: geometry.Point{X: 1}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 0, Y: 1, Z: 1}},
		{Origin: geometry.Point{}, Orientation: geometry.OrientationZYX, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}},
	} {
		if comparePlacement(base, candidate) >= 0 {
			t.Fatalf("placement tie-break changed: %+v", candidate)
		}
	}
	items := []knapsack.NormalizedItem{
		{ID: "volume", Dimensions: geometry.Dimensions{X: 2, Y: 1, Z: 1}, Weight: 1},
		{ID: "weight", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 2},
		{ID: "id-b", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1},
		{ID: "id-a", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1},
	}
	if compareItems(items[0], items[1]) >= 0 || compareItems(items[1], items[2]) >= 0 || compareItems(items[3], items[2]) >= 0 {
		t.Fatal("item ordering changed")
	}
	if compareItems(items[1], items[0]) <= 0 || compareItems(items[2], items[1]) <= 0 {
		t.Fatal("reverse item ordering changed")
	}
	containers := []knapsack.NormalizedContainer{
		{ID: "priority", Priority: 0, Dimensions: geometry.Dimensions{X: 2, Y: 1, Z: 1}},
		{ID: "volume", Priority: 1, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}},
		{ID: "id-b", Priority: 1, Dimensions: geometry.Dimensions{X: 2, Y: 1, Z: 1}},
		{ID: "id-a", Priority: 1, Dimensions: geometry.Dimensions{X: 2, Y: 1, Z: 1}},
	}
	if compareContainers(containers[0], containers[1]) >= 0 || compareContainers(containers[1], containers[2]) >= 0 || compareContainers(containers[3], containers[2]) >= 0 {
		t.Fatal("container ordering changed")
	}
	if compareContainers(containers[1], containers[0]) <= 0 || compareContainers(containers[2], containers[1]) <= 0 {
		t.Fatal("reverse container ordering changed")
	}
	target := baseInternalBin()
	first, second := baseInternalItem(), baseInternalItem()
	first.ID, first.Group, second.ID = "linked", "group", "ordinary"
	target.items = []knapsack.NormalizedItem{first, second}
	target.placements = []knapsack.Placement{{ItemID: first.ID, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}}, {ItemID: second.ID, Origin: geometry.Point{X: 1}, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}}}
	target.weight = 2
	if selected, found := groupTargets([]*bin{target}, "group"); !found || len(selected) != 1 {
		t.Fatalf("group target=%d found=%v", len(selected), found)
	}
	if selected, found := groupTargets([]*bin{target}, "missing"); found || len(selected) != 1 {
		t.Fatalf("missing target=%d found=%v", len(selected), found)
	}
	remaining, removed := rollbackGroup([]*bin{target}, "group", false, nil)
	if len(remaining) != 1 || len(removed) != 1 || remaining[0].weight != 1 || len(remaining[0].placements) != 1 {
		t.Fatalf("remaining=%+v removed=%v", remaining, removed)
	}
	empty := baseInternalBin()
	empty.items, empty.placements = []knapsack.NormalizedItem{first}, []knapsack.Placement{{ItemID: first.ID, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}}}
	stock := map[string]uint32{"box": 1}
	remaining, _ = rollbackGroup([]*bin{empty}, "group", true, stock)
	if len(remaining) != 0 || stock["box"] != 0 {
		t.Fatalf("empty bin retained: %+v stock=%v", remaining, stock)
	}
}

func TestInterruptedPlanVerifiesRetainedPlacements(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	target := baseInternalBin()
	target.placements = []knapsack.Placement{{ItemID: "item", ContainerID: "box#1", Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1}}
	target.items = []knapsack.NormalizedItem{baseInternalItem()}
	target.weight = 1
	plan, err := interruptedPlan(request, []*bin{target}, nil, 1, 0, context.Canceled)
	if !errors.Is(err, context.Canceled) || len(plan.Placements()) != 1 {
		t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
	}
	target.placements[0].Origin.X = 2
	if _, err = interruptedPlan(request, []*bin{target}, nil, 1, 0, context.Canceled); !errors.Is(err, knapsack.ErrInternalInvariant) {
		t.Fatalf("invalid retained plan error = %v", err)
	}
}

func TestInterruptedHeuristicPlanRejectsUnscoredAndUnverifiedResults(t *testing.T) {
	t.Parallel()

	request := internalRequest(t)
	if _, err := interruptedHeuristicPlan(request, nil, []string{"item"}, 1, Options{}, objective.Objective{}, context.Canceled); !errors.Is(err, objective.ErrInvalidObjective) {
		t.Fatalf("invalid objective error = %v", err)
	}
	if _, err := interruptedHeuristicPlan(request, nil, []string{"item"}, 1, Options{}, &changingObjective{}, context.Canceled); !errors.Is(err, knapsack.ErrInternalInvariant) {
		t.Fatalf("objective disagreement error = %v", err)
	}
	bad := baseInternalBin()
	bad.placements = []knapsack.Placement{{ItemID: "item", ContainerID: "box#1", Origin: geometry.Point{X: 2}, Orientation: geometry.OrientationXYZ, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 1}}
	bad.items = []knapsack.NormalizedItem{baseInternalItem()}
	bad.weight = 1
	goal, _ := objective.New(objective.Minimize(objective.ContainerCount))
	if plan, err := interruptedHeuristicPlan(request, []*bin{bad}, nil, 1, Options{}, goal, context.Canceled); plan.Status() != "" || !errors.Is(err, knapsack.ErrInternalInvariant) {
		t.Fatalf("invalid retained plan=%s error=%v", plan.CanonicalString(), err)
	}
}

func TestInterruptedBestPlanClassifiesEveryCause(t *testing.T) {
	t.Parallel()

	best := buildPlan(nil, []string{"item"}, knapsack.StatusBestKnown, knapsack.TerminationNoPlacement, 1, 0, nil)
	for _, test := range []struct {
		cause error
		want  knapsack.TerminationReason
	}{
		{context.Canceled, knapsack.TerminationCancelled},
		{context.DeadlineExceeded, knapsack.TerminationDeadline},
		{knapsack.ErrBudgetExhausted, knapsack.TerminationCandidateLimit},
	} {
		plan, err := interruptedBestPlan(best, 2, 1, test.cause)
		if !errors.Is(err, test.cause) || plan.Termination() != test.want || plan.Work().ImprovementRounds != 1 {
			t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
		}
	}
}

func TestHeuristicEntryCancellationAndCandidateExhaustion(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	for name, solve := range map[string]func(context.Context, Options) (knapsack.Plan, error){
		"fixed": func(ctx context.Context, options Options) (knapsack.Plan, error) {
			return (Heuristic{}).PackFixed(ctx, request, instances, options)
		},
		"all": func(ctx context.Context, options Options) (knapsack.Plan, error) {
			return (Heuristic{}).PackAll(ctx, request, options)
		},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		plan, err := solve(ctx, Options{Constraints: []constraint.Placement{internalCancel{cancel: cancel}}})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s cancellation plan=%s error=%v", name, plan.CanonicalString(), err)
		}
	}
	item := request.Items()[0]
	item.Orientations = []geometry.Orientation{geometry.OrientationXYZ, geometry.OrientationXZY}
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{Items: []knapsack.NormalizedItem{item}, Containers: request.Containers(), Resolution: request.Resolution(), Limits: request.Limits()})
	if err != nil {
		t.Fatal(err)
	}
	limits := request.Limits()
	limits.MaxCandidatePlacements = 1
	request = request.WithLimits(limits)
	for name, solve := range map[string]func() (knapsack.Plan, error){
		"fixed": func() (knapsack.Plan, error) {
			return (Heuristic{}).PackFixed(context.Background(), request, instances, Options{})
		},
		"all": func() (knapsack.Plan, error) { return (Heuristic{}).PackAll(context.Background(), request, Options{}) },
	} {
		plan, err := solve()
		if !errors.Is(err, knapsack.ErrBudgetExhausted) || plan.Termination() != knapsack.TerminationCandidateLimit {
			t.Fatalf("%s budget plan=%s error=%v", name, plan.CanonicalString(), err)
		}
	}
}

func TestEverySolverRejectsInvalidAndEmptyNormalizedRequests(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	limits := request.Limits()
	limits.MaxItems = 0
	invalid := request.WithLimits(limits)
	empty := (knapsack.NormalizedRequest{}).WithLimits(knapsack.DefaultLimits())
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	for label, candidate := range map[string]knapsack.NormalizedRequest{"invalid": invalid, "empty": empty} {
		for name, solve := range map[string]func() error{
			"heuristic fixed": func() error {
				_, err := (Heuristic{}).PackFixed(context.Background(), candidate, instances, Options{})
				return err
			},
			"heuristic all": func() error { _, err := (Heuristic{}).PackAll(context.Background(), candidate, Options{}); return err },
			"exact fixed": func() error {
				_, err := (Exact{}).PackFixed(context.Background(), candidate, instances, Options{})
				return err
			},
			"exact all": func() error { _, err := (Exact{}).PackAll(context.Background(), candidate, Options{}); return err },
		} {
			if err := solve(); !errors.Is(err, knapsack.ErrInvalidRequest) {
				t.Fatalf("%s %s error = %v", label, name, err)
			}
		}
	}
}

func TestSolversPropagateFinalObjectiveComponentFailure(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	for name, solve := range map[string]func(objective.PlanObjective) error{
		"heuristic fixed": func(goal objective.PlanObjective) error {
			_, err := (Heuristic{}).PackFixed(context.Background(), request, instances, Options{PlanObjective: goal})
			return err
		},
		"heuristic all": func(goal objective.PlanObjective) error {
			_, err := (Heuristic{}).PackAll(context.Background(), request, Options{PlanObjective: goal})
			return err
		},
		"exact fixed": func(goal objective.PlanObjective) error {
			_, err := (Exact{}).PackFixed(context.Background(), request, instances, Options{PlanObjective: goal})
			return err
		},
		"exact all": func(goal objective.PlanObjective) error {
			_, err := (Exact{}).PackAll(context.Background(), request, Options{PlanObjective: goal})
			return err
		},
	} {
		if err := solve(&componentSequence{}); !errors.Is(err, objective.ErrInvalidObjective) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestExactTreatsObjectiveNondeterminismAsInvariantFailure(t *testing.T) {
	t.Parallel()

	request := internalRequest(t)
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	for name, solve := range map[string]func() error{
		"fixed": func() error {
			_, err := (Exact{}).PackFixed(context.Background(), request, instances, Options{PlanObjective: &changingObjective{}})
			return err
		},
		"all": func() error {
			_, err := (Exact{}).PackAll(context.Background(), request, Options{PlanObjective: &changingObjective{}})
			return err
		},
	} {
		if err := solve(); !errors.Is(err, knapsack.ErrInternalInvariant) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestHeuristicsObserveCancellationBetweenItems(t *testing.T) {
	t.Parallel()
	base := internalRequest(t)
	items := base.Items()
	second := items[0]
	second.ID = "second"
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{Items: append(items, second), Containers: base.Containers(), Resolution: base.Resolution(), Limits: base.Limits()})
	if err != nil {
		t.Fatal(err)
	}
	instances := []knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}
	for name, solve := range map[string]func(context.Context, objective.PlanObjective) (knapsack.Plan, error){
		"fixed": func(ctx context.Context, goal objective.PlanObjective) (knapsack.Plan, error) {
			return (Heuristic{}).PackFixed(ctx, request, instances, Options{PlanObjective: goal})
		},
		"all": func(ctx context.Context, goal objective.PlanObjective) (knapsack.Plan, error) {
			return (Heuristic{}).PackAll(ctx, request, Options{PlanObjective: goal})
		},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		plan, err := solve(ctx, &cancelingObjective{cancel: cancel})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s plan=%s error=%v", name, plan.CanonicalString(), err)
		}
		if plan.Status() != "" || len(plan.Placements()) != 0 {
			t.Fatalf("%s returned an unverified custom-objective plan: %s", name, plan.CanonicalString())
		}
	}
}

func TestHeuristicReturnsVerifiedBestPlanWhenRepackingIsCancelled(t *testing.T) {
	t.Parallel()

	request := internalRequest(t)
	foundImprovementCancellation := false
	for cancelAt := 1; cancelAt <= 24; cancelAt++ {
		ctx, cancel := context.WithCancel(context.Background())
		goal := &countedObjective{cancelAt: cancelAt, cancel: cancel}
		plan, err := (Heuristic{}).PackAll(ctx, request, Options{PlanObjective: goal})
		if !errors.Is(err, context.Canceled) || plan.Work().ImprovementRounds != 1 {
			continue
		}
		foundImprovementCancellation = true
		if plan.Status() != knapsack.StatusBudgetExhausted || plan.Termination() != knapsack.TerminationCancelled {
			t.Fatalf("cancelAt=%d plan=%s error=%v", cancelAt, plan.CanonicalString(), err)
		}
		stableGoal := &countedObjective{}
		if result := verify.Plan(request, plan, verify.RequireAll().WithObjective(stableGoal)); !result.Valid() {
			t.Fatalf("cancelAt=%d unsafe retained plan: %+v", cancelAt, result.Violations())
		}
		break
	}
	if !foundImprovementCancellation {
		t.Fatal("did not exercise cancellation during the repacking pass")
	}
}

func TestHeuristicPropagatesRepackingObjectiveFailures(t *testing.T) {
	t.Parallel()

	request := internalRequest(t)
	withoutImprovement := request.Limits()
	withoutImprovement.MaxImprovementRounds = 0
	componentCounter := &countedObjective{}
	if _, err := (Heuristic{}).PackAll(context.Background(), request.WithLimits(withoutImprovement), Options{PlanObjective: componentCounter}); err != nil {
		t.Fatal(err)
	}
	if _, err := (Heuristic{}).PackAll(context.Background(), request, Options{PlanObjective: &countedObjective{failAt: componentCounter.calls + 1}}); err == nil || err.Error() != "counted objective failure" {
		t.Fatalf("repacking component error = %v", err)
	}

	comparisonCounter := &countedCompareObjective{}
	if _, err := (Heuristic{}).PackAll(context.Background(), request, Options{PlanObjective: comparisonCounter}); err != nil {
		t.Fatal(err)
	}
	if comparisonCounter.calls == 0 {
		t.Fatal("repacking performed no objective comparisons")
	}
	if _, err := (Heuristic{}).PackAll(context.Background(), request, Options{PlanObjective: &countedCompareObjective{failAt: comparisonCounter.calls}}); err == nil || err.Error() != "counted comparison failure" {
		t.Fatalf("repacking comparison error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if plan, err := (Heuristic{}).PackAll(ctx, request, Options{PlanObjective: &countedCompareObjective{cancelAt: comparisonCounter.calls, cancel: cancel}}); !errors.Is(err, context.Canceled) || plan.Work().ImprovementRounds != 1 {
		t.Fatalf("repacking comparison cancellation plan=%s error=%v", plan.CanonicalString(), err)
	}
}

func TestFixedHeuristicKeepsAFailedGroupOutOfOtherContainers(t *testing.T) {
	t.Parallel()
	base := internalRequest(t)
	prototype := base.Items()[0]
	prototype.Group = "linked"
	items := make([]knapsack.NormalizedItem, 3)
	for index, id := range []string{"a", "b", "c"} {
		items[index] = prototype
		items[index].ID = id
	}
	containers := base.Containers()
	containers[0].Dimensions = geometry.Dimensions{X: 1, Y: 1, Z: 1}
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{Items: items, Containers: containers, Resolution: base.Resolution(), Limits: base.Limits()})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (Heuristic{}).PackFixed(context.Background(), request, []knapsack.ContainerInstance{{ID: "box#2", TypeID: "box"}, {ID: "box#1", TypeID: "box"}}, Options{AllowUnpacked: true})
	if err != nil || len(plan.Placements()) != 0 || len(plan.UnpackedItemIDs()) != 3 {
		t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
	}
	plan, err = (Heuristic{}).PackAll(context.Background(), request, Options{AllowUnpacked: true})
	if err != nil || len(plan.Placements()) != 0 || len(plan.UnpackedItemIDs()) != 3 {
		t.Fatalf("pack-all plan=%s error=%v", plan.CanonicalString(), err)
	}
}

func TestExactPackAllPropagatesLateObjectiveErrorsAndCancellation(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	componentCounter := &countedObjective{}
	_, _ = (Exact{}).PackAll(context.Background(), request, Options{PlanObjective: componentCounter})
	for failAt := 1; failAt <= componentCounter.calls; failAt++ {
		_, err := (Exact{}).PackAll(context.Background(), request, Options{PlanObjective: &countedObjective{failAt: failAt}})
		if err == nil || err.Error() != "counted objective failure" {
			t.Fatalf("failAt %d error = %v", failAt, err)
		}
	}
	comparisonCounter := &countedCompareObjective{}
	_, _ = (Exact{}).PackAll(context.Background(), request, Options{PlanObjective: comparisonCounter})
	for failAt := 1; failAt <= comparisonCounter.calls; failAt++ {
		_, err := (Exact{}).PackAll(context.Background(), request, Options{PlanObjective: &countedCompareObjective{failAt: failAt}})
		if err == nil || err.Error() != "counted comparison failure" {
			t.Fatalf("comparison failAt %d error = %v", failAt, err)
		}
	}
	types := request.Containers()
	second := types[0]
	second.ID = "box2"
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{Items: request.Items(), Containers: append(types, second), Resolution: request.Resolution(), Limits: request.Limits()})
	if err != nil {
		t.Fatal(err)
	}
	for _, cancelAt := range []int{2, 3} {
		ctx, cancel := context.WithCancel(context.Background())
		_, err := (Exact{}).PackAll(ctx, request, Options{PlanObjective: &countedObjective{cancelAt: cancelAt, cancel: cancel}})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelAt %d error = %v", cancelAt, err)
		}
	}
}

func TestPlacementSelectionPropagatesExistingBinCallbackAndStockLimits(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	goal, _ := objective.New(objective.Minimize(objective.ContainerCount))
	var candidates uint64
	if _, _, _, err := chooseHeuristicPlacement(context.Background(), request, request.Items()[0], []*bin{baseInternalBin()}, nil, nil, false, &candidates, Options{Constraints: []constraint.Placement{internalPanic{}}}, goal); !errors.Is(err, constraint.ErrCallbackPanic) {
		t.Fatalf("existing-bin callback error = %v", err)
	}
	types := request.Containers()
	types[0].Stock = knapsack.FiniteStock(1)
	candidates = 0
	bins, _, placed, err := chooseHeuristicPlacement(context.Background(), request, request.Items()[0], nil, types, map[string]uint32{"box": 1}, true, &candidates, Options{}, goal)
	if err != nil || placed || len(bins) != 0 {
		t.Fatalf("bins=%+v placed=%v error=%v", bins, placed, err)
	}
}

func TestSolverVerificationRejectsInvalidGeneratedState(t *testing.T) {
	t.Parallel()
	request := internalRequest(t)
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted})
	if err := verifySolverPlan(request, plan, verify.RequireAll()); !errors.Is(err, knapsack.ErrInternalInvariant) {
		t.Fatalf("error = %v", err)
	}
}

func TestExactLowerBoundNeverExceedsRelaxedBruteForceOptimum(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		items      []knapsack.NormalizedItem
		containers []knapsack.NormalizedContainer
		want       int
	}{
		{
			items: []knapsack.NormalizedItem{
				{Dimensions: geometry.Dimensions{X: 2, Y: 1, Z: 1}, Weight: 3},
				{Dimensions: geometry.Dimensions{X: 2, Y: 1, Z: 1}, Weight: 3},
			},
			containers: []knapsack.NormalizedContainer{{Dimensions: geometry.Dimensions{X: 4, Y: 1, Z: 1}, MaxContentWeight: 4}},
			want:       2,
		},
		{
			items: []knapsack.NormalizedItem{
				{Dimensions: geometry.Dimensions{X: 3, Y: 1, Z: 1}, Weight: 1},
				{Dimensions: geometry.Dimensions{X: 3, Y: 1, Z: 1}, Weight: 1},
				{Dimensions: geometry.Dimensions{X: 3, Y: 1, Z: 1}, Weight: 1},
			},
			containers: []knapsack.NormalizedContainer{{Dimensions: geometry.Dimensions{X: 4, Y: 1, Z: 1}, MaxContentWeight: 10}},
			want:       3,
		},
		{
			items: []knapsack.NormalizedItem{
				{Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, Weight: 3},
			},
			containers: []knapsack.NormalizedContainer{{
				Dimensions:       geometry.Dimensions{X: 10, Y: 1, Z: 1},
				MaxContentWeight: 10, HasGrossWeight: true, TareWeight: 1, MaxGrossWeight: 3,
			}},
			want: 2,
		},
	} {
		if got := exactContainerLowerBound(test.items, test.containers); got != test.want {
			t.Fatalf("lower bound=%d want=%d", got, test.want)
		}
		// The relaxed brute-force oracle tries every box count and accepts when
		// aggregate volume and mass fit. The production bound must equal this
		// relaxation and therefore cannot prune a feasible geometric solution.
		brute := 1
		for ; brute <= len(test.items); brute++ {
			if relaxedAggregateFits(test.items, test.containers, brute) {
				break
			}
		}
		if got := exactContainerLowerBound(test.items, test.containers); got != brute {
			t.Fatalf("lower bound=%d relaxed brute force=%d", got, brute)
		}
	}
	huge := new(big.Int).Lsh(big.NewInt(1), 100)
	if got := ceilingQuotient(huge, 1); got != int(^uint(0)>>1) {
		t.Fatalf("huge quotient = %d", got)
	}
	if got := ceilingQuotient(big.NewInt(1), 0); got != int(^uint(0)>>1) {
		t.Fatalf("zero-capacity quotient = %d", got)
	}
}

func TestContainerMultisetSymmetryMatchesBruteForceCanonicalForms(t *testing.T) {
	t.Parallel()
	var multisets []string
	err := enumerateTypeMultisets(context.Background(), 3, 2, func(selected []int) error {
		multisets = append(multisets, fmt.Sprint(selected))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	bruteCanonical := make(map[string]struct{})
	for left := range 3 {
		for right := range 3 {
			pair := []int{left, right}
			slices.Sort(pair)
			bruteCanonical[fmt.Sprint(pair)] = struct{}{}
		}
	}
	if len(multisets) != len(bruteCanonical) {
		t.Fatalf("multisets=%v brute=%v", multisets, bruteCanonical)
	}
	for _, multiset := range multisets {
		if _, ok := bruteCanonical[multiset]; !ok {
			t.Fatalf("missing brute-force canonical form %s", multiset)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := enumerateTypeMultisets(cancelled, 1, 1, func([]int) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func relaxedAggregateFits(items []knapsack.NormalizedItem, containers []knapsack.NormalizedContainer, count int) bool {
	var itemVolume, itemWeight int64
	for _, item := range items {
		volume, _ := item.Dimensions.Volume()
		itemVolume += volume
		itemWeight += item.Weight
	}
	var maxVolume, maxWeight int64
	for _, container := range containers {
		volume, _ := container.Dimensions.Volume()
		if volume > maxVolume {
			maxVolume = volume
		}
		weight := container.MaxContentWeight
		if container.HasGrossWeight && container.MaxGrossWeight-container.TareWeight < weight {
			weight = container.MaxGrossWeight - container.TareWeight
		}
		if weight > maxWeight {
			maxWeight = weight
		}
	}
	return itemVolume <= int64(count)*maxVolume && itemWeight <= int64(count)*maxWeight
}

func TestPhysicalPlacementRejectsClassRatioAndStackDepth(t *testing.T) {
	t.Parallel()
	item, target := baseInternalItem(), baseInternalBin()
	box, _ := geometry.NewCuboid(geometry.Point{}, item.Dimensions)
	target.info.AllowedClasses = []string{"allowed"}
	if physicalPlacementAllowed(item, target, box, nil) {
		t.Fatal("ineligible class accepted")
	}
	target.info.AllowedClasses = nil
	item.MinimumSupportPPM = 1_000_000
	floating, _ := geometry.NewCuboid(geometry.Point{Z: 1}, item.Dimensions)
	if physicalPlacementAllowed(item, target, floating, nil) {
		t.Fatal("unsupported item accepted")
	}
	base, middle := baseInternalItem(), baseInternalItem()
	base.ID, base.MaxStackCount = "base", 1
	middle.ID = "middle"
	target.items = []knapsack.NormalizedItem{base, middle}
	target.placements = []knapsack.Placement{
		{ItemID: "base", Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}},
		{ItemID: "middle", Origin: geometry.Point{Z: 1}, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}},
	}
	item.ID, item.MinimumSupportPPM = "top", 0
	top, _ := geometry.NewCuboid(geometry.Point{Z: 2}, item.Dimensions)
	if physicalPlacementAllowed(item, target, top, []string{"middle"}) {
		t.Fatal("excess stack depth accepted")
	}
}

func TestCenterOfGravityCandidateAndMemoryBounds(t *testing.T) {
	t.Parallel()

	if got := gravityOrigins(1, 2, 0, 1_000_000); got != nil {
		t.Fatalf("oversized item origins = %v", got)
	}
	if got := gravityOrigins(10, 2, 0, 1_000_000); !slices.Equal(got, []int64{0, 4, 8}) {
		t.Fatalf("clamped origins = %v", got)
	}
	target := &bin{info: knapsack.NormalizedContainer{
		Dimensions:      geometry.Dimensions{X: 100, Y: 100, Z: 100},
		CenterOfGravity: &knapsack.CenterOfGravityBounds{},
	}}
	if _, ok := exactPoints(target, 1_000); ok {
		t.Fatal("full-lattice coordinates exceeded their pre-allocation budget")
	}
}

func TestCenterOfGravityDisabledFastPathDoesNotAllocate(t *testing.T) {
	item, target := baseInternalItem(), baseInternalBin()
	placement := knapsack.Placement{
		ItemID: item.ID, ContainerID: target.instance.ID,
		Dimensions: item.Dimensions, Weight: item.Weight,
	}
	allocations := testing.AllocsPerRun(100, func() {
		if !centerOfGravityAllowedWith(item, target, placement) {
			t.Fatal("disabled center-of-gravity policy rejected a candidate")
		}
	})
	if allocations != 0 {
		t.Fatalf("disabled center-of-gravity policy allocated %.0f objects", allocations)
	}
}

func TestBuildPlanRecomputesFinalSupportRelationships(t *testing.T) {
	t.Parallel()
	target := baseInternalBin()
	target.placements = []knapsack.Placement{
		{ItemID: "top", ContainerID: target.instance.ID, Origin: geometry.Point{Z: 1}, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}, SupporterIDs: []string{"stale"}},
		{ItemID: "base", ContainerID: target.instance.ID, Dimensions: geometry.Dimensions{X: 1, Y: 1, Z: 1}},
	}
	plan := buildPlan([]*bin{target}, nil, knapsack.StatusFeasible, knapsack.TerminationCompleted, 0, 0, nil)
	placements := plan.Placements()
	if !slices.Equal(placements[0].SupporterIDs, []string{"base"}) {
		t.Fatalf("supporters = %v, want final geometric supporters", placements[0].SupporterIDs)
	}
	if len(placements[1].SupporterIDs) != 0 {
		t.Fatalf("floor placement supporters = %v, want none", placements[1].SupporterIDs)
	}
}

func internalRequest(t *testing.T) knapsack.NormalizedRequest {
	t.Helper()
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items:      []knapsack.NormalizedItem{baseInternalItem()},
		Containers: []knapsack.NormalizedContainer{baseInternalBin().info},
		Resolution: knapsack.Resolution{
			Length: measurement.MustNew(decimal.New(1), measurement.Metre),
			Mass:   measurement.MustNew(decimal.New(1), measurement.Kilogram),
		}, Limits: knapsack.DefaultLimits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return request
}
