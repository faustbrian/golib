package hedge_test

import (
	"sync"
	"testing"

	"github.com/faustbrian/golib/pkg/hedge"
)

func TestOutstandingBudgetConcurrentStress(t *testing.T) {
	t.Parallel()

	budget, err := hedge.NewOutstandingBudget(8)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for worker := 0; worker < 32; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for iteration := 0; iteration < 500; iteration++ {
				if permit, admitted := budget.TryAcquire("shared"); admitted {
					if budget.Outstanding() > 8 {
						t.Errorf("budget exceeded: %d", budget.Outstanding())
					}
					permit.Release()
				}
			}
		}()
	}
	wait.Wait()
	if budget.Outstanding() != 0 {
		t.Fatalf("outstanding = %d", budget.Outstanding())
	}
}
