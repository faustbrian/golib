package resilience_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/resilience"
)

func ExampleExecutor() {
	metadata, _ := resilience.NewMetadata("request-1", "postal.lookup", "postal:FI")
	executor, _ := resilience.NewExecutor[string]()
	result := executor.Execute(context.Background(), metadata,
		func(_ context.Context, attempt resilience.Attempt) (string, error) {
			return fmt.Sprintf("attempt-%d", attempt.Ordinal), nil
		},
	)
	fmt.Println(result.Value, result.Outcome.Kind)
	// Output: attempt-1 success
}

func ExampleBudget() {
	clock := fixedExampleClock{}
	budget, _ := resilience.NewBudget(resilience.BudgetConfig{
		MaxResources: 16, MaxAdditionalPerExecution: 2,
		MaxConcurrentAdditional: 1, MaxAdditionalPerWindow: 8,
		AdditionalWindow: exampleDuration, PermitTTL: exampleDuration, Clock: clock,
	})
	metadata, _ := resilience.NewMetadata("request-1", "postal.lookup", "postal:FI")
	scope, ctx, _ := budget.Start(context.Background(), metadata)
	attempt, _ := resilience.NewAttempt(1, resilience.OriginOriginal, 0, clock.Now())
	permit, _ := scope.Acquire(ctx, attempt)
	_ = permit.Complete()
	fmt.Println(scope.Snapshot().AdditionalAdmitted)
	_ = scope.Close()
	// Output: 0
}
