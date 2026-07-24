// Package defaults builds typed lowest-precedence sources from struct tags.
package defaults

import (
	"bytes"
	"context"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/decode"
	"github.com/faustbrian/golib/pkg/config/internal/safeerror"
)

type source struct {
	info    config.SourceInfo
	tree    map[string]any
	origins map[string]config.Origin
}

// Error describes an invalid default without exposing its configured text.
type Error struct {
	Path     string
	Expected string
	Cause    error
}

// SchemaError reports a recursive or excessively deep default schema.
type SchemaError struct {
	Path   string
	Type   string
	Reason string
}

func (e *SchemaError) Error() string {
	return fmt.Sprintf("default schema at %q: %s %s", e.Path, e.Reason, e.Type)
}

func (e *Error) Error() string {
	return fmt.Sprintf("decode default for config field %q as %s", e.Path, e.Expected)
}

func (e *Error) Unwrap() error {
	return safeerror.Redact(e.Cause, "default conversion cause redacted")
}

// Format prevents detailed formatting from traversing the conversion cause.
func (e *Error) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte(e.Error()))
}

// MarshalText serializes only the redacted diagnostic message.
func (e *Error) MarshalText() ([]byte, error) {
	return []byte(e.Error()), nil
}

// For builds a default source from default tags on T.
func For[T any](name string) (config.Source, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("default source name must not be empty")
	}
	typeOf := reflect.TypeFor[T]()
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	if typeOf.Kind() != reflect.Struct {
		return nil, errors.New("default destination type must be a struct")
	}

	tree := make(map[string]any)
	origins := make(map[string]config.Origin)
	if err := collect(typeOf, tree, origins, "", 1, make(map[reflect.Type]bool)); err != nil {
		return nil, err
	}
	return &source{
		info: config.SourceInfo{Name: name, Priority: config.PriorityDefaults},
		tree: tree, origins: origins,
	}, nil
}

func (s *source) Info() config.SourceInfo { return s.info }

func (s *source) Load(ctx context.Context) (config.Document, error) {
	if err := ctx.Err(); err != nil {
		return config.Document{}, err
	}
	return config.Document{Tree: cloneMap(s.tree), Origins: cloneOrigins(s.origins)}, nil
}

func collect(
	typeOf reflect.Type,
	tree map[string]any,
	origins map[string]config.Origin,
	parent string,
	depth int,
	visiting map[reflect.Type]bool,
) error {
	if depth > maxSchemaDepth {
		return &SchemaError{Path: parent, Type: typeOf.String(), Reason: "depth limit"}
	}
	if visiting[typeOf] {
		return &SchemaError{Path: parent, Type: typeOf.String(), Reason: "recursive type"}
	}
	visiting[typeOf] = true
	defer delete(visiting, typeOf)

	for index := 0; index < typeOf.NumField(); index++ {
		definition := typeOf.Field(index)
		if !definition.IsExported() {
			continue
		}
		name, metadata := configTag(definition)
		if name == "-" {
			continue
		}
		path := name
		if parent != "" {
			path = parent + "." + name
		}

		fieldType := definition.Type
		baseType := fieldType
		for baseType.Kind() == reflect.Pointer {
			baseType = baseType.Elem()
		}
		defaultValue, hasDefault := definition.Tag.Lookup("default")
		if baseType.Kind() == reflect.Struct && !isScalar(fieldType) {
			if hasDefault {
				return defaultError(path, fieldType.String(), errors.New("struct default"))
			}
			nested := make(map[string]any)
			if err := collect(baseType, nested, origins, path, depth+1, visiting); err != nil {
				return err
			}
			if len(nested) > 0 {
				tree[name] = nested
				origins[path] = config.Origin{Present: true, State: config.Defaulted}
			}
			continue
		}
		if !hasDefault {
			continue
		}

		value, err := convert(defaultValue, fieldType)
		if err != nil {
			return defaultError(path, fieldType.String(), err)
		}
		tree[name] = value
		origins[path] = config.Origin{
			Sensitive: metadata["secret"], Present: true, State: config.Defaulted,
		}
	}
	return nil
}

const maxSchemaDepth = 64

func defaultError(path, expected string, cause error) error {
	return &Error{
		Path: path, Expected: expected,
		Cause: safeerror.Redact(cause, "default conversion cause redacted"),
	}
}

func configTag(field reflect.StructField) (string, map[string]bool) {
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
	valueType := reflect.TypeFor[decode.ValueUnmarshaler]()
	textType := reflect.TypeFor[interface{ UnmarshalText([]byte) error }]()
	return typeOf.Implements(valueType) || reflect.PointerTo(typeOf).Implements(valueType) ||
		typeOf.Implements(textType) || reflect.PointerTo(typeOf).Implements(textType)
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
		return strconv.ParseInt(input, 10, typeOf.Bits())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.ParseUint(input, 10, typeOf.Bits())
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
				return nil, errors.New("multiple default values")
			}
			return nil, err
		}
		return normalizeJSON(value)
	default:
		return nil, errors.New("unsupported default destination type")
	}
}

func textTarget(typeOf reflect.Type) (reflect.Type, bool) {
	for typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	targeter, ok := reflect.New(typeOf).Interface().(decode.TextTargeter)
	if !ok {
		return nil, false
	}
	return targeter.ConfigTextTarget(), true
}

func normalizeJSON(value any) (any, error) {
	switch value := value.(type) {
	case stdjson.Number:
		text := value.String()
		if strings.ContainsAny(text, ".eE") {
			return strconv.ParseFloat(text, 64)
		}
		return strconv.ParseInt(text, 10, 64)
	case []any:
		result := make([]any, len(value))
		for index, item := range value {
			converted, err := normalizeJSON(item)
			if err != nil {
				return nil, err
			}
			result[index] = converted
		}
		return result, nil
	case map[string]any:
		result := make(map[string]any, len(value))
		for key, item := range value {
			converted, err := normalizeJSON(item)
			if err != nil {
				return nil, err
			}
			result[key] = converted
		}
		return result, nil
	default:
		return value, nil
	}
}

func cloneMap(value map[string]any) map[string]any {
	clone := make(map[string]any, len(value))
	for key, item := range value {
		clone[key] = cloneValue(item)
	}
	return clone
}

func cloneValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneMap(value)
	case []any:
		items := make([]any, len(value))
		for index, item := range value {
			items[index] = cloneValue(item)
		}
		return items
	default:
		return value
	}
}

func cloneOrigins(value map[string]config.Origin) map[string]config.Origin {
	clone := make(map[string]config.Origin, len(value))
	for path, origin := range value {
		clone[path] = origin
	}
	return clone
}
