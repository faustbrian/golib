package yamlwire

import (
	"bytes"
	"errors"
	"io"
	"math"
	"reflect"
	"strings"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/internal/outputlimit"
	"github.com/faustbrian/golib/pkg/wire/internal/valuecheck"
	"go.yaml.in/yaml/v4"
	"go.yaml.in/yaml/v4/plugin/limit"
)

// DefaultMaxBytes is the default maximum YAML stream size.
const DefaultMaxBytes int64 = 1 << 20

// ErrPayloadTooLarge identifies YAML streams over the configured byte limit.
var ErrPayloadTooLarge = errors.New("payload exceeds size limit")

var (
	errAliasLimit       = errors.New("YAML alias limit exceeded")
	errAliasesDisabled  = errors.New("YAML aliases are disabled")
	errDepthLimit       = errors.New("YAML nesting depth limit exceeded")
	errMergeKeyDisabled = errors.New("YAML merge keys are disabled")
)

// DecodeOptions controls YAML parsing, interoperability, and resource limits.
// Zero limits retain safe library defaults. AllowMultipleDocuments requires a
// pointer to a slice and decodes every document into that slice.
type DecodeOptions struct {
	MaxBytes               int64
	MaxDepth               int
	MaxAliases             int
	DisallowUnknownFields  bool
	AllowDuplicateKeys     bool
	AllowMultipleDocuments bool
	DisallowAliases        bool
	DisallowMergeKeys      bool
}

// EncodeOptions controls deterministic YAML serialization.
type EncodeOptions struct {
	MaxBytes              int64
	Indent                int
	DefaultSequenceIndent bool
}

// Decode parses a bounded YAML stream into target.
func Decode(payload []byte, target any, options DecodeOptions) error {
	return DecodeReader(bytes.NewReader(payload), target, options)
}

// DecodeReader reads a bounded YAML stream and decodes it into target.
func DecodeReader(reader io.Reader, target any, options DecodeOptions) error {
	if err := validateOptions(options); err != nil {
		return wrap(wire.ErrorKindValidation, "decode options", err)
	}
	if err := validateTarget(target, options.AllowMultipleDocuments); err != nil {
		return wrap(wire.ErrorKindTarget, "decode", err)
	}
	payload, err := readBounded(reader, options.MaxBytes)
	if err != nil {
		return err
	}
	if err := preflightDocuments(payload, options); err != nil {
		return err
	}

	yamlOptions := decodeYAMLOptions(options)
	if err := yaml.Load(payload, target, yamlOptions...); err != nil {
		return classifyDecodeError(err)
	}
	return nil
}

// Encode serializes value deterministically using sorted mapping keys.
func Encode(value any, options EncodeOptions) ([]byte, error) {
	return encode(value, options, yaml.NewDumper)
}

func encode(
	value any,
	options EncodeOptions,
	newDumper func(io.Writer, ...yaml.Option) (*yaml.Dumper, error),
) ([]byte, error) {
	if err := valuecheck.Validate(value); err != nil {
		return nil, wrap(wire.ErrorKindEncode, "encode", err)
	}
	if !validIndent(options.Indent) {
		return nil, wrap(wire.ErrorKindValidation, "encode options", errors.New("indent must be between 2 and 9"))
	}
	yamlOptions := []yaml.Option{yaml.WithV4Defaults(), yaml.WithLineWidth(-1)}
	if options.Indent != 0 {
		yamlOptions = append(yamlOptions, yaml.WithIndent(options.Indent))
	}
	if options.DefaultSequenceIndent {
		yamlOptions = append(yamlOptions, yaml.WithCompactSeqIndent(false))
	}
	output, err := outputlimit.New(options.MaxBytes, DefaultMaxBytes)
	if err != nil {
		return nil, wrap(wire.ErrorKindValidation, "encode options", err)
	}
	dumper, err := newDumper(output, yamlOptions...)
	if err != nil {
		return nil, wrap(wire.ErrorKindValidation, "encode options", err)
	}
	err = dumper.Dump(value)
	if err == nil {
		err = dumper.Close()
	}
	if err != nil {
		if errors.Is(err, outputlimit.ErrLimit) {
			return nil, wrap(wire.ErrorKindSizeLimit, "encode", errors.Join(ErrPayloadTooLarge, err))
		}
		kind := wire.ErrorKindEncode
		if strings.Contains(err.Error(), "cannot marshal") || strings.Contains(err.Error(), "cannot represent") || strings.Contains(err.Error(), "unsupported") {
			kind = wire.ErrorKindUnsupported
		}
		return nil, wrap(kind, "encode", err)
	}
	indent := options.Indent
	if indent == 0 {
		indent = 2
	}
	payload, err := addBlockIndentIndicators(output.Bytes(), indent, options.MaxBytes)
	if err != nil {
		return nil, wrap(wire.ErrorKindSizeLimit, "encode", errors.Join(ErrPayloadTooLarge, err))
	}
	return payload, nil
}

func addBlockIndentIndicators(payload []byte, indent int, configuredMax int64) ([]byte, error) {
	maxBytes := configuredMax
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}
	lines := bytes.SplitAfter(payload, []byte{'\n'})
	indicators := 0
	for _, line := range lines {
		if hasBlockScalar(blockScalarIndicator(line)) {
			indicators++
		}
	}
	if indicators == 0 {
		return payload, nil
	}
	if exceedsLimit(indicators, maxBytes-int64(len(payload))) {
		return nil, outputlimit.ErrLimit
	}
	result := make([]byte, 0, outputCapacity(len(payload), indicators))
	for _, line := range lines {
		index := blockScalarIndicator(line)
		if !hasBlockScalar(index) {
			result = append(result, line...)
			continue
		}
		result = append(result, line[:index+1]...)
		result = append(result, byte('0'+indent))
		result = append(result, line[index+1:]...)
	}
	return result, nil
}

func blockScalarIndicator(line []byte) int {
	line = bytes.TrimSuffix(line, []byte{'\n'})
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if len(line) == 0 {
		return -1
	}
	index := len(line) - 1
	if line[index] == '+' || line[index] == '-' {
		index--
	}
	if index <= 1 || (line[index] != '|' && line[index] != '>') ||
		line[index-1] != ' ' || (line[index-2] != ':' && line[index-2] != '-') {
		return -1
	}
	return index
}

// EncodeWriter serializes value and writes one complete YAML document.
func EncodeWriter(writer io.Writer, value any, options EncodeOptions) error {
	payload, err := Encode(value, options)
	if err != nil {
		return err
	}
	if writer == nil {
		return wrap(wire.ErrorKindValidation, "write", errors.New("writer must not be nil"))
	}
	if _, err := io.Copy(writer, bytes.NewReader(payload)); err != nil {
		return wrap(wire.ErrorKindWrite, "write", err)
	}
	return nil
}

func validateOptions(options DecodeOptions) error {
	if options.MaxBytes < 0 {
		return errors.New("max bytes must not be negative")
	}
	if options.MaxDepth < 0 {
		return errors.New("max depth must not be negative")
	}
	if options.MaxAliases < 0 {
		return errors.New("max aliases must not be negative")
	}
	return nil
}

func validateTarget(target any, multiple bool) error {
	if target == nil {
		return errors.New("target must be a non-nil pointer")
	}
	value := reflect.ValueOf(target)
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return errors.New("target must be a non-nil pointer")
	}
	if multiple && value.Elem().Kind() != reflect.Slice {
		return errors.New("multi-document target must point to a slice")
	}
	return nil
}

func readBounded(reader io.Reader, configuredMax int64) ([]byte, error) {
	if reader == nil {
		return nil, wrap(wire.ErrorKindValidation, "read", errors.New("reader must not be nil"))
	}
	maxBytes := configuredMax
	if maxBytes == 0 {
		maxBytes = DefaultMaxBytes
	}
	readLimit := maxBytes + 1
	if maxBytes == math.MaxInt64 {
		readLimit = maxBytes
	}
	payload, err := io.ReadAll(io.LimitReader(reader, readLimit))
	if err != nil {
		return nil, wrap(wire.ErrorKindParse, "read", err)
	}
	if exceedsLimit(len(payload), maxBytes) {
		return nil, wrap(wire.ErrorKindSizeLimit, "read", ErrPayloadTooLarge)
	}
	return payload, nil
}

func decodeYAMLOptions(options DecodeOptions) []yaml.Option {
	yamlOptions := baseDecodeYAMLOptions(options)
	if options.AllowMultipleDocuments {
		return append(yamlOptions, yaml.WithAllDocuments())
	}
	return append(yamlOptions, yaml.WithSingleDocument())
}

func baseDecodeYAMLOptions(options DecodeOptions) []yaml.Option {
	yamlOptions := []yaml.Option{
		yaml.WithV4Defaults(),
		yaml.WithKnownFields(options.DisallowUnknownFields),
		yaml.WithUniqueKeys(!options.AllowDuplicateKeys),
	}
	if needsLimitPlugin(options) {
		limitOptions := make([]limit.Option, 0, 2)
		if depthLimitEnabled(options.MaxDepth) {
			limitOptions = append(limitOptions, limit.DepthFunc(func(depth int, _ *limit.DepthContext) error {
				if exceedsDepth(depth, options.MaxDepth) {
					return errDepthLimit
				}
				return nil
			}))
		}
		if aliasLimitEnabled(options.DisallowAliases, options.MaxAliases) {
			limitOptions = append(limitOptions, limit.AliasFunc(func(aliasCount, _ int) error {
				if aliasesDisabled(options.DisallowAliases, aliasCount) {
					return errAliasesDisabled
				}
				if aliasesExceeded(options.MaxAliases, aliasCount) {
					return errAliasLimit
				}
				return nil
			}))
		}
		yamlOptions = append(yamlOptions, yaml.WithPlugin(limit.New(limitOptions...)))
	}
	return yamlOptions
}

func validIndent(indent int) bool {
	return indent == 0 || indent >= 2 && indent <= 9
}

func hasBlockScalar(index int) bool {
	return index >= 0
}

func exceedsLimit(length int, maximum int64) bool {
	return int64(length) > maximum
}

func outputCapacity(length, indicators int) int {
	return length + indicators
}

func needsLimitPlugin(options DecodeOptions) bool {
	return options.DisallowAliases || options.MaxAliases > 0 || options.MaxDepth > 0
}

func depthLimitEnabled(maximum int) bool {
	return maximum > 0
}

func exceedsDepth(depth, maximum int) bool {
	return depth > maximum
}

func aliasLimitEnabled(disallow bool, maximum int) bool {
	return disallow || maximum > 0
}

func aliasesDisabled(disallow bool, count int) bool {
	return disallow && count > 0
}

func aliasesExceeded(maximum, count int) bool {
	return maximum > 0 && count > maximum
}

func preflightDocuments(payload []byte, options DecodeOptions) error {
	var documents []yaml.Node
	yamlOptions := baseDecodeYAMLOptions(options)
	yamlOptions = append(yamlOptions, yaml.WithAllDocuments())
	if err := yaml.Load(payload, &documents, yamlOptions...); err != nil {
		return classifyDecodeError(err)
	}
	if !options.AllowMultipleDocuments && len(documents) != 1 {
		return wrap(wire.ErrorKindParse, "decode", errors.New("YAML stream must contain exactly one document"))
	}
	if options.DisallowMergeKeys {
		for i := range documents {
			if hasMergeKey(&documents[i]) {
				return wrap(wire.ErrorKindUnsupported, "decode", errMergeKeyDisabled)
			}
		}
	}
	return nil
}

func hasMergeKey(node *yaml.Node) bool {
	if node.Tag == "!!merge" {
		return true
	}
	for _, child := range node.Content {
		if hasMergeKey(child) {
			return true
		}
	}
	return false
}

func classifyDecodeError(err error) error {
	message := err.Error()
	switch {
	case errors.Is(err, errAliasLimit), errors.Is(err, errDepthLimit),
		strings.Contains(message, errAliasLimit.Error()), strings.Contains(message, errDepthLimit.Error()),
		strings.Contains(message, "document contains excessive aliasing"),
		strings.Contains(message, "exceeded max depth"):
		return wrap(wire.ErrorKindSizeLimit, "decode", err)
	case errors.Is(err, errAliasesDisabled), strings.Contains(message, errAliasesDisabled.Error()):
		return wrap(wire.ErrorKindUnsupported, "decode", err)
	case strings.Contains(message, "field "):
		return wrap(wire.ErrorKindValidation, "decode", err)
	default:
		return wrap(wire.ErrorKindParse, "decode", err)
	}
}

func wrap(kind wire.ErrorKind, op string, err error) error {
	return &wire.Error{Kind: kind, Format: wire.FormatYAML, Op: op, Err: err}
}
