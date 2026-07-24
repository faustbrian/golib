package apiquery

import (
	"fmt"
	"strings"
)

// ErrorCode is a stable machine-readable query failure category.
type ErrorCode string

const (
	CodeInvalidElement  ErrorCode = "invalid_element"
	CodeUnsupported     ErrorCode = "unsupported_operation"
	CodeConflict        ErrorCode = "conflict"
	CodeAuthorization   ErrorCode = "authorization_rejected"
	CodeCostLimit       ErrorCode = "cost_limit"
	CodeCursorFailure   ErrorCode = "cursor_failure"
	CodeVersionMismatch ErrorCode = "version_mismatch"
	CodeLimitExceeded   ErrorCode = "limit_exceeded"
)

// Violation is one sanitized failure at a stable request path.
type Violation struct {
	Code    ErrorCode `json:"code"`
	Path    string    `json:"path"`
	Message string    `json:"message"`
}

// Violations aggregates a bounded set of query failures.
type Violations struct {
	items []Violation
}

// Error returns a stable, sanitized summary.
func (v *Violations) Error() string {
	if v == nil || len(v.items) == 0 {
		return "query validation failed"
	}
	parts := make([]string, len(v.items))
	for index, item := range v.items {
		parts[index] = fmt.Sprintf("%s: %s", item.Path, item.Message)
	}
	return strings.Join(parts, "; ")
}

// Items returns a defensive copy of the structured failures.
func (v *Violations) Items() []Violation {
	if v == nil {
		return nil
	}
	return append([]Violation(nil), v.items...)
}

type violationCollector struct {
	limit int
	items []Violation
}

func (c *violationCollector) add(code ErrorCode, path, message string) {
	if c.limit > 0 && len(c.items) >= c.limit {
		return
	}
	c.items = append(c.items, Violation{Code: code, Path: path, Message: message})
}

func (c *violationCollector) err() error {
	if len(c.items) == 0 {
		return nil
	}
	return &Violations{items: append([]Violation(nil), c.items...)}
}
