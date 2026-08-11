package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type sourceDocumentationCatalog struct {
	SchemaVersion int                         `json:"schema_version"`
	Repository    string                      `json:"repository"`
	GoVersion     string                      `json:"go_version"`
	Modules       []sourceDocumentationModule `json:"modules"`
}

type sourceDocumentationModule struct {
	Directory                  string                     `json:"directory"`
	ModulePath                 string                     `json:"module_path"`
	Packages                   int                        `json:"packages"`
	DocumentedPackages         int                        `json:"documented_packages"`
	MissingPackageComments     int                        `json:"missing_package_comments"`
	DuplicatePackageComments   int                        `json:"duplicate_package_comments"`
	ExportedDeclarations       int                        `json:"exported_declarations"`
	DocumentedDeclarations     int                        `json:"documented_declarations"`
	MissingDeclarationComments int                        `json:"missing_declaration_comments"`
	MalformedComments          int                        `json:"malformed_comments"`
	GeneratedFiles             int                        `json:"generated_files"`
	GeneratedMissingComments   int                        `json:"generated_missing_comments"`
	Markers                    int                        `json:"markers"`
	Issues                     []sourceDocumentationIssue `json:"issues"`
}

type sourceDocumentationIssue struct {
	Code      string `json:"code"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Symbol    string `json:"symbol,omitempty"`
	Generated bool   `json:"generated,omitempty"`
	Message   string `json:"message"`
}

type sourcePackageDocumentation struct {
	Name     string
	Path     string
	Comments int
}

func discoverSourceDocumentationCatalog(root string, current catalog) (sourceDocumentationCatalog, error) {
	result := sourceDocumentationCatalog{
		SchemaVersion: 1,
		Repository:    canonicalRoot,
		GoVersion:     current.GoVersion,
		Modules:       make([]sourceDocumentationModule, 0, len(current.Modules)),
	}
	for _, item := range current.Modules {
		audit, err := auditModuleSourceDocumentation(root, item, current)
		if err != nil {
			return sourceDocumentationCatalog{}, fmt.Errorf("audit source documentation for %s: %w", item.Directory, err)
		}
		result.Modules = append(result.Modules, audit)
	}
	return result, nil
}

func auditModuleSourceDocumentation(root string, item module, current catalog) (sourceDocumentationModule, error) {
	result := sourceDocumentationModule{
		Directory:  item.Directory,
		ModulePath: item.Path,
		Issues:     []sourceDocumentationIssue{},
	}
	packages := map[string]*sourcePackageDocumentation{}
	moduleRoot := root
	if item.Directory != "." {
		moduleRoot = filepath.Join(root, filepath.FromSlash(item.Directory))
	}
	nestedModules := nestedModuleDirectories(item, current)

	err := filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "." {
			relative = ""
		}
		if entry.IsDir() {
			if relative == ".git" || relative == ".artifacts" || relative == "vendor" || relative == "testdata" || strings.Contains(relative, "/.artifacts") || strings.Contains(relative, "/vendor/") || strings.Contains(relative, "/testdata/") {
				return filepath.SkipDir
			}
			if relative != item.Directory && nestedModules[relative] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		return auditSourceFile(path, relative, packages, &result)
	})
	if err != nil {
		return sourceDocumentationModule{}, err
	}

	packageKeys := make([]string, 0, len(packages))
	for key := range packages {
		packageKeys = append(packageKeys, key)
	}
	sort.Strings(packageKeys)
	result.Packages = len(packageKeys)
	for _, key := range packageKeys {
		pack := packages[key]
		switch pack.Comments {
		case 0:
			result.MissingPackageComments++
			result.Issues = append(result.Issues, sourceDocumentationIssue{
				Code:    "missing-package-comment",
				Path:    pack.Path,
				Line:    1,
				Symbol:  pack.Name,
				Message: "package has no canonical Package " + pack.Name + " comment",
			})
		case 1:
			result.DocumentedPackages++
		default:
			result.DuplicatePackageComments++
			result.Issues = append(result.Issues, sourceDocumentationIssue{
				Code:    "duplicate-package-comment",
				Path:    pack.Path,
				Line:    1,
				Symbol:  pack.Name,
				Message: "package has more than one canonical package comment",
			})
		}
	}
	sort.Slice(result.Issues, func(left, right int) bool {
		if result.Issues[left].Path != result.Issues[right].Path {
			return result.Issues[left].Path < result.Issues[right].Path
		}
		if result.Issues[left].Line != result.Issues[right].Line {
			return result.Issues[left].Line < result.Issues[right].Line
		}
		if result.Issues[left].Code != result.Issues[right].Code {
			return result.Issues[left].Code < result.Issues[right].Code
		}
		return result.Issues[left].Symbol < result.Issues[right].Symbol
	})
	return result, nil
}

func auditSourceFile(path, relative string, packages map[string]*sourcePackageDocumentation, result *sourceDocumentationModule) error {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse %s: %w", relative, err)
	}
	generated := ast.IsGenerated(file)
	if generated {
		result.GeneratedFiles++
	}
	directory := filepath.ToSlash(filepath.Dir(relative))
	key := directory + "|" + file.Name.Name
	pack := packages[key]
	if pack == nil {
		pack = &sourcePackageDocumentation{Name: file.Name.Name, Path: relative}
		packages[key] = pack
	}
	if validPackageComment(file.Doc, file.Name.Name) {
		pack.Comments++
	}

	for _, group := range file.Comments {
		for _, comment := range group.List {
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(comment.Text, "//"), "/*"))
			if hasDocumentationMarker(text) {
				position := fileSet.Position(comment.Pos())
				result.Markers++
				result.Issues = append(result.Issues, sourceDocumentationIssue{
					Code:    "marker-review-required",
					Path:    relative,
					Line:    position.Line,
					Message: "TODO, FIXME, HACK, NOTE, or WARNING marker requires a documented disposition",
				})
			}
		}
	}

	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			auditExportedName(typed.Name, typed.Doc, true, relative, generated, fileSet, result)
		case *ast.GenDecl:
			auditGeneralDeclaration(typed, relative, generated, fileSet, result)
		}
	}
	return nil
}

func auditGeneralDeclaration(declaration *ast.GenDecl, path string, generated bool, fileSet *token.FileSet, result *sourceDocumentationModule) {
	for _, specification := range declaration.Specs {
		switch typed := specification.(type) {
		case *ast.TypeSpec:
			documentation := typed.Doc
			requireNamePrefix := documentation != nil
			if documentation == nil {
				documentation = declaration.Doc
				requireNamePrefix = len(declaration.Specs) == 1
			}
			auditExportedName(typed.Name, documentation, requireNamePrefix, path, generated, fileSet, result)
			auditTypeMembers(typed.Type, path, generated, fileSet, result)
		case *ast.ValueSpec:
			documentation := typed.Doc
			requireNamePrefix := documentation != nil && len(typed.Names) == 1
			if documentation == nil {
				documentation = typed.Comment
				requireNamePrefix = documentation != nil && len(typed.Names) == 1
			}
			if documentation == nil {
				documentation = declaration.Doc
				requireNamePrefix = false
			}
			for _, name := range typed.Names {
				auditExportedName(name, documentation, requireNamePrefix, path, generated, fileSet, result)
			}
		}
	}
}

func auditTypeMembers(expression ast.Expr, path string, generated bool, fileSet *token.FileSet, result *sourceDocumentationModule) {
	var fields *ast.FieldList
	switch typed := expression.(type) {
	case *ast.StructType:
		fields = typed.Fields
	case *ast.InterfaceType:
		fields = typed.Methods
	default:
		return
	}
	for _, field := range fields.List {
		documentation := field.Doc
		if documentation == nil {
			documentation = field.Comment
		}
		for _, name := range field.Names {
			auditExportedName(name, documentation, false, path, generated, fileSet, result)
		}
	}
}

func auditExportedName(name *ast.Ident, documentation *ast.CommentGroup, requireNamePrefix bool, path string, generated bool, fileSet *token.FileSet, result *sourceDocumentationModule) {
	if name == nil || !name.IsExported() {
		return
	}
	result.ExportedDeclarations++
	position := fileSet.Position(name.Pos())
	if documentation == nil || strings.TrimSpace(documentation.Text()) == "" {
		issue := sourceDocumentationIssue{
			Code:      "missing-exported-comment",
			Path:      path,
			Line:      position.Line,
			Symbol:    name.Name,
			Generated: generated,
			Message:   "exported declaration has no documentation comment",
		}
		if generated {
			issue.Code = "generated-missing-exported-comment"
			issue.Message = "generated exported declaration lacks documentation and must be fixed through its generator"
			result.GeneratedMissingComments++
		} else {
			result.MissingDeclarationComments++
		}
		result.Issues = append(result.Issues, issue)
		return
	}
	if requireNamePrefix && !commentStartsWithName(documentation.Text(), name.Name) {
		result.MalformedComments++
		result.Issues = append(result.Issues, sourceDocumentationIssue{
			Code:      "malformed-exported-comment",
			Path:      path,
			Line:      position.Line,
			Symbol:    name.Name,
			Generated: generated,
			Message:   "exported declaration comment does not start with the declaration name",
		})
		return
	}
	result.DocumentedDeclarations++
}

func validPackageComment(documentation *ast.CommentGroup, packageName string) bool {
	return documentation != nil && strings.HasPrefix(strings.TrimSpace(documentation.Text()), "Package "+packageName)
}

func commentStartsWithName(documentation, name string) bool {
	text := strings.TrimSpace(documentation)
	return strings.HasPrefix(text, name+" ") || strings.HasPrefix(text, name+".") || strings.HasPrefix(text, name+",") || strings.HasPrefix(text, name+" (")
}

func hasDocumentationMarker(text string) bool {
	for _, marker := range []string{"TODO:", "FIXME:", "HACK:", "NOTE:", "WARNING:"} {
		if strings.HasPrefix(text, marker) {
			return true
		}
	}
	return false
}

func sourceDocumentationReport(current sourceDocumentationCatalog) string {
	var output strings.Builder
	output.WriteString("# Source Documentation Audit\n\n")
	output.WriteString("Generated by `go run ./cmd/golib manifest`; do not edit manually.\n\n")
	output.WriteString("Counts are objective AST findings, not prose-quality scores. Generated-source gaps must be fixed through their generators. Internal rationale, stale claims, and contract accuracy still require technical review.\n\n")
	output.WriteString("| Module | Packages | Package gaps | Exported declarations | Missing comments | Malformed comments | Generated gaps | Markers |\n")
	output.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, item := range current.Modules {
		fmt.Fprintf(
			&output,
			"| `%s` | %d | %d | %d | %d | %d | %d | %d |\n",
			item.Directory,
			item.Packages,
			item.MissingPackageComments+item.DuplicatePackageComments,
			item.ExportedDeclarations,
			item.MissingDeclarationComments,
			item.MalformedComments,
			item.GeneratedMissingComments,
			item.Markers,
		)
	}
	return output.String()
}

func writeSourceDocumentationCatalog(root string, current catalog) {
	documentation, err := discoverSourceDocumentationCatalog(root, current)
	if err != nil {
		fatal("discover source documentation catalog: %v", err)
	}
	writeJSON(filepath.Join(root, "code-documentation.json"), documentation)
	writeText(filepath.Join(root, "docs", "source-documentation-audit.md"), sourceDocumentationReport(documentation))
}

func validateSourceDocumentationCatalog(root string, current catalog) {
	wanted, err := discoverSourceDocumentationCatalog(root, current)
	if err != nil {
		fatal("discover source documentation catalog: %v", err)
	}
	actual := sourceDocumentationCatalog{}
	readJSON(filepath.Join(root, "code-documentation.json"), &actual)
	if !equalJSON(actual, wanted) {
		fatal("code-documentation.json is stale; run `make manifests`")
	}
	wantedReport := sourceDocumentationReport(wanted)
	actualReport, err := os.ReadFile(filepath.Join(root, "docs", "source-documentation-audit.md"))
	if err != nil {
		fatal("read generated documentation docs/source-documentation-audit.md: %v", err)
	}
	if string(actualReport) != wantedReport {
		fatal("docs/source-documentation-audit.md is stale; run `make manifests`")
	}
}
