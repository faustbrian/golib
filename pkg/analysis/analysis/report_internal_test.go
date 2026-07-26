package analysis

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportPathPropagatesRelativePathFailure(t *testing.T) {
	t.Parallel()

	_, err := reportPathWithRel(
		filepath.VolumeName(filepath.Clean("/repo"))+string(filepath.Separator)+"repo",
		filepath.VolumeName(filepath.Clean("/repo"))+string(filepath.Separator)+"repo/file.go",
		func(string, string) (string, error) {
			return "", errors.New("different volumes")
		},
	)
	if err == nil || !strings.Contains(err.Error(), "make report path relative") {
		t.Fatalf("reportPathWithRel() error = %v", err)
	}
}

func TestReportOrderingPredicatesAreStrict(t *testing.T) {
	t.Parallel()

	rule := Rule{ID: "security/no-unsafe"}
	diagnostic := Diagnostic{Filename: "a.go", Rule: rule.ID}
	suppression := Suppression{Filename: "a.go", Rule: rule.ID}
	exception := PolicyException{Rule: rule.ID, Package: "example.com/a"}
	if ruleLess(rule, rule) ||
		diagnosticLess(diagnostic, diagnostic) ||
		suppressionLess(suppression, suppression) ||
		exceptionLess(exception, exception) {
		t.Fatal("report ordering predicate accepted an equal value")
	}
	if !ruleLess(Rule{ID: "a/a"}, Rule{ID: "z/z"}) ||
		!diagnosticLess(Diagnostic{Filename: "a.go"}, Diagnostic{Filename: "z.go"}) ||
		!suppressionLess(Suppression{Filename: "a.go"}, Suppression{Filename: "z.go"}) ||
		!exceptionLess(
			PolicyException{Rule: "a/a"},
			PolicyException{Rule: "z/z"},
		) {
		t.Fatal("report ordering predicate rejected ascending values")
	}
}
