package validation

import "reflect"

// Presence represents whether an input was omitted, explicitly null, or set.
type Presence uint8

const (
	// MissingState means no input was supplied.
	MissingState Presence = iota
	// NullState means an explicit null was supplied.
	NullState
	// PresentState means a typed value was supplied.
	PresentState
)

// Value preserves presence separately from a typed value.
type Value[T any] struct {
	value    T
	presence Presence
}

// Missing returns an omitted typed value.
func Missing[T any]() Value[T] { return Value[T]{presence: MissingState} }

// Null returns an explicitly null typed value.
func Null[T any]() Value[T] { return Value[T]{presence: NullState} }

// Present returns a supplied typed value, including its zero value.
func Present[T any](value T) Value[T] {
	return Value[T]{value: value, presence: PresentState}
}

// Presence returns the explicit input state.
func (v Value[T]) Presence() Presence { return v.presence }

// IsPresent reports whether a typed value was supplied.
func (v Value[T]) IsPresent() bool { return v.presence == PresentState }

// Get returns the supplied value and whether it is present.
func (v Value[T]) Get() (T, bool) { return v.value, v.IsPresent() }

// IsZero reports whether a present value is its Go zero value.
func (v Value[T]) IsZero() bool {
	if !v.IsPresent() {
		return false
	}
	value := reflect.ValueOf(v.value)
	return !value.IsValid() || value.IsZero()
}

// IsEmpty reports whether a present string, collection, or map has length zero.
// Other values are empty when they are their Go zero value.
func (v Value[T]) IsEmpty() bool {
	if !v.IsPresent() {
		return false
	}
	value := reflect.ValueOf(v.value)
	if !value.IsValid() {
		return true
	}
	empty := false
	switch value.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		empty = value.Len() == 0
	case reflect.Invalid:
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16,
		reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8,
		reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Func, reflect.Interface, reflect.Pointer, reflect.Struct,
		reflect.UnsafePointer:
		empty = value.IsZero()
	}
	return empty
}
