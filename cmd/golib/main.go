package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

const (
	canonicalRoot = "github.com/faustbrian/golib"
	requiredGo    = "1.26.5"
)

type catalog struct {
	SchemaVersion int      `json:"schema_version"`
	Repository    string   `json:"repository"`
	GoVersion     string   `json:"go_version"`
	Modules       []module `json:"modules"`
}

type module struct {
	Directory           string          `json:"directory"`
	Path                string          `json:"module_path"`
	GoVersion           string          `json:"go_version"`
	Packages            []packageInfo   `json:"packages"`
	Purpose             string          `json:"purpose"`
	Lifecycle           string          `json:"lifecycle"`
	Kind                string          `json:"kind"`
	Releasable          bool            `json:"releasable"`
	Version             string          `json:"version"`
	TagPrefix           string          `json:"tag_prefix,omitempty"`
	OwnedDependencies   []string        `json:"owned_dependencies"`
	ReverseDependencies []string        `json:"reverse_owned_dependencies"`
	ExternalRuntime     []string        `json:"external_runtime_dependencies"`
	RequiredServices    []string        `json:"required_services"`
	TestTags            []string        `json:"test_tags"`
	InteropTools        []string        `json:"interoperability_tools"`
	Specifications      []string        `json:"specifications"`
	ConformanceCorpora  []string        `json:"conformance_corpora"`
	Provenance          []string        `json:"provenance"`
	Goals               []string        `json:"goal_files"`
	GoalStatus          string          `json:"goal_status"`
	GoalEvidence        []goalEvidence  `json:"goal_evidence"`
	Gates               map[string]bool `json:"gates"`
}

type goalEvidence struct {
	File                   string   `json:"file"`
	RequirementsSHA256     string   `json:"requirements_sha256"`
	ImplementationEvidence []string `json:"implementation_evidence"`
	VerificationGates      []string `json:"verification_gates"`
	ImplementationStatus   string   `json:"implementation_status"`
}

type packageInfo struct {
	ModuleDirectory  string `json:"module_directory"`
	Directory        string `json:"directory"`
	Name             string `json:"name"`
	Import           string `json:"import_path"`
	Kind             string `json:"kind"`
	Production       bool   `json:"production"`
	Executable       bool   `json:"executable"`
	CoverageRequired bool   `json:"coverage_required"`
}

type modFile struct {
	Module struct {
		Path string
	}
	Go      string
	Require []struct {
		Path     string
		Indirect bool
	}
	Replace []json.RawMessage
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: golib <manifest|validate|select|safety>")
	}

	root, err := repositoryRoot()
	if err != nil {
		fatal("%v", err)
	}

	switch os.Args[1] {
	case "manifest":
		manifest(root)
	case "validate":
		validate(root)
	case "select":
		selectModules(root, os.Args[2:])
	case "safety":
		safety(root, os.Args[2:])
	default:
		fatal("unknown command %q", os.Args[1])
	}
}

func repositoryRoot() (string, error) {
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

func manifest(root string) {
	current, err := discover(root)
	if err != nil {
		fatal("discover modules: %v", err)
	}

	writeJSON(filepath.Join(root, "modules.json"), current)

	type packageCatalog struct {
		SchemaVersion int           `json:"schema_version"`
		Repository    string        `json:"repository"`
		Packages      []packageInfo `json:"packages"`
	}

	packages := make([]packageInfo, 0)
	for _, item := range current.Modules {
		packages = append(packages, item.Packages...)
	}
	sort.Slice(packages, func(left, right int) bool {
		return packages[left].Import < packages[right].Import
	})
	writeJSON(filepath.Join(root, "packages.json"), packageCatalog{1, canonicalRoot, packages})
	writeCatalogDocumentation(root, current)
}

func validate(root string) {
	wanted, err := discover(root)
	if err != nil {
		fatal("discover modules: %v", err)
	}

	actual := catalog{}
	readJSON(filepath.Join(root, "modules.json"), &actual)
	if !equalJSON(actual, wanted) {
		fatal("modules.json is stale; run `make manifests`")
	}

	type packageCatalog struct {
		SchemaVersion int           `json:"schema_version"`
		Repository    string        `json:"repository"`
		Packages      []packageInfo `json:"packages"`
	}
	wantedPackages := packageCatalog{SchemaVersion: 1, Repository: canonicalRoot}
	for _, item := range wanted.Modules {
		wantedPackages.Packages = append(wantedPackages.Packages, item.Packages...)
	}
	sort.Slice(wantedPackages.Packages, func(left, right int) bool {
		return wantedPackages.Packages[left].Import < wantedPackages.Packages[right].Import
	})
	actualPackages := packageCatalog{}
	readJSON(filepath.Join(root, "packages.json"), &actualPackages)
	if !equalJSON(actualPackages, wantedPackages) {
		fatal("packages.json is stale; run `make manifests`")
	}
	for path, expected := range catalogDocumentation(wanted) {
		actual, readErr := os.ReadFile(filepath.Join(root, path))
		if readErr != nil {
			fatal("read generated documentation %s: %v", path, readErr)
		}
		if string(actual) != expected {
			fatal("%s is stale; run `make manifests`", path)
		}
	}

	validateWorkspace(root, wanted)
	validatePaths(root)
	fmt.Printf("validated %d modules and %d packages\n", len(wanted.Modules), packageCount(wanted))
}

func selectModules(root string, arguments []string) {
	flags := flag.NewFlagSet("select", flag.ExitOnError)
	all := flags.Bool("all", false, "select every active module")
	changed := flags.String("changed", "", "select changes since this git revision")
	explicit := flags.String("modules", "", "comma-separated module directories or paths")
	outputFormat := flags.String("format", "text", "output format: text, json, or matrix")
	order := flags.String("order", "directory", "selection order: directory or dependency")
	if err := flags.Parse(arguments); err != nil {
		fatal("parse selection: %v", err)
	}

	current, err := discover(root)
	if err != nil {
		fatal("discover modules: %v", err)
	}
	selected := map[string]bool{}

	if *all {
		for _, item := range current.Modules {
			if item.Kind != "fixture" {
				selected[item.Directory] = true
			}
		}
	}
	if *explicit != "" {
		for value := range strings.SplitSeq(*explicit, ",") {
			resolved := resolveModule(current, strings.TrimSpace(value))
			if resolved == "" {
				fatal("unknown module %q", value)
			}
			selected[resolved] = true
		}
	}
	if *changed != "" {
		for _, directory := range changedModules(root, current, *changed) {
			selected[directory] = true
		}
	}
	if !*all && *explicit == "" && *changed == "" {
		fatal("one of --all, --changed, or --modules is required")
	}

	if *changed != "" {
		expandReverseDependencies(current, selected)
	}
	result := make([]string, 0, len(selected))
	for directory := range selected {
		result = append(result, directory)
	}
	sort.Strings(result)
	switch *order {
	case "directory":
	case "dependency":
		result = dependencyOrderedDirectories(current, result)
		if len(result) != len(selected) {
			fatal("selected module dependency graph contains a cycle")
		}
	default:
		fatal("unsupported selection order %q", *order)
	}
	switch *outputFormat {
	case "text":
		fmt.Println(strings.Join(result, "\n"))
	case "json":
		encoded, err := json.Marshal(result)
		if err != nil {
			fatal("encode selection: %v", err)
		}
		fmt.Println(string(encoded))
	case "matrix":
		type matrixEntry struct {
			Directory string `json:"directory"`
			Artifact  string `json:"artifact"`
		}
		matrix := make([]matrixEntry, 0, len(result))
		for _, directory := range result {
			artifact := strings.NewReplacer("/", "-", ".", "root").Replace(directory)
			matrix = append(matrix, matrixEntry{Directory: directory, Artifact: artifact})
		}
		encoded, err := json.Marshal(matrix)
		if err != nil {
			fatal("encode matrix selection: %v", err)
		}
		fmt.Println(string(encoded))
	default:
		fatal("unsupported selection format %q", *outputFormat)
	}
}

func dependencyOrderedDirectories(current catalog, directories []string) []string {
	selected := map[string]bool{}
	byPath := map[string]module{}
	for _, directory := range directories {
		selected[directory] = true
	}
	for _, item := range current.Modules {
		byPath[item.Path] = item
	}

	pending := map[string]int{}
	dependants := map[string][]string{}
	for _, item := range current.Modules {
		if !selected[item.Directory] {
			continue
		}
		for _, dependencyPath := range item.OwnedDependencies {
			dependency, exists := byPath[dependencyPath]
			if !exists || !selected[dependency.Directory] {
				continue
			}
			pending[item.Directory]++
			dependants[dependency.Directory] = append(
				dependants[dependency.Directory],
				item.Directory,
			)
		}
	}

	ready := make([]string, 0, len(directories))
	for _, directory := range directories {
		if pending[directory] == 0 {
			ready = append(ready, directory)
		}
	}
	sort.Strings(ready)

	ordered := make([]string, 0, len(directories))
	for len(ready) != 0 {
		directory := ready[0]
		ready = ready[1:]
		ordered = append(ordered, directory)
		for _, dependant := range dependants[directory] {
			pending[dependant]--
			if pending[dependant] == 0 {
				ready = append(ready, dependant)
				sort.Strings(ready)
			}
		}
	}
	return ordered
}

func discover(root string) (catalog, error) {
	directories, err := moduleDirectories(root)
	if err != nil {
		return catalog{}, err
	}
	verificationGates, err := canonicalGates(root)
	if err != nil {
		return catalog{}, err
	}

	result := catalog{SchemaVersion: 1, Repository: canonicalRoot, GoVersion: requiredGo}
	paths := map[string]string{}
	for _, directory := range directories {
		parsed, parseErr := parseMod(filepath.Join(root, directory, "go.mod"))
		if parseErr != nil {
			return catalog{}, parseErr
		}
		if previous, exists := paths[parsed.Module.Path]; exists {
			return catalog{}, fmt.Errorf("duplicate module path %s in %s and %s", parsed.Module.Path, previous, directory)
		}
		paths[parsed.Module.Path] = directory
	}

	for _, directory := range directories {
		parsed, parseErr := parseMod(filepath.Join(root, directory, "go.mod"))
		if parseErr != nil {
			return catalog{}, parseErr
		}
		kind, releasable := classify(directory)
		if licenseErr := validateModuleLicense(root, directory, releasable); licenseErr != nil {
			return catalog{}, licenseErr
		}
		if directory != "." && kind != "fixture" {
			expected := canonicalRoot + "/" + directory
			if parsed.Module.Path != expected {
				return catalog{}, fmt.Errorf("module %s has path %s; expected %s", directory, parsed.Module.Path, expected)
			}
		}
		if parsed.Go != requiredGo && kind != "fixture" {
			return catalog{}, fmt.Errorf("module %s uses Go %s; expected %s", directory, parsed.Go, requiredGo)
		}
		if len(parsed.Replace) != 0 {
			return catalog{}, fmt.Errorf("module %s contains replace directives", directory)
		}

		owned := make([]string, 0)
		external := make([]string, 0)
		for _, requirement := range parsed.Require {
			if strings.HasPrefix(requirement.Path, canonicalRoot+"/") {
				owned = append(owned, requirement.Path)
			} else if !requirement.Indirect {
				external = append(external, requirement.Path)
			}
		}
		sort.Strings(owned)
		sort.Strings(external)

		packages, packageErr := discoverPackages(root, directory, parsed.Module.Path, kind)
		if packageErr != nil {
			return catalog{}, fmt.Errorf("discover packages in %s: %w", directory, packageErr)
		}
		hasDefaultFiles, defaultFilesErr := hasDefaultGoFiles(root, directory)
		if defaultFilesErr != nil {
			return catalog{}, fmt.Errorf("inspect default Go files in %s: %w", directory, defaultFilesErr)
		}
		testTags, testTagsErr := requiredTestTags(root, directory)
		if testTagsErr != nil {
			return catalog{}, fmt.Errorf("discover test tags in %s: %w", directory, testTagsErr)
		}
		moduleSpecifications := specifications(directory)
		moduleCorpora := conformanceCorpora(directory)
		moduleGates := gates(kind, hasDefaultFiles)
		moduleGates["conformance"] = conformanceRequired(
			kind,
			moduleSpecifications,
			moduleCorpora,
		)
		goals := goalFiles(root, directory)
		goalRecords, goalErr := goalEvidenceFor(
			root,
			directory,
			goals,
			verificationGates,
		)
		if goalErr != nil {
			return catalog{}, goalErr
		}
		goalStatus := "not-applicable"
		if len(goalRecords) != 0 {
			goalStatus = "implementation-evidence-inventoried"
		}
		result.Modules = append(result.Modules, module{
			Directory:          directory,
			Path:               parsed.Module.Path,
			GoVersion:          parsed.Go,
			Packages:           packages,
			Purpose:            purpose(root, directory),
			Lifecycle:          lifecycle(directory, kind),
			Kind:               kind,
			Releasable:         releasable,
			Version:            "unreleased",
			TagPrefix:          tagPrefix(directory, releasable),
			OwnedDependencies:  owned,
			ExternalRuntime:    external,
			RequiredServices:   requiredServices(directory),
			TestTags:           testTags,
			InteropTools:       interoperabilityTools(directory),
			Specifications:     moduleSpecifications,
			ConformanceCorpora: moduleCorpora,
			Provenance:         provenanceFiles(root, directory),
			Goals:              goals,
			GoalStatus:         goalStatus,
			GoalEvidence:       goalRecords,
			Gates:              moduleGates,
		})
	}

	for index := range result.Modules {
		for _, candidate := range result.Modules {
			if slices.Contains(candidate.OwnedDependencies, result.Modules[index].Path) {
				result.Modules[index].ReverseDependencies = append(
					result.Modules[index].ReverseDependencies,
					candidate.Path,
				)
			}
		}
		sort.Strings(result.Modules[index].ReverseDependencies)
	}
	if cycle := dependencyCycle(result); len(cycle) != 0 {
		return catalog{}, fmt.Errorf("owned module dependency cycle: %s", strings.Join(cycle, " -> "))
	}

	return result, nil
}

func conformanceRequired(kind string, specifications, corpora []string) bool {
	return kind == "public library" && (len(specifications) != 0 || len(corpora) != 0)
}

func validateModuleLicense(root, directory string, releasable bool) error {
	if !releasable {
		return nil
	}

	path := filepath.Join(root, directory, "LICENSE")
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("releasable module %s license: %w", directory, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("releasable module %s license is not a nonempty regular file", directory)
	}

	return nil
}

func moduleDirectories(root string) ([]string, error) {
	directories := []string{"."}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != root && excludedModuleDirectory(entry.Name()) {
			return filepath.SkipDir
		}
		if entry.IsDir() || entry.Name() != "go.mod" || path == filepath.Join(root, "go.mod") {
			return nil
		}
		relative, relativeErr := filepath.Rel(root, filepath.Dir(path))
		if relativeErr != nil {
			return relativeErr
		}
		directories = append(directories, filepath.ToSlash(relative))
		return nil
	})
	sort.Strings(directories)
	return directories, err
}

func excludedModuleDirectory(name string) bool {
	return name == ".artifacts" || name == ".git" || name == ".tools" ||
		name == "node_modules" || name == "vendor"
}

func parseMod(path string) (modFile, error) {
	command := exec.Command("go", "mod", "edit", "-json", path)
	output, err := command.Output()
	if err != nil {
		return modFile{}, fmt.Errorf("parse %s: %w", path, err)
	}
	parsed := modFile{}
	if err := json.Unmarshal(output, &parsed); err != nil {
		return modFile{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return parsed, nil
}

func classify(directory string) (string, bool) {
	switch {
	case directory == ".":
		return "internal tool", false
	case strings.Contains(directory, "/testdata/"):
		return "fixture", false
	case strings.HasPrefix(directory, "cmd/"), strings.HasPrefix(directory, "internal/"):
		return "internal tool", false
	case strings.Contains(directory, "/benchmarks"):
		return "benchmark harness", false
	case strings.Contains(directory, "/interoperability"), strings.Contains(directory, "/compatibility"), strings.Contains(directory, "/integration/"):
		return "interoperability harness", false
	case strings.Contains(directory, "/examples/"):
		return "example", false
	case strings.Contains(directory, "/adapters/") || strings.Contains(directory, "/objective/"):
		return "adapter", true
	default:
		return "public library", true
	}
}

func discoverPackages(
	root, moduleDirectory, modulePath, moduleKind string,
) ([]packageInfo, error) {
	base := filepath.Join(root, moduleDirectory)
	type packageRecord struct {
		name       string
		executable bool
	}
	packages := map[string]packageRecord{}
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != base && excludedSourceDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			if path != base {
				if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(
			token.NewFileSet(), path, nil, parser.SkipObjectResolution,
		)
		if parseErr != nil {
			return parseErr
		}
		relative, relativeErr := filepath.Rel(base, filepath.Dir(path))
		if relativeErr != nil {
			return relativeErr
		}
		directory := filepath.ToSlash(relative)
		record := packages[directory]
		if record.name != "" && record.name != file.Name.Name {
			return fmt.Errorf("directory %s contains packages %s and %s", directory, record.name, file.Name.Name)
		}
		record.name = file.Name.Name
		matched, matchErr := build.Default.MatchFile(filepath.Dir(path), filepath.Base(path))
		if matchErr != nil {
			return matchErr
		}
		if matched && executableFile(file) {
			record.executable = true
		}
		packages[directory] = record
		return nil
	})
	if err != nil {
		return nil, err
	}

	result := make([]packageInfo, 0, len(packages))
	for directory, record := range packages {
		importPath := modulePath
		if directory != "." {
			importPath += "/" + directory
		}
		if err := validateOwnedCommandName(moduleKind, directory, record.name); err != nil {
			return nil, err
		}
		kind, production := classifyPackage(moduleKind, directory, record.name)
		result = append(result, packageInfo{
			ModuleDirectory:  moduleDirectory,
			Directory:        directory,
			Name:             record.name,
			Import:           importPath,
			Kind:             kind,
			Production:       production,
			Executable:       record.executable,
			CoverageRequired: production && record.executable,
		})
	}
	sort.Slice(result, func(left, right int) bool { return result[left].Import < result[right].Import })
	return result, nil
}

func validateOwnedCommandName(moduleKind, directory, packageName string) error {
	if packageName != "main" || moduleKind == "benchmark harness" ||
		moduleKind == "interoperability harness" || moduleKind == "fixture" {
		return nil
	}
	segments := strings.Split(filepath.ToSlash(directory), "/")
	if !slices.Contains(segments, "cmd") {
		return nil
	}
	name := segments[len(segments)-1]
	if strings.HasPrefix(name, "go-") {
		return fmt.Errorf(
			"repository-owned command %s uses forbidden standalone go- prefix; use an unambiguous domain name or golib-*",
			directory,
		)
	}
	return nil
}

func excludedSourceDirectory(name string) bool {
	return name == ".artifacts" || name == ".git" || name == ".tools" ||
		name == "node_modules" || name == "testdata" || name == "vendor" ||
		strings.HasPrefix(name, "_")
}

func executableFile(file *ast.File) bool {
	executable := false
	ast.Inspect(file, func(node ast.Node) bool {
		block, ok := node.(*ast.BlockStmt)
		if ok && len(block.List) != 0 {
			executable = true
			return false
		}
		return !executable
	})
	return executable
}

func classifyPackage(moduleKind, directory, name string) (string, bool) {
	if moduleKind == "fixture" {
		return "fixture", false
	}
	if moduleKind == "benchmark harness" || moduleKind == "interoperability harness" {
		return "harness", false
	}
	segments := strings.Split(filepath.ToSlash(directory), "/")
	base := segments[len(segments)-1]
	if slices.Contains(segments, "scripts") ||
		(slices.Contains(segments, "internal") && slices.Contains(segments, "cmd")) ||
		base == "international-dataset-review" || base == "international-generate" ||
		base == "generate-reference" || base == "process-fixture" ||
		base == "referenceapp" || base == "semver" || base == "semvercheck" {
		return "tooling", false
	}
	if slices.Contains(segments, "examples") {
		return "example", false
	}
	if base == "conformance" || base == "coveragecheck" || base == "mocks" ||
		slices.Contains(segments, "testutil") ||
		strings.HasPrefix(base, "test") || strings.HasSuffix(base, "test") ||
		strings.HasSuffix(base, "testkit") || strings.HasSuffix(name, "test") ||
		strings.HasSuffix(name, "testkit") {
		return "test support", false
	}
	production := moduleKind == "public library" || moduleKind == "adapter"
	if name == "main" {
		return "command", production
	}
	if slices.Contains(segments, "internal") {
		return "internal", production
	}
	return "public", production
}

func hasDefaultGoFiles(root, moduleDirectory string) (bool, error) {
	base := filepath.Join(root, moduleDirectory)
	found := false
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != base && excludedSourceDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			if path != base {
				_, statErr := os.Stat(filepath.Join(path, "go.mod"))
				if statErr == nil {
					return filepath.SkipDir
				}
				if !errors.Is(statErr, os.ErrNotExist) {
					return statErr
				}
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		matched, matchErr := build.Default.MatchFile(filepath.Dir(path), filepath.Base(path))
		if matchErr != nil {
			return matchErr
		}
		if matched {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return found, nil
}

func requiredTestTags(root, moduleDirectory string) ([]string, error) {
	base := filepath.Join(root, moduleDirectory)
	tags := []string{}
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != base && excludedSourceDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			if path != base {
				_, statErr := os.Stat(filepath.Join(path, "go.mod"))
				if statErr == nil {
					return filepath.SkipDir
				}
				if !errors.Is(statErr, os.ErrNotExist) {
					return statErr
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(data, []byte("//go:build integration")) &&
			!slices.Contains(tags, "integration") {
			tags = append(tags, "integration")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(tags)
	return tags, nil
}

func purpose(root, directory string) string {
	if directory == "." {
		return "Repository-wide orchestration and policy tooling."
	}
	readme, err := os.Open(filepath.Join(root, directory, "README.md"))
	if err != nil {
		return "See the module README and goal files."
	}
	defer func() {
		_ = readme.Close()
	}()
	scanner := bufio.NewScanner(readme)
	paragraph := []string{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			if len(paragraph) != 0 {
				break
			}
			continue
		}
		if len(paragraph) == 0 && (strings.HasPrefix(line, "#") ||
			strings.HasPrefix(line, "[") || strings.HasPrefix(line, "!") ||
			strings.HasPrefix(line, "<")) {
			continue
		}
		paragraph = append(paragraph, line)
	}
	if len(paragraph) != 0 {
		return strings.Join(paragraph, " ")
	}
	return "See the module README and goal files."
}

func lifecycle(directory, kind string) string {
	if directory == "." || kind == "fixture" || strings.Contains(kind, "harness") || kind == "example" {
		return "internal"
	}
	return "pre-v1"
}

func tagPrefix(directory string, releasable bool) string {
	if !releasable {
		return ""
	}
	return directory + "/v"
}

func requiredServices(directory string) []string {
	library := libraryName(directory)
	services := []string{}
	postgresLibraries := []string{
		"api-query", "authorization", "calendar", "feature-flags",
		"idempotency", "lease", "localized", "migrations", "opening-hours",
		"outbox", "postgres", "queue-control-plane", "rate-limit",
		"scheduler", "sequencer", "settings", "state-machine", "temporal",
	}
	valkeyLibraries := []string{
		"authorization", "cache", "feature-flags", "idempotency", "lease",
		"queue", "queue-control-plane", "rate-limit", "scheduler", "settings",
	}
	if slices.Contains(postgresLibraries, library) {
		services = append(services, "postgresql")
	}
	if slices.Contains(valkeyLibraries, library) {
		services = append(services, "valkey")
	}
	if slices.Contains([]string{"cache", "queue", "queue-control-plane"}, library) {
		services = append(services, "redis")
	}
	if library == "queue" {
		services = append(services, "nats", "nsq", "rabbitmq")
	}
	sort.Strings(services)
	return services
}

func interoperabilityTools(directory string) []string {
	switch libraryName(directory) {
	case "wsdl":
		return []string{"Java", "Apache Woden"}
	case "ecma-regexp":
		return []string{"Node.js", "Test262"}
	case "xsd":
		return []string{"Docker", "Eclipse Temurin 25 JAXP"}
	default:
		return []string{}
	}
}

func specifications(directory string) []string {
	prefix := libraryName(directory)
	switch prefix {
	case "json-schema":
		return []string{"JSON Schema drafts 4, 6, 7, 2019-09, and 2020-12", "JSON-Schema-Test-Suite"}
	case "jsonapi":
		return []string{"JSON:API 1.0 and 1.1", "JSON:API extensions and recommendations"}
	case "jsonrpc":
		return []string{"JSON-RPC 2.0"}
	case "openapi":
		return []string{"OpenAPI 2.0, 3.0, and 3.1"}
	case "openrpc":
		return []string{"OpenRPC 1.3"}
	case "wsdl":
		return []string{"WSDL 1.1 and 2.0"}
	case "xsd":
		return []string{"W3C XML Schema 1.0 Second Edition", "W3C XML Schema Test Suite"}
	default:
		return []string{}
	}
}

func conformanceCorpora(directory string) []string {
	prefix := libraryName(directory)
	switch prefix {
	case "ecma-regexp":
		return []string{"TC39 Test262"}
	case "json-schema":
		return []string{"JSON-Schema-Test-Suite", "Bowtie"}
	case "xsd":
		return []string{"W3C XML Schema Test Suite"}
	default:
		return []string{}
	}
}

func libraryName(directory string) string {
	trimmed := strings.TrimPrefix(directory, "pkg/")
	return strings.Split(trimmed, "/")[0]
}

func provenanceFiles(root, directory string) []string {
	candidates := []string{
		"specification/manifest.json",
		"specification/manifest.tsv",
		"specification/provenance.json",
		"testdata/manifest.json",
		"testdata/provenance.json",
	}
	result := []string{}
	for _, candidate := range candidates {
		path := filepath.Join(root, directory, candidate)
		if _, err := os.Stat(path); err == nil {
			result = append(result, filepath.ToSlash(filepath.Join(directory, candidate)))
		}
	}
	return result
}

func goalFiles(root, directory string) []string {
	base := filepath.Join(root, directory, ".ai")
	entries, err := os.ReadDir(base)
	if err != nil {
		return []string{}
	}
	goals := []string{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "GOAL") && strings.HasSuffix(entry.Name(), ".md") {
			goals = append(goals, filepath.ToSlash(filepath.Join(directory, ".ai", entry.Name())))
		}
	}
	sort.Strings(goals)
	return goals
}

func canonicalGates(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, "scripts", "check-gates.txt"))
	if err != nil {
		return nil, fmt.Errorf("read canonical gates: %w", err)
	}
	seen := map[string]bool{}
	result := []string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		gate := strings.TrimSpace(scanner.Text())
		if gate == "" {
			continue
		}
		if seen[gate] {
			return nil, fmt.Errorf("duplicate canonical gate %q", gate)
		}
		seen[gate] = true
		result = append(result, gate)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan canonical gates: %w", err)
	}
	if len(result) == 0 {
		return nil, errors.New("canonical gate contract is empty")
	}
	return result, nil
}

func goalEvidenceFor(
	root string,
	directory string,
	goals []string,
	verificationGates []string,
) ([]goalEvidence, error) {
	result := make([]goalEvidence, 0, len(goals))
	for _, goal := range goals {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(goal)))
		if err != nil {
			return nil, fmt.Errorf("read goal %s: %w", goal, err)
		}
		digest := sha256.Sum256(data)
		evidence, evidenceErr := implementationEvidence(root, directory, goal)
		if evidenceErr != nil {
			return nil, evidenceErr
		}
		if len(evidence) == 0 {
			return nil, fmt.Errorf("goal %s has no implementation evidence", goal)
		}
		result = append(result, goalEvidence{
			File:                   goal,
			RequirementsSHA256:     hex.EncodeToString(digest[:]),
			ImplementationEvidence: evidence,
			VerificationGates:      slices.Clone(verificationGates),
			ImplementationStatus:   "implemented-requires-fresh-verification",
		})
	}
	return result, nil
}

func implementationEvidence(root, directory, goal string) ([]string, error) {
	candidates := []string{
		"README.md",
		"CHANGELOG.md",
		"docs/README.md",
		"docs/api.md",
		"docs/architecture.md",
		"docs/compatibility.md",
		"docs/hardening.md",
		"docs/hardening-audit.md",
		"docs/hardening-report.md",
		"docs/audit-report.md",
		"docs/performance.md",
		"docs/security.md",
		"docs/security/findings.md",
		"docs/security/threat-model.md",
		"docs/threat-model.md",
	}
	result := []string{}
	seen := map[string]bool{}
	add := func(relative string) {
		path := filepath.ToSlash(filepath.Join(directory, relative))
		if directory == "." {
			path = filepath.ToSlash(relative)
		}
		if !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}
	for _, candidate := range candidates {
		path := filepath.Join(root, directory, filepath.FromSlash(candidate))
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			add(candidate)
		}
	}

	goalName := strings.TrimSuffix(filepath.Base(goal), filepath.Ext(goal))
	goalName = strings.TrimPrefix(goalName, "GOAL_")
	tokens := strings.FieldsFunc(strings.ToLower(goalName), func(r rune) bool {
		return r == '_' || r == '-'
	})
	docs := filepath.Join(root, directory, "docs")
	if _, err := os.Stat(docs); err == nil {
		walkErr := filepath.WalkDir(docs, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
				return nil
			}
			lower := strings.ToLower(filepath.ToSlash(path))
			for _, token := range tokens {
				if token != "goal" && token != "harden" && token != "polish" &&
					strings.Contains(lower, token) {
					relative, relativeErr := filepath.Rel(
						filepath.Join(root, directory),
						path,
					)
					if relativeErr != nil {
						return relativeErr
					}
					add(relative)
					break
				}
			}
			return nil
		})
		if walkErr != nil {
			return nil, fmt.Errorf("discover implementation evidence for %s: %w", goal, walkErr)
		}
	}
	sort.Strings(result)
	return result, nil
}

func gates(kind string, hasPackages bool) map[string]bool {
	production := kind == "public library" || kind == "adapter"
	return map[string]bool{
		"api_compatibility": production,
		"benchmarks":        production,
		"coverage":          production,
		"documentation":     kind != "fixture" && hasPackages,
		"fuzz":              production,
		"lint":              kind != "fixture" && hasPackages,
		"mutation":          production,
		"race":              production,
		"security":          kind != "fixture" && hasPackages,
		"tests":             kind != "fixture" && hasPackages,
	}
}

func validateWorkspace(root string, current catalog) {
	data, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err != nil {
		fatal("read go.work: %v", err)
	}
	text := string(data)
	for _, item := range current.Modules {
		if item.Kind == "fixture" {
			continue
		}
		entry := "\t./" + strings.TrimPrefix(item.Directory, "./") + "\n"
		if item.Directory == "." {
			entry = "\t.\n"
		}
		if !strings.Contains(text, entry) {
			fatal("go.work omits active module %s", item.Directory)
		}
	}
}

func validatePaths(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		fatal("read repository root: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "go-") {
			fatal("obsolete top-level go- directory: %s", entry.Name())
		}
	}
	fixtureWorkflowRoot := "pkg/json-schema/testdata/official/JSON-Schema-Test-Suite/.github/workflows/"
	rootWorkflows := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != root && excludedModuleDirectory(entry.Name()) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.Contains(filepath.ToSlash(path), "/.github/workflows/") {
			return nil
		}
		workflow, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil {
			return relativeErr
		}
		workflow = filepath.ToSlash(workflow)
		if strings.HasPrefix(workflow, fixtureWorkflowRoot) {
			return nil
		}
		if workflow != ".github/workflows/ci.yml" {
			return fmt.Errorf("non-authoritative workflow remains: %s", workflow)
		}
		rootWorkflows++
		return nil
	})
	if err != nil {
		fatal("validate workflow topology: %v", err)
	}
	if rootWorkflows != 1 {
		fatal("expected exactly one authoritative root workflow, found %d", rootWorkflows)
	}
	obsoleteRoot := "github.com/faustbrian/" + "go-"
	command := exec.Command("git", "grep", "-n", obsoleteRoot)
	command.Dir = root
	command.Args = append(command.Args, "--", ":(exclude).ai/GOAL_MONOREPO_REMEDIATION.md")
	if output, err := command.Output(); err == nil && len(output) != 0 {
		fatal("obsolete owned module paths remain:\n%s", output)
	}
}

func dependencyCycle(current catalog) []string {
	modules := map[string]module{}
	for _, item := range current.Modules {
		modules[item.Path] = item
	}
	state := map[string]uint8{}
	stack := []string{}
	var visit func(string) []string
	visit = func(path string) []string {
		state[path] = 1
		stack = append(stack, path)
		for _, dependency := range modules[path].OwnedDependencies {
			if _, exists := modules[dependency]; !exists {
				continue
			}
			if state[dependency] == 1 {
				index := slices.Index(stack, dependency)
				return append(slices.Clone(stack[index:]), dependency)
			}
			if state[dependency] == 0 {
				if cycle := visit(dependency); len(cycle) != 0 {
					return cycle
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[path] = 2
		return nil
	}
	for path := range modules {
		if state[path] == 0 {
			if cycle := visit(path); len(cycle) != 0 {
				return cycle
			}
		}
	}
	return nil
}

func changedModules(root string, current catalog, revision string) []string {
	command := exec.Command("git", "diff", "--name-only", revision+"...HEAD")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		fatal("list changed files: %v", err)
	}
	selected := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		best := ""
		for _, item := range current.Modules {
			if item.Directory == "." {
				continue
			}
			if (line == item.Directory || strings.HasPrefix(line, item.Directory+"/")) && len(item.Directory) > len(best) {
				best = item.Directory
			}
		}
		if best == "" || isRootPolicyPath(line) {
			for _, item := range current.Modules {
				if item.Kind != "fixture" {
					selected[item.Directory] = true
				}
			}
			continue
		}
		selected[best] = true
	}
	result := make([]string, 0, len(selected))
	for directory := range selected {
		result = append(result, directory)
	}
	return result
}

func isRootPolicyPath(path string) bool {
	return !strings.Contains(path, "/") || strings.HasPrefix(path, ".github/") || strings.HasPrefix(path, ".ai/") || strings.HasPrefix(path, "cmd/") || strings.HasPrefix(path, "scripts/")
}

func expandReverseDependencies(current catalog, selected map[string]bool) {
	byPath := map[string]module{}
	for _, item := range current.Modules {
		byPath[item.Path] = item
	}
	changed := true
	for changed {
		changed = false
		for _, item := range current.Modules {
			if !selected[item.Directory] {
				continue
			}
			for _, reverse := range item.ReverseDependencies {
				candidate := byPath[reverse]
				if !selected[candidate.Directory] {
					selected[candidate.Directory] = true
					changed = true
				}
			}
		}
	}
}

func resolveModule(current catalog, value string) string {
	for _, item := range current.Modules {
		if value == item.Directory || value == item.Path {
			return item.Directory
		}
	}
	return ""
}

func packageCount(current catalog) int {
	count := 0
	for _, item := range current.Modules {
		count += len(item.Packages)
	}
	return count
}

func writeCatalogDocumentation(root string, current catalog) {
	for path, content := range catalogDocumentation(current) {
		writeText(filepath.Join(root, path), content)
	}
}

func catalogDocumentation(current catalog) map[string]string {
	var packageCatalog strings.Builder
	packageCatalog.WriteString("# Package Catalog\n\n")
	packageCatalog.WriteString("Generated by `go run ./cmd/golib manifest`; do not edit manually.\n\n")
	packageCatalog.WriteString("| Module | Kind | Lifecycle | Purpose | Owned dependencies | Services | Specifications |\n")
	packageCatalog.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, item := range current.Modules {
		if item.Directory == "." {
			continue
		}
		moduleLink := fmt.Sprintf("[`%s`](../%s)", item.Path, item.Directory)
		fmt.Fprintf(
			&packageCatalog,
			"| %s | %s | %s | %s | %s | %s | %s |\n",
			moduleLink,
			markdownCell(item.Kind),
			markdownCell(item.Lifecycle),
			markdownCell(item.Purpose),
			markdownList(item.OwnedDependencies),
			markdownList(item.RequiredServices),
			markdownList(item.Specifications),
		)
	}
	packageCatalogText := packageCatalog.String()

	var goals strings.Builder
	goals.WriteString("# Goal Traceability\n\n")
	goals.WriteString("Generated by `go run ./cmd/golib manifest`; requirement hashes and implementation links are deterministic. ")
	goals.WriteString("Fresh completion status is emitted by the strict module contract after every gate fingerprint is verified.\n\n")
	goals.WriteString("| Module | Goal | Requirements | Implementation evidence | Verification contract | Implementation status |\n")
	goals.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, item := range current.Modules {
		for _, goal := range item.GoalEvidence {
			path := strings.TrimPrefix(goal.File, "./")
			fmt.Fprintf(
				&goals,
				"| `%s` | [`%s`](../%s) | `%s` | %s | %d canonical gates | %s |\n",
				item.Directory,
				filepath.Base(goal.File),
				path,
				goal.RequirementsSHA256[:12],
				markdownPathList(goal.ImplementationEvidence),
				len(goal.VerificationGates),
				markdownCell(goal.ImplementationStatus),
			)
		}
	}
	goalsText := goals.String()

	var dependencies strings.Builder
	dependencies.WriteString("# Owned Module Dependencies\n\n")
	dependencies.WriteString("Generated by `go run ./cmd/golib manifest`; edges point from consumer to dependency.\n\n")
	dependencies.WriteString("| Consumer | Dependency |\n")
	dependencies.WriteString("| --- | --- |\n")
	for _, item := range current.Modules {
		for _, dependency := range item.OwnedDependencies {
			fmt.Fprintf(&dependencies, "| `%s` | `%s` |\n", item.Path, dependency)
		}
	}
	return map[string]string{
		"docs/package-catalog.md":     packageCatalogText,
		"docs/goal-traceability.md":   goalsText,
		"docs/module-dependencies.md": dependencies.String(),
	}
}

func markdownCell(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	return strings.ReplaceAll(value, "|", "\\|")
}

func markdownList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = "`" + markdownCell(value) + "`"
	}
	return strings.Join(quoted, "<br>")
}

func markdownPathList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	links := make([]string, len(values))
	for index, value := range values {
		links[index] = fmt.Sprintf(
			"[`%s`](../%s)",
			markdownCell(value),
			strings.TrimPrefix(filepath.ToSlash(value), "./"),
		)
	}
	return strings.Join(links, "<br>")
}

func writeJSON(path string, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fatal("encode %s: %v", path, err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fatal("write %s: %v", path, err)
	}
}

func writeText(path, value string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fatal("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		fatal("write %s: %v", path, err)
	}
}

func readJSON(path string, target any) {
	data, err := os.ReadFile(path)
	if err != nil {
		fatal("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		fatal("decode %s: %v", path, err)
	}
}

func equalJSON(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func fatal(format string, arguments ...any) {
	message := fmt.Sprintf(format, arguments...)
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}
