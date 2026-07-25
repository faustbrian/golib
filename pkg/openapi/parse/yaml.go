package parse

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

var (
	// ErrInvalidYAML reports malformed or ambiguous YAML input.
	ErrInvalidYAML = errors.New("invalid YAML")
	// ErrUnsupportedYAMLFeature reports syntax intentionally excluded from the
	// strict JSON-equivalent YAML mode.
	ErrUnsupportedYAMLFeature = errors.New("unsupported YAML feature")
)

// YAML parses one YAML document under strict JSON-equivalence policy. Anchors,
// aliases, merge keys, custom tags, non-string keys, and non-JSON numeric
// spellings are rejected instead of being assigned parser-specific semantics.
func YAML(ctx context.Context, reader io.Reader, limits Limits) (jsonvalue.Value, error) {
	if ctx == nil {
		return jsonvalue.Value{}, yamlError("invalid_context", 0, 0, ErrInvalidYAML, nil)
	}
	if reader == nil {
		return jsonvalue.Value{}, yamlError("nil_reader", 0, 0, ErrInvalidYAML, nil)
	}
	if err := validateLimits(limits); err != nil {
		return jsonvalue.Value{}, err
	}
	if err := ctx.Err(); err != nil {
		return jsonvalue.Value{}, err
	}

	raw, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: reader}, limits.MaxBytes+1))
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return jsonvalue.Value{}, contextErr
		}
		return jsonvalue.Value{}, yamlError("reader_failed", 0, 0, ErrInvalidYAML, err)
	}
	if int64(len(raw)) > limits.MaxBytes {
		return jsonvalue.Value{}, yamlError("max_bytes", 0, 0, ErrLimitExceeded, nil)
	}
	if err := ctx.Err(); err != nil {
		return jsonvalue.Value{}, err
	}
	if !utf8.Valid(raw) {
		return jsonvalue.Value{}, yamlError("invalid_utf8", 0, 0, ErrInvalidYAML, nil)
	}

	decoder := yaml.NewDecoder(&contextReader{
		ctx: ctx, reader: bytes.NewReader(raw),
	})
	var document yaml.Node
	decodeErr := decoder.Decode(&document)
	if contextErr := ctx.Err(); contextErr != nil {
		return jsonvalue.Value{}, contextErr
	}
	if decodeErr != nil {
		return jsonvalue.Value{}, yamlError("malformed_yaml", document.Line, document.Column, ErrInvalidYAML, decodeErr)
	}
	// A successful document decode always supplies exactly one root node.
	var trailing yaml.Node
	trailingErr := decoder.Decode(&trailing)
	if contextErr := ctx.Err(); contextErr != nil {
		return jsonvalue.Value{}, contextErr
	}
	if !errors.Is(trailingErr, io.EOF) {
		if trailingErr == nil {
			trailingErr = errors.New("multiple YAML documents")
		}
		return jsonvalue.Value{}, yamlError("multiple_documents", trailing.Line, trailing.Column, ErrInvalidYAML, trailingErr)
	}

	parser := yamlParser{ctx: ctx, limits: limits}
	return parser.value(document.Content[0], 1)
}

type yamlParser struct {
	ctx    context.Context
	limits Limits
	tokens int
	values int
}

func (parser *yamlParser) value(node *yaml.Node, depth int) (jsonvalue.Value, error) {
	if err := parser.ctx.Err(); err != nil {
		return jsonvalue.Value{}, err
	}
	if node == nil {
		return jsonvalue.Value{}, yamlError("nil_node", 0, 0, ErrInvalidYAML, nil)
	}
	parser.tokens++
	if parser.tokens > parser.limits.MaxTokens {
		return jsonvalue.Value{}, parser.nodeError("max_tokens", node, ErrLimitExceeded, nil)
	}
	parser.values++
	if parser.values > parser.limits.MaxTotalValues {
		return jsonvalue.Value{}, parser.nodeError("max_values", node, ErrLimitExceeded, nil)
	}
	if depth > parser.limits.MaxDepth {
		return jsonvalue.Value{}, parser.nodeError("max_depth", node, ErrLimitExceeded, nil)
	}
	if node.Anchor != "" || node.Alias != nil || node.Kind == yaml.AliasNode {
		return jsonvalue.Value{}, parser.nodeError("yaml_alias_or_anchor", node, ErrUnsupportedYAMLFeature, nil)
	}

	switch node.Kind {
	case yaml.ScalarNode:
		return parser.scalar(node)
	case yaml.SequenceNode:
		return parser.sequence(node, depth)
	case yaml.MappingNode:
		return parser.mapping(node, depth)
	default:
		return jsonvalue.Value{}, parser.nodeError("unsupported_yaml_node", node, ErrUnsupportedYAMLFeature, nil)
	}
}

func (parser *yamlParser) scalar(node *yaml.Node) (jsonvalue.Value, error) {
	if len(node.Value) > parser.limits.MaxScalarBytes {
		return jsonvalue.Value{}, parser.nodeError("max_scalar_bytes", node, ErrLimitExceeded, nil)
	}
	switch node.Tag {
	case "!!null":
		return jsonvalue.Null(), nil
	case "!!bool":
		switch node.Value {
		case "true":
			return jsonvalue.Boolean(true), nil
		case "false":
			return jsonvalue.Boolean(false), nil
		default:
			return jsonvalue.Value{}, parser.nodeError("non_json_boolean", node, ErrUnsupportedYAMLFeature, nil)
		}
	case "!!int", "!!float":
		if strings.Contains(node.Value, "_") || strings.EqualFold(node.Value, ".nan") ||
			strings.EqualFold(node.Value, ".inf") || strings.EqualFold(node.Value, "+.inf") ||
			strings.EqualFold(node.Value, "-.inf") {
			return jsonvalue.Value{}, parser.nodeError("non_json_number", node, ErrUnsupportedYAMLFeature, nil)
		}
		number, err := jsonvalue.Number(node.Value)
		if err != nil {
			return jsonvalue.Value{}, parser.nodeError("non_json_number", node, ErrUnsupportedYAMLFeature, err)
		}
		return number, nil
	case "!!str", "":
		value, err := jsonvalue.String(node.Value)
		if err != nil {
			return jsonvalue.Value{}, parser.nodeError("invalid_string", node, ErrInvalidYAML, err)
		}
		return value, nil
	default:
		return jsonvalue.Value{}, parser.nodeError("custom_tag", node, ErrUnsupportedYAMLFeature, nil)
	}
}

func (parser *yamlParser) sequence(node *yaml.Node, depth int) (jsonvalue.Value, error) {
	if len(node.Content) > parser.limits.MaxArrayItems {
		return jsonvalue.Value{}, parser.nodeError("max_array_items", node, ErrLimitExceeded, nil)
	}
	elements := make([]jsonvalue.Value, 0, len(node.Content))
	for _, child := range node.Content {
		value, err := parser.value(child, depth+1)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		elements = append(elements, value)
	}
	// Every element was constructed by this parser and is therefore valid.
	value, _ := jsonvalue.Array(elements)
	return value, nil
}

func (parser *yamlParser) mapping(node *yaml.Node, depth int) (jsonvalue.Value, error) {
	if len(node.Content)%2 != 0 {
		return jsonvalue.Value{}, parser.nodeError("malformed_mapping", node, ErrInvalidYAML, nil)
	}
	if len(node.Content)/2 > parser.limits.MaxObjectMembers {
		return jsonvalue.Value{}, parser.nodeError("max_object_members", node, ErrLimitExceeded, nil)
	}
	members := make([]jsonvalue.Member, 0)
	names := make(map[string]struct{})
	for index := 0; index < len(node.Content); index += 2 {
		key := node.Content[index]
		if err := parser.ctx.Err(); err != nil {
			return jsonvalue.Value{}, err
		}
		parser.tokens++
		if parser.tokens > parser.limits.MaxTokens {
			return jsonvalue.Value{}, parser.nodeError("max_tokens", key, ErrLimitExceeded, nil)
		}
		if key.Value == "<<" {
			return jsonvalue.Value{}, parser.nodeError("merge_key", key, ErrUnsupportedYAMLFeature, nil)
		}
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return jsonvalue.Value{}, parser.nodeError("non_string_key", key, ErrInvalidYAML, nil)
		}
		if key.Anchor != "" || key.Alias != nil {
			return jsonvalue.Value{}, parser.nodeError("yaml_alias_or_anchor", key, ErrUnsupportedYAMLFeature, nil)
		}
		if len(key.Value) > parser.limits.MaxScalarBytes {
			return jsonvalue.Value{}, parser.nodeError("max_scalar_bytes", key, ErrLimitExceeded, nil)
		}
		if _, duplicate := names[key.Value]; duplicate {
			return jsonvalue.Value{}, parser.nodeError("duplicate_key", key, ErrDuplicateKey, nil)
		}
		names[key.Value] = struct{}{}

		value, err := parser.value(node.Content[index+1], depth+1)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		members = append(members, jsonvalue.Member{Name: key.Value, Value: value})
	}
	// Every member is valid and duplicate names were rejected above.
	value, _ := jsonvalue.Object(members)
	return value, nil
}

func (parser *yamlParser) nodeError(code string, node *yaml.Node, kind error, cause error) error {
	return yamlError(code, node.Line, node.Column, kind, cause)
}

func yamlError(code string, line int, column int, kind error, cause error) error {
	if cause != nil {
		cause = fmt.Errorf("line %d column %d: %w", line, column, cause)
	}
	return &Error{
		Code:   code,
		Offset: int64(line),
		Kind:   kind,
		Cause:  cause,
	}
}
