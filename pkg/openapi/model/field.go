// Package model provides immutable containers shared by versioned OpenAPI
// model packages.
package model

import "github.com/faustbrian/golib/pkg/openapi/jsonvalue"

type fieldState uint8

const (
	fieldAbsent fieldState = iota
	fieldNull
	fieldPresent
	fieldInvalid
)

// Field preserves the distinction between an absent field, an explicit null,
// and a concrete value including its zero or empty value.
type Field[T any] struct {
	state fieldState
	value T
	raw   jsonvalue.Value
}

// Absent constructs an absent field.
func Absent[T any]() Field[T] {
	return Field[T]{state: fieldAbsent}
}

// Null constructs an explicitly null field.
func Null[T any]() Field[T] {
	return Field[T]{state: fieldNull}
}

// Present constructs a concrete field value without applying defaults.
func Present[T any](value T) Field[T] {
	return Field[T]{state: fieldPresent, value: value}
}

// Invalid constructs a present, non-null field whose representation cannot be
// interpreted as T. The raw value remains available for lossless diagnostics.
func Invalid[T any](raw jsonvalue.Value) Field[T] {
	return Field[T]{state: fieldInvalid, raw: raw}
}

// Present reports whether the field occurred, including as explicit null.
func (field Field[T]) Present() bool {
	return field.state != fieldAbsent
}

// Null reports whether the field occurred as explicit null.
func (field Field[T]) Null() bool {
	return field.state == fieldNull
}

// Valid reports whether the representation can be interpreted as T. Absent
// and explicit-null fields are representation-valid; version validation
// decides whether null is legal at the field's location.
func (field Field[T]) Valid() bool {
	return field.state != fieldInvalid
}

// Value returns the concrete value only for a present, non-null field.
func (field Field[T]) Value() (T, bool) {
	return field.value, field.state == fieldPresent
}

// Raw returns the lossless representation of an invalid typed field.
func (field Field[T]) Raw() (jsonvalue.Value, bool) {
	return field.raw, field.state == fieldInvalid
}
