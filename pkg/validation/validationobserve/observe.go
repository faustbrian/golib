// Package validationobserve exposes non-sensitive observation hooks.
package validationobserve

import validation "github.com/faustbrian/golib/pkg/validation"

// Observation contains bounded labels only. It intentionally excludes paths,
// parameters, causes, and rejected values.
type Observation struct {
	Code      string
	Severity  string
	Operation string
}

// Observer receives safe validation observations.
type Observer interface {
	Observe(Observation)
}

// Report emits one safe observation per retained violation.
func Report(ctx validation.Context, report validation.Report, observer Observer) {
	for _, violation := range report.Violations() {
		severity := "error"
		if violation.Severity() == validation.Warning {
			severity = "warning"
		}
		operation := safeLabel(ctx.Operation(), "invalid_operation")
		observer.Observe(Observation{Code: violation.Code(), Severity: severity,
			Operation: operation})
	}
}

func safeLabel(value string, replacement string) string {
	if len(value) == 0 {
		return value
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '-' || character == '.' ||
			character == ':' {
			continue
		}
		return replacement
	}
	return value
}
