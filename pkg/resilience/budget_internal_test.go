package resilience

import (
	"context"
	"testing"
	"time"
)

func TestBudgetScopeFromContextRejectsTypedNilScope(t *testing.T) {
	t.Parallel()

	var scope *BudgetScope
	ctx := context.WithValue(context.Background(), budgetContextKey{}, WorkBudgetScope(scope))
	if got, ok := BudgetScopeFromContext(ctx); ok {
		t.Fatalf("scope = %#v, ok = %t", got, ok)
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
