package workflow_test

import (
	"testing"
	"time"

	workflow "github.com/faustbrian/golib/pkg/workflow"
)

func FuzzWorkDecisionBoundaries(fuzz *testing.F) {
	fuzz.Add(uint8(workflow.WorkComplete), "", int64(0))
	fuzz.Add(uint8(workflow.WorkRetryDecision), "temporary", int64(time.Minute))
	fuzz.Fuzz(func(t *testing.T, kind uint8, code string, retryNanos int64) {
		decision, err := workflow.NewWorkDecision(workflow.WorkDecisionSpec{
			Kind: workflow.WorkDecisionKind(kind), Code: code,
			RetryAt: time.Time{}.Add(time.Duration(retryNanos)),
		})
		if err == nil && !decision.Valid() {
			t.Fatal("accepted work decision is invalid")
		}
	})
}
