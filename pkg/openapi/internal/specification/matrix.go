// Package specification maintains traceability to pinned OpenAPI prose.
package specification

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

const maxSpecificationLineBytes = 1_048_576

var normativeKeyword = regexp.MustCompile(
	`\b(MUST NOT|SHALL NOT|SHOULD NOT|NOT RECOMMENDED|MUST|SHALL|REQUIRED|SHOULD|RECOMMENDED|MAY|OPTIONAL)\b`,
)

// Occurrence records one normative keyword occurrence in pinned prose. A
// sentence containing more than one keyword produces more than one occurrence
// so evidence cannot accidentally cover an unreviewed clause.
type Occurrence struct {
	ID      string
	Version string
	Source  string
	Line    int
	Section string
	Keyword string
	Text    string
}

// WriteNormativeTSV writes the stable normative occurrence interchange used
// by conformance tooling and human review.
func WriteNormativeTSV(writer io.Writer, occurrences []Occurrence) error {
	if writer == nil {
		return errors.New("specification: writer is required")
	}

	tsv := csv.NewWriter(writer)
	tsv.Comma = '\t'
	_ = tsv.Write([]string{"id", "version", "source", "line", "section", "keyword", "text"})
	for _, occurrence := range occurrences {
		record := []string{
			occurrence.ID,
			occurrence.Version,
			occurrence.Source,
			strconv.Itoa(occurrence.Line),
			occurrence.Section,
			occurrence.Keyword,
			occurrence.Text,
		}
		_ = tsv.Write(record)
	}
	tsv.Flush()
	if err := tsv.Error(); err != nil {
		return fmt.Errorf("specification: flush normative occurrences: %w", err)
	}

	return nil
}

// WriteInitialEvidenceTSV creates an explicit evidence row for every
// occurrence. It is intended only for first-time generation because later
// evidence updates are human-reviewed conformance claims.
func WriteInitialEvidenceTSV(writer io.Writer, occurrences []Occurrence) error {
	if writer == nil {
		return errors.New("specification: writer is required")
	}

	tsv := csv.NewWriter(writer)
	tsv.Comma = '\t'
	_ = tsv.Write([]string{"id", "status", "implementation", "tests", "documentation", "notes"})
	for _, occurrence := range occurrences {
		_ = tsv.Write([]string{occurrence.ID, "unimplemented", "", "", "", ""})
	}
	tsv.Flush()
	if err := tsv.Error(); err != nil {
		return fmt.Errorf("specification: flush evidence rows: %w", err)
	}

	return nil
}

// ExtractNormative inventories every BCP 14 keyword occurrence outside the
// specification's keyword-definition boilerplate.
func ExtractNormative(version string, source string, reader io.Reader) ([]Occurrence, error) {
	if version == "" {
		return nil, errors.New("specification: version is required")
	}
	if source == "" {
		return nil, errors.New("specification: source is required")
	}
	if reader == nil {
		return nil, errors.New("specification: reader is required")
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 65_536), maxSpecificationLineBytes)

	var occurrences []Occurrence
	section := "Document"
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.ReplaceAll(scanner.Text(), "\t", " "))
		if heading, ok := markdownHeading(line); ok {
			section = heading
			continue
		}
		if line == "" || isKeywordDefinition(line) {
			continue
		}

		for _, match := range normativeKeyword.FindAllString(line, -1) {
			occurrences = append(occurrences, Occurrence{
				ID:      fmt.Sprintf("OAS-%s-%04d", version, len(occurrences)+1),
				Version: version,
				Source:  source,
				Line:    lineNumber,
				Section: section,
				Keyword: match,
				Text:    line,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("specification: scan normative prose: %w", err)
	}

	return occurrences, nil
}

func markdownHeading(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, "#")
	if len(trimmed) == len(line) || trimmed == "" || trimmed[0] != ' ' {
		return "", false
	}

	return strings.TrimSpace(trimmed), true
}

func isKeywordDefinition(line string) bool {
	lower := strings.ToLower(line)
	return strings.Contains(lower, "interpreted as described in") ||
		strings.Contains(lower, "interpreted as described by")
}
