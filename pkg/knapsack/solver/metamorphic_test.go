package solver_test

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/geometry"
	"github.com/faustbrian/golib/pkg/knapsack/solver"
	"github.com/faustbrian/golib/pkg/knapsack/verify"
)

func TestHeuristicIsInvariantToInputPermutation(t *testing.T) {
	t.Parallel()
	request := exactRequest(t, 4, 1)
	items, containers := request.Items(), request.Containers()
	slices.Reverse(items)
	slices.Reverse(containers)
	permuted, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: items, Containers: containers, Resolution: request.Resolution(), Limits: request.Limits(),
	})
	if err != nil {
		t.Fatal(err)
	}

	originalPlan, err := (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	permutedPlan, err := (solver.Heuristic{}).PackAll(context.Background(), permuted, solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if originalPlan.CanonicalString() != permutedPlan.CanonicalString() {
		t.Fatalf("permutation changed plan:\n%s\n%s", originalPlan.CanonicalString(), permutedPlan.CanonicalString())
	}
}

func TestHeuristicPackingScalesWithTheIntegerLattice(t *testing.T) {
	t.Parallel()
	request := exactRequest(t, 4, 1)
	const factor int64 = 3
	items := request.Items()
	for index := range items {
		items[index].Dimensions = scaleDimensions(t, items[index].Dimensions, factor)
	}
	containers := request.Containers()
	for index := range containers {
		containers[index].Dimensions = scaleDimensions(t, containers[index].Dimensions, factor)
	}
	scaled, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: items, Containers: containers, Resolution: request.Resolution(), Limits: request.Limits(),
	})
	if err != nil {
		t.Fatal(err)
	}

	basePlan, err := (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	scaledPlan, err := (solver.Heuristic{}).PackAll(context.Background(), scaled, solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	basePlacements, scaledPlacements := basePlan.Placements(), scaledPlan.Placements()
	if len(basePlacements) != len(scaledPlacements) || basePlan.Status() != scaledPlan.Status() {
		t.Fatalf("base=%s scaled=%s", basePlan.CanonicalString(), scaledPlan.CanonicalString())
	}
	for index := range basePlacements {
		base, scaledPlacement := basePlacements[index], scaledPlacements[index]
		if base.ItemID != scaledPlacement.ItemID || base.ContainerID != scaledPlacement.ContainerID ||
			scaledPlacement.Origin.X != base.Origin.X*factor || scaledPlacement.Origin.Y != base.Origin.Y*factor || scaledPlacement.Origin.Z != base.Origin.Z*factor ||
			scaledPlacement.Dimensions.X != base.Dimensions.X*factor || scaledPlacement.Dimensions.Y != base.Dimensions.Y*factor || scaledPlacement.Dimensions.Z != base.Dimensions.Z*factor {
			t.Fatalf("placement did not scale: base=%+v scaled=%+v", base, scaledPlacement)
		}
	}
	if result := verify.Plan(scaled, scaledPlan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("scaled plan invalid: %+v", result.Violations())
	}
}

func TestIncreasingContainerCapacityPreservesFeasibility(t *testing.T) {
	t.Parallel()
	request := exactRequest(t, 4, 1)
	containers := request.Containers()
	containers[0].Dimensions.X++
	containers[0].MaxContentWeight++
	larger, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: request.Items(), Containers: containers, Resolution: request.Resolution(), Limits: request.Limits(),
	})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), larger, solver.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status() != knapsack.StatusFeasible {
		t.Fatalf("status = %s", plan.Status())
	}
	if result := verify.Plan(larger, plan, verify.RequireAll()); !result.Valid() {
		t.Fatalf("larger-capacity plan invalid: %+v", result.Violations())
	}
}

func TestSolverEntryPointsHonorExpiredDeadlines(t *testing.T) {
	t.Parallel()
	request := exactRequest(t, 4, 1)
	instances := []knapsack.ContainerInstance{{ID: "box#000001", TypeID: "box"}}
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel()
	for name, solve := range map[string]func() error{
		"exact pack all": func() error { _, err := (solver.Exact{}).PackAll(ctx, request, solver.Options{}); return err },
		"exact fixed": func() error {
			_, err := (solver.Exact{}).PackFixed(ctx, request, instances, solver.Options{})
			return err
		},
		"heuristic pack all": func() error { _, err := (solver.Heuristic{}).PackAll(ctx, request, solver.Options{}); return err },
		"heuristic fixed": func() error {
			_, err := (solver.Heuristic{}).PackFixed(ctx, request, instances, solver.Options{})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := solve(); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func scaleDimensions(t *testing.T, dimensions geometry.Dimensions, factor int64) geometry.Dimensions {
	t.Helper()
	if dimensions.X > math.MaxInt64/factor || dimensions.Y > math.MaxInt64/factor || dimensions.Z > math.MaxInt64/factor {
		t.Fatal("test scale overflows")
	}
	return geometry.Dimensions{X: dimensions.X * factor, Y: dimensions.Y * factor, Z: dimensions.Z * factor}
}
