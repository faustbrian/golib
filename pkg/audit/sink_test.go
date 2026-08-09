package audit_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/audit"
)

func TestAppendErrorsDistinguishUnknownOutcomeFromConfirmedRejection(t *testing.T) {
	t.Parallel()

	cause := errors.New("connection lost")
	unknown := audit.NewAppendError(audit.AppendUnknown, cause)
	rejected := audit.NewAppendError(audit.AppendRejected, cause)

	if !errors.Is(unknown, cause) || audit.AppendOutcomeOf(unknown) != audit.AppendUnknown {
		t.Fatalf("unknown append error lost its cause or outcome: %v", unknown)
	}
	if !errors.Is(rejected, cause) || audit.AppendOutcomeOf(rejected) != audit.AppendRejected {
		t.Fatalf("rejected append error lost its cause or outcome: %v", rejected)
	}
	if audit.AppendOutcomeOf(cause) != audit.AppendUnknown {
		t.Fatal("unclassified append error was not treated conservatively")
	}
	if unknown.Error() == cause.Error() || rejected.Error() == cause.Error() {
		t.Fatal("append error exposed an unsafe infrastructure diagnostic")
	}
}

func TestAppendErrorNormalizesInvalidClassifications(t *testing.T) {
	t.Parallel()

	missingCause := audit.NewAppendError(audit.AppendRejected, nil)
	if !errors.Is(missingCause, audit.ErrInvalidArgument) || audit.AppendOutcomeOf(missingCause) != audit.AppendRejected {
		t.Fatalf("missing-cause append error = %v", missingCause)
	}
	invalid := audit.NewAppendError(audit.AppendOutcome(99), errors.New("cause"))
	if audit.AppendOutcomeOf(invalid) != audit.AppendUnknown || invalid.Error() != "audit append outcome is unknown" {
		t.Fatalf("invalid-outcome append error = %v", invalid)
	}
	committed := audit.NewAppendError(audit.AppendCommitted, errors.New("ack failed"))
	if audit.AppendOutcomeOf(committed) != audit.AppendCommitted || committed.Error() != "audit append committed before a later failure" {
		t.Fatalf("committed append error = %v", committed)
	}
}
