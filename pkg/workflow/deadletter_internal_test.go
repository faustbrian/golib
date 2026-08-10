package workflow

import (
	"testing"
	"time"
)

func TestDeadLetterResolutionValidityRequiresFieldsAndExactFingerprint(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 10, 21, 0, 0, 0, time.UTC)
	resolution, err := NewDeadLetterResolution(DeadLetterResolutionSpec{
		CommandID: "command-1", WorkID: "work-1", Token: 1,
		Action: DeadLetterDiscard, Actor: "operator-1", Reason: "discard-work",
		OccurredAt: now,
	})
	if err != nil {
		t.Fatalf("construct resolution: %v", err)
	}
	invalidFields := resolution
	invalidFields.token = 0
	invalidFields.fingerprint = deadLetterResolutionFingerprint(invalidFields)
	if invalidFields.Valid() {
		t.Fatal("resolution with invalid fields reported valid")
	}
	invalidFingerprint := resolution
	invalidFingerprint.fingerprint = "different"
	if invalidFingerprint.Valid() {
		t.Fatal("resolution with a mismatched fingerprint reported valid")
	}
}
