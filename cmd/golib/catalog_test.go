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

func TestClassifyDifferentialModuleAsInteroperabilityHarness(t *testing.T) {
	t.Parallel()

	kind, releasable := classify("pkg/http-signature/differential/shared-corpus")
	if kind != "interoperability harness" || releasable {
		t.Fatalf("classify() = (%q, %t), want (%q, false)", kind, releasable, "interoperability harness")
	}
}

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

func TestGoalEvidenceDefersFutureSecurityGoalAndSecurityGates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	hardeningGoal := "pkg/sample/.ai/GOAL_HARDEN.md"
	securityGoal := "pkg/sample/.ai/GOAL_SECURITY.md"
	mustWriteFile(t, filepath.Join(root, hardeningGoal), "# Hardening Goal\n")
	mustWriteFile(t, filepath.Join(root, securityGoal), "# Future Goal: Security Audit\n")
	mustWriteFile(t, filepath.Join(root, "pkg/sample/README.md"), "# Sample\n")

	records, err := goalEvidenceFor(
		root,
		"pkg/sample",
		[]string{hardeningGoal, securityGoal},
		[]string{"test", "vulnerability", "secrets", "mutation"},
	)
	if err != nil {
		t.Fatalf("goalEvidenceFor() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("goalEvidenceFor() records = %d, want 2", len(records))
	}
	active := records[0]
	if active.ImplementationStatus != "implemented-requires-fresh-verification" {
		t.Fatalf("active goal status = %q", active.ImplementationStatus)
	}
	if !slices.Equal(active.VerificationGates, []string{"test", "mutation"}) {
		t.Fatalf("active verification gates = %v", active.VerificationGates)
	}
	record := records[1]
	if record.ImplementationStatus != "future-not-started" {
		t.Fatalf("future goal status = %q", record.ImplementationStatus)
	}
	if len(record.ImplementationEvidence) != 0 {
		t.Fatalf("future implementation evidence = %v, want empty", record.ImplementationEvidence)
	}
	if len(record.VerificationGates) != 0 {
		t.Fatalf("future verification gates = %v, want empty", record.VerificationGates)
	}
}

func TestGoalEvidenceIncludesExplicitDesignLanguage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	goal := ".ai/GOAL_COHESION.md"
	mustWriteFile(t, filepath.Join(root, goal), "# Cohesion Goal\n")
	mustWriteFile(t, filepath.Join(root, "README.md"), "# Repository\n")
	mustWriteFile(
		t,
		filepath.Join(root, "docs/design-language.md"),
		"# Design Language\n",
	)

	records, err := goalEvidenceFor(root, ".", []string{goal}, []string{"docs"})
	if err != nil {
		t.Fatalf("goalEvidenceFor() error = %v", err)
	}
	want := []string{"README.md", "docs/design-language.md"}
	if !slices.Equal(records[0].ImplementationEvidence, want) {
		t.Fatalf(
			"implementation evidence = %v, want %v",
			records[0].ImplementationEvidence,
			want,
		)
	}
}

func TestHardeningGoalEvidenceIgnoresAncestorNames(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	goal := ".ai/GOAL_HARDEN.md"
	mustWriteFile(t, filepath.Join(root, goal), "# Hardening Goal\n")
	mustWriteFile(t, filepath.Join(root, "README.md"), "# Repository\n")
	mustWriteFile(t, filepath.Join(root, "docs/unrelated.md"), "# Unrelated\n")

	records, err := goalEvidenceFor(root, ".", []string{goal}, []string{"docs"})
	if err != nil {
		t.Fatalf("goalEvidenceFor() error = %v", err)
	}
	want := []string{"README.md"}
	if !slices.Equal(records[0].ImplementationEvidence, want) {
		t.Fatalf(
			"implementation evidence = %v, want %v",
			records[0].ImplementationEvidence,
			want,
		)
	}
}

func TestApplyCohesionFamiliesRequiresExactReleasableCoverage(t *testing.T) {
	t.Parallel()
	base := catalog{Modules: []module{
		{Directory: ".", Releasable: false},
		{Directory: "pkg/clock", Releasable: true},
		{Directory: "pkg/retry", Releasable: true},
	}}
	valid := cohesionConfig{
		SchemaVersion: 1,
		Families: []cohesionFamily{
			{
				ID:          "foundations",
				Label:       "Foundations",
				Description: "Shared immutable values and deterministic seams.",
				Modules:     []string{"pkg/clock"},
			},
			{
				ID:          "resilience",
				Label:       "Resilience",
				Description: "Bounded failure and admission policies.",
				Modules:     []string{"pkg/retry"},
			},
		},
	}

	tests := []struct {
		name       string
		mutate     func(*cohesionConfig)
		wantError  bool
		wantFamily map[string]string
	}{
		{
			name: "valid",
			wantFamily: map[string]string{
				"pkg/clock": "foundations",
				"pkg/retry": "resilience",
			},
		},
		{
			name:      "unsupported schema",
			mutate:    func(current *cohesionConfig) { current.SchemaVersion = 2 },
			wantError: true,
		},
		{
			name: "missing module",
			mutate: func(current *cohesionConfig) {
				current.Families[1].Modules = nil
			},
			wantError: true,
		},
		{
			name: "duplicate module",
			mutate: func(current *cohesionConfig) {
				current.Families[1].Modules = []string{"pkg/clock", "pkg/retry"}
			},
			wantError: true,
		},
		{
			name: "unknown module",
			mutate: func(current *cohesionConfig) {
				current.Families[1].Modules = []string{"pkg/retry", "pkg/unknown"}
			},
			wantError: true,
		},
		{
			name: "internal module",
			mutate: func(current *cohesionConfig) {
				current.Families[1].Modules = []string{".", "pkg/retry"}
			},
			wantError: true,
		},
		{
			name: "invalid family",
			mutate: func(current *cohesionConfig) {
				current.Families[0].ID = "Foundations"
			},
			wantError: true,
		},
		{
			name: "missing description",
			mutate: func(current *cohesionConfig) {
				current.Families[0].Description = ""
			},
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			current := base
			current.Modules = slices.Clone(base.Modules)
			config := valid
			config.Families = slices.Clone(valid.Families)
			for index := range config.Families {
				config.Families[index].Modules = slices.Clone(valid.Families[index].Modules)
			}
			if test.mutate != nil {
				test.mutate(&config)
			}
			err := applyCohesionFamilies(&current, config)
			if (err != nil) != test.wantError {
				t.Fatalf("applyCohesionFamilies() error = %v, wantError %t", err, test.wantError)
			}
			if err != nil {
				return
			}
			for _, item := range current.Modules {
				if item.Releasable && item.Family != test.wantFamily[item.Directory] {
					t.Errorf("module %s family = %q", item.Directory, item.Family)
				}
			}
		})
	}
}

func TestCatalogDocumentationSeparatesConsumerAndEngineeringViews(t *testing.T) {
	t.Parallel()
	current := catalog{Modules: []module{
		{
			Directory:         "pkg/clock",
			Path:              "example.com/clock",
			Kind:              "public library",
			Lifecycle:         "pre-v1",
			Purpose:           "Explicit time.",
			Family:            "foundations",
			FamilyLabel:       "Foundations",
			FamilyDescription: "Shared immutable values and deterministic seams.",
			FamilyOrder:       1,
			Releasable:        true,
		},
		{
			Directory:  "pkg/clock/benchmarks/competitors",
			Path:       "example.com/clock-benchmarks",
			Kind:       "benchmark harness",
			Lifecycle:  "internal",
			Purpose:    "Comparison harness.",
			Releasable: false,
		},
	}}

	documents := catalogDocumentation(current)
	consumer := documents["docs/package-catalog.md"]
	if !strings.Contains(consumer, "## Foundations") ||
		!strings.Contains(consumer, "example.com/clock") ||
		strings.Contains(consumer, "clock-benchmarks") {
		t.Fatalf("consumer catalog does not isolate releasable modules:\n%s", consumer)
	}
	engineering := documents["docs/engineering-inventory.md"]
	if !strings.Contains(engineering, "example.com/clock") ||
		!strings.Contains(engineering, "clock-benchmarks") ||
		!strings.Contains(engineering, "| benchmark harness | - | internal |") {
		t.Fatalf("engineering inventory omits registered modules:\n%s", engineering)
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
		{directory: "pkg/cloudevents", want: []string{
			"cloudevents/sdk-go v2.16.2",
			"cloudevents/sdk-javascript v10.0.0 on Node.js 24.19.0",
		}},
		{directory: "pkg/ecma-regexp", want: []string{"Node.js", "Test262"}},
		{directory: "pkg/wsdl", want: []string{"Java", "Apache Woden"}},
		{directory: "pkg/xsd", want: []string{"Docker", "Eclipse Temurin 25 JAXP"}},
		{directory: "pkg/search/adapters/opensearch", want: []string{"OpenSearch 2.19.6", "OpenSearch 3.8.0", "opensearch-go/v4 v4.7.3"}},
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

func TestHTTPSignatureInteroperabilityCatalogMetadata(t *testing.T) {
	t.Parallel()

	want := []string{
		"dadrus/httpsig at 0f24bf7dd9b76727af985d9a6f7ce87207a18387",
		"shogo82148/go-sfv v0.3.3",
		"yaronf/httpsign at de382d35c1add89cc09b9355161d61471fb7f632",
	}
	if got := interoperabilityTools("pkg/http-signature"); !slices.Equal(got, want) {
		t.Fatalf("interoperabilityTools(pkg/http-signature) = %v, want %v", got, want)
	}
}

func TestHTTPSignatureSpecificationCatalogMetadata(t *testing.T) {
	t.Parallel()

	if got := specifications("pkg/http-signature"); !slices.Equal(got, []string{
		"RFC 9421 HTTP Message Signatures",
		"RFC 9530 Digest Fields",
		"RFC 8941 Structured Field Values for HTTP",
		"IANA HTTP Message Signature registries",
		"IANA Hash Algorithms for HTTP Digest Fields registry",
		"IANA HTTP Field Name registry",
		"NIST CAVP FIPS 186-3 ECDSA test vectors",
	}) {
		t.Fatalf("specifications(pkg/http-signature) = %v", got)
	}
	if got := conformanceCorpora("pkg/http-signature"); !slices.Equal(got, []string{
		"RFC 9421 Appendix B examples",
		"RFC 9530 examples",
		"NIST CAVP FIPS 186-3 ECDSA P-384 vectors",
		"yaronf/httpsign RFC 9421 fixtures at de382d35c1add89cc09b9355161d61471fb7f632",
		"dadrus/httpsig RFC 9421 fixtures at 0f24bf7dd9b76727af985d9a6f7ce87207a18387",
		"Shared Structured Fields and signature-base differential corpus",
	}) {
		t.Fatalf("conformanceCorpora(pkg/http-signature) = %v", got)
	}
}

func TestVerkleTreeSpecificationCatalogMetadata(t *testing.T) {
	t.Parallel()

	if got := interoperabilityTools("pkg/verkle-tree"); !slices.Equal(got, []string{
		"ethereum/go-verkle at aa0a270c0ed03faa6c502e0d96bf26189d1d6542",
		"crate-crypto/rust-verkle at e27b8b4edf1992b4afa636c2fc7983bcc27ddb88",
		"Rust 1.97.0",
	}) {
		t.Fatalf("interoperabilityTools(pkg/verkle-tree) = %v", got)
	}
	if got := specifications("pkg/verkle-tree"); !slices.Equal(got, []string{
		"verkletree-bandersnatch-ipa-256-v0 package-owned pre-v1 profile",
		"Ethereum Verkle EIPs 4762, 6800, 7612, and 7748 at c55786f4242e5324afd14c6bca890a369a771d7f (research only; not implemented)",
	}) {
		t.Fatalf("specifications(pkg/verkle-tree) = %v", got)
	}
	if got := conformanceCorpora("pkg/verkle-tree"); !slices.Equal(got, []string{
		"Pinned go-verkle tree and aggregate-proof corpus at aa0a270c0ed03faa6c502e0d96bf26189d1d6542",
		"Pinned rust-verkle encoding, generator, vector-commitment, multiproof, tree-root, topology, and transition corpora at e27b8b4edf1992b4afa636c2fc7983bcc27ddb88",
		"Package-owned verkletree-bandersnatch-ipa-256-v0 positive and hostile-input evidence",
	}) {
		t.Fatalf("conformanceCorpora(pkg/verkle-tree) = %v", got)
	}

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "pkg/verkle-tree/specification/sources.json"), "{}\n")
	if got := provenanceFiles(root, "pkg/verkle-tree"); !slices.Equal(got, []string{
		"pkg/verkle-tree/specification/sources.json",
	}) {
		t.Fatalf("provenanceFiles() = %v", got)
	}
}

func TestProvenanceFilesIncludesSpecificationSourceLocks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "pkg/http-signature/spec/sources.lock.json"), "{}\n")

	want := []string{"pkg/http-signature/spec/sources.lock.json"}
	if got := provenanceFiles(root, "pkg/http-signature"); !slices.Equal(got, want) {
		t.Fatalf("provenanceFiles() = %v, want %v", got, want)
	}
}

func TestCloudEventsSpecificationCatalogMetadata(t *testing.T) {
	t.Parallel()
	if got := specifications("pkg/cloudevents"); !slices.Equal(got, []string{
		"CloudEvents specification 1.0.2",
		"CloudEvents JSON event format 1.0.2",
		"CloudEvents HTTP protocol binding 1.0.2",
		"CloudEvents Kafka protocol binding 1.0.2",
		"CloudEvents distributed tracing extension 1.0.2",
		"CloudEvents partitioning extension 1.0.2",
	}) {
		t.Fatalf("specifications(pkg/cloudevents) = %v", got)
	}
	if got := conformanceCorpora("pkg/cloudevents"); !slices.Equal(got, []string{
		"cloudevents/conformance v0.4.1 HTTP and Kafka features",
	}) {
		t.Fatalf("conformanceCorpora(pkg/cloudevents) = %v", got)
	}
}

func TestOpenSearchSpecificationCatalogMetadata(t *testing.T) {
	t.Parallel()
	if got := specifications("pkg/search/adapters/opensearch"); !slices.Equal(
		got,
		[]string{"OpenSearch REST API 2.19.6 and 3.8.0"},
	) {
		t.Fatalf("specifications(pkg/search/adapters/opensearch) = %v", got)
	}
}

func TestAuthenticationSpecificationCatalogMetadata(t *testing.T) {
	t.Parallel()
	if got := specifications("pkg/authentication"); !slices.Equal(got, []string{
		"RFC 7617 Basic HTTP Authentication",
		"RFC 6750 OAuth 2.0 Bearer Token Usage",
		"RFC 9110 HTTP Authentication Framework",
	}) {
		t.Fatalf("specifications(pkg/authentication) = %v", got)
	}
	if got := conformanceCorpora("pkg/authentication"); !slices.Equal(got, []string{
		"RFC 7617 Sections 2 and 2.1 credential vectors",
		"RFC 6750 Section 2.1 bearer b64token vector",
	}) {
		t.Fatalf("conformanceCorpora(pkg/authentication) = %v", got)
	}
}

func TestHTTPClientSpecificationCatalogMetadata(t *testing.T) {
	t.Parallel()
	if got := specifications("pkg/http-client"); !slices.Equal(got, []string{
		"RFC 3986 URI Generic Syntax",
		"RFC 9110 HTTP Semantics",
		"RFC 9111 HTTP Caching",
		"RFC 8288 Web Linking",
		"RFC 7617 Basic HTTP Authentication",
		"RFC 6750 OAuth 2.0 Bearer Token Usage",
		"RFC 6749 OAuth 2.0 Authorization Framework",
		"RFC 6265 HTTP State Management Mechanism",
		"RFC 8259 JSON",
		"RFC 8470 HTTP Early Data",
		"RFC 6585 Additional HTTP Status Codes",
		"W3C Trace Context Level 1 Recommendation 2021-11-23",
	}) {
		t.Fatalf("specifications(pkg/http-client) = %v", got)
	}
	if got := conformanceCorpora("pkg/http-client"); !slices.Equal(got, []string{
		"Pinned normative-source matrix and specification decision evidence",
	}) {
		t.Fatalf("conformanceCorpora(pkg/http-client) = %v", got)
	}
}

func TestHTTPMiddlewareSpecificationCatalogMetadata(t *testing.T) {
	t.Parallel()
	if got := specifications("pkg/http-middleware"); !slices.Equal(got, []string{
		"Go 1.26.6 net/http and context contracts",
		"RFC 9110 HTTP Semantics",
		"RFC 9111 HTTP Caching",
		"RFC 7239 Forwarded HTTP Extension",
		"RFC 6797 HTTP Strict Transport Security",
		"RFC 7034 X-Frame-Options",
		"WHATWG Fetch CORS protocol at 586cd2a44c2a",
		"WHATWG URL origin model at 9dc3827fc722",
		"W3C Referrer Policy at cc435b05ca4a",
	}) {
		t.Fatalf("specifications(pkg/http-middleware) = %v", got)
	}
	if got := conformanceCorpora("pkg/http-middleware"); !slices.Equal(got, []string{
		"Pinned normative-source matrix and specification decision evidence",
	}) {
		t.Fatalf("conformanceCorpora(pkg/http-middleware) = %v", got)
	}
}

func TestRouterSpecificationCatalogMetadata(t *testing.T) {
	t.Parallel()
	if got := specifications("pkg/router"); !slices.Equal(got, []string{
		"Go 1.26.6 net/http and net/url contracts",
		"RFC 3986 URI Generic Syntax",
		"RFC 9110 HTTP Semantics",
		"RFC 9112 HTTP/1.1 request-target forms",
	}) {
		t.Fatalf("specifications(pkg/router) = %v", got)
	}
	if got := conformanceCorpora("pkg/router"); !slices.Equal(got, []string{
		"Pinned normative-source matrix and ServeMux differential evidence",
	}) {
		t.Fatalf("conformanceCorpora(pkg/router) = %v", got)
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
		{name: "adapter specification", kind: "adapter", specifications: []string{"Example 1.0"}, want: true},
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

	if err := validateOwnedDependencyVersion(
		"pkg/consumer",
		"github.com/faustbrian/golib/pkg/dependency",
		"v0.0.0",
	); err != nil {
		t.Fatalf("validateOwnedDependencyVersion(v0.0.0) error = %v", err)
	}
	for _, version := range []string{
		"v0.0.0-20260728110331-b7c4c77520dd",
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

func TestValidateDependencyUpdateTopology(t *testing.T) {
	t.Parallel()

	valid := `version: 2
updates:
  - package-ecosystem: gomod
    directories:
      - "/"
      - "/pkg/**"
    schedule:
      interval: weekly
    groups:
      monorepo-dependencies:
        group-by: dependency-name
  - package-ecosystem: github-actions
    directory: "/"
    schedule:
      interval: weekly
    groups:
      github-actions:
        patterns:
          - "*"
`
	tests := []struct {
		name       string
		configure  func(*testing.T, string)
		wantSubstr string
	}{
		{
			name: "canonical root configuration",
			configure: func(t *testing.T, root string) {
				mustWriteFile(t, filepath.Join(root, ".github/dependabot.yml"), valid)
			},
		},
		{
			name:       "missing root configuration",
			configure:  func(*testing.T, string) {},
			wantSubstr: "root Dependabot configuration",
		},
		{
			name: "nested configuration",
			configure: func(t *testing.T, root string) {
				mustWriteFile(t, filepath.Join(root, ".github/dependabot.yml"), valid)
				mustWriteFile(t, filepath.Join(root, "pkg/cache/.github/dependabot.yml"), valid)
			},
			wantSubstr: "non-authoritative Dependabot configuration",
		},
		{
			name: "incomplete module selection",
			configure: func(t *testing.T, root string) {
				mustWriteFile(t, filepath.Join(root, ".github/dependabot.yml"), strings.Replace(
					valid,
					"      - \"/pkg/**\"\n",
					"",
					1,
				))
			},
			wantSubstr: "all nested Go modules",
		},
		{
			name: "missing action updates",
			configure: func(t *testing.T, root string) {
				mustWriteFile(t, filepath.Join(root, ".github/dependabot.yml"), strings.Split(
					valid,
					"  - package-ecosystem: github-actions\n",
				)[0])
			},
			wantSubstr: "GitHub Actions",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			test.configure(t, root)
			err := validateDependencyUpdateTopology(root)
			if test.wantSubstr == "" {
				if err != nil {
					t.Fatalf("validateDependencyUpdateTopology() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantSubstr) {
				t.Fatalf(
					"validateDependencyUpdateTopology() error = %v, want substring %q",
					err,
					test.wantSubstr,
				)
			}
		})
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
