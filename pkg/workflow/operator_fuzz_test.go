package workflow_test

import (
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func FuzzOperatorAuditBoundaries(fuzz *testing.F) {
	fuzz.Add("operator-command-1", []byte(`{"action":1,"actor":"operator-1","reason":"maintenance"}`))
	fuzz.Add("operator-command-1", []byte("bad"))
	fuzz.Fuzz(func(t *testing.T, commandID string, data []byte) {
		if len(commandID) > 300 || len(data) > workflow.MaxOperatorAuditBytes+1 {
			t.Skip()
		}
		event, err := workflow.NewHistoryEvent(workflow.HistoryEventSpec{
			Sequence: 2, InstanceID: "instance-1", Kind: workflow.EventOperatorCommandRecorded,
			OccurredAt:     time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC),
			IdempotencyKey: commandID, Data: data,
		})
		if err == nil && (event.IdempotencyKey() == "" || len(event.Data()) == 0) {
			t.Fatal("accepted incomplete operator audit")
		}
	})
}
