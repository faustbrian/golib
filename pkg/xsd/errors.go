package xsd

import (
	"errors"
	"fmt"
)

var (
	// ErrDTDForbidden reports a DTD or other XML directive. XSD documents do
	// not need DTD processing and directives are rejected before compilation.
	ErrDTDForbidden = errors.New("xsd: DTD and XML directives are forbidden")
	// ErrNotSchema reports that the document element is not xs:schema.
	ErrNotSchema = errors.New("xsd: root element is not an XML Schema schema")
	// ErrLimitExceeded reports that an explicit parser, serializer, or compiler
	// bound was exceeded.
	ErrLimitExceeded = errors.New("xsd: resource limit exceeded")
)

// Location identifies an offset in an input resource. Line and Column are
// one-based when known; Offset is a zero-based byte offset.
type Location struct {
	SystemID string
	Line     int
	Column   int
	Offset   int64
}

// Severity is the stable importance of a validation diagnostic.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic is a stable machine-readable schema or instance finding.
type Diagnostic struct {
	Severity Severity
	Code     string
	Message  string
	Path     string
	Location Location
}

// ParseError adds stable resource location information to a parsing failure.
type ParseError struct {
	Location Location
	Err      error
}

func (e *ParseError) Error() string {
	if e.Location.SystemID == "" {
		return fmt.Sprintf("xsd: line %d, column %d: %v", e.Location.Line, e.Location.Column, e.Err)
	}

	return fmt.Sprintf(
		"xsd: %s:%d:%d: %v",
		e.Location.SystemID,
		e.Location.Line,
		e.Location.Column,
		e.Err,
	)
}

// Unwrap supports errors.Is and errors.As.
func (e *ParseError) Unwrap() error { return e.Err }
