// Package jsonvalue provides an immutable JSON semantic value that preserves
// exact number spelling and object member order.
package jsonvalue

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"unicode/utf8"
)

// Kind identifies a JSON value kind.
type Kind uint8

const (
	// InvalidKind identifies the invalid zero Value.
	InvalidKind Kind = iota
	// NullKind identifies null.
	NullKind
	// BooleanKind identifies a boolean.
	BooleanKind
	// NumberKind identifies an exact JSON number.
	NumberKind
	// StringKind identifies a string.
	StringKind
	// ArrayKind identifies an array.
	ArrayKind
	// ObjectKind identifies an ordered object.
	ObjectKind
)

// ErrInvalidValue reports construction or serialization of an invalid JSON
// semantic value.
var ErrInvalidValue = errors.New("invalid JSON value")

// ErrMarshalLimit reports that JSON serialization exceeded a caller-selected
// byte, nesting-depth, or semantic-node limit.
var ErrMarshalLimit = errors.New("JSON value serialization limit exceeded")

// MarshalLimits bounds every independently controllable serialization growth
// axis.
type MarshalLimits struct {
	MaxBytes int
	MaxDepth int
	MaxNodes int
}

// DefaultMarshalLimits returns conservative limits for direct use of
// MarshalJSON. Callers serializing a known smaller value should lower them and
// call MarshalJSONWithLimits.
func DefaultMarshalLimits() MarshalLimits {
	return MarshalLimits{
		MaxBytes: 16 * 1024 * 1024,
		MaxDepth: 512,
		MaxNodes: 500_000,
	}
}

var numberPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// Member is one object member. Object copies the supplied slice and member
// names before retaining it.
type Member struct {
	Name  string
	Value Value
}

// Value is an immutable JSON semantic value. Its zero value is invalid.
type Value struct {
	kind    Kind
	boolean bool
	number  string
	text    string
	array   []Value
	object  []Member
}

// Raw returns this complete immutable semantic value. It allows Value to be
// used directly by packages that accept a lossless semantic source.
func (value Value) Raw() Value {
	return value
}

// Null constructs the JSON null value.
func Null() Value {
	return Value{kind: NullKind}
}

// Boolean constructs a JSON boolean.
func Boolean(value bool) Value {
	return Value{kind: BooleanKind, boolean: value}
}

// Number constructs an exact JSON number without normalizing its spelling.
func Number(value string) (Value, error) {
	if !numberPattern.MatchString(value) {
		return Value{}, fmt.Errorf("%w: malformed number", ErrInvalidValue)
	}
	return Value{kind: NumberKind, number: value}, nil
}

// String constructs a JSON string after validating its UTF-8 encoding.
func String(value string) (Value, error) {
	if !utf8.ValidString(value) {
		return Value{}, fmt.Errorf("%w: string is not valid UTF-8", ErrInvalidValue)
	}
	return Value{kind: StringKind, text: value}, nil
}

// Array constructs an array while copying the caller's slice.
func Array(values []Value) (Value, error) {
	owned := append([]Value(nil), values...)
	for _, value := range owned {
		if value.kind == InvalidKind {
			return Value{}, fmt.Errorf("%w: array contains zero value", ErrInvalidValue)
		}
	}
	return Value{kind: ArrayKind, array: owned}, nil
}

// Object constructs an ordered object while rejecting duplicate names and
// copying the caller's slice.
func Object(members []Member) (Value, error) {
	owned := append([]Member(nil), members...)
	names := make(map[string]struct{}, len(owned))
	for _, member := range owned {
		if !utf8.ValidString(member.Name) {
			return Value{}, fmt.Errorf("%w: member name is not valid UTF-8", ErrInvalidValue)
		}
		if member.Value.kind == InvalidKind {
			return Value{}, fmt.Errorf("%w: object contains zero value", ErrInvalidValue)
		}
		if _, duplicate := names[member.Name]; duplicate {
			return Value{}, fmt.Errorf("%w: duplicate object member", ErrInvalidValue)
		}
		names[member.Name] = struct{}{}
	}
	return Value{kind: ObjectKind, object: owned}, nil
}

// Kind returns the value's JSON kind.
func (value Value) Kind() Kind {
	return value.kind
}

// Bool returns a boolean value when this is BooleanKind.
func (value Value) Bool() (bool, bool) {
	return value.boolean, value.kind == BooleanKind
}

// NumberText returns the exact number spelling when this is NumberKind.
func (value Value) NumberText() (string, bool) {
	return value.number, value.kind == NumberKind
}

// Text returns the string when this is StringKind.
func (value Value) Text() (string, bool) {
	return value.text, value.kind == StringKind
}

// Length returns the number of array elements or object members without
// exposing or copying their storage.
func (value Value) Length() (int, bool) {
	switch value.kind {
	case ArrayKind:
		return len(value.array), true
	case ObjectKind:
		return len(value.object), true
	default:
		return 0, false
	}
}

// Elements returns a caller-owned array slice when this is ArrayKind.
func (value Value) Elements() ([]Value, bool) {
	if value.kind != ArrayKind {
		return nil, false
	}
	return append([]Value(nil), value.array...), true
}

// Members returns a caller-owned ordered member slice when this is ObjectKind.
func (value Value) Members() ([]Member, bool) {
	if value.kind != ObjectKind {
		return nil, false
	}
	return append([]Member(nil), value.object...), true
}

// Lookup returns an object member without exposing mutable internal storage.
func (value Value) Lookup(name string) (Value, bool) {
	if value.kind != ObjectKind {
		return Value{}, false
	}
	for _, member := range value.object {
		if member.Name == name {
			return member.Value, true
		}
	}
	return Value{}, false
}

// MarshalJSON preserves exact number spelling and object member order while
// enforcing DefaultMarshalLimits.
func (value Value) MarshalJSON() ([]byte, error) {
	return value.MarshalJSONWithLimits(DefaultMarshalLimits())
}

// MarshalJSONWithLimits preserves exact number spelling and object member
// order while enforcing positive byte, nesting-depth, and semantic-node
// limits.
func (value Value) MarshalJSONWithLimits(limits MarshalLimits) ([]byte, error) {
	if limits.MaxBytes < 1 || limits.MaxDepth < 1 || limits.MaxNodes < 1 {
		return nil, fmt.Errorf("%w: limits must be positive", ErrMarshalLimit)
	}
	budget := marshalBudget{bytes: limits.MaxBytes, nodes: limits.MaxNodes}
	if err := value.measureJSON(limits.MaxDepth, 1, &budget); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	value.appendJSON(&output)
	return output.Bytes(), nil
}

type marshalBudget struct {
	bytes int
	nodes int
}

func (value Value) measureJSON(
	maximumDepth int,
	depth int,
	budget *marshalBudget,
) error {
	if depth > maximumDepth {
		return fmt.Errorf("%w: depth", ErrMarshalLimit)
	}
	if budget.nodes < 1 {
		return fmt.Errorf("%w: nodes", ErrMarshalLimit)
	}
	budget.nodes--
	switch value.kind {
	case NullKind:
		return budget.takeBytes(4)
	case BooleanKind:
		if value.boolean {
			return budget.takeBytes(4)
		}
		return budget.takeBytes(5)
	case NumberKind:
		return budget.takeBytes(len(value.number))
	case StringKind:
		return budget.takeJSONString(value.text)
	case ArrayKind:
		if err := budget.takeContainer(len(value.array), false); err != nil {
			return err
		}
		for _, element := range value.array {
			if err := element.measureJSON(maximumDepth, depth+1, budget); err != nil {
				return err
			}
		}
		return nil
	case ObjectKind:
		if err := budget.takeContainer(len(value.object), true); err != nil {
			return err
		}
		for _, member := range value.object {
			if err := budget.takeJSONString(member.Name); err != nil {
				return err
			}
			if err := member.Value.measureJSON(maximumDepth, depth+1, budget); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%w: zero value", ErrInvalidValue)
	}
}

func (budget *marshalBudget) takeBytes(count int) error {
	if count < 0 || count > budget.bytes {
		return fmt.Errorf("%w: bytes", ErrMarshalLimit)
	}
	budget.bytes -= count
	return nil
}

func (budget *marshalBudget) takeJSONString(value string) error {
	size, ok := jsonStringSize(value, budget.bytes)
	if !ok {
		return fmt.Errorf("%w: bytes", ErrMarshalLimit)
	}
	budget.bytes -= size
	return nil
}

func (budget *marshalBudget) takeContainer(count int, object bool) error {
	if count == 0 {
		return budget.takeBytes(2)
	}
	size := count + 1
	if object {
		size += count
	}
	return budget.takeBytes(size)
}

func jsonStringSize(value string, maximum int) (int, bool) {
	if maximum < 2 {
		return 0, false
	}
	size := 2
	for _, character := range value {
		width := utf8.RuneLen(character)
		switch character {
		case '\\', '"', '\b', '\f', '\n', '\r', '\t':
			width = 2
		case '<', '>', '&', '\u2028', '\u2029':
			width = 6
		default:
			if character < 0x20 {
				width = 6
			}
		}
		if width > maximum-size {
			return 0, false
		}
		size += width
	}
	return size, true
}

func (value Value) appendJSON(output *bytes.Buffer) {
	switch value.kind {
	case NullKind:
		output.WriteString("null")
	case BooleanKind:
		if value.boolean {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case NumberKind:
		output.WriteString(value.number)
	case StringKind:
		raw, _ := json.Marshal(value.text)
		output.Write(raw)
	case ArrayKind:
		output.WriteByte('[')
		for index, element := range value.array {
			if index > 0 {
				output.WriteByte(',')
			}
			element.appendJSON(output)
		}
		output.WriteByte(']')
	case ObjectKind:
		output.WriteByte('{')
		for index, member := range value.object {
			if index > 0 {
				output.WriteByte(',')
			}
			name, _ := json.Marshal(member.Name)
			output.Write(name)
			output.WriteByte(':')
			member.Value.appendJSON(output)
		}
		output.WriteByte('}')
	}
}
