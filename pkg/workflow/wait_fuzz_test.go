package workflow_test

import (
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func FuzzSignalAcceptanceBoundaries(fuzz *testing.F) {
	fuzz.Add(uint64(1), int64(time.Second), "signal-1", []byte("yes"))
	fuzz.Add(^uint64(0), int64(0), " spaces ", make([]byte, 17))
	definition, err := workflow.NewDefinition(workflow.DefinitionSpec{
		Name: "orders", Version: "waits-v1", Mode: workflow.Orchestration,
		Steps: []workflow.StepSpec{{
			Name: "approved", Kind: workflow.StepSignal, Target: "orders.approved",
			Timeout: time.Hour, InputLimit: 16,
		}},
	})
	if err != nil {
		fuzz.Fatalf("construct definition: %v", err)
	}
	fuzz.Fuzz(func(t *testing.T, sequence uint64, receivedNanos int64, signalID string, payload []byte) {
		transition, err := workflow.NewSignalAcceptance(workflow.SignalAcceptanceSpec{
			InstanceID:       "instance-1",
			ExpectedSequence: sequence, Definition: definition, StepName: "approved",
			SignalID: signalID, ReceivedAt: time.Time{}.Add(time.Duration(receivedNanos)),
			Payload: payload,
		})
		if err == nil && (!transition.Valid() || len(transition.Events()) != 1) {
			t.Fatal("accepted signal transition is invalid")
		}
	})
}
