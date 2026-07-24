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

var (
	htmlAnchor      = regexp.MustCompile(`(?i)</?a(?:\s+[^>]*)?>`)
	tableSeparator  = regexp.MustCompile(`^:?-{3,}:?$`)
	requiredMarker  = regexp.MustCompile(`(?i)(\*\*required\*\*|\brequired\b)`)
	markdownLink    = regexp.MustCompile(`\[([^\[\]]+)]\([^)]+\)`)
	objectNameSpace = regexp.MustCompile(`\s+`)
)

// ObjectField records one fixed or patterned field occurrence in normative
// object prose. Variant tables deliberately retain repeated field names.
type ObjectField struct {
	ID          string
	Version     string
	Source      string
	Line        int
	Object      string
	Variant     string
	Name        string
	Type        string
	Pattern     bool
	Required    bool
	Description string
}

// ExtractObjectFields inventories object table rows from pinned normative
// Markdown without treating the informative JSON Schemas as authoritative.
func ExtractObjectFields(version string, source string, reader io.Reader) ([]ObjectField, error) {
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
	lines := make([]string, 0)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("specification: scan object prose: %w", err)
	}

	var fields []ObjectField
	object := ""
	objectLevel := 0
	variant := ""
	var tableHeader []string
	expectSeparator := false
	inTable := false
	pattern := false
	for index, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if level, heading, ok := markdownHeadingWithLevel(line); ok {
			expectSeparator = false
			inTable = false
			if strings.HasSuffix(heading, " Object") {
				object = normalizeMarkdownText(heading)
				objectLevel = level
				variant = ""
			} else if object != "" && level <= objectLevel {
				object = ""
				objectLevel = 0
				variant = ""
			} else if object != "" && strings.Contains(heading, "Fields") {
				variant = normalizeMarkdownText(heading)
			}
			continue
		}
		if object == "" {
			expectSeparator = false
			inTable = false
			continue
		}
		if expectSeparator {
			separator := splitMarkdownTableRow(line)
			expectSeparator = false
			if len(separator) == len(tableHeader) && isTableSeparator(separator) {
				pattern = tableHeader[0] == "Field Pattern"
				inTable = true
			}
			continue
		}
		if inTable {
			row := splitMarkdownTableRow(line)
			if len(row) >= 3 {
				name := normalizeMarkdownText(row[0])
				if name == "" {
					continue
				}
				description := normalizeWhitespace(strings.Join(row[2:], " | "))
				fields = append(fields, ObjectField{
					ID:          fmt.Sprintf("OAS-%s-F%04d", version, len(fields)+1),
					Version:     version,
					Source:      source,
					Line:        index + 1,
					Object:      object,
					Variant:     defaultVariant(variant, pattern),
					Name:        name,
					Type:        normalizeMarkdownText(row[1]),
					Pattern:     pattern,
					Required:    requiredMarker.MatchString(description),
					Description: description,
				})
				continue
			}
			inTable = false
		}
		header := splitMarkdownTableRow(line)
		if len(header) >= 3 &&
			(header[0] == "Field Name" || header[0] == "Field Pattern") {
			tableHeader = header
			expectSeparator = true
		}
	}

	return fields, nil
}

// WriteObjectFieldsTSV writes the stable object/field inventory.
func WriteObjectFieldsTSV(writer io.Writer, fields []ObjectField) error {
	if writer == nil {
		return errors.New("specification: writer is required")
	}
	tsv := csv.NewWriter(writer)
	tsv.Comma = '\t'
	_ = tsv.Write([]string{
		"id", "version", "source", "line", "object", "variant", "name",
		"type", "pattern", "required", "description",
	})
	for _, field := range fields {
		_ = tsv.Write([]string{
			field.ID,
			field.Version,
			field.Source,
			strconv.Itoa(field.Line),
			field.Object,
			field.Variant,
			field.Name,
			field.Type,
			strconv.FormatBool(field.Pattern),
			strconv.FormatBool(field.Required),
			field.Description,
		})
	}
	tsv.Flush()
	if err := tsv.Error(); err != nil {
		return fmt.Errorf("specification: flush object fields: %w", err)
	}
	return nil
}

func markdownHeadingWithLevel(line string) (int, string, bool) {
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level == 0 || level >= len(line) || line[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(line[level:]), true
}

func splitMarkdownTableRow(line string) []string {
	if !strings.Contains(line, "|") {
		return nil
	}
	var cells []string
	var cell strings.Builder
	inCode := false
	escaped := false
	for _, character := range line {
		if escaped {
			cell.WriteRune(character)
			escaped = false
			continue
		}
		if character == '\\' {
			escaped = true
			continue
		}
		if character == '`' {
			inCode = !inCode
			cell.WriteRune(character)
			continue
		}
		if character == '|' && !inCode {
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
			continue
		}
		cell.WriteRune(character)
	}
	if escaped {
		cell.WriteRune('\\')
	}
	cells = append(cells, strings.TrimSpace(cell.String()))
	if cells[0] == "" {
		cells = cells[1:]
	}
	if cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	return cells
}

func isTableSeparator(cells []string) bool {
	for _, cell := range cells {
		if !tableSeparator.MatchString(strings.ReplaceAll(cell, " ", "")) {
			return false
		}
	}
	return true
}

func normalizeMarkdownText(value string) string {
	value = htmlAnchor.ReplaceAllString(value, "")
	value = markdownLink.ReplaceAllString(value, "$1")
	value = strings.ReplaceAll(value, "`", "")
	value = strings.ReplaceAll(value, "**", "")
	return normalizeWhitespace(value)
}

func normalizeWhitespace(value string) string {
	return strings.TrimSpace(objectNameSpace.ReplaceAllString(value, " "))
}

func defaultVariant(variant string, pattern bool) string {
	if variant != "" {
		return variant
	}
	if pattern {
		return "Patterned Fields"
	}
	return "Fixed Fields"
}
