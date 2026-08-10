package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	decisionHeadingPattern = regexp.MustCompile(
		`^([A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*-DEC-([0-9]{3})): (.+)$`,
	)
	evidenceIdentifierPattern = regexp.MustCompile(
		`\b(?:Test|Fuzz|Benchmark)[A-Z][A-Za-z0-9_]*\*?`,
	)
	authoritativeURLPattern  = regexp.MustCompile(`https?://[^\s)>|]+`)
	httpsURLPattern          = regexp.MustCompile(`https://[^\s)>|]+`)
	sha256Pattern            = regexp.MustCompile(`(?i)\b[0-9a-f]{64}\b`)
	markdownLinkPattern      = regexp.MustCompile(`\[[^]]*]\(([^)]+)\)`)
	decisionStatusPattern    = regexp.MustCompile("(?i)`(resolved|unresolved|superseded)`")
	decisionReferencePattern = regexp.MustCompile(
		`\b[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*-DEC-[0-9]{3}\b`,
	)
	sha256ValuePattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	markdownHeadingPattern = regexp.MustCompile(`^#{1,6}[ \t]+(.+?)[ \t]*#*[ \t]*$`)
)

type specificationDecision struct {
	identifier string
	number     string
	body       string
}

func validateSpecifications(root string, arguments []string) {
	flags := flag.NewFlagSet("specifications", flag.ExitOnError)
	all := flags.Bool("all", false, "validate every specification-backed module")
	explicit := flags.String("modules", "", "comma-separated module directories or paths")
	if err := flags.Parse(arguments); err != nil {
		fatal("parse specification selection: %v", err)
	}

	current, err := discover(root)
	if err != nil {
		fatal("discover modules: %v", err)
	}
	selected, err := selectSpecificationDecisionModules(current, *all, *explicit)
	if err != nil {
		fatal("select specification modules: %v", err)
	}
	if err := validateSpecificationDecisions(root, selected); err != nil {
		fatal("validate specification decisions: %v", err)
	}
	fmt.Printf("validated specification decisions for %d modules\n", specificationDecisionModuleCount(selected))
}

func selectSpecificationDecisionModules(current catalog, all bool, explicit string) (catalog, error) {
	if all && strings.TrimSpace(explicit) != "" {
		return catalog{}, errors.New("--all and --modules cannot be combined")
	}
	if all {
		return current, nil
	}
	if strings.TrimSpace(explicit) == "" {
		return catalog{}, errors.New("one of --all or --modules is required")
	}

	wanted := make(map[string]bool)
	for value := range strings.SplitSeq(explicit, ",") {
		value = strings.TrimSpace(value)
		resolved := resolveModule(current, value)
		if resolved == "" {
			return catalog{}, fmt.Errorf("unknown module %q", value)
		}
		wanted[resolved] = true
	}

	selected := current
	selected.Modules = make([]module, 0, len(wanted))
	for _, item := range current.Modules {
		if wanted[item.Directory] {
			selected.Modules = append(selected.Modules, item)
		}
	}
	return selected, nil
}

func validateSpecificationDecisions(root string, current catalog) error {
	problems := []error{}
	for _, item := range current.Modules {
		if !requiresSpecificationDecisionRegister(item) {
			continue
		}
		if err := validateSpecificationDecisionModule(root, item); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", item.Directory, err))
		}
	}
	return errors.Join(problems...)
}

func requiresSpecificationDecisionRegister(item module) bool {
	return (len(item.Specifications) != 0 || len(item.Provenance) != 0) &&
		(item.Kind == "public library" || item.Kind == "adapter")
}

func specificationDecisionModuleCount(current catalog) int {
	count := 0
	for _, item := range current.Modules {
		if requiresSpecificationDecisionRegister(item) {
			count++
		}
	}
	return count
}

func validateSpecificationDecisionModule(root string, item module) error {
	problems := []error{}
	if len(item.Specifications) == 0 {
		problems = append(problems, errors.New("provenance exists without specification metadata"))
	}
	if len(item.ConformanceCorpora) == 0 {
		problems = append(problems, errors.New("specification-backed module has no conformance corpus"))
	}
	if len(item.Provenance) == 0 {
		problems = append(problems, errors.New("specification-backed module has no provenance manifest"))
	}
	if !item.Gates["conformance"] {
		problems = append(problems, errors.New("specification-backed module has no required conformance gate"))
	}
	for _, provenance := range item.Provenance {
		if err := validateSpecificationProvenance(root, item.Directory, provenance); err != nil {
			problems = append(problems, err)
		}
	}

	registerPath := filepath.Join(root, item.Directory, "docs", "specification-decisions.md")
	contents, err := os.ReadFile(registerPath)
	if err != nil {
		problems = append(problems, fmt.Errorf("read decision register: %w", err))
		return errors.Join(problems...)
	}
	if err := validateUnresolvedDecisionInventory(string(contents)); err != nil {
		problems = append(problems, err)
	}
	if err := validateSpecificationDecisionLinks(
		filepath.Join(root, item.Directory),
		registerPath,
	); err != nil {
		problems = append(problems, err)
	}
	decisions, parseErr := parseSpecificationDecisions(string(contents))
	if parseErr != nil {
		problems = append(problems, parseErr)
		return errors.Join(problems...)
	}
	evidence, evidenceErr := specificationEvidenceIdentifiers(
		filepath.Join(root, item.Directory),
	)
	if evidenceErr != nil {
		problems = append(problems, fmt.Errorf("inventory executable evidence: %w", evidenceErr))
		return errors.Join(problems...)
	}
	knownDecisions := make(map[string]bool, len(decisions))
	for _, decision := range decisions {
		knownDecisions[decision.identifier] = true
	}
	for _, decision := range decisions {
		if decisionErr := validateSpecificationDecision(
			registerPath,
			decision,
			evidence,
			knownDecisions,
		); decisionErr != nil {
			problems = append(problems, decisionErr)
		}
	}
	return errors.Join(problems...)
}

func validateUnresolvedDecisionInventory(contents string) error {
	lines := strings.Split(strings.ReplaceAll(contents, "\r\n", "\n"), "\n")
	sections := []string{}
	nonterminal := false
	for index, line := range lines {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		if !isUnresolvedDecisionInventoryHeading(strings.TrimSpace(strings.TrimPrefix(line, "## "))) {
			nonterminal = nonterminal || len(sections) != 0
			continue
		}

		body := []string{}
		for next := index + 1; next < len(lines) && !strings.HasPrefix(lines[next], "## "); next++ {
			body = append(body, lines[next])
		}
		sections = append(sections, strings.Join(strings.Fields(strings.Join(body, "\n")), " "))
	}

	if len(sections) == 0 {
		return errors.New("decision register has no unresolved decision inventory")
	}
	if len(sections) > 1 {
		return errors.New("decision register has more than one unresolved decision inventory")
	}
	if nonterminal {
		return errors.New("unresolved decision inventory must be the final level-two section")
	}
	if sections[0] == "" {
		return errors.New("unresolved decision inventory has no disposition")
	}

	disposition := strings.ToLower(sections[0])
	closedWithNone := disposition == "none" ||
		strings.HasPrefix(disposition, "none.") ||
		strings.HasPrefix(disposition, "none ")
	closedWithStatement := strings.HasPrefix(disposition, "no known ") &&
		(strings.Contains(disposition, " unresolved") ||
			strings.Contains(disposition, " remains open"))
	if !closedWithNone && !closedWithStatement {
		return errors.New("unresolved decision inventory remains open")
	}
	return nil
}

func isUnresolvedDecisionInventoryHeading(heading string) bool {
	switch strings.ToLower(strings.TrimSpace(heading)) {
	case "unresolved decisions", "unresolved and excluded behavior":
		return true
	default:
		return false
	}
}

func validateSpecificationDecisionLinks(moduleRoot, registerPath string) error {
	problems := []error{}
	type decisionLinkDocument struct {
		relative string
		required bool
	}
	documents := []decisionLinkDocument{
		{relative: "README.md", required: true},
		{relative: "CONTRIBUTING.md"},
	}
	for _, pattern := range []string{"docs/conformance*.md", "docs/compatib*.md"} {
		matches, err := filepath.Glob(filepath.Join(moduleRoot, filepath.FromSlash(pattern)))
		if err != nil {
			problems = append(problems, fmt.Errorf("discover %s: %w", pattern, err))
			continue
		}
		for _, match := range matches {
			relative, err := filepath.Rel(moduleRoot, match)
			if err != nil {
				problems = append(problems, fmt.Errorf("resolve documentation path %s: %w", match, err))
				continue
			}
			documents = append(
				documents,
				decisionLinkDocument{relative: filepath.ToSlash(relative)},
			)
		}
	}

	for _, document := range documents {
		path := filepath.Join(moduleRoot, filepath.FromSlash(document.relative))
		contents, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) && !document.required {
			continue
		}
		if err != nil {
			problems = append(problems, fmt.Errorf("read %s: %w", document.relative, err))
			continue
		}
		if !markdownLinksToFile(path, registerPath, contents) {
			problems = append(
				problems,
				fmt.Errorf("%s does not link docs/specification-decisions.md", document.relative),
			)
		}
	}

	return errors.Join(problems...)
}

func markdownLinksToFile(documentPath, targetPath string, contents []byte) bool {
	for _, match := range markdownLinkPattern.FindAllSubmatch(contents, -1) {
		target := strings.Trim(strings.TrimSpace(string(match[1])), "<>")
		parsed, err := url.Parse(target)
		if err != nil || parsed.IsAbs() || parsed.Path == "" {
			continue
		}
		decoded, err := url.PathUnescape(parsed.Path)
		if err != nil {
			continue
		}
		candidate := filepath.Clean(filepath.Join(
			filepath.Dir(documentPath),
			filepath.FromSlash(decoded),
		))
		if candidate == filepath.Clean(targetPath) {
			return true
		}
	}
	return false
}

func validateSpecificationProvenance(root, moduleDirectory, provenance string) error {
	clean := filepath.Clean(filepath.FromSlash(provenance))
	moduleRoot := filepath.Clean(filepath.Join(root, moduleDirectory))
	path := filepath.Clean(filepath.Join(root, clean))
	relative, err := filepath.Rel(moduleRoot, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("provenance path %q is outside the module", provenance)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("read provenance %s: %w", provenance, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("provenance %s is not a non-empty regular file", provenance)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read provenance %s: %w", provenance, err)
	}
	if !httpsURLPattern.Match(contents) {
		return fmt.Errorf("provenance %s has no pinned HTTPS source", provenance)
	}
	if !sha256Pattern.Match(contents) {
		return fmt.Errorf("provenance %s has no SHA-256 source digest", provenance)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".tsv":
		if err := validateSpecificationTSV(contents); err != nil {
			return fmt.Errorf("provenance %s: %w", provenance, err)
		}
	case ".json":
		if err := validateSpecificationJSON(contents); err != nil {
			return fmt.Errorf("provenance %s: %w", provenance, err)
		}
	default:
		return fmt.Errorf("provenance %s uses an unsupported manifest format", provenance)
	}
	return nil
}

type specificationJSONState struct {
	hasURL    bool
	hasDigest bool
	hasPin    bool
}

func validateSpecificationJSON(contents []byte) error {
	var document any
	if err := json.Unmarshal(contents, &document); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}
	state := specificationJSONState{}
	if err := inspectSpecificationJSON(document, "$", &state); err != nil {
		return err
	}
	if !state.hasURL || !state.hasDigest || !state.hasPin {
		return errors.New("JSON must contain an authoritative URL, SHA-256 digest, and version or revision pin")
	}
	return nil
}

func inspectSpecificationJSON(value any, path string, state *specificationJSONState) error {
	switch typed := value.(type) {
	case []any:
		for index, item := range typed {
			if err := inspectSpecificationJSON(item, fmt.Sprintf("%s[%d]", path, index), state); err != nil {
				return err
			}
		}
	case map[string]any:
		_, hasPath := typed["path"]
		hasObjectDigest := false
		for key, item := range typed {
			normalizedKey := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(key))
			text, isText := item.(string)
			if strings.Contains(normalizedKey, "sha256") {
				hasObjectDigest = true
				state.hasDigest = true
				if !isText || !sha256ValuePattern.MatchString(strings.ToLower(strings.TrimSpace(text))) {
					return fmt.Errorf("%s.%s has invalid sha256", path, key)
				}
			}
			if isText && (strings.HasPrefix(text, "http://") || strings.HasPrefix(text, "https://")) {
				parsed, err := url.Parse(text)
				if err != nil || parsed.Host == "" {
					return fmt.Errorf("%s.%s has invalid source URL", path, key)
				}
				state.hasURL = true
			}
			switch normalizedKey {
			case "version", "revision", "release", "commit", "mergecommit", "retrieved", "retrievedat", "reviewedat", "generatedat", "released":
				if isText && strings.TrimSpace(text) != "" {
					state.hasPin = true
				}
			}
			if err := inspectSpecificationJSON(item, path+"."+key, state); err != nil {
				return err
			}
		}
		if hasPath && !hasObjectDigest {
			return fmt.Errorf("%s has a path without sha256 provenance", path)
		}
	}
	return nil
}

func validateSpecificationTSV(contents []byte) error {
	reader := csv.NewReader(strings.NewReader(string(contents)))
	reader.Comma = '\t'
	records, err := reader.ReadAll()
	if err != nil {
		return fmt.Errorf("parse TSV: %w", err)
	}
	if len(records) < 2 {
		return errors.New("TSV has no source records")
	}
	columns := map[string]int{}
	for index, name := range records[0] {
		name = strings.TrimSpace(strings.ToLower(name))
		_, duplicate := columns[name]
		if name == "" || duplicate {
			return fmt.Errorf("TSV has an empty or duplicate column %q", name)
		}
		columns[name] = index
	}
	for _, required := range []string{"id", "version", "url", "sha256", "status"} {
		if _, ok := columns[required]; !ok {
			return fmt.Errorf("TSV is missing %s column", required)
		}
	}
	seen := map[string]bool{}
	for rowIndex, record := range records[1:] {
		row := rowIndex + 2
		identifier := strings.TrimSpace(record[columns["id"]])
		if identifier == "" || seen[identifier] {
			return fmt.Errorf("TSV row %d has an empty or duplicate id %q", row, identifier)
		}
		seen[identifier] = true
		if strings.TrimSpace(record[columns["version"]]) == "" {
			return fmt.Errorf("TSV row %d has no version", row)
		}
		digest := strings.ToLower(strings.TrimSpace(record[columns["sha256"]]))
		if !sha256ValuePattern.MatchString(digest) {
			return fmt.Errorf("TSV row %d has invalid sha256", row)
		}
		status := strings.TrimSpace(record[columns["status"]])
		if status != "pinned" && status != "snapshot" {
			return fmt.Errorf("TSV row %d has unsupported status %q", row, status)
		}
		source, parseErr := url.Parse(strings.TrimSpace(record[columns["url"]]))
		if parseErr != nil || source.Host == "" ||
			(source.Scheme != "http" && source.Scheme != "https") {
			return fmt.Errorf("TSV row %d has invalid source URL", row)
		}
	}
	return nil
}

func parseSpecificationDecisions(contents string) ([]specificationDecision, error) {
	lines := strings.Split(strings.ReplaceAll(contents, "\r\n", "\n"), "\n")
	decisions := []specificationDecision{}
	seen := map[string]bool{}
	current := -1
	for _, line := range lines {
		if !strings.HasPrefix(line, "## ") {
			if current >= 0 {
				decisions[current].body += line + "\n"
			}
			continue
		}
		heading := strings.TrimPrefix(line, "## ")
		if isUnresolvedDecisionInventoryHeading(heading) {
			current = -1
			continue
		}
		matches := decisionHeadingPattern.FindStringSubmatch(heading)
		if matches == nil {
			return nil, fmt.Errorf("heading %q has no stable decision identifier", heading)
		}
		identifier := matches[1]
		if seen[identifier] {
			return nil, fmt.Errorf("duplicate decision identifier %s", identifier)
		}
		seen[identifier] = true
		decisions = append(decisions, specificationDecision{
			identifier: identifier,
			number:     matches[2],
		})
		current = len(decisions) - 1
	}
	if len(decisions) == 0 {
		return nil, errors.New("decision register has no stable decision identifiers")
	}
	seriesCounts := map[string]int{}
	for _, decision := range decisions {
		series := strings.TrimSuffix(decision.identifier, "-DEC-"+decision.number)
		seriesCounts[series]++
		want := fmt.Sprintf("%03d", seriesCounts[series])
		if decision.number != want {
			return nil, fmt.Errorf(
				"decision %s has sequence %s, want %s",
				decision.identifier,
				decision.number,
				want,
			)
		}
	}
	return decisions, nil
}

func validateSpecificationDecision(
	registerPath string,
	decision specificationDecision,
	knownEvidence map[string]bool,
	knownDecisions map[string]bool,
) error {
	normalized := strings.ToLower(decision.body)
	required := []struct {
		name  string
		terms []string
	}{
		{name: "status", terms: []string{"status"}},
		{name: "owner", terms: []string{"owner"}},
		{name: "source", terms: []string{"source"}},
		{name: "classification", terms: []string{"classification"}},
		{name: "issue", terms: []string{"issue"}},
		{name: "credible interpretations", terms: []string{"interpretation"}},
		{name: "known peer behavior", terms: []string{"peer"}},
		{name: "selected behavior", terms: []string{"selected behavior"}},
		{name: "security consequences", terms: []string{"security", "consequence"}},
		{name: "resource consequences", terms: []string{"resource", "consequence"}},
		{name: "compatibility consequences", terms: []string{"compatibility", "consequence"}},
		{name: "wire consequences", terms: []string{"wire", "consequence"}},
		{name: "executable evidence", terms: []string{"evidence"}},
		{name: "public surface", terms: []string{"public surface"}},
		{name: "upstream record", terms: []string{"upstream"}},
		{name: "reconsideration condition", terms: []string{"reconsider"}},
	}
	problems := []error{}
	for _, field := range required {
		missing := false
		for _, term := range field.terms {
			if !strings.Contains(normalized, term) {
				missing = true
				break
			}
		}
		if missing {
			problems = append(problems, fmt.Errorf("%s is missing %s", decision.identifier, field.name))
		}
	}
	if !authoritativeURLPattern.MatchString(decision.body) {
		problems = append(problems, fmt.Errorf("%s has no authoritative HTTPS source", decision.identifier))
	}
	status, statusErr := specificationDecisionStatus(decision.body)
	if statusErr != nil {
		problems = append(problems, fmt.Errorf("%s: %w", decision.identifier, statusErr))
	} else if status == "unresolved" {
		problems = append(problems, fmt.Errorf("%s remains unresolved", decision.identifier))
	} else if status == "superseded" {
		if !hasKnownReplacementDecision(decision, knownDecisions) {
			problems = append(problems, fmt.Errorf(
				"%s is superseded without a known replacement decision",
				decision.identifier,
			))
		}
	}

	references := evidenceIdentifierPattern.FindAllString(decision.body, -1)
	if len(references) == 0 {
		problems = append(problems, fmt.Errorf("%s has no executable evidence identifier", decision.identifier))
	}
	for _, reference := range references {
		if !specificationEvidenceExists(knownEvidence, reference) {
			problems = append(problems, fmt.Errorf(
				"%s references missing executable evidence %s",
				decision.identifier,
				reference,
			))
		}
	}
	for _, match := range markdownLinkPattern.FindAllStringSubmatch(decision.body, -1) {
		if err := validateDecisionLink(registerPath, match[1]); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", decision.identifier, err))
		}
	}
	return errors.Join(problems...)
}

func specificationDecisionStatus(body string) (string, error) {
	statuses := []string{}
	for line := range strings.SplitSeq(body, "\n") {
		field := strings.ToLower(strings.TrimSpace(line))
		if !strings.HasPrefix(field, "| status") &&
			!strings.HasPrefix(field, "- **status") &&
			!strings.HasPrefix(field, "**status") &&
			!strings.HasPrefix(field, "status:") {
			continue
		}
		for _, match := range decisionStatusPattern.FindAllStringSubmatch(line, -1) {
			statuses = append(statuses, strings.ToLower(match[1]))
		}
	}
	if len(statuses) == 0 {
		return "", errors.New("no recognized decision status")
	}
	if len(statuses) > 1 {
		return "", errors.New("more than one decision status")
	}
	return statuses[0], nil
}

func hasKnownReplacementDecision(
	decision specificationDecision,
	knownDecisions map[string]bool,
) bool {
	for line := range strings.SplitSeq(decision.body, "\n") {
		normalized := strings.ToLower(line)
		if !strings.Contains(normalized, "replacement") &&
			!strings.Contains(normalized, "replaced by") {
			continue
		}
		for _, reference := range decisionReferencePattern.FindAllString(line, -1) {
			if reference != decision.identifier && knownDecisions[reference] {
				return true
			}
		}
	}
	return false
}

func validateDecisionLink(registerPath, target string) error {
	parsed, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("invalid link %q: %w", target, err)
	}
	if parsed.IsAbs() {
		if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("authoritative link %q must use HTTP or HTTPS", target)
		}
		return nil
	}
	if parsed.Path == "" && parsed.Fragment == "" {
		return nil
	}
	path := registerPath
	if parsed.Path != "" {
		path = filepath.Clean(filepath.Join(filepath.Dir(registerPath), filepath.FromSlash(parsed.Path)))
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("local link %q is broken: %w", target, err)
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("local link %q is not a file or directory", target)
	}
	if parsed.Fragment != "" && info.Mode().IsRegular() &&
		(strings.EqualFold(filepath.Ext(path), ".md") || strings.EqualFold(filepath.Ext(path), ".markdown")) {
		if err := validateMarkdownAnchor(path, parsed.Fragment); err != nil {
			return fmt.Errorf("local link %q: %w", target, err)
		}
	}
	return nil
}

func validateMarkdownAnchor(path, fragment string) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read Markdown anchors: %w", err)
	}
	seen := map[string]int{}
	for line := range strings.SplitSeq(strings.ReplaceAll(string(contents), "\r\n", "\n"), "\n") {
		match := markdownHeadingPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		base := markdownAnchor(match[1])
		if base == "" {
			continue
		}
		anchor := base
		if count := seen[base]; count != 0 {
			anchor = fmt.Sprintf("%s-%d", base, count)
		}
		seen[base]++
		if anchor == fragment {
			return nil
		}
	}
	return fmt.Errorf("Markdown anchor %q does not exist", fragment)
}

func markdownAnchor(heading string) string {
	var anchor strings.Builder
	for _, character := range strings.ToLower(heading) {
		switch {
		case unicode.IsLetter(character), unicode.IsNumber(character),
			character == '-', character == '_':
			anchor.WriteRune(character)
		case character == ' ' || character == '\t':
			anchor.WriteByte('-')
		}
	}
	return strings.Trim(anchor.String(), "-")
}

func specificationEvidenceExists(knownEvidence map[string]bool, reference string) bool {
	if !strings.HasSuffix(reference, "*") {
		return knownEvidence[reference]
	}
	prefix := strings.TrimSuffix(reference, "*")
	for candidate := range knownEvidence {
		if strings.HasPrefix(candidate, prefix) {
			return true
		}
	}
	return false
}

func specificationEvidenceIdentifiers(moduleRoot string) (map[string]bool, error) {
	identifiers := map[string]bool{}
	err := filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != moduleRoot && excludedSourceDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			if path != moduleRoot {
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
		file, parseErr := parser.ParseFile(
			token.NewFileSet(),
			path,
			nil,
			parser.SkipObjectResolution,
		)
		if parseErr != nil {
			return parseErr
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if ok && function.Recv == nil && evidenceIdentifierPattern.MatchString(function.Name.Name) {
				identifiers[function.Name.Name] = true
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return identifiers, nil
}
