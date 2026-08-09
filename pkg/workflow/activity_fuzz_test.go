package workflow_test

import (
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func FuzzActivityRequestValidation(f *testing.F) {
	f.Add("instance-1", "execute", uint32(1), uint32(3), "key-1", []byte("input"))
	f.Add("", "", uint32(0), uint32(0), "", []byte(nil))

	reference, err := workflow.NewDefinitionReference(
		"orders",
		"1",
		"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	)
	if err != nil {
		f.Fatalf("construct definition reference: %v", err)
	}
	now := time.Date(2026, 8, 9, 6, 0, 0, 0, time.UTC)

	f.Fuzz(func(t *testing.T, instanceID, stepName string, attempt, maxAttempts uint32, idempotencyKey string, input []byte) {
		request, err := workflow.NewActivityRequest(workflow.ActivityRequestSpec{
			InstanceID: instanceID, Definition: reference, StepName: stepName,
			Attempt: attempt, MaxAttempts: maxAttempts, IdempotencyKey: idempotencyKey,
			StartedAt: now, Deadline: now.Add(time.Second), Input: input,
			InputLimit: 1024, ResultLimit: 1024,
		})
		if err != nil {
			return
		}
		if request.InstanceID() != instanceID || request.StepName() != stepName ||
			request.Attempt() == 0 || request.Attempt() > request.MaxAttempts() ||
			len(request.Input()) > int(request.InputLimit()) {
			t.Fatal("accepted activity request violated validated bounds")
		}
	})
}

func FuzzActivityOutcomeValidation(f *testing.F) {
	f.Add(uint8(workflow.ActivitySucceeded), "", false, []byte("result"))
	f.Add(uint8(workflow.ActivityFailed), "temporary", true, []byte("details"))
	f.Add(uint8(workflow.ActivityUnknown), "connection-lost", false, []byte("details"))

	f.Fuzz(func(t *testing.T, kind uint8, code string, retryable bool, data []byte) {
		outcome, err := workflow.NewActivityOutcome(workflow.ActivityOutcomeSpec{
			Kind: workflow.ActivityOutcomeKind(kind), Code: code,
			Retryable: retryable, Data: data,
		})
		if err != nil {
			return
		}
		if outcome.Kind() == workflow.ActivityUnknown && outcome.Retryable() {
			t.Fatal("accepted unknown outcome as automatically retryable")
		}
		if len(outcome.Data()) > workflow.MaxPayloadBytes {
			t.Fatal("accepted oversized activity outcome")
		}
	})
}
