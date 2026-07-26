package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportedSymbolsHaveGoDocumentation(t *testing.T) {
	t.Parallel()

	set := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != "." && (strings.HasPrefix(entry.Name(), ".") ||
				entry.Name() == "examples" || entry.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(set, path, nil, parser.ParseComments)
		if parseErr != nil {
			return parseErr
		}
		for _, declaration := range file.Decls {
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				if declaration.Name.IsExported() && exportedReceiver(declaration) && declaration.Doc == nil {
					t.Errorf("%s: exported function %s has no Go documentation", path, declaration.Name)
				}
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						if spec.Name.IsExported() && declaration.Doc == nil && spec.Doc == nil {
							t.Errorf("%s: exported type %s has no Go documentation", path, spec.Name)
						}
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							if name.IsExported() && declaration.Doc == nil && spec.Doc == nil {
								t.Errorf("%s: exported value %s has no Go documentation", path, name)
							}
						}
					}
				}
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk Go source: %v", err)
	}
}

func exportedReceiver(function *ast.FuncDecl) bool {
	if function.Recv == nil {
		return true
	}
	receiver := function.Recv.List[0].Type
	if pointer, ok := receiver.(*ast.StarExpr); ok {
		receiver = pointer.X
	}
	identifier, ok := receiver.(*ast.Ident)

	return ok && identifier.IsExported()
}
