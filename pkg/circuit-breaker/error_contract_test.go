package breaker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

func TestEveryAdmissionRejectionSupportsErrorsIsAndAs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		cause error
		make  func(*testing.T) (*breaker.Breaker, error)
	}{
		{
			name:  "open",
			cause: breaker.ErrOpen,
			make: func(t *testing.T) (*breaker.Breaker, error) {
				clock := &fakeClock{now: time.Unix(100, 0)}
				b := openBreaker(t, clock, &breaker.HalfOpenPolicy{MaxProbes: 1, RequiredSuccesses: 1})
				_, err := b.Acquire(context.Background())
				return b, err
			},
		},
		{
			name:  "force open",
			cause: breaker.ErrForceOpen,
			make: func(t *testing.T) (*breaker.Breaker, error) {
				b := mustBreaker(t, breaker.Config{Name: "force-open"})
				_ = b.ForceOpen()
				_, err := b.Acquire(context.Background())
				return b, err
			},
		},
		{
			name:  "isolated",
			cause: breaker.ErrIsolated,
			make: func(t *testing.T) (*breaker.Breaker, error) {
				b := mustBreaker(t, breaker.Config{Name: "isolated"})
				_ = b.Isolate()
				_, err := b.Acquire(context.Background())
				return b, err
			},
		},
		{
			name:  "half-open exhausted",
			cause: breaker.ErrHalfOpenExhausted,
			make: func(t *testing.T) (*breaker.Breaker, error) {
				clock := &fakeClock{now: time.Unix(100, 0)}
				b := openBreaker(t, clock, &breaker.HalfOpenPolicy{MaxProbes: 1, RequiredSuccesses: 1})
				clock.Advance(time.Minute)
				_, _ = b.Acquire(context.Background())
				_, err := b.Acquire(context.Background())
				return b, err
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b, err := test.make(t)
			if !errors.Is(err, test.cause) {
				t.Fatalf("Acquire() error = %v, want %v", err, test.cause)
			}
			var rejection *breaker.RejectionError
			if !errors.As(err, &rejection) {
				t.Fatalf("Acquire() error type = %T, want *RejectionError", err)
			}
			if rejection.Name != b.Snapshot().Name || rejection.Generation == 0 {
				t.Fatalf("RejectionError = %+v", rejection)
			}
		})
	}
}
