package apiqueryvalidation

import (
	"errors"
	"testing"

	validation "github.com/faustbrian/golib/pkg/validation"
)

func TestReportHandlesEmptyAndGenericErrors(t *testing.T) {
	t.Parallel()

	limits := validation.DefaultLimits()
	if report := Report(nil, limits); !report.Empty() {
		t.Fatalf("Report(nil) = %#v", report)
	}
	report := Report(errors.New("unsafe details"), limits)
	if report.Len() != 1 || !report.HasCode("query_error") || report.Violations()[0].Cause() != nil {
		t.Fatalf("Report(generic) = %#v", report.Violations())
	}
}

func TestParsePathSemanticMatrix(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"filter.children[2].values[key]": "filter.children[2].values[key]",
		"plain":                          "plain",
		"[3]":                            "[3]",
		"[key]":                          "[key]",
		"[unfinished":                    "[unfinished]",
		".leading":                       "leading",
	}
	for input, want := range tests {
		if got := parsePath(input).String(); got != want {
			t.Fatalf("parsePath(%q) = %q, want %q", input, got, want)
		}
	}
}
