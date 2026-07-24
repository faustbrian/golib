package cache

import (
	"errors"
	"testing"
	"time"
)

func TestElapsedClampsBackwardClock(t *testing.T) {
	t.Parallel()

	now := time.Now()
	if got := elapsed(now, now.Add(-time.Second)); got != 0 {
		t.Fatalf("backward clock produced %v", got)
	}
}

func TestResultOutcomeDefaultsAndErrors(t *testing.T) {
	t.Parallel()

	if got := resultOutcome(Result[string]{}, nil); got != OutcomeSuccess {
		t.Fatalf("zero result outcome is %q", got)
	}
	if got := resultOutcome(Result[string]{State: Hit}, errors.New("failed")); got != OutcomeError {
		t.Fatalf("error outcome is %q", got)
	}
}
