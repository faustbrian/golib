package wire_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPublicAPIReferenceInventoriesEveryExport(t *testing.T) {
	t.Parallel()

	documentation, err := os.ReadFile("docs/api.md")
	if err != nil {
		t.Fatal(err)
	}
	packages := map[string]string{
		"wire":        ".",
		"jsonwire":    "jsonwire",
		"xmlwire":     "xmlwire",
		"soap":        "soap",
		"yamlwire":    "yamlwire",
		"tomlwire":    "tomlwire",
		"msgpackwire": "msgpackwire",
		"cborwire":    "cborwire",
		"bsonwire":    "bsonwire",
	}
	for name, directory := range packages {
		t.Run(name, func(t *testing.T) {
			section := apiSection(t, string(documentation), name)
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			exported := map[string]struct{}{}
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
					continue
				}
				file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, entry.Name()), nil, 0)
				if err != nil {
					t.Fatal(err)
				}
				if file.Name.Name != name {
					t.Fatalf("%s belongs to package %s, want %s", entry.Name(), file.Name.Name, name)
				}
				for _, declaration := range file.Decls {
					switch typed := declaration.(type) {
					case *ast.FuncDecl:
						if typed.Name.IsExported() && receiverIsExported(typed) {
							exported[typed.Name.Name] = struct{}{}
						}
					case *ast.GenDecl:
						for _, specification := range typed.Specs {
							switch specification := specification.(type) {
							case *ast.ValueSpec:
								for _, identifier := range specification.Names {
									if identifier.IsExported() {
										exported[identifier.Name] = struct{}{}
									}
								}
							case *ast.TypeSpec:
								if !specification.Name.IsExported() {
									continue
								}
								exported[specification.Name.Name] = struct{}{}
								if structure, ok := specification.Type.(*ast.StructType); ok {
									for _, field := range structure.Fields.List {
										for _, identifier := range field.Names {
											if identifier.IsExported() {
												exported[identifier.Name] = struct{}{}
											}
										}
									}
								}
							}
						}
					}
				}
			}
			for identifier := range exported {
				if !documentedIdentifier(section, identifier) {
					t.Errorf("docs/api.md package %s does not inventory %s", name, identifier)
				}
			}
		})
	}
}

func receiverIsExported(function *ast.FuncDecl) bool {
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

func documentedIdentifier(section, identifier string) bool {
	codeSpans := strings.Split(section, "`")
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(identifier) + `\b`)
	for index := 1; index < len(codeSpans); index += 2 {
		if pattern.MatchString(codeSpans[index]) {
			return true
		}
	}
	return false
}

func apiSection(t *testing.T, documentation, name string) string {
	t.Helper()

	heading := "## Package `" + name + "`"
	start := strings.Index(documentation, heading)
	if start < 0 {
		t.Fatalf("docs/api.md has no %s heading", heading)
	}
	section := documentation[start:]
	if end := strings.Index(section[len(heading):], "\n## Package `"); end >= 0 {
		section = section[:len(heading)+end]
	}
	return section
}
