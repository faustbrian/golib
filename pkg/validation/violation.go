package validation

import (
	"fmt"
	"strconv"
	"unicode/utf8"
)

// Severity distinguishes blocking errors from advisory warnings.
type Severity uint8

const (
	// Error is a blocking validation failure.
	Error Severity = iota + 1
	// Warning is a non-blocking validation observation.
	Warning
)

// Violation is a value-safe machine-readable validation finding.
type Violation struct {
	path       Path
	code       string
	severity   Severity
	parameters map[string]string
	cause      error
}

// NewViolation constructs a violation. Parameters and paths are copied;
// malformed or unsafe diagnostic metadata fails closed.
func NewViolation(path Path, code string, severity Severity,
	parameters map[string]string, safeCause error,
) Violation {
	violation := Violation{path: path, code: code, severity: severity,
		parameters: parameters, cause: safeCause}
	if !validDiagnostic(violation, DefaultLimits()) {
		return invalidViolation()
	}
	violation.parameters = cloneMap(parameters)
	return violation
}

// Path returns the stable field location.
func (v Violation) Path() Path { return v.path }

// Code returns the machine-readable rule identity.
func (v Violation) Code() string { return v.code }

// Severity returns whether the finding blocks acceptance.
func (v Violation) Severity() Severity { return v.severity }

// Parameters returns a defensive copy of safe message parameters.
func (v Violation) Parameters() map[string]string { return cloneMap(v.parameters) }

// Cause returns an explicitly safe underlying cause.
func (v Violation) Cause() error { return v.cause }

// String deliberately excludes parameters, causes, and rejected values.
func (v Violation) String() string {
	path := v.path.String()
	if path == "" {
		return v.code
	}
	if !safeText(path) {
		path = strconv.QuoteToGraphic(path)
	}
	return fmt.Sprintf("%s: %s", path, v.code)
}

func validDiagnostic(violation Violation, limits Limits) bool {
	if violation.severity != Error && violation.severity != Warning {
		return false
	}
	if !validCode(violation.code, limits.MaxMetadataKeyLength) ||
		len(violation.parameters) > limits.MaxMetadataEntries {
		return false
	}
	for key, value := range violation.parameters {
		if !validCode(key, limits.MaxMetadataKeyLength) ||
			len(value) > limits.MaxMetadataValueLength || !safeText(value) {
			return false
		}
	}
	return true
}

func validCode(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '-' || character == '.' ||
			character == ':' {
			continue
		}
		return false
	}
	return true
}

func safeText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character < ' ' || character == 0x7f {
			return false
		}
	}
	return true
}

func invalidViolation() Violation {
	return Violation{path: RootPath(), code: "invalid_violation",
		severity: Error, parameters: make(map[string]string),
		cause: ErrInvalidViolation}
}
