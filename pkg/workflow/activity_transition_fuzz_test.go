package workflow_test

import (
	"testing"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func FuzzActivityDispatchBoundaries(fuzz *testing.F) {
	fuzz.Add([]byte(`{"step_name":"execute","attempt":1,"idempotency_key":"attempt-1"}`))
	fuzz.Add([]byte("bad"))
	fuzz.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > workflow.MaxActivityDispatchBytes+1 {
			t.Skip()
		}
		dispatch, err := workflow.DecodeActivityDispatch(payload)
		if err == nil && (dispatch.StepName() == "" || dispatch.Attempt() == 0 || dispatch.IdempotencyKey() == "") {
			t.Fatal("accepted incomplete activity dispatch")
		}
	})
}
