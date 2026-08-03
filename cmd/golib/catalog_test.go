package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestClassifyPackage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		moduleKind  string
		directory   string
		packageName string
		wantKind    string
		production  bool
	}{
		{name: "public root", moduleKind: "public library", directory: ".", packageName: "clock", wantKind: "public", production: true},
		{name: "internal implementation", moduleKind: "public library", directory: "internal/strictjson", packageName: "strictjson", wantKind: "internal", production: true},
		{name: "public command", moduleKind: "public library", directory: "cmd/queue-control", packageName: "main", wantKind: "command", production: true},
		{name: "example", moduleKind: "public library", directory: "examples/service", packageName: "main", wantKind: "example", production: false},
		{name: "test helper", moduleKind: "public library", directory: "clocktest", packageName: "clocktest", wantKind: "test support", production: false},
		{name: "test utility", moduleKind: "public library", directory: "internal/testutil/apiguard", packageName: "apiguard", wantKind: "test support", production: false},
		{name: "conformance helper", moduleKind: "public library", directory: "lease/conformance", packageName: "conformance", wantKind: "test support", production: false},
		{name: "script tool", moduleKind: "public library", directory: "scripts", packageName: "main", wantKind: "tooling", production: false},
		{name: "internal generator", moduleKind: "public library", directory: "internal/cmd/unicodegen", packageName: "main", wantKind: "tooling", production: false},
		{name: "semver tool", moduleKind: "public library", directory: "internal/semver", packageName: "semver", wantKind: "tooling", production: false},
		{name: "mocks", moduleKind: "public library", directory: "mocks", packageName: "mocks", wantKind: "test support", production: false},
		{name: "benchmark harness", moduleKind: "benchmark harness", directory: "cmd/competitor", packageName: "main", wantKind: "harness", production: false},
		{name: "interoperability harness", moduleKind: "interoperability harness", directory: ".", packageName: "main", wantKind: "harness", production: false},
		{name: "fixture", moduleKind: "fixture", directory: "sample", packageName: "sample", wantKind: "fixture", production: false},
		{name: "root tool", moduleKind: "internal tool", directory: "cmd/golib", packageName: "main", wantKind: "command", production: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			kind, production := classifyPackage(test.moduleKind, test.directory, test.packageName)
			if kind != test.wantKind || production != test.production {
				t.Fatalf("classifyPackage() = (%q, %t), want (%q, %t)", kind, production, test.wantKind, test.production)
			}
		})
	}
}

func TestValidateOwnedCommandName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		moduleKind  string
		directory   string
		packageName string
		wantError   bool
	}{
		{name: "qualified tool", moduleKind: "public library", directory: "cmd/golib-analysis", packageName: "main"},
		{name: "domain command", moduleKind: "public library", directory: "cmd/queue-control", packageName: "main"},
		{name: "standalone repository prefix", moduleKind: "public library", directory: "cmd/go-analysis", packageName: "main", wantError: true},
		{name: "competitor harness", moduleKind: "benchmark harness", directory: "cmd/go-prompts", packageName: "main"},
		{name: "ordinary package", moduleKind: "public library", directory: "go-parser", packageName: "parser"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateOwnedCommandName(test.moduleKind, test.directory, test.packageName)
			if (err != nil) != test.wantError {
				t.Fatalf("validateOwnedCommandName() error = %v, wantError %t", err, test.wantError)
			}
		})
	}
}

func TestExecutableFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		source     string
		executable bool
	}{
		{name: "declarations only", source: "package sample\nconst Value = 1\ntype Item struct{}\n", executable: false},
		{name: "empty function", source: "package sample\nfunc Empty() {}\n", executable: false},
		{name: "function statement", source: "package sample\nfunc Value() int { return 1 }\n", executable: true},
		{name: "initializer closure", source: "package sample\nvar Value = func() int { return 1 }()\n", executable: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			file, err := parser.ParseFile(token.NewFileSet(), "sample.go", test.source, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("ParseFile() error = %v", err)
			}
			if got := executableFile(file); got != test.executable {
				t.Fatalf("executableFile() = %t, want %t", got, test.executable)
			}
		})
	}
}

func TestExcludedSourceDirectory(t *testing.T) {
	t.Parallel()
	for _, directory := range []string{
		".artifacts", ".git", ".tools", "node_modules", "testdata", "vendor", "_fixtures",
	} {
		if !excludedSourceDirectory(directory) {
			t.Errorf("excludedSourceDirectory(%q) = false", directory)
		}
	}
	for _, directory := range []string{"cmd", "docs", "internal", "pkg"} {
		if excludedSourceDirectory(directory) {
			t.Errorf("excludedSourceDirectory(%q) = true", directory)
		}
	}
}

func TestRequiredTestTags(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "unit_test.go"), "package sample\n")
	mustWriteFile(t, filepath.Join(root, "integration_test.go"), "//go:build integration\n\npackage sample\n")
	mustWriteFile(
		t,
		filepath.Join(root, "interoperability_test.go"),
		"//go:build interoperability\n\npackage sample\n",
	)
	mustWriteFile(t, filepath.Join(root, "testdata", "ignored_test.go"), "//go:build ignored\n\npackage sample\n")
	mustWriteFile(t, filepath.Join(root, "nested", "go.mod"), "module example.com/nested\n")
	mustWriteFile(t, filepath.Join(root, "nested", "nested_test.go"), "//go:build nested\n\npackage nested\n")

	tags, err := requiredTestTags(root, ".")
	if err != nil {
		t.Fatalf("requiredTestTags() error = %v", err)
	}
	if !slices.Equal(tags, []string{"integration"}) {
		t.Fatalf("requiredTestTags() = %v, want [integration]", tags)
	}
}

func TestRequiredBuildTags(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "runner.go"), "//go:build interop\n\npackage main\n")
	mustWriteFile(t, filepath.Join(root, "runner_test.go"), "//go:build integration\n\npackage main\n")
	mustWriteFile(t, filepath.Join(root, "testdata", "ignored.go"), "//go:build fixture\n\npackage fixture\n")
	mustWriteFile(t, filepath.Join(root, "nested", "go.mod"), "module example.com/nested\n")
	mustWriteFile(t, filepath.Join(root, "nested", "nested.go"), "//go:build nested\n\npackage nested\n")

	tags, err := requiredBuildTags(root, ".")
	if err != nil {
		t.Fatalf("requiredBuildTags() error = %v", err)
	}
	if !slices.Equal(tags, []string{"interop"}) {
		t.Fatalf("requiredBuildTags() = %v, want [interop]", tags)
	}
}

func TestRequiredBuildTagsRejectsCompoundCustomConstraints(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWriteFile(
		t,
		filepath.Join(root, "runner.go"),
		"//go:build interop && linux\n\npackage main\n",
	)

	if _, err := requiredBuildTags(root, "."); err == nil {
		t.Fatal("requiredBuildTags() accepted a compound custom constraint")
	}
}

func TestDiscoverPackagesTracksRequiredBuildVariants(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "default.go"), "package sample\nfunc Value() int { return 1 }\n")
	mustWriteFile(t, filepath.Join(root, "tagged", "runner.go"), "//go:build interop\n\npackage tagged\nfunc Run() {}\n")
	mustWriteFile(t, filepath.Join(root, "scripts", "tool.go"), "//go:build ignore\n\npackage main\nfunc main() {}\n")

	packages, err := discoverPackages(root, ".", "example.com/sample", "public library")
	if err != nil {
		t.Fatalf("discoverPackages() error = %v", err)
	}
	type buildPolicy struct {
		required bool
		tags     []string
	}
	got := map[string]buildPolicy{}
	for _, packageInfo := range packages {
		got[packageInfo.Directory] = buildPolicy{
			required: packageInfo.BuildRequired,
			tags:     packageInfo.BuildTags,
		}
	}
	want := map[string]buildPolicy{
		".":       {required: true, tags: []string{}},
		"tagged":  {required: true, tags: []string{"interop"}},
		"scripts": {required: false, tags: []string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("build policies = %#v, want %#v", got, want)
	}
}

func TestGoalEvidenceTracksRequirementsImplementationAndCanonicalGates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	goal := "pkg/sample/.ai/GOAL_HARDEN.md"
	goalContents := "# Hardening Goal\n\nVerify every hostile boundary.\n"
	mustWriteFile(t, filepath.Join(root, goal), goalContents)
	mustWriteFile(t, filepath.Join(root, "pkg/sample/README.md"), "# Sample\n")
	mustWriteFile(t, filepath.Join(root, "pkg/sample/CHANGELOG.md"), "# Changelog\n")
	mustWriteFile(
		t,
		filepath.Join(root, "pkg/sample/docs/security/findings.md"),
		"# Findings\n",
	)

	records, err := goalEvidenceFor(
		root,
		"pkg/sample",
		[]string{goal},
		[]string{"test", "mutation"},
	)
	if err != nil {
		t.Fatalf("goalEvidenceFor() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("goalEvidenceFor() records = %d, want 1", len(records))
	}
	digest := sha256.Sum256([]byte(goalContents))
	wantDigest := hex.EncodeToString(digest[:])
	record := records[0]
	if record.RequirementsSHA256 != wantDigest ||
		record.ImplementationStatus != "implemented-requires-fresh-verification" {
		t.Fatalf("goal evidence metadata = %+v", record)
	}
	wantEvidence := []string{
		"pkg/sample/CHANGELOG.md",
		"pkg/sample/README.md",
		"pkg/sample/docs/security/findings.md",
	}
	if !slices.Equal(record.ImplementationEvidence, wantEvidence) {
		t.Fatalf(
			"implementation evidence = %v, want %v",
			record.ImplementationEvidence,
			wantEvidence,
		)
	}
	if !slices.Equal(record.VerificationGates, []string{"test", "mutation"}) {
		t.Fatalf("verification gates = %v", record.VerificationGates)
	}
}

func TestCanonicalGatesRejectDuplicateAndEmptyContracts(t *testing.T) {
	t.Parallel()
	for name, contents := range map[string]string{
		"duplicate": "test\nmutation\ntest\n",
		"empty":     "\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			mustWriteFile(
				t,
				filepath.Join(root, "scripts/check-gates.txt"),
				contents,
			)
			if _, err := canonicalGates(root); err == nil {
				t.Fatalf("canonicalGates() accepted %s contract", name)
			}
		})
	}
}

func TestCatalogFilesystemInspectionFailsClosed(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := hasDefaultGoFiles(missing, "."); err == nil {
		t.Fatal("hasDefaultGoFiles() error = nil, want filesystem error")
	}
	if _, err := requiredTestTags(missing, "."); err == nil {
		t.Fatal("requiredTestTags() error = nil, want filesystem error")
	}
}

func TestValidateModuleLicense(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := validateModuleLicense(root, "pkg/public", true); err == nil {
		t.Fatal("validateModuleLicense() accepted a releasable module without a license")
	}
	if err := validateModuleLicense(root, "pkg/harness", false); err != nil {
		t.Fatalf("validateModuleLicense() rejected a non-releasable module: %v", err)
	}

	mustWriteFile(t, filepath.Join(root, "pkg", "public", "LICENSE"), "MIT License\n")
	if err := validateModuleLicense(root, "pkg/public", true); err != nil {
		t.Fatalf("validateModuleLicense() rejected a licensed module: %v", err)
	}
}

func TestCatalogForExplicitSelectionUsesRegisteredModules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "modules.json"), `{
  "schema_version": 1,
  "repository": "github.com/faustbrian/golib",
  "modules": [{
    "directory": "pkg/kafka",
    "module_path": "github.com/faustbrian/golib/pkg/kafka",
    "owned_dependencies": []
  }]
}
`)
	mustWriteFile(
		t,
		filepath.Join(root, "pkg", "unfinished", "go.mod"),
		"module github.com/faustbrian/golib/pkg/unfinished\n",
	)

	current, err := catalogForSelection(root, true)
	if err != nil {
		t.Fatalf("catalogForSelection() error = %v", err)
	}
	if len(current.Modules) != 1 ||
		current.Modules[0].Directory != "pkg/kafka" {
		t.Fatalf("catalogForSelection() modules = %#v", current.Modules)
	}
}

func TestModuleDirectoriesExcludeGeneratedCaches(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n")
	mustWriteFile(t, filepath.Join(root, "pkg", "live", "go.mod"), "module example.com/live\n")
	mustWriteFile(
		t,
		filepath.Join(root, ".artifacts", "cache", "go.mod"),
		"module example.com/cache\n",
	)
	mustWriteFile(
		t,
		filepath.Join(root, "pkg", "live", "testdata", "fixture", "go.mod"),
		"module example.com/fixture\n",
	)

	got, err := moduleDirectories(root)
	if err != nil {
		t.Fatalf("moduleDirectories() error = %v", err)
	}
	want := []string{".", "pkg/live", "pkg/live/testdata/fixture"}
	if !slices.Equal(got, want) {
		t.Fatalf("moduleDirectories() = %v, want %v", got, want)
	}
}

func TestInteroperabilityCatalogMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		directory string
		want      []string
	}{
		{directory: "pkg/ecma-regexp", want: []string{"Node.js", "Test262"}},
		{directory: "pkg/wsdl", want: []string{"Java", "Apache Woden"}},
		{directory: "pkg/xsd", want: []string{"Docker", "Eclipse Temurin 25 JAXP"}},
		{directory: "pkg/jsonrpc", want: []string{}},
	}
	for _, test := range tests {
		if got := interoperabilityTools(test.directory); !slices.Equal(
			got,
			test.want,
		) {
			t.Errorf("interoperabilityTools(%s) = %v", test.directory, got)
		}
	}
}

func TestXSDSpecificationCatalogMetadata(t *testing.T) {
	t.Parallel()
	if got := specifications("pkg/xsd"); !slices.Equal(
		got,
		[]string{"W3C XML Schema 1.0 Second Edition", "W3C XML Schema Test Suite"},
	) {
		t.Fatalf("specifications(pkg/xsd) = %v", got)
	}
}

func TestConformanceRequired(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		kind           string
		specifications []string
		corpora        []string
		want           bool
	}{
		{name: "public specification", kind: "public library", specifications: []string{"Example 1.0"}, want: true},
		{name: "public corpus", kind: "public library", corpora: []string{"Official suite"}, want: true},
		{name: "ordinary library", kind: "public library"},
		{name: "benchmark harness", kind: "benchmark harness", specifications: []string{"Example 1.0"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := conformanceRequired(test.kind, test.specifications, test.corpora); got != test.want {
				t.Fatalf("conformanceRequired() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestDependencyOrderedDirectories(t *testing.T) {
	t.Parallel()
	current := catalog{Modules: []module{
		{Directory: "consumer", Path: "example.com/consumer", OwnedDependencies: []string{"example.com/middle"}},
		{Directory: "independent", Path: "example.com/independent"},
		{Directory: "leaf", Path: "example.com/leaf"},
		{Directory: "middle", Path: "example.com/middle", OwnedDependencies: []string{"example.com/leaf"}},
	}}

	got := dependencyOrderedDirectories(
		current,
		[]string{"consumer", "independent", "leaf", "middle"},
	)
	want := []string{"independent", "leaf", "middle", "consumer"}
	if !slices.Equal(got, want) {
		t.Fatalf("dependencyOrderedDirectories() = %v, want %v", got, want)
	}
}

func TestExpandOwnedDependenciesIncludesTransitiveClosure(t *testing.T) {
	t.Parallel()

	current := catalog{Modules: []module{
		{Directory: "consumer", Path: "example.com/consumer", OwnedDependencies: []string{"example.com/middle"}},
		{Directory: "independent", Path: "example.com/independent"},
		{Directory: "leaf", Path: "example.com/leaf"},
		{Directory: "middle", Path: "example.com/middle", OwnedDependencies: []string{"example.com/leaf"}},
	}}
	selected := map[string]bool{"consumer": true}

	expandOwnedDependencies(current, selected)

	want := map[string]bool{"consumer": true, "leaf": true, "middle": true}
	if !maps.Equal(selected, want) {
		t.Fatalf("expanded dependencies = %v, want %v", selected, want)
	}
}

func TestValidateOwnedDependencyVersion(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"v0.0.0",
		"v0.0.0-20260728110331-b7c4c77520dd",
	} {
		if err := validateOwnedDependencyVersion(
			"pkg/consumer",
			"github.com/faustbrian/golib/pkg/dependency",
			version,
		); err != nil {
			t.Fatalf(
				"validateOwnedDependencyVersion(%q) error = %v",
				version,
				err,
			)
		}
	}
	for _, version := range []string{
		"v0.1.0",
		"v1.0.0",
		"latest",
		"",
	} {
		if err := validateOwnedDependencyVersion(
			"pkg/consumer",
			"github.com/faustbrian/golib/pkg/dependency",
			version,
		); err == nil {
			t.Fatalf("validateOwnedDependencyVersion(%q) error = nil", version)
		}
	}
}

func TestValidateWorkspaceContentRequiresLocalZeroVersionReplacements(t *testing.T) {
	t.Parallel()

	current := catalog{Modules: []module{{
		Directory: "pkg/dependency",
		Path:      "github.com/faustbrian/golib/pkg/dependency",
	}}}
	valid := "use (\n\t./pkg/dependency\n)\n\n" +
		"replace github.com/faustbrian/golib/pkg/dependency v0.0.0 => ./pkg/dependency\n"
	if err := validateWorkspaceContent(valid, current); err != nil {
		t.Fatalf("validateWorkspaceContent(valid) error = %v", err)
	}
	invalid := strings.Replace(valid, "v0.0.0", "v0.1.0", 1)
	if err := validateWorkspaceContent(invalid, current); err == nil {
		t.Fatal("validateWorkspaceContent(v0.1.0) error = nil")
	}
}

func TestValidateMutationThresholdsRequiresLiteralOneHundred(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		contents  string
		wantError bool
	}{
		{
			name: "exact thresholds",
			contents: "gremlins unleash . --threshold-efficacy 100 " +
				"--threshold-mcover 100\n",
		},
		{
			name:      "reduced efficacy",
			contents:  "gremlins unleash . --threshold-efficacy 99.99\n",
			wantError: true,
		},
		{
			name:      "reduced mutation coverage",
			contents:  "gremlins unleash . --threshold-mcover 95\n",
			wantError: true,
		},
		{
			name:      "runtime override",
			contents:  "gremlins unleash . --threshold-efficacy \"$(MUTATION_EFFICACY)\"\n",
			wantError: true,
		},
		{
			name:      "implicit defaults",
			contents:  "gremlins unleash .\n",
			wantError: true,
		},
		{
			name:     "unrelated command",
			contents: "printf '%s\\n' unleash\n",
		},
		{
			name: "continued exact threshold",
			contents: "gremlins unleash . --threshold-efficacy \\\n" +
				"  100 --threshold-mcover 100\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateMutationThresholdContents("scripts/check-mutation.sh", []byte(test.contents))
			if (err != nil) != test.wantError {
				t.Fatalf(
					"validateMutationThresholdContents() error = %v, wantError %t",
					err,
					test.wantError,
				)
			}
		})
	}
}

func TestRepositoryMutationCommandsCannotReduceThresholds(t *testing.T) {
	root, err := repositoryRoot()
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(root, "modules.json"))
	if err != nil {
		t.Fatalf("read module catalog: %v", err)
	}
	current := catalog{}
	if err := json.Unmarshal(contents, &current); err != nil {
		t.Fatalf("decode module catalog: %v", err)
	}
	if err := validateMutationThresholds(root, current); err != nil {
		t.Fatalf("validate repository mutation thresholds: %v", err)
	}
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
