package mpt_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProductionPackagesOwnNoGoroutines(t *testing.T) {
	t.Parallel()

	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate module source")
	}
	moduleDirectory := filepath.Dir(sourceFile)
	fileSet := token.NewFileSet()
	err := filepath.WalkDir(
		moduleDirectory,
		func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				switch entry.Name() {
				case ".ai", "_interop", "benchmarks", "node_modules", "testdata":
					if path != moduleDirectory {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".go") ||
				strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			parsed, parseErr := parser.ParseFile(
				fileSet,
				path,
				nil,
				parser.SkipObjectResolution,
			)
			if parseErr != nil {
				return parseErr
			}
			ast.Inspect(parsed, func(node ast.Node) bool {
				statement, isGoStatement := node.(*ast.GoStmt)
				if isGoStatement {
					t.Errorf(
						"production goroutine at %s",
						fileSet.Position(statement.Go),
					)
				}
				return true
			})
			return nil
		},
	)
	if err != nil {
		t.Fatalf("inspect production sources: %v", err)
	}
}
