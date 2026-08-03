package verkletree_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestProductionSourceHasNoPackageInitOrOwnedGoroutines(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test source path")
	}
	moduleRoot := filepath.Dir(filename)
	violations := make([]sourceArchitectureViolation, 0)
	err := filepath.WalkDir(moduleRoot, func(
		path string,
		entry fs.DirEntry,
		walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() ||
			filepath.Ext(path) != ".go" ||
			strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fileSet := token.NewFileSet()
		file, err := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse production source %s: %w", path, err)
		}
		violations = append(
			violations,
			findSourceArchitectureViolations(fileSet, file)...,
		)

		return nil
	})
	if err != nil {
		t.Fatalf("scan production source: %v", err)
	}
	for _, violation := range violations {
		t.Errorf(
			"package-owned production source must not %s: %s",
			violation.rule,
			violation.position,
		)
	}
}

func TestSourceArchitectureViolationDetection(t *testing.T) {
	t.Parallel()

	const source = `package fixture

func init() {}

func launch() {
	go func() {}()
}
`
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(
		fileSet,
		"representative_violation.go",
		source,
		parser.SkipObjectResolution,
	)
	if err != nil {
		t.Fatalf("parse representative violation: %v", err)
	}

	violations := findSourceArchitectureViolations(fileSet, file)
	rules := make([]string, len(violations))
	for index := range violations {
		rules[index] = violations[index].rule
	}
	slices.Sort(rules)
	want := []string{"define package init functions", "start goroutines"}
	if !slices.Equal(rules, want) {
		t.Fatalf("detected rules = %q, want %q", rules, want)
	}
}

type sourceArchitectureViolation struct {
	rule     string
	position token.Position
}

func findSourceArchitectureViolations(
	fileSet *token.FileSet,
	file *ast.File,
) []sourceArchitectureViolation {
	violations := make([]sourceArchitectureViolation, 0)
	ast.Inspect(file, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.FuncDecl:
			if value.Recv == nil && value.Name.Name == "init" {
				violations = append(violations, sourceArchitectureViolation{
					rule:     "define package init functions",
					position: fileSet.Position(value.Pos()),
				})
			}
		case *ast.GoStmt:
			violations = append(violations, sourceArchitectureViolation{
				rule:     "start goroutines",
				position: fileSet.Position(value.Pos()),
			})
		}

		return true
	})

	return violations
}
