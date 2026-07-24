// Package apiguard provides source-level assertions for public Go APIs.
package apiguard

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// NoPackageReferences fails when an exported declaration refers to a blocked
// imported package. blocked maps import paths to their declared package names.
func NoPackageReferences(t testing.TB, directory string, blocked map[string]string) {
	t.Helper()
	references, err := PackageReferences(directory, blocked)
	if err != nil {
		t.Fatalf("inspect public API: %v", err)
	}
	if len(references) != 0 {
		t.Fatalf("public API references blocked client types: %s", strings.Join(references, ", "))
	}
}

// PackageReferences returns source positions where exported declarations refer
// to blocked imported packages.
func PackageReferences(directory string, blocked map[string]string) ([]string, error) {
	files := token.NewFileSet()
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read package directory: %w", err)
	}
	var references []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(files, directory+string(os.PathSeparator)+entry.Name(), nil, 0)
		if parseErr != nil {
			return nil, fmt.Errorf("parse package file: %w", parseErr)
		}
		aliases := blockedAliases(file, blocked)
		for _, declaration := range file.Decls {
			for _, node := range exportedNodes(declaration) {
				ast.Inspect(node, func(candidate ast.Node) bool {
					selector, ok := candidate.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					identifier, ok := selector.X.(*ast.Ident)
					if ok && aliases[identifier.Name] {
						references = append(references, files.Position(selector.Pos()).String())
					}
					return true
				})
			}
		}
	}
	return references, nil
}

func blockedAliases(file *ast.File, blocked map[string]string) map[string]bool {
	aliases := make(map[string]bool)
	for _, imported := range file.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			continue
		}
		packageName, blockedPath := blocked[path]
		if !blockedPath {
			continue
		}
		if imported.Name != nil {
			if imported.Name.Name == "_" {
				continue
			}
			aliases[imported.Name.Name] = true
			continue
		}
		aliases[packageName] = true
	}
	return aliases
}

func exportedNodes(declaration ast.Decl) []ast.Node {
	switch typed := declaration.(type) {
	case *ast.FuncDecl:
		if exportedFunction(typed) {
			return []ast.Node{typed.Type}
		}
	case *ast.GenDecl:
		var nodes []ast.Node
		for _, specification := range typed.Specs {
			switch spec := specification.(type) {
			case *ast.TypeSpec:
				if spec.Name.IsExported() {
					nodes = append(nodes, exportedTypeNodes(spec.Type)...)
				}
			case *ast.ValueSpec:
				if exportedValue(spec.Names) {
					if spec.Type != nil {
						nodes = append(nodes, spec.Type)
					}
					for _, value := range spec.Values {
						nodes = append(nodes, value)
					}
				}
			}
		}
		return nodes
	}
	return nil
}

func exportedFunction(function *ast.FuncDecl) bool {
	if !function.Name.IsExported() {
		return false
	}
	if function.Recv == nil {
		return true
	}
	for _, field := range function.Recv.List {
		if identifier := receiverIdentifier(field.Type); identifier != nil {
			return identifier.IsExported()
		}
	}
	return false
}

func receiverIdentifier(expression ast.Expr) *ast.Ident {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed
	case *ast.StarExpr:
		return receiverIdentifier(typed.X)
	case *ast.IndexExpr:
		return receiverIdentifier(typed.X)
	case *ast.IndexListExpr:
		return receiverIdentifier(typed.X)
	default:
		return nil
	}
}

func exportedTypeNodes(expression ast.Expr) []ast.Node {
	structure, ok := expression.(*ast.StructType)
	if !ok {
		return []ast.Node{expression}
	}
	var nodes []ast.Node
	for _, field := range structure.Fields.List {
		if len(field.Names) == 0 || exportedValue(field.Names) {
			nodes = append(nodes, field.Type)
		}
	}
	return nodes
}

func exportedValue(names []*ast.Ident) bool {
	for _, name := range names {
		if name.IsExported() {
			return true
		}
	}
	return false
}
