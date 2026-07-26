package jsonrpc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportedSymbolsHaveGoDocumentation(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(files, name, nil, parser.ParseComments)
		if err != nil {
			t.Errorf("parse %s: %v", name, err)
			continue
		}
		for _, declaration := range file.Decls {
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				if ast.IsExported(typed.Name.Name) && typed.Doc == nil {
					t.Errorf("%s: exported function %s has no Go documentation", name, typed.Name)
				}
			case *ast.GenDecl:
				for _, specification := range typed.Specs {
					specificationDocumented(t, name, typed.Doc != nil, specification)
				}
			}
		}
	}
}

func specificationDocumented(t *testing.T, filename string, groupDocumented bool, specification ast.Spec) {
	t.Helper()

	switch typed := specification.(type) {
	case *ast.TypeSpec:
		if ast.IsExported(typed.Name.Name) && !groupDocumented && typed.Doc == nil && typed.Comment == nil {
			t.Errorf("%s: exported type %s has no Go documentation", filename, typed.Name)
		}
	case *ast.ValueSpec:
		for _, name := range typed.Names {
			if ast.IsExported(name.Name) && !groupDocumented && typed.Doc == nil && typed.Comment == nil {
				t.Errorf("%s: exported value %s has no Go documentation", filename, name)
			}
		}
	}
}

func TestCoreDocumentationContract(t *testing.T) {
	t.Parallel()

	required := map[string][]string{
		"README.md": {
			"JSON-RPC 2.0",
			"go get github.com/faustbrian/golib/pkg/jsonrpc",
			"Package Guarantees",
		},
		"docs/quickstart.md":   {"Server", "Client", "Notification", "Batch"},
		"docs/architecture.md": {"Transport", "Protocol", "Dispatch", "Execution"},
		"docs/api.md": {
			"## Protocol",
			"## Server",
			"## Client",
			"## HTTP",
		},
		"docs/middleware.md":      {"Observability", "Authentication", "correlation"},
		"docs/cookbook.md":        {"Custom application errors", "Batch", "Custom transport"},
		"docs/adoption.md":        {"Inventory", "Shadow", "Rollout", "Rollback"},
		"docs/faq.md":             {"notification", "batch", "WebSocket"},
		"docs/troubleshooting.md": {"Parse error", "Invalid Request", "Method not found", "Invalid params"},
		"docs/compatibility.md":   {"Semantic Versioning", "Wire compatibility", "Stable releases"},
		"docs/releasing.md": {
			"Release checklist",
			"semantic version",
			"make release-patch",
			"make release-minor",
			"make release-major",
		},
		"CHANGELOG.md":            {"Unreleased", "Keep a Changelog"},
		"ROADMAP.md":              {"v1.0.0", "WebSocket", "OpenRPC"},
		"CONTRIBUTING.md":         {"Development Setup", "Pull Requests", "protocol change"},
		"SECURITY.md":             {"privately", "Supported Versions", "Response Process"},
		"CODE_OF_CONDUCT.md":      {"Contributor Covenant", "Enforcement"},
		"LICENSE":                 {"MIT License", "Permission is hereby granted"},
		"examples/server/main.go": {"NewHTTPHandler", "Register"},
		"examples/client/main.go": {"NewHTTPTransport", "Call"},
		"examples/e2e/main.go":    {"httptest.NewServer", "NewClient"},
	}

	for path, fragments := range required {
		path, fragments := path, fragments
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile(%q) error = %v", path, err)
			}
			for _, fragment := range fragments {
				if !strings.Contains(string(contents), fragment) {
					t.Errorf("%s does not contain %q", path, fragment)
				}
			}
		})
	}
}

func TestV1ReleaseDocumentationContract(t *testing.T) {
	t.Parallel()

	required := map[string][]string{
		"CHANGELOG.md": {
			"## [Unreleased]",
			"## [1.0.0] - 2026-07-14",
			"[Unreleased]: https://github.com/faustbrian/golib/pkg/jsonrpc/compare/v1.0.0...HEAD",
			"[1.0.0]: https://github.com/faustbrian/golib/pkg/jsonrpc/releases/tag/v1.0.0",
		},
		"README.md":             {"stable v1 API"},
		"SECURITY.md":           {"Before `v1.0.0`", "security fixes are applied"},
		"ROADMAP.md":            {"## Post-v1 roadmap"},
		"docs/compatibility.md": {"As of `v1.0.0`"},
		"docs/api.md":           {"Starting with `v1.0.0`"},
		"go.mod":                {"module github.com/faustbrian/golib/pkg/jsonrpc"},
	}

	for path, fragments := range required {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		for _, fragment := range fragments {
			if !strings.Contains(string(contents), fragment) {
				t.Errorf("%s does not contain %q", path, fragment)
			}
		}
	}

}

func TestSpecificationReferences(t *testing.T) {
	t.Parallel()

	required := map[string][]string{
		"doc.go": {"https://www.jsonrpc.org/specification"},
		"protocol.go": {
			"https://www.jsonrpc.org/specification#request_object",
			"https://www.jsonrpc.org/specification#notification",
			"https://www.jsonrpc.org/specification#parameter_structures",
			"https://www.jsonrpc.org/specification#response_object",
		},
		"error.go": {"https://www.jsonrpc.org/specification#error_object"},
		"server.go": {
			"https://www.jsonrpc.org/specification#notification",
			"https://www.jsonrpc.org/specification#batch",
		},
		"conformance_test.go": {"https://www.jsonrpc.org/specification#examples"},
	}

	for path, references := range required {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", path, err)
		}
		for _, reference := range references {
			if !strings.Contains(string(contents), reference) {
				t.Errorf("%s does not contain %q", path, reference)
			}
		}
	}
}
