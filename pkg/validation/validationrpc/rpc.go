// Package validationrpc projects reports into JSON-RPC invalid-params errors.
package validationrpc

import validation "github.com/faustbrian/golib/pkg/validation"

// Error is a JSON-RPC error object.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    Data   `json:"data"`
}

// Data is the stable machine-readable invalid-params payload.
type Data struct {
	Violations []Violation `json:"violations"`
	Truncated  bool        `json:"truncated,omitempty"`
	HasErrors  bool        `json:"has_errors"`
}

// Violation is a safe JSON-RPC validation finding.
type Violation struct {
	Path       string            `json:"path"`
	Code       string            `json:"code"`
	Parameters map[string]string `json:"parameters,omitempty"`
	Severity   string            `json:"severity"`
}

// InvalidParams projects a report using the standard -32602 error code.
func InvalidParams(report validation.Report) Error {
	violations := report.Violations()
	projected := make([]Violation, 0, len(violations))
	for _, violation := range violations {
		projected = append(projected, Violation{
			Path: violation.Path().String(), Code: violation.Code(),
			Parameters: violation.Parameters(), Severity: severity(violation.Severity()),
		})
	}
	return Error{Code: -32602, Message: "Invalid params",
		Data: Data{Violations: projected, Truncated: report.Truncated(),
			HasErrors: report.HasErrors()}}
}

func severity(value validation.Severity) string {
	if value == validation.Warning {
		return "warning"
	}
	return "error"
}
