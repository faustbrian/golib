package prompts_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestCoreDependencyAndRuntimeBoundaries(t *testing.T) {
	t.Parallel()

	root := packageRoot(t)
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("ReadFile(go.mod) error = %v", err)
	}
	content := string(module)
	declaration, _, _ := strings.Cut(content, "\n")
	if strings.TrimSuffix(declaration, "\r") != "module github.com/faustbrian/golib/pkg/prompts" {
		t.Fatalf("unexpected module declaration: %q", content)
	}
	for _, dependency := range []string{
		"charm.land/", "charmbracelet/", "AlecAivazis/survey", "manifoldco/promptui",
	} {
		if strings.Contains(content, dependency) {
			t.Fatalf("core module leaked interactive engine dependency %q", dependency)
		}
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), filepath.Join(root, name), nil, parser.SkipObjectResolution)
		if parseErr != nil {
			t.Fatalf("ParseFile(%s) error = %v", name, parseErr)
		}
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, `"`)
			if slices.Contains([]string{"os", "os/exec"}, path) || strings.Contains(path, "charm") ||
				strings.Contains(path, "survey") || strings.Contains(path, "promptui") {
				t.Errorf("%s imports forbidden core dependency %q", name, path)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch value := node.(type) {
			case *ast.GoStmt:
				t.Errorf("%s launches an unowned goroutine at %d", name, value.Pos())
			case *ast.FuncDecl:
				if value.Name.Name == "init" {
					t.Errorf("%s defines package initialization side effects", name)
				}
			case *ast.CallExpr:
				if identifier, ok := value.Fun.(*ast.Ident); ok && identifier.Name == "panic" {
					t.Errorf("%s contains a production panic at %d", name, value.Pos())
				}
			}
			return true
		})
	}
}

func TestSupportedPlatformBoundary(t *testing.T) {
	t.Parallel()

	root := packageRoot(t)
	terminalEntries, err := os.ReadDir(filepath.Join(root, "terminal"))
	if err != nil {
		t.Fatalf("ReadDir(terminal) error = %v", err)
	}
	for _, entry := range terminalEntries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), "_windows.go") {
			t.Errorf("unsupported Windows implementation remains: %s", entry.Name())
		}
	}
}

func packageRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() did not resolve the package root")
	}
	return filepath.Dir(file)
}
