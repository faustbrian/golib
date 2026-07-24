//go:build ignore

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckMarkdownLinksAcceptsLocalExternalAndFencedTargets(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "docs", "target.md"), "# Target\n")
	writeTestFile(t, filepath.Join(root, "README.md"), `[local](docs/target.md#section)
[external](https://example.com)

`+"```markdown"+`
[fixture](missing.md)
`+"```"+`
`)

	if err := checkMarkdownLinks(root); err != nil {
		t.Fatalf("check valid documentation: %v", err)
	}
}

func TestCheckMarkdownLinksRejectsMissingRelativeTarget(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "README.md"), "[missing](docs/missing.md)\n")

	err := checkMarkdownLinks(root)
	if err == nil || !strings.Contains(err.Error(), "broken relative link") {
		t.Fatalf("missing target error = %v", err)
	}
}

func writeTestFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
