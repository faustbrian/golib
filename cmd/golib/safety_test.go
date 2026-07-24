package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanGoSafetyUsesSyntaxInsteadOfStringMatching(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSafetyFile(t, root, "safe.go", `package fixture

func classify(value string) {
	switch value {
	case "unsafe", "C":
	}
}
`)
	writeSafetyFile(t, root, "unsafe_test.go", "package fixture\n\nimport \"unsafe\"\n")
	writeSafetyFile(t, root, "testdata/unsafe.go", "package fixture\n\nimport \"unsafe\"\n")

	violations, err := scanGoSafety(root)
	if err != nil || len(violations) != 0 {
		t.Fatalf("scanGoSafety() = %v, %v", violations, err)
	}
}

func TestScanGoSafetyReportsForbiddenProductionFeatures(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		source string
		want   string
	}{
		"unsafe":   {source: "package fixture\n\nimport \"unsafe\"\n", want: `forbidden import "unsafe"`},
		"cgo":      {source: "package fixture\n\nimport \"C\"\n", want: `forbidden import "C"`},
		"linkname": {source: "package fixture\n\n//go:linkname local target\nfunc local()\n", want: "forbidden go:linkname directive"},
	}
	for name, test := range tests {
		test := test
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeSafetyFile(t, root, "violation.go", test.source)
			violations, err := scanGoSafety(root)
			if err != nil || len(violations) != 1 || !strings.Contains(violations[0], test.want) {
				t.Fatalf("scanGoSafety() = %v, %v; want %q", violations, err, test.want)
			}
		})
	}
}

func TestScanGoSafetyFailsClosedOnInvalidProductionSource(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSafetyFile(t, root, "invalid.go", "package")
	if _, err := scanGoSafety(root); err == nil {
		t.Fatal("scanGoSafety() accepted invalid Go source")
	}
}

func writeSafetyFile(t *testing.T, root string, name string, source string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
}
