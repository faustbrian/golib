// Package safeerr provides errors that preserve a cause without exposing its
// potentially credential-bearing text through Error.
package safeerr

// Error wraps an error with safe public text.
type Error struct {
	message string
	cause   error
}

// Wrap preserves cause for errors.Is and errors.As while exposing only message.
func Wrap(message string, cause error) error {
	return &Error{message: message, cause: cause}
}

// Error returns text that is safe to log.
func (e *Error) Error() string {
	return e.message
}

// Unwrap returns the original error for programmatic inspection.
func (e *Error) Unwrap() error {
	return e.cause
}
