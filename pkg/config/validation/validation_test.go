package validation_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/config/validation"
)

var errSelfValidation = errors.New("self validation failed")

type valueValidated string

func (valueValidated) Validate() error { return errSelfValidation }

type pointerValidated struct{ Validated bool }

func (v *pointerValidated) Validate() error {
	v.Validated = true
	return errSelfValidation
}

type panicValidated string

func (panicValidated) Validate() error { panic("canary-secret-value") }

func TestRunAggregatesFailuresInRegistrationOrder(t *testing.T) {
	t.Parallel()

	first := errors.New("first")
	second := errors.New("second")
	err := validation.Run(
		context.Background(),
		"value",
		func(context.Context, string) error { return validation.At("server.port", first) },
		func(context.Context, string) error { return nil },
		func(context.Context, string) error { return validation.At("server.host", second) },
	)

	var failures *validation.Errors
	if !errors.As(err, &failures) {
		t.Fatalf("Run() error = %v, want *validation.Errors", err)
	}
	if got, want := failures.Paths(), []string{"server.port", "server.host"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Errors.Paths() = %#v, want %#v", got, want)
	}
	if !errors.Is(err, first) || !errors.Is(err, second) {
		t.Fatalf("Run() error = %v, want wrapped causes", err)
	}
}

func TestRunErrorTextDoesNotExposeValidatorCause(t *testing.T) {
	t.Parallel()

	err := validation.Run(
		context.Background(),
		"value",
		func(context.Context, string) error {
			return validation.At("token", errors.New("canary-secret-value"))
		},
	)
	if err == nil {
		t.Fatal("Run() error = nil, want failure")
	}
	if got := err.Error(); got != "configuration validation failed (1 error)" {
		t.Fatalf("Run() error text = %q", got)
	}
}

func TestRunHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := validation.Run(ctx, "value", func(context.Context, string) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("Run() called validator after cancellation")
	}
}

func TestRunRecoversAndRedactsValidatorPanic(t *testing.T) {
	t.Parallel()

	err := validation.Run(
		context.Background(),
		"value",
		func(context.Context, string) error { panic("canary-secret-value") },
	)
	var panicErr *validation.PanicError
	if !errors.As(err, &panicErr) {
		t.Fatalf("expected PanicError, got %T: %v", err, err)
	}
	if strings.Contains(err.Error(), "canary-secret-value") {
		t.Fatalf("error leaked panic payload: %q", err)
	}
}

func TestRunInvokesValueAndPointerSelfValidatorsBeforeRegisteredValidators(t *testing.T) {
	t.Parallel()

	tests := map[string]any{
		"value":   valueValidated("value"),
		"pointer": pointerValidated{},
	}
	for name, value := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			registered := errors.New("registered failed")
			var err error
			switch value := value.(type) {
			case valueValidated:
				err = validation.Run(context.Background(), value,
					func(context.Context, valueValidated) error { return registered })
			case pointerValidated:
				err = validation.Run(context.Background(), value,
					func(context.Context, pointerValidated) error { return registered })
			}
			if !errors.Is(err, errSelfValidation) || !errors.Is(err, registered) {
				t.Fatalf("Run() error = %v, want both failures", err)
			}
			var failures *validation.Errors
			if !errors.As(err, &failures) || !reflect.DeepEqual(failures.Paths(), []string{"", ""}) {
				t.Fatalf("Run() failures = %#v", failures)
			}
		})
	}
}

func TestRunHandlesNilValidatorAndCancellationBetweenValidators(t *testing.T) {
	t.Parallel()

	err := validation.Run[string](context.Background(), "value", nil)
	var failures *validation.Errors
	if !errors.As(err, &failures) || !reflect.DeepEqual(failures.Paths(), []string{""}) {
		t.Fatalf("Run(nil) error = %#v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	secondCalled := false
	err = validation.Run(
		ctx,
		"value",
		func(context.Context, string) error { cancel(); return nil },
		func(context.Context, string) error { secondCalled = true; return nil },
	)
	if !errors.Is(err, context.Canceled) || secondCalled {
		t.Fatalf("Run() = %v, second called = %t", err, secondCalled)
	}
}

func TestRunReturnsNilForValidCandidateWithoutValidators(t *testing.T) {
	t.Parallel()

	if err := validation.Run(context.Background(), "value"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if validation.At("path", nil) != nil {
		t.Fatal("At(nil) != nil")
	}
}

func TestRunRecoversAndRedactsSelfValidatorPanic(t *testing.T) {
	t.Parallel()

	err := validation.Run(context.Background(), panicValidated("value"))
	var panicErr *validation.PanicError
	if !errors.As(err, &panicErr) || panicErr.Operation != "self-validator" {
		t.Fatalf("Run() panic error = %#v", panicErr)
	}
	if strings.Contains(err.Error(), "canary-secret-value") {
		t.Fatalf("Run() leaked panic payload: %q", err)
	}
	if panicErr.Error() != "configuration self-validator panicked" {
		t.Fatalf("PanicError.Error() = %q", panicErr)
	}
}

func TestFailureFormattingAndAggregateUnwrapAreSafe(t *testing.T) {
	t.Parallel()

	cause := errors.New("canary-secret-value")
	pathFailure := validation.At("token", cause)
	rootFailure := validation.At("", cause)
	if pathFailure.Error() != `configuration validation failed at "token"` {
		t.Fatalf("path Failure.Error() = %q", pathFailure)
	}
	if rootFailure.Error() != "configuration validation failed" {
		t.Fatalf("root Failure.Error() = %q", rootFailure)
	}
	if !errors.Is(pathFailure, cause) {
		t.Fatal("Failure does not unwrap cause")
	}
	if unwrapped := errors.Unwrap(pathFailure); unwrapped == nil ||
		strings.Contains(unwrapped.Error(), "canary-secret-value") {
		t.Fatalf("Failure.Unwrap() leaked cause: %v", unwrapped)
	}

	err := validation.Run(
		context.Background(), "value",
		func(context.Context, string) error { return pathFailure },
	)
	var aggregate *validation.Errors
	if !errors.As(err, &aggregate) {
		t.Fatalf("Run() error = %T %v", err, err)
	}
	first := aggregate.Unwrap()
	first[0] = errors.New("mutated")
	if !errors.Is(aggregate.Unwrap()[0], cause) {
		t.Fatal("Errors.Unwrap() exposed internal slice")
	}
}

func TestRunRedactsRawCustomCauseAtEveryExposedErrorLayer(t *testing.T) {
	t.Parallel()

	cause := errors.New("canary-secret-value")
	err := validation.Run(
		context.Background(),
		"value",
		func(context.Context, string) error { return cause },
	)
	if !errors.Is(err, cause) {
		t.Fatalf("Run() error = %v, want cause identity", err)
	}
	var aggregate *validation.Errors
	if !errors.As(err, &aggregate) {
		t.Fatalf("Run() error = %T %v", err, err)
	}
	failures := aggregate.Unwrap()
	if len(failures) != 1 {
		t.Fatalf("Errors.Unwrap() = %#v", failures)
	}
	for current := failures[0]; current != nil; current = errors.Unwrap(current) {
		if strings.Contains(current.Error(), "canary-secret-value") {
			t.Fatalf("exposed error leaked custom cause: %T %v", current, current)
		}
	}
}
