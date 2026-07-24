package specification

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	normativePattern = regexp.MustCompile(`\b(MUST NOT|SHALL NOT|SHOULD NOT|NOT RECOMMENDED|REQUIRED|RECOMMENDED|MUST|SHALL|SHOULD|MAY|OPTIONAL)\b`)
)

type normativeStatement struct {
	Source string
	Line   int
	Level  string
	Text   string
}

type fieldRow struct {
	Object        string
	Field         string
	Shape         string
	Required      bool
	Nullable      bool
	Default       string
	Extensible    bool
	UnknownFields string
}

type descriptionBlock struct {
	Source string
	Text   string
}

// GenerateMatrices inventories normative prose and every schema-declared
// object field. Empty implementation and evidence columns are deliberate: a
// generated inventory must not claim conformance before executable proof is
// linked during review.
func GenerateMatrices(specification string, schemaJSON []byte) ([]byte, []byte, error) {
	var schema any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		return nil, nil, fmt.Errorf("decode OpenRPC schema: %w", err)
	}

	statements := normativeSentences("spec-template.md", specification)
	for _, description := range schemaDescriptions(schema) {
		found := normativeSentences(description.Source, description.Text)
		for index := range found {
			found[index].Line = 0
		}
		statements = append(statements, found...)
	}

	return renderNormativeMatrix(statements), renderFieldMatrix(fieldRows(schema)), nil
}

// ApplyNormativeEvidence joins reviewed executable evidence onto every
// generated normative inventory row.
func ApplyNormativeEvidence(inventory []byte, review []byte) ([]byte, error) {
	reviewLines := strings.Split(strings.TrimSpace(string(review)), "\n")
	if len(reviewLines) < 2 || reviewLines[0] != "id\timplementation\tevidence\tstatus\tnotes" {
		return nil, errors.New("invalid normative evidence header")
	}
	reviews := make(map[string][]string)
	for _, line := range reviewLines[1:] {
		columns := strings.Split(line, "\t")
		if len(columns) != 5 || columns[0] == "" ||
			(columns[3] != "complete" && columns[3] != "not-applicable") {
			return nil, errors.New("invalid normative evidence row")
		}
		if _, duplicate := reviews[columns[0]]; duplicate {
			return nil, errors.New("duplicate normative evidence row")
		}
		reviews[columns[0]] = columns
	}

	inventoryLines := strings.Split(strings.TrimSpace(string(inventory)), "\n")
	if len(inventoryLines) < 2 || inventoryLines[0] != "id\tsource\tline\tlevel\trequirement\timplementation\tevidence\tstatus" {
		return nil, errors.New("invalid normative inventory header")
	}
	used := make(map[string]bool, len(reviews))
	for index, line := range inventoryLines[1:] {
		columns := strings.Split(line, "\t")
		if len(columns) != 8 {
			return nil, errors.New("invalid normative inventory row")
		}
		reviewColumns, found := reviews[columns[0]]
		if !found {
			return nil, fmt.Errorf("missing evidence for requirement %s", columns[0])
		}
		copy(columns[5:8], reviewColumns[1:4])
		inventoryLines[index+1] = strings.Join(columns, "\t")
		used[columns[0]] = true
	}
	for requirement := range reviews {
		if !used[requirement] {
			return nil, fmt.Errorf("evidence references unknown requirement %s", requirement)
		}
	}
	return []byte(strings.Join(inventoryLines, "\n") + "\n"), nil
}

// ApplyFieldEvidence joins a reviewed object-level evidence map onto every
// generated field row without allowing the generator itself to claim proof.
func ApplyFieldEvidence(inventory []byte, review []byte) ([]byte, error) {
	reviewLines := strings.Split(strings.TrimSpace(string(review)), "\n")
	if len(reviewLines) < 2 || reviewLines[0] != "object\tmodel\tvalidation\tevidence\tstatus" {
		return nil, errors.New("invalid object-field evidence header")
	}
	reviews := make(map[string][]string)
	for _, line := range reviewLines[1:] {
		columns := strings.Split(line, "\t")
		if len(columns) != 5 || columns[0] == "" || columns[4] != "complete" {
			return nil, errors.New("invalid object-field evidence row")
		}
		if _, duplicate := reviews[columns[0]]; duplicate {
			return nil, errors.New("duplicate object-field evidence row")
		}
		reviews[columns[0]] = columns
	}

	inventoryLines := strings.Split(strings.TrimSpace(string(inventory)), "\n")
	if len(inventoryLines) < 2 || !strings.HasPrefix(inventoryLines[0], "object\tfield\t") {
		return nil, errors.New("invalid object-field inventory header")
	}
	used := make(map[string]bool, len(reviews))
	for index, line := range inventoryLines[1:] {
		columns := strings.Split(line, "\t")
		if len(columns) != 12 {
			return nil, errors.New("invalid object-field inventory row")
		}
		reviewColumns, found := reviews[columns[0]]
		if !found {
			return nil, fmt.Errorf("missing evidence for object %s", columns[0])
		}
		copy(columns[8:12], reviewColumns[1:5])
		inventoryLines[index+1] = strings.Join(columns, "\t")
		used[columns[0]] = true
	}
	for object := range reviews {
		if !used[object] {
			return nil, fmt.Errorf("evidence references unknown object %s", object)
		}
	}
	return []byte(strings.Join(inventoryLines, "\n") + "\n"), nil
}

func renderNormativeMatrix(statements []normativeStatement) []byte {
	var output bytes.Buffer
	output.WriteString("id\tsource\tline\tlevel\trequirement\timplementation\tevidence\tstatus\n")
	for index, statement := range statements {
		fmt.Fprintf(
			&output,
			"ORPC-1.4-%04d\t%s\t%d\t%s\t%s\t\t\tunimplemented\n",
			index+1,
			tsvValue(statement.Source),
			statement.Line,
			tsvValue(statement.Level),
			tsvValue(statement.Text),
		)
	}
	return output.Bytes()
}

func renderFieldMatrix(rows []fieldRow) []byte {
	var output bytes.Buffer
	output.WriteString("object\tfield\tshape\trequired\tnullable\tdefault\textensions\tunknownFields\tmodel\tvalidation\tevidence\tstatus\n")
	for _, row := range rows {
		fmt.Fprintf(
			&output,
			"%s\t%s\t%s\t%t\t%t\t%s\t%t\t%s\t\t\t\tunimplemented\n",
			tsvValue(row.Object),
			tsvValue(row.Field),
			tsvValue(row.Shape),
			row.Required,
			row.Nullable,
			tsvValue(row.Default),
			row.Extensible,
			tsvValue(row.UnknownFields),
		)
	}
	return output.Bytes()
}

func schemaDescriptions(schema any) []descriptionBlock {
	descriptions := make([]descriptionBlock, 0)
	walkDescriptions("schema.json#", schema, &descriptions)
	return descriptions
}

func walkDescriptions(path string, value any, descriptions *[]descriptionBlock) {
	switch node := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(node))
		for key := range node {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childPath := path + "/" + escapePointerToken(key)
			if key == "description" {
				if text, ok := node[key].(string); ok {
					*descriptions = append(*descriptions, descriptionBlock{
						Source: childPath,
						Text:   text,
					})
				}
				continue
			}
			walkDescriptions(childPath, node[key], descriptions)
		}
	case []any:
		for index, child := range node {
			walkDescriptions(path+"/"+jsonValue(index), child, descriptions)
		}
	}
}

func tsvValue(value string) string {
	value = strings.ReplaceAll(value, "\t", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.ReplaceAll(value, "\n", " ")
}

func normativeSentences(source string, input string) []normativeStatement {
	statements := make([]normativeStatement, 0)
	scanner := bufio.NewScanner(strings.NewReader(input))
	line := 0

	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || isNormativePreamble(text) {
			continue
		}

		for _, sentence := range splitSentences(text) {
			match := normativePattern.FindStringSubmatch(sentence)
			if len(match) == 0 {
				continue
			}
			statements = append(statements, normativeStatement{
				Source: source,
				Line:   line,
				Level:  match[1],
				Text:   sentence,
			})
		}
	}

	return statements
}

func isNormativePreamble(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "the key words") &&
		strings.Contains(lower, "interpreted as")
}

func splitSentences(text string) []string {
	parts := make([]string, 0, 1)
	start := 0
	for index := 0; index < len(text); index++ {
		if text[index] != '.' && text[index] != '!' && text[index] != '?' {
			continue
		}
		if index+1 != len(text) && text[index+1] != ' ' && text[index+1] != '\t' {
			continue
		}
		if sentence := strings.TrimSpace(text[start : index+1]); sentence != "" {
			parts = append(parts, sentence)
		}
		start = index + 1
	}
	if tail := strings.TrimSpace(text[start:]); tail != "" {
		parts = append(parts, tail)
	}
	return parts
}

func fieldRows(schema any) []fieldRow {
	rows := make([]fieldRow, 0)
	walkSchema("#", schema, &rows)
	return rows
}

func walkSchema(path string, value any, rows *[]fieldRow) {
	object, ok := value.(map[string]any)
	if !ok {
		return
	}

	properties, hasProperties := object["properties"].(map[string]any)
	if hasProperties {
		required := stringSet(object["required"])
		fields := orderedFields(properties, object["required"])
		extensible := hasExtensionPattern(object)
		unknownFields := additionalPropertiesPolicy(object)

		for _, field := range fields {
			property, _ := properties[field].(map[string]any)
			*rows = append(*rows, fieldRow{
				Object:        path,
				Field:         field,
				Shape:         schemaShape(property),
				Required:      required[field],
				Nullable:      isNullable(property),
				Default:       jsonValue(property["default"]),
				Extensible:    extensible,
				UnknownFields: unknownFields,
			})
		}
	}

	keys := make([]string, 0, len(object))
	for key := range object {
		switch key {
		case "properties":
		default:
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		walkChild(path+"/"+escapePointerToken(key), object[key], rows)
	}

	if hasProperties {
		fields := make([]string, 0, len(properties))
		for field := range properties {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			walkSchema(path+"/properties/"+escapePointerToken(field), properties[field], rows)
		}
	}
}

func walkChild(path string, value any, rows *[]fieldRow) {
	switch child := value.(type) {
	case map[string]any:
		walkSchema(path, child, rows)
	case []any:
		for index, item := range child {
			walkSchema(path+"/"+jsonValue(index), item, rows)
		}
	}
}

func orderedFields(properties map[string]any, requiredValue any) []string {
	fields := make([]string, 0, len(properties))
	seen := make(map[string]bool, len(properties))
	if required, ok := requiredValue.([]any); ok {
		for _, value := range required {
			field, stringOK := value.(string)
			if stringOK && !seen[field] {
				fields = append(fields, field)
				seen[field] = true
			}
		}
	}

	remaining := make([]string, 0)
	for field := range properties {
		if !seen[field] {
			remaining = append(remaining, field)
		}
	}
	sort.Strings(remaining)
	return append(fields, remaining...)
}

func stringSet(value any) map[string]bool {
	set := make(map[string]bool)
	values, _ := value.([]any)
	for _, value := range values {
		if text, ok := value.(string); ok {
			set[text] = true
		}
	}
	return set
}

func hasExtensionPattern(object map[string]any) bool {
	patterns, _ := object["patternProperties"].(map[string]any)
	for pattern := range patterns {
		if strings.Contains(pattern, "x-") {
			return true
		}
	}
	return false
}

func additionalPropertiesPolicy(object map[string]any) string {
	value, exists := object["additionalProperties"]
	if !exists {
		return "allow"
	}
	if allowed, ok := value.(bool); ok && !allowed {
		return "reject"
	}
	return "schema"
}

func schemaShape(schema map[string]any) string {
	if schema == nil {
		return "any"
	}
	if reference, ok := schema["$ref"].(string); ok {
		return reference
	}
	if value, exists := schema["type"]; exists {
		return jsonValue(value)
	}
	for _, keyword := range []string{"oneOf", "anyOf", "allOf"} {
		if _, exists := schema[keyword]; exists {
			return keyword
		}
	}
	return "any"
}

func isNullable(schema map[string]any) bool {
	typeValue, hasType := schema["type"]
	if !hasType {
		if _, constrained := schema["$ref"]; constrained {
			return false
		}
		for _, keyword := range []string{"allOf", "anyOf", "oneOf", "properties", "required"} {
			if _, constrained := schema[keyword]; constrained {
				return false
			}
		}
		return true
	}
	if single, ok := typeValue.(string); ok {
		return single == "null"
	}
	types, ok := typeValue.([]any)
	if !ok {
		return false
	}
	for _, value := range types {
		if value == "null" {
			return true
		}
	}
	return false
}

func jsonValue(value any) string {
	if value == nil {
		return ""
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func escapePointerToken(value string) string {
	value = strings.ReplaceAll(value, "~", "~0")
	return strings.ReplaceAll(value, "/", "~1")
}
