package main

import (
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func safety(root string, arguments []string) {
	flags := flag.NewFlagSet("safety", flag.ExitOnError)
	module := flags.String("module", "", "module directory to inspect")
	if err := flags.Parse(arguments); err != nil {
		fatal("parse safety arguments: %v", err)
	}
	if *module == "" {
		fatal("safety requires -module <directory>")
	}

	directory := *module
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(root, directory)
	}
	if _, err := os.Stat(filepath.Join(directory, "go.mod")); err != nil {
		fatal("module has no go.mod: %s", *module)
	}

	violations, err := scanGoSafety(directory)
	if err != nil {
		fatal("scan Go safety: %v", err)
	}
	if len(violations) != 0 {
		for _, violation := range violations {
			fmt.Fprintln(os.Stderr, violation)
		}
		fatal("GO-SAFETY-1 violation")
	}
	fmt.Printf("GO-SAFETY-1 passed for %s\n", *module)
}

func scanGoSafety(directory string) ([]string, error) {
	var violations []string
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != directory && excludedSafetyDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		for _, imported := range file.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return fmt.Errorf("parse import in %s: %w", path, err)
			}
			if name == "unsafe" || name == "C" {
				violations = append(violations, fmt.Sprintf(
					"%s:%d: forbidden import %q",
					path,
					fileSet.Position(imported.Pos()).Line,
					name,
				))
			}
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if strings.HasPrefix(strings.TrimSpace(comment.Text), "//go:linkname") {
					violations = append(violations, fmt.Sprintf(
						"%s:%d: forbidden go:linkname directive",
						path,
						fileSet.Position(comment.Pos()).Line,
					))
				}
			}
		}
		return nil
	})
	sort.Strings(violations)
	return violations, err
}

func excludedSafetyDirectory(name string) bool {
	return name == "vendor" || name == "testdata" || strings.HasPrefix(name, ".")
}
