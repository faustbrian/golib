package cli

import (
	"context"
	"errors"
	"fmt"
)

// ErrorKind is a stable framework error classification.
type ErrorKind string

const (
	// ErrorKindHelp identifies an explicit help request.
	ErrorKindHelp ErrorKind = "help"
	// ErrorKindVersion identifies an explicit version request.
	ErrorKindVersion ErrorKind = "version"
	// ErrorKindUnknownCommand identifies an unknown command token.
	ErrorKindUnknownCommand ErrorKind = "unknown_command"
	// ErrorKindUnknownOption identifies an unknown long or short option.
	ErrorKindUnknownOption ErrorKind = "unknown_option"
	// ErrorKindMissingValue identifies an option without its required value.
	ErrorKindMissingValue ErrorKind = "missing_value"
	// ErrorKindUsage identifies malformed argv or invalid typed input.
	ErrorKindUsage ErrorKind = "usage"
	// ErrorKindMalformedValue identifies failed typed conversion.
	ErrorKindMalformedValue ErrorKind = "malformed_value"
	// ErrorKindCommand identifies a handler failure.
	ErrorKindCommand ErrorKind = "command"
	// ErrorKindValidation identifies application input validation failure.
	ErrorKindValidation ErrorKind = "validation"
	// ErrorKindCleanup identifies resource cleanup failure.
	ErrorKindCleanup ErrorKind = "cleanup"
	// ErrorKindOutput identifies rendering or writer failure.
	ErrorKindOutput ErrorKind = "output"
	// ErrorKindCompletion identifies dynamic completion provider failure.
	ErrorKindCompletion ErrorKind = "completion"
	// ErrorKindCanceled identifies cancellation.
	ErrorKindCanceled ErrorKind = "canceled"
	// ErrorKindDeadline identifies deadline expiration.
	ErrorKindDeadline ErrorKind = "deadline"
	// ErrorKindInternal identifies invalid framework construction or state.
	ErrorKindInternal ErrorKind = "internal"
)

var (
	// ErrHelp matches an explicit help request.
	ErrHelp = errors.New("cli help requested")
	// ErrVersion matches an explicit version request.
	ErrVersion = errors.New("cli version requested")
	// ErrUnknownCommand matches an unknown command token.
	ErrUnknownCommand = errors.New("cli unknown command")
	// ErrUnknownOption matches an unknown option token.
	ErrUnknownOption = errors.New("cli unknown option")
	// ErrMissingValue matches an option without its required value.
	ErrMissingValue = errors.New("cli missing option value")
	// ErrUsage matches malformed argv and invalid typed input.
	ErrUsage = errors.New("cli usage error")
	// ErrMalformedValue matches failed typed conversion.
	ErrMalformedValue = errors.New("cli malformed value error")
	// ErrCommand matches command execution failures.
	ErrCommand = errors.New("cli command error")
	// ErrValidation matches application validation failures.
	ErrValidation = errors.New("cli validation error")
	// ErrCleanup matches resource cleanup failures.
	ErrCleanup = errors.New("cli cleanup error")
	// ErrOutput matches rendering and writer failures.
	ErrOutput = errors.New("cli output error")
	// ErrCompletion matches dynamic completion provider failures.
	ErrCompletion = errors.New("cli completion error")
	// ErrCanceled matches canceled execution.
	ErrCanceled = errors.New("cli execution canceled")
	// ErrDeadline matches deadline expiration.
	ErrDeadline = errors.New("cli execution deadline exceeded")
	// ErrInternal matches framework failures.
	ErrInternal = errors.New("cli internal error")
)

// Error retains a stable classification and an optional underlying cause.
type Error struct {
	kind    ErrorKind
	message string
	cause   error
}

// Kind returns the stable error classification.
func (err *Error) Kind() ErrorKind {
	if err == nil {
		return ""
	}

	return err.kind
}

// Error returns the safe public diagnostic.
func (err *Error) Error() string {
	if err == nil {
		return "<nil>"
	}

	return err.message
}

// Unwrap exposes the retained cause to errors.Is and errors.As.
func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.cause
}

// Is supports stable classification through errors.Is.
func (err *Error) Is(target error) bool {
	if err == nil {
		return false
	}
	if target == context.Canceled && err.kind == ErrorKindCanceled {
		return true
	}
	if target == context.DeadlineExceeded && err.kind == ErrorKindDeadline {
		return true
	}

	return target == sentinelForKind(err.kind)
}

func newInternalError(message string, cause error) error {
	return newClassifiedError(ErrorKindInternal, message, cause, true)
}

func newClassifiedError(
	kind ErrorKind,
	message string,
	cause error,
	includeCause bool,
) error {
	if cause != nil && includeCause {
		message = fmt.Sprintf("%s: %v", message, cause)
	}
	message = sanitizeTerminal(message)

	return &Error{kind: kind, message: message, cause: cause}
}

func sentinelForKind(kind ErrorKind) error {
	switch kind {
	case ErrorKindHelp:
		return ErrHelp
	case ErrorKindVersion:
		return ErrVersion
	case ErrorKindUnknownCommand:
		return ErrUnknownCommand
	case ErrorKindUnknownOption:
		return ErrUnknownOption
	case ErrorKindMissingValue:
		return ErrMissingValue
	case ErrorKindUsage:
		return ErrUsage
	case ErrorKindMalformedValue:
		return ErrMalformedValue
	case ErrorKindCommand:
		return ErrCommand
	case ErrorKindValidation:
		return ErrValidation
	case ErrorKindCleanup:
		return ErrCleanup
	case ErrorKindOutput:
		return ErrOutput
	case ErrorKindCompletion:
		return ErrCompletion
	case ErrorKindCanceled:
		return ErrCanceled
	case ErrorKindDeadline:
		return ErrDeadline
	case ErrorKindInternal:
		return ErrInternal
	default:
		return nil
	}
}
