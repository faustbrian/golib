//go:build ignore

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	var failures []string
	set := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != "." && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "scripts" || entry.Name() == "testdata" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(set, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			switch value := declaration.(type) {
			case *ast.FuncDecl:
				if value.Name.IsExported() {
					checkComment(set, path, value.Name, value.Doc, value.Name.Name, &failures)
				}
			case *ast.GenDecl:
				for _, specification := range value.Specs {
					inspectSpec(set, path, value, specification, &failures)
				}
			}
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sort.Strings(failures)
	for _, failure := range failures {
		fmt.Fprintln(os.Stderr, failure)
	}
	if len(failures) != 0 {
		os.Exit(1)
	}
}

func inspectSpec(set *token.FileSet, path string, declaration *ast.GenDecl, specification ast.Spec, failures *[]string) {
	switch value := specification.(type) {
	case *ast.TypeSpec:
		if value.Name.IsExported() {
			comment := value.Doc
			if comment == nil {
				comment = declaration.Doc
			}
			checkComment(set, path, value.Name, comment, value.Name.Name, failures)
			inspectFields(set, path, value.Type, failures)
		}
	case *ast.ValueSpec:
		for _, name := range value.Names {
			if !name.IsExported() {
				continue
			}
			comment := value.Doc
			if comment == nil {
				comment = declaration.Doc
			}
			checkComment(set, path, name, comment, name.Name, failures)
		}
	}
}

func inspectFields(set *token.FileSet, path string, expression ast.Expr, failures *[]string) {
	var fields *ast.FieldList
	switch value := expression.(type) {
	case *ast.StructType:
		fields = value.Fields
	case *ast.InterfaceType:
		fields = value.Methods
	}
	if fields == nil {
		return
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			if name.IsExported() {
				checkComment(set, path, name, field.Doc, name.Name, failures)
			}
		}
	}
}

func checkComment(set *token.FileSet, path string, node ast.Node, comment *ast.CommentGroup, name string, failures *[]string) {
	if comment != nil && strings.HasPrefix(strings.TrimSpace(comment.Text()), name) {
		return
	}
	position := set.Position(node.Pos())
	*failures = append(*failures, fmt.Sprintf("%s:%d: exported %s lacks a leading doc comment", path, position.Line, name))
}
