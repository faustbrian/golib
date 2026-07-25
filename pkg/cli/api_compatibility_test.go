package cli_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestPublicAPICompatibility(t *testing.T) {
	t.Parallel()

	actual := publicAPI(t)
	const path = "api/public.txt"

	if os.Getenv("UPDATE_API") == "1" {
		// #nosec G301 -- generated public API artifacts must be readable.
		if err := os.MkdirAll("api", 0o755); err != nil {
			t.Fatal(err)
		}
		// #nosec G306 -- generated public API artifacts must be readable.
		if err := os.WriteFile(path, []byte(strings.Join(actual, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	expectedBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.Split(strings.TrimSpace(string(expectedBytes)), "\n")
	if !slices.Equal(actual, expected) {
		t.Fatalf("public API drifted; review compatibility and run UPDATE_API=1 go test . -run TestPublicAPICompatibility\nexpected:\n%s\nactual:\n%s", strings.Join(expected, "\n"), strings.Join(actual, "\n"))
	}
}

func publicAPI(t *testing.T) []string {
	t.Helper()

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var declarations []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, entry.Name(), nil, 0)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		for _, declaration := range file.Decls {
			declarations = append(declarations, exportedDeclarations(t, fset, declaration)...)
		}
	}
	slices.Sort(declarations)
	return declarations
}

func exportedDeclarations(t *testing.T, fset *token.FileSet, declaration ast.Decl) []string {
	t.Helper()

	switch value := declaration.(type) {
	case *ast.FuncDecl:
		if !value.Name.IsExported() || (value.Recv != nil && !receiverExported(value.Recv)) {
			return nil
		}
		copy := *value
		copy.Body = nil
		return []string{renderNode(t, fset, &copy)}
	case *ast.GenDecl:
		var result []string
		for _, specification := range value.Specs {
			result = append(result, exportedSpecification(t, fset, value.Tok, specification)...)
		}
		return result
	default:
		return nil
	}
}

func exportedSpecification(t *testing.T, fset *token.FileSet, kind token.Token, specification ast.Spec) []string {
	t.Helper()

	switch value := specification.(type) {
	case *ast.TypeSpec:
		if !value.Name.IsExported() {
			return nil
		}
		copy := *value
		if structure, ok := value.Type.(*ast.StructType); ok {
			structureCopy := *structure
			fieldsCopy := *structure.Fields
			fieldsCopy.List = exportedFields(structure.Fields.List)
			structureCopy.Fields = &fieldsCopy
			copy.Type = &structureCopy
		}
		return []string{renderDeclaration(t, fset, kind, &copy)}
	case *ast.ValueSpec:
		var result []string
		for index, name := range value.Names {
			if !name.IsExported() {
				continue
			}
			copy := *value
			copy.Names = []*ast.Ident{name}
			if len(value.Values) == len(value.Names) {
				copy.Values = []ast.Expr{value.Values[index]}
			}
			result = append(result, renderDeclaration(t, fset, kind, &copy))
		}
		return result
	default:
		return nil
	}
}

func exportedFields(fields []*ast.Field) []*ast.Field {
	var result []*ast.Field
	for _, field := range fields {
		if len(field.Names) == 0 {
			result = append(result, field)
			continue
		}
		copy := *field
		copy.Names = nil
		for _, name := range field.Names {
			if name.IsExported() {
				copy.Names = append(copy.Names, name)
			}
		}
		if len(copy.Names) > 0 {
			result = append(result, &copy)
		}
	}
	return result
}

func receiverExported(receiver *ast.FieldList) bool {
	if receiver == nil || len(receiver.List) != 1 {
		return false
	}
	expression := receiver.List[0].Type
	for {
		switch value := expression.(type) {
		case *ast.StarExpr:
			expression = value.X
		case *ast.IndexExpr:
			expression = value.X
		case *ast.IndexListExpr:
			expression = value.X
		case *ast.Ident:
			return value.IsExported()
		default:
			return false
		}
	}
}

func renderDeclaration(t *testing.T, fset *token.FileSet, kind token.Token, specification ast.Spec) string {
	t.Helper()
	return renderNode(t, fset, &ast.GenDecl{Tok: kind, Specs: []ast.Spec{specification}})
}

func renderNode(t *testing.T, fset *token.FileSet, node any) string {
	t.Helper()
	var output bytes.Buffer
	if err := format.Node(&output, fset, node); err != nil {
		t.Fatal(fmt.Errorf("format public declaration: %w", err))
	}
	return strings.ReplaceAll(output.String(), "\n", " ")
}
