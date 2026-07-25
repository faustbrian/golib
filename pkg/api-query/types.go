// Package apiquery compiles declared API query capabilities into immutable,
// transport-neutral plans.
package apiquery

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
)

// ValueType identifies the wire and comparison semantics of a typed value.
type ValueType string

const (
	TypeString ValueType = "string"
	TypeInt    ValueType = "int"
	TypeUint   ValueType = "uint"
	TypeFloat  ValueType = "float"
	TypeBool   ValueType = "bool"
	TypeTime   ValueType = "time"
	TypeBytes  ValueType = "bytes"
	TypeNull   ValueType = "null"
)

// Value is a closed, typed query value. It deliberately cannot hold arbitrary
// Go values or persistence expressions.
type Value struct {
	typeName ValueType
	text     string
}

// StringValue constructs a string value.
func StringValue(value string) Value { return Value{typeName: TypeString, text: value} }

// IntValue constructs a signed integer value.
func IntValue(value int64) Value {
	return Value{typeName: TypeInt, text: strconv.FormatInt(value, 10)}
}

// UintValue constructs an unsigned integer value.
func UintValue(value uint64) Value {
	return Value{typeName: TypeUint, text: strconv.FormatUint(value, 10)}
}

// FloatValue constructs a finite floating-point value.
func FloatValue(value float64) Value {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return Value{}
	}
	return Value{typeName: TypeFloat, text: strconv.FormatFloat(value, 'g', -1, 64)}
}

// BoolValue constructs a boolean value.
func BoolValue(value bool) Value {
	return Value{typeName: TypeBool, text: strconv.FormatBool(value)}
}

// TimeValue constructs a UTC RFC 3339 timestamp value.
func TimeValue(value time.Time) Value {
	return Value{typeName: TypeTime, text: value.UTC().Format(time.RFC3339Nano)}
}

// BytesValue constructs a value from a defensive copy represented as base64.
func BytesValue(value []byte) Value {
	return Value{typeName: TypeBytes, text: base64.RawURLEncoding.EncodeToString(value)}
}

// NullValue constructs an explicit null cursor position. Null is not a valid
// declared field type, filter value, or equality constraint.
func NullValue() Value { return Value{typeName: TypeNull} }

// Type reports the value's closed type.
func (v Value) Type() ValueType { return v.typeName }

// String reports the canonical textual value. Protected values should not be
// placed in diagnostics by callers.
func (v Value) String() string { return v.text }

// MarshalJSON emits a deterministic typed representation.
func (v Value) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type  ValueType `json:"type"`
		Value string    `json:"value"`
	}{Type: v.typeName, Value: v.text})
}

// UnmarshalJSON accepts only the canonical closed representation emitted by
// MarshalJSON.
func (v *Value) UnmarshalJSON(data []byte) error {
	var encoded struct {
		Type  ValueType `json:"type"`
		Value string    `json:"value"`
	}
	if err := json.Unmarshal(data, &encoded); err != nil {
		return fmt.Errorf("decode typed value: %w", err)
	}
	if !validValueType(encoded.Type) || !canonicalValue(encoded.Type, encoded.Value) {
		return fmt.Errorf("decode typed value: invalid %s value", encoded.Type)
	}
	v.typeName = encoded.Type
	v.text = encoded.Value
	return nil
}

func canonicalValue(valueType ValueType, value string) bool {
	switch valueType {
	case TypeString:
		return true
	case TypeInt:
		parsed, err := strconv.ParseInt(value, 10, 64)
		return err == nil && strconv.FormatInt(parsed, 10) == value
	case TypeUint:
		parsed, err := strconv.ParseUint(value, 10, 64)
		return err == nil && strconv.FormatUint(parsed, 10) == value
	case TypeFloat:
		parsed, err := strconv.ParseFloat(value, 64)
		return err == nil && !math.IsInf(parsed, 0) && !math.IsNaN(parsed) &&
			strconv.FormatFloat(parsed, 'g', -1, 64) == value
	case TypeBool:
		parsed, err := strconv.ParseBool(value)
		return err == nil && strconv.FormatBool(parsed) == value
	case TypeTime:
		parsed, err := time.Parse(time.RFC3339Nano, value)
		return err == nil && parsed.UTC().Format(time.RFC3339Nano) == value
	case TypeBytes:
		parsed, err := base64.RawURLEncoding.DecodeString(value)
		return err == nil && base64.RawURLEncoding.EncodeToString(parsed) == value
	case TypeNull:
		return value == ""
	default:
		return false
	}
}

func validValueType(valueType ValueType) bool {
	return validType(valueType) || valueType == TypeNull
}

// Optional distinguishes an absent request component from an explicitly
// supplied zero or empty value.
type Optional[T any] struct {
	present bool
	value   T
}

// Present constructs an explicitly supplied request component.
func Present[T any](value T) Optional[T] { return Optional[T]{present: true, value: value} }

// IsPresent reports whether the component was supplied.
func (o Optional[T]) IsPresent() bool { return o.present }

// Value returns the supplied value and whether it was present.
func (o Optional[T]) Value() (T, bool) { return o.value, o.present }

// Operator is a declared, typed predicate operation.
type Operator string

const (
	OpEqual          Operator = "eq"
	OpNotEqual       Operator = "neq"
	OpLess           Operator = "lt"
	OpLessOrEqual    Operator = "lte"
	OpGreater        Operator = "gt"
	OpGreaterOrEqual Operator = "gte"
	OpIn             Operator = "in"
	OpNotIn          Operator = "not_in"
	OpBetween        Operator = "between"
	OpIsNull         Operator = "is_null"
	OpContains       Operator = "contains"
	OpStartsWith     Operator = "starts_with"
	OpEndsWith       Operator = "ends_with"
)

// Logic identifies a bounded logical composition.
type Logic string

const (
	LogicAnd Logic = "and"
	LogicOr  Logic = "or"
	LogicNot Logic = "not"
)

// Predicate is a typed filter leaf.
type Predicate struct {
	Name     string   `json:"name"`
	Operator Operator `json:"operator"`
	Values   []Value  `json:"values"`
}

// FilterExpr is either one Predicate or one logical group.
type FilterExpr struct {
	Predicate *Predicate   `json:"predicate,omitempty"`
	Logic     Logic        `json:"logic,omitempty"`
	Children  []FilterExpr `json:"children,omitempty"`
}

// Direction controls ordered sorting.
type Direction string

const (
	Ascending  Direction = "asc"
	Descending Direction = "desc"
)

// NullOrder declares deterministic null placement.
type NullOrder string

const (
	NullsFirst NullOrder = "first"
	NullsLast  NullOrder = "last"
)

// SortTerm is one ordered sort component.
type SortTerm struct {
	Name      string    `json:"name"`
	Direction Direction `json:"direction"`
	Nulls     NullOrder `json:"nulls,omitempty"`
}

// PageMode selects a declared pagination capability.
type PageMode string

const (
	PageNone   PageMode = "none"
	PageCursor PageMode = "cursor"
	PageOffset PageMode = "offset"
)

// CursorDirection identifies the seek direction authenticated by a cursor.
type CursorDirection string

const (
	CursorForward  CursorDirection = "forward"
	CursorBackward CursorDirection = "backward"
)

// CursorState is the authenticated, typed cursor state retained by a plan.
// Positions are sensitive application data and must not be logged.
type CursorState struct {
	Direction CursorDirection `json:"direction"`
	Positions []Value         `json:"positions"`
	Policy    string          `json:"policy,omitempty"`
}

// PageRequest describes bounded pagination without decoding a cursor.
type PageRequest struct {
	Mode   PageMode `json:"mode"`
	Size   int      `json:"size,omitempty"`
	After  string   `json:"after,omitempty"`
	Before string   `json:"before,omitempty"`
	Offset int      `json:"offset,omitempty"`
}

// Request is the transport-neutral query input.
type Request struct {
	SchemaRevision Optional[string]
	Fields         Optional[[]string]
	Includes       Optional[[]string]
	Filter         *FilterExpr
	Sorts          Optional[[]SortTerm]
	Page           PageRequest
}
