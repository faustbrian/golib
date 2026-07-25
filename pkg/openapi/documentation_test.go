package openapi_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var markdownLink = regexp.MustCompile(`\[[^]]+\]\(([^)]+)\)`)

func TestRepositoryDocumentationLocalLinksResolve(t *testing.T) {
	t.Parallel()

	paths := []string{"README.md", "CHANGELOG.md", "specification/README.md"}
	documents, err := filepath.Glob(filepath.Join("docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, documents...)
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range markdownLink.FindAllStringSubmatch(string(raw), -1) {
			target := match[1]
			if target == "" || strings.HasPrefix(target, "#") ||
				strings.HasPrefix(target, "https://") ||
				strings.HasPrefix(target, "http://") ||
				strings.HasPrefix(target, "mailto:") {
				continue
			}
			file, _, _ := strings.Cut(target, "#")
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), filepath.FromSlash(file))); err != nil {
				t.Errorf("%s links to %q: %v", path, target, err)
			}
		}
	}
}
