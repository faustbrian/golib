// Package validationjsonapi projects reports into JSON:API error objects.
package validationjsonapi

import validation "github.com/faustbrian/golib/pkg/validation"

// Document is a JSON:API error document with report-level metadata.
type Document struct {
	Errors []Error      `json:"errors"`
	Meta   DocumentMeta `json:"meta"`
}

// DocumentMeta preserves aggregation state.
type DocumentMeta struct {
	Truncated bool `json:"truncated"`
	HasErrors bool `json:"has_errors"`
}

// Error is a JSON:API validation error object.
type Error struct {
	Status string    `json:"status"`
	Code   string    `json:"code"`
	Source Source    `json:"source"`
	Meta   ErrorMeta `json:"meta"`
}

// ErrorMeta preserves safe rule parameters and severity.
type ErrorMeta struct {
	Parameters map[string]string `json:"parameters,omitempty"`
	Severity   string            `json:"severity"`
}

// Source identifies the invalid document location.
type Source struct {
	Pointer string `json:"pointer"`
}

// Errors projects report findings in their deterministic order.
func Errors(report validation.Report) Document {
	violations := report.Violations()
	projected := make([]Error, 0, len(violations))
	for _, violation := range violations {
		status := "422"
		severity := "error"
		if violation.Severity() == validation.Warning {
			status = "200"
			severity = "warning"
		}
		projected = append(projected, Error{
			Status: status, Code: violation.Code(),
			Source: Source{Pointer: violation.Path().JSONPointer()},
			Meta: ErrorMeta{Parameters: violation.Parameters(),
				Severity: severity},
		})
	}
	return Document{Errors: projected,
		Meta: DocumentMeta{Truncated: report.Truncated(),
			HasErrors: report.HasErrors()}}
}
