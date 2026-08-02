package hedge_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/hedge"
)

func TestBulkheadBreakerAndRateAccountingCanBePerAttemptOrLogical(t *testing.T) {
	t.Parallel()

	config := validConfig()
	config.Delay = time.Millisecond
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	var perAttemptAdmission atomic.Uint32
	var perAttemptBreaker atomic.Uint32
	var perAttemptRate atomic.Uint32
	var logicalAdmission atomic.Uint32
	var logicalBreaker atomic.Uint32
	var logicalRate atomic.Uint32

	logicalAdmission.Add(1) // outside the policy: one logical bulkhead permit
	logicalRate.Add(1)      // outside the policy: one shared logical rate permit
	factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(ctx context.Context) (string, error) {
			perAttemptAdmission.Add(1) // inside: dependency admission per attempt
			perAttemptRate.Add(1)      // inside: dependency permit per attempt
			perAttemptBreaker.Add(1)   // inside: dependency health per attempt
			if info.Ordinal == 0 {
				<-ctx.Done()
				return "loser", ctx.Err()
			}
			return "winner", nil
		}, "pod", nil
	})
	value, report, gotErr := hedge.Do(context.Background(), policy, factory)
	logicalBreaker.Add(1) // outside: one logical outcome
	if gotErr != nil || value != "winner" {
		t.Fatalf("Do() = (%q, %v)", value, gotErr)
	}
	if err := report.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if perAttemptAdmission.Load() != 2 || perAttemptRate.Load() != 2 || perAttemptBreaker.Load() != 2 {
		t.Fatalf("per-attempt accounting = admission:%d rate:%d breaker:%d", perAttemptAdmission.Load(), perAttemptRate.Load(), perAttemptBreaker.Load())
	}
	if logicalAdmission.Load() != 1 || logicalRate.Load() != 1 || logicalBreaker.Load() != 1 {
		t.Fatalf("logical accounting = admission:%d rate:%d breaker:%d", logicalAdmission.Load(), logicalRate.Load(), logicalBreaker.Load())
	}
}

func TestAttemptDeadlineIsEarlierThanTotalDeadline(t *testing.T) {
	t.Parallel()

	budget, err := hedge.NewOutstandingBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	config := hedge.Config[time.Duration]{
		MaxHedges: 1, ReplaySafe: true, Delay: time.Hour, TotalTimeout: time.Second,
		AttemptTimeout: 100 * time.Millisecond, CleanupTimeout: time.Second,
		Clock: hedge.RealClock{}, Budget: budget,
		Classifier: hedge.ClassifyFunc[time.Duration](func(_ context.Context, result hedge.AttemptResult[time.Duration]) (hedge.Classification, error) {
			if result.Err == nil {
				return hedge.ClassificationSuccess, nil
			}
			return hedge.ClassificationFailure, nil
		}),
		Disposer: hedge.DisposeFunc[time.Duration](func(context.Context, time.Duration) error { return nil }),
		Resource: "deadline", FactoryFailureMode: hedge.FactoryFailureStop,
	}
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	factory := hedge.AttemptFactoryFunc[time.Duration](func(hedge.AttemptInfo) (hedge.Attempt[time.Duration], string, error) {
		return func(ctx context.Context) (time.Duration, error) {
			deadline, ok := ctx.Deadline()
			if !ok {
				return 0, errors.New("missing attempt deadline")
			}
			return time.Until(deadline), nil
		}, "pod", nil
	})
	duration, _, gotErr := hedge.Do(context.Background(), policy, factory)
	if gotErr != nil || duration <= 0 || duration > config.AttemptTimeout {
		t.Fatalf("attempt deadline remaining = %v, error = %v", duration, gotErr)
	}
}

func TestCacheHitPreventsHedgedDependencyLookup(t *testing.T) {
	t.Parallel()

	var factories atomic.Uint32
	lookup := func(ctx context.Context, hit bool) string {
		if hit {
			return "cached"
		}
		policy, _ := hedge.NewPolicy(validConfig())
		value, _, _ := hedge.Do(ctx, policy, hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
			factories.Add(1)
			return func(context.Context) (string, error) { return "dependency", nil }, "pod", nil
		}))
		return value
	}
	if value := lookup(context.Background(), true); value != "cached" || factories.Load() != 0 {
		t.Fatalf("cache result = %q, factories = %d", value, factories.Load())
	}
}

func TestAdaptiveThrottleRejectionIsTerminalByDefault(t *testing.T) {
	t.Parallel()

	throttled := errors.New("adaptive throttle rejected")
	config := validConfig()
	config.Delay = 50 * time.Millisecond
	config.Classifier = hedge.ClassifyFunc[string](func(_ context.Context, result hedge.AttemptResult[string]) (hedge.Classification, error) {
		if errors.Is(result.Err, throttled) {
			return hedge.ClassificationTerminal, nil
		}
		return hedge.ClassificationFailure, nil
	})
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	factory := hedge.AttemptFactoryFunc[string](func(hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		return func(context.Context) (string, error) { return "", throttled }, "local", nil
	})
	_, report, gotErr := hedge.Do(context.Background(), policy, factory)
	if !errors.Is(gotErr, throttled) || report.Reason != hedge.ReasonTerminalFailure || report.HedgesStarted != 0 {
		t.Fatalf("Do() = (%+v, %v)", report, gotErr)
	}
}

func TestRetryAndHedgeLayersShareOneHardAdditionalWorkBound(t *testing.T) {
	t.Parallel()

	clock := newManualClock()
	budget, err := hedge.NewOutstandingBudget(1)
	if err != nil {
		t.Fatal(err)
	}
	config := validConfig()
	config.Clock = clock
	config.MaxHedges = 2
	config.Delay = time.Millisecond
	config.Budget = budget
	budgetDenied := make(outcomeSignal, 1)
	config.Observer = budgetDenied
	policy, err := hedge.NewPolicy(config)
	if err != nil {
		t.Fatal(err)
	}
	releaseOriginal := make(chan struct{})
	started := make(chan uint, 3)
	factory := hedge.AttemptFactoryFunc[string](func(info hedge.AttemptInfo) (hedge.Attempt[string], string, error) {
		started <- info.Ordinal
		if info.Ordinal == 0 {
			return func(context.Context) (string, error) { <-releaseOriginal; return "original", nil }, "pod-a", nil
		}
		return func(ctx context.Context) (string, error) { <-ctx.Done(); return "hedge", ctx.Err() }, "pod-b", nil
	})
	done := make(chan hedge.Report, 1)
	go func() { _, report, _ := hedge.Do(context.Background(), policy, factory); done <- report }()
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
		t.Fatal(err)
	}
}

type outcomeSignal chan hedge.Outcome

func (signal outcomeSignal) TryObserve(observation hedge.Observation) bool {
	if observation.Outcome == hedge.OutcomeBudgetDenied {
		signal <- observation.Outcome
	}
	return true
}

func TestFleetAmplificationEqualsReplicaMaximumTimesPerPodBudget(t *testing.T) {
	t.Parallel()

	const replicas = 5
	const perPodBudget = 3
	total := 0
	for range replicas {
		budget, err := hedge.NewOutstandingBudget(perPodBudget)
		if err != nil {
			t.Fatal(err)
		}
		permits := make([]hedge.Permit, 0, perPodBudget)
		for range perPodBudget {
			permit, admitted := budget.TryAcquire("resource")
			if !admitted {
				t.Fatal("per-pod budget denied below bound")
			}
			permits = append(permits, permit)
			total++
		}
		if permit, admitted := budget.TryAcquire("resource"); admitted || permit != nil {
			t.Fatal("per-pod budget admitted above bound")
		}
		for _, permit := range permits {
			permit.Release()
		}
	}
	if total != replicas*perPodBudget {
		t.Fatalf("fleet additional attempts = %d", total)
	}
}
