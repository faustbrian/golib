package breaker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
	"github.com/faustbrian/golib/pkg/circuit-breaker/breakertest"
)

type typedNilError struct{}

func (*typedNilError) Error() string { return "typed nil" }

func TestDefaultClassifierErrorMatrix(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("dependency failure")
	wrapped := errors.Join(errors.New("secondary"), sentinel)
	var typedNil *typedNilError
	tests := []struct {
		name string
		err  error
		want breaker.Outcome
	}{
		{name: "nil", err: nil, want: breaker.OutcomeSuccess},
		{name: "sentinel", err: sentinel, want: breaker.OutcomeFailure},
		{name: "wrapped and joined", err: wrapped, want: breaker.OutcomeFailure},
		{name: "typed nil interface", err: typedNil, want: breaker.OutcomeFailure},
		{name: "canceled", err: context.Canceled, want: breaker.OutcomeFailure},
		{name: "deadline", err: context.DeadlineExceeded, want: breaker.OutcomeFailure},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			b := mustBreaker(t, breaker.Config{Name: test.name})
			result, err := breaker.Execute(context.Background(), b, func(context.Context) (int, error) {
				return 42, test.err
			})
			if result != 42 || err != test.err {
				t.Fatalf("Execute() = %d, %v; want exact result/error", result, err)
			}
			snapshot := b.Snapshot()
			if test.want == breaker.OutcomeSuccess && (snapshot.Successes != 1 || snapshot.Failures != 0) {
				t.Fatalf("success Snapshot() = %+v", snapshot)
			}
			if test.want == breaker.OutcomeFailure && (snapshot.Failures != 1 || snapshot.Successes != 0) {
				t.Fatalf("failure Snapshot() = %+v", snapshot)
			}
		})
	}
}

func TestCallerClassifierOwnsCancellationPolicy(t *testing.T) {
	t.Parallel()

	for _, operationErr := range []error{context.Canceled, context.DeadlineExceeded} {
		operationErr := operationErr
		b := mustBreaker(t, breaker.Config{
			Name: "caller-cancellation",
			Classifier: func(completion breaker.Completion) breaker.Outcome {
				if errors.Is(completion.Err, context.Canceled) ||
					errors.Is(completion.Err, context.DeadlineExceeded) {
					return breaker.OutcomeIgnored
				}
				return breaker.OutcomeFailure
			},
		})
		_, err := breaker.Execute(context.Background(), b, func(context.Context) (struct{}, error) {
			return struct{}{}, operationErr
		})
		if err != operationErr {
			t.Fatalf("Execute() error = %v, want exact %v", err, operationErr)
		}
		if got := b.Snapshot(); got.Ignored != 1 || got.WindowClassified != 0 {
			t.Fatalf("Snapshot() = %+v", got)
		}
	}
}

func TestExecuteElapsedAndSlowOutcomeMatrix(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		operation  func(*breakertest.Clock) error
		wantSlowOK uint64
		wantSlowNG uint64
	}{
		{
			name: "slow success",
			operation: func(clock *breakertest.Clock) error {
				clock.Advance(time.Second)
				return nil
			},
			wantSlowOK: 1,
		},
		{
			name: "slow failure",
			operation: func(clock *breakertest.Clock) error {
				clock.Advance(time.Second)
				return errors.New("slow failure")
			},
			wantSlowNG: 1,
		},
		{
			name:      "fast success",
			operation: func(*breakertest.Clock) error { return nil },
		},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			clock := breakertest.NewClock(time.Unix(100, 0))
			b := mustBreaker(t, breaker.Config{
				Name:             test.name,
				Clock:            clock,
				SlowCallDuration: time.Second,
			})
			_, _ = breaker.Execute(context.Background(), b, func(context.Context) (struct{}, error) {
				return struct{}{}, test.operation(clock)
			})
			got := b.Snapshot()
			if got.SlowSuccesses != test.wantSlowOK || got.SlowFailures != test.wantSlowNG {
				t.Fatalf("Snapshot() = %+v", got)
			}
		})
	}
}

func TestOperationPanicUsesElapsedSlowClassification(t *testing.T) {
	clock := breakertest.NewClock(time.Unix(100, 0))
	b := mustBreaker(t, breaker.Config{
		Name:             "panic",
		Clock:            clock,
		SlowCallDuration: time.Second,
	})
	recovered := capturePanic(func() {
		_, _ = breaker.Execute(context.Background(), b, func(context.Context) (struct{}, error) {
			clock.Advance(time.Second)
			panic("operation panic")
		})
	})
	if recovered != "operation panic" {
		t.Fatalf("recovered panic = %#v", recovered)
	}
	if got := b.Snapshot(); got.Failures != 1 || got.SlowFailures != 1 {
		t.Fatalf("Snapshot() = %+v", got)
	}
}

func TestNewCopiesValueConfigurationBeforeAdmission(t *testing.T) {
	opening := &breaker.OpeningRules{FailureCount: 2}
	halfOpen := &breaker.HalfOpenPolicy{MaxProbes: 2, RequiredSuccesses: 2}
	config := breaker.Config{
		Name:              "immutable",
		MinimumThroughput: 1,
		Opening:           opening,
		HalfOpen:          halfOpen,
	}
	b := mustBreaker(t, config)
	opening.FailureCount = 1
	halfOpen.RequiredSuccesses = 1
	config.Name = "mutated"

	completeOutcome(t, b, breaker.OutcomeFailure)
	if got := b.Snapshot(); got.Name != "immutable" || got.State != breaker.StateClosed {
		t.Fatalf("Snapshot() after caller mutation = %+v", got)
	}
	completeOutcome(t, b, breaker.OutcomeFailure)
	if got := b.Snapshot().State; got != breaker.StateOpen {
		t.Fatalf("state = %s, want open after original threshold", got)
	}
}
