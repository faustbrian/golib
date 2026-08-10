package workflow_test

import (
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func BenchmarkReplayPersistedLifecycle(b *testing.B) {
	definition := benchmarkDefinition(b)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		b.Fatalf("compile definitions: %v", err)
	}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	events := []workflow.HistoryEvent{
		benchmarkHistoryEvent(b, workflow.HistoryEventSpec{
			Sequence: 1, InstanceID: "benchmark-instance", Kind: workflow.EventInstanceStarted,
			OccurredAt: now, Definition: definition.Reference(), Data: []byte("order-42"),
		}),
		benchmarkHistoryEvent(b, workflow.HistoryEventSpec{
			Sequence: 2, InstanceID: "benchmark-instance", Kind: workflow.EventInstancePaused,
			OccurredAt: now.Add(time.Second),
		}),
		benchmarkHistoryEvent(b, workflow.HistoryEventSpec{
			Sequence: 3, InstanceID: "benchmark-instance", Kind: workflow.EventInstanceResumed,
			OccurredAt: now.Add(2 * time.Second),
		}),
		benchmarkHistoryEvent(b, workflow.HistoryEventSpec{
			Sequence: 4, InstanceID: "benchmark-instance", Kind: workflow.EventInstanceCompleted,
			OccurredAt: now.Add(3 * time.Second), Data: []byte("complete"),
		}),
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		instance, replayErr := workflow.Replay(registry, events)
		if replayErr != nil || instance.Status() != workflow.StatusCompleted {
			b.Fatalf("replay lifecycle: status %d, error %v", instance.Status(), replayErr)
		}
	}
}

func BenchmarkPlanDurableActivityTransition(b *testing.B) {
	definition := benchmarkDefinition(b)
	registry, err := workflow.CompileDefinitions(definition)
	if err != nil {
		b.Fatalf("compile definitions: %v", err)
	}
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	started := benchmarkHistoryEvent(b, workflow.HistoryEventSpec{
		Sequence: 1, InstanceID: "benchmark-instance", Kind: workflow.EventInstanceStarted,
		OccurredAt: now, Definition: definition.Reference(), Data: []byte("order-42"),
	})
	instance, err := workflow.Replay(registry, []workflow.HistoryEvent{started})
	if err != nil {
		b.Fatalf("replay started instance: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		decision, decisionErr := workflow.NewOrchestrationDecision(workflow.OrchestrationDecisionSpec{
			TransitionID: "benchmark-transition", WorkID: "benchmark-work",
			Instance: instance, Definition: definition, DecidedAt: now.Add(time.Second),
			Deadline: now.Add(time.Minute), IdempotencyKey: "benchmark-attempt",
			Input: []byte("order-42"), TenantID: "benchmark-tenant",
			CorrelationID: "benchmark-correlation",
		})
		if decisionErr != nil || !decision.Transition().Valid() {
			b.Fatalf("plan activity transition: valid %t, error %v", decision.Transition().Valid(), decisionErr)
		}
	}
}

func benchmarkDefinition(tb testing.TB) workflow.Definition {
	tb.Helper()
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "benchmark.workflow", Version: "1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "execute", Kind: workflow.StepActivity, Target: "orders.execute",
			Timeout: time.Minute, InputLimit: 1024, ResultLimit: 1024,
			Retry: workflow.RetryPolicy{MaxAttempts: 3, InitialDelay: time.Second, MaxDelay: time.Minute},
		}},
	})
	if err != nil {
		tb.Fatalf("construct benchmark definition: %v", err)
	}
	return definition
}

func benchmarkHistoryEvent(tb testing.TB, spec workflow.HistoryEventSpec) workflow.HistoryEvent {
	tb.Helper()
	event, err := workflow.NewHistoryEvent(spec)
	if err != nil {
		tb.Fatalf("construct benchmark event: %v", err)
	}
	return event
}
