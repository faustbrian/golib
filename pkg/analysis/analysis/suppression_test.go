package analysis_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
	"time"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
)

func TestParseSuppressionsCreatesAuditableNarrowInventory(t *testing.T) {
	t.Parallel()

	fset, file := parseGo(t, `package p
//analysis:ignore architecture/import-boundary -- legacy adapter; expires=2030-01-02; issue=ARCH-7
import _ "example.com/infrastructure"
`)
	suppressions, err := shared.ParseSuppressions(
		fset,
		file,
		[]string{"architecture/import-boundary"},
		time.Date(2029, 1, 1, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("ParseSuppressions() error = %v", err)
	}
	if len(suppressions) != 1 {
		t.Fatalf("len(suppressions) = %d, want 1", len(suppressions))
	}
	got := suppressions[0]
	if got.Rule != "architecture/import-boundary" || got.TargetLine != 3 ||
		got.Reason != "legacy adapter" || got.Issue != "ARCH-7" {
		t.Fatalf("suppression = %#v", got)
	}
}

func TestParseSuppressionsIgnoresOrdinaryComments(t *testing.T) {
	t.Parallel()

	fset, file := parseGo(t, "package p\n// ordinary comment\nvar Value int\n")
	suppressions, err := shared.ParseSuppressions(fset, file, nil, time.Now())
	if err != nil || len(suppressions) != 0 {
		t.Fatalf("ParseSuppressions() = %#v, %v", suppressions, err)
	}
}

func TestParseSuppressionsRejectsInvalidDirective(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"missing rule":     "//analysis:ignore  -- reviewed",
		"unknown rule":     "//analysis:ignore security/unknown -- reviewed",
		"missing reason":   "//analysis:ignore architecture/import-boundary --  ",
		"expired":          "//analysis:ignore architecture/import-boundary -- old; expires=2020-01-01",
		"bad expiry":       "//analysis:ignore architecture/import-boundary -- old; expires=tomorrow",
		"unknown metadata": "//analysis:ignore architecture/import-boundary -- old; owner=team",
		"empty metadata":   "//analysis:ignore architecture/import-boundary -- old; issue=",
		"duplicate issue metadata": "//analysis:ignore architecture/import-boundary -- old; " +
			"issue=ARCH-1; issue=ARCH-2",
		"duplicate expiry metadata": "//analysis:ignore architecture/import-boundary -- old; " +
			"expires=2030-01-01; expires=2031-01-01",
		"duplicate": "//analysis:ignore architecture/import-boundary -- one\n" +
			"//analysis:ignore architecture/import-boundary -- two",
	}
	for name, directive := range tests {
		directive := directive
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fset, file := parseGo(t, "package p\n"+directive+"\nvar Value int\n")
			_, err := shared.ParseSuppressions(
				fset,
				file,
				[]string{"architecture/import-boundary"},
				time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			)
			if err == nil {
				t.Fatal("ParseSuppressions() error = nil, want rejection")
			}
		})
	}
}

func TestApplySuppressionsRejectsStaleAndFiltersExactLocation(t *testing.T) {
	t.Parallel()

	suppression := shared.Suppression{
		Rule:       "architecture/import-boundary",
		Filename:   "p.go",
		TargetLine: 4,
		Reason:     "reviewed exception",
	}
	diagnostic := shared.Diagnostic{
		Rule:     "architecture/import-boundary",
		Filename: "p.go",
		Line:     4,
		Message:  "forbidden import",
	}
	remaining, inventory, err := shared.ApplySuppressions(
		[]shared.Diagnostic{diagnostic},
		[]shared.Suppression{suppression},
	)
	if err != nil || len(remaining) != 0 || !inventory[0].Used {
		t.Fatalf("ApplySuppressions() = %#v, %#v, %v", remaining, inventory, err)
	}

	suppression.TargetLine = 5
	if _, _, err := shared.ApplySuppressions(
		[]shared.Diagnostic{diagnostic},
		[]shared.Suppression{suppression},
	); err == nil {
		t.Fatal("ApplySuppressions() error = nil, want stale rejection")
	}
}

func parseGo(t *testing.T, source string) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", source, parser.ParseComments)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	return fset, file
}
