package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateRepositoryDocumentation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*testing.T, string)
		wantError string
	}{
		{name: "complete portal"},
		{
			name: "missing required page",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Remove(filepath.Join(root, "docs", "limitations.md")); err != nil {
					t.Fatal(err)
				}
			},
			wantError: "required documentation page is missing",
		},
		{
			name: "broken local link",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				appendDocumentation(t, filepath.Join(root, "docs", "index.md"), "\n[missing](missing.md)\n")
			},
			wantError: "local link \"missing.md\" is broken",
		},
		{
			name: "broken anchor",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				appendDocumentation(t, filepath.Join(root, "docs", "index.md"), "\n[missing anchor](limitations.md#absent)\n")
			},
			wantError: "markdown anchor \"absent\" does not exist",
		},
		{
			name: "private path",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				appendDocumentation(t, filepath.Join(root, "docs", "index.md"), "\n`/Users/example/private`\n")
			},
			wantError: "contains a private absolute path",
		},
		{
			name: "scheme relative link",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				appendDocumentation(t, filepath.Join(root, "docs", "index.md"), "\n[remote](//example.test/path)\n")
			},
			wantError: "unsupported scheme-relative link",
		},
		{
			name: "orphan page",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(root, "docs", "orphan.md"), "# Orphan\n")
			},
			wantError: "documentation page is unreachable from README.md",
		},
		{
			name: "unlinked required page",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				index := filepath.Join(root, "docs", "index.md")
				contents, err := os.ReadFile(index)
				if err != nil {
					t.Fatal(err)
				}
				updated := strings.ReplaceAll(string(contents), "[limitations.md](limitations.md)\n", "")
				if err := os.WriteFile(index, []byte(updated), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantError: "required documentation page is not linked from docs/index.md",
		},
		{
			name: "package without ecosystem backlink",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(t, filepath.Join(root, "pkg", "sample", "README.md"), "# Sample\n")
			},
			wantError: "releasable module README does not link to the documentation portal: pkg/sample/README.md",
		},
		{
			name: "heading level jump",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(
					t,
					filepath.Join(root, "docs", "limitations.md"),
					"# Limitations\n\n### Skipped level\n\n[Documentation](index.md)\n",
				)
			},
			wantError: "heading level jumps from 1 to 3",
		},
		{
			name: "unclosed code fence",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				appendDocumentation(t, filepath.Join(root, "docs", "limitations.md"), "\n```go\n")
			},
			wantError: "has an unclosed fenced code block",
		},
		{
			name: "missing top-level heading",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				writeFile(
					t,
					filepath.Join(root, "docs", "limitations.md"),
					"Limitations\n\n[Documentation](index.md)\n",
				)
			},
			wantError: "must begin with one level-one heading",
		},
		{
			name: "multiple top-level headings",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				appendDocumentation(t, filepath.Join(root, "docs", "limitations.md"), "\n# Duplicate\n")
			},
			wantError: "has 2 level-one headings; want exactly one",
		},
		{
			name: "headings inside code fences are ignored",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				appendDocumentation(
					t,
					filepath.Join(root, "docs", "limitations.md"),
					"\n~~~markdown\n# Example heading\n#### Example jump\n~~~\n",
				)
			},
		},
		{
			name: "noncanonical repository source link",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				appendDocumentation(
					t,
					filepath.Join(root, "docs", "index.md"),
					"\n[queue](https://github.com/faustbrian/golib/pkg/queue)\n",
				)
			},
			wantError: "noncanonical repository source link",
		},
		{
			name: "noncanonical package documentation link",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				appendDocumentation(
					t,
					filepath.Join(root, "pkg", "sample", "README.md"),
					"\n[workflow](https://github.com/faustbrian/golib/pkg/sample/actions/workflows/ci.yml)\n",
				)
			},
			wantError: "package documentation contains a noncanonical standalone-repository URL",
		},
		{
			name: "pre-v1 module claims a released changelog version",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				appendDocumentation(
					t,
					filepath.Join(root, "pkg", "sample", "CHANGELOG.md"),
					"\n## [1.0.0] - 2026-08-14\n",
				)
			},
			wantError: "pre-v1 module changelog claims a released version",
		},
		{
			name: "pre-v1 module claims an unbracketed released changelog version",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				appendDocumentation(
					t,
					filepath.Join(root, "pkg", "sample", "CHANGELOG.md"),
					"\n## 1.0.0 - 2026-08-14\n",
				)
			},
			wantError: "pre-v1 module changelog claims a released version",
		},
		{
			name: "pre-v1 module claims a v-prefixed changelog version",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				appendDocumentation(
					t,
					filepath.Join(root, "pkg", "sample", "CHANGELOG.md"),
					"\n## v1.0.0\n",
				)
			},
			wantError: "pre-v1 module changelog claims a released version",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := documentationFixture(t)
			if test.mutate != nil {
				test.mutate(t, root)
			}
			err := validateRepositoryDocumentation(root)
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("validate documentation: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func TestMarkdownCatalogTextPreservesOnlyPortableLinks(t *testing.T) {
	t.Parallel()

	input := "Uses [`queue`](../queue) and [`pgx`](https://github.com/jackc/pgx)."
	want := "Uses `queue` and [`pgx`](https://github.com/jackc/pgx)."
	if got := markdownCatalogText(input); got != want {
		t.Fatalf("markdown catalog text = %q, want %q", got, want)
	}
}

func documentationFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "# Repository\n\n[Documentation](docs/index.md)\n")
	links := make([]string, 0, len(requiredRepositoryDocumentation))
	for _, path := range requiredRepositoryDocumentation {
		target := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		backlink, err := filepath.Rel(filepath.Dir(target), filepath.Join(root, "docs", "index.md"))
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, target, "# "+filepath.Base(path)+"\n\n[Documentation]("+filepath.ToSlash(backlink)+")\n")
		links = append(links, "["+filepath.Base(path)+"]("+strings.TrimPrefix(path, "docs/")+")")
	}
	writeFile(t, filepath.Join(root, "docs", "index.md"), "# Documentation\n\n"+strings.Join(links, "\n")+"\n")
	if err := os.MkdirAll(filepath.Join(root, "pkg", "sample"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(
		t,
		filepath.Join(root, "pkg", "sample", "README.md"),
		"# Sample\n\n[Golib documentation portal](https://github.com/faustbrian/golib/blob/main/docs/index.md)\n",
	)
	writeFile(
		t,
		filepath.Join(root, "pkg", "sample", "CHANGELOG.md"),
		"# Changelog\n\n## [Unreleased]\n",
	)
	writeFile(
		t,
		filepath.Join(root, "modules.json"),
		`{"modules":[{"directory":"pkg/sample","lifecycle":"pre-v1","releasable":true}]}`+"\n",
	)
	return root
}

func appendDocumentation(t *testing.T, path, suffix string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if _, err := file.WriteString(suffix); err != nil {
		t.Fatal(err)
	}
}
