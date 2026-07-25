package driver

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	shared "github.com/faustbrian/golib/pkg/analysis/analysis"
	"github.com/faustbrian/golib/pkg/analysis/policy"
	toolanalysis "golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"
)

func TestRunPropagatesSystemBoundaryFailures(t *testing.T) {
	t.Parallel()

	config := writeInternalPolicy(t, "version: 1\n")
	base := dependencies{
		registry: policy.Builtin,
		load: func(*packages.Config, ...string) ([]*packages.Package, error) {
			return []*packages.Package{{PkgPath: "example.com/p"}}, nil
		},
		analyze: func([]*toolanalysis.Analyzer, []*packages.Package, *checker.Options) (*checker.Graph, error) {
			return &checker.Graph{}, nil
		},
		now:      time.Now,
		relative: repositoryPath,
	}
	tests := map[string]dependencies{
		"registry": withRegistry(base, func() (*policy.Registry, error) {
			return nil, errors.New("registry failure")
		}),
		"load": withLoad(base, func(*packages.Config, ...string) ([]*packages.Package, error) {
			return nil, errors.New("load failure")
		}),
		"empty": withLoad(base, func(*packages.Config, ...string) ([]*packages.Package, error) {
			return nil, nil
		}),
		"analyze": withAnalyze(base, func([]*toolanalysis.Analyzer, []*packages.Package, *checker.Options) (*checker.Graph, error) {
			return nil, errors.New("checker failure")
		}),
	}
	for name, dependencies := range tests {
		dependencies := dependencies
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := run(context.Background(), Options{ConfigPath: config}, dependencies); err == nil {
				t.Fatal("run() error = nil")
			}
		})
	}
}

func TestRunDefaultsEmptyPatternsAndPreservesExplicitPatterns(t *testing.T) {
	t.Parallel()

	config := writeInternalPolicy(t, "version: 1\n")
	tests := []struct {
		name     string
		options  Options
		patterns []string
	}{
		{
			name:     "default",
			options:  Options{ConfigPath: config},
			patterns: []string{"./..."},
		},
		{
			name: "explicit",
			options: Options{
				ConfigPath: config,
				Patterns:   []string{"./analysis", "./policy"},
			},
			patterns: []string{"./analysis", "./policy"},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var loadedPatterns []string
			dependencies := dependencies{
				registry: policy.Builtin,
				load: func(_ *packages.Config, patterns ...string) ([]*packages.Package, error) {
					loadedPatterns = append([]string(nil), patterns...)

					return []*packages.Package{{PkgPath: "example.com/p"}}, nil
				},
				analyze: func([]*toolanalysis.Analyzer, []*packages.Package, *checker.Options) (*checker.Graph, error) {
					return &checker.Graph{}, nil
				},
				now:      time.Now,
				relative: repositoryPath,
			}
			if _, err := run(context.Background(), test.options, dependencies); err != nil {
				t.Fatalf("run() error = %v", err)
			}
			if strings.Join(loadedPatterns, "\x00") != strings.Join(test.patterns, "\x00") {
				t.Fatalf("loaded patterns = %#v, want %#v", loadedPatterns, test.patterns)
			}
		})
	}
}

func TestRunLoadsTargetSyntaxWithoutDependencySyntax(t *testing.T) {
	t.Parallel()

	config := writeInternalPolicy(t, "version: 1\n")
	var loadedMode packages.LoadMode
	dependencies := dependencies{
		registry: policy.Builtin,
		load: func(config *packages.Config, _ ...string) ([]*packages.Package, error) {
			loadedMode = config.Mode

			return []*packages.Package{{PkgPath: "example.com/p"}}, nil
		},
		analyze: func([]*toolanalysis.Analyzer, []*packages.Package, *checker.Options) (*checker.Graph, error) {
			return &checker.Graph{}, nil
		},
		now:      time.Now,
		relative: repositoryPath,
	}
	if _, err := run(
		context.Background(),
		Options{ConfigPath: config},
		dependencies,
	); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if loadedMode != packages.LoadSyntax {
		t.Fatalf("package load mode = %v, want LoadSyntax", loadedMode)
	}
}

func TestRunPropagatesPipelineFailures(t *testing.T) {
	t.Parallel()

	validConfig := writeInternalPolicy(t, "version: 1\n")
	invalidBoundary := writeInternalPolicy(t, "version: 1\npackages:\n"+
		"  - pattern: bad/*\n    deny_imports: [example.com/infra]\n")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "/repo/a.go", `package p
//analysis:ignore security/unknown -- invalid
var Value int
`, parser.ParseComments)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	basePackage := &packages.Package{
		PkgPath: "example.com/p",
		Fset:    fset,
		Syntax:  []*ast.File{file},
	}
	cleanSet := token.NewFileSet()
	cleanFile, err := parser.ParseFile(
		cleanSet,
		"/repo/clean.go",
		"package p\nvar Value int\n",
		parser.ParseComments,
	)
	if err != nil {
		t.Fatalf("ParseFile(clean) error = %v", err)
	}
	cleanPackage := &packages.Package{
		PkgPath: "example.com/p",
		Fset:    cleanSet,
		Syntax:  []*ast.File{cleanFile},
	}
	base := dependencies{
		registry: policy.Builtin,
		load: func(*packages.Config, ...string) ([]*packages.Package, error) {
			return []*packages.Package{basePackage}, nil
		},
		analyze: func([]*toolanalysis.Analyzer, []*packages.Package, *checker.Options) (*checker.Graph, error) {
			return &checker.Graph{}, nil
		},
		now:      time.Now,
		relative: repositoryPath,
	}
	tests := []struct {
		name         string
		config       string
		dependencies dependencies
	}{
		{name: "config", config: "/missing/policy.yml", dependencies: base},
		{name: "boundary", config: invalidBoundary, dependencies: base},
		{
			name:   "package errors",
			config: validConfig,
			dependencies: withLoad(base, func(*packages.Config, ...string) ([]*packages.Package, error) {
				return []*packages.Package{{Errors: []packages.Error{{Msg: "broken syntax"}}}}, nil
			}),
		},
		{
			name:   "analyzer error",
			config: validConfig,
			dependencies: withAnalyze(base, func(analyzers []*toolanalysis.Analyzer, _ []*packages.Package, _ *checker.Options) (*checker.Graph, error) {
				return &checker.Graph{Roots: []*checker.Action{{
					Analyzer: analyzers[0],
					Package:  basePackage,
					Err:      errors.New("analyzer failure"),
				}}}, nil
			}),
		},
		{name: "suppression", config: validConfig, dependencies: base},
		{
			name:   "relative path",
			config: validConfig,
			dependencies: func() dependencies {
				value := base
				value.load = func(*packages.Config, ...string) ([]*packages.Package, error) {
					return []*packages.Package{cleanPackage}, nil
				}
				value.analyze = func(analyzers []*toolanalysis.Analyzer, _ []*packages.Package, _ *checker.Options) (*checker.Graph, error) {
					return &checker.Graph{Roots: []*checker.Action{{
						Analyzer: analyzers[0],
						Package:  cleanPackage,
						Diagnostics: []toolanalysis.Diagnostic{{
							Pos: cleanFile.Pos(), Message: "finding",
						}},
					}}}, nil
				}
				value.relative = func(string, string) (string, error) {
					return "", errors.New("relative failure")
				}
				return value
			}(),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := run(context.Background(), Options{
				ConfigPath: test.config,
				Now:        time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			}, test.dependencies); err == nil {
				t.Fatal("run() error = nil")
			}
		})
	}
}

func TestBuildSpecsAppliesConfiguration(t *testing.T) {
	t.Parallel()
	variadicFrom := 1

	specs, err := buildSpecs(&shared.Config{
		Packages: []shared.PackagePolicy{
			{Pattern: "example.com/application/...", Layer: "application", Context: "orders"},
			{
				Pattern:           "example.com/domain",
				DenyImports:       []string{"example.com/infra/..."},
				AllowImports:      []string{"example.com/infra/approved"},
				BlockingFunctions: []string{"Repository.Load"},
			},
		},
		Layers: []shared.DirectionPolicy{
			{Name: "application", MayImport: []string{"shared"}},
			{Name: "shared"},
		},
		Contexts: []shared.DirectionPolicy{
			{Name: "orders", MayImport: []string{"shared"}},
			{Name: "shared"},
		},
		Entrypoints:   []string{"example.com/cmd/service"},
		InitPackages:  []string{"example.com/init"},
		ContextOwners: []string{"example.com/context-owner"},
		HTTPTimeoutExceptions: []string{
			"example.com/streaming",
		},
		Constructors: []shared.ConstructorPolicy{{
			Package: "example.com/worker",
			Symbols: []string{"New"},
		}},
		ResourceConstructors: []shared.ResourceConstructorPolicy{{
			Package:       "example.com/resource",
			Symbol:        "Open",
			CleanupResult: 1,
		}},
		Transactions: []shared.TransactionPolicy{{
			Package: "example.com/tx", Symbol: "Begin", Result: 0,
			RollbackMethod: "Rollback",
		}},
		LockSensitiveCalls: []shared.LockSensitiveCallPolicy{{
			Package: "example.com/backend",
			Symbol:  "Call",
		}},
		SensitiveTypes: []shared.SensitiveTypePolicy{{
			Package: "example.com/security", Name: "Token",
		}},
		SensitiveSinks: []shared.SensitiveSinkPolicy{{
			Package: "example.com/log",
			Symbol:  "Record",
			Arguments: []int{
				0,
			},
		}},
		ForbiddenAPIs: []shared.ForbiddenAPIPolicy{{
			Package:         "example.com/legacy",
			Symbol:          "Old",
			Replacement:     "example.com/modern.New",
			AllowedPackages: []string{"example.com/adapter"},
		}},
		BackendClients: []shared.BackendClientPolicy{{
			Package:         "example.com/backend/...",
			AllowedPackages: []string{"example.com/adapter/..."},
		}},
		MutableGlobals: []shared.MutableGlobalPolicy{{
			Package: "example.com/runtime/...",
		}},
		InterfaceProviderPackages: []string{"example.com/provider/..."},
		InterfaceNames: []shared.InterfaceNamePolicy{{
			Package: "example.com/ports/...", RequiredSuffix: "Port",
		}},
		MetricLabelTypes: []shared.MetricLabelTypePolicy{{
			Package: "example.com/model", Name: "UserID",
		}},
		MetricLabelSinks: []shared.MetricLabelSinkPolicy{{
			Package: "example.com/metrics", Symbol: "Label", Arguments: []int{0},
		}},
		MetricLabelNameTypes: []shared.MetricLabelNameTypePolicy{{
			Package: "example.com/model", Name: "LabelName",
		}},
		MetricLabelNameSinks: []shared.MetricLabelSinkPolicy{{
			Package: "example.com/metrics", Symbol: "Name", Arguments: []int{0},
		}},
		BackendErrorBoundaries: []string{"example.com/api/..."},
		BackendErrorSources: []shared.BackendErrorFlowPolicy{{
			Package: "example.com/backend", Symbol: "Load", Result: 1,
		}},
		BackendErrorPassthroughs: []shared.BackendErrorPassthroughPolicy{{
			Package: "fmt", Symbol: "Errorf", Result: 0, VariadicFrom: &variadicFrom,
		}},
		GoroutineFanout: []shared.GoroutineFanoutPolicy{{
			Package: "example.com/worker/...", MaxStatic: 8,
		}},
		Rules: map[string]shared.RulePolicy{
			"security/no-unsafe": {
				Status:   shared.StatusBlocking,
				Severity: shared.SeverityWarning,
			},
		},
	})
	if err != nil {
		t.Fatalf("buildSpecs() error = %v", err)
	}
	if len(specs) != 23 || specs[16].status != shared.StatusBlocking ||
		specs[16].rule.Severity != shared.SeverityWarning {
		t.Fatalf("buildSpecs() = %#v", specs)
	}
	if _, err := buildSpecs(&shared.Config{Packages: []shared.PackagePolicy{{
		Pattern: "example.com/unused",
	}}}); err == nil {
		t.Fatal("buildSpecs() accepted an inert package policy")
	}
	if _, err := buildSpecs(&shared.Config{Packages: []shared.PackagePolicy{{
		Pattern:      "example.com/domain",
		AllowImports: []string{"example.com/infra"},
	}}}); err == nil {
		t.Fatal("buildSpecs() accepted allow imports without deny imports")
	}
	if _, err := buildSpecs(&shared.Config{Packages: []shared.PackagePolicy{{
		Pattern:     "bad/*",
		DenyImports: []string{"example.com/infra"},
	}}}); err == nil {
		t.Fatal("buildSpecs() accepted invalid boundary")
	}
	if _, err := buildSpecs(&shared.Config{Packages: []shared.PackagePolicy{{
		Pattern:           "bad/*",
		BlockingFunctions: []string{"Fetch"},
	}}}); err == nil {
		t.Fatal("buildSpecs() accepted invalid blocking API")
	}
	if _, err := buildSpecs(&shared.Config{ForbiddenAPIs: []shared.ForbiddenAPIPolicy{{
		Package: "bad/*", Symbol: "Old", Replacement: "modern.New",
	}}}); err == nil {
		t.Fatal("buildSpecs() accepted invalid forbidden API")
	}
	if _, err := buildSpecs(&shared.Config{BackendClients: []shared.BackendClientPolicy{{
		Package: "bad/*", AllowedPackages: []string{"example.com/adapter"},
	}}}); err == nil {
		t.Fatal("buildSpecs() accepted invalid backend client")
	}
	if _, err := buildSpecs(&shared.Config{MutableGlobals: []shared.MutableGlobalPolicy{{
		Package: "bad/*",
	}}}); err == nil {
		t.Fatal("buildSpecs() accepted invalid mutable global policy")
	}
	if _, err := buildSpecs(&shared.Config{
		InterfaceProviderPackages: []string{"bad/*"},
	}); err == nil {
		t.Fatal("buildSpecs() accepted invalid interface provider package")
	}
	if _, err := buildSpecs(&shared.Config{
		InterfaceNames: []shared.InterfaceNamePolicy{{Package: "bad/*", RequiredSuffix: "Port"}},
	}); err == nil {
		t.Fatal("buildSpecs() accepted invalid interface naming package")
	}
	if _, err := buildSpecs(&shared.Config{
		MetricLabelTypes: []shared.MetricLabelTypePolicy{{Package: "bad/*", Name: "UserID"}},
	}); err == nil {
		t.Fatal("buildSpecs() accepted invalid metric label policy")
	}
	if _, err := buildSpecs(&shared.Config{
		MetricLabelNameTypes: []shared.MetricLabelNameTypePolicy{{
			Package: "bad/*", Name: "LabelName",
		}},
	}); err == nil {
		t.Fatal("buildSpecs() accepted invalid metric label name policy")
	}
	if _, err := buildSpecs(&shared.Config{
		Transactions: []shared.TransactionPolicy{{
			Package: "bad/*", Symbol: "Begin", RollbackMethod: "Rollback",
		}},
	}); err == nil {
		t.Fatal("buildSpecs() accepted invalid transaction policy")
	}
	if _, err := buildSpecs(&shared.Config{
		BackendErrorBoundaries: []string{"bad/*"},
	}); err == nil {
		t.Fatal("buildSpecs() accepted invalid backend error policy")
	}
	if _, err := buildSpecs(&shared.Config{
		GoroutineFanout: []shared.GoroutineFanoutPolicy{{Package: "bad/*", MaxStatic: 8}},
	}); err == nil {
		t.Fatal("buildSpecs() accepted invalid goroutine fan-out policy")
	}
	if _, err := buildSpecs(&shared.Config{Constructors: []shared.ConstructorPolicy{{
		Package: "bad/*", Symbols: []string{"New"},
	}}}); err == nil {
		t.Fatal("buildSpecs() accepted invalid constructor")
	}
	if _, err := buildSpecs(&shared.Config{
		ResourceConstructors: []shared.ResourceConstructorPolicy{{
			Package: "bad/*", Symbol: "Open", CleanupResult: 1,
		}},
	}); err == nil {
		t.Fatal("buildSpecs() accepted invalid resource constructor")
	}
	if _, err := buildSpecs(&shared.Config{
		HTTPTimeoutExceptions: []string{"bad/*"},
	}); err == nil {
		t.Fatal("buildSpecs() accepted invalid HTTP timeout exception")
	}
	if _, err := buildSpecs(&shared.Config{
		LockSensitiveCalls: []shared.LockSensitiveCallPolicy{{
			Package: "bad/*", Symbol: "Call",
		}},
	}); err == nil {
		t.Fatal("buildSpecs() accepted invalid lock-sensitive call")
	}
	if _, err := buildSpecs(&shared.Config{
		SensitiveTypes: []shared.SensitiveTypePolicy{{
			Package: "bad/*", Name: "Token",
		}},
	}); err == nil {
		t.Fatal("buildSpecs() accepted invalid sensitive type")
	}
}

func TestValidateRejectsNilConfiguration(t *testing.T) {
	t.Parallel()

	if err := Validate(nil); err == nil {
		t.Fatal("Validate(nil) error = nil")
	}
	if err := Validate(&shared.Config{}); err == nil {
		t.Fatal("Validate(version zero) error = nil")
	}
	if err := Validate(&shared.Config{
		Version:      1,
		AdapterRoots: []string{"example.com/service/internal/adapters/..."},
	}); err == nil {
		t.Fatal("Validate(adapter roots) error = nil")
	}
	if err := Validate(&shared.Config{
		Version: 1,
		Rules: map[string]shared.RulePolicy{
			"security/no-unsafe": {Options: map[string]any{"mode": "strict"}},
		},
	}); err == nil {
		t.Fatal("Validate(rule options) error = nil")
	}
	cycle := Validate(&shared.Config{
		Version: 1,
		Layers: []shared.DirectionPolicy{
			{Name: "application", MayImport: []string{"domain"}},
			{Name: "domain", MayImport: []string{"application"}},
		},
	})
	if cycle == nil || !strings.Contains(
		cycle.Error(),
		"layer dependency cycle: application -> domain -> application",
	) {
		t.Fatalf("Validate(layer cycle) error = %v", cycle)
	}
	if err := Validate(&shared.Config{Version: 1}); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}
	if err := validate(&shared.Config{Version: 1}, func() (*policy.Registry, error) {
		return nil, errors.New("registry failure")
	}); err == nil || !strings.Contains(err.Error(), "build rule registry") {
		t.Fatalf("validate(registry failure) error = %v", err)
	}
}

func TestCollectDiagnosticsPropagatesAnalyzerFailure(t *testing.T) {
	t.Parallel()

	analyzer := &toolanalysis.Analyzer{Name: "broken"}
	graph := &checker.Graph{Roots: []*checker.Action{{
		Analyzer: analyzer,
		Package:  &packages.Package{PkgPath: "example.com/p"},
		Err:      errors.New("analyzer failed"),
	}}}
	if _, err := collectDiagnostics(graph, map[*toolanalysis.Analyzer]analyzerSpec{
		analyzer: {analyzer: analyzer},
	}, nil); err == nil {
		t.Fatal("collectDiagnostics() error = nil")
	}
}

func TestDiagnosticBudgetBoundsAnalyzerEmission(t *testing.T) {
	t.Parallel()

	original := &toolanalysis.Analyzer{
		Name: "emitter",
		Run: func(pass *toolanalysis.Pass) (any, error) {
			for index := 0; index < 3; index++ {
				pass.Report(toolanalysis.Diagnostic{Message: "finding"})
			}
			return "result", errors.New("analyzer failure")
		},
	}
	budget := newDiagnosticBudget(2)
	wrapped := budget.wrap(original)
	reported := 0
	result, err := wrapped.Run(&toolanalysis.Pass{
		Report: func(toolanalysis.Diagnostic) { reported++ },
	})
	if result != "result" || err == nil || err.Error() != "analyzer failure" {
		t.Fatalf("Run() = %v, %v", result, err)
	}
	if reported != 2 || !budget.exceeded() {
		t.Fatalf("reported = %d, exceeded = %t", reported, budget.exceeded())
	}
	if original == wrapped || original.Name != wrapped.Name {
		t.Fatal("wrap() mutated analyzer identity or metadata")
	}

	originalReports := 0
	_, _ = original.Run(&toolanalysis.Pass{
		Report: func(toolanalysis.Diagnostic) { originalReports++ },
	})
	if originalReports != 3 {
		t.Fatalf("original reports = %d, want 3", originalReports)
	}
}

func TestRunRejectsDiagnosticEmissionOverflow(t *testing.T) {
	t.Parallel()

	config := writeInternalPolicy(t, "version: 1\n")
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "/repo/init.go", `package p
func init() {}
func init() {}
func init() {}
`, 0)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	loaded := &packages.Package{
		PkgPath: "example.com/p",
		Fset:    files,
		Syntax:  []*ast.File{file},
	}
	dependencies := dependencies{
		registry: policy.Builtin,
		load: func(*packages.Config, ...string) ([]*packages.Package, error) {
			return []*packages.Package{loaded}, nil
		},
		analyze: func(
			analyzers []*toolanalysis.Analyzer,
			_ []*packages.Package,
			_ *checker.Options,
		) (*checker.Graph, error) {
			for _, analyzer := range analyzers {
				if analyzer.Name != "noinit" {
					continue
				}
				_, runErr := analyzer.Run(&toolanalysis.Pass{
					Analyzer: analyzer,
					Fset:     files,
					Files:    []*ast.File{file},
					Pkg:      types.NewPackage("example.com/p", "p"),
					Report:   func(toolanalysis.Diagnostic) {},
				})
				return &checker.Graph{}, runErr
			}
			return nil, errors.New("noinit analyzer not found")
		},
		now:             time.Now,
		relative:        repositoryPath,
		diagnosticLimit: 2,
	}
	_, err = run(context.Background(), Options{ConfigPath: config}, dependencies)
	if err == nil || err.Error() != "diagnostics exceed 2 entries" {
		t.Fatalf("run() error = %v", err)
	}
}

func TestPackageErrorsAreDeterministic(t *testing.T) {
	t.Parallel()

	if err := packageErrors(nil); err != nil {
		t.Fatalf("packageErrors(nil) = %v", err)
	}
	err := packageErrors([]*packages.Package{{Errors: []packages.Error{
		{Msg: "z error"},
		{Msg: "a error"},
	}}})
	if err == nil || strings.Index(err.Error(), "a error") > strings.Index(err.Error(), "z error") {
		t.Fatalf("packageErrors() = %v", err)
	}
}

func TestRelativizePropagatesBothPathFailures(t *testing.T) {
	t.Parallel()

	fail := func(string, string) (string, error) {
		return "", errors.New("relative failure")
	}
	if err := relativize("/repo", []shared.Diagnostic{{Filename: "/repo/a.go"}}, nil, fail); err == nil {
		t.Fatal("relativize(diagnostic) error = nil")
	}
	if err := relativize("/repo", nil, []shared.Suppression{{Filename: "/repo/a.go"}}, fail); err == nil {
		t.Fatal("relativize(suppression) error = nil")
	}
}

func TestRepositoryPathRejectsFailureAndEscape(t *testing.T) {
	t.Parallel()

	if _, err := repositoryPathWithRel("/repo", "/repo/a.go", func(string, string) (string, error) {
		return "", errors.New("different volume")
	}); err == nil {
		t.Fatal("repositoryPathWithRel() error = nil")
	}
	for _, relative := range []string{"..", "../secret.go"} {
		if _, err := repositoryPathWithRel("/repo", "/secret.go", func(string, string) (string, error) {
			return relative, nil
		}); err == nil {
			t.Fatalf("repositoryPathWithRel() accepted %q", relative)
		}
	}
	got, err := repositoryPathWithRel("/repo", "/repo/a.go", func(string, string) (string, error) {
		return "internal\\a.go", nil
	})
	if err != nil || got == "" {
		t.Fatalf("repositoryPathWithRel() = %q, %v", got, err)
	}
}

func TestCollectDiagnosticsMapsRuleMetadata(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file := fset.AddFile("/repo/a.go", -1, 20)
	file.AddLine(10)
	analyzer := &toolanalysis.Analyzer{Name: "test"}
	graph := &checker.Graph{Roots: []*checker.Action{{
		Analyzer: analyzer,
		Package: &packages.Package{
			PkgPath: "example.com/p",
			Fset:    fset,
			Types:   types.NewPackage("example.com/p", "p"),
		},
		Diagnostics: []toolanalysis.Diagnostic{{Pos: token.Pos(11), Message: "finding"}},
	}}}
	rule := shared.Rule{ID: "security/test", Rationale: "why", Remediation: "fix"}
	diagnostics, err := collectDiagnostics(graph, map[*toolanalysis.Analyzer]analyzerSpec{
		analyzer: {analyzer: analyzer, rule: rule},
	}, nil)
	if err != nil || len(diagnostics) != 1 || diagnostics[0].Rule != rule.ID {
		t.Fatalf("collectDiagnostics() = %#v, %v", diagnostics, err)
	}
	if _, err := collectDiagnosticsLimited(
		graph,
		map[*toolanalysis.Analyzer]analyzerSpec{analyzer: {analyzer: analyzer, rule: rule}},
		nil,
		0,
	); err == nil || !strings.Contains(err.Error(), "diagnostics exceed") {
		t.Fatalf("collectDiagnosticsLimited() error = %v", err)
	}
}

func TestEnsureReportCapacityBoundsEntries(t *testing.T) {
	t.Parallel()

	if err := ensureReportCapacity(maxReportEntries, maxReportEntries-1, 1, "diagnostics"); err != nil {
		t.Fatalf("ensureReportCapacity(exact limit) error = %v", err)
	}
	if err := ensureReportCapacity(maxReportEntries, maxReportEntries, 1, "diagnostics"); err == nil ||
		!strings.Contains(err.Error(), "diagnostics exceed") {
		t.Fatalf("ensureReportCapacity(over limit) error = %v", err)
	}
	if err := ensureReportCapacity(maxReportEntries, 0, -1, "diagnostics"); err == nil {
		t.Fatal("ensureReportCapacity() accepted a negative addition")
	}
}

func TestCollectSuppressionsRejectsReportOverflow(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "/repo/a.go", `package p
//analysis:ignore security/test -- reviewed boundary
var Value int
`, parser.ParseComments)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	_, err = collectSuppressionsLimited([]*packages.Package{{
		PkgPath: "example.com/p",
		Fset:    fset,
		Syntax:  []*ast.File{file},
	}}, []string{"security/test"}, time.Now(), nil, 0)
	if err == nil || !strings.Contains(err.Error(), "suppressions exceed") {
		t.Fatalf("collectSuppressionsLimited() error = %v", err)
	}
}

func withRegistry(value dependencies, replacement func() (*policy.Registry, error)) dependencies {
	value.registry = replacement
	return value
}

func withLoad(value dependencies, replacement func(*packages.Config, ...string) ([]*packages.Package, error)) dependencies {
	value.load = replacement
	return value
}

func withAnalyze(value dependencies, replacement func([]*toolanalysis.Analyzer, []*packages.Package, *checker.Options) (*checker.Graph, error)) dependencies {
	value.analyze = replacement
	return value
}

func writeInternalPolicy(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "analysis.yml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	return path
}
