package relay

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestExponentialBackoffIsBounded(t *testing.T) {
	t.Parallel()

	for _, attempt := range []int{-1, 0, 1, 5, 100} {
		delay := exponentialBackoff(attempt)
		if delay < 0 || delay > maximumBackoff {
			t.Fatalf("attempt %d delay = %s", attempt, delay)
		}
	}
	if maximumBackoff != time.Minute {
		t.Fatalf("maximum backoff = %s", maximumBackoff)
	}
}

func TestWaitContextHandlesTimerAndCancellation(t *testing.T) {
	t.Parallel()

	if err := waitContext(context.Background(), time.Nanosecond); err != nil {
		t.Fatalf("timer wait: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait error = %v", err)
	}
}

func TestMaintainLeaseExtendsUntilCancellationOrFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("extension failed")
	if err := maintainLease(context.Background(), time.Nanosecond, func(context.Context) error {
		return want
	}); !errors.Is(err, want) {
		t.Fatalf("extension error = %v, want %v", err, want)
	}

	extensions := 0
	if err := maintainLease(context.Background(), time.Nanosecond, func(context.Context) error {
		extensions++
		if extensions == 1 {
			return nil
		}

		return want
	}); !errors.Is(err, want) {
		t.Fatalf("second extension error = %v, want %v", err, want)
	}
	if extensions != 2 {
		t.Fatalf("extensions = %d, want 2", extensions)
	}
}
