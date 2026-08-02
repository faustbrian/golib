package hedge_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/hedge"
)

func TestOutstandingBudgetBoundsSharedConcurrentHedges(t *testing.T) {
	t.Parallel()

	budget, err := hedge.NewOutstandingBudget(2)
	if err != nil {
		t.Fatal(err)
	}
	first, ok := budget.TryAcquire("inventory")
	if !ok {
		t.Fatal("first acquisition denied")
	}
	second, ok := budget.TryAcquire("inventory")
	if !ok {
		t.Fatal("second acquisition denied")
	}
	if permit, admitted := budget.TryAcquire("inventory"); admitted || permit != nil {
		t.Fatal("third acquisition admitted above bound")
	}
	if got := budget.Outstanding(); got != 2 {
		t.Fatalf("Outstanding() = %d", got)
	}
	if budget.Capacity() != 2 {
		t.Fatalf("Capacity() = %d", budget.Capacity())
	}

	var wait sync.WaitGroup
	wait.Add(2)
	go func() { defer wait.Done(); first.Release() }()
	go func() { defer wait.Done(); first.Release() }()
	wait.Wait()
	if got := budget.Outstanding(); got != 1 {
		t.Fatalf("Outstanding() after idempotent release = %d", got)
	}
	second.Release()
	if got := budget.Outstanding(); got != 0 {
		t.Fatalf("Outstanding() after release = %d", got)
	}
}

func TestOutstandingBudgetRejectsZeroLimit(t *testing.T) {
	t.Parallel()

	if _, err := hedge.NewOutstandingBudget(0); !errors.Is(err, hedge.ErrInvalidBudget) {
		t.Fatalf("NewOutstandingBudget() error = %v", err)
	}
	if _, err := hedge.NewOutstandingBudget(hedge.MaxBudgetCapacity + 1); !errors.Is(err, hedge.ErrInvalidBudget) {
		t.Fatalf("NewOutstandingBudget(too large) error = %v", err)
	}
	var budget *hedge.OutstandingBudget
	if budget.Capacity() != 0 {
		t.Fatalf("nil Capacity() = %d", budget.Capacity())
	}
}
