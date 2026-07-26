package solver_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
	"github.com/faustbrian/golib/pkg/math/decimal"
	"github.com/faustbrian/golib/pkg/measurement"
)

func FuzzHeuristic(f *testing.F) {
	f.Add(int64(1), int64(2))
	f.Fuzz(func(t *testing.T, itemX, boxX int64) {
		if itemX <= 0 || boxX <= 0 || itemX > 100 || boxX > 100 {
			return
		}
		q := func(value int64, unit measurement.Unit) measurement.Quantity {
			return measurement.MustNew(decimal.New(value), unit)
		}
		item, _ := knapsack.NewItem(knapsack.ItemSpec{ID: "item", Dimensions: knapsack.PhysicalDimensions{X: q(itemX, measurement.Metre), Y: q(1, measurement.Metre), Z: q(1, measurement.Metre)}, Weight: q(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ}})
		box, _ := knapsack.NewContainerType(knapsack.ContainerTypeSpec{ID: "box", InternalDimensions: knapsack.PhysicalDimensions{X: q(boxX, measurement.Metre), Y: q(1, measurement.Metre), Z: q(1, measurement.Metre)}, MaxContentWeight: q(1, measurement.Kilogram), Stock: knapsack.FiniteStock(1)})
		request, err := knapsack.NewRequest([]knapsack.Item{item}, []knapsack.ContainerType{box}, knapsack.Resolution{Length: q(1, measurement.Metre), Mass: q(1, measurement.Kilogram)}, knapsack.DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		plan, err := (solver.Heuristic{}).PackAll(context.Background(), request.Normalized(), solver.Options{AllowUnpacked: true})
		if err != nil {
			t.Fatal(err)
		}
		if result := verify.Plan(request.Normalized(), plan, verify.AllowUnpacked()); !result.Valid() {
			t.Fatalf("invalid plan: %+v", result.Violations())
		}
	})
}

func FuzzFixedContainerPacking(f *testing.F) {
	f.Add(int64(1), int64(2), uint64(100))
	f.Fuzz(func(t *testing.T, itemX, boxX int64, candidateLimit uint64) {
		if itemX <= 0 || boxX <= 0 || itemX > 100 || boxX > 100 {
			return
		}
		request := fuzzRequest(t, []int64{itemX}, []int64{boxX})
		limits := request.Limits()
		limits.MaxCandidatePlacements = candidateLimit%100 + 1
		request = request.WithLimits(limits)
		plan, err := (solver.Heuristic{}).PackFixed(context.Background(), request,
			[]knapsack.ContainerInstance{{ID: "box-0#1", TypeID: "box-0"}},
			solver.Options{AllowUnpacked: true},
		)
		if err != nil && !errors.Is(err, knapsack.ErrBudgetExhausted) {
			t.Fatal(err)
		}
		if result := verify.Plan(request, plan, verify.AllowUnpacked()); !result.Valid() {
			t.Fatalf("invalid fixed plan: %+v", result.Violations())
		}
	})
}

func FuzzVariableContainerSelection(f *testing.F) {
	f.Add(int64(2), int64(1), int64(2))
	f.Fuzz(func(t *testing.T, itemX, smallX, largeX int64) {
		if itemX <= 0 || smallX <= 0 || largeX <= 0 || itemX > 50 || smallX > 50 || largeX > 50 {
			return
		}
		request := fuzzRequest(t, []int64{itemX}, []int64{smallX, largeX})
		first, err := (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{AllowUnpacked: true})
		if err != nil {
			t.Fatal(err)
		}
		second, err := (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{AllowUnpacked: true})
		if err != nil || first.CanonicalString() != second.CanonicalString() {
			t.Fatalf("nondeterministic variable selection: %v", err)
		}
		if result := verify.Plan(request, first, verify.AllowUnpacked()); !result.Valid() {
			t.Fatalf("invalid variable plan: %+v", result.Violations())
		}
	})
}

func FuzzExactTinyAgreement(f *testing.F) {
	f.Add(uint8(2), uint8(2))
	f.Fuzz(func(t *testing.T, itemCount, capacity uint8) {
		itemCount = itemCount%3 + 1
		capacity = capacity%3 + 1
		items := make([]int64, int(itemCount))
		for index := range items {
			items[index] = 1
		}
		request := fuzzRequest(t, items, []int64{int64(capacity)})
		plan, err := (solver.Exact{}).PackAll(context.Background(), request, solver.Options{})
		if err != nil {
			t.Fatal(err)
		}
		want := (int(itemCount) + int(capacity) - 1) / int(capacity)
		if len(plan.Containers()) != want || plan.Status() != knapsack.StatusOptimal {
			t.Fatalf("containers=%d want=%d status=%s", len(plan.Containers()), want, plan.Status())
		}
		if result := verify.Plan(request, plan, verify.RequireAll()); !result.Valid() {
			t.Fatalf("invalid exact plan: %+v", result.Violations())
		}
	})
}

func FuzzCancellationAndBudgets(f *testing.F) {
	f.Add(uint64(1), false)
	f.Add(uint64(10), true)
	f.Fuzz(func(t *testing.T, candidateLimit uint64, cancelBefore bool) {
		request := fuzzRequest(t, []int64{1, 1}, []int64{2})
		limits := request.Limits()
		limits.MaxCandidatePlacements = candidateLimit%32 + 1
		request = request.WithLimits(limits)
		ctx, cancel := context.WithCancel(context.Background())
		if cancelBefore {
			cancel()
		}
		plan, err := (solver.Heuristic{}).PackAll(ctx, request, solver.Options{AllowUnpacked: true})
		cancel()
		if cancelBefore {
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation error = %v", err)
			}
			return
		}
		if err != nil && !errors.Is(err, knapsack.ErrBudgetExhausted) {
			t.Fatal(err)
		}
		if result := verify.Plan(request, plan, verify.AllowUnpacked()); !result.Valid() {
			t.Fatalf("invalid bounded plan: %+v", result.Violations())
		}
	})
}

func fuzzRequest(t *testing.T, itemWidths, boxWidths []int64) knapsack.NormalizedRequest {
	t.Helper()
	q := func(value int64, unit measurement.Unit) measurement.Quantity {
		return measurement.MustNew(decimal.New(value), unit)
	}
	items := make([]knapsack.Item, len(itemWidths))
	for index, width := range itemWidths {
		var err error
		items[index], err = knapsack.NewItem(knapsack.ItemSpec{
			ID:         fmt.Sprintf("item-%d", index),
			Dimensions: knapsack.PhysicalDimensions{X: q(width, measurement.Metre), Y: q(1, measurement.Metre), Z: q(1, measurement.Metre)},
			Weight:     q(1, measurement.Kilogram), Orientations: []geometry.Orientation{geometry.OrientationXYZ},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	containers := make([]knapsack.ContainerType, len(boxWidths))
	for index, width := range boxWidths {
		var err error
		containers[index], err = knapsack.NewContainerType(knapsack.ContainerTypeSpec{
			ID:                 fmt.Sprintf("box-%d", index),
			InternalDimensions: knapsack.PhysicalDimensions{X: q(width, measurement.Metre), Y: q(1, measurement.Metre), Z: q(1, measurement.Metre)},
			MaxContentWeight:   q(int64(len(itemWidths)), measurement.Kilogram), Stock: knapsack.UnlimitedStock(),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	request, err := knapsack.NewRequest(items, containers,
		knapsack.Resolution{Length: q(1, measurement.Metre), Mass: q(1, measurement.Kilogram)},
		knapsack.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return request.Normalized()
}
