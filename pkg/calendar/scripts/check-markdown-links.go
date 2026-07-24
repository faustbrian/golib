//go:build ignore

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var markdownLink = regexp.MustCompile(`\[[^]]*\]\(([^)]+)\)`)

func main() {
	if err := checkMarkdownLinks("."); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("documentation links resolve")
}

func checkMarkdownLinks(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".artifacts", ".git", "vendor":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}

		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		for _, match := range markdownLink.FindAllStringSubmatch(markdownProse(string(contents)), -1) {
			target := strings.TrimSpace(match[1])
			if isExternalLink(target) {
				continue
			}
			relative, _, _ := strings.Cut(target, "#")
			if relative == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(path), relative)); err != nil {
				return fmt.Errorf("broken relative link in %s: %s", path, target)
			}
		}
		return nil
	})
}

func markdownProse(contents string) string {
	var prose strings.Builder
	fenced := false
	for _, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if !fenced {
			prose.WriteString(line)
			prose.WriteByte('\n')
		}
	}
	return prose.String()
}

func isExternalLink(target string) bool {
	return strings.HasPrefix(target, "http://") ||
		strings.HasPrefix(target, "https://") ||
		strings.HasPrefix(target, "mailto:") ||
		strings.HasPrefix(target, "#")
}
