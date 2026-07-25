// Package router provides explicit, immutable HTTP route composition on top of
// the standard net/http programming model.
package router

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	// ErrInvalidRoute identifies a malformed route descriptor.
	ErrInvalidRoute = errors.New("invalid route")
	// ErrConflict identifies ambiguous or duplicate semantic routes.
	ErrConflict = errors.New("route conflict")
	// ErrDuplicateName identifies a repeated stable route name.
	ErrDuplicateName = errors.New("duplicate route name")
	// ErrInvalidParameter identifies malformed URL-generation parameters.
	ErrInvalidParameter = errors.New("invalid route parameter")
	// ErrGeneration identifies a named-route URL generation failure.
	ErrGeneration = errors.New("route generation failed")
	// ErrUnsupported identifies behavior deliberately unsupported by v1.
	ErrUnsupported = errors.New("unsupported routing behavior")
	// ErrCompileState identifies use of a builder after successful compilation.
	ErrCompileState = errors.New("invalid router compile state")
	// ErrLimitExceeded identifies a configured resource-budget violation.
	ErrLimitExceeded = errors.New("router limit exceeded")
)

// Error is a bounded startup or generation diagnostic. Kind supports
// errors.Is, while callers may use errors.As to inspect Field and Source.
type Error struct {
	Kind   error
	Field  string
	Source string
	Detail string
}

// Error returns a deterministic diagnostic without request data or handlers.
func (e *Error) Error() string {
	if e == nil {
		return "router error"
	}
	kind := "router error"
	if e.Kind != nil {
		kind = sanitizeDiagnostic(e.Kind.Error(), 80)
	}
	message := kind
	if e.Field != "" {
		message += ": " + sanitizeDiagnostic(e.Field, 32)
	}
	if e.Detail != "" {
		message += ": " + sanitizeDiagnostic(e.Detail, 80)
	}
	if e.Source != "" {
		message += fmt.Sprintf(" (source %s)", sanitizeDiagnostic(e.Source, 32))
	}
	return bounded(message, 160)
}

// Unwrap exposes the stable error category.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Kind
}

func bounded(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	inputTruncated := len(value) > limit
	if inputTruncated {
		value = value[:limit]
	}
	value = strings.ToValidUTF8(value, "�")
	if !inputTruncated && len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:validUTF8Boundary(value, limit)]
	}
	return value[:validUTF8Boundary(value, limit-3)] + "..."
}

func validUTF8Boundary(value string, boundary int) int {
	if boundary >= len(value) {
		return len(value)
	}
	for boundary > 0 && !utf8.RuneStart(value[boundary]) {
		boundary--
	}
	return boundary
}

func sanitizeDiagnostic(value string, limit int) string {
	value = bounded(value, limit)
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return '?'
		}
		return character
	}, value)
}
