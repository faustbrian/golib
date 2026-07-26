// Package expression parses and evaluates the JSON Template Language used by
// OpenRPC runtime expressions. Evaluation only selects JSON values; it does
// not execute code or invoke user callbacks.
package expression

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

var (
	// ErrInvalidExpression reports malformed JSON Template Language syntax.
	ErrInvalidExpression = errors.New("expression: invalid runtime expression")
	// ErrExpressionLimit reports a parsing or evaluation resource violation.
	ErrExpressionLimit = errors.New("expression: resource limit exceeded")
	// ErrExpressionPolicy reports non-positive expression limits.
	ErrExpressionPolicy = errors.New("expression: invalid resource policy")
	// ErrInvalidContext reports an invalid zero JSON value in a binding.
	ErrInvalidContext = errors.New("expression: invalid evaluation context")
	// ErrMissingValue reports an identifier, property, or array item that is
	// absent from the evaluation context.
	ErrMissingValue = errors.New("expression: referenced value is missing")
	// ErrUnsupportedValue reports an object or array embedded into surrounding
	// text. A whole-expression result can preserve either type directly.
	ErrUnsupportedValue = errors.New("expression: value cannot be interpolated")
)

// Policy bounds expression parsing and the resulting JSON value.
type Policy struct {
	MaxLength      int
	MaxExpressions int
	MaxSegments    int
	MaxIndexDigits int
	MaxNodes       int
	MaxOutputBytes int
}

// DefaultPolicy returns finite bounds suitable for server and link values.
func DefaultPolicy() Policy {
	return Policy{
		MaxLength:      16 << 10,
		MaxExpressions: 256,
		MaxSegments:    256,
		MaxIndexDigits: 19,
		MaxNodes:       4_096,
		MaxOutputBytes: 1 << 20,
	}
}

// Context is an immutable set of top-level JSON bindings.
type Context struct {
	bindings map[string]jsonvalue.Value
}

// NewContext constructs an ownership-safe evaluation context.
func NewContext(bindings map[string]jsonvalue.Value) (Context, error) {
	owned := make(map[string]jsonvalue.Value, len(bindings))
	for name, value := range bindings {
		if len(value.Bytes()) == 0 {
			return Context{}, ErrInvalidContext
		}
		owned[name] = value
	}
	return Context{bindings: owned}, nil
}

// Template is one immutable parsed runtime expression.
type Template struct {
	source string
	parts  []part
	policy Policy
}

type part struct {
	literal string
	access  []segment
}

type segment struct {
	name  string
	index int
	array bool
}

// Parse validates one JSON Template Language template.
func Parse(source string, policy Policy) (Template, error) {
	if !validPolicy(policy) {
		return Template{}, ErrExpressionPolicy
	}
	if source == "" || !utf8.ValidString(source) {
		return Template{}, ErrInvalidExpression
	}
	if len(source) > policy.MaxLength {
		return Template{}, ErrExpressionLimit
	}

	parts := make([]part, 0, 4)
	expressions := 0
	for offset := 0; offset != len(source); {
		if strings.HasPrefix(source[offset:], "${") {
			end := strings.IndexByte(source[offset+2:], '}')
			if end == strings.IndexByte("", '}') {
				return Template{}, ErrInvalidExpression
			}
			end += offset + 2
			access, err := parseAccess(source[offset+2:end], policy)
			if err != nil {
				return Template{}, err
			}
			expressions++
			if expressions > policy.MaxExpressions {
				return Template{}, ErrExpressionLimit
			}
			parts = append(parts, part{access: access})
			offset = end + 1
			continue
		}

		literal, _, _ := strings.Cut(source[offset:], "${")
		for index := range literal {
			if !literalCharacter(literal[index]) {
				return Template{}, ErrInvalidExpression
			}
		}
		offset += len(literal)
		parts = append(parts, part{literal: literal})
	}
	return Template{source: source, parts: parts, policy: policy}, nil
}

// String returns the original expression spelling.
func (template Template) String() string { return template.source }

// Evaluate deterministically evaluates template against context. A template
// consisting of exactly one expression preserves the referenced JSON type.
func (template Template) Evaluate(context Context) (jsonvalue.Value, error) {
	if len(template.parts) == 0 || !validPolicy(template.policy) {
		return jsonvalue.Value{}, ErrInvalidExpression
	}
	remainingNodes := template.policy.MaxNodes
	singleAccess := false
	switch len(template.parts) {
	case 1:
		singleAccess = template.parts[0].access != nil
	}
	if singleAccess {
		value, err := evaluateAccess(context, template.parts[0].access, &remainingNodes)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		if len(value.Bytes()) > template.policy.MaxOutputBytes {
			return jsonvalue.Value{}, ErrExpressionLimit
		}
		return value, nil
	}

	var output strings.Builder
	for _, part := range template.parts {
		if part.access == nil {
			if err := appendBounded(&output, part.literal, template.policy.MaxOutputBytes); err != nil {
				return jsonvalue.Value{}, err
			}
			continue
		}
		value, err := evaluateAccess(context, part.access, &remainingNodes)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		text, err := interpolationText(value)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		if err := appendBounded(&output, text, template.policy.MaxOutputBytes); err != nil {
			return jsonvalue.Value{}, err
		}
	}
	encoded, _ := json.Marshal(output.String())
	if len(encoded) > template.policy.MaxOutputBytes {
		return jsonvalue.Value{}, ErrExpressionLimit
	}
	parsePolicy := jsonvalue.DefaultPolicy()
	parsePolicy.MaxBytes = template.policy.MaxOutputBytes
	return jsonvalue.Parse(encoded, parsePolicy)
}

func parseAccess(source string, policy Policy) ([]segment, error) {
	if source == "" {
		return nil, ErrInvalidExpression
	}
	segments := make([]segment, 0, 4)
	offset := 0
	name, next := parseIdentifier(source, offset)
	if name == "" {
		return nil, ErrInvalidExpression
	}
	segments = append(segments, segment{name: name})
	offset = next
	for offset < len(source) {
		switch source[offset] {
		case '.':
			name, next = parseIdentifier(source, offset+1)
			if name == "" {
				return nil, ErrInvalidExpression
			}
			segments = append(segments, segment{name: name})
			offset = next
		case '[':
			end := strings.IndexByte(source[offset+1:], ']')
			if end == strings.IndexByte("", ']') {
				return nil, ErrInvalidExpression
			}
			end += offset + 1
			digits := source[offset+1 : end]
			if digits == "" || len(digits) > policy.MaxIndexDigits ||
				(len(digits) > 1 && digits[0] == '0') {
				return nil, ErrInvalidExpression
			}
			for _, digit := range digits {
				if digit < '0' || digit > '9' {
					return nil, ErrInvalidExpression
				}
			}
			index, err := strconv.Atoi(digits)
			if err != nil {
				return nil, ErrExpressionLimit
			}
			segments = append(segments, segment{array: true, index: index})
			offset = end + 1
		default:
			return nil, ErrInvalidExpression
		}
		if len(segments) > policy.MaxSegments {
			return nil, ErrExpressionLimit
		}
	}
	return segments, nil
}

func parseIdentifier(source string, offset int) (string, int) {
	start := offset
	for offset < len(source) && identifierCharacter(source[offset]) {
		offset++
	}
	return source[start:offset], offset
}

func evaluateAccess(context Context, access []segment, remainingNodes *int) (jsonvalue.Value, error) {
	value, exists := context.bindings[access[0].name]
	if !exists {
		return jsonvalue.Value{}, ErrMissingValue
	}
	for _, segment := range access[1:] {
		raw := value.Bytes()
		if err := consumeNodes(raw, remainingNodes); err != nil {
			return jsonvalue.Value{}, err
		}
		var selected json.RawMessage
		if segment.array {
			var array []json.RawMessage
			if err := json.Unmarshal(raw, &array); err != nil || segment.index >= len(array) {
				return jsonvalue.Value{}, ErrMissingValue
			}
			selected = array[segment.index]
		} else {
			var object map[string]json.RawMessage
			if err := json.Unmarshal(raw, &object); err != nil {
				return jsonvalue.Value{}, ErrMissingValue
			}
			var ok bool
			selected, ok = object[segment.name]
			if !ok {
				return jsonvalue.Value{}, ErrMissingValue
			}
		}
		policy := jsonvalue.DefaultPolicy()
		policy.MaxBytes = len(selected)
		// selected came from a successfully decoded JSON value.
		parsed, _ := jsonvalue.Parse(selected, policy)
		value = parsed
	}
	return value, nil
}

func consumeNodes(input []byte, remaining *int) error {
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	for {
		_, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return ErrInvalidContext
		}
		if *remaining <= 0 {
			return ErrExpressionLimit
		}
		(*remaining)--
	}
}

func interpolationText(value jsonvalue.Value) (string, error) {
	raw := bytes.TrimSpace(value.Bytes())
	if len(raw) == 0 {
		return "", ErrInvalidContext
	}
	if raw[0] == '{' || raw[0] == '[' {
		return "", ErrUnsupportedValue
	}
	if raw[0] == '"' {
		var text string
		// Value guarantees that a quote-prefixed value is a JSON string.
		_ = json.Unmarshal(raw, &text)
		return text, nil
	}
	return string(raw), nil
}

func appendBounded(output *strings.Builder, value string, maximum int) error {
	if output.Len() > maximum-len(value) {
		return ErrExpressionLimit
	}
	output.WriteString(value)
	return nil
}

func validPolicy(policy Policy) bool {
	return policy.MaxLength > 0 &&
		policy.MaxExpressions > 0 &&
		policy.MaxSegments > 0 &&
		policy.MaxIndexDigits > 0 &&
		policy.MaxNodes > 0 &&
		policy.MaxOutputBytes > 0
}

func identifierCharacter(character byte) bool {
	return character == '_' ||
		character >= 'A' && character <= 'Z' ||
		character >= 'a' && character <= 'z'
}

func literalCharacter(character byte) bool {
	return character >= 'A' && character <= 'Z' ||
		character >= 'a' && character <= 'z' ||
		character >= '0' && character <= '9' ||
		strings.ContainsRune("-_~.:/?#[]@!&'()*+,;=", rune(character))
}
