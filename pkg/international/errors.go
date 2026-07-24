package international

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

const (
	maxDiagnosticBytes = 256
	maxKindBytes       = 64
)

var (
	// ErrInvalid identifies syntactically invalid input without retaining it.
	ErrInvalid = errors.New("international: invalid value")
	// ErrInvalidProvenance identifies incomplete or malformed source metadata.
	ErrInvalidProvenance = errors.New("international: invalid provenance")
	// ErrInvalidDataset identifies structurally invalid generated data.
	ErrInvalidDataset = errors.New("international: invalid dataset")
	// ErrResourceLimit identifies input rejected before excessive work.
	ErrResourceLimit = errors.New("international: resource limit exceeded")
)

// ParseError is a bounded diagnostic that never stores or echoes caller input.
type ParseError struct {
	kind   string
	reason string
}

// NewParseError creates a redacted parse error for a public value kind.
func NewParseError(kind, reason string) *ParseError {
	kind = diagnosticKind(kind)
	prefix := "international: invalid " + kind + ": "
	return &ParseError{kind: kind, reason: truncateUTF8(reason, maxDiagnosticBytes-len(prefix))}
}

// Error returns a bounded, input-redacted diagnostic.
func (err *ParseError) Error() string {
	return fmt.Sprintf("international: invalid %s: %s", err.kind, err.reason)
}

// Unwrap makes all ParseError values match ErrInvalid.
func (err *ParseError) Unwrap() error {
	return ErrInvalid
}

func truncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func diagnosticKind(value string) string {
	if value == "" || len(value) > maxKindBytes || !utf8.ValidString(value) {
		return "value"
	}
	for index := range value {
		character := value[index]
		if (character < 'a' || character > 'z') && character != ' ' && character != '-' {
			return "value"
		}
	}
	return value
}
