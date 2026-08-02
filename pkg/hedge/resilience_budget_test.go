package hedge_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
	"github.com/faustbrian/golib/pkg/resilience"
)

func TestHedgeConsumesAttachedResilienceBudget(t *testing.T) {
	t.Parallel()

	clock := newManualClock()
	budget, err := resilience.NewBudget(resilience.BudgetConfig{
		MaxResources:              1,
		MaxAdditionalPerExecution: 1,
		MaxConcurrentAdditional:   1,
		MaxAdditionalPerWindow:    1,
		AdditionalWindow:          time.Minute,
		PermitTTL:                 time.Minute,
		Clock:                     clock,
	})
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	metadata, err := resilience.NewMetadata("logical", "lookup", "dependency")
	if err != nil {
		t.Fatalf("new metadata: %v", err)
	}
	scope, ctx, err := budget.Start(context.Background(), metadata)
	if err != nil {
		t.Fatalf("start budget: %v", err)
	}
	config := validConfig()
	config.Clock = clock
	config.MaxHedges = 2
	config.Delay = time.Millisecond
	config.Budget = nil
	config.UseResilienceBudget = true
	budgetDenied := make(outcomeSignal, 1)
	config.Observer = budgetDenied
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatalf("new policy: %v", err)
	}
	releaseOriginal := make(chan struct{})
	started := make(chan uint, 2)
	factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		started <- info.Ordinal
		if info.Ordinal == 0 {
			return func(context.Context) (string, error) {
				<-releaseOriginal
				return "original", nil
			}, "pod-a", nil
		}
		return func(ctx context.Context) (string, error) {
			<-ctx.Done()
			return "hedge", ctx.Err()
		}, "pod-b", nil
	})
	done := make(chan hedge.Report, 1)
	go func() {
		_, report, _ := hedge.Do(ctx, policy, factory)
		done <- report
	}()
	<-started
	clock.WaitTimers(2)
	clock.Advance(time.Millisecond)
	<-started
	clock.WaitTimers(3)
	clock.Advance(time.Millisecond)
	if outcome := <-budgetDenied; outcome != hedge.OutcomeBudgetDenied {
		t.Fatalf("outcome = %s", outcome.String())
	}
	close(releaseOriginal)
	report := <-done
	if report.HedgesStarted != 1 || report.BudgetDenied != 1 || report.AttemptsStarted != 2 {
		t.Fatalf("report = %+v", report)
	}
	if err := report.Wait(context.Background()); err != nil {
		t.Fatalf("wait: %v", err)
	}
	if snapshot := scope.Snapshot(); snapshot.AdditionalAdmitted != 1 || snapshot.AdditionalActive != 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestHedgeResilienceBudgetRequiresAttachedScope(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Budget = nil
	config.UseResilienceBudget = true
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatalf("new policy: %v", err)
	}
	called := false
	_, report, err := hedge.Do(context.Background(), policy, hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		called = true
		return func(context.Context) (string, error) { return "", nil }, "pod", nil
	}))
	if !errors.Is(err, resilience.ErrBudgetScopeRequired) || called || report.Reason != hedge.ReasonBudgetFailure {
		t.Fatalf("called = %v, report = %+v, error = %v", called, report, err)
	}
}

func TestHedgeRejectsCompetingBudgetOwners(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.UseResilienceBudget = true
	if _, err := hedge.NewPolicy(config); !errors.Is(err, hedge.ErrInvalidPolicy) {
		t.Fatalf("new policy error = %v", err)
	}
}

func TestHedgeReusesThePhysicalAttemptAlreadyInContext(t *testing.T) {
	t.Parallel()

	budget, err := resilience.NewBudget(resilience.BudgetConfig{
		MaxResources: 1, MaxAdditionalPerExecution: 1,
		MaxConcurrentAdditional: 1, MaxAdditionalPerWindow: 1,
		AdditionalWindow: time.Minute, PermitTTL: time.Minute, Clock: hedge.RealClock{},
	})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := resilience.NewMetadata("logical", "lookup", "dependency")
	if err != nil {
		t.Fatal(err)
	}
	_, ctx, err := budget.Start(context.Background(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	attemptCtx, original, permit, err := resilience.AdmitAttempt(ctx, resilience.OriginOriginal, 0, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	config := validConfig()
	config.Budget = nil
	config.UseResilienceBudget = true
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	value, report, err := hedge.Do(attemptCtx, policy, hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(ctx context.Context) (string, error) {
			current, ok := resilience.AttemptFromContext(ctx)
			if !ok || current != original {
				t.Fatalf("current attempt = (%+v, %v), want %+v", current, ok, original)
			}
			return "ok", nil
		}, "pod", nil
	}))
	if err != nil || value != "ok" || report.Reason != hedge.ReasonNoHedgeNeeded {
		t.Fatalf("value = %q, report = %+v, error = %v", value, report, err)
	}
	if err := report.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := permit.Complete(); err != nil {
		t.Fatal(err)
	}
}

func TestHedgeSharedBudgetDistinguishesCancellationFromInvalidLineage(t *testing.T) {
	t.Parallel()

	t.Run("cancellation", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		scope := &scriptedWorkScope{second: func() error {
			cancel()
			return context.Canceled
		}}
		_, report, err := executeWithScriptedScope(t, ctx, scope)
		if !errors.Is(err, context.Canceled) || report.Reason != hedge.ReasonCallerCanceled {
			t.Fatalf("report = %+v, error = %v", report, err)
		}
	})

	t.Run("invalid lineage", func(t *testing.T) {
		t.Parallel()
		rejection := &resilience.BudgetRejectionError{Reason: resilience.ReasonUnknownParent}
		_, report, err := executeWithScriptedScope(t, context.Background(), &scriptedWorkScope{second: func() error { return rejection }})
		if !errors.Is(err, resilience.ErrBudgetRejected) || report.Reason != hedge.ReasonBudgetFailure {
			t.Fatalf("report = %+v, error = %v", report, err)
		}
	})
}

func executeWithScriptedScope(t *testing.T, ctx context.Context, scope *scriptedWorkScope) (string, hedge.Report, error) {
	t.Helper()
	attached, err := resilience.WithBudgetScope(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	clock := newManualClock()
	config := validConfig()
	config.Clock = clock
	config.Delay = time.Millisecond
	config.Budget = nil
	config.UseResilienceBudget = true
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		value  string
		report hedge.Report
		err    error
	}
	done := make(chan result, 1)
	go func() {
		value, report, executeErr := hedge.Do(attached, policy, hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
			return func(ctx context.Context) (string, error) {
				<-ctx.Done()
				return "", ctx.Err()
			}, "pod", nil
		}))
		done <- result{value: value, report: report, err: executeErr}
	}()
	clock.WaitTimers(2)
	clock.Advance(time.Millisecond)
	got := <-done
	return got.value, got.report, got.err
}

type scriptedWorkScope struct {
	calls  atomic.Uint32
	second func() error
}

func (scope *scriptedWorkScope) Acquire(context.Context, resilience.Attempt) (resilience.Permit, error) {
	if scope.calls.Add(1) == 1 {
		return scriptedWorkPermit{}, nil
	}
	return nil, scope.second()
}

func (*scriptedWorkScope) Snapshot() resilience.BudgetSnapshot { return resilience.BudgetSnapshot{} }
func (*scriptedWorkScope) Matches(resilience.Metadata) bool    { return true }
func (*scriptedWorkScope) Close() error                        { return nil }

type scriptedWorkPermit struct{}

func (scriptedWorkPermit) Complete() error { return nil }
