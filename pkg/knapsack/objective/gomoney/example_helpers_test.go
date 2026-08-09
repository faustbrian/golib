package gomoney_test

import (
	"fmt"

	"github.com/faustbrian/golib/pkg/knapsack"
)

func mustPlanForExample(typeIDs ...string) knapsack.Plan {
	containers := make([]knapsack.ContainerInstance, len(typeIDs))
	for index, typeID := range typeIDs {
		containers[index] = knapsack.ContainerInstance{
			ID: fmt.Sprintf("container-%d", index), TypeID: typeID,
		}
	}
	plan, err := knapsack.NewPlan(knapsack.PlanSpec{
		Containers: containers, Status: knapsack.StatusFeasible,
		Termination: knapsack.TerminationCompleted,
	})
	if err != nil {
		panic(err)
	}
	return plan
}
