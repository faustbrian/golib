// Package validation orchestrates small post-decode configuration validators.
package validation

import (
	"context"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/config/internal/safeerror"
)

// Validator checks a complete decoded candidate.
type Validator[T any] func(context.Context, T) error

// PanicError reports a recovered validator panic without retaining its value.
type PanicError struct {
	Operation string
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("configuration %s panicked", e.Operation)
}

// Failure associates a safe field path with an underlying validator cause.
type Failure struct {
	path  string
	cause error
}

// At associates cause with path without including the cause text in formatted
// diagnostics.
func At(path string, cause error) error {
	if cause == nil {
		return nil
	}
	return &Failure{path: path, cause: redactCause(cause)}
}

func (e *Failure) Error() string {
	if e.path == "" {
		return "configuration validation failed"
	}
	return fmt.Sprintf("configuration validation failed at %q", e.path)
}

func (e *Failure) Unwrap() error { return redactCause(e.cause) }

// Path returns the safe configuration field path.
func (e *Failure) Path() string { return e.path }

// Errors deterministically aggregates independent validation failures.
type Errors struct {
	failures []error
}

func (e *Errors) Error() string {
	label := "errors"
	if len(e.failures) == 1 {
		label = "error"
	}
	return fmt.Sprintf("configuration validation failed (%d %s)", len(e.failures), label)
}

func (e *Errors) Unwrap() []error {
	return append([]error(nil), e.failures...)
}

// Paths returns failure paths in validation order.
func (e *Errors) Paths() []string {
	paths := make([]string, 0, len(e.failures))
	for _, err := range e.failures {
		var failure *Failure
		if errors.As(err, &failure) {
			paths = append(paths, failure.Path())
		}
	}
	return paths
}

// Run invokes the candidate's Validate method, when present, followed by all
// registered validators. Independent failures are aggregated in stable order.
func Run[T any](ctx context.Context, value T, validators ...Validator[T]) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	failures := make([]error, 0, len(validators)+1)
	if validator, ok := any(value).(interface{ Validate() error }); ok {
		if err := safely("self-validator", validator.Validate); err != nil {
			failures = append(failures, normalize(err))
		}
	} else if validator, ok := any(&value).(interface{ Validate() error }); ok {
		if err := safely("self-validator", validator.Validate); err != nil {
			failures = append(failures, normalize(err))
		}
	}

	for _, validator := range validators {
		if err := ctx.Err(); err != nil {
			return err
		}
		if validator == nil {
			failures = append(failures, At("", errors.New("nil validator")))
			continue
		}
		if err := safely("validator", func() error { return validator(ctx, value) }); err != nil {
			failures = append(failures, normalize(err))
		}
	}

	if len(failures) == 0 {
		return nil
	}
	return &Errors{failures: failures}
}

func safely(operation string, run func() error) (err error) {
	defer func() {
		if recover() != nil {
			err = &PanicError{Operation: operation}
		}
	}()
	return run()
}

func normalize(err error) error {
	var failure *Failure
	if errors.As(err, &failure) {
		return err
	}
	return At("", err)
}

func redactCause(cause error) error {
	// A wrapped arbitrary error may contain secret text. Only the exact
	// library-owned type is safe to expose through errors.As.
	//nolint:errorlint // Deliberately do not traverse an untrusted error chain.
	if _, safe := cause.(*PanicError); safe {
		return cause
	}
	return safeerror.Redact(cause, "configuration validation cause redacted")
}
