package knapsack_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	packingjson "github.com/faustbrian/golib/pkg/knapsack/encoding"
)

type evidenceManifest struct {
	SchemaVersion         string             `json:"schema_version"`
	Generated             generatedEvidence  `json:"generated"`
	API                   []apiSymbol        `json:"api"`
	FeasibilityInvariants []proofEntry       `json:"feasibility_invariants"`
	SolverMatrix          []map[string]any   `json:"solver_matrix"`
	BoxPackerFeatures     []map[string]any   `json:"boxpacker_features"`
	FuzzTargets           []proofEntry       `json:"fuzz_targets"`
	FuzzExecution         fuzzExecutionProof `json:"fuzz_execution"`
	Mutation              map[string]any     `json:"mutation"`
	Benchmarks            map[string]any     `json:"benchmarks"`
	Concurrency           concurrencyProof   `json:"concurrency"`
	ResourceLimits        []map[string]any   `json:"resource_limits"`
	Serialization         serializationProof `json:"serialization"`
	WorkflowSupplyChain   workflowProof      `json:"workflow_supply_chain"`
	SupplyChain           map[string]any     `json:"supply_chain"`
	Limitations           []map[string]any   `json:"limitations"`
}

type generatedEvidence struct {
	PackageCommit string            `json:"package_commit"`
	SourceSHA256  string            `json:"source_sha256"`
	GoVersion     string            `json:"go_version"`
	Environment   string            `json:"environment"`
	Date          string            `json:"date"`
	Commands      []string          `json:"commands"`
	Dependencies  map[string]string `json:"dependencies"`
	Fixtures      map[string]string `json:"fixtures"`
}

type apiSymbol struct {
	Package   string `json:"package"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature"`
	Doc       string `json:"doc"`
}

type proofEntry struct {
	ID    string   `json:"id"`
	Tests []string `json:"tests"`
}

type concurrencyProof struct {
	RaceCommand                 string   `json:"race_command"`
	LeakCommand                 string   `json:"leak_command"`
	InternalGoroutinePolicy     string   `json:"internal_goroutine_policy"`
	CancellationRoundsPerSolver int      `json:"cancellation_rounds_per_solver"`
	LeakGateRepetitions         int      `json:"leak_gate_repetitions"`
	Tests                       []string `json:"tests"`
}

type fuzzExecutionProof struct {
	Command           string `json:"command"`
	Budgets           string `json:"budgets"`
	BudgetMode        string `json:"budget_mode"`
	LocalMultiplier   int    `json:"local_multiplier"`
	CIMultiplier      int    `json:"ci_multiplier"`
	ReleaseMultiplier int    `json:"release_multiplier"`
}

type workflowProof struct {
	LintCommand     string            `json:"lint_command"`
	ActionlintTool  string            `json:"actionlint_tool"`
	DependencyTest  string            `json:"dependency_test"`
	PublicationTest string            `json:"publication_test"`
	NilAwayJob      string            `json:"nilaway_job"`
	NilAwayTest     string            `json:"nilaway_test"`
	Workflows       []string          `json:"workflows"`
	Actions         map[string]string `json:"actions"`
}

type serializationProof struct {
	CurrentVersion      string   `json:"current_version"`
	SupportedVersions   []string `json:"supported_versions"`
	CompatibilityWindow string   `json:"compatibility_window"`
	PredecessorStatus   string   `json:"predecessor_status"`
	Fixtures            []string `json:"fixtures"`
	Tests               []string `json:"tests"`
}

type mutationRaw struct {
	MutantsKilled     int     `json:"mutants_killed"`
	MutantsLived      int     `json:"mutants_lived"`
	MutantsNotCovered int     `json:"mutants_not_covered"`
	MutationsCoverage float64 `json:"mutations_coverage"`
	TestEfficacy      float64 `json:"test_efficacy"`
	Files             []struct {
		Mutations []struct {
			Status string `json:"status"`
		} `json:"mutations"`
	} `json:"files"`
}

const requiredCancellationRounds = 32

func TestEvidenceSourceNormalizationExcludesOnlyParentZipChecksum(t *testing.T) {
	t.Parallel()

	input := []byte(
		"github.com/faustbrian/golib/pkg/knapsack v0.1.0 h1:zip\n" +
			"github.com/faustbrian/golib/pkg/knapsack v0.1.0/go.mod h1:mod\n" +
			"example.com/external v1.0.0 h1:external\n",
	)
	got := normalizeEvidenceSource("integration/references/go.sum", input)
	if bytes.Contains(got, []byte("h1:zip")) {
		t.Fatal("normalized evidence retains the self-referential parent zip checksum")
	}
	for _, retained := range []string{"h1:mod", "h1:external"} {
		if !bytes.Contains(got, []byte(retained)) {
			t.Fatalf("normalized evidence removed independent checksum %q", retained)
		}
	}
}

func TestEvidenceManifestIsCurrent(t *testing.T) {
	t.Parallel()

	manifest := readEvidence(t)
	want := generatedEvidenceForTree(t)
	want.Date = manifest.Generated.Date
	want.Environment = manifest.Generated.Environment
	want.Commands = manifest.Generated.Commands
	if manifest.SchemaVersion != "v1" {
		t.Fatalf("evidence schema = %q, want v1", manifest.SchemaVersion)
	}
	if !equalGenerated(manifest.Generated, want) {
		t.Fatalf("generated evidence is stale; run UPDATE_EVIDENCE=1 go test . -run TestUpdateEvidence")
	}
	wantAPI := publicAPI(t)
	for _, symbol := range wantAPI {
		if strings.TrimSpace(symbol.Doc) == "" {
			t.Fatalf("exported API symbol %s.%s has no documentation", symbol.Package, symbol.Name)
		}
	}
	if !slices.Equal(manifest.API, wantAPI) {
		t.Fatalf("public API inventory is stale; run UPDATE_EVIDENCE=1 go test . -run TestUpdateEvidence")
	}
	if !reflect.DeepEqual(manifest.Benchmarks, benchmarkEvidenceForTree()) {
		t.Fatalf("benchmark evidence is stale; run UPDATE_EVIDENCE=1 go test . -run TestUpdateEvidence")
	}
	validateBoxPackerFeatureEvidence(t, manifest.BoxPackerFeatures)
	if len(manifest.FeasibilityInvariants) == 0 || len(manifest.SolverMatrix) == 0 ||
		len(manifest.BoxPackerFeatures) == 0 || len(manifest.FuzzTargets) == 0 ||
		manifest.FuzzExecution.Command == "" || manifest.FuzzExecution.Budgets == "" ||
		len(manifest.Mutation) == 0 || len(manifest.Benchmarks) == 0 ||
		manifest.Concurrency.RaceCommand == "" || manifest.Concurrency.LeakCommand == "" ||
		len(manifest.ResourceLimits) == 0 || len(manifest.SupplyChain) == 0 ||
		manifest.Serialization.CurrentVersion == "" ||
		manifest.WorkflowSupplyChain.LintCommand == "" ||
		len(manifest.Limitations) == 0 {
		t.Fatal("evidence manifest omits a required proof section")
	}
	knownTests := testFunctions(t)
	if manifest.Concurrency.InternalGoroutinePolicy != "forbidden" ||
		manifest.Concurrency.CancellationRoundsPerSolver < requiredCancellationRounds ||
		manifest.Concurrency.LeakGateRepetitions < 5 || len(manifest.Concurrency.Tests) == 0 {
		t.Fatalf("concurrency evidence is incomplete: %+v", manifest.Concurrency)
	}
	for _, name := range manifest.Concurrency.Tests {
		if !knownTests[name] {
			t.Fatalf("concurrency evidence references missing test %q", name)
		}
	}
	validateConcurrencyEvidence(t, manifest.Concurrency)
	validateSerializationEvidence(t, manifest.Serialization, manifest.Generated, knownTests)
	validateWorkflowEvidence(t, manifest.WorkflowSupplyChain, knownTests)
	referencedFuzz := make(map[string]bool)
	for _, entries := range [][]proofEntry{manifest.FeasibilityInvariants, manifest.FuzzTargets} {
		for _, entry := range entries {
			if entry.ID == "" || len(entry.Tests) == 0 {
				t.Fatalf("evidence entry is incomplete: %+v", entry)
			}
			for _, name := range entry.Tests {
				if !knownTests[name] {
					t.Fatalf("evidence references missing test %q", name)
				}
				if strings.HasPrefix(name, "Fuzz") {
					referencedFuzz[name] = true
				}
			}
		}
	}
	for name := range knownTests {
		if strings.HasPrefix(name, "Fuzz") && !referencedFuzz[name] {
			t.Fatalf("fuzz target %q is missing from evidence", name)
		}
	}
	validateFuzzExecution(t, manifest.FuzzExecution, knownTests)
}

func TestMutationEvidenceMatchesRawArtifacts(t *testing.T) {
	t.Parallel()

	manifest := readEvidence(t)
	for name, path := range map[string]string{
		"root":    "docs/mutation/raw/root.json",
		"gomoney": "docs/mutation/raw/gomoney.json",
		"adapter": "docs/mutation/raw/adapter.json",
	} {
		raw := readMutationRaw(t, path)
		section, ok := manifest.Mutation[name].(map[string]any)
		if !ok {
			t.Fatalf("mutation evidence omits %q module", name)
		}
		wantRecords, wantTimedOut := 0, 0
		for _, file := range raw.Files {
			wantRecords += len(file.Mutations)
			for _, mutation := range file.Mutations {
				if mutation.Status == "TIMED OUT" {
					wantTimedOut++
				}
			}
		}
		want := map[string]float64{
			"records":                   float64(wantRecords),
			"killed":                    float64(raw.MutantsKilled),
			"lived":                     float64(raw.MutantsLived),
			"timed_out":                 float64(wantTimedOut),
			"tool_uncovered_classified": float64(raw.MutantsNotCovered),
			"mutator_coverage_percent":  raw.MutationsCoverage,
			"test_efficacy_percent":     raw.TestEfficacy,
		}
		for field, value := range want {
			if section[field] != value {
				t.Fatalf("mutation evidence %s.%s = %v, want %v from %s", name, field, section[field], value, path)
			}
		}
	}
}

func validateSerializationEvidence(
	t *testing.T,
	proof serializationProof,
	generated generatedEvidence,
	knownTests map[string]bool,
) {
	t.Helper()
	wantFixtures := []string{
		"encoding/testdata/v1/plan.json",
		"encoding/testdata/v1/request.json",
	}
	if proof.CurrentVersion != packingjson.Version ||
		!slices.Equal(proof.SupportedVersions, []string{packingjson.Version}) ||
		proof.CompatibilityWindow != "current_and_immediately_previous_after_first_transition" ||
		proof.PredecessorStatus != "none_before_initial_v1_release" ||
		!slices.Equal(proof.Fixtures, wantFixtures) || len(proof.Tests) == 0 {
		t.Fatalf("serialization compatibility evidence is incomplete: %+v", proof)
	}
	for _, fixture := range proof.Fixtures {
		if generated.Fixtures[fixture] == "" {
			t.Fatalf("serialization fixture %q has no generated hash", fixture)
		}
	}
	for _, name := range proof.Tests {
		if !knownTests[name] {
			t.Fatalf("serialization evidence references missing test %q", name)
		}
	}
}

func validateWorkflowEvidence(t *testing.T, proof workflowProof, knownTests map[string]bool) {
	t.Helper()
	wantWorkflows := []string{
		".github/workflows/ci.yml",
	}
	if proof.LintCommand != "make repository-check" ||
		proof.ActionlintTool != "github.com/rhysd/actionlint v1.7.12" ||
		!knownTests[proof.DependencyTest] || proof.NilAwayJob != "module contract" ||
		!knownTests[proof.PublicationTest] ||
		!knownTests[proof.NilAwayTest] || !slices.Equal(proof.Workflows, wantWorkflows) ||
		!mapsEqual(proof.Actions, pinnedWorkflowActions(t)) {
		t.Fatalf("workflow supply-chain evidence is stale: %+v", proof)
	}
}

func validateFuzzExecution(t *testing.T, proof fuzzExecutionProof, knownTests map[string]bool) {
	t.Helper()
	if proof.Command != "make fuzz" || proof.BudgetMode != "exact_iterations" ||
		proof.LocalMultiplier != 1 || proof.CIMultiplier < 1 ||
		proof.ReleaseMultiplier < proof.CIMultiplier {
		t.Fatalf("fuzz execution evidence is incomplete: %+v", proof)
	}
	data, err := os.ReadFile(proof.Budgets)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 || lines[0] != "package\ttarget\titerations" {
		t.Fatal("fuzz budget manifest has an invalid header or no targets")
	}
	budgeted := make(map[string]bool, len(lines)-1)
	for index, line := range lines[1:] {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 || fields[0] == "" || fields[1] == "" ||
			fields[2] == "" || budgeted[fields[1]] || !knownTests[fields[1]] {
			t.Fatalf("invalid fuzz budget row %d: %q", index+2, line)
		}
		budgeted[fields[1]] = true
	}
	for name := range knownTests {
		if strings.HasPrefix(name, "Fuzz") && !budgeted[name] {
			t.Fatalf("fuzz target %q has no execution budget", name)
		}
	}
	workflow, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(workflow, []byte("./scripts/run-modules.sh check")) {
		t.Fatal("root workflow does not execute the module fuzz contract")
	}
}

func validateConcurrencyEvidence(t *testing.T, proof concurrencyProof) {
	t.Helper()
	leakTest, err := os.ReadFile("solver/leak_test.go")
	if err != nil {
		t.Fatal(err)
	}
	wantRounds := fmt.Sprintf("const cancellationRounds = %d", proof.CancellationRoundsPerSolver)
	if !bytes.Contains(leakTest, []byte(wantRounds)) {
		t.Fatalf("concurrency evidence does not match solver/leak_test.go: want %q", wantRounds)
	}
	makefile, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	wantRepetitions := fmt.Sprintf("\t\t-count=%d\n", proof.LeakGateRepetitions)
	if !bytes.Contains(makefile, []byte("\nrace:\n")) ||
		!bytes.Contains(makefile, []byte("\nleak:\n")) ||
		!bytes.Contains(makefile, []byte(wantRepetitions)) {
		t.Fatalf("concurrency evidence does not match Makefile targets")
	}
	for _, name := range proof.Tests {
		if !bytes.Contains(makefile, []byte(name)) {
			t.Fatalf("leak gate does not execute %q", name)
		}
	}
}

func TestUpdateEvidence(t *testing.T) {
	if os.Getenv("UPDATE_EVIDENCE") != "1" {
		t.Skip("set UPDATE_EVIDENCE=1 to regenerate machine-readable evidence")
	}
	manifest := readEvidence(t)
	manifest.Generated = generatedEvidenceForTree(t)
	manifest.API = publicAPI(t)
	manifest.Mutation = mutationEvidenceForTree(t)
	manifest.Benchmarks = benchmarkEvidenceForTree()
	updateBoxPackerFeatureEvidence(t, manifest.BoxPackerFeatures)
	manifest.WorkflowSupplyChain.Actions = pinnedWorkflowActions(t)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile("specification/evidence.json", data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readEvidence(t *testing.T) evidenceManifest {
	t.Helper()
	data, err := os.ReadFile("specification/evidence.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest evidenceManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func readMutationRaw(t *testing.T, path string) mutationRaw {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result mutationRaw
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func mutationEvidenceForTree(t *testing.T) map[string]any {
	t.Helper()
	result := map[string]any{
		"classifications":           "specification/mutation-classifications.tsv",
		"command":                   "make mutation",
		"required_efficacy_percent": 100,
		"required_execution_or_classification_percent": 100,
		"survivor_policy": "kill or classify with concrete proof",
		"tool":            "github.com/go-gremlins/gremlins v0.6.0",
	}
	for name, path := range map[string]string{
		"root":    "docs/mutation/raw/root.json",
		"gomoney": "docs/mutation/raw/gomoney.json",
		"adapter": "docs/mutation/raw/adapter.json",
	} {
		raw := readMutationRaw(t, path)
		records, timedOut := 0, 0
		for _, file := range raw.Files {
			records += len(file.Mutations)
			for _, mutation := range file.Mutations {
				if mutation.Status == "TIMED OUT" {
					timedOut++
				}
			}
		}
		result[name] = map[string]any{
			"killed":                    raw.MutantsKilled,
			"lived":                     raw.MutantsLived,
			"mutator_coverage_percent":  raw.MutationsCoverage,
			"raw":                       path,
			"records":                   records,
			"test_efficacy_percent":     raw.TestEfficacy,
			"timed_out":                 timedOut,
			"tool_uncovered_classified": raw.MutantsNotCovered,
		}
	}
	return result
}

func benchmarkEvidenceForTree() map[string]any {
	return map[string]any{
		"boxpacker_runtime_command":    "scripts/benchmark-boxpacker.sh",
		"boxpacker_runtime_raw_output": boxPackerRuntimeRaw,
		"command":                      "make benchmark",
		"comparison_command":           "make benchmark-compare",
		"environment":                  benchmarkEvidenceDocument,
		"raw_output":                   nativeBenchmarkRaw,
		"rss_command":                  "make benchmark-rss",
		"rss_raw_output":               rssBenchmarkRaw,
		"rss_thresholds":               "specification/benchmark-rss-thresholds.tsv",
		"semantic_normalization":       "identical lattice, weights, rotations, stock, constraints, and objectives",
		"thresholds":                   "specification/benchmark-thresholds.tsv",
	}
}

func validateBoxPackerFeatureEvidence(t *testing.T, features []map[string]any) {
	t.Helper()
	for _, feature := range features {
		if feature["feature"] == "fresh-process runtime comparison" {
			if feature["status"] != boxPackerRuntimeRaw {
				t.Fatalf("BoxPacker feature evidence is stale: %v", feature["status"])
			}
			return
		}
	}
	t.Fatal("BoxPacker feature evidence omits the runtime comparison")
}

func updateBoxPackerFeatureEvidence(t *testing.T, features []map[string]any) {
	t.Helper()
	for _, feature := range features {
		if feature["feature"] == "fresh-process runtime comparison" {
			feature["status"] = boxPackerRuntimeRaw
			return
		}
	}
	t.Fatal("BoxPacker feature evidence omits the runtime comparison")
}

func generatedEvidenceForTree(t *testing.T) generatedEvidence {
	t.Helper()
	files := evidenceSourceFiles(t)
	hash := sha256.New()
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		data = normalizeEvidenceSource(path, data)
		fmt.Fprintf(hash, "%s\x00%d\x00", path, len(data))
		_, _ = hash.Write(data)
	}
	production := make([]string, 0)
	for _, path := range files {
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			production = append(production, path)
		}
	}
	arguments := append([]string{"log", "-1", "--format=%H", "--"}, production...)
	commit, err := exec.Command("git", arguments...).Output()
	if err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]string{}
	for _, path := range []string{
		"docs/benchmarks/raw/2026-07-24-darwin-arm64.txt",
		"docs/benchmarks/raw/2026-07-24-darwin-arm64-rss.tsv",
		"docs/benchmarks/raw/2026-07-24-boxpacker-runtime.json",
		"docs/mutation/raw/gomoney.json",
		"docs/mutation/raw/adapter.json",
		"docs/mutation/raw/root.json",
		"encoding/testdata/v1/plan.json",
		"encoding/testdata/v1/request.json",
		"integration/boxpacker/composer.lock",
		"solver/testdata/corpus/dwave-sample-data-1.json",
		"testdata/fuzz/FuzzItemContainerValidation/2f162b4f416df340",
		"specification/corpora.tsv",
		"specification/dependency-licenses.tsv",
		"specification/fuzz-budgets.tsv",
		"specification/benchmark-thresholds.tsv",
		"specification/benchmark-rss-thresholds.tsv",
		"specification/mutation-classifications.tsv",
		"specification/references.tsv",
	} {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		digest := sha256.Sum256(data)
		fixtures[path] = hex.EncodeToString(digest[:])
	}
	return generatedEvidence{
		PackageCommit: strings.TrimSpace(string(commit)),
		SourceSHA256:  hex.EncodeToString(hash.Sum(nil)),
		GoVersion:     runtime.Version(),
		Environment:   runtime.GOOS + "/" + runtime.GOARCH,
		Date:          benchmarkEvidenceDate,
		Commands:      []string{"make check", "make release-check"},
		Dependencies: map[string]string{
			"github.com/faustbrian/golib/pkg/math":        "v0.1.0",
			"github.com/faustbrian/golib/pkg/measurement": "v0.1.0",
		},
		Fixtures: fixtures,
	}
}

func equalGenerated(got, want generatedEvidence) bool {
	return got.SourceSHA256 == want.SourceSHA256 && got.GoVersion == want.GoVersion &&
		mapsEqual(got.Dependencies, want.Dependencies) &&
		mapsEqual(got.Fixtures, want.Fixtures) && got.Date != "" && got.Environment != "" &&
		len(got.Commands) > 0
}

func mapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func evidenceSourceFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		clean := strings.TrimPrefix(filepath.ToSlash(path), "./")
		if clean == "specification/evidence.json" || strings.HasPrefix(clean, "docs/benchmarks/raw/") {
			return nil
		}
		base := filepath.Base(clean)
		if strings.HasSuffix(clean, ".go") || strings.HasSuffix(clean, ".md") ||
			strings.HasSuffix(clean, ".tsv") || strings.HasSuffix(clean, ".json") ||
			strings.HasSuffix(clean, ".sh") || base == "Makefile" || base == "go.mod" || base == "go.sum" {
			files = append(files, clean)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	return files
}

func normalizeEvidenceSource(path string, data []byte) []byte {
	if path != "integration/references/go.sum" && path != "objective/gomoney/go.sum" {
		return data
	}
	const parentModule = "github.com/faustbrian/golib/pkg/knapsack"
	normalized := make([]byte, 0, len(data))
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		fields := bytes.Fields(line)
		if len(fields) == 3 && string(fields[0]) == parentModule &&
			!bytes.HasSuffix(fields[1], []byte("/go.mod")) {
			continue
		}
		normalized = append(normalized, line...)
	}
	return normalized
}

func publicAPI(t *testing.T) []apiSymbol {
	t.Helper()
	set := token.NewFileSet()
	packages := map[string][]*ast.File{}
	for _, path := range evidenceSourceFiles(t) {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(path, "integration/references") {
			continue
		}
		file, err := parser.ParseFile(set, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		key := filepath.ToSlash(filepath.Dir(path))
		packages[key] = append(packages[key], file)
	}
	var symbols []apiSymbol
	for path, files := range packages {
		packageName := "github.com/faustbrian/golib/pkg/knapsack"
		if path != "." {
			packageName += "/" + path
		}
		for _, file := range files {
			for _, declaration := range file.Decls {
				symbols = append(symbols, exportedSymbols(set, packageName, declaration)...)
			}
		}
	}
	slices.SortFunc(symbols, func(left, right apiSymbol) int {
		return strings.Compare(left.Package+"\x00"+left.Kind+"\x00"+left.Name, right.Package+"\x00"+right.Kind+"\x00"+right.Name)
	})
	return symbols
}

func exportedSymbols(set *token.FileSet, packageName string, declaration ast.Decl) []apiSymbol {
	var symbols []apiSymbol
	switch value := declaration.(type) {
	case *ast.FuncDecl:
		if !value.Name.IsExported() {
			return nil
		}
		name, kind := value.Name.Name, "function"
		if value.Recv != nil {
			name, kind = receiverName(value.Recv.List[0].Type)+"."+name, "method"
		}
		symbols = append(symbols, apiSymbol{packageName, kind, name, renderNode(set, value.Type), docText(value.Doc)})
	case *ast.GenDecl:
		for _, specification := range value.Specs {
			switch spec := specification.(type) {
			case *ast.TypeSpec:
				if !spec.Name.IsExported() {
					continue
				}
				symbols = append(symbols, apiSymbol{packageName, "type", spec.Name.Name, renderNode(set, spec.Type), docText(spec.Doc, value.Doc)})
				if structure, ok := spec.Type.(*ast.StructType); ok {
					for _, field := range structure.Fields.List {
						for _, name := range field.Names {
							if name.IsExported() {
								symbols = append(symbols, apiSymbol{packageName, "field", spec.Name.Name + "." + name.Name, renderNode(set, field.Type), docText(field.Doc, field.Comment)})
							}
						}
					}
				}
			case *ast.ValueSpec:
				for _, name := range spec.Names {
					if name.IsExported() {
						symbols = append(symbols, apiSymbol{packageName, strings.ToLower(value.Tok.String()), name.Name, renderNode(set, spec.Type), docText(spec.Doc, value.Doc)})
					}
				}
			}
		}
	}
	return symbols
}

func receiverName(expression ast.Expr) string {
	if star, ok := expression.(*ast.StarExpr); ok {
		expression = star.X
	}
	if identifier, ok := expression.(*ast.Ident); ok {
		return identifier.Name
	}
	return renderNode(token.NewFileSet(), expression)
}

func renderNode(set *token.FileSet, node any) string {
	if node == nil {
		return ""
	}
	var output bytes.Buffer
	if err := printer.Fprint(&output, set, node); err != nil {
		return ""
	}
	return output.String()
}

func docText(groups ...*ast.CommentGroup) string {
	for _, group := range groups {
		if group != nil {
			return strings.TrimSpace(group.Text())
		}
	}
	return ""
}

func testFunctions(t *testing.T) map[string]bool {
	t.Helper()
	result := map[string]bool{}
	for _, path := range evidenceSourceFiles(t) {
		if !strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv == nil &&
				(strings.HasPrefix(function.Name.Name, "Test") || strings.HasPrefix(function.Name.Name, "Fuzz") || strings.HasPrefix(function.Name.Name, "Benchmark")) {
				result[function.Name.Name] = true
			}
		}
	}
	return result
}
