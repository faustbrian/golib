package deadline

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestExactDeadlinePolicyBoundsAreAccepted(t *testing.T) {
	t.Parallel()

	for name, policy := range map[string]Policy{
		"minimum timeout": {Timeout: time.Nanosecond},
		"maximum timeout": {Timeout: maximumTimeout},
	} {
		if _, err := New(policy); err != nil {
			t.Fatalf("New(%s exact bound) error = %v", name, err)
		}
	}

	for name, policy := range map[string]TimeoutPolicy{
		"minimums": {
			Timeout: time.Nanosecond, MaxResponseBytes: 1,
			MaxConcurrent: 1, Status: http.StatusInternalServerError,
		},
		"maximums": {
			Timeout: maximumTimeout, MaxResponseBytes: 16 << 20,
			MaxConcurrent: 65_536, Status: 599,
		},
	} {
		if _, err := NewTimeout(policy); err != nil {
			t.Fatalf("NewTimeout(%s exact bounds) error = %v", name, err)
		}
	}
}

func TestZeroBufferedTimeoutIsRejectedIndependently(t *testing.T) {
	t.Parallel()

	_, err := NewTimeout(TimeoutPolicy{
		MaxResponseBytes: 1, MaxConcurrent: 1, Status: http.StatusInternalServerError,
	})
	if !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("NewTimeout(zero timeout) error = %v", err)
	}
}

func TestTimeoutWriterClosedAndCommittedStatesIndependentlyBlockHeaders(t *testing.T) {
	t.Parallel()

	closed := newTimeoutWriter(1)
	closed.timeout()
	closed.WriteHeader(http.StatusCreated)
	if closed.status != 0 {
		t.Fatalf("closed writer status = %d", closed.status)
	}

	committed := newTimeoutWriter(1)
	committed.WriteHeader(http.StatusCreated)
	committed.WriteHeader(http.StatusAccepted)
	if committed.status != http.StatusCreated {
		t.Fatalf("committed writer status = %d", committed.status)
	}
}

func TestTimeoutWriterExactInformationalStatusBounds(t *testing.T) {
	t.Parallel()

	var informational []int
	writer := newTimeoutWriter(1, func(status int, _ http.Header) {
		informational = append(informational, status)
	})
	writer.WriteHeader(http.StatusContinue)
	if len(informational) != 1 || informational[0] != http.StatusContinue || writer.status != 0 {
		t.Fatalf("100 response = informational %v, final %d", informational, writer.status)
	}
	writer.WriteHeader(http.StatusOK)
	if writer.status != http.StatusOK {
		t.Fatalf("200 response status = %d", writer.status)
	}
}
