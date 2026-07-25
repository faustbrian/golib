package prompts

import (
	"context"
	"errors"
	"strings"
)

// ValidationContext supplies caller-owned dependencies to validation and
// transformation callbacks.
type ValidationContext struct {
	Dependencies any
}

// Validator checks a typed value without transforming it.
type Validator[T any] func(context.Context, T, ValidationContext) error

// Transformer returns the next typed value in a transformation pipeline.
type Transformer[T any] func(context.Context, T, ValidationContext) (T, error)

// ValidationIssue is a safe, stable, field-addressable validation failure.
type ValidationIssue struct {
	code    string
	message string
	fields  []string
}

// NewValidationIssue creates a validation failure with defensively copied,
// terminal-safe content.
func NewValidationIssue(code string, message string, fields ...string) *ValidationIssue {
	safeFields := make([]string, len(fields))
	for index, field := range fields {
		safeFields[index] = safeText(field)
	}

	return &ValidationIssue{
		code:    safeText(code),
		message: safeText(message),
		fields:  safeFields,
	}
}

func (issue *ValidationIssue) Error() string {
	if issue == nil {
		return "validation failed"
	}
	if issue.message != "" {
		return issue.message
	}
	if issue.code != "" {
		return issue.code
	}

	return "validation failed"
}

// Code returns the stable machine-readable issue code.
func (issue *ValidationIssue) Code() string {
	if issue == nil {
		return ""
	}

	return issue.code
}

// Message returns safe caller-facing text.
func (issue *ValidationIssue) Message() string {
	if issue == nil {
		return ""
	}

	return issue.message
}

// Fields returns a defensive copy of relevant prompt identities.
func (issue *ValidationIssue) Fields() []string {
	if issue == nil {
		return nil
	}

	return append([]string(nil), issue.fields...)
}

func normalizeIssue(err error, promptID string) error {
	var issue *ValidationIssue
	if errors.As(err, &issue) {
		return issue
	}

	message := strings.TrimSpace(safeText(err.Error()))
	if message == "" {
		message = "validation failed"
	}

	return NewValidationIssue("validation", message, promptID)
}
