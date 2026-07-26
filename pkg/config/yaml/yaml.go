// Package yaml provides strict, bounded YAML configuration sources.
package yaml

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"strconv"
	"strings"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/internal/safeerror"
	"github.com/faustbrian/golib/pkg/config/internal/sourceio"
	yamlv4 "go.yaml.in/yaml/v4"
)

const (
	defaultMaxBytes = 1 << 20
	defaultMaxDepth = 64
	defaultMaxKeys  = 100_000
)

// Limits bounds parser resource use. Zero values select conservative defaults.
type Limits struct {
	MaxBytes int64
	MaxDepth int
	MaxKeys  int
}

// Options configures source metadata and parser bounds.
type Options struct {
	Name      string
	Priority  int
	Sensitive bool
	Optional  bool
	Limits    Limits
}

// ParseError wraps a parser cause without including source text in diagnostics.
type ParseError struct {
	Line   int
	Column int
	Cause  error
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("decode YAML config at %d:%d: malformed document", e.Line, e.Column)
	}
	return "decode YAML config: malformed document"
}

func (e *ParseError) Unwrap() error {
	return safeerror.Redact(e.Cause, "YAML parser cause redacted")
}

// Format prevents detailed formatting from traversing the parser cause.
func (e *ParseError) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte(e.Error()))
}

// MarshalText serializes only the redacted diagnostic message.
func (e *ParseError) MarshalText() ([]byte, error) {
	return []byte(e.Error()), nil
}

// DuplicateKeyError reports an ambiguous mapping key.
type DuplicateKeyError struct {
	Path   string
	Line   int
	Column int
}

func (e *DuplicateKeyError) Error() string {
	return fmt.Sprintf("decode YAML config: duplicate key %q at %d:%d", e.Path, e.Line, e.Column)
}

type source struct {
	info   config.SourceInfo
	input  sourceio.Input
	limits Limits
}

// Bytes constructs a repeatable source from an immutable copy of data.
func Bytes(data []byte, options Options) (config.Source, error) {
	info, limits, err := validate(options)
	if err != nil {
		return nil, err
	}
	return &source{info: info, input: sourceio.Bytes(data), limits: limits}, nil
}

// FromFS constructs a repeatable source for path in filesystem.
func FromFS(filesystem fs.FS, path string, options Options) (config.Source, error) {
	info, limits, err := validate(options)
	if err != nil {
		return nil, err
	}
	input, err := sourceio.FromFS(filesystem, path)
	if err != nil {
		return nil, err
	}
	return &source{info: info, input: input, limits: limits}, nil
}

func (s *source) Info() config.SourceInfo { return s.info }

func (s *source) Load(ctx context.Context) (config.Document, error) {
	data, err := s.input.Read(ctx, s.limits.MaxBytes)
	if err != nil {
		return config.Document{}, err
	}
	if len(data) == 0 || len(bytesTrimSpace(data)) == 0 {
		return config.Document{Tree: map[string]any{}}, nil
	}

	loader, _ := yamlv4.NewLoader(
		sourceio.ContextReader(ctx, data),
		yamlv4.WithV4Defaults(),
		yamlv4.WithUniqueKeys(false),
		yamlv4.WithAllDocuments(),
	)
	var documents []yamlv4.Node
	for {
		var document yamlv4.Node
		err = loader.Load(&document)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return config.Document{}, parseError(0, 0, err)
		}
		documents = append(documents, document)
	}
	if len(documents) != 1 {
		return config.Document{}, parseError(0, 0, errors.New("expected one document"))
	}
	document := &documents[0]

	keys := 0
	value, err := convert(ctx, document.Content[0], 1, "", s.limits, &keys)
	if err != nil {
		return config.Document{}, err
	}
	tree, ok := value.(map[string]any)
	if !ok {
		return config.Document{}, parseError(
			document.Line,
			document.Column,
			errors.New("root must be a mapping"),
		)
	}
	return config.Document{Tree: tree}, nil
}

func validate(options Options) (config.SourceInfo, Limits, error) {
	if strings.TrimSpace(options.Name) == "" {
		return config.SourceInfo{}, Limits{}, errors.New("YAML source name must not be empty")
	}
	limits := options.Limits
	if limits.MaxBytes < 0 || limits.MaxDepth < 0 || limits.MaxKeys < 0 {
		return config.SourceInfo{}, Limits{}, errors.New("YAML source limits must not be negative")
	}
	if limits.MaxBytes == 0 {
		limits.MaxBytes = defaultMaxBytes
	}
	if limits.MaxDepth == 0 {
		limits.MaxDepth = defaultMaxDepth
	}
	if limits.MaxKeys == 0 {
		limits.MaxKeys = defaultMaxKeys
	}
	return config.SourceInfo{
		Name: options.Name, Priority: options.Priority,
		Sensitive: options.Sensitive, Optional: options.Optional,
	}, limits, nil
}

func convert(
	ctx context.Context,
	node *yamlv4.Node,
	depth int,
	path string,
	limits Limits,
	keys *int,
) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depth > limits.MaxDepth {
		return nil, fmt.Errorf("decode YAML config: depth exceeds %d at %q", limits.MaxDepth, path)
	}
	switch node.Kind {
	case yamlv4.AliasNode:
		return nil, parseError(node.Line, node.Column, errors.New("aliases disabled"))
	case yamlv4.MappingNode:
		if node.ShortTag() != "!!map" {
			return nil, unsupportedTag(node)
		}
		if len(node.Content)%2 != 0 {
			return nil, parseError(node.Line, node.Column, errors.New("odd mapping"))
		}
		object := make(map[string]any, len(node.Content)/2)
		for index := 0; index < len(node.Content); index += 2 {
			keyNode := node.Content[index]
			if keyNode.Kind != yamlv4.ScalarNode || keyNode.ShortTag() != "!!str" {
				return nil, parseError(keyNode.Line, keyNode.Column, errors.New("non-string key"))
			}
			if keyNode.Value == "<<" || keyNode.ShortTag() == "!!merge" {
				return nil, parseError(keyNode.Line, keyNode.Column, errors.New("merge keys disabled"))
			}
			childPath := join(path, keyNode.Value)
			if _, exists := object[keyNode.Value]; exists {
				return nil, &DuplicateKeyError{Path: childPath, Line: keyNode.Line, Column: keyNode.Column}
			}
			*keys++
			if *keys > limits.MaxKeys {
				return nil, fmt.Errorf("decode YAML config: keys exceed %d at %q", limits.MaxKeys, childPath)
			}
			value, err := convert(ctx, node.Content[index+1], depth+1, childPath, limits, keys)
			if err != nil {
				return nil, err
			}
			object[keyNode.Value] = value
		}
		return object, nil
	case yamlv4.SequenceNode:
		if node.ShortTag() != "!!seq" {
			return nil, unsupportedTag(node)
		}
		items := make([]any, len(node.Content))
		for index, child := range node.Content {
			value, err := convert(ctx, child, depth+1, fmt.Sprintf("%s[%d]", path, index), limits, keys)
			if err != nil {
				return nil, err
			}
			items[index] = value
		}
		return items, nil
	case yamlv4.ScalarNode:
		return scalar(node)
	default:
		return nil, parseError(node.Line, node.Column, errors.New("unsupported node"))
	}
}

func scalar(node *yamlv4.Node) (any, error) {
	switch node.ShortTag() {
	case "!!str":
		return node.Value, nil
	case "!!null":
		return nil, nil
	case "!!bool":
		return strconv.ParseBool(strings.ToLower(node.Value))
	case "!!int":
		value, err := parseInteger(node.Value)
		if err != nil {
			return nil, parseError(node.Line, node.Column, err)
		}
		return value, nil
	case "!!float":
		value, err := strconv.ParseFloat(strings.ReplaceAll(node.Value, "_", ""), 64)
		if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
			return nil, parseError(node.Line, node.Column, errors.New("invalid float"))
		}
		return value, nil
	case "!!timestamp":
		return node.Value, nil
	default:
		return nil, unsupportedTag(node)
	}
}

func parseInteger(input string) (any, error) {
	text := strings.ReplaceAll(input, "_", "")
	base := 10
	unsignedText := text
	if strings.HasPrefix(unsignedText, "+") || strings.HasPrefix(unsignedText, "-") {
		unsignedText = unsignedText[1:]
	}
	if strings.HasPrefix(unsignedText, "0x") || strings.HasPrefix(unsignedText, "0X") ||
		strings.HasPrefix(unsignedText, "0o") || strings.HasPrefix(unsignedText, "0O") ||
		strings.HasPrefix(unsignedText, "0b") || strings.HasPrefix(unsignedText, "0B") {
		base = 0
	}
	value, err := strconv.ParseInt(text, base, 64)
	if err == nil {
		return value, nil
	}
	unsigned, unsignedErr := strconv.ParseUint(strings.TrimPrefix(text, "+"), base, 64)
	if unsignedErr == nil {
		return unsigned, nil
	}
	return nil, errors.New("integer out of range")
}

func unsupportedTag(node *yamlv4.Node) error {
	return parseError(node.Line, node.Column, errors.New("unsupported YAML tag"))
}

func parseError(line, column int, cause error) error {
	return &ParseError{
		Line: line, Column: column,
		Cause: safeerror.Redact(cause, "YAML parser cause redacted"),
	}
}

func join(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

func bytesTrimSpace(value []byte) []byte {
	return []byte(strings.TrimSpace(string(value)))
}
