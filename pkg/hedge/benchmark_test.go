package hedge_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/failsafe-go/failsafe-go"
	"github.com/failsafe-go/failsafe-go/hedgepolicy"
	"github.com/faustbrian/golib/pkg/hedge"
)

func BenchmarkDirectSuccess(benchmark *testing.B) {
	operation := func(context.Context) (int, error) { return 1, nil }
	ctx := context.Background()
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		_, _ = operation(ctx)
	}
}

func BenchmarkFailsafeGoNoHedgeSuccess(benchmark *testing.B) {
	policy := hedgepolicy.NewBuilderWithDelay[int](time.Hour).WithMaxHedges(1).Build()
	executor := failsafe.With(policy)
	operation := func() (int, error) { return 1, nil }
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		_, _ = executor.Get(operation)
	}
}

func BenchmarkPolicyOneHedgeWinner(benchmark *testing.B) {
	benchmarkPolicyHedgeWinner(benchmark, 1)
}

func BenchmarkPolicyTwoHedgeWinner(benchmark *testing.B) {
	benchmarkPolicyHedgeWinner(benchmark, 2)
}

func benchmarkPolicyHedgeWinner(benchmark *testing.B, maxHedges uint) {
	budget, _ := hedge.NewOutstandingBudget(maxHedges)
	policy, _ := hedge.NewPolicy(hedge.Config[int]{
		MaxHedges: maxHedges, ReplaySafe: true, Delay: time.Nanosecond, TotalTimeout: time.Second,
		CleanupTimeout: time.Second, Clock: hedge.RealClock{}, Budget: budget,
		Classifier: hedge.ClassifyFunc[int](func(context.Context, hedge.AttemptResult[int]) (hedge.Classification, error) {
			return hedge.ClassificationSuccess, nil
		}),
		Disposer: hedge.DisposeFunc[int](func(context.Context, int) error { return nil }),
		Resource: "benchmark", FactoryFailureMode: hedge.FactoryFailureStop,
	})
	factory := hedge.AttemptFactoryFunc[int](func(info hedge.AttemptInfo) (hedge.Attempt[int], string, error) {
		if info.Ordinal == maxHedges {
			return func(context.Context) (int, error) { return 1, nil }, "pod-b", nil
		}
		return func(ctx context.Context) (int, error) { <-ctx.Done(); return 0, ctx.Err() }, "pod-a", nil
	})
	ctx := context.Background()
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		_, report, _ := hedge.Do(ctx, policy, factory)
		_ = report.Wait(ctx)
	}
}

func BenchmarkFailsafeGoOneHedgeWinner(benchmark *testing.B) {
	benchmarkFailsafeHedgeWinner(benchmark, 1)
}

func BenchmarkFailsafeGoTwoHedgeWinner(benchmark *testing.B) {
	benchmarkFailsafeHedgeWinner(benchmark, 2)
}

func benchmarkFailsafeHedgeWinner(benchmark *testing.B, maxHedges int) {
	policy := hedgepolicy.NewBuilderWithDelay[int](time.Nanosecond).WithMaxHedges(maxHedges).Build()
	executor := failsafe.With(policy)
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		var calls atomic.Uint32
		_, _ = executor.GetWithExecution(func(execution failsafe.Execution[int]) (int, error) {
			if calls.Add(1) <= uint32(maxHedges) { //nolint:gosec // benchmark inputs are the constants 1 and 2
				<-execution.Canceled()
				return 0, context.Canceled
			}
			return 1, nil
		})
	}
}

func BenchmarkPolicyNoHedgeSuccess(benchmark *testing.B) {
	budget, _ := hedge.NewOutstandingBudget(1)
	policy, _ := hedge.NewPolicy(hedge.Config[int]{
		MaxHedges: 1, ReplaySafe: true, Delay: time.Hour, TotalTimeout: time.Hour,
		CleanupTimeout: time.Second, Clock: hedge.RealClock{}, Budget: budget,
		Classifier: hedge.ClassifyFunc[int](func(context.Context, hedge.AttemptResult[int]) (hedge.Classification, error) {
			return hedge.ClassificationSuccess, nil
		}),
		Disposer: hedge.DisposeFunc[int](func(context.Context, int) error { return nil }),
		Resource: "benchmark", FactoryFailureMode: hedge.FactoryFailureStop,
	})
	factory := hedge.AttemptFactoryFunc[int](func(hedge.AttemptInfo) (hedge.Attempt[int], string, error) {
		return func(context.Context) (int, error) { return 1, nil }, "pod", nil
	})
	ctx := context.Background()
	benchmark.ReportAllocs()
	for benchmark.Loop() {
		_, _, _ = hedge.Do(ctx, policy, factory)
	}
}
