package workflow_test

import (
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func FuzzHistoryEventValidation(f *testing.F) {
	f.Add(uint64(1), "instance-1", uint8(workflow.EventInstancePaused), int64(1), []byte("payload"))
	f.Add(uint64(0), "", uint8(0), int64(0), []byte(nil))
	f.Add(^uint64(0), "tenant/order:instance-1", uint8(workflow.EventContinuedAsNew), int64(-1), []byte("state"))

	reference, err := workflow.NewDefinitionReference(
		"orders",
		"1",
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	)
	if err != nil {
		f.Fatalf("construct seed definition reference: %v", err)
	}

	f.Fuzz(func(t *testing.T, sequence uint64, instanceID string, kind uint8, unixNano int64, data []byte) {
		spec := workflow.HistoryEventSpec{
			Sequence: sequence, InstanceID: instanceID, Kind: workflow.EventKind(kind),
			OccurredAt: time.Unix(0, unixNano), Data: data,
		}
		if spec.Kind == workflow.EventInstanceStarted || spec.Kind == workflow.EventDefinitionMigrated {
			spec.Definition = reference
		}
		if spec.Kind == workflow.EventContinuedAsNew {
			spec.Definition = reference
			spec.SuccessorID = "successor-1"
		}

		event, err := workflow.NewHistoryEvent(spec)
		if err != nil {
			return
		}
		if event.Sequence() == 0 || event.InstanceID() != instanceID || event.Kind() != spec.Kind {
			t.Fatal("accepted event did not preserve validated identity")
		}
		if event.OccurredAt().Location() != time.UTC || len(event.Data()) > workflow.MaxPayloadBytes {
			t.Fatal("accepted event violated canonical bounds")
		}
	})
}

func FuzzActivityHistoryEventValidation(f *testing.F) {
	f.Add(uint8(workflow.EventActivityScheduled), "execute", uint32(0), "", "", false, []byte("input"))
	f.Add(uint8(workflow.EventActivityAttemptStarted), "execute", uint32(1), "key-1", "", false, []byte(nil))
	f.Add(uint8(workflow.EventActivityAttemptUnknown), "execute", uint32(1), "", "transport-lost", false, []byte("details"))

	f.Fuzz(func(t *testing.T, kind uint8, stepName string, attempt uint32, idempotencyKey, code string, retryable bool, data []byte) {
		eventKind := workflow.EventKind(kind)
		now := time.Unix(1, 0)
		spec := workflow.HistoryEventSpec{
			Sequence: 2, InstanceID: "instance-1", Kind: eventKind, OccurredAt: now,
			StepName: stepName, Attempt: attempt, IdempotencyKey: idempotencyKey,
			Code: code, Retryable: retryable, Data: data,
		}
		if eventKind == workflow.EventActivityAttemptStarted || eventKind == workflow.EventActivityRetryScheduled {
			spec.DueAt = now.Add(time.Second)
		}
		event, err := workflow.NewHistoryEvent(spec)
		if err != nil {
			return
		}
		if event.StepName() != stepName || event.Attempt() != attempt ||
			event.IdempotencyKey() != idempotencyKey || event.Code() != code ||
			event.Retryable() != retryable || len(event.Data()) > workflow.MaxPayloadBytes {
			t.Fatal("accepted activity event did not preserve validated bounds")
		}
	})
}
