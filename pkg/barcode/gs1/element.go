package gs1

import (
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/faustbrian/golib/pkg/barcode/specification"
)

const (
	defaultMaxInput    = 4096
	defaultMaxElements = 128
)

var (
	// ErrInvalidElement reports malformed AI syntax or values.
	ErrInvalidElement = errors.New("gs1: invalid element")
	// ErrUnknownAI reports an AI absent from the pinned dictionary.
	ErrUnknownAI = errors.New("gs1: unknown application identifier")
	// ErrLimitExceeded reports parser input or element budget exhaustion.
	ErrLimitExceeded = errors.New("gs1: limit exceeded")
)

// ParseLimits bound parser work before element slices are allocated.
type ParseLimits struct {
	MaxInputBytes int
	MaxElements   int
}

// Element is one validated GS1 Application Identifier and value.
type Element struct {
	AI    string
	Value string
	Title string
}

// ElementString is an immutable ordered collection of GS1 elements.
type ElementString struct {
	elements []Element
}

// Elements returns a defensive copy in encoded order.
func (elements ElementString) Elements() []Element {
	return append([]Element(nil), elements.elements...)
}

// Raw returns barcode element-string syntax with ASCII 29 separators after
// variable-length fields when another element follows.
func (elements ElementString) Raw() string {
	loaded, _ := loadDefinitions()
	var result strings.Builder
	for index, element := range elements.elements {
		result.WriteString(element.AI)
		result.WriteString(element.Value)
		definition := loaded[element.AI]
		if !definition.predefined && index+1 < len(elements.elements) {
			result.WriteByte(0x1d)
		}
	}

	return result.String()
}

// Bracketed returns human-readable parenthesized AI syntax. Parentheses are
// not barcode data and are never returned by Raw.
func (elements ElementString) Bracketed() string {
	var result strings.Builder
	for _, element := range elements.elements {
		result.WriteByte('(')
		result.WriteString(element.AI)
		result.WriteByte(')')
		result.WriteString(element.Value)
	}

	return result.String()
}

type component struct {
	kind     byte
	min      int
	max      int
	optional bool
	checksum bool
	linters  []string
}

type definition struct {
	ai         string
	title      string
	predefined bool
	min        int
	max        int
	components []component
	required   [][][]string
	excluded   []string
}

var (
	definitionsOnce   sync.Once
	definitions       map[string]definition
	definitionsErr    error
	definitionsSource = specification.GS1SyntaxDictionary
)

// DefinitionCount reports the number of concrete AIs loaded from the pinned
// dictionary after numeric ranges are expanded.
func DefinitionCount() int {
	loaded, err := loadDefinitions()
	if err != nil {
		return 0
	}

	return len(loaded)
}

// ParseBracketed parses human-readable parenthesized AI syntax.
func ParseBracketed(input string, limits ParseLimits) (ElementString, error) {
	limits, err := normalizeParseLimits(input, limits)
	if err != nil {
		return ElementString{}, err
	}
	loaded, err := loadDefinitions()
	if err != nil {
		return ElementString{}, err
	}
	elements := make([]Element, 0, min(8, limits.MaxElements))
	for cursor := 0; cursor < len(input); {
		if input[cursor] != '(' {
			return ElementString{}, ErrInvalidElement
		}
		closeOffset := strings.IndexByte(input[cursor+1:], ')')
		if closeOffset < 0 {
			return ElementString{}, ErrInvalidElement
		}
		closeIndex := cursor + 1 + closeOffset
		ai := input[cursor+1 : closeIndex]
		definition, ok := loaded[ai]
		if !ok {
			return ElementString{}, ErrUnknownAI
		}
		valueStart := closeIndex + 1
		nextOffset := strings.IndexByte(input[valueStart:], '(')
		valueEnd := len(input)
		if nextOffset >= 0 {
			valueEnd = valueStart + nextOffset
		}
		value := input[valueStart:valueEnd]
		if err := validateValue(definition, value); err != nil {
			return ElementString{}, err
		}
		if len(elements) == limits.MaxElements {
			return ElementString{}, ErrLimitExceeded
		}
		elements = append(elements, Element{AI: ai, Value: value, Title: definition.title})
		cursor = valueEnd
	}
	if err := validateAssociations(elements, loaded); err != nil {
		return ElementString{}, err
	}
	return ElementString{elements: elements}, nil
}

// ParseRaw parses scanner element-string data using ASCII 29 as the FNC1
// separator after non-predefined-length fields.
func ParseRaw(input string, limits ParseLimits) (ElementString, error) {
	limits, err := normalizeParseLimits(input, limits)
	if err != nil {
		return ElementString{}, err
	}
	loaded, err := loadDefinitions()
	if err != nil {
		return ElementString{}, err
	}
	elements := make([]Element, 0, min(8, limits.MaxElements))
	for cursor := 0; cursor < len(input); {
		if input[cursor] == 0x1d {
			return ElementString{}, ErrInvalidElement
		}
		definition, aiLength, ok := matchDefinition(loaded, input[cursor:])
		if !ok {
			return ElementString{}, ErrUnknownAI
		}
		cursor += aiLength
		var valueEnd int
		if definition.predefined {
			if len(input)-cursor < definition.max {
				return ElementString{}, ErrInvalidElement
			}
			valueEnd = cursor + definition.max
		} else {
			separator := strings.IndexByte(input[cursor:], 0x1d)
			valueEnd = len(input)
			if separator >= 0 {
				valueEnd = cursor + separator
			}
		}
		value := input[cursor:valueEnd]
		if err := validateValue(definition, value); err != nil {
			return ElementString{}, err
		}
		if len(elements) == limits.MaxElements {
			return ElementString{}, ErrLimitExceeded
		}
		elements = append(elements, Element{
			AI: definition.ai, Value: value, Title: definition.title,
		})
		cursor = valueEnd
		if cursor < len(input) && input[cursor] == 0x1d {
			cursor++
			if cursor == len(input) {
				return ElementString{}, ErrInvalidElement
			}
		}
	}
	if err := validateAssociations(elements, loaded); err != nil {
		return ElementString{}, err
	}
	return ElementString{elements: elements}, nil
}

func normalizeParseLimits(input string, limits ParseLimits) (ParseLimits, error) {
	if limits.MaxInputBytes < 0 || limits.MaxElements < 0 {
		return ParseLimits{}, ErrLimitExceeded
	}
	if limits.MaxInputBytes == 0 {
		limits.MaxInputBytes = defaultMaxInput
	}
	if limits.MaxElements == 0 {
		limits.MaxElements = defaultMaxElements
	}
	if len(input) == 0 {
		return ParseLimits{}, ErrInvalidElement
	}
	if len(input) > limits.MaxInputBytes || limits.MaxElements < 1 {
		return ParseLimits{}, ErrLimitExceeded
	}

	return limits, nil
}

func loadDefinitions() (map[string]definition, error) {
	definitionsOnce.Do(func() {
		definitions, definitionsErr = parseDictionary(definitionsSource())
	})

	return definitions, definitionsErr
}

func parseDictionary(dictionary string) (map[string]definition, error) {
	result := make(map[string]definition, 256)
	for _, line := range strings.Split(dictionary, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		content, title, found := strings.Cut(line, "#")
		if !found {
			content = line
			title = ""
		}
		fields := strings.Fields(content)
		if len(fields) < 2 {
			return nil, ErrInvalidElement
		}
		aiPattern := fields[0]
		fieldIndex := 1
		flags := ""
		if isFlags(fields[fieldIndex]) {
			flags = fields[fieldIndex]
			fieldIndex++
		}
		components := make([]component, 0, 4)
		for fieldIndex < len(fields) && isComponent(fields[fieldIndex]) {
			parsed, err := parseComponent(fields[fieldIndex])
			if err != nil {
				return nil, err
			}
			components = append(components, parsed)
			fieldIndex++
		}
		if len(components) == 0 {
			return nil, ErrInvalidElement
		}
		required := make([][][]string, 0, 2)
		excluded := make([]string, 0, 2)
		for ; fieldIndex < len(fields); fieldIndex++ {
			key, value, attribute := strings.Cut(fields[fieldIndex], "=")
			if !attribute {
				continue
			}
			switch key {
			case "req":
				parsed, ok := parseRequirements(value)
				if !ok {
					return nil, ErrInvalidElement
				}
				required = append(required, parsed)
			case "ex":
				parsed := strings.Split(value, ",")
				if !validPatterns(parsed) {
					return nil, ErrInvalidElement
				}
				excluded = append(excluded, parsed...)
			}
		}
		minimum, maximum := 0, 0
		for _, parsed := range components {
			if !parsed.optional {
				minimum += parsed.min
			}
			maximum += parsed.max
		}
		for _, ai := range expandAI(aiPattern) {
			if ai == "" {
				return nil, ErrInvalidElement
			}
			result[ai] = definition{
				ai: ai, title: strings.TrimSpace(title),
				predefined: strings.Contains(flags, "*"),
				min:        minimum, max: maximum,
				components: append([]component(nil), components...),
				required:   cloneRequirements(required),
				excluded:   append([]string(nil), excluded...),
			}
		}
	}

	return result, nil
}

func parseRequirements(value string) ([][]string, bool) {
	alternatives := strings.Split(value, ",")
	parsed := make([][]string, len(alternatives))
	for index, alternative := range alternatives {
		parsed[index] = strings.Split(alternative, "+")
		if !validPatterns(parsed[index]) {
			return nil, false
		}
	}

	return parsed, len(parsed) > 0
}

func validPatterns(patterns []string) bool {
	for _, pattern := range patterns {
		if len(pattern) < 2 || len(pattern) > 4 {
			return false
		}
		for _, character := range pattern {
			if (character < '0' || character > '9') && character != 'n' {
				return false
			}
		}
	}

	return len(patterns) > 0
}

func cloneRequirements(source [][][]string) [][][]string {
	cloned := make([][][]string, len(source))
	for index, alternatives := range source {
		cloned[index] = make([][]string, len(alternatives))
		for group, patterns := range alternatives {
			cloned[index][group] = append([]string(nil), patterns...)
		}
	}

	return cloned
}

func validateAssociations(elements []Element, definitions map[string]definition) error {
	present := make(map[string]struct{}, len(elements))
	for _, element := range elements {
		present[element.AI] = struct{}{}
	}
	for _, element := range elements {
		definition := definitions[element.AI]
		for _, pattern := range definition.excluded {
			if hasPatternExcept(present, pattern, element.AI) {
				return ErrInvalidElement
			}
		}
		for _, requirement := range definition.required {
			satisfied := false
			for _, alternative := range requirement {
				matches := true
				for _, pattern := range alternative {
					matches = matches && hasPattern(present, pattern)
				}
				satisfied = satisfied || matches
			}
			if !satisfied {
				return ErrInvalidElement
			}
		}
	}

	return nil
}

func hasPatternExcept(present map[string]struct{}, pattern, excluded string) bool {
	for ai := range present {
		if ai == excluded || len(ai) != len(pattern) {
			continue
		}
		matched := true
		for index := range pattern {
			if pattern[index] != 'n' && pattern[index] != ai[index] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}

	return false
}

func hasPattern(present map[string]struct{}, pattern string) bool {
	for ai := range present {
		if len(ai) != len(pattern) {
			continue
		}
		matched := true
		for index := range pattern {
			if pattern[index] != 'n' && pattern[index] != ai[index] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}

	return false
}

func isFlags(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character != '*' && character != '?' {
			return false
		}
	}

	return true
}

func isComponent(value string) bool {
	if value == "" {
		return false
	}
	first := value[0]
	if first == '[' && len(value) > 1 {
		first = value[1]
	}

	return first == 'N' || first == 'X' || first == 'Y' || first == 'Z'
}

func parseComponent(value string) (component, error) {
	parsed := component{optional: strings.HasPrefix(value, "[")}
	typeExpression := value
	if parsed.optional {
		closing := strings.IndexByte(typeExpression, ']')
		if closing < 0 {
			return component{}, ErrInvalidElement
		}
		typeExpression = typeExpression[1:closing]
	} else if comma := strings.IndexByte(typeExpression, ','); comma >= 0 {
		typeExpression = typeExpression[:comma]
	}
	if len(typeExpression) < 2 {
		return component{}, ErrInvalidElement
	}
	parsed.kind = typeExpression[0]
	length := typeExpression[1:]
	if strings.HasPrefix(length, "..") {
		parsed.min = 1
		parsed.max, _ = strconv.Atoi(length[2:])
	} else {
		parsed.min, _ = strconv.Atoi(length)
		parsed.max = parsed.min
	}
	parsed.checksum = strings.Contains(value, ",csum")
	parts := strings.Split(value, ",")
	for _, linter := range parts[1:] {
		linter = strings.TrimSuffix(linter, "]")
		if linter != "" {
			parsed.linters = append(parsed.linters, linter)
		}
	}
	if parsed.min < 1 || parsed.max < parsed.min {
		return component{}, ErrInvalidElement
	}

	return parsed, nil
}

func expandAI(value string) []string {
	startText, endText, ranged := strings.Cut(value, "-")
	if !ranged {
		return []string{value}
	}
	start, startErr := strconv.Atoi(startText)
	end, endErr := strconv.Atoi(endText)
	if startErr != nil || endErr != nil || end < start || len(startText) != len(endText) {
		return []string{""}
	}
	result := make([]string, 0, end-start+1)
	for current := start; current <= end; current++ {
		result = append(result, fmtFixedWidth(current, len(startText)))
	}

	return result
}

func fmtFixedWidth(value, width int) string {
	text := strconv.Itoa(value)
	return strings.Repeat("0", width-len(text)) + text
}

func matchDefinition(loaded map[string]definition, input string) (definition, int, bool) {
	for length := min(4, len(input)); length >= 2; length-- {
		if definition, ok := loaded[input[:length]]; ok {
			return definition, length, true
		}
	}

	return definition{}, 0, false
}

func validateValue(definition definition, value string) error {
	if len(value) < definition.min || len(value) > definition.max {
		return ErrInvalidElement
	}
	offset := 0
	for index, component := range definition.components {
		remainingMinimum := 0
		for _, remaining := range definition.components[index+1:] {
			if !remaining.optional {
				remainingMinimum += remaining.min
			}
		}
		available := len(value) - offset - remainingMinimum
		length := component.max
		if component.min != component.max {
			length = min(component.max, available)
		} else if component.optional && available < component.min {
			continue
		}
		if length < component.min || offset+length > len(value) {
			return ErrInvalidElement
		}
		segment := value[offset : offset+length]
		if !validCharacters(component.kind, segment) {
			return ErrInvalidElement
		}
		if component.checksum && ValidateCheckDigit(segment) != nil {
			return ErrInvalidElement
		}
		for _, linter := range component.linters {
			if !validateLinter(linter, segment) {
				return ErrInvalidElement
			}
		}
		offset += length
	}
	if offset != len(value) {
		return ErrInvalidElement
	}

	return nil
}

func validCharacters(kind byte, value string) bool {
	for index := range value {
		character := value[index]
		switch kind {
		case 'N':
			if character < '0' || character > '9' {
				return false
			}
		case 'X':
			if character < 32 || character > 126 {
				return false
			}
		case 'Y':
			if !strings.ContainsRune("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ-./", rune(character)) {
				return false
			}
		case 'Z':
			if (character < 'A' || character > 'Z') &&
				(character < 'a' || character > 'z') &&
				(character < '0' || character > '9') && character != '_' && character != '-' {
				return false
			}
		default:
			return false
		}
	}

	return true
}
