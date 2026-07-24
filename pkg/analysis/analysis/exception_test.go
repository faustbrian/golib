package analysis_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
)

func TestApplyPolicyExceptionsFiltersExactMatchesWithoutMutatingInput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	diagnostics := []shared.Diagnostic{
		{
			Rule:     "security/no-unsafe",
			Package:  "example.com/service/internal/bridge",
			Filename: filepath.Join(root, "internal", "bridge", "abi.go"),
		},
		{
			Rule:     "security/no-unsafe",
			Package:  "example.com/service/internal/other",
			Filename: filepath.Join(root, "internal", "other", "unsafe.go"),
		},
	}
	exceptions := []shared.PolicyException{
		{
			Rule:    "security/no-unsafe",
			Package: "example.com/service/internal/bridge",
			Path:    "internal/bridge/abi.go",
			Reason:  "reviewed ABI bridge",
			Expires: "2027-01-31",
		},
		{
			Rule:    "security/no-unsafe",
			Package: "example.com/service/internal/other",
			Reason:  "reviewed package bridge",
		},
	}
	remaining, inventory, err := shared.ApplyPolicyExceptions(
		root,
		diagnostics,
		exceptions,
		time.Date(2027, 1, 31, 23, 59, 0, 0, time.FixedZone("local", 7200)),
	)
	if err != nil {
		t.Fatalf("ApplyPolicyExceptions() error = %v", err)
	}
	if len(remaining) != 0 || len(inventory) != 2 ||
		!inventory[0].Used || !inventory[1].Used {
		t.Fatalf("ApplyPolicyExceptions() = %#v, %#v", remaining, inventory)
	}
	if exceptions[0].Used || diagnostics[0].Filename == "internal/bridge/abi.go" {
		t.Fatal("ApplyPolicyExceptions() mutated its input")
	}
}

func TestApplyPolicyExceptionsWithoutConfigurationIsNoOp(t *testing.T) {
	t.Parallel()

	diagnostics := []shared.Diagnostic{{Rule: "security/no-unsafe"}}
	remaining, inventory, err := shared.ApplyPolicyExceptions(
		"",
		diagnostics,
		nil,
		time.Time{},
	)
	if err != nil || len(remaining) != 1 || inventory != nil {
		t.Fatalf("ApplyPolicyExceptions() = %#v, %#v, %v",
			remaining, inventory, err)
	}
}

func TestApplyPolicyExceptionsRejectsUnmatchedAndExpiredEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	diagnostic := shared.Diagnostic{
		Rule:     "security/no-unsafe",
		Package:  "example.com/service/internal/bridge",
		Filename: filepath.Join(root, "internal", "bridge", "abi.go"),
	}
	tests := map[string]struct {
		diagnostics []shared.Diagnostic
		exception   shared.PolicyException
		want        string
	}{
		"rule": {
			diagnostics: []shared.Diagnostic{diagnostic},
			exception: shared.PolicyException{
				Rule: "context/no-background", Package: diagnostic.Package,
			},
			want: "stale policy exception",
		},
		"package": {
			diagnostics: []shared.Diagnostic{diagnostic},
			exception: shared.PolicyException{
				Rule: diagnostic.Rule, Package: "example.com/service/internal/other",
			},
			want: "stale policy exception",
		},
		"path": {
			diagnostics: []shared.Diagnostic{diagnostic},
			exception: shared.PolicyException{
				Rule: diagnostic.Rule, Package: diagnostic.Package, Path: "other.go",
			},
			want: "stale policy exception",
		},
		"empty diagnostics": {
			exception: shared.PolicyException{
				Rule: diagnostic.Rule, Package: diagnostic.Package,
			},
			want: "stale policy exception",
		},
		"expired": {
			diagnostics: []shared.Diagnostic{diagnostic},
			exception: shared.PolicyException{
				Rule: diagnostic.Rule, Package: diagnostic.Package, Expires: "2026-07-15",
			},
			want: "expired on 2026-07-15",
		},
		"malformed expiry": {
			diagnostics: []shared.Diagnostic{diagnostic},
			exception: shared.PolicyException{
				Rule: diagnostic.Rule, Package: diagnostic.Package, Expires: "tomorrow",
			},
			want: "parse policy exception expiry",
		},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := shared.ApplyPolicyExceptions(
				root,
				test.diagnostics,
				[]shared.PolicyException{test.exception},
				time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC),
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ApplyPolicyExceptions() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestApplyPolicyExceptionsRejectsDiagnosticOutsideRoot(t *testing.T) {
	t.Parallel()

	_, _, err := shared.ApplyPolicyExceptions(
		t.TempDir(),
		[]shared.Diagnostic{{
			Rule: "security/no-unsafe", Package: "example.com/service",
			Filename: filepath.Join(t.TempDir(), "unsafe.go"),
		}},
		[]shared.PolicyException{{
			Rule: "security/no-unsafe", Package: "example.com/service",
		}},
		time.Time{},
	)
	if err == nil || !strings.Contains(err.Error(), "escapes the report root") {
		t.Fatalf("ApplyPolicyExceptions() error = %v", err)
	}
}
