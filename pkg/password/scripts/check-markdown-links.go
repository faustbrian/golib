//go:build ignore

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var linkPattern = regexp.MustCompile(`\[[^]]+\]\(([^)]+)\)`)

func main() {
	var failures []string
	_ = filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != "." && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		scanner := bufio.NewScanner(file)
		line := 0
		for scanner.Scan() {
			line++
			for _, match := range linkPattern.FindAllStringSubmatch(scanner.Text(), -1) {
				target := strings.SplitN(match[1], "#", 2)[0]
				if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
					continue
				}
				resolved := filepath.Clean(filepath.Join(filepath.Dir(path), target))
				if _, err := os.Stat(resolved); err != nil {
					failures = append(failures, fmt.Sprintf("%s:%d: broken link %s", path, line, match[1]))
				}
			}
		}
		return scanner.Err()
	})
	sort.Strings(failures)
	for _, failure := range failures {
		fmt.Fprintln(os.Stderr, failure)
	}
	if len(failures) != 0 {
		os.Exit(1)
	}
}
