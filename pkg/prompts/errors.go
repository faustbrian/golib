package prompts

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorKind identifies a stable class of prompt failure.
type ErrorKind string

const (
	ErrorInteractionNotPermitted ErrorKind = "interaction_not_permitted"
	ErrorTerminalUnavailable     ErrorKind = "terminal_unavailable"
	ErrorCanceled                ErrorKind = "canceled"
	ErrorDeadlineExceeded        ErrorKind = "deadline_exceeded"
	ErrorEndOfInput              ErrorKind = "end_of_input"
	ErrorTerminalDetached        ErrorKind = "terminal_detached"
	ErrorInvalidDefinition       ErrorKind = "invalid_definition"
	ErrorUnsupported             ErrorKind = "unsupported_configuration"
	ErrorValidationExhausted     ErrorKind = "validation_exhausted"
	ErrorRenderer                ErrorKind = "renderer_failure"
	ErrorReader                  ErrorKind = "reader_failure"
	ErrorWriter                  ErrorKind = "writer_failure"
	ErrorTerminalControl         ErrorKind = "terminal_control_failure"
	ErrorAdapter                 ErrorKind = "adapter_failure"
)

type sentinelError ErrorKind

func (err sentinelError) Error() string {
	return string(err)
}

var (
	ErrInteractionNotPermitted error = sentinelError(ErrorInteractionNotPermitted)
	ErrTerminalUnavailable     error = sentinelError(ErrorTerminalUnavailable)
	ErrCanceled                error = sentinelError(ErrorCanceled)
	ErrDeadlineExceeded        error = sentinelError(ErrorDeadlineExceeded)
	ErrEndOfInput              error = sentinelError(ErrorEndOfInput)
	ErrTerminalDetached        error = sentinelError(ErrorTerminalDetached)
	ErrInvalidDefinition       error = sentinelError(ErrorInvalidDefinition)
	ErrUnsupported             error = sentinelError(ErrorUnsupported)
	ErrValidationExhausted     error = sentinelError(ErrorValidationExhausted)
	ErrRenderer                error = sentinelError(ErrorRenderer)
	ErrReader                  error = sentinelError(ErrorReader)
	ErrWriter                  error = sentinelError(ErrorWriter)
	ErrTerminalControl         error = sentinelError(ErrorTerminalControl)
	ErrAdapter                 error = sentinelError(ErrorAdapter)
)

// Error describes a prompt failure without incorporating unsafe cause text.
type Error struct {
	Kind      ErrorKind
	Operation string
	PromptID  string
	Cause     error
}

func (err *Error) Error() string {
	if err == nil {
		return "prompt failure"
	}

	message := string(err.Kind)
	if err.Operation != "" {
		message = safeText(err.Operation) + ": " + message
	}
	if err.PromptID != "" {
		message += fmt.Sprintf(" (prompt %q)", safeText(err.PromptID))
	}

	return message
}

func (err *Error) Is(target error) bool {
	if err == nil || target == nil {
		return false
	}

	var sentinel sentinelError
	if errors.As(target, &sentinel) {
		return err.Kind == ErrorKind(sentinel)
	}

	var other *Error
	return errors.As(target, &other) && err.Kind == other.Kind
}

func (err *Error) Unwrap() error {
	if err == nil {
		return nil
	}

	return err.Cause
}

func safeText(value string) string {
	return strings.Map(func(char rune) rune {
		if char < ' ' || char == '\u007f' {
			return '\ufffd'
		}

		return char
	}, value)
}
