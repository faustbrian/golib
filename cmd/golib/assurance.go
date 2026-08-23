package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

const operationalAssuranceFile = "operational-assurance.json"

var requiredAssuranceScenarios = []string{
	"OA-REFERENCE-HTTP",
	"OA-REFERENCE-DURABILITY",
	"OA-REFERENCE-EXTERNAL",
	"OA-PLATFORM-MATRIX",
	"OA-FAILURE-RECOVERY",
	"OA-DEPLOYMENT-COMPATIBILITY",
	"OA-RESOURCE-PERFORMANCE",
	"OA-SECURITY-PRIVACY-SUPPLY-CHAIN",
	"OA-OBSERVABILITY-OPERATIONS",
	"OA-CROSS-PACKAGE-CONSISTENCY",
	"OA-RELEASE-CONSUMER",
}

type operationalAssuranceRecord struct {
	SchemaVersion         int                       `json:"schema_version"`
	Verdict               string                    `json:"verdict"`
	Modules               []string                  `json:"modules"`
	Scenarios             []operationalScenario     `json:"scenarios"`
	ResidualRisks         []string                  `json:"residual_risks"`
	RiskAcceptances       []operationalRiskApproval `json:"risk_acceptances"`
	InputDigestMigrations []inputDigestMigration    `json:"input_digest_migrations"`
}

type operationalScenario struct {
	ID              string                `json:"id"`
	Status          string                `json:"status"`
	AffectedModules []string              `json:"affected_modules"`
	Evidence        []operationalEvidence `json:"evidence"`
	AcceptedRisks   []string              `json:"accepted_risks"`
}

type operationalEvidence struct {
	Path              string                      `json:"path"`
	SHA256            string                      `json:"sha256"`
	ObservedUTC       string                      `json:"observed_utc"`
	Environment       string                      `json:"environment"`
	InputEnvironment  operationalInputEnvironment `json:"input_environment,omitempty"`
	ModuleScope       []string                    `json:"module_scope"`
	InputModules      []string                    `json:"input_modules,omitempty"`
	ExactInputModules []string                    `json:"exact_input_modules,omitempty"`
	InputDigests      map[string]string           `json:"input_digests"`
}

type operationalInputEnvironment struct {
	GoVersion  string `json:"go_version"`
	GOOS       string `json:"goos"`
	GOARCH     string `json:"goarch"`
	CGOEnabled string `json:"cgo_enabled"`
	Kernel     string `json:"kernel"`
	Node       string `json:"node"`
}

type operationalRiskApproval struct {
	ID          string   `json:"id"`
	AcceptedBy  string   `json:"accepted_by"`
	AcceptedUTC string   `json:"accepted_utc"`
	Rationale   string   `json:"rationale"`
	Scenarios   []string `json:"scenarios"`
}

type inputDigestMigration struct {
	Module         string `json:"module"`
	FromSHA256     string `json:"from_sha256"`
	ToSHA256       string `json:"to_sha256"`
	EvidencePath   string `json:"evidence_path"`
	EvidenceSHA256 string `json:"evidence_sha256"`
	Rationale      string `json:"rationale"`
}

func assurance(root string, arguments []string) {
	flags := flag.NewFlagSet("assurance", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	requireReady := flags.Bool("require-ready", false, "require a release-authorizing verdict")
	format := flags.String("format", "text", "output format: text or json")
	if err := flags.Parse(arguments); err != nil {
		fatal("parse assurance flags: %v", err)
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		fatal("usage: golib assurance [--require-ready] [--format text|json]")
	}

	current, err := discover(root)
	if err != nil {
		fatal("discover modules: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(root, operationalAssuranceFile))
	if err != nil {
		fatal("read operational assurance: %v", err)
	}
	if err := validateOperationalAssurance(root, current, contents); err != nil {
		fatal("validate operational assurance: %v", err)
	}
	record := operationalAssuranceRecord{}
	if err := json.Unmarshal(contents, &record); err != nil {
		fatal("decode operational assurance: %v", err)
	}
	if *requireReady && record.Verdict == "not ready" {
		fatal("operational assurance verdict is not ready")
	}

	passed := 0
	for _, scenario := range record.Scenarios {
		if scenario.Status == "passed" {
			passed++
		}
	}
	summary := struct {
		Verdict         string `json:"verdict"`
		Modules         int    `json:"modules"`
		Scenarios       int    `json:"scenarios"`
		PassedScenarios int    `json:"passed_scenarios"`
		ResidualRisks   int    `json:"residual_risks"`
		AcceptedRisks   int    `json:"accepted_risks"`
	}{record.Verdict, len(record.Modules), len(record.Scenarios), passed, len(record.ResidualRisks), len(record.RiskAcceptances)}
	if *format == "json" {
		output, err := json.Marshal(summary)
		if err != nil {
			fatal("encode operational assurance summary: %v", err)
		}
		fmt.Println(string(output))
		return
	}
	fmt.Printf(
		"operational assurance: %s (%d/%d scenarios passed, %d residual risks)\n",
		record.Verdict,
		passed,
		len(record.Scenarios),
		len(record.ResidualRisks),
	)
}

func validateOperationalAssurance(root string, current catalog, contents []byte) error {
	record := operationalAssuranceRecord{}
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return fmt.Errorf("decode record: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("record contains trailing data")
		}
		return fmt.Errorf("record contains trailing data: %w", err)
	}
	if record.SchemaVersion != 1 {
		return fmt.Errorf("schema version = %d, want 1", record.SchemaVersion)
	}
	if !slices.Contains([]string{
		"ready",
		"not ready",
		"ready with named accepted risks",
	}, record.Verdict) {
		return fmt.Errorf("unsupported verdict %q", record.Verdict)
	}

	wantModules := make([]string, 0)
	for _, item := range current.Modules {
		if item.Releasable {
			wantModules = append(wantModules, item.Directory)
		}
	}
	sort.Strings(wantModules)
	if !slices.Equal(record.Modules, wantModules) {
		return fmt.Errorf("module scope = %v, want every releasable module %v", record.Modules, wantModules)
	}
	moduleSet := make(map[string]bool, len(record.Modules))
	for _, directory := range record.Modules {
		moduleSet[directory] = true
	}
	digestMigrations, err := validateInputDigestMigrations(
		root,
		current,
		record.InputDigestMigrations,
	)
	if err != nil {
		return err
	}

	riskSet, err := validateOperationalRisks(record)
	if err != nil {
		return err
	}
	seenScenarios := make(map[string]bool, len(record.Scenarios))
	currentInputDigests := make(map[string]string, len(record.Modules))
	allPassed := true
	for _, scenario := range record.Scenarios {
		if seenScenarios[scenario.ID] {
			return fmt.Errorf("duplicate scenario %s", scenario.ID)
		}
		seenScenarios[scenario.ID] = true
		if !slices.Contains(requiredAssuranceScenarios, scenario.ID) {
			return fmt.Errorf("unknown operational assurance scenario %s", scenario.ID)
		}
		if err := validateOperationalScenario(
			root,
			current,
			moduleSet,
			riskSet,
			digestMigrations,
			currentInputDigests,
			scenario,
		); err != nil {
			return fmt.Errorf("scenario %s: %w", scenario.ID, err)
		}
		if scenario.Status != "passed" {
			allPassed = false
		}
	}
	for _, identifier := range requiredAssuranceScenarios {
		if !seenScenarios[identifier] {
			return fmt.Errorf("required scenario %s is missing", identifier)
		}
	}

	switch record.Verdict {
	case "ready":
		if !allPassed {
			return errors.New("ready verdict requires every scenario to pass")
		}
		if len(record.ResidualRisks) != 0 {
			return errors.New("ready verdict cannot retain residual risks")
		}
		if len(record.RiskAcceptances) != 0 {
			return errors.New("ready verdict cannot retain risk acceptances")
		}
	case "ready with named accepted risks":
		if len(record.ResidualRisks) == 0 {
			return errors.New("accepted-risk verdict requires at least one residual risk")
		}
		if err := validateAcceptedRiskVerdict(record); err != nil {
			return err
		}
	}
	return nil
}

func validateOperationalRisks(record operationalAssuranceRecord) (map[string]bool, error) {
	risks := make(map[string]bool, len(record.ResidualRisks))
	for _, identifier := range record.ResidualRisks {
		if strings.TrimSpace(identifier) == "" || risks[identifier] {
			return nil, fmt.Errorf("invalid or duplicate residual risk %q", identifier)
		}
		risks[identifier] = true
	}
	approvals := make(map[string]bool, len(record.RiskAcceptances))
	for _, approval := range record.RiskAcceptances {
		if !risks[approval.ID] || approvals[approval.ID] {
			return nil, fmt.Errorf("risk acceptance %q has no unique residual risk", approval.ID)
		}
		if strings.TrimSpace(approval.AcceptedBy) == "" ||
			strings.TrimSpace(approval.Rationale) == "" || len(approval.Scenarios) == 0 {
			return nil, fmt.Errorf("risk acceptance %s is incomplete", approval.ID)
		}
		if !isUTCRFC3339(approval.AcceptedUTC) {
			return nil, fmt.Errorf("risk acceptance %s has invalid UTC time", approval.ID)
		}
		for _, scenario := range approval.Scenarios {
			if !slices.Contains(requiredAssuranceScenarios, scenario) {
				return nil, fmt.Errorf("risk acceptance %s references unknown scenario %s", approval.ID, scenario)
			}
		}
		approvals[approval.ID] = true
	}
	return risks, nil
}

func validateOperationalScenario(
	root string,
	current catalog,
	modules map[string]bool,
	risks map[string]bool,
	digestMigrations map[string]inputDigestMigration,
	currentInputDigests map[string]string,
	scenario operationalScenario,
) error {
	if !slices.Contains([]string{"pending", "passed", "failed", "unavailable", "accepted risk"}, scenario.Status) {
		return fmt.Errorf("unsupported status %q", scenario.Status)
	}
	if err := validateAssuranceModuleScope(scenario.AffectedModules, modules); err != nil {
		return fmt.Errorf("affected modules: %w", err)
	}
	for _, identifier := range scenario.AcceptedRisks {
		if !risks[identifier] {
			return fmt.Errorf("accepted risk %s is not in the residual-risk register", identifier)
		}
	}
	if scenario.Status == "accepted risk" && len(scenario.AcceptedRisks) == 0 {
		return errors.New("accepted-risk status requires a named risk")
	}
	if scenario.Status != "accepted risk" && len(scenario.AcceptedRisks) != 0 {
		return errors.New("accepted risks require accepted-risk status")
	}
	covered := map[string]bool{}
	for _, evidence := range scenario.Evidence {
		if err := validateOperationalEvidence(
			root,
			current,
			modules,
			digestMigrations,
			currentInputDigests,
			scenario.Status == "passed" || scenario.Status == "accepted risk",
			evidence,
		); err != nil {
			return err
		}
		for _, scope := range evidence.ModuleScope {
			covered[scope] = true
		}
	}
	if scenario.Status == "passed" {
		if len(scenario.Evidence) == 0 {
			return errors.New("passed status requires evidence")
		}
		if !assuranceScopeCovered(scenario.AffectedModules, covered) {
			return errors.New("evidence does not cover every affected module")
		}
	}
	return nil
}

func validateOperationalEvidence(
	root string,
	current catalog,
	modules map[string]bool,
	digestMigrations map[string]inputDigestMigration,
	currentInputDigests map[string]string,
	requireCurrentInputs bool,
	evidence operationalEvidence,
) error {
	if err := validateAssuranceModuleScope(evidence.ModuleScope, modules); err != nil {
		return fmt.Errorf("evidence module scope: %w", err)
	}
	if err := validateAssuranceInputModuleScope(current, evidence.InputModules); err != nil {
		return err
	}
	if err := validateAssuranceInputModuleScope(current, evidence.ExactInputModules); err != nil {
		return err
	}
	if len(evidence.InputModules) != 0 && len(evidence.ExactInputModules) != 0 {
		return errors.New("evidence cannot combine additive and exact input modules")
	}
	if requireCurrentInputs && len(evidence.ExactInputModules) != 0 {
		return errors.New("current evidence cannot use an exact historical input snapshot")
	}
	if strings.TrimSpace(evidence.Environment) == "" {
		return errors.New("evidence environment is empty")
	}
	if err := validateAssuranceInputEnvironment(
		evidence.InputEnvironment,
		requireCurrentInputs,
	); err != nil {
		return err
	}
	if !isUTCRFC3339(evidence.ObservedUTC) {
		return fmt.Errorf("evidence observed_utc %q is not UTC RFC3339", evidence.ObservedUTC)
	}
	if err := validateRepositoryArtifact(
		root,
		evidence.Path,
		evidence.SHA256,
		"evidence",
	); err != nil {
		return err
	}
	if err := validateAssuranceInputDigests(
		root,
		current,
		evidence.ModuleScope,
		evidence.InputModules,
		evidence.ExactInputModules,
		evidence.InputDigests,
		digestMigrations,
		currentInputDigests,
		evidence.InputEnvironment,
		requireCurrentInputs,
	); err != nil {
		return fmt.Errorf("evidence %s: %w", evidence.Path, err)
	}
	return nil
}

func validateAssuranceInputDigests(
	root string,
	current catalog,
	scope []string,
	inputModules []string,
	exactInputModules []string,
	recorded map[string]string,
	migrations map[string]inputDigestMigration,
	cache map[string]string,
	inputEnvironment operationalInputEnvironment,
	requireCurrent bool,
) error {
	required := assuranceInputModules(current, scope, inputModules, exactInputModules)
	if len(recorded) != len(required) {
		return fmt.Errorf("input digest scope has %d modules, want %d", len(recorded), len(required))
	}
	for _, directory := range required {
		stored, exists := recorded[directory]
		if !exists {
			return fmt.Errorf("input digest for %s is missing", directory)
		}
		decoded, err := hex.DecodeString(stored)
		if err != nil || len(decoded) != sha256.Size {
			return fmt.Errorf("input digest for %s is invalid", directory)
		}
		if !requireCurrent {
			continue
		}
		cacheKey := inputEnvironment.cacheKey(directory)
		currentDigest, exists := cache[cacheKey]
		if !exists {
			command := exec.Command(
				filepath.Join(root, "scripts", "gate-input-digest.sh"),
				"operational-assurance",
				directory,
			)
			command.Dir = root
			command.Env = inputEnvironment.commandEnvironment(os.Environ())
			output, commandErr := command.Output()
			if commandErr != nil {
				return fmt.Errorf("calculate input digest for %s: %w", directory, commandErr)
			}
			currentDigest = strings.TrimSpace(string(output))
			decoded, decodeErr := hex.DecodeString(currentDigest)
			if decodeErr != nil || len(decoded) != sha256.Size {
				return fmt.Errorf("calculated input digest for %s is invalid", directory)
			}
			cache[cacheKey] = currentDigest
		}
		if !inputDigestMatches(stored, currentDigest, directory, migrations) {
			return fmt.Errorf("input digest mismatch for %s", directory)
		}
	}
	for directory := range recorded {
		if !slices.Contains(required, directory) {
			return fmt.Errorf("input digest for out-of-scope module %s", directory)
		}
	}
	return nil
}

func validateAssuranceInputEnvironment(
	environment operationalInputEnvironment,
	required bool,
) error {
	values := []string{
		environment.GoVersion,
		environment.GOOS,
		environment.GOARCH,
		environment.CGOEnabled,
		environment.Kernel,
		environment.Node,
	}
	present := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			present++
		}
		if strings.ContainsFunc(value, unicode.IsControl) {
			return errors.New("evidence input environment contains control characters")
		}
	}
	if present == 0 && !required {
		return nil
	}
	if present != len(values) {
		return errors.New("evidence input environment is incomplete")
	}
	if environment.CGOEnabled != "0" && environment.CGOEnabled != "1" {
		return errors.New("evidence input environment has invalid cgo_enabled")
	}
	return nil
}

func (environment operationalInputEnvironment) cacheKey(module string) string {
	return strings.Join([]string{
		environment.GoVersion,
		environment.GOOS,
		environment.GOARCH,
		environment.CGOEnabled,
		environment.Kernel,
		environment.Node,
		module,
	}, "\x00")
}

func (environment operationalInputEnvironment) commandEnvironment(base []string) []string {
	values := []string{
		"GOLIB_ASSURANCE_GO_VERSION=" + environment.GoVersion,
		"GOLIB_ASSURANCE_GOOS=" + environment.GOOS,
		"GOLIB_ASSURANCE_GOARCH=" + environment.GOARCH,
		"GOLIB_ASSURANCE_CGO_ENABLED=" + environment.CGOEnabled,
		"GOLIB_ASSURANCE_KERNEL=" + environment.Kernel,
		"GOLIB_ASSURANCE_NODE=" + environment.Node,
	}
	prefixes := make([]string, 0, len(values))
	for _, value := range values {
		prefixes = append(prefixes, strings.SplitN(value, "=", 2)[0]+"=")
	}
	result := make([]string, 0, len(base)+len(values))
	for _, variable := range base {
		if !slices.ContainsFunc(prefixes, func(prefix string) bool {
			return strings.HasPrefix(variable, prefix)
		}) {
			result = append(result, variable)
		}
	}
	return append(result, values...)
}

func validateInputDigestMigrations(
	root string,
	current catalog,
	migrations []inputDigestMigration,
) (map[string]inputDigestMigration, error) {
	indexed := make(map[string]inputDigestMigration, len(migrations))
	modules := make(map[string]bool, len(current.Modules))
	for _, item := range current.Modules {
		modules[item.Directory] = true
	}
	for _, migration := range migrations {
		if !modules[migration.Module] {
			return nil, fmt.Errorf("input digest migration has unknown module %s", migration.Module)
		}
		if !validSHA256(migration.FromSHA256) || !validSHA256(migration.ToSHA256) {
			return nil, fmt.Errorf("input digest migration for %s has invalid SHA-256", migration.Module)
		}
		if migration.FromSHA256 == migration.ToSHA256 {
			return nil, fmt.Errorf("input digest migration for %s does not change the digest", migration.Module)
		}
		if strings.TrimSpace(migration.Rationale) == "" {
			return nil, fmt.Errorf("input digest migration for %s has empty rationale", migration.Module)
		}
		if err := validateRepositoryArtifact(
			root,
			migration.EvidencePath,
			migration.EvidenceSHA256,
			"migration evidence",
		); err != nil {
			return nil, err
		}
		key := digestMigrationKey(migration.Module, migration.FromSHA256)
		if _, exists := indexed[key]; exists {
			return nil, fmt.Errorf(
				"duplicate input digest migration for %s from %s",
				migration.Module,
				migration.FromSHA256,
			)
		}
		indexed[key] = migration
	}
	return indexed, nil
}

func inputDigestMatches(
	stored string,
	current string,
	module string,
	migrations map[string]inputDigestMigration,
) bool {
	seen := make(map[string]bool, len(migrations))
	for stored != current {
		if seen[stored] {
			return false
		}
		seen[stored] = true
		migration, exists := migrations[digestMigrationKey(module, stored)]
		if !exists {
			return false
		}
		stored = migration.ToSHA256
	}
	return true
}

func digestMigrationKey(module, from string) string {
	return module + "\x00" + from
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validateRepositoryArtifact(root, path, expected, label string) error {
	if filepath.IsAbs(path) || filepath.Clean(path) != path ||
		path == "." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s path %q is not a clean repository-relative path", label, path)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, path))
	if err != nil {
		return fmt.Errorf("resolve %s %s: %w", label, path, err)
	}
	relative, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%s path %q escapes the repository", label, path)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("stat %s %s: %w", label, path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s %s is not a regular file", label, path)
	}
	contents, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf("read %s %s: %w", label, path, err)
	}
	if !validSHA256(expected) {
		return fmt.Errorf("%s %s has invalid SHA-256", label, path)
	}
	got := sha256.Sum256(contents)
	if expected != hex.EncodeToString(got[:]) {
		return fmt.Errorf("%s digest mismatch for %s", label, path)
	}
	return nil
}

func assuranceInputModules(current catalog, scope, inputModules, exactInputModules []string) []string {
	if len(exactInputModules) != 0 {
		required := slices.Clone(exactInputModules)
		sort.Strings(required)
		return required
	}
	selected := make(map[string]bool, len(scope)+len(inputModules))
	explicitInputs := make(map[string]bool, len(inputModules))
	if slices.Contains(scope, "*") {
		for _, item := range current.Modules {
			if item.Releasable {
				selected[item.Directory] = true
			}
		}
	} else {
		for _, directory := range scope {
			selected[directory] = true
		}
	}
	for _, directory := range inputModules {
		selected[directory] = true
		explicitInputs[directory] = true
	}
	required := make([]string, 0, len(selected))
	for _, item := range current.Modules {
		if selected[item.Directory] && (item.Releasable || explicitInputs[item.Directory]) {
			required = append(required, item.Directory)
		}
	}
	sort.Strings(required)
	return required
}

func validateAssuranceInputModuleScope(current catalog, inputModules []string) error {
	known := make(map[string]bool, len(current.Modules))
	for _, item := range current.Modules {
		known[item.Directory] = true
	}
	seen := make(map[string]bool, len(inputModules))
	for _, directory := range inputModules {
		if directory == "*" || !known[directory] {
			return fmt.Errorf("unknown evidence input module %s", directory)
		}
		if seen[directory] {
			return fmt.Errorf("duplicate evidence input module %s", directory)
		}
		seen[directory] = true
	}
	return nil
}

func isUTCRFC3339(value string) bool {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || strings.HasSuffix(value, "-00:00") {
		return false
	}
	_, offset := parsed.Zone()
	return offset == 0
}

func validateAssuranceModuleScope(scope []string, modules map[string]bool) error {
	if len(scope) == 0 {
		return errors.New("scope is empty")
	}
	seen := map[string]bool{}
	for _, directory := range scope {
		if seen[directory] {
			return fmt.Errorf("duplicate module %s", directory)
		}
		seen[directory] = true
		if directory == "*" {
			if len(scope) != 1 {
				return errors.New("wildcard cannot be combined with module names")
			}
			continue
		}
		if !modules[directory] {
			return fmt.Errorf("unknown or non-releasable module %s", directory)
		}
	}
	return nil
}

func assuranceScopeCovered(affected []string, covered map[string]bool) bool {
	if covered["*"] {
		return true
	}
	for _, directory := range affected {
		if directory == "*" || !covered[directory] {
			return false
		}
	}
	return true
}

func validateAcceptedRiskVerdict(record operationalAssuranceRecord) error {
	approvals := map[string]operationalRiskApproval{}
	for _, approval := range record.RiskAcceptances {
		approvals[approval.ID] = approval
	}
	for _, risk := range record.ResidualRisks {
		if _, accepted := approvals[risk]; !accepted {
			return fmt.Errorf("residual risk %s has no explicit acceptance", risk)
		}
	}
	scenarios := make(map[string]operationalScenario, len(record.Scenarios))
	for _, scenario := range record.Scenarios {
		scenarios[scenario.ID] = scenario
	}
	for risk, approval := range approvals {
		for _, scenarioID := range approval.Scenarios {
			scenario := scenarios[scenarioID]
			if scenario.Status != "accepted risk" || !slices.Contains(scenario.AcceptedRisks, risk) {
				return fmt.Errorf("risk acceptance %s scenario %s does not name risk", risk, scenarioID)
			}
		}
	}
	for _, scenario := range record.Scenarios {
		if scenario.Status != "passed" && scenario.Status != "accepted risk" {
			return errors.New("accepted-risk verdict requires every scenario to pass or name an accepted risk")
		}
		for _, risk := range scenario.AcceptedRisks {
			approval, accepted := approvals[risk]
			if !accepted {
				return fmt.Errorf("scenario %s references unaccepted risk %s", scenario.ID, risk)
			}
			if !slices.Contains(approval.Scenarios, scenario.ID) {
				return fmt.Errorf("risk acceptance %s does not authorize scenario %s", risk, scenario.ID)
			}
		}
	}
	return nil
}
