package workflow_test

import (
	"testing"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func FuzzCompensationDispatchBoundaries(fuzz *testing.F) {
	fuzz.Add([]byte(`{"step_name":"reserve","attempt":1,"idempotency_key":"attempt-1"}`))
	fuzz.Add([]byte("not-json"))
	fuzz.Fuzz(func(t *testing.T, payload []byte) {
		dispatch, err := workflow.DecodeCompensationDispatch(payload)
		if err == nil && (dispatch.StepName() == "" || dispatch.Attempt() == 0 || dispatch.IdempotencyKey() == "") {
			t.Fatal("accepted compensation dispatch is incomplete")
		}
	})
}
