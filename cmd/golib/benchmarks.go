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

type benchmarkCatalog struct {
	SchemaVersion int               `json:"schema_version"`
	Repository    string            `json:"repository"`
	GoVersion     string            `json:"go_version"`
	Modules       []benchmarkModule `json:"modules"`
}

type benchmarkModule struct {
	Directory         string   `json:"directory"`
	ModulePath        string   `json:"module_path"`
	Kind              string   `json:"kind"`
	Releasable        bool     `json:"releasable"`
	BenchmarkRequired bool     `json:"benchmark_required"`
	BenchmarkFiles    []string `json:"benchmark_files"`
	Documentation     []string `json:"documentation"`
	BaselineArtifacts []string `json:"baseline_artifacts"`
	HarnessModules    []string `json:"harness_modules"`
	RequiredServices  []string `json:"required_services"`
}

func discoverBenchmarkCatalog(root string, current catalog) (benchmarkCatalog, error) {
	result := benchmarkCatalog{
		SchemaVersion: 1,
		Repository:    canonicalRoot,
		GoVersion:     current.GoVersion,
		Modules:       make([]benchmarkModule, 0, len(current.Modules)),
	}

	for _, item := range current.Modules {
		benchmarkFiles, documentation, baselines, err := discoverModuleBenchmarkAssets(root, item, current)
		if err != nil {
			return benchmarkCatalog{}, fmt.Errorf("discover benchmark assets for %s: %w", item.Directory, err)
		}
		result.Modules = append(result.Modules, benchmarkModule{
			Directory:         item.Directory,
			ModulePath:        item.Path,
			Kind:              item.Kind,
			Releasable:        item.Releasable,
			BenchmarkRequired: item.Gates["benchmarks"],
			BenchmarkFiles:    benchmarkFiles,
			Documentation:     documentation,
			BaselineArtifacts: baselines,
			HarnessModules:    benchmarkHarnesses(item, current),
			RequiredServices:  cloneStrings(item.RequiredServices),
		})
	}

	return result, nil
}

func discoverModuleBenchmarkAssets(root string, item module, current catalog) ([]string, []string, []string, error) {
	benchmarkFiles := []string{}
	documentation := []string{}
	baselines := []string{}
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
			if relative == ".git" || relative == ".artifacts" || strings.Contains(relative, "/.artifacts") {
				return filepath.SkipDir
			}
			if relative != item.Directory && nestedModules[relative] {
				return filepath.SkipDir
			}
			return nil
		}
		if relative == "docs/benchmark-catalog.md" {
			return nil
		}

		lower := strings.ToLower(relative)
		base := strings.ToLower(entry.Name())
		if strings.HasSuffix(base, "_test.go") {
			containsBenchmark, err := containsGoBenchmark(path)
			if err != nil {
				return err
			}
			if containsBenchmark {
				benchmarkFiles = append(benchmarkFiles, relative)
			}
		}
		if strings.HasSuffix(base, ".md") && (strings.Contains(lower, "benchmark") || strings.Contains(lower, "performance")) {
			documentation = append(documentation, relative)
		}
		if strings.Contains(lower, "/benchmarks/raw/") || strings.Contains(lower, "/benchmarks/results/") || strings.Contains(lower, "/benchmark-results/") {
			baselines = append(baselines, relative)
		}
		return nil
	})
	if err != nil {
		return nil, nil, nil, err
	}

	sort.Strings(benchmarkFiles)
	sort.Strings(documentation)
	sort.Strings(baselines)
	return benchmarkFiles, documentation, baselines, nil
}

func containsGoBenchmark(path string) (bool, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Recv == nil && strings.HasPrefix(function.Name.Name, "Benchmark") {
			return true, nil
		}
	}
	return false, nil
}

func nestedModuleDirectories(item module, current catalog) map[string]bool {
	result := map[string]bool{}
	prefix := ""
	if item.Directory != "." {
		prefix = item.Directory + "/"
	}
	for _, candidate := range current.Modules {
		if candidate.Directory != item.Directory && strings.HasPrefix(candidate.Directory, prefix) {
			result[candidate.Directory] = true
		}
	}
	return result
}

func benchmarkHarnesses(item module, current catalog) []string {
	harnesses := []string{}
	if item.Directory == "." {
		return harnesses
	}
	prefix := item.Directory + "/"
	for _, candidate := range current.Modules {
		if strings.Contains(candidate.Kind, "benchmark") && strings.HasPrefix(candidate.Directory, prefix) {
			if nearestReleasableBenchmarkOwner(candidate, current) == item.Directory {
				harnesses = append(harnesses, candidate.Directory)
			}
		}
	}
	sort.Strings(harnesses)
	return harnesses
}

func nearestReleasableBenchmarkOwner(harness module, current catalog) string {
	owner := ""
	for _, candidate := range current.Modules {
		if !candidate.Releasable || candidate.Directory == "." {
			continue
		}
		if strings.HasPrefix(harness.Directory, candidate.Directory+"/") && len(candidate.Directory) > len(owner) {
			owner = candidate.Directory
		}
	}
	return owner
}

func cloneStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return append([]string{}, values...)
}

func benchmarkCatalogDocumentation(current benchmarkCatalog) string {
	var output strings.Builder
	output.WriteString("# Benchmark Catalog\n\n")
	output.WriteString("Generated by `go run ./cmd/golib manifest`; do not edit manually.\n\n")
	output.WriteString("This inventory reports discoverable benchmark assets. A passing benchmark gate proves execution, while comparative fairness and release baselines require separate reviewed evidence.\n\n")
	output.WriteString("| Module | Gate | Assets | Benchmark files | Documentation | Baselines | Harnesses | Services |\n")
	output.WriteString("| --- | --- | --- | ---: | ---: | ---: | ---: | --- |\n")
	for _, item := range current.Modules {
		gate := "not required"
		if item.BenchmarkRequired {
			gate = "required"
		}
		assetState := "present"
		if len(item.BenchmarkFiles) == 0 && len(item.HarnessModules) == 0 {
			assetState = "missing"
		}
		fmt.Fprintf(
			&output,
			"| `%s` | %s | %s | %d | %d | %d | %d | %s |\n",
			item.Directory,
			gate,
			assetState,
			len(item.BenchmarkFiles),
			len(item.Documentation),
			len(item.BaselineArtifacts),
			len(item.HarnessModules),
			markdownList(item.RequiredServices),
		)
	}
	return output.String()
}

func writeBenchmarkCatalog(root string, current catalog) {
	benchmarks, err := discoverBenchmarkCatalog(root, current)
	if err != nil {
		fatal("discover benchmark catalog: %v", err)
	}
	writeJSON(filepath.Join(root, "benchmarks.json"), benchmarks)
	writeText(filepath.Join(root, "docs", "benchmark-catalog.md"), benchmarkCatalogDocumentation(benchmarks))
}

func validateBenchmarkCatalog(root string, current catalog) {
	wanted, err := discoverBenchmarkCatalog(root, current)
	if err != nil {
		fatal("discover benchmark catalog: %v", err)
	}
	actual := benchmarkCatalog{}
	readJSON(filepath.Join(root, "benchmarks.json"), &actual)
	if !equalJSON(actual, wanted) {
		fatal("benchmarks.json is stale; run `make manifests`")
	}
	wantedDocumentation := benchmarkCatalogDocumentation(wanted)
	actualDocumentation, err := os.ReadFile(filepath.Join(root, "docs", "benchmark-catalog.md"))
	if err != nil {
		fatal("read generated documentation docs/benchmark-catalog.md: %v", err)
	}
	if string(actualDocumentation) != wantedDocumentation {
		fatal("docs/benchmark-catalog.md is stale; run `make manifests`")
	}
}
