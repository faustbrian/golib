package knapsacktest_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/knapsack"
	"github.com/faustbrian/golib/pkg/knapsack/knapsacktest"
)

func TestRequireCanonicalEqual(t *testing.T) {
	t.Parallel()
	plan, _ := knapsack.NewPlan(knapsack.PlanSpec{Status: knapsack.StatusFeasible, Termination: knapsack.TerminationCompleted})
	knapsacktest.RequireCanonicalEqual(t, plan, plan)
}
