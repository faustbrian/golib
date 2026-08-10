package workflow_test

import (
	"fmt"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func Example_durableOrchestration() {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	definition, _ := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "orders", Version: "1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "reserve", Kind: workflow.StepActivity, Target: "inventory.reserve",
			Timeout: time.Minute, InputLimit: 1024, ResultLimit: 1024,
			Retry: workflow.RetryPolicy{
				MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: time.Minute,
			},
		}},
	})
	registry, _ := workflow.CompileDefinitions(definition)
	started, _ := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "order-42", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(),
	})
	instance, _ := workflow.Replay(registry, []workflow.HistoryEvent{started})
	decision, _ := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
		TransitionID: "schedule-reserve-42", WorkID: "reserve-work-42",
		Instance: instance, Definition: definition, DecidedAt: now.Add(time.Second),
		Deadline: now.Add(time.Hour), IdempotencyKey: "reserve-attempt-42",
		Input: []byte("order-42"), TenantID: "tenant-1", CorrelationID: "request-1",
	})

	fmt.Println(decision.Kind(), decision.StepName())
	fmt.Println(decision.Transition().Events()[0].Kind())
	fmt.Println(decision.Transition().Work()[0].ID())
	// Output:
	// 1 reserve
	// 11
	// reserve-work-42
}
