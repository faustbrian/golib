package breaker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

func TestExecutePreservesTypedResultAndOriginalError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("dependency unavailable")
	b := mustBreaker(t, breaker.Config{Name: "inventory"})
	result, err := breaker.Execute(context.Background(), b, func(context.Context) (int, error) {
		return 42, wantErr
	})

	if result != 42 {
		t.Fatalf("Execute() result = %d, want 42", result)
	}
	if err != wantErr {
		t.Fatalf("Execute() error = %v, want original error", err)
	}
	if got := b.Snapshot(); got.Failures != 1 || got.Successes != 0 {
		t.Fatalf("Snapshot() = %+v", got)
	}
}

func TestExecuteDoesNotInvokeRejectedOperation(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := openBreaker(t, clock, &breaker.HalfOpenPolicy{
		MaxProbes:         1,
		RequiredSuccesses: 1,
	})
	invoked := false

	_, err := breaker.Execute(context.Background(), b, func(context.Context) (string, error) {
		invoked = true
		return "unexpected", nil
	})

	if !errors.Is(err, breaker.ErrOpen) {
		t.Fatalf("Execute() error = %v, want ErrOpen", err)
	}
	if invoked {
		t.Fatal("Execute() invoked rejected operation")
	}
}

func TestExecuteHonorsClassifierAndSlowThreshold(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := mustBreaker(t, breaker.Config{
		Name:             "inventory",
		Clock:            clock,
		SlowCallDuration: 100 * time.Millisecond,
		Classifier: func(completion breaker.Completion) breaker.Outcome {
			if completion.Result == "cached" {
				return breaker.OutcomeIgnored
			}
			return breaker.OutcomeSuccess
		},
	})

	_, err := breaker.Execute(context.Background(), b, func(context.Context) (string, error) {
		clock.Advance(time.Second)
		return "cached", errors.New("local cache marker")
	})
	if err == nil {
		t.Fatal("Execute() error = nil, want original operation error")
	}
	if got := b.Snapshot(); got.Ignored != 1 || got.WindowClassified != 0 || got.SlowFailures != 0 {
		t.Fatalf("Snapshot() = %+v", got)
	}
}

func TestExecuteProvidesCallerContextToClassifier(t *testing.T) {
	t.Parallel()

	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request")
	classifierCalled := false
	b := mustBreaker(t, breaker.Config{
		Name: "inventory",
		Classifier: func(completion breaker.Completion) breaker.Outcome {
			classifierCalled = true
			if completion.Context != ctx || completion.Context.Value(contextKey{}) != "request" {
				t.Fatalf("Completion.Context = %#v", completion.Context)
			}

			return breaker.OutcomeSuccess
		},
	})
	if _, err := breaker.Execute(ctx, b, func(context.Context) (int, error) {
		return 42, nil
	}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !classifierCalled {
		t.Fatal("classifier was not called")
	}
}

func TestExecuteCancellationBeforeAdmissionDoesNotConsumePermit(t *testing.T) {
	t.Parallel()

	b := mustBreaker(t, breaker.Config{Name: "inventory"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	invoked := false

	_, err := breaker.Execute(ctx, b, func(context.Context) (int, error) {
		invoked = true
		return 0, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() error = %v, want context.Canceled", err)
	}
	if invoked {
		t.Fatal("Execute() invoked operation after pre-admission cancellation")
	}
	if got := b.Snapshot(); got.Admitted != 0 || got.WindowClassified != 0 {
		t.Fatalf("Snapshot() = %+v", got)
	}
}

func TestExecuteRecordsOperationPanicAndRepanicsOriginalValue(t *testing.T) {
	t.Parallel()

	b := mustBreaker(t, breaker.Config{Name: "inventory"})
	panicValue := &struct{ message string }{message: "boom"}

	recovered := capturePanic(func() {
		_, _ = breaker.Execute(context.Background(), b, func(context.Context) (int, error) {
			panic(panicValue)
		})
	})
	if recovered != panicValue {
		t.Fatalf("recovered panic = %#v, want original value", recovered)
	}
	if got := b.Snapshot(); got.Failures != 1 {
		t.Fatalf("Snapshot().Failures = %d, want 1", got.Failures)
	}
}

func TestExecuteRecordsCompletionsAfterPermitTTL(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("dependency unavailable")
	tests := []struct {
		name         string
		operationErr error
		wantSuccess  uint64
		wantFailure  uint64
	}{
		{name: "success", wantSuccess: 1},
		{name: "failure", operationErr: wantErr, wantFailure: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			clock := &fakeClock{now: time.Unix(100, 0)}
			b := mustBreaker(t, breaker.Config{
				Name: "inventory", Clock: clock, PermitTTL: time.Second,
			})
			result, err := breaker.Execute(context.Background(), b, func(context.Context) (int, error) {
				clock.Advance(time.Second)

				return 42, test.operationErr
			})
			if result != 42 || err != test.operationErr {
				t.Fatalf("Execute() = %d, %v", result, err)
			}
			snapshot := b.Snapshot()
			if snapshot.Admitted != 1 || snapshot.Completed != 1 ||
				snapshot.TotalSuccesses != test.wantSuccess ||
				snapshot.TotalFailures != test.wantFailure {
				t.Fatalf("Snapshot() = %+v", snapshot)
			}
		})
	}
}

func TestExecuteRecordsPanicAfterPermitTTL(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := mustBreaker(t, breaker.Config{
		Name: "inventory", Clock: clock, PermitTTL: time.Second,
	})
	panicValue := &struct{ message string }{message: "late boom"}
	recovered := capturePanic(func() {
		_, _ = breaker.Execute(context.Background(), b, func(context.Context) (int, error) {
			clock.Advance(time.Second)
			panic(panicValue)
		})
	})
	if recovered != panicValue {
		t.Fatalf("recovered panic = %#v, want original value", recovered)
	}
	if snapshot := b.Snapshot(); snapshot.Admitted != 1 ||
		snapshot.Completed != 1 || snapshot.TotalFailures != 1 {
		t.Fatalf("Snapshot() = %+v", snapshot)
	}
}

func TestExecuteRecordsLifetimeOutcomeAfterHalfOpenPermitExpiry(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{now: time.Unix(100, 0)}
	b := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		Clock:             clock,
		PermitTTL:         time.Second,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Second),
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         1,
			RequiredSuccesses: 1,
		},
	})
	completeOutcome(t, b, breaker.OutcomeFailure)
	clock.Advance(time.Second)

	var replacement *breaker.Permit
	result, err := breaker.Execute(context.Background(), b, func(ctx context.Context) (int, error) {
		clock.Advance(time.Second)
		var acquireErr error
		replacement, acquireErr = b.Acquire(ctx)
		if acquireErr != nil {
			t.Fatalf("replacement Acquire() error = %v", acquireErr)
		}

		return 42, nil
	})
	if result != 42 || err != nil {
		t.Fatalf("Execute() = %d, %v", result, err)
	}
	if err := replacement.Cancel(); err != nil {
		t.Fatalf("replacement Cancel() error = %v", err)
	}
	if snapshot := b.Snapshot(); snapshot.Admitted != 3 ||
		snapshot.Completed != 2 || snapshot.TotalSuccesses != 1 ||
		snapshot.TotalFailures != 1 || snapshot.State != breaker.StateHalfOpen {
		t.Fatalf("Snapshot() = %+v", snapshot)
	}
}

func TestExecuteRecordsClassifierPanicAndRepanics(t *testing.T) {
	t.Parallel()

	b := mustBreaker(t, breaker.Config{
		Name: "inventory",
		Classifier: func(breaker.Completion) breaker.Outcome {
			panic("classifier panic")
		},
	})

	recovered := capturePanic(func() {
		_, _ = breaker.Execute(context.Background(), b, func(context.Context) (int, error) {
			return 1, nil
		})
	})
	if recovered != "classifier panic" {
		t.Fatalf("recovered panic = %#v", recovered)
	}
	if got := b.Snapshot(); got.Failures != 1 {
		t.Fatalf("Snapshot().Failures = %d, want 1", got.Failures)
	}
}

func TestExecuteInvalidClassifierOutcomeReleasesHalfOpenCapacity(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	b := mustBreaker(t, breaker.Config{
		Name:              "inventory",
		Clock:             clock,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Second),
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         1,
			RequiredSuccesses: 1,
		},
		Classifier: func(breaker.Completion) breaker.Outcome {
			return breaker.Outcome(99)
		},
	})
	completeOutcome(t, b, breaker.OutcomeFailure)
	clock.Advance(time.Second)

	_, err := breaker.Execute(context.Background(), b, func(context.Context) (string, error) {
		return "result", nil
	})
	if !errors.Is(err, breaker.ErrInvalidOutcome) {
		t.Fatalf("Execute() error = %v, want ErrInvalidOutcome", err)
	}
	if got := b.Snapshot().ActiveHalfOpen; got != 0 {
		t.Fatalf("Snapshot().ActiveHalfOpen = %d, want 0", got)
	}
	if _, err := b.Acquire(context.Background()); err != nil {
		t.Fatalf("replacement Acquire() error = %v", err)
	}
}

func capturePanic(operation func()) (recovered any) {
	defer func() { recovered = recover() }()
	operation()
	return nil
}
