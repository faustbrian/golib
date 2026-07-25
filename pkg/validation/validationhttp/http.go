// Package validationhttp provides router-neutral HTTP report projection.
package validationhttp

import (
	"encoding/json"
	"net/http"

	validation "github.com/faustbrian/golib/pkg/validation"
)

// Problem is an RFC 9457-style validation problem.
type Problem struct {
	Type   string  `json:"type"`
	Title  string  `json:"title"`
	Status int     `json:"status"`
	Errors []Error `json:"errors"`
	// Truncated reports whether findings were omitted by MaxViolations.
	Truncated bool `json:"truncated,omitempty"`
}

// Error is one safe HTTP validation finding.
type Error struct {
	Path       string            `json:"path"`
	Code       string            `json:"code"`
	Parameters map[string]string `json:"parameters,omitempty"`
	Severity   string            `json:"severity"`
}

// FromReport projects a report into an unprocessable-content problem.
func FromReport(report validation.Report) Problem {
	violations := report.Violations()
	errors := make([]Error, 0, len(violations))
	for _, violation := range violations {
		errors = append(errors, Error{Path: violation.Path().String(),
			Code: violation.Code(), Parameters: violation.Parameters(),
			Severity: severity(violation.Severity())})
	}
	status := http.StatusOK
	title := "Validation warnings"
	if report.HasErrors() {
		status = http.StatusUnprocessableEntity
		title = "Validation failed"
	}
	return Problem{Type: "https://validation.invalid/problem", Title: title,
		Status: status, Errors: errors, Truncated: report.Truncated()}
}

func severity(value validation.Severity) string {
	if value == validation.Warning {
		return "warning"
	}
	return "error"
}

// WriteProblem writes a problem without depending on a router.
func WriteProblem(writer http.ResponseWriter, problem Problem) error {
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.WriteHeader(problem.Status)
	return json.NewEncoder(writer).Encode(problem)
}

// Hook is a transport integration seam that does not own routing or binding.
type Hook[T any] func(*http.Request, T) validation.Report

// Validate invokes the application-supplied request validation hook.
func (hook Hook[T]) Validate(request *http.Request, value T) validation.Report {
	return hook(request, value)
}
