package workflow_test

import (
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func FuzzPendingWorkValidation(f *testing.F) {
	f.Add("work-1", uint8(workflow.WorkActivity), "instance-1", uint64(1), int64(1), []byte("input"))
	f.Add("", uint8(0), "", uint64(0), int64(0), []byte(nil))

	f.Fuzz(func(t *testing.T, id string, kind uint8, instanceID string, sequence uint64, deadlineOffset int64, payload []byte) {
		now := time.Unix(1, 0)
		work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
			ID: id, Kind: workflow.WorkKind(kind), InstanceID: instanceID, Sequence: sequence,
			AvailableAt: now, Deadline: now.Add(time.Duration(deadlineOffset)), Payload: payload,
		})
		if err != nil {
			return
		}
		if work.ID() != id || work.InstanceID() != instanceID || work.Sequence() == 0 ||
			!work.Deadline().After(work.AvailableAt()) || len(work.Payload()) > workflow.MaxPayloadBytes {
			t.Fatal("accepted pending work violated validated bounds")
		}
	})
}

func FuzzTransitionValidation(f *testing.F) {
	f.Add("transition-1", "instance-1", uint64(1), uint64(2), uint64(2))
	f.Add("", "", uint64(0), uint64(0), uint64(0))
	definition := mustDefinitionForFuzz(f)
	now := time.Unix(1, 0)

	f.Fuzz(func(t *testing.T, transitionID, instanceID string, expected, eventSequence, workSequence uint64) {
		event, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
			Sequence: eventSequence, InstanceID: instanceID, Kind: workflow.EventInstancePaused, OccurredAt: now,
		})
		if err != nil {
			return
		}
		work, err := workflow.NewPendingWork(workflow.PendingWorkSpec{
			ID: "work-1", Kind: workflow.WorkActivity, InstanceID: instanceID, Sequence: workSequence,
			AvailableAt: now, Deadline: now.Add(time.Second),
		})
		if err != nil {
			return
		}
		transition, err := workflow.NewTransition(workflow.TransitionSpec{
			ID: transitionID, InstanceID: instanceID, ExpectedSequence: expected,
			Definition: definition.Reference(), Events: []workflow.HistoryEvent{event},
			Work: []workflow.PendingWork{work},
		})
		if err != nil {
			return
		}
		if transition.ExpectedSequence()+1 != transition.Events()[0].Sequence() ||
			transition.Work()[0].Sequence() != transition.Events()[0].Sequence() {
			t.Fatal("accepted transition violated atomic sequence bounds")
		}
	})
}

func mustDefinitionForFuzz(f *testing.F) workflow.Definition {
	f.Helper()
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "orders", Version: "1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "execute", Kind: workflow.StepActivity, Target: "orders.execute",
			Timeout: time.Second, InputLimit: 1, ResultLimit: 1,
			Retry: workflow.RetryPolicy{MaxAttempts: 1, InitialDelay: time.Second, MaxDelay: time.Second},
		}},
	})
	if err != nil {
		f.Fatalf("construct definition: %v", err)
	}
	return definition
}
