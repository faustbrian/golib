// Package environment maps explicit environment snapshots into typed trees.
package environment

import (
	"bytes"
	"context"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/decode"
	"github.com/faustbrian/golib/pkg/config/internal/safeerror"
)

// CaseMode controls environment-name comparison.
type CaseMode uint8

const (
	// CaseNative follows the host operating system.
	CaseNative CaseMode = iota
	// CaseSensitive compares names byte-for-byte.
	CaseSensitive
	// CaseInsensitive compares names after Unicode uppercasing.
	CaseInsensitive
)

// Options configures typed environment mapping.
type Options struct {
	Name      string
	Priority  int
	Sensitive bool
	Optional  bool
	Prefix    string
	Separator string
	Case      CaseMode
	Limits    Limits
}

// Limits bounds work and memory consumed by an environment source. Zero
// values select conservative defaults.
type Limits struct {
	MaxVariables  int
	MaxBytes      int
	MaxValueBytes int
}

// LimitError reports a resource bound without including variable values.
type LimitError struct {
	Kind   string
	Limit  int
	Actual int
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("environment %s limit exceeded: %d > %d", e.Kind, e.Actual, e.Limit)
}

// MappingError describes an environment mapping failure without exposing the
// variable value.
type MappingError struct {
	Path     string
	Name     string
	Expected string
	Received string
	Cause    error
}

// SchemaError reports a recursive or excessively deep environment schema.
type SchemaError struct {
	Path   string
	Type   string
	Reason string
}

func (e *SchemaError) Error() string {
	return fmt.Sprintf("environment schema at %q: %s %s", e.Path, e.Reason, e.Type)
}

func (e *MappingError) Error() string {
	return fmt.Sprintf(
		"map environment variable %q to config field %q: expected %s, received %s",
		e.Name,
		e.Path,
		e.Expected,
		e.Received,
	)
}

func (e *MappingError) Unwrap() error {
	return safeerror.Redact(e.Cause, "environment conversion cause redacted")
}

// Format prevents detailed formatting from traversing the conversion cause.
func (e *MappingError) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte(e.Error()))
}

// MarshalText serializes only the redacted diagnostic message.
func (e *MappingError) MarshalText() ([]byte, error) {
	return []byte(e.Error()), nil
}

type field struct {
	path      []string
	envName   string
	typeOf    reflect.Type
	sensitive bool
}

type source struct {
	info   config.SourceInfo
	fields []field
	values func() []string
	mode   CaseMode
	limits Limits
}

// EnvironFor maps an immutable copy of values using the schema of T.
func EnvironFor[T any](values []string, options Options) (config.Source, error) {
	copyOfValues := append([]string(nil), values...)
	return newSource[T](func() []string {
		return append([]string(nil), copyOfValues...)
	}, options)
}

// ProcessFor reads the process environment on each load without mutating it.
func ProcessFor[T any](options Options) (config.Source, error) {
	return newSource[T](os.Environ, options)
}

func newSource[T any](values func() []string, options Options) (config.Source, error) {
	if strings.TrimSpace(options.Name) == "" {
		return nil, errors.New("environment source name must not be empty")
	}
	if options.Separator == "" {
		options.Separator = "__"
	}
	if options.Case > CaseInsensitive {
		return nil, errors.New("environment source case mode is invalid")
	}
	limits, err := normalizeLimits(options.Limits)
	if err != nil {
		return nil, err
	}
	if !validPrefix(options.Prefix) || !validSeparator(options.Separator) {
		return nil, errors.New("environment source prefix or separator is invalid")
	}

	typeOf := reflect.TypeFor[T]()
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if typeOf.Kind() != reflect.Struct {
		return nil, errors.New("environment destination type must be a struct")
	}

	fields, err := collectFields(
		typeOf,
		nil,
		nil,
		options,
		1,
		make(map[reflect.Type]bool),
	)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]string, len(fields))
	for _, field := range fields {
		normalized := normalize(field.envName, options.Case)
		if previous, exists := seen[normalized]; exists {
			return nil, fmt.Errorf(
				"environment schema collision: %q and %q map to %q",
				previous,
				strings.Join(field.path, "."),
				field.envName,
			)
		}
		seen[normalized] = strings.Join(field.path, ".")
	}

	return &source{
		info: config.SourceInfo{
			Name:      options.Name,
			Priority:  options.Priority,
			Sensitive: options.Sensitive,
			Optional:  options.Optional,
		},
		fields: fields,
		values: values,
		mode:   options.Case,
		limits: limits,
	}, nil
}

func (s *source) Info() config.SourceInfo { return s.info }

func (s *source) Load(ctx context.Context) (config.Document, error) {
	if err := ctx.Err(); err != nil {
		return config.Document{}, err
	}

	entries := s.values()
	if len(entries) > s.limits.MaxVariables {
		return config.Document{}, &LimitError{
			Kind: "variables", Limit: s.limits.MaxVariables, Actual: len(entries),
		}
	}
	values := make(map[string]variable)
	wanted := make(map[string]struct{}, len(s.fields))
	for _, field := range s.fields {
		wanted[normalize(field.envName, s.mode)] = struct{}{}
	}
	totalBytes := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return config.Document{}, err
		}
		totalBytes += len(entry)
		if totalBytes > s.limits.MaxBytes {
			return config.Document{}, &LimitError{
				Kind: "bytes", Limit: s.limits.MaxBytes, Actual: totalBytes,
			}
		}
		name, value, found := strings.Cut(entry, "=")
		normalized := normalize(name, s.mode)
		if _, expected := wanted[normalized]; !expected {
			continue
		}
		if !found || !validName(name) {
			return config.Document{}, &MappingError{
				Name: name, Expected: "NAME=value", Received: "malformed variable",
			}
		}
		if len(value) > s.limits.MaxValueBytes {
			return config.Document{}, &LimitError{
				Kind: "value_bytes", Limit: s.limits.MaxValueBytes, Actual: len(value),
			}
		}
		if previous, exists := values[normalized]; exists {
			return config.Document{}, &MappingError{
				Name:     name,
				Expected: "unique environment name",
				Received: "collision with " + previous.name,
			}
		}
		values[normalized] = variable{name: name, value: value}
	}

	tree := make(map[string]any)
	origins := make(map[string]config.Origin)
	for _, field := range s.fields {
		variable, exists := values[normalize(field.envName, s.mode)]
		path := strings.Join(field.path, ".")
		if !exists {
			continue
		}

		value, err := convert(variable.value, field.typeOf)
		if err != nil {
			return config.Document{}, &MappingError{
				Path: path, Name: field.envName,
				Expected: field.typeOf.String(), Received: "string",
				Cause: safeerror.Redact(err, "environment conversion cause redacted"),
			}
		}
		setPath(tree, field.path, value)
		state := config.Present
		origins[path] = config.Origin{
			Sensitive: field.sensitive, Present: true, State: state,
		}
	}

	return config.Document{Tree: tree, Origins: origins}, nil
}

func normalizeLimits(limits Limits) (Limits, error) {
	if limits.MaxVariables < 0 || limits.MaxBytes < 0 || limits.MaxValueBytes < 0 {
		return Limits{}, errors.New("environment limits must not be negative")
	}
	if limits.MaxVariables == 0 {
		limits.MaxVariables = 4096
	}
	if limits.MaxBytes == 0 {
		limits.MaxBytes = 1 << 20
	}
	if limits.MaxValueBytes == 0 {
		limits.MaxValueBytes = 256 << 10
	}
	return limits, nil
}

type variable struct {
	name  string
	value string
}

func collectFields(
	typeOf reflect.Type,
	configPath []string,
	envPath []string,
	options Options,
	depth int,
	visiting map[reflect.Type]bool,
) ([]field, error) {
	pathText := strings.Join(configPath, ".")
	if depth > maxSchemaDepth {
		return nil, &SchemaError{Path: pathText, Type: typeOf.String(), Reason: "depth limit"}
	}
	if visiting[typeOf] {
		return nil, &SchemaError{Path: pathText, Type: typeOf.String(), Reason: "recursive type"}
	}
	visiting[typeOf] = true
	defer delete(visiting, typeOf)

	fields := make([]field, 0, typeOf.NumField())
	for index := 0; index < typeOf.NumField(); index++ {
		definition := typeOf.Field(index)
		if !definition.IsExported() {
			continue
		}

		configName, metadata := parseConfigTag(definition)
		if configName == "-" || definition.Tag.Get("env") == "-" {
			continue
		}
		path := appendCopy(configPath, configName)
		environmentName := definition.Tag.Get("env")
		parts := appendCopy(envPath, strings.ToUpper(configName))
		if environmentName != "" {
			parts = []string{environmentName}
		}

		fieldType := definition.Type
		baseType := fieldType
		for baseType.Kind() == reflect.Pointer {
			baseType = baseType.Elem()
		}
		if baseType.Kind() == reflect.Struct && !isScalar(fieldType) {
			nested, err := collectFields(
				baseType,
				path,
				parts,
				options,
				depth+1,
				visiting,
			)
			if err != nil {
				return nil, err
			}
			fields = append(fields, nested...)
			continue
		}

		name := options.Prefix + strings.Join(parts, options.Separator)
		if !validName(name) {
			return nil, fmt.Errorf("environment name %q for field %q is invalid", name, strings.Join(path, "."))
		}
		fields = append(fields, field{
			path: path, envName: name, typeOf: fieldType,
			sensitive: metadata["secret"],
		})
	}
	return fields, nil
}

const maxSchemaDepth = 64

func parseConfigTag(field reflect.StructField) (string, map[string]bool) {
	parts := strings.Split(field.Tag.Get("config"), ",")
	name := parts[0]
	if name == "" {
		name = strings.ToLower(field.Name)
	}
	metadata := make(map[string]bool, len(parts)-1)
	for _, option := range parts[1:] {
		metadata[option] = true
	}
	return name, metadata
}

func isScalar(typeOf reflect.Type) bool {
	if typeOf == reflect.TypeFor[time.Duration]() || typeOf == reflect.TypeFor[url.URL]() {
		return true
	}
	if typeOf.Implements(reflect.TypeFor[decode.ValueUnmarshaler]()) ||
		reflect.PointerTo(typeOf).Implements(reflect.TypeFor[decode.ValueUnmarshaler]()) {
		return true
	}
	if typeOf.Implements(reflect.TypeFor[interface{ UnmarshalText([]byte) error }]()) ||
		reflect.PointerTo(typeOf).Implements(reflect.TypeFor[interface{ UnmarshalText([]byte) error }]()) {
		return true
	}
	return false
}

func convert(input string, typeOf reflect.Type) (any, error) {
	if target, ok := textTarget(typeOf); ok {
		return convert(input, target)
	}
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if isScalar(typeOf) || typeOf.Kind() == reflect.String {
		return input, nil
	}

	switch typeOf.Kind() {
	case reflect.Bool:
		return strconv.ParseBool(input)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, err := strconv.ParseInt(input, 10, typeOf.Bits())
		if err != nil {
			return nil, err
		}
		return value, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, err := strconv.ParseUint(input, 10, typeOf.Bits())
		if err != nil {
			return nil, err
		}
		return value, nil
	case reflect.Float32, reflect.Float64:
		return strconv.ParseFloat(input, typeOf.Bits())
	case reflect.Slice, reflect.Map:
		decoder := stdjson.NewDecoder(bytes.NewBufferString(input))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			if err == nil {
				return nil, errors.New("multiple environment collection values")
			}
			return nil, err
		}
		return convertJSON(value, typeOf)
	default:
		return nil, fmt.Errorf("unsupported environment destination type")
	}
}

func textTarget(typeOf reflect.Type) (reflect.Type, bool) {
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	candidate := reflect.New(typeOf).Interface()
	targeter, ok := candidate.(decode.TextTargeter)
	if !ok {
		return nil, false
	}
	return targeter.ConfigTextTarget(), true
}

func convertJSON(value any, typeOf reflect.Type) (any, error) {
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if isScalar(typeOf) {
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("expected JSON string")
		}
		return text, nil
	}
	switch typeOf.Kind() {
	case reflect.String:
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("expected JSON string")
		}
		return text, nil
	case reflect.Bool:
		boolean, ok := value.(bool)
		if !ok {
			return nil, errors.New("expected JSON boolean")
		}
		return boolean, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		number, ok := value.(stdjson.Number)
		if !ok {
			return nil, errors.New("expected JSON integer")
		}
		return strconv.ParseInt(number.String(), 10, typeOf.Bits())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		number, ok := value.(stdjson.Number)
		if !ok {
			return nil, errors.New("expected JSON unsigned integer")
		}
		return strconv.ParseUint(number.String(), 10, typeOf.Bits())
	case reflect.Float32, reflect.Float64:
		number, ok := value.(stdjson.Number)
		if !ok {
			return nil, errors.New("expected JSON number")
		}
		return strconv.ParseFloat(number.String(), typeOf.Bits())
	case reflect.Slice:
		items, ok := value.([]any)
		if !ok {
			return nil, errors.New("expected JSON array")
		}
		result := make([]any, len(items))
		for index, item := range items {
			converted, err := convertJSON(item, typeOf.Elem())
			if err != nil {
				return nil, err
			}
			result[index] = converted
		}
		return result, nil
	case reflect.Map:
		if typeOf.Key().Kind() != reflect.String {
			return nil, errors.New("environment maps require string keys")
		}
		object, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("expected JSON object")
		}
		result := make(map[string]any, len(object))
		for key, item := range object {
			converted, err := convertJSON(item, typeOf.Elem())
			if err != nil {
				return nil, err
			}
			result[key] = converted
		}
		return result, nil
	case reflect.Interface:
		if number, ok := value.(stdjson.Number); ok {
			if strings.ContainsAny(number.String(), ".eE") {
				return strconv.ParseFloat(number.String(), 64)
			}
			return strconv.ParseInt(number.String(), 10, 64)
		}
		return value, nil
	default:
		return nil, errors.New("unsupported JSON collection element type")
	}
}

func setPath(tree map[string]any, path []string, value any) {
	current := tree
	for _, segment := range path[:len(path)-1] {
		nested, exists := current[segment].(map[string]any)
		if !exists {
			nested = make(map[string]any)
			current[segment] = nested
		}
		current = nested
	}
	current[path[len(path)-1]] = value
}

func normalize(name string, mode CaseMode) string {
	if mode == CaseInsensitive || (mode == CaseNative && runtime.GOOS == "windows") {
		return strings.ToUpper(name)
	}
	return name
}

func validName(name string) bool {
	for index, character := range name {
		if character == '_' || unicode.IsLetter(character) || (index > 0 && unicode.IsDigit(character)) {
			continue
		}
		return false
	}
	return name != ""
}

func validPrefix(prefix string) bool {
	return prefix == "" || validName(strings.TrimSuffix(prefix, "_"))
}

func validSeparator(separator string) bool {
	return separator != "" && strings.Trim(separator, "_") == ""
}

func appendCopy(values []string, value string) []string {
	result := make([]string, len(values), len(values)+1)
	copy(result, values)
	return append(result, value)
}
