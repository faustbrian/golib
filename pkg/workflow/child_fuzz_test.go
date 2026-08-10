package workflow_test

import (
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func FuzzChildDispatchBoundaries(fuzz *testing.F) {
	fuzz.Add([]byte(`{"step_name":"child","child_id":"child-1","definition_name":"child","definition_version":"1","definition_fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","attempt":1,"idempotency_key":"child-1"}`))
	fuzz.Add([]byte("bad"))
	fuzz.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > workflow.MaxChildDispatchBytes+1 {
			t.Skip()
		}
		dispatch, err := workflow.DecodeChildDispatch(payload)
		if err == nil && (dispatch.StepName() == "" || dispatch.ChildID() == "" ||
			dispatch.Definition().Name() == "" || dispatch.Attempt() == 0 ||
			dispatch.IdempotencyKey() == "") {
			t.Fatal("accepted incomplete child dispatch")
		}
	})
}

func FuzzChildStartRequestBoundaries(fuzz *testing.F) {
	now := time.Date(2036, 8, 11, 20, 0, 0, 0, time.UTC)
	parent := mustDefinitionForFuzz(fuzz)
	child := parent
	fuzz.Add("parent-1", "child", "child-1", uint32(1), uint32(1), "child-1", uint32(8), []byte("input"))
	fuzz.Add("", "", "", uint32(0), uint32(0), "", uint32(0), []byte(nil))
	fuzz.Fuzz(func(
		t *testing.T,
		parentID, stepName, childID string,
		attempt, maxAttempts uint32,
		idempotencyKey string,
		inputLimit uint32,
		input []byte,
	) {
		if len(input) > workflow.MaxPayloadBytes+1 {
			t.Skip()
		}
		request, err := workflow.NewChildStartRequest(workflow.ChildStartRequestSpec{
			ParentInstanceID: parentID, ParentDefinition: parent.Reference(),
			StepName: stepName, ChildID: childID, ChildDefinition: child.Reference(),
			Attempt: attempt, MaxAttempts: maxAttempts, IdempotencyKey: idempotencyKey,
			StartedAt: now, Deadline: now.Add(time.Minute),
			Input: input, InputLimit: inputLimit,
		})
		if err == nil && (request.ParentInstanceID() == "" || request.ChildID() == "" ||
			request.Attempt() == 0 || request.Attempt() > request.MaxAttempts() ||
			len(request.Input()) > int(inputLimit)) {
			t.Fatal("accepted incoherent child start request")
		}
	})
}
