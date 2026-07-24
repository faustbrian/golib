package validation

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Report is an immutable, ordered, deduplicated collection of violations.
type Report struct {
	limits     Limits
	violations []Violation
	keys       map[string]struct{}
	truncated  bool
	hasErrors  bool
}

// NewReport creates an empty report governed by limits.
func NewReport(limits Limits) Report {
	return Report{limits: limits, keys: make(map[string]struct{})}
}

// Add returns a report with a violation appended if it is not a duplicate.
func (r Report) Add(violation Violation) Report {
	if !validDiagnostic(violation, r.limits) {
		violation = invalidViolation()
	}
	if violation.path.exceedsRenderedLength(r.limits.MaxPathLength) {
		violation = NewViolation(RootPath(), "path_limit", Error, nil,
			ErrLimitExceeded)
	}
	key := violationKey(violation)
	if violation.severity == Error {
		r.hasErrors = true
	}
	if _, exists := r.keys[key]; exists {
		return r
	}
	if len(r.violations) >= r.limits.MaxViolations {
		r.truncated = true
		return r
	}
	r.violations = append(append([]Violation(nil), r.violations...), violation)
	r.keys = cloneSet(r.keys)
	r.keys[key] = struct{}{}
	return r
}

// Merge returns a report with other's violations appended in their order.
func (r Report) Merge(other Report) Report {
	for _, violation := range other.violations {
		r = r.Add(violation)
	}
	if other.truncated {
		r.truncated = true
	}
	if other.hasErrors {
		r.hasErrors = true
	}
	return r
}

// Len returns the number of retained violations.
func (r Report) Len() int { return len(r.violations) }

// Empty reports whether no violations were retained.
func (r Report) Empty() bool { return len(r.violations) == 0 }

// Truncated reports whether a violation was omitted by the configured limit.
func (r Report) Truncated() bool { return r.truncated }

// HasErrors reports whether any blocking violation was observed, including one
// omitted because MaxViolations was reached.
func (r Report) HasErrors() bool { return r.hasErrors }

// Violations returns a defensive copy preserving validation order.
func (r Report) Violations() []Violation {
	return append([]Violation(nil), r.violations...)
}

// HasCode reports whether a retained violation has code.
func (r Report) HasCode(code string) bool {
	for _, violation := range r.violations {
		if violation.code == code {
			return true
		}
	}
	return false
}

// Err returns a typed error if the report contains a blocking violation.
func (r Report) Err() error {
	if r.hasErrors {
		return &InvalidError{report: r}
	}
	return nil
}

// String returns a value-safe report summary.
func (r Report) String() string {
	noun := "violations"
	if len(r.violations) == 1 {
		noun = "violation"
	}
	result := fmt.Sprintf("validation failed with %d %s", len(r.violations), noun)
	if r.truncated {
		result += " (truncated)"
	}
	return result
}

func violationKey(violation Violation) string {
	keys := make([]string, 0, len(violation.parameters))
	for key := range violation.parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var result strings.Builder
	writeIdentityPart(&result, violation.path.identity())
	writeIdentityPart(&result, violation.code)
	writeIdentityPart(&result, strconv.Itoa(int(violation.severity)))
	for _, key := range keys {
		writeIdentityPart(&result, key)
		writeIdentityPart(&result, violation.parameters[key])
	}
	return result.String()
}

func writeIdentityPart(result *strings.Builder, value string) {
	result.WriteString(strconv.Itoa(len(value)))
	result.WriteByte(':')
	result.WriteString(value)
}

func cloneSet(source map[string]struct{}) map[string]struct{} {
	clone := make(map[string]struct{}, len(source)+1)
	for key := range source {
		clone[key] = struct{}{}
	}
	return clone
}
