package conformance

import (
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/scheduler/lease"
)

func TestConformanceValidators(t *testing.T) {
	t.Parallel()

	if err := validateAcquisitionResults(1, 31, 32); err != nil {
		t.Fatalf("validateAcquisitionResults(valid) error = %v", err)
	}
	for _, input := range []struct {
		winners int
		held    int
	}{
		{winners: 0, held: 31},
		{winners: 1, held: 30},
	} {
		if err := validateAcquisitionResults(
			input.winners,
			input.held,
			32,
		); err == nil {
			t.Fatalf("validateAcquisitionResults(%+v) error = nil", input)
		}
	}

	if err := validateFencingAdvance(2, 1); err != nil {
		t.Fatalf("validateFencingAdvance(valid) error = %v", err)
	}
	for _, current := range []uint64{0, 1} {
		if err := validateFencingAdvance(current, 1); err == nil {
			t.Fatalf("validateFencingAdvance(%d, 1) error = nil", current)
		}
	}

	if err := validateHeartbeatExtension(minimumHeartbeatExtension); err != nil {
		t.Fatalf("validateHeartbeatExtension(limit) error = %v", err)
	}
	if err := validateHeartbeatExtension(
		minimumHeartbeatExtension - time.Nanosecond,
	); err == nil {
		t.Fatal("validateHeartbeatExtension(below limit) error = nil")
	}

	expected := lease.Lease{Owner: "owner", FencingToken: 7}
	if err := validateSameLease(expected, expected); err != nil {
		t.Fatalf("validateSameLease(valid) error = %v", err)
	}
	for _, current := range []lease.Lease{
		{Owner: "other", FencingToken: expected.FencingToken},
		{Owner: expected.Owner, FencingToken: expected.FencingToken + 1},
	} {
		if err := validateSameLease(current, expected); err == nil {
			t.Fatalf("validateSameLease(%+v) error = nil", current)
		}
	}
}

func TestDifferentFencingTokenStaysBackendSafe(t *testing.T) {
	t.Parallel()

	for _, token := range []uint64{1, 2, 1<<63 - 1} {
		different := differentFencingToken(token)
		if different == token || different > 1<<63-1 {
			t.Fatalf("differentFencingToken(%d) = %d", token, different)
		}
	}
}
