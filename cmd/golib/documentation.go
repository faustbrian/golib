package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var requiredRepositoryDocumentation = []string{
	"docs/api-protocols.md",
	"docs/architecture.md",
	"docs/choosing-packages.md",
	"docs/comparisons/index.md",
	"docs/index.md",
	"docs/integration-map.md",
	"docs/limitations.md",
	"docs/migration/index.md",
	"docs/migration/laravel.md",
	"docs/migration/standalone.md",
	"docs/operations/index.md",
	"docs/packages.md",
	"docs/recipes/durable-worker.md",
	"docs/recipes/external-integration.md",
	"docs/recipes/index.md",
	"docs/recipes/service.md",
	"docs/recommended-stacks.md",
}

var repositoryDocumentationIndexEntries = []string{
	"docs/comparisons/index.md",
	"docs/limitations.md",
	"docs/migration/index.md",
	"docs/operations/index.md",
	"docs/recipes/index.md",
}

func documentation(root string) {
	if err := validateRepositoryDocumentation(root); err != nil {
		fatal("validate repository documentation: %v", err)
	}
	fmt.Printf("validated repository documentation\n")
}

func validateRepositoryDocumentation(root string) error {
	for _, path := range requiredRepositoryDocumentation {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("required documentation page is missing: %s", path)
			}
			return fmt.Errorf("stat required documentation page %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("required documentation page is not a regular file: %s", path)
		}
	}

	indexPath := filepath.Join(root, "docs", "index.md")
	indexTargets, err := repositoryMarkdownTargets(root, indexPath)
	if err != nil {
		return err
	}
	for _, path := range repositoryDocumentationIndexEntries {
		if !indexTargets[filepath.Join(root, filepath.FromSlash(path))] {
			return fmt.Errorf(
				"required documentation page is not linked from docs/index.md: %s",
				path,
			)
		}
	}

	documents := []string{filepath.Join(root, "README.md")}
	err = filepath.WalkDir(filepath.Join(root, "docs"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		documents = append(documents, path)
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk repository documentation: %w", err)
	}
	slices.Sort(documents)
	documentSet := make(map[string]bool, len(documents))
	for _, path := range documents {
		documentSet[path] = true
	}
	links := make(map[string][]string, len(documents))
	for _, path := range documents {
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("read documentation %s: %w", relativeDocumentationPath(root, path), readErr)
		}
		if strings.Contains(string(contents), "/Users/") {
			return fmt.Errorf("%s contains a private absolute path", relativeDocumentationPath(root, path))
		}
		for _, match := range markdownLinkPattern.FindAllSubmatch(contents, -1) {
			target := string(match[1])
			if err := validateRepositoryDocumentationLink(root, path, target); err != nil {
				return fmt.Errorf("%s: %w", relativeDocumentationPath(root, path), err)
			}
			if resolved, ok := localMarkdownDocument(path, target); ok && documentSet[resolved] {
				links[path] = append(links[path], resolved)
			}
		}
	}
	if unreachable := unreachableDocumentation(filepath.Join(root, "README.md"), documents, links); len(unreachable) != 0 {
		relative := make([]string, 0, len(unreachable))
		for _, path := range unreachable {
			relative = append(relative, relativeDocumentationPath(root, path))
		}
		return fmt.Errorf(
			"documentation page is unreachable from README.md: %s",
			strings.Join(relative, ", "),
		)
	}

	return nil
}

func localMarkdownDocument(document, target string) (string, bool) {
	parsed, err := url.Parse(target)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path == "" {
		return "", false
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(document), filepath.FromSlash(parsed.Path)))
	extension := filepath.Ext(resolved)
	if !strings.EqualFold(extension, ".md") && !strings.EqualFold(extension, ".markdown") {
		return "", false
	}
	return resolved, true
}

func unreachableDocumentation(root string, documents []string, links map[string][]string) []string {
	if len(documents) == 0 {
		return nil
	}
	visited := map[string]bool{root: true}
	queue := []string{root}
	for len(queue) != 0 {
		current := queue[0]
		queue = queue[1:]
		for _, target := range links[current] {
			if visited[target] {
				continue
			}
			visited[target] = true
			queue = append(queue, target)
		}
	}
	unreachable := []string{}
	for _, path := range documents {
		if !visited[path] {
			unreachable = append(unreachable, path)
		}
	}
	return unreachable
}

func repositoryMarkdownTargets(root, path string) (map[string]bool, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read documentation index: %w", err)
	}
	targets := map[string]bool{}
	for _, match := range markdownLinkPattern.FindAllSubmatch(contents, -1) {
		parsed, parseErr := url.Parse(string(match[1]))
		if parseErr != nil || parsed.IsAbs() || parsed.Path == "" {
			continue
		}
		resolved := filepath.Clean(filepath.Join(filepath.Dir(path), filepath.FromSlash(parsed.Path)))
		if withinRepository(root, resolved) {
			targets[resolved] = true
		}
	}
	return targets, nil
}

func validateRepositoryDocumentationLink(root, document, target string) error {
	parsed, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("invalid link %q: %w", target, err)
	}
	if parsed.Scheme == "" && parsed.Host != "" {
		return fmt.Errorf("unsupported scheme-relative link %q", target)
	}
	if parsed.IsAbs() {
		if parsed.Scheme != "http" && parsed.Scheme != "https" && parsed.Scheme != "mailto" {
			return fmt.Errorf("unsupported absolute link %q", target)
		}
		return nil
	}
	if parsed.Path != "" {
		resolved := filepath.Clean(filepath.Join(filepath.Dir(document), filepath.FromSlash(parsed.Path)))
		if !withinRepository(root, resolved) {
			return fmt.Errorf("local link %q escapes the repository", target)
		}
	}
	return validateDecisionLink(document, target)
}

func withinRepository(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func relativeDocumentationPath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(relative)
}
