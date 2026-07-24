package knapsack_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProductionPackageDependencyDirection(t *testing.T) {
	t.Parallel()
	for _, rule := range []struct {
		directory string
		forbidden []string
	}{
		{".", []string{"/constraint", "/encoding", "/objective", "/solver", "/verify", "/visualize"}},
		{"geometry", []string{"github.com/faustbrian/golib/pkg/knapsack"}},
		{"verify", []string{"/solver"}},
		{"constraint", []string{"/encoding", "/objective", "/solver", "/verify", "/visualize"}},
		{"objective", []string{"/encoding", "/solver", "/verify", "/visualize"}},
	} {
		files, err := filepath.Glob(filepath.Join(rule.directory, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			parsed, parseErr := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			for _, imported := range parsed.Imports {
				path, unquoteErr := strconv.Unquote(imported.Path.Value)
				if unquoteErr != nil {
					t.Fatal(unquoteErr)
				}
				for _, forbidden := range rule.forbidden {
					if strings.Contains(path, forbidden) {
						t.Fatalf("%s imports forbidden dependency %s", file, path)
					}
				}
			}
		}
	}
}

func TestProductionContainsNoGoroutineLaunches(t *testing.T) {
	t.Parallel()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == "vendor" || entry.Name() == "testdata") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if _, ok := node.(*ast.GoStmt); ok {
				t.Errorf("%s launches an unmanaged goroutine", path)
			}
			return true
		})

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAuthoritativeCoreContainsNoFloatRandomnessOrClock(t *testing.T) {
	t.Parallel()
	for _, directory := range []string{".", "constraint", "geometry", "objective", "solver", "verify"} {
		files, err := filepath.Glob(filepath.Join(directory, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			parsed, parseErr := parser.ParseFile(token.NewFileSet(), file, nil, 0)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			for _, imported := range parsed.Imports {
				path, unquoteErr := strconv.Unquote(imported.Path.Value)
				if unquoteErr != nil {
					t.Fatal(unquoteErr)
				}
				if path == "math/rand" || path == "math/rand/v2" || path == "time" {
					t.Errorf("%s imports ambient nondeterminism %s", file, path)
				}
			}
			ast.Inspect(parsed, func(node ast.Node) bool {
				identifier, ok := node.(*ast.Ident)
				if ok && (identifier.Name == "float32" || identifier.Name == "float64") {
					t.Errorf("%s uses forbidden authoritative float %s", file, identifier.Name)
				}
				return true
			})
		}
	}
}
