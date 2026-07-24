package authorizationtest

import (
	"errors"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
)

type recordingT struct {
	failures int
}

func (*recordingT) Helper() {}

func (test *recordingT) Fatalf(string, ...any) {
	test.failures++
}

func TestFailureReportingBranches(t *testing.T) {
	recorder := &recordingT{}

	if snapshot := MustSnapshot(recorder, 1, authorization.CombiningAlgorithm(255)); snapshot != nil {
		t.Fatalf("MustSnapshot() = %#v, want nil after failure", snapshot)
	}
	if engine := MustEngine(recorder, nil); engine != nil {
		t.Fatalf("MustEngine() = %#v, want nil after failure", engine)
	}
	RequireDecision(recorder, authorization.Decision{}, errors.New("failure"), authorization.Decision{})
	RequireDecision(recorder, authorization.Decision{Outcome: authorization.Allow}, nil, authorization.Decision{Outcome: authorization.Deny})
	RequireOutcome(recorder, authorization.Decision{Outcome: authorization.Deny}, authorization.Allow)
	requireConformanceDecision(recorder, authorization.Decision{}, nil, authorization.Allow, "expected", true)
	requireConformanceDecision(recorder, authorization.Decision{}, nil, authorization.Allow, "expected", false)

	if recorder.failures != 8 {
		t.Fatalf("Fatalf() calls = %d, want 8", recorder.failures)
	}
}
