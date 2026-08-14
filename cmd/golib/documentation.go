package main

import (
	"encoding/json"
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

const repositoryDocumentationPortal = "https://github.com/faustbrian/golib/blob/main/docs/index.md"

var repositoryDocumentationBacklinkExemptions = map[string]bool{
	"pkg/event-sourcing/adapters/gokafka": true,
	"pkg/kafka":                           true,
	"pkg/kafka/adapters/gotelemetry":      true,
	"pkg/kafka/adapters/mskiam":           true,
	"pkg/kafka/kafkaservice":              true,
	"pkg/outbox/adapters/gokafka":         true,
	"pkg/verkle-tree":                     true,
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
	if err := validatePackageDocumentationBacklinks(root); err != nil {
		return err
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
		if err := validateRepositoryMarkdownStyle(relativeDocumentationPath(root, path), contents); err != nil {
			return err
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

func validateRepositoryMarkdownStyle(path string, contents []byte) error {
	lines := strings.Split(string(contents), "\n")
	firstContent := ""
	headingCount := 0
	previousHeadingLevel := 0
	fenceCharacter := byte(0)
	fenceLength := 0
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if firstContent == "" && trimmed != "" {
			firstContent = trimmed
		}
		if marker, length, closing := markdownFence(trimmed, fenceCharacter, fenceLength); marker != 0 {
			if closing {
				fenceCharacter = 0
				fenceLength = 0
			} else if fenceCharacter == 0 {
				fenceCharacter = marker
				fenceLength = length
			}
			continue
		}
		if fenceCharacter != 0 {
			continue
		}
		level := markdownHeadingLevel(line)
		if level == 0 {
			continue
		}
		if level == 1 {
			headingCount++
		}
		if previousHeadingLevel != 0 && level > previousHeadingLevel+1 {
			return fmt.Errorf(
				"%s:%d heading level jumps from %d to %d",
				path,
				index+1,
				previousHeadingLevel,
				level,
			)
		}
		previousHeadingLevel = level
	}
	if firstContent == "" || !strings.HasPrefix(firstContent, "# ") {
		return fmt.Errorf("%s must begin with one level-one heading", path)
	}
	if headingCount != 1 {
		return fmt.Errorf("%s has %d level-one headings; want exactly one", path, headingCount)
	}
	if fenceCharacter != 0 {
		return fmt.Errorf("%s has an unclosed fenced code block", path)
	}
	return nil
}

func markdownFence(line string, openCharacter byte, openLength int) (byte, int, bool) {
	if len(line) < 3 || (line[0] != '`' && line[0] != '~') {
		return 0, 0, false
	}
	character := line[0]
	length := 0
	for length < len(line) && line[length] == character {
		length++
	}
	if length < 3 {
		return 0, 0, false
	}
	if openCharacter == 0 {
		return character, length, false
	}
	if character != openCharacter || length < openLength || strings.TrimSpace(line[length:]) != "" {
		return 0, 0, false
	}
	return character, length, true
}

func markdownHeadingLevel(line string) int {
	level := 0
	for level < len(line) && level < 6 && line[level] == '#' {
		level++
	}
	if level == 0 || level >= len(line) || line[level] != ' ' {
		return 0
	}
	return level
}

func validatePackageDocumentationBacklinks(root string) error {
	contents, err := os.ReadFile(filepath.Join(root, "modules.json"))
	if err != nil {
		return fmt.Errorf("read module catalog for documentation backlinks: %w", err)
	}
	current := catalog{}
	if err := json.Unmarshal(contents, &current); err != nil {
		return fmt.Errorf("decode module catalog for documentation backlinks: %w", err)
	}
	portalPath := filepath.Join(root, "docs", "index.md")
	for _, item := range current.Modules {
		if !item.Releasable || repositoryDocumentationBacklinkExemptions[item.Directory] {
			continue
		}
		readmePath := filepath.Join(root, filepath.FromSlash(item.Directory), "README.md")
		readme, err := os.ReadFile(readmePath)
		if err != nil {
			return fmt.Errorf("read releasable module README %s: %w", relativeDocumentationPath(root, readmePath), err)
		}
		if !containsDocumentationPortalLink(readmePath, portalPath, readme) {
			return fmt.Errorf(
				"releasable module README does not link to the documentation portal: %s",
				relativeDocumentationPath(root, readmePath),
			)
		}
		if err := validatePreV1Changelog(root, item.Directory, item.Lifecycle); err != nil {
			return err
		}
		if err := validatePackageDocumentationURLs(root, item.Directory); err != nil {
			return err
		}
	}
	return nil
}

func validatePreV1Changelog(root, directory, lifecycle string) error {
	if lifecycle != "pre-v1" {
		return nil
	}
	path := filepath.Join(root, filepath.FromSlash(directory), "CHANGELOG.md")
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read pre-v1 module changelog %s: %w", relativeDocumentationPath(root, path), err)
	}
	var fenceCharacter byte
	fenceLength := 0
	for lineNumber, rawLine := range strings.Split(string(contents), "\n") {
		line := strings.TrimSpace(rawLine)
		character, length, closes := markdownFence(line, fenceCharacter, fenceLength)
		if character != 0 {
			if fenceCharacter == 0 {
				fenceCharacter, fenceLength = character, length
			} else if closes {
				fenceCharacter, fenceLength = 0, 0
			}
			continue
		}
		if fenceCharacter == 0 && strings.HasPrefix(line, "## [") && line != "## [Unreleased]" {
			return fmt.Errorf(
				"pre-v1 module changelog claims a released version at %s:%d: %s",
				relativeDocumentationPath(root, path),
				lineNumber+1,
				line,
			)
		}
	}
	return nil
}

func validatePackageDocumentationURLs(root, directory string) error {
	moduleRoot := filepath.Join(root, filepath.FromSlash(directory))
	paths, err := filepath.Glob(filepath.Join(moduleRoot, "*.md"))
	if err != nil {
		return fmt.Errorf("discover package documentation for %s: %w", directory, err)
	}
	docsRoot := filepath.Join(moduleRoot, "docs")
	if info, statErr := os.Stat(docsRoot); statErr == nil && info.IsDir() {
		if err := filepath.WalkDir(docsRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
				paths = append(paths, path)
			}
			return nil
		}); err != nil {
			return fmt.Errorf("walk package documentation for %s: %w", directory, err)
		}
	} else if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
		return fmt.Errorf("stat package documentation for %s: %w", directory, statErr)
	}
	slices.Sort(paths)
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read package documentation %s: %w", relativeDocumentationPath(root, path), err)
		}
		if strings.Contains(string(contents), "https://github.com/faustbrian/golib/pkg/") {
			return fmt.Errorf(
				"package documentation contains a noncanonical standalone-repository URL: %s",
				relativeDocumentationPath(root, path),
			)
		}
	}
	return nil
}

func containsDocumentationPortalLink(readmePath, portalPath string, contents []byte) bool {
	for _, match := range markdownLinkPattern.FindAllSubmatch(contents, -1) {
		target := string(match[1])
		parsed, err := url.Parse(target)
		if err != nil {
			continue
		}
		if parsed.IsAbs() {
			parsed.Fragment = ""
			if parsed.String() == repositoryDocumentationPortal {
				return true
			}
			continue
		}
		resolved := filepath.Clean(filepath.Join(filepath.Dir(readmePath), filepath.FromSlash(parsed.Path)))
		if resolved == portalPath {
			return true
		}
	}
	return false
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
		if strings.EqualFold(parsed.Host, "github.com") && strings.HasPrefix(parsed.Path, "/faustbrian/golib/pkg/") {
			return fmt.Errorf(
				"noncanonical repository source link %q; use a repository-relative link",
				target,
			)
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
