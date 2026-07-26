package wsdl

import "strings"

// Severity classifies a validation diagnostic.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Diagnostic describes one bounded parsing, validation, or compilation issue.
type Diagnostic struct {
	Code     string
	Severity Severity
	Message  string
	Path     string
	Location Location
}

// Diagnostics is an ordered collection of issues.
type Diagnostics []Diagnostic

// HasErrors reports whether at least one error diagnostic is present.
func (d Diagnostics) HasErrors() bool {
	for _, diagnostic := range d {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Error returns a compact, deterministic summary suitable for error paths.
func (d Diagnostics) Error() string {
	if len(d) == 0 {
		return ""
	}
	messages := make([]string, 0, len(d))
	for _, diagnostic := range d {
		messages = append(messages, diagnostic.Code+": "+diagnostic.Message)
	}
	return strings.Join(messages, "; ")
}

// Err returns diagnostics as an error when any error diagnostic is present.
func (d Diagnostics) Err() error {
	if !d.HasErrors() {
		return nil
	}
	return d
}
