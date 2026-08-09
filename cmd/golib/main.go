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
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const (
	canonicalRoot = "github.com/faustbrian/golib"
	requiredGo    = "1.26.5"
)

var ownedDependencyPseudoVersionPattern = regexp.MustCompile(
	`^v0\.0\.0-[0-9]{14}-[0-9a-f]{12}$`,
)

var mutationThresholdPattern = regexp.MustCompile(
	`--threshold-(efficacy|mcover)(?:[[:space:]]+|=)[[:space:]]*([^[:space:]\\]+)`,
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
	BuildTags           []string        `json:"build_tags"`
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
	ModuleDirectory  string   `json:"module_directory"`
	Directory        string   `json:"directory"`
	Name             string   `json:"name"`
	Import           string   `json:"import_path"`
	Kind             string   `json:"kind"`
	Production       bool     `json:"production"`
	Executable       bool     `json:"executable"`
	CoverageRequired bool     `json:"coverage_required"`
	BuildRequired    bool     `json:"build_required"`
	BuildTags        []string `json:"build_tags"`
}

type modFile struct {
	Module struct {
		Path string
	}
	Go      string
	Require []struct {
		Path     string
		Version  string
		Indirect bool
	}
	Replace []json.RawMessage
}

func main() {
	if len(os.Args) < 2 {
		fatal("usage: golib <manifest|validate|specifications|select|safety>")
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
	case "specifications":
		validateSpecifications(root, os.Args[2:])
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
	if err := validateMutationThresholds(root, wanted); err != nil {
		fatal("validate mutation thresholds: %v", err)
	}
	validatePaths(root)
	fmt.Printf("validated %d modules and %d packages\n", len(wanted.Modules), packageCount(wanted))
}

func selectModules(root string, arguments []string) {
	flags := flag.NewFlagSet("select", flag.ExitOnError)
	all := flags.Bool("all", false, "select every active module")
	changed := flags.String("changed", "", "select changes since this git revision")
	dependencies := flags.Bool("dependencies", false, "include transitive owned dependencies")
	explicit := flags.String("modules", "", "comma-separated module directories or paths")
	outputFormat := flags.String("format", "text", "output format: text, json, or matrix")
	order := flags.String("order", "directory", "selection order: directory or dependency")
	if err := flags.Parse(arguments); err != nil {
		fatal("parse selection: %v", err)
	}

	current, err := catalogForSelection(
		root,
		*explicit != "" && !*all && *changed == "",
	)
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
	if *dependencies {
		expandOwnedDependencies(current, selected)
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

func expandOwnedDependencies(current catalog, selected map[string]bool) {
	byDirectory := map[string]module{}
	byPath := map[string]module{}
	for _, item := range current.Modules {
		byDirectory[item.Directory] = item
		byPath[item.Path] = item
	}

	queue := make([]string, 0, len(selected))
	for directory := range selected {
		queue = append(queue, directory)
	}
	for len(queue) != 0 {
		directory := queue[0]
		queue = queue[1:]
		for _, dependencyPath := range byDirectory[directory].OwnedDependencies {
			dependency, exists := byPath[dependencyPath]
			if !exists || selected[dependency.Directory] {
				continue
			}
			selected[dependency.Directory] = true
			queue = append(queue, dependency.Directory)
		}
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
				if versionErr := validateOwnedDependencyVersion(
					directory,
					requirement.Path,
					requirement.Version,
				); versionErr != nil {
					return catalog{}, versionErr
				}
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
		buildTags, buildTagsErr := requiredBuildTags(root, directory)
		if buildTagsErr != nil {
			return catalog{}, fmt.Errorf("discover build tags in %s: %w", directory, buildTagsErr)
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
			BuildTags:          buildTags,
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

func validateOwnedDependencyVersion(directory, path, version string) error {
	if version == "v0.0.0" ||
		ownedDependencyPseudoVersionPattern.MatchString(version) {
		return nil
	}

	return fmt.Errorf(
		"module %s requires owned dependency %s at %s; repository manifests must use local v0.0.0 or an immutable main pseudo-version",
		directory,
		path,
		version,
	)
}

func conformanceRequired(kind string, specifications, corpora []string) bool {
	return (kind == "public library" || kind == "adapter") &&
		(len(specifications) != 0 || len(corpora) != 0)
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
		name          string
		executable    bool
		buildRequired bool
		buildTags     []string
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
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		buildTag, ignored, buildTagErr := sourceBuildPolicy(data)
		if buildTagErr != nil {
			return fmt.Errorf("%s: %w", path, buildTagErr)
		}
		file, parseErr := parser.ParseFile(
			token.NewFileSet(), path, data, parser.SkipObjectResolution,
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
		if !ignored {
			record.buildRequired = true
		}
		if buildTag != "" && !slices.Contains(record.buildTags, buildTag) {
			record.buildTags = append(record.buildTags, buildTag)
			sort.Strings(record.buildTags)
		}
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
			BuildRequired:    record.buildRequired,
			BuildTags:        append([]string{}, record.buildTags...),
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

func requiredBuildTags(root, moduleDirectory string) ([]string, error) {
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
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		tag, _, policyErr := sourceBuildPolicy(data)
		if policyErr != nil {
			return fmt.Errorf("%s: %w", path, policyErr)
		}
		if tag != "" && !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(tags)
	return tags, nil
}

func sourceBuildPolicy(data []byte) (string, bool, error) {
	var expression constraint.Expr
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "//go:build ") {
			if strings.HasPrefix(line, "package ") {
				break
			}
			continue
		}
		parsed, err := constraint.Parse(line)
		if err != nil {
			return "", false, err
		}
		expression = parsed
		break
	}
	if expression == nil {
		return "", false, nil
	}
	if tag, direct := expression.(*constraint.TagExpr); direct {
		if tag.Tag == "ignore" {
			return "", true, nil
		}
		if !reservedBuildTag(tag.Tag) {
			return tag.Tag, false, nil
		}
		return "", false, nil
	}

	tags := map[string]struct{}{}
	collectConstraintTags(expression, tags)
	custom := []string{}
	for tag := range tags {
		if tag != "ignore" && !reservedBuildTag(tag) {
			custom = append(custom, tag)
		}
	}
	if len(custom) != 0 {
		sort.Strings(custom)
		return "", false, fmt.Errorf(
			"compound custom build constraint is unsupported: %s",
			strings.Join(custom, ", "),
		)
	}
	return "", false, nil
}

func collectConstraintTags(expression constraint.Expr, tags map[string]struct{}) {
	switch current := expression.(type) {
	case *constraint.TagExpr:
		tags[current.Tag] = struct{}{}
	case *constraint.NotExpr:
		collectConstraintTags(current.X, tags)
	case *constraint.AndExpr:
		collectConstraintTags(current.X, tags)
		collectConstraintTags(current.Y, tags)
	case *constraint.OrExpr:
		collectConstraintTags(current.X, tags)
		collectConstraintTags(current.Y, tags)
	}
}

func reservedBuildTag(tag string) bool {
	if tag == "cgo" || tag == "ignore" || tag == "race" || tag == "unix" ||
		tag == "gc" || tag == "gccgo" || strings.HasPrefix(tag, "go1.") {
		return true
	}
	platformTags := strings.Fields(
		"aix android darwin dragonfly freebsd hurd illumos ios js linux " +
			"netbsd openbsd plan9 solaris wasip1 windows " +
			"386 amd64 amd64p32 arm arm64 arm64be armbe loong64 mips " +
			"mips64 mips64le mipsle ppc ppc64 ppc64le riscv riscv64 " +
			"s390 s390x sparc sparc64 wasm",
	)
	return slices.Contains(platformTags, tag)
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
	if directory == "pkg/outbox/adapters/goqueue" {
		return []string{"redis", "valkey"}
	}
	if directory == "pkg/capability" {
		return []string{"postgresql", "valkey"}
	}
	library := libraryName(directory)
	services := []string{}
	postgresLibraries := []string{
		"api-query", "authorization", "calendar", "feature-flags",
		"idempotency", "lease", "localized", "migrations", "opening-hours",
		"outbox", "postgres", "queue-control-plane", "rate-limit",
		"scheduler", "sequencer", "settings", "state-machine", "temporal",
		"tenancy",
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
	if directory == "pkg/cloudevents" {
		return []string{
			"cloudevents/sdk-go v2.16.2",
			"cloudevents/sdk-javascript v10.0.0 on Node.js 24.13.0",
		}
	}
	if directory == "pkg/barcode" {
		return []string{
			"speedata/barcode v1.1.1 independent writer",
			"ericlevine/zxinggo v0.1.0 independent reader",
			"ruudk/golang-pdf417 at a7e3863a1245 independent writer",
		}
	}
	if directory == "pkg/capability" {
		return []string{"Python 3 standard-library HMAC implementation"}
	}
	if directory == "pkg/authentication/oidc" {
		return []string{"Google, Keycloak, and Dex provider metadata profiles"}
	}
	if directory == "pkg/webhook" {
		return []string{"Python 3 standard-library HMAC-SHA-256 and HMAC-SHA-512 vectors"}
	}
	if directory == "pkg/search/adapters/opensearch" {
		return []string{"OpenSearch 2.19.3", "OpenSearch 3.6.0", "opensearch-go/v4 v4.7.3"}
	}
	switch libraryName(directory) {
	case "wsdl":
		return []string{"Java", "Apache Woden"}
	case "ecma-regexp":
		return []string{"Node.js", "Test262"}
	case "merkle-patricia-trie":
		return []string{"go-ethereum v1.17.3", "@ethereumjs/mpt v10.1.2", "Node.js 20 or newer"}
	case "xsd":
		return []string{"Docker", "Eclipse Temurin 25 JAXP"}
	default:
		return []string{}
	}
}

func specifications(directory string) []string {
	if directory == "pkg/cloudevents" {
		return []string{
			"CloudEvents specification 1.0.2",
			"CloudEvents JSON event format 1.0.2",
			"CloudEvents HTTP protocol binding 1.0.2",
			"CloudEvents Kafka protocol binding 1.0.2",
			"CloudEvents distributed tracing extension 1.0.2",
			"CloudEvents partitioning extension 1.0.2",
		}
	}
	if directory == "pkg/barcode" {
		return []string{
			"ISO/IEC 18004:2024 QR Code",
			"ISO/IEC 15417:2007 with Amendment 1:2026 Code 128",
			"ISO/IEC 16388:2023 Code 39",
			"ANSI/AIM BC5-1995 Code 93",
			"ISO/IEC 15420:2009 EAN/UPC",
			"ISO/IEC 16390:2007 Interleaved 2 of 5",
			"AIM Europe 1995 Codabar",
			"ISO/IEC 16022:2024 Data Matrix",
			"ISO/IEC 15438:2015 PDF417",
			"ISO/IEC 24778:2008 Aztec Code",
			"GS1 General Specifications 26.0.0",
		}
	}
	if directory == "pkg/capability" {
		return []string{"RFC 4231 HMAC-SHA-256 vectors", "RFC 8032 Ed25519 vectors"}
	}
	if directory == "pkg/authentication" {
		return []string{
			"RFC 7617 Basic HTTP Authentication",
			"RFC 6750 OAuth 2.0 Bearer Token Usage",
			"RFC 9110 HTTP Authentication Framework",
		}
	}
	if directory == "pkg/http-client" {
		return []string{
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
		}
	}
	if directory == "pkg/http-middleware" {
		return []string{
			"Go 1.26.5 net/http and context contracts",
			"RFC 9110 HTTP Semantics",
			"RFC 9111 HTTP Caching",
			"RFC 7239 Forwarded HTTP Extension",
			"RFC 6797 HTTP Strict Transport Security",
			"RFC 7034 X-Frame-Options",
			"WHATWG Fetch CORS protocol at 586cd2a44c2a",
			"WHATWG URL origin model at 9dc3827fc722",
			"W3C Referrer Policy at cc435b05ca4a",
		}
	}
	if directory == "pkg/router" {
		return []string{
			"Go 1.26.5 net/http and net/url contracts",
			"RFC 3986 URI Generic Syntax",
			"RFC 9110 HTTP Semantics",
			"RFC 9112 HTTP/1.1 request-target forms",
		}
	}
	if directory == "pkg/wire" {
		return []string{
			"Go 1.26.5 encoding/json and encoding/xml contracts",
			"RFC 8259 JSON",
			"XML 1.0 Fifth Edition and Namespaces in XML 1.0 Third Edition",
			"SOAP 1.1 and SOAP 1.2 Part 1 Second Edition",
			"YAML 1.2.2",
			"TOML 1.1.0",
			"MessagePack format at 8aa09e2a6a91",
			"RFC 7049 and RFC 8949 CBOR",
			"CTAP 2.2 deterministic CBOR profile",
			"BSON 1.1",
		}
	}
	if directory == "pkg/authentication/oidc" {
		return []string{
			"OpenID Connect Core 1.0 incorporating errata set 2",
			"OpenID Connect Discovery 1.0 incorporating errata set 2",
		}
	}
	if directory == "pkg/webhook" {
		return []string{
			"Go 1.26.5 cryptography, HTTP, URL, address, time, and encoding contracts",
			"RFC 2104 HMAC and RFC 4231 HMAC-SHA-256/HMAC-SHA-512 vectors",
			"RFC 4648 Base-N Encodings",
			"RFC 3986 URI Generic Syntax",
			"RFC 3339 Internet date and time",
			"RFC 8259 JSON",
			"RFC 8941 Structured Fields comparison boundary",
			"RFC 9110 HTTP Semantics",
			"RFC 9421 HTTP Message Signatures comparison boundary",
			"IANA IPv4 and IPv6 Special-Purpose Address Registries at 2025-10-09",
			"CloudEvents 1.0.2 comparison boundary",
			"W3C Trace Context Level 1",
		}
	}
	if directory == "pkg/kafka" {
		return []string{
			"Apache Kafka protocol and client semantics",
			"implemented Kafka Improvement Proposals",
		}
	}
	if directory == "pkg/search/adapters/opensearch" {
		return []string{"OpenSearch REST API 2.19.3 and 3.6.0"}
	}
	prefix := libraryName(directory)
	switch prefix {
	case "json-schema":
		return []string{"JSON Schema drafts 4, 6, 7, 2019-09, and 2020-12", "JSON-Schema-Test-Suite"}
	case "jsonapi":
		return []string{"JSON:API 1.0 and 1.1", "JSON:API extensions and recommendations"}
	case "jsonrpc":
		return []string{"JSON-RPC 2.0"}
	case "merkle-tree":
		return []string{"RFC 9162"}
	case "merkle-patricia-trie":
		return []string{
			"Ethereum Yellow Paper modified Merkle Patricia trie",
			"Ethereum Recursive Length Prefix encoding",
		}
	case "openapi":
		return []string{"OpenAPI 2.0, 3.0, and 3.1"}
	case "openrpc":
		return []string{
			"OpenRPC 1.3.x and 1.4.x",
			"JSON Schema Draft 7",
			"JSON-RPC 2.0",
		}
	case "wsdl":
		return []string{"WSDL 1.1 and 2.0"}
	case "xsd":
		return []string{"W3C XML Schema 1.0 Second Edition", "W3C XML Schema Test Suite"}
	default:
		return []string{}
	}
}

func conformanceCorpora(directory string) []string {
	if directory == "pkg/cloudevents" {
		return []string{"cloudevents/conformance v0.4.1 HTTP and Kafka features"}
	}
	if directory == "pkg/barcode" {
		return []string{
			"Normative requirement and executable evidence matrix",
			"Pinned GS1 syntax dictionary 2026-01-27",
			"Independent writer and reciprocal reader interoperability corpus",
			"Deterministic logical, PNG, and SVG fixture corpus",
		}
	}
	if directory == "pkg/capability" {
		return []string{"RFC 4231 test case 6", "RFC 8032 section 7.1 test 1"}
	}
	if directory == "pkg/authentication" {
		return []string{
			"RFC 7617 Sections 2 and 2.1 credential vectors",
			"RFC 6750 Section 2.1 bearer b64token vector",
		}
	}
	if directory == "pkg/http-client" {
		return []string{
			"Pinned normative-source matrix and specification decision evidence",
		}
	}
	if directory == "pkg/http-middleware" {
		return []string{
			"Pinned normative-source matrix and specification decision evidence",
		}
	}
	if directory == "pkg/router" {
		return []string{
			"Pinned normative-source matrix and ServeMux differential evidence",
		}
	}
	if directory == "pkg/wire" {
		return []string{
			"Pinned normative-source matrix and format-specific decision evidence",
			"Codec differential, hostile-input, round-trip, and official format fixtures",
		}
	}
	if directory == "pkg/authentication/oidc" {
		return []string{"OpenID Connect Core 1.0 Section 2 ID-token claim vector"}
	}
	if directory == "pkg/webhook" {
		return []string{
			"Pinned normative-source matrix and 26-decision protocol register",
			"Independent Python HMAC-SHA-256 and HMAC-SHA-512 v1 vectors",
			"Controlled body, replay, delivery, DNS, redirect, and SSRF matrices",
		}
	}
	prefix := libraryName(directory)
	switch prefix {
	case "ecma-regexp":
		return []string{"TC39 Test262"}
	case "json-schema":
		return []string{"JSON-Schema-Test-Suite", "Bowtie"}
	case "merkle-patricia-trie":
		return []string{
			"ethereum/execution-spec-tests stable fixtures v5.4.0 at 88e9fb8f10ed89805aa3110d0a2cd5dcadc19689",
			"ethereum/tests TrieTests at c67e485ff8b5be9abc8ad15345ec21aa22e290d9",
			"go-ethereum transition receipt fixtures at 117e067f0f0bae1a17082321f224dedb6765b10f",
		}
	case "openrpc":
		return []string{
			"Pinned OpenRPC 1.4.1 meta-schema and prose at 3a13c7a8bad248e6edd2d48339cd1c06b57f8f22",
			"Pinned official OpenRPC examples at dce69463ba9a3ca2232506b734606fa97f25dd45",
			"Generated normative and object-field evidence matrices",
		}
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

func catalogForSelection(root string, explicitOnly bool) (catalog, error) {
	if !explicitOnly {
		return discover(root)
	}

	contents, err := os.ReadFile(filepath.Join(root, "modules.json"))
	if err != nil {
		return catalog{}, fmt.Errorf("read registered modules: %w", err)
	}
	current := catalog{}
	if err := json.Unmarshal(contents, &current); err != nil {
		return catalog{}, fmt.Errorf("decode registered modules: %w", err)
	}

	return current, nil
}

func validateWorkspace(root string, current catalog) {
	data, err := os.ReadFile(filepath.Join(root, "go.work"))
	if err != nil {
		fatal("read go.work: %v", err)
	}
	if validationErr := validateWorkspaceContent(string(data), current); validationErr != nil {
		fatal("%v", validationErr)
	}
}

func validateWorkspaceContent(text string, current catalog) error {
	for _, item := range current.Modules {
		if item.Kind == "fixture" {
			continue
		}
		entry := "\t./" + strings.TrimPrefix(item.Directory, "./") + "\n"
		if item.Directory == "." {
			entry = "\t.\n"
		}
		if !strings.Contains(text, entry) {
			return fmt.Errorf("go.work omits active module %s", item.Directory)
		}
		if item.Directory == "." {
			continue
		}
		replacement := fmt.Sprintf(
			"replace %s v0.0.0 => ./%s\n",
			item.Path,
			strings.TrimPrefix(item.Directory, "./"),
		)
		if !strings.Contains(text, replacement) {
			return fmt.Errorf(
				"go.work must replace active module %s at local v0.0.0",
				item.Directory,
			)
		}
	}

	return nil
}

func validateMutationThresholds(root string, current catalog) error {
	for _, item := range current.Modules {
		if !item.Gates["mutation"] {
			continue
		}
		base := filepath.Join(root, item.Directory)
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if path != base && excludedSourceDirectory(entry.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Name() == ".gremlins.yml" || entry.Name() == ".gremlins.yaml" {
				return fmt.Errorf(
					"%s duplicates canonical mutation policy; package-local Gremlins configuration is forbidden",
					filepath.ToSlash(path),
				)
			}
			if entry.Name() != "Makefile" && filepath.Ext(entry.Name()) != ".sh" &&
				filepath.Ext(entry.Name()) != ".mk" {
				return nil
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			return validateMutationThresholdContents(filepath.ToSlash(relative), contents)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func validateMutationThresholdContents(path string, contents []byte) error {
	normalized := bytes.ReplaceAll(contents, []byte("\\\r\n"), []byte(" "))
	normalized = bytes.ReplaceAll(normalized, []byte("\\\n"), []byte(" "))
	thresholds := map[string]bool{}
	for _, match := range mutationThresholdPattern.FindAllSubmatch(normalized, -1) {
		name := string(match[1])
		value := strings.Trim(string(match[2]), "'\"")
		if value != "100" {
			return fmt.Errorf(
				"%s configures mutation threshold %q; thresholds must be literal 100",
				path,
				value,
			)
		}
		thresholds[name] = true
	}
	lower := bytes.ToLower(normalized)
	if bytes.Contains(lower, []byte("gremlins")) &&
		bytes.Contains(lower, []byte("unleash")) &&
		(!thresholds["efficacy"] || !thresholds["mcover"]) {
		return fmt.Errorf(
			"%s invokes Gremlins directly without literal 100 efficacy and mutator coverage thresholds",
			path,
		)
	}
	return nil
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
