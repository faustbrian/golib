// Package safeerror preserves error identity without exposing arbitrary error
// text through an unwrap chain.
package safeerror

import "errors"

type redacted struct {
	cause   error
	message string
}

// Redact replaces cause formatting while preserving errors.Is checks. The
// original cause is deliberately not available through Unwrap or errors.As.
func Redact(cause error, message string) error {
	if cause == nil {
		return nil
	}
	// Traversing arbitrary wrapped errors here could invoke extension-owned
	// As methods. Only wrappers created directly by this package are trusted.
	//nolint:errorlint // Deliberately do not traverse an untrusted error chain.
	if _, ok := cause.(*redacted); ok {
		return cause
	}
	return &redacted{cause: cause, message: message}
}

func (e *redacted) Error() string { return e.message }

func (e *redacted) Is(target error) bool {
	return errors.Is(e.cause, target)
}
