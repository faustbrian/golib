package ruleengine

import "time"

// Kind is the exact runtime type of a Value.
type Kind uint8

const (
	// KindMissing represents an absent path rather than a supplied value.
	KindMissing Kind = iota
	// KindNull represents an explicitly supplied null.
	KindNull
	// KindBool represents a boolean.
	KindBool
	// KindInt represents a signed 64-bit integer.
	KindInt
	// KindFloat represents a finite 64-bit floating-point number.
	KindFloat
	// KindString represents valid UTF-8 text.
	KindString
	// KindTime represents an instant with no monotonic clock reading.
	KindTime
	// KindDuration represents a time duration.
	KindDuration
	// KindList represents an ordered immutable list of values.
	KindList
)

// Value is a typed fact or operand value.
type Value struct {
	kind Kind
	data any
}

// Missing returns the absent-path sentinel.
func Missing() Value { return Value{kind: KindMissing} }

// Null returns an explicit null value.
func Null() Value { return Value{kind: KindNull} }

// Bool returns a boolean value.
func Bool(value bool) Value { return Value{kind: KindBool, data: value} }

// Int returns a signed integer value.
func Int(value int64) Value { return Value{kind: KindInt, data: value} }

// Float returns a floating-point value; contexts reject non-finite values.
func Float(value float64) Value { return Value{kind: KindFloat, data: value} }

// String returns a string value.
func String(value string) Value { return Value{kind: KindString, data: value} }

// Time returns a time value with its monotonic reading removed.
func Time(value time.Time) Value { return Value{kind: KindTime, data: value.Round(0)} }

// Duration returns a duration value.
func Duration(value time.Duration) Value { return Value{kind: KindDuration, data: value} }

// List copies its input and returns a collection value.
func List(values ...Value) Value {
	return Value{kind: KindList, data: cloneValues(values)}
}

// Kind returns the exact value kind.
func (v Value) Kind() Kind { return v.kind }

// Interface returns the scalar value or a copied []Value for a list.
func (v Value) Interface() any {
	if v.kind == KindList {
		values, _ := v.ListValue()
		return values
	}
	return v.data
}

// BoolValue returns the boolean and whether the kind matches.
func (v Value) BoolValue() (bool, bool) {
	value, ok := v.data.(bool)
	return value, ok && v.kind == KindBool
}

// IntValue returns the integer and whether the kind matches.
func (v Value) IntValue() (int64, bool) {
	value, ok := v.data.(int64)
	return value, ok && v.kind == KindInt
}

// FloatValue returns the float and whether the kind matches.
func (v Value) FloatValue() (float64, bool) {
	value, ok := v.data.(float64)
	return value, ok && v.kind == KindFloat
}

// StringValue returns the string and whether the kind matches.
func (v Value) StringValue() (string, bool) {
	value, ok := v.data.(string)
	return value, ok && v.kind == KindString
}

// TimeValue returns the time and whether the kind matches.
func (v Value) TimeValue() (time.Time, bool) {
	value, ok := v.data.(time.Time)
	return value, ok && v.kind == KindTime
}

// DurationValue returns the duration and whether the kind matches.
func (v Value) DurationValue() (time.Duration, bool) {
	value, ok := v.data.(time.Duration)
	return value, ok && v.kind == KindDuration
}

// ListValue returns a defensive copy and whether the kind matches.
func (v Value) ListValue() ([]Value, bool) {
	values, ok := v.data.([]Value)
	if !ok || v.kind != KindList {
		return nil, false
	}
	return cloneValues(values), true
}

func (v Value) clone() Value {
	if v.kind != KindList {
		return v
	}
	values, _ := v.data.([]Value)
	return List(values...)
}

func cloneValues(values []Value) []Value {
	cloned := make([]Value, len(values))
	for index := range values {
		cloned[index] = values[index].clone()
	}
	return cloned
}
