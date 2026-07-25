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

func BenchmarkHeuristicOrdinaryOrder(b *testing.B) {
	request := benchmarkRequest(b, 24, geometry.Dimensions{X: 2, Y: 2, Z: 1}, geometry.Dimensions{X: 8, Y: 6, Z: 2})
	benchmarkSolver(b, request, func() (knapsack.Plan, error) {
		return (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	})
}

func BenchmarkExactTinyFixed(b *testing.B) {
	request := benchmarkRequest(b, 3, geometry.Dimensions{X: 1, Y: 1, Z: 1}, geometry.Dimensions{X: 3, Y: 1, Z: 1})
	instances := []knapsack.ContainerInstance{{ID: "box#000001", TypeID: "box"}}
	benchmarkSolver(b, request, func() (knapsack.Plan, error) {
		return (solver.Exact{}).PackFixed(context.Background(), request, instances, solver.Options{})
	})
}

func BenchmarkVerifyOrdinaryOrder(b *testing.B) {
	request := benchmarkRequest(b, 24, geometry.Dimensions{X: 2, Y: 2, Z: 1}, geometry.Dimensions{X: 8, Y: 6, Z: 2})
	plan, err := (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if result := verify.Plan(request, plan, verify.RequireAll()); !result.Valid() {
			b.Fatalf("invalid plan: %+v", result.Violations())
		}
	}
}

func BenchmarkHeuristicOrientationHeavy(b *testing.B) {
	request := benchmarkRequest(b, 12, geometry.Dimensions{X: 3, Y: 2, Z: 1}, geometry.Dimensions{X: 6, Y: 6, Z: 2})
	items := request.Items()
	for index := range items {
		items[index].Orientations, _ = geometry.Orientations(items[index].Dimensions)
	}
	request = rebuildBenchmarkRequest(b, request, items, request.Containers())
	benchmarkSolver(b, request, func() (knapsack.Plan, error) {
		return (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	})
}

func BenchmarkHeuristicWeightLimited(b *testing.B) {
	request := benchmarkRequest(b, 16, geometry.Dimensions{X: 1, Y: 1, Z: 1}, geometry.Dimensions{X: 8, Y: 1, Z: 1})
	containers := request.Containers()
	containers[0].MaxContentWeight = 4
	request = rebuildBenchmarkRequest(b, request, request.Items(), containers)
	benchmarkSolver(b, request, func() (knapsack.Plan, error) {
		return (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	})
}

func BenchmarkHeuristicStabilityHeavy(b *testing.B) {
	request := benchmarkRequest(b, 12, geometry.Dimensions{X: 1, Y: 1, Z: 1}, geometry.Dimensions{X: 2, Y: 2, Z: 3})
	items := request.Items()
	for index := range items {
		items[index].MinimumSupportPPM = 1_000_000
		maximum := int64(12)
		items[index].MaxSupportedWeight = &maximum
		items[index].MaxStackCount = 2
	}
	containers := request.Containers()
	containers[0].CenterOfGravity = &knapsack.CenterOfGravityBounds{
		MinXPPM: 500_000, MaxXPPM: 500_000,
		MinYPPM: 500_000, MaxYPPM: 500_000,
		MinZPPM: 500_000, MaxZPPM: 500_000,
	}
	request = rebuildBenchmarkRequest(b, request, items, containers)
	benchmarkSolver(b, request, func() (knapsack.Plan, error) {
		return (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	})
}

func BenchmarkHeuristicFiniteStock(b *testing.B) {
	request := benchmarkRequest(b, 12, geometry.Dimensions{X: 1, Y: 1, Z: 1}, geometry.Dimensions{X: 4, Y: 3, Z: 1})
	containers := request.Containers()
	containers[0].Stock = knapsack.FiniteStock(1)
	request = rebuildBenchmarkRequest(b, request, request.Items(), containers)
	benchmarkSolver(b, request, func() (knapsack.Plan, error) {
		return (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	})
}

func BenchmarkHeuristicImpossible(b *testing.B) {
	request := benchmarkRequest(b, 1, geometry.Dimensions{X: 2, Y: 1, Z: 1}, geometry.Dimensions{X: 1, Y: 1, Z: 1})
	benchmarkPartialSolver(b, request, func() (knapsack.Plan, error) {
		return (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{AllowUnpacked: true})
	})
}

func BenchmarkHeuristicFragmentedReservedSpace(b *testing.B) {
	request := benchmarkRequest(b, 12, geometry.Dimensions{X: 1, Y: 1, Z: 1}, geometry.Dimensions{X: 8, Y: 4, Z: 1})
	containers := request.Containers()
	for index := int64(1); index < 8; index += 2 {
		reserved, err := geometry.NewCuboid(geometry.Point{X: index, Y: 1}, geometry.Dimensions{X: 1, Y: 2, Z: 1})
		if err != nil {
			b.Fatal(err)
		}
		containers[0].Reserved = append(containers[0].Reserved, reserved)
	}
	request = rebuildBenchmarkRequest(b, request, request.Items(), containers)
	benchmarkSolver(b, request, func() (knapsack.Plan, error) {
		return (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	})
}

func BenchmarkHeuristicLargeBounded(b *testing.B) {
	request := benchmarkRequest(b, 100, geometry.Dimensions{X: 1, Y: 1, Z: 1}, geometry.Dimensions{X: 10, Y: 10, Z: 1})
	benchmarkSolver(b, request, func() (knapsack.Plan, error) {
		return (solver.Heuristic{}).PackAll(context.Background(), request, solver.Options{})
	})
}

func BenchmarkCancellationLatency(b *testing.B) {
	request := benchmarkRequest(b, 100, geometry.Dimensions{X: 1, Y: 1, Z: 1}, geometry.Dimensions{X: 10, Y: 10, Z: 1})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	b.ResetTimer()
	for b.Loop() {
		if _, err := (solver.Heuristic{}).PackAll(ctx, request, solver.Options{}); !errors.Is(err, context.Canceled) {
			b.Fatalf("cancellation error = %v", err)
		}
	}
}

func benchmarkSolver(b *testing.B, request knapsack.NormalizedRequest, solve func() (knapsack.Plan, error)) {
	b.Helper()
	var plan knapsack.Plan
	b.ResetTimer()
	for b.Loop() {
		var err error
		plan, err = solve()
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if result := verify.Plan(request, plan, verify.RequireAll()); !result.Valid() {
		b.Fatalf("invalid plan: %+v", result.Violations())
	}
	statistics := plan.Statistics()
	work := plan.Work()
	b.ReportMetric(float64(statistics.ContainerCount), "boxes/op")
	b.ReportMetric(float64(work.CandidatePlacements), "candidates/op")
	if work.Nodes > 0 {
		b.ReportMetric(float64(work.Nodes), "nodes/op")
		b.ReportMetric(float64(work.Nodes)*float64(b.N)/b.Elapsed().Seconds(), "nodes/s")
	}
	b.ReportMetric(100*float64(statistics.ItemVolume)/float64(statistics.ContainerVolume), "util_pct")
}

func benchmarkPartialSolver(b *testing.B, request knapsack.NormalizedRequest, solve func() (knapsack.Plan, error)) {
	b.Helper()
	var plan knapsack.Plan
	b.ResetTimer()
	for b.Loop() {
		var err error
		plan, err = solve()
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	if result := verify.Plan(request, plan, verify.AllowUnpacked()); !result.Valid() {
		b.Fatalf("invalid partial plan: %+v", result.Violations())
	}
	b.ReportMetric(float64(len(plan.UnpackedItemIDs())), "unpacked/op")
	b.ReportMetric(float64(plan.Work().CandidatePlacements), "candidates/op")
}

func benchmarkRequest(tb testing.TB, itemCount int, itemDimensions, containerDimensions geometry.Dimensions) knapsack.NormalizedRequest {
	tb.Helper()
	items := make([]knapsack.NormalizedItem, itemCount)
	for index := range items {
		items[index] = knapsack.NormalizedItem{
			ID:           fmt.Sprintf("item-%06d", index),
			Dimensions:   itemDimensions,
			Weight:       1,
			Orientations: []geometry.Orientation{geometry.OrientationXYZ},
		}
	}
	request, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: items,
		Containers: []knapsack.NormalizedContainer{{
			ID: "box", Dimensions: containerDimensions,
			MaxContentWeight: int64(itemCount), Stock: knapsack.UnlimitedStock(),
		}},
		Resolution: knapsack.Resolution{
			Length: quantity(1, measurement.Metre),
			Mass:   quantity(1, measurement.Kilogram),
		},
		Limits: knapsack.DefaultLimits(),
	})
	if err != nil {
		tb.Fatal(err)
	}

	return request
}

func rebuildBenchmarkRequest(tb testing.TB, request knapsack.NormalizedRequest, items []knapsack.NormalizedItem, containers []knapsack.NormalizedContainer) knapsack.NormalizedRequest {
	tb.Helper()
	rebuilt, err := knapsack.NewNormalizedRequest(knapsack.NormalizedSpec{
		Items: items, Containers: containers, Resolution: request.Resolution(), Limits: request.Limits(),
	})
	if err != nil {
		tb.Fatal(err)
	}
	return rebuilt
}
