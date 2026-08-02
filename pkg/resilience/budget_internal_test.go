package resilience

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestBudgetScopeFromContextRejectsTypedNilScope(t *testing.T) {
	t.Parallel()

	var scope *BudgetScope
	ctx := context.WithValue(context.Background(), budgetContextKey{}, &budgetExecutionState{scope: scope})
	if got, ok := BudgetScopeFromContext(ctx); ok {
		t.Fatalf("scope = %#v, ok = %t", got, ok)
	}
}

func TestAttemptContextRejectsInvalidStateAndExhaustedOrdinals(t *testing.T) {
	t.Parallel()

	var nilContext context.Context
	if attempt, ok := AttemptFromContext(nilContext); ok || attempt != (Attempt{}) {
		t.Fatalf("nil context attempt = (%+v, %v)", attempt, ok)
	}
	invalidCtx := context.WithValue(context.Background(), attemptContextKey{}, Attempt{Ordinal: 1})
	if attempt, ok := AttemptFromContext(invalidCtx); ok || attempt != (Attempt{}) {
		t.Fatalf("invalid context attempt = (%+v, %v)", attempt, ok)
	}
	if _, _, _, err := AdmitAttempt(nilContext, OriginOriginal, 0, time.Unix(1, 0)); !errors.Is(err, ErrInvalidMetadata) {
		t.Fatalf("nil admission error = %v", err)
	}

	state := &budgetExecutionState{scope: &BudgetScope{}}
	state.next.Store(math.MaxUint64)
	exhaustedCtx := context.WithValue(context.Background(), budgetContextKey{}, state)
	if _, _, _, err := AdmitAttempt(exhaustedCtx, OriginRetry, 1, time.Unix(1, 0)); !errors.Is(err, ErrInvalidAttempt) {
		t.Fatalf("exhausted admission error = %v", err)
	}

	state.next.Store(1)
	if _, _, _, err := AdmitAttempt(exhaustedCtx, OriginOriginal, 0, time.Time{}); !errors.Is(err, ErrInvalidAttempt) {
		t.Fatalf("invalid attempt error = %v", err)
	}
}

func TestCompletingLastPermitImmediatelyReleasesClosedScope(t *testing.T) {
	t.Parallel()

	clock := &internalClock{now: time.Unix(1, 0)}
	budget, err := NewBudget(BudgetConfig{
		MaxResources: 1, MaxAdditionalPerExecution: 1,
		MaxConcurrentAdditional: 1, MaxAdditionalPerWindow: 1,
		AdditionalWindow: time.Minute, PermitTTL: time.Minute, Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := NewMetadata("logical", "operation", "resource")
	if err != nil {
		t.Fatal(err)
	}
	publicScope, ctx, err := budget.Start(context.Background(), metadata)
	if err != nil {
		t.Fatal(err)
	}
	scope, ok := publicScope.(*BudgetScope)
	if !ok {
		t.Fatalf("scope type = %T, want *BudgetScope", publicScope)
	}
	attempt, err := NewAttempt(1, OriginOriginal, 0, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	permit, err := scope.Acquire(ctx, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
	if err := permit.Complete(); err != nil {
		t.Fatal(err)
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if len(budget.scopes) != 0 || scope.resource.scopes != 0 {
		t.Fatalf("retained scopes = %d, resource references = %d", len(budget.scopes), scope.resource.scopes)
	}
}

type internalClock struct{ now time.Time }

func (clock *internalClock) Now() time.Time { return clock.now }
