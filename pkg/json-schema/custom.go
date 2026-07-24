package jsonschema

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
)

// ValueKind identifies an immutable exact JSON value kind.
type ValueKind uint8

const (
	// NullKind identifies null or an absent Value.
	NullKind ValueKind = iota
	// BooleanKind identifies a JSON boolean.
	BooleanKind
	// NumberKind identifies an exact JSON number.
	NumberKind
	// StringKind identifies a JSON string.
	StringKind
	// ArrayKind identifies a JSON array.
	ArrayKind
	// ObjectKind identifies a JSON object.
	ObjectKind
)

// Value is a read-only view of an exact schema or instance JSON value.
type Value struct {
	value *jsonValue
}

// Kind returns the JSON value kind.
func (value Value) Kind() ValueKind {
	if value.value == nil {
		return NullKind
	}
	return ValueKind(value.value.kind)
}

// Bool returns a boolean value and whether the kind matched.
func (value Value) Bool() (bool, bool) {
	if value.value == nil || value.value.kind != kindBoolean {
		return false, false
	}
	return value.value.boolean, true
}

// Number returns the exact JSON number text and whether the kind matched.
func (value Value) Number() (string, bool) {
	if value.value == nil || value.value.kind != kindNumber {
		return "", false
	}
	return value.value.number, true
}

// String returns a string value and whether the kind matched.
func (value Value) String() (string, bool) {
	if value.value == nil || value.value.kind != kindString {
		return "", false
	}
	return value.value.text, true
}

// Len returns the array item or object member count, or zero otherwise.
func (value Value) Len() int {
	if value.value == nil {
		return 0
	}
	switch value.value.kind {
	case kindArray:
		return len(value.value.array)
	case kindObject:
		return len(value.value.object)
	default:
		return 0
	}
}

// Index returns one array item.
func (value Value) Index(index int) (Value, bool) {
	if value.value == nil || value.value.kind != kindArray ||
		index < 0 || index >= len(value.value.array) {
		return Value{}, false
	}
	return Value{value: value.value.array[index]}, true
}

// Lookup returns one object member.
func (value Value) Lookup(name string) (Value, bool) {
	if value.value == nil || value.value.kind != kindObject {
		return Value{}, false
	}
	item, exists := value.value.object[name]
	return Value{value: item}, exists
}

// Names returns a sorted copy of object member names.
func (value Value) Names() []string {
	if value.value == nil || value.value.kind != kindObject {
		return nil
	}
	return sortedStringKeys(value.value.object)
}

// KeywordCompiler compiles one keyword value into an immutable evaluator.
type KeywordCompiler interface {
	Compile(context.Context, Dialect, Value) (KeywordEvaluator, error)
}

// KeywordCompilerFunc adapts a function to KeywordCompiler.
type KeywordCompilerFunc func(context.Context, Dialect, Value) (KeywordEvaluator, error)

// Compile implements KeywordCompiler.
func (compiler KeywordCompilerFunc) Compile(
	ctx context.Context,
	dialect Dialect,
	value Value,
) (KeywordEvaluator, error) {
	return compiler(ctx, dialect, value)
}

// KeywordEvaluator evaluates a compiled custom keyword.
type KeywordEvaluator interface {
	Evaluate(context.Context, Value) (KeywordResult, error)
}

// KeywordEvaluatorFunc adapts a function to KeywordEvaluator.
type KeywordEvaluatorFunc func(context.Context, Value) (KeywordResult, error)

// Evaluate implements KeywordEvaluator.
func (evaluator KeywordEvaluatorFunc) Evaluate(
	ctx context.Context,
	value Value,
) (KeywordResult, error) {
	return evaluator(ctx, value)
}

// KeywordResult reports custom assertion validity and an optional exact JSON
// annotation. A nil Annotation means no annotation; use `json.RawMessage("null")`
// to annotate with JSON null.
type KeywordResult struct {
	Valid      bool
	Annotation json.RawMessage
}

type registeredVocabulary struct {
	keywords map[string]KeywordCompiler
}

// WithVocabulary registers one instance-owned custom vocabulary.
func WithVocabulary(
	identifier string,
	keywords map[string]KeywordCompiler,
) Option {
	return func(config *compilerConfig) error {
		parsed, err := url.Parse(identifier)
		if err != nil || !parsed.IsAbs() || parsed.Fragment != "" {
			return fmt.Errorf("%w: invalid vocabulary identifier %q", ErrInvalidSchema, identifier)
		}
		if _, exists := config.vocabularies[identifier]; exists {
			return fmt.Errorf("%w: duplicate vocabulary %q", ErrInvalidSchema, identifier)
		}
		registered := registeredVocabulary{keywords: make(map[string]KeywordCompiler, len(keywords))}
		for name, compiler := range keywords {
			if name == "" || isKnownKeyword(name) || interfaceIsNil(compiler) {
				return fmt.Errorf("%w: invalid custom keyword %q", ErrInvalidSchema, name)
			}
			for _, vocabulary := range config.vocabularies {
				if _, duplicate := vocabulary.keywords[name]; duplicate {
					return fmt.Errorf("%w: duplicate custom keyword %q", ErrInvalidSchema, name)
				}
			}
			registered.keywords[name] = compiler
		}
		config.vocabularies[identifier] = registered
		return nil
	}
}

func interfaceIsNil(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

func cloneVocabularies(
	source map[string]registeredVocabulary,
) map[string]registeredVocabulary {
	result := make(map[string]registeredVocabulary, len(source))
	for identifier, vocabulary := range source {
		keywords := make(map[string]KeywordCompiler, len(vocabulary.keywords))
		for name, compiler := range vocabulary.keywords {
			keywords[name] = compiler
		}
		result[identifier] = registeredVocabulary{keywords: keywords}
	}
	return result
}
