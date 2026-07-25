package analysistestkit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEveryAnalyzerHasCrossBuildFixtureEvidence(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() did not locate fixture test")
	}
	analyzersRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "analyzers"))
	entries, err := os.ReadDir(analyzersRoot)
	if err != nil {
		t.Fatalf("ReadDir(analyzers) error = %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || !hasProductionGo(filepath.Join(analyzersRoot, entry.Name())) {
			continue
		}
		entry := entry
		t.Run(entry.Name(), func(t *testing.T) {
			t.Parallel()
			verifyFixtureDimensions(t, filepath.Join(analyzersRoot, entry.Name(), "testdata"))
		})
	}
}

func TestEveryAnalyzerHasSemanticNearMissFixture(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() did not locate fixture test")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), ".."))
	repositoryRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("OpenRoot(repository) error = %v", err)
	}
	t.Cleanup(func() {
		if err := repositoryRoot.Close(); err != nil {
			t.Errorf("Close(repository root) error = %v", err)
		}
	})
	contents, err := repositoryRoot.ReadFile("analysistestkit/precision.tsv")
	if err != nil {
		t.Fatalf("ReadFile(precision manifest) error = %v", err)
	}
	expected := make(map[string]struct{})
	for _, analyzer := range configuredAnalyzers(t) {
		expected[analyzer.Name] = struct{}{}
	}
	seen := make(map[string]struct{})
	for lineNumber, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 {
			t.Fatalf("precision manifest line %d requires two fields", lineNumber+1)
		}
		name, relative := fields[0], fields[1]
		if _, exists := seen[name]; exists {
			t.Fatalf("precision manifest duplicates %s", name)
		}
		if _, exists := expected[name]; !exists {
			t.Errorf("precision manifest contains unknown analyzer %s", name)
			continue
		}
		seen[name] = struct{}{}
		verifySemanticNearMiss(t, repositoryRoot, relative)
	}
	for name := range expected {
		if _, exists := seen[name]; !exists {
			t.Errorf("precision manifest omits analyzer %s", name)
		}
	}
}

func TestMutationGateCoversDiagnosticDecisionPackages(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() did not locate mutation gate test")
	}
	path := filepath.Clean(filepath.Join(
		filepath.Dir(filename),
		"..",
		"scripts",
		"mutation.sh",
	))
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(mutation gate) error = %v", err)
	}
	for _, required := range []string{
		"run_mutation ./analysis",
		"run_mutation ./internal/driver",
		"run_mutation ./policy",
		"for package in ./analyzers/*",
	} {
		if !strings.Contains(string(contents), required) {
			t.Errorf("mutation gate does not contain %q", required)
		}
	}
}

func TestShippedAnalyzersDeclareNoFacts(t *testing.T) {
	t.Parallel()

	for _, analyzer := range configuredAnalyzers(t) {
		if len(analyzer.FactTypes) != 0 {
			t.Errorf("%s declares %d fact types", analyzer.Name, len(analyzer.FactTypes))
		}
		for _, required := range analyzer.Requires {
			if len(required.FactTypes) != 0 {
				t.Errorf(
					"%s requires %s with %d fact types",
					analyzer.Name,
					required.Name,
					len(required.FactTypes),
				)
			}
		}
	}
}

func hasProductionGo(directory string) bool {
	matches, err := filepath.Glob(filepath.Join(directory, "*.go"))
	if err != nil {
		return false
	}
	for _, match := range matches {
		if !strings.HasSuffix(match, "_test.go") {
			return true
		}
	}
	return false
}

func verifyFixtureDimensions(t *testing.T, testdata string) {
	t.Helper()

	root, err := os.OpenRoot(testdata)
	if err != nil {
		t.Fatalf("OpenRoot(testdata) error = %v", err)
	}
	t.Cleanup(func() {
		if err := root.Close(); err != nil {
			t.Errorf("Close(testdata root) error = %v", err)
		}
	})
	generatedDiagnostic := false
	buildTaggedDiagnostic := false
	packageDirectories := make(map[string]struct{})
	err = filepath.WalkDir(testdata, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		relative, err := filepath.Rel(testdata, path)
		if err != nil {
			return err
		}
		contents, err := root.ReadFile(relative)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, contents, parser.ParseComments)
		if err != nil {
			return err
		}
		packageDirectories[filepath.Dir(path)] = struct{}{}
		hasDiagnostic := strings.Contains(string(contents), "// want `")
		if ast.IsGenerated(file) && hasDiagnostic {
			generatedDiagnostic = true
		}
		if strings.HasPrefix(string(contents), "//go:build ") && hasDiagnostic {
			buildTaggedDiagnostic = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(testdata) error = %v", err)
	}
	if !generatedDiagnostic {
		t.Error("missing a recognized generated fixture with an expected diagnostic")
	}
	if !buildTaggedDiagnostic {
		t.Error("missing a build-tagged fixture with an expected diagnostic")
	}
	if len(packageDirectories) < 2 {
		t.Errorf("fixture corpus spans %d package directories, want at least 2", len(packageDirectories))
	}
}

func verifySemanticNearMiss(t *testing.T, root *os.Root, path string) {
	t.Helper()

	contents, err := root.ReadFile(path)
	if err != nil {
		t.Errorf("ReadFile(%s) error = %v", path, err)
		return
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, contents, parser.ParseComments)
	if err != nil {
		t.Errorf("ParseFile(%s) error = %v", path, err)
		return
	}
	if strings.Contains(string(contents), "// want `") {
		t.Errorf("%s is not a diagnostic-free near miss", path)
	}
	features := map[string]bool{
		"alias":     false,
		"closure":   false,
		"embedding": false,
		"generics":  false,
		"interface": false,
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.TypeSpec:
			features["alias"] = features["alias"] || node.Assign.IsValid()
			features["generics"] = features["generics"] || node.TypeParams != nil
		case *ast.Field:
			features["embedding"] = features["embedding"] || len(node.Names) == 0
		case *ast.InterfaceType:
			features["interface"] = true
		case *ast.FuncLit:
			features["closure"] = true
		}
		return true
	})
	for feature, present := range features {
		if !present {
			t.Errorf("%s lacks %s evidence", path, feature)
		}
	}
}
