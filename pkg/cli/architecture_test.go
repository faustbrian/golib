package cli_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestArchitectureKeepsEngineAndRuntimeDetailsInternal(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == "benchmarks" || path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(name, "github.com/spf13/") && !strings.HasPrefix(path, "internal/engine/") {
				t.Errorf("%s exposes the replaceable Cobra engine boundary", path)
			}
			if name == "unsafe" || name == "C" || name == "os/exec" || name == "reflect" {
				t.Errorf("%s imports forbidden runtime facility %q", path, name)
			}
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && function.Name.Name == "init" {
				t.Errorf("%s contains package-global init behavior", path)
			}
		}
		for _, comment := range file.Comments {
			if strings.Contains(comment.Text(), "go:linkname") {
				t.Errorf("%s contains forbidden go:linkname directive", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
