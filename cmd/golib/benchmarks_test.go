package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiscoverBenchmarkCatalogAttributesAssetsToOwningModules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "pkg", "clock", "clock_test.go"), "package clock\n\nfunc BenchmarkNow(b *testing.B) {}\n")
	mustWriteFile(t, filepath.Join(root, "pkg", "clock", "docs", "performance.md"), "# Performance\n")
	mustWriteFile(t, filepath.Join(root, "pkg", "clock", "docs", "benchmarks", "raw", "baseline.txt"), "raw\n")
	mustWriteFile(t, filepath.Join(root, "pkg", "clock", "benchmarks", "comparison", "go.mod"), "module example.com/clock-benchmarks\n")
	mustWriteFile(t, filepath.Join(root, "pkg", "clock", "benchmarks", "comparison", "comparison_benchmark_test.go"), "package comparison\n\nfunc BenchmarkComparison(b *testing.B) {}\n")

	current := catalog{Modules: []module{
		{
			Directory:        "pkg/clock",
			Path:             "example.com/clock",
			Kind:             "public library",
			Releasable:       true,
			RequiredServices: []string{"valkey"},
			Gates:            map[string]bool{"benchmarks": true},
		},
		{
			Directory:  "pkg/clock/benchmarks/comparison",
			Path:       "example.com/clock-benchmarks",
			Kind:       "benchmark harness",
			Releasable: false,
			Gates:      map[string]bool{"benchmarks": true},
		},
	}}

	got, err := discoverBenchmarkCatalog(root, current)
	if err != nil {
		t.Fatalf("discoverBenchmarkCatalog() error = %v", err)
	}
	if len(got.Modules) != 2 {
		t.Fatalf("discoverBenchmarkCatalog() modules = %d, want 2", len(got.Modules))
	}

	clock := got.Modules[0]
	if !reflect.DeepEqual(clock.BenchmarkFiles, []string{"pkg/clock/clock_test.go"}) {
		t.Fatalf("clock benchmark files = %#v", clock.BenchmarkFiles)
	}
	if !reflect.DeepEqual(clock.Documentation, []string{"pkg/clock/docs/performance.md"}) {
		t.Fatalf("clock documentation = %#v", clock.Documentation)
	}
	if !reflect.DeepEqual(clock.BaselineArtifacts, []string{"pkg/clock/docs/benchmarks/raw/baseline.txt"}) {
		t.Fatalf("clock baseline artifacts = %#v", clock.BaselineArtifacts)
	}
	if !reflect.DeepEqual(clock.HarnessModules, []string{"pkg/clock/benchmarks/comparison"}) {
		t.Fatalf("clock harness modules = %#v", clock.HarnessModules)
	}
	if !reflect.DeepEqual(clock.RequiredServices, []string{"valkey"}) {
		t.Fatalf("clock required services = %#v", clock.RequiredServices)
	}

	harness := got.Modules[1]
	if !reflect.DeepEqual(harness.BenchmarkFiles, []string{"pkg/clock/benchmarks/comparison/comparison_benchmark_test.go"}) {
		t.Fatalf("harness benchmark files = %#v", harness.BenchmarkFiles)
	}
}

func TestBenchmarkCatalogDocumentationReportsMissingAssets(t *testing.T) {
	t.Parallel()

	contents := benchmarkCatalogDocumentation(benchmarkCatalog{Modules: []benchmarkModule{
		{
			Directory:         "pkg/clock",
			ModulePath:        "example.com/clock",
			BenchmarkRequired: true,
		},
	}})

	for _, expected := range []string{"# Benchmark Catalog", "`pkg/clock`", "required", "missing", "0"} {
		if !strings.Contains(contents, expected) {
			t.Fatalf("benchmark catalog documentation does not contain %q:\n%s", expected, contents)
		}
	}
}

func TestDiscoverBenchmarkCatalogExcludesGeneratedCatalog(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "docs", "performance.md"), "# Performance\n")
	mustWriteFile(t, filepath.Join(root, "docs", "benchmark-catalog.md"), "# Generated\n")

	got, err := discoverBenchmarkCatalog(root, catalog{Modules: []module{{
		Directory: ".",
		Path:      canonicalRoot,
		Gates:     map[string]bool{"benchmarks": false},
	}}})
	if err != nil {
		t.Fatalf("discoverBenchmarkCatalog() error = %v", err)
	}
	if !reflect.DeepEqual(got.Modules[0].Documentation, []string{"docs/performance.md"}) {
		t.Fatalf("root documentation = %#v", got.Modules[0].Documentation)
	}
}
