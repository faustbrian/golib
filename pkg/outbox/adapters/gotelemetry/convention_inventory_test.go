package gotelemetry_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/outbox"
)

const outboxImportPath = "github.com/faustbrian/golib/pkg/outbox"

func declaredOutboxConventions(t *testing.T) ([]outbox.Operation, []outbox.Outcome) {
	t.Helper()

	command := exec.Command("go", "list", "-m", "-json", outboxImportPath)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("locate outbox convention package: %v", err)
	}
	var listed struct {
		Dir string
	}
	if err := json.Unmarshal(output, &listed); err != nil {
		t.Fatalf("decode outbox package inventory: %v", err)
	}
	if listed.Dir == "" {
		t.Fatalf("outbox package inventory is incomplete: %#v", listed)
	}
	paths, err := filepath.Glob(filepath.Join(listed.Dir, "*.go"))
	if err != nil {
		t.Fatalf("inventory outbox package files: %v", err)
	}

	files := make([]*ast.File, 0, len(paths))
	fileSet := token.NewFileSet()
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("parse outbox package file %q: %v", path, err)
		}
		files = append(files, file)
	}
	var operations []outbox.Operation
	var outcomes []outbox.Outcome
	for _, file := range files {
		for _, declaration := range file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			for _, specification := range group.Specs {
				values, ok := specification.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, name := range values.Names {
					kind := conventionKind(name.Name, values.Type)
					if kind == "" {
						continue
					}
					if index >= len(values.Values) {
						t.Fatalf("outbox convention %s must have an explicit string value", name.Name)
					}
					literal, ok := values.Values[index].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						t.Fatalf("outbox convention %s must be a string literal", name.Name)
					}
					value, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Fatalf("decode outbox convention %s: %v", name.Name, err)
					}
					switch kind {
					case "Operation":
						operations = append(operations, outbox.Operation(value))
					case "Outcome":
						outcomes = append(outcomes, outbox.Outcome(value))
					}
				}
			}
		}
	}
	sort.Slice(operations, func(left, right int) bool { return operations[left] < operations[right] })
	sort.Slice(outcomes, func(left, right int) bool { return outcomes[left] < outcomes[right] })
	if len(operations) == 0 || len(outcomes) == 0 {
		t.Fatalf("outbox convention inventory is empty: operations=%v outcomes=%v", operations, outcomes)
	}

	return operations, outcomes
}

func conventionKind(name string, expression ast.Expr) string {
	if identifier, ok := expression.(*ast.Ident); ok &&
		(identifier.Name == "Operation" || identifier.Name == "Outcome") {
		return identifier.Name
	}
	if strings.HasPrefix(name, "Operation") {
		return "Operation"
	}
	if strings.HasPrefix(name, "Outcome") {
		return "Outcome"
	}

	return ""
}
