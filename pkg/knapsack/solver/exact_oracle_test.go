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
	"github.com/faustbrian/golib/pkg/measurement"
)

type gridCell struct{ x, y int64 }

func TestExactFixedAgreesWithExhaustiveGridOracle(t *testing.T) {
	t.Parallel()

	shapes := []geometry.Dimensions{
		{X: 1, Y: 1, Z: 1},
		{X: 1, Y: 2, Z: 1},
		{X: 2, Y: 1, Z: 1},
		{X: 2, Y: 2, Z: 1},
	}
	for containerX := int64(1); containerX <= 3; containerX++ {
		for containerY := int64(1); containerY <= 3; containerY++ {
			container := geometry.Dimensions{X: containerX, Y: containerY, Z: 1}
			for _, first := range shapes {
				for _, second := range shapes {
					name := fmt.Sprintf("box_%dx%d_items_%dx%d_%dx%d", containerX, containerY, first.X, first.Y, second.X, second.Y)
					t.Run(name, func(t *testing.T) {
						items := []knapsack.NormalizedItem{
							oracleItem(t, "a", first),
							oracleItem(t, "b", second),
						}
						request := oracleRequest(t, items, container)
						wantFeasible := bruteGridFeasible(items, container, nil, 0)
						plan, err := (solver.Exact{}).PackFixed(context.Background(), request,
							[]knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}, solver.Options{})
						gotFeasible := err == nil
						if gotFeasible != wantFeasible {
							t.Fatalf("feasible=%v, want %v; plan=%s error=%v", gotFeasible, wantFeasible, plan.CanonicalString(), err)
						}
						if gotFeasible {
							if plan.Status() != knapsack.StatusOptimal {
								t.Fatalf("feasible plan status = %s", plan.Status())
							}
							if result := verify.Plan(request, plan, verify.RequireAll()); !result.Valid() {
								t.Fatalf("exact plan invalid: %+v", result.Violations())
							}
						} else if !errors.Is(err, knapsack.ErrProvenInfeasible) || plan.Status() != knapsack.StatusInfeasible {
							t.Fatalf("infeasible result lacks proof: plan=%s error=%v", plan.CanonicalString(), err)
						}
					})
				}
			}
		}
	}
}

func TestExactCenterOfGravityAgreesWithIndependentGridOracle(t *testing.T) {
	t.Parallel()

	items := []knapsack.NormalizedItem{oracleItem(t, "a", geometry.Dimensions{X: 1, Y: 1, Z: 1})}
	for minimum := uint32(0); minimum <= 1_000_000; minimum += 125_000 {
		for maximum := minimum; maximum <= 1_000_000; maximum += 125_000 {
			request := oracleRequest(t, items, geometry.Dimensions{X: 4, Y: 1, Z: 1})
			spec := knapsack.NormalizedSpec{
				Items: request.Items(), Containers: request.Containers(),
				Resolution: request.Resolution(), Limits: request.Limits(),
			}
			spec.Containers[0].CenterOfGravity = &knapsack.CenterOfGravityBounds{
				MinXPPM: minimum, MaxXPPM: maximum,
				MinYPPM: 500_000, MaxYPPM: 500_000,
				MinZPPM: 500_000, MaxZPPM: 500_000,
			}
			request, err := knapsack.NewNormalizedRequest(spec)
			if err != nil {
				t.Fatal(err)
			}
			want := false
			for origin := range int64(4) {
				doubledCenter := 2*origin + 1
				want = want || doubledCenter*1_000_000 >= 8*int64(minimum) &&
					doubledCenter*1_000_000 <= 8*int64(maximum)
			}
			plan, err := (solver.Exact{}).PackFixed(context.Background(), request,
				[]knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}, solver.Options{})
			got := err == nil
			if got != want {
				t.Fatalf("bounds %d..%d feasible=%v, want %v: %s %v", minimum, maximum, got, want, plan.CanonicalString(), err)
			}
		}
	}
}

func TestExactCenterOfGravityGridFailsBeforeOversizedAllocation(t *testing.T) {
	t.Parallel()

	request := oracleRequest(t,
		[]knapsack.NormalizedItem{oracleItem(t, "item", geometry.Dimensions{X: 1, Y: 1, Z: 1})},
		geometry.Dimensions{X: 100, Y: 100, Z: 100},
	)
	spec := knapsack.NormalizedSpec{
		Items: request.Items(), Containers: request.Containers(),
		Resolution: request.Resolution(), Limits: request.Limits(),
	}
	spec.Limits.MaxMemoryBytes = 1 << 20
	spec.Containers[0].CenterOfGravity = &knapsack.CenterOfGravityBounds{}
	request, err := knapsack.NewNormalizedRequest(spec)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := (solver.Exact{}).PackFixed(context.Background(), request,
		[]knapsack.ContainerInstance{{ID: "box#1", TypeID: "box"}}, solver.Options{})
	if !errors.Is(err, knapsack.ErrMemoryBudgetExhausted) || len(plan.Placements()) != 0 {
		t.Fatalf("plan=%s error=%v", plan.CanonicalString(), err)
	}
}

func oracleItem(t *testing.T, id string, dimensions geometry.Dimensions) knapsack.NormalizedItem {
	t.Helper()
	orientations, err := geometry.Orientations(dimensions)
	if err != nil {
		t.Fatal(err)
	}
	return knapsack.NormalizedItem{ID: id, Dimensions: dimensions, Weight: 1, Orientations: orientations}
}

func oracleRequest(t *testing.T, items []knapsack.NormalizedItem, dimensions geometry.Dimensions) knapsack.NormalizedRequest {
	t.Helper()
	limits := knapsack.DefaultLimits()
	limits.MaxSearchNodes = 1_000_000
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: items,
		Containers: []knapsack.NormalizedContainer{{
			ID: "box", Dimensions: dimensions,
			MaxContentWeight: int64(len(items)), Stock: knapsack.FiniteStock(1),
		}},
		Resolution: knapsack.Resolution{
			Length: exactQuantity(1, measurement.Metre),
			Mass:   exactQuantity(1, measurement.Kilogram),
		},
		Limits: limits,
	})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func bruteGridFeasible(items []knapsack.NormalizedItem, container geometry.Dimensions, occupied map[gridCell]struct{}, index int) bool {
	if index == len(items) {
		return true
	}
	if occupied == nil {
		occupied = make(map[gridCell]struct{})
	}
	item := items[index]
	for _, dimensions := range independentOrientations(item.Dimensions) {
		for y := int64(0); y+dimensions.Y <= container.Y; y++ {
			for x := int64(0); x+dimensions.X <= container.X; x++ {
				cells := make([]gridCell, 0, dimensions.X*dimensions.Y)
				available := true
				for offsetY := range dimensions.Y {
					for offsetX := range dimensions.X {
						cell := gridCell{x + offsetX, y + offsetY}
						if _, used := occupied[cell]; used {
							available = false
						}
						cells = append(cells, cell)
					}
				}
				if !available {
					continue
				}
				for _, cell := range cells {
					occupied[cell] = struct{}{}
				}
				if bruteGridFeasible(items, container, occupied, index+1) {
					return true
				}
				for _, cell := range cells {
					delete(occupied, cell)
				}
			}
		}
	}
	return false
}

func independentOrientations(dimensions geometry.Dimensions) []geometry.Dimensions {
	result := []geometry.Dimensions{dimensions}
	rotated := geometry.Dimensions{X: dimensions.Y, Y: dimensions.X, Z: dimensions.Z}
	if rotated != dimensions {
		result = append(result, rotated)
	}
	return result
}
