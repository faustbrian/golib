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
	SchemaVersion   int                       `json:"schema_version"`
	Verdict         string                    `json:"verdict"`
	Modules         []string                  `json:"modules"`
	Scenarios       []operationalScenario     `json:"scenarios"`
	ResidualRisks   []string                  `json:"residual_risks"`
	RiskAcceptances []operationalRiskApproval `json:"risk_acceptances"`
}

type operationalScenario struct {
	ID              string                `json:"id"`
	Status          string                `json:"status"`
	AffectedModules []string              `json:"affected_modules"`
	Evidence        []operationalEvidence `json:"evidence"`
	AcceptedRisks   []string              `json:"accepted_risks"`
}

type operationalEvidence struct {
	Path         string            `json:"path"`
	SHA256       string            `json:"sha256"`
	ObservedUTC  string            `json:"observed_utc"`
	Environment  string            `json:"environment"`
	ModuleScope  []string          `json:"module_scope"`
	InputModules []string          `json:"input_modules,omitempty"`
	InputDigests map[string]string `json:"input_digests"`
}

type operationalRiskApproval struct {
	ID          string   `json:"id"`
	AcceptedBy  string   `json:"accepted_by"`
	AcceptedUTC string   `json:"accepted_utc"`
	Rationale   string   `json:"rationale"`
	Scenarios   []string `json:"scenarios"`
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
			currentInputDigests,
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
	currentInputDigests map[string]string,
	evidence operationalEvidence,
) error {
	if err := validateAssuranceModuleScope(evidence.ModuleScope, modules); err != nil {
		return fmt.Errorf("evidence module scope: %w", err)
	}
	if err := validateAssuranceInputModuleScope(current, evidence.InputModules); err != nil {
		return err
	}
	if strings.TrimSpace(evidence.Environment) == "" {
		return errors.New("evidence environment is empty")
	}
	if !isUTCRFC3339(evidence.ObservedUTC) {
		return fmt.Errorf("evidence observed_utc %q is not UTC RFC3339", evidence.ObservedUTC)
	}
	if filepath.IsAbs(evidence.Path) || filepath.Clean(evidence.Path) != evidence.Path ||
		evidence.Path == "." || strings.HasPrefix(evidence.Path, ".."+string(filepath.Separator)) {
		return fmt.Errorf("evidence path %q is not a clean repository-relative path", evidence.Path)
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root, evidence.Path))
	if err != nil {
		return fmt.Errorf("resolve evidence %s: %w", evidence.Path, err)
	}
	relative, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("evidence path %q escapes the repository", evidence.Path)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("stat evidence %s: %w", evidence.Path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("evidence %s is not a regular file", evidence.Path)
	}
	contents, err := os.ReadFile(resolved)
	if err != nil {
		return fmt.Errorf("read evidence %s: %w", evidence.Path, err)
	}
	want, err := hex.DecodeString(evidence.SHA256)
	if err != nil || len(want) != sha256.Size {
		return fmt.Errorf("evidence %s has invalid SHA-256", evidence.Path)
	}
	got := sha256.Sum256(contents)
	if !slices.Equal(want, got[:]) {
		return fmt.Errorf("evidence %s digest mismatch", evidence.Path)
	}
	if err := validateAssuranceInputDigests(
		root,
		current,
		evidence.ModuleScope,
		evidence.InputModules,
		evidence.InputDigests,
		currentInputDigests,
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
	recorded map[string]string,
	cache map[string]string,
) error {
	required := assuranceInputModules(current, scope, inputModules)
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
		currentDigest, exists := cache[directory]
		if !exists {
			command := exec.Command(
				filepath.Join(root, "scripts", "gate-input-digest.sh"),
				"operational-assurance",
				directory,
			)
			command.Dir = root
			output, commandErr := command.Output()
			if commandErr != nil {
				return fmt.Errorf("calculate input digest for %s: %w", directory, commandErr)
			}
			currentDigest = strings.TrimSpace(string(output))
			decoded, decodeErr := hex.DecodeString(currentDigest)
			if decodeErr != nil || len(decoded) != sha256.Size {
				return fmt.Errorf("calculated input digest for %s is invalid", directory)
			}
			cache[directory] = currentDigest
		}
		if stored != currentDigest {
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

func assuranceInputModules(current catalog, scope, inputModules []string) []string {
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
	expandReverseDependencies(current, selected)
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
