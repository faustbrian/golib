package resilience_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/resilience"
)

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *manualClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *manualClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

func validBudgetConfig(clock resilience.Clock) resilience.BudgetConfig {
	return resilience.BudgetConfig{
		MaxResources:              2,
		MaxAdditionalPerExecution: 2,
		MaxConcurrentAdditional:   1,
		MaxAdditionalPerWindow:    2,
		AdditionalWindow:          time.Minute,
		PermitTTL:                 time.Minute,
		Clock:                     clock,
	}
}

func metadataFor(t testing.TB, logicalID, resource string) resilience.Metadata {
	t.Helper()
	metadata, err := resilience.NewMetadata(logicalID, "lookup", resource)
	if err != nil {
		t.Fatalf("new metadata: %v", err)
	}
	return metadata
}

func attemptFor(t testing.TB, ordinal uint64, origin resilience.AttemptOrigin, parent uint64, at time.Time) resilience.Attempt {
	t.Helper()
	attempt, err := resilience.NewAttempt(ordinal, origin, parent, at)
	if err != nil {
		t.Fatalf("new attempt: %v", err)
	}
	return attempt
}

func admitOriginal(ctx context.Context, t testing.TB, scope resilience.WorkBudgetScope, at time.Time) {
	t.Helper()
	permit, err := scope.Acquire(ctx, attemptFor(t, 1, resilience.OriginOriginal, 0, at))
	if err != nil {
		t.Fatalf("acquire original: %v", err)
	}
	if err := permit.Complete(); err != nil {
		t.Fatalf("complete original: %v", err)
	}
}

func TestBudgetSharesRetryAndHedgeAmplificationLimits(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(100, 0)}
	budget, err := resilience.NewBudget(validBudgetConfig(clock))
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	scope, ctx, err := budget.Start(context.Background(), metadataFor(t, "logical-1", "postal:FI"))
	if err != nil {
		t.Fatalf("start budget: %v", err)
	}

	original, err := scope.Acquire(ctx, attemptFor(t, 1, resilience.OriginOriginal, 0, clock.Now()))
	if err != nil {
		t.Fatalf("acquire original: %v", err)
	}
	if err := original.Complete(); err != nil {
		t.Fatalf("complete original: %v", err)
	}
	retry, err := scope.Acquire(ctx, attemptFor(t, 2, resilience.OriginRetry, 1, clock.Now()))
	if err != nil {
		t.Fatalf("acquire retry: %v", err)
	}
	if got := scope.Snapshot().AdditionalActive; got != 1 {
		t.Fatalf("active retry count = %d, want 1", got)
	}
	if _, err := scope.Acquire(ctx, attemptFor(t, 3, resilience.OriginHedge, 1, clock.Now())); !errors.Is(err, resilience.ErrBudgetRejected) || resilience.RejectionReasonOf(err) != resilience.ReasonConcurrentLimit {
		t.Fatalf("concurrent hedge error = %v", err)
	}
	if err := retry.Complete(); err != nil {
		t.Fatalf("complete retry: %v", err)
	}
	hedge, err := scope.Acquire(ctx, attemptFor(t, 3, resilience.OriginHedge, 1, clock.Now()))
	if err != nil {
		t.Fatalf("acquire hedge: %v", err)
	}
	if err := hedge.Complete(); err != nil {
		t.Fatalf("complete hedge: %v", err)
	}
	if _, err := scope.Acquire(ctx, attemptFor(t, 4, resilience.OriginRetry, 3, clock.Now())); !errors.Is(err, resilience.ErrBudgetRejected) || resilience.RejectionReasonOf(err) != resilience.ReasonExecutionLimit {
		t.Fatalf("execution limit error = %v", err)
	}

	snapshot := scope.Snapshot()
	if snapshot.AdditionalAdmitted != 2 || snapshot.AdditionalActive != 0 || snapshot.AdditionalRecent != 2 || snapshot.Closed {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close scope: %v", err)
	}
}

func TestAdmitAttemptCoordinatesRetryAndHedgeLineageThroughContext(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(150, 0)}
	budget, err := resilience.NewBudget(validBudgetConfig(clock))
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	metadata := metadataFor(t, "logical", "resource")
	scope, ctx, err := budget.Start(context.Background(), metadata)
	if err != nil {
		t.Fatalf("start budget: %v", err)
	}

	originalCtx, original, originalPermit, err := resilience.AdmitAttempt(ctx, resilience.OriginOriginal, 0, clock.Now())
	if err != nil {
		t.Fatalf("admit original: %v", err)
	}
	if current, ok := resilience.AttemptFromContext(originalCtx); !ok || current != original || original.Ordinal != 1 {
		t.Fatalf("current original = (%+v, %v), admitted = %+v", current, ok, original)
	}
	if err := originalPermit.Complete(); err != nil {
		t.Fatalf("complete original: %v", err)
	}

	retryCtx, retryAttempt, retryPermit, err := resilience.AdmitAttempt(ctx, resilience.OriginRetry, original.Ordinal, clock.Now())
	if err != nil {
		t.Fatalf("admit retry: %v", err)
	}
	if current, ok := resilience.AttemptFromContext(retryCtx); !ok || current != retryAttempt || retryAttempt.Ordinal != 2 || retryAttempt.ParentOrdinal != original.Ordinal {
		t.Fatalf("current retry = (%+v, %v), admitted = %+v", current, ok, retryAttempt)
	}
	if err := retryPermit.Complete(); err != nil {
		t.Fatalf("complete retry: %v", err)
	}

	hedgeCtx, hedgeAttempt, hedgePermit, err := resilience.AdmitAttempt(ctx, resilience.OriginHedge, original.Ordinal, clock.Now())
	if err != nil {
		t.Fatalf("admit hedge: %v", err)
	}
	if current, ok := resilience.AttemptFromContext(hedgeCtx); !ok || current != hedgeAttempt || hedgeAttempt.Ordinal != 3 || hedgeAttempt.ParentOrdinal != original.Ordinal {
		t.Fatalf("current hedge = (%+v, %v), admitted = %+v", current, ok, hedgeAttempt)
	}
	if err := hedgePermit.Complete(); err != nil {
		t.Fatalf("complete hedge: %v", err)
	}
	if snapshot := scope.Snapshot(); snapshot.AdditionalAdmitted != 2 || snapshot.AdditionalActive != 0 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestAdmitAttemptRequiresAnAttachedBudgetScope(t *testing.T) {
	t.Parallel()

	_, _, _, err := resilience.AdmitAttempt(context.Background(), resilience.OriginOriginal, 0, time.Unix(1, 0))
	if !errors.Is(err, resilience.ErrBudgetScopeRequired) {
		t.Fatalf("admit attempt error = %v", err)
	}
	if _, ok := resilience.AttemptFromContext(context.Background()); ok {
		t.Fatal("background context unexpectedly has an attempt")
	}
}

func TestBudgetRejectsNestedScopeCreation(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(150, 0)}
	budget, err := resilience.NewBudget(validBudgetConfig(clock))
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	metadata := metadataFor(t, "logical", "resource")
	scope, ctx, err := budget.Start(context.Background(), metadata)
	if err != nil {
		t.Fatalf("start scope: %v", err)
	}
	if nested, nestedContext, nestedErr := budget.Start(ctx, metadata); nested != nil || nestedContext != nil || !errors.Is(nestedErr, resilience.ErrBudgetAlreadyAttached) {
		t.Fatalf("nested start = (%v, %v, %v)", nested, nestedContext, nestedErr)
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close scope: %v", err)
	}
}

func TestCustomBudgetCanAttachItsScopeToContext(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(175, 0)}
	budget, err := resilience.NewBudget(validBudgetConfig(clock))
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	scope, _, err := budget.Start(context.Background(), metadataFor(t, "logical", "resource"))
	if err != nil {
		t.Fatalf("start scope: %v", err)
	}
	ctx, err := resilience.WithBudgetScope(context.Background(), scope)
	if err != nil {
		t.Fatalf("attach scope: %v", err)
	}
	attached, ok := resilience.BudgetScopeFromContext(ctx)
	if !ok || attached != scope {
		t.Fatalf("attached scope = (%v, %v)", attached, ok)
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close scope: %v", err)
	}
}

func TestBudgetRollingWindowAndResourceCardinalityAreBounded(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(200, 0)}
	config := validBudgetConfig(clock)
	config.MaxConcurrentAdditional = 2
	config.MaxAdditionalPerExecution = 3
	config.MaxAdditionalPerWindow = 1
	config.MaxResources = 1
	budget, err := resilience.NewBudget(config)
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	scope, ctx, err := budget.Start(context.Background(), metadataFor(t, "logical-a", "resource-a"))
	if err != nil {
		t.Fatalf("start scope: %v", err)
	}
	admitOriginal(ctx, t, scope, clock.Now())
	permit, err := scope.Acquire(ctx, attemptFor(t, 2, resilience.OriginRetry, 1, clock.Now()))
	if err != nil {
		t.Fatalf("acquire retry: %v", err)
	}
	if err := permit.Complete(); err != nil {
		t.Fatalf("complete retry: %v", err)
	}
	if _, err := scope.Acquire(ctx, attemptFor(t, 3, resilience.OriginHedge, 1, clock.Now())); resilience.RejectionReasonOf(err) != resilience.ReasonWindowLimit {
		t.Fatalf("window rejection = %v", err)
	}
	if _, _, err := budget.Start(context.Background(), metadataFor(t, "logical-b", "resource-b")); resilience.RejectionReasonOf(err) != resilience.ReasonResourceLimit {
		t.Fatalf("resource rejection = %v", err)
	}

	if err := scope.Close(); err != nil {
		t.Fatalf("close scope: %v", err)
	}
	clock.Advance(time.Minute + time.Nanosecond)
	scopeB, _, err := budget.Start(context.Background(), metadataFor(t, "logical-b", "resource-b"))
	if err != nil {
		t.Fatalf("expired resource was not reclaimed: %v", err)
	}
	if err := scopeB.Close(); err != nil {
		t.Fatalf("close second scope: %v", err)
	}
}

func TestBudgetPermitCompletionAndExpiryAreExactOnce(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(300, 0)}
	config := validBudgetConfig(clock)
	config.PermitTTL = time.Second
	config.MaxAdditionalPerExecution = 3
	config.MaxAdditionalPerWindow = 3
	budget, err := resilience.NewBudget(config)
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	scope, ctx, err := budget.Start(context.Background(), metadataFor(t, "logical", "resource"))
	if err != nil {
		t.Fatalf("start scope: %v", err)
	}
	admitOriginal(ctx, t, scope, clock.Now())
	permit, err := scope.Acquire(ctx, attemptFor(t, 2, resilience.OriginRetry, 1, clock.Now()))
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := permit.Complete(); err != nil {
		t.Fatalf("first completion: %v", err)
	}
	if err := permit.Complete(); !errors.Is(err, resilience.ErrPermitCompleted) {
		t.Fatalf("second completion error = %v", err)
	}

	abandoned, err := scope.Acquire(ctx, attemptFor(t, 3, resilience.OriginHedge, 2, clock.Now()))
	if err != nil {
		t.Fatalf("acquire abandoned permit: %v", err)
	}
	clock.Advance(time.Second)
	if got := scope.Snapshot().AdditionalActive; got != 0 {
		t.Fatalf("active after expiry = %d", got)
	}
	if err := abandoned.Complete(); !errors.Is(err, resilience.ErrPermitExpired) {
		t.Fatalf("expired completion error = %v", err)
	}
	replacement, err := scope.Acquire(ctx, attemptFor(t, 4, resilience.OriginRetry, 3, clock.Now()))
	if err != nil {
		t.Fatalf("expired capacity was not reclaimed: %v", err)
	}
	if err := replacement.Complete(); err != nil {
		t.Fatalf("complete replacement: %v", err)
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close scope: %v", err)
	}
}

func TestBudgetRequiresOwnedAttemptLineageAndAttachedScope(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(350, 0)}
	budget, err := resilience.NewBudget(validBudgetConfig(clock))
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	scope, ctx, err := budget.Start(context.Background(), metadataFor(t, "logical", "resource"))
	if err != nil {
		t.Fatalf("start scope: %v", err)
	}
	if _, err := scope.Acquire(ctx, attemptFor(t, 2, resilience.OriginRetry, 1, clock.Now())); resilience.RejectionReasonOf(err) != resilience.ReasonOriginalRequired {
		t.Fatalf("missing original error = %v", err)
	}
	admitOriginal(ctx, t, scope, clock.Now())
	if _, err := scope.Acquire(context.Background(), attemptFor(t, 2, resilience.OriginRetry, 1, clock.Now())); !errors.Is(err, resilience.ErrBudgetScopeMismatch) {
		t.Fatalf("detached scope error = %v", err)
	}
	foreign, foreignCtx, err := budget.Start(context.Background(), metadataFor(t, "foreign", "resource"))
	if err != nil {
		t.Fatalf("start foreign scope: %v", err)
	}
	if _, err := scope.Acquire(foreignCtx, attemptFor(t, 2, resilience.OriginRetry, 1, clock.Now())); !errors.Is(err, resilience.ErrBudgetScopeMismatch) {
		t.Fatalf("foreign scope error = %v", err)
	}
	if err := foreign.Close(); err != nil {
		t.Fatalf("close foreign scope: %v", err)
	}
	if _, err := scope.Acquire(ctx, attemptFor(t, 3, resilience.OriginRetry, 2, clock.Now())); resilience.RejectionReasonOf(err) != resilience.ReasonUnknownParent {
		t.Fatalf("unknown parent error = %v", err)
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close scope: %v", err)
	}
}

func TestExecutorCentrallyAccountsBudgetedPhysicalAttempts(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(400, 0)}
	budget, err := resilience.NewBudget(validBudgetConfig(clock))
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	metadata := metadataFor(t, "logical", "resource")
	scope, ctx, err := budget.Start(context.Background(), metadata)
	if err != nil {
		t.Fatalf("start scope: %v", err)
	}
	executor, err := resilience.NewExecutor[string]()
	if err != nil {
		t.Fatalf("new executor: %v", err)
	}
	executor, err = executor.WithClock(clock)
	if err != nil {
		t.Fatalf("with clock: %v", err)
	}

	calls := 0
	result := executor.Execute(ctx, metadata, func(context.Context, resilience.Attempt) (string, error) {
		calls++
		return "ok", nil
	})
	if result.Err != nil || calls != 1 {
		t.Fatalf("first result = %+v, calls = %d", result, calls)
	}
	result = executor.Execute(ctx, metadata, func(context.Context, resilience.Attempt) (string, error) {
		calls++
		return "unexpected", nil
	})
	if result.Outcome.Kind != resilience.OutcomeLocalRejection || !errors.Is(result.Err, resilience.ErrBudgetRejected) || calls != 1 {
		t.Fatalf("duplicate original result = %+v, calls = %d", result, calls)
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close scope: %v", err)
	}
}

func TestBudgetRejectsCanceledAndClosedScopes(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(500, 0)}
	budget, err := resilience.NewBudget(validBudgetConfig(clock))
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	scope, budgetCtx, err := budget.Start(ctx, metadataFor(t, "logical", "resource"))
	if err != nil {
		t.Fatalf("start scope: %v", err)
	}
	cancel()
	if _, err := scope.Acquire(budgetCtx, attemptFor(t, 2, resilience.OriginRetry, 1, clock.Now())); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquire error = %v", err)
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close scope: %v", err)
	}
	if err := scope.Close(); !errors.Is(err, resilience.ErrBudgetClosed) {
		t.Fatalf("second close error = %v", err)
	}
	if _, err := scope.Acquire(context.Background(), attemptFor(t, 2, resilience.OriginRetry, 1, clock.Now())); !errors.Is(err, resilience.ErrBudgetClosed) {
		t.Fatalf("closed acquire error = %v", err)
	}
}

func TestClosingScopeDefersReleaseUntilActivePermitCompletes(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(800, 0)}
	config := validBudgetConfig(clock)
	config.MaxResources = 1
	budget, err := resilience.NewBudget(config)
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	scope, ctx, err := budget.Start(context.Background(), metadataFor(t, "logical-a", "resource-a"))
	if err != nil {
		t.Fatalf("start scope: %v", err)
	}
	permit, err := scope.Acquire(ctx, attemptFor(t, 1, resilience.OriginOriginal, 0, clock.Now()))
	if err != nil {
		t.Fatalf("acquire original: %v", err)
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close active scope: %v", err)
	}
	if _, _, err := budget.Start(context.Background(), metadataFor(t, "logical-b", "resource-b")); resilience.RejectionReasonOf(err) != resilience.ReasonResourceLimit {
		t.Fatalf("resource admitted before active permit completed: %v", err)
	}
	if err := permit.Complete(); err != nil {
		t.Fatalf("complete original: %v", err)
	}
	scopeB, _, err := budget.Start(context.Background(), metadataFor(t, "logical-b", "resource-b"))
	if err != nil {
		t.Fatalf("resource not released after completion: %v", err)
	}
	if err := scopeB.Close(); err != nil {
		t.Fatalf("close second scope: %v", err)
	}
}

func TestCompletedPermitDoesNotReleaseAnOpenScope(t *testing.T) {
	t.Parallel()

	clock := &manualClock{now: time.Unix(850, 0)}
	config := validBudgetConfig(clock)
	config.MaxResources = 1
	budget, err := resilience.NewBudget(config)
	if err != nil {
		t.Fatalf("new budget: %v", err)
	}
	scope, ctx, err := budget.Start(context.Background(), metadataFor(t, "logical-a", "resource-a"))
	if err != nil {
		t.Fatalf("start scope: %v", err)
	}
	admitOriginal(ctx, t, scope, clock.Now())
	if _, _, err := budget.Start(context.Background(), metadataFor(t, "logical-b", "resource-b")); resilience.RejectionReasonOf(err) != resilience.ReasonResourceLimit {
		t.Fatalf("open scope released resource identity: %v", err)
	}
	if err := scope.Close(); err != nil {
		t.Fatalf("close scope: %v", err)
	}
	scopeB, _, err := budget.Start(context.Background(), metadataFor(t, "logical-b", "resource-b"))
	if err != nil {
		t.Fatalf("closed scope retained resource identity: %v", err)
	}
	if err := scopeB.Close(); err != nil {
		t.Fatalf("close second scope: %v", err)
	}
}
