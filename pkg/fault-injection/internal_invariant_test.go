package faultinject

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestInternalOverflowAndUnknownTypeGuards(t *testing.T) {
	t.Parallel()

	if saturatingIncrement(math.MaxUint64) != math.MaxUint64 {
		t.Fatal("counter wrapped")
	}
	injector := &Injector{enabled: true, generation: math.MaxUint64, evaluations: 9}
	if injector.Reset() != math.MaxUint64 || injector.evaluations != 9 {
		t.Fatal("exhausted generation reset state")
	}
	if _, err := cloneSchedule(struct{}{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("unknown schedule error = %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("unknown validated schedule did not panic")
		}
	}()
	scheduleMatches(struct{}{}, 1)
}

func TestInternalFaultValidationGuards(t *testing.T) {
	t.Parallel()

	invalidPhase := Fault{Kind: KindCancel}
	if err := validateFault("fault", invalidPhase, time.Second, 8); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid phase error = %v", err)
	}
	var typedNil *typedNilError
	if !nilError(typedNil) || !nilError(nil) || nilError(errors.New("value")) || nilError(valueError{}) {
		t.Fatal("nil error classification failed")
	}
}

func TestInternalSystemTimeAndPanicContainment(t *testing.T) {
	t.Parallel()

	if (systemClock{}).Now().IsZero() {
		t.Fatal("system clock returned zero")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is((systemSleeper{}).Sleep(ctx, time.Hour), context.Canceled) {
		t.Fatal("system sleeper ignored cancellation")
	}
	if err := (systemSleeper{}).Sleep(context.Background(), time.Nanosecond); err != nil {
		t.Fatalf("system sleeper error = %v", err)
	}
	if safePredicate(func(Metadata) bool { panic("predicate") }, Metadata{}) {
		t.Fatal("panicking predicate matched")
	}
	if safeAuthorize(AuthorizerFunc(func(context.Context, Metadata) bool { panic("authorization") }), context.Background(), Metadata{}) {
		t.Fatal("panicking authorizer allowed")
	}
	if _, ok := runtimeNow(panickingInternalClock{}); ok {
		t.Fatal("panicking runtime clock succeeded")
	}
}

type typedNilError struct{}

func (*typedNilError) Error() string { return "typed nil" }

type valueError struct{}

func (valueError) Error() string { return "value" }

type panickingInternalClock struct{}

func (panickingInternalClock) Now() time.Time { panic("clock") }
