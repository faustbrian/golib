package model

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// ErrInvalidCollection reports an invalid immutable collection construction.
var ErrInvalidCollection = errors.New("invalid model collection")

// List is an immutable ordered list.
type List[T any] struct {
	values []T
}

// NewList copies values into an immutable list.
func NewList[T any](values []T) List[T] {
	return List[T]{values: append([]T(nil), values...)}
}

// Len returns the number of list elements.
func (list List[T]) Len() int {
	return len(list.values)
}

// At returns one element when index is in range.
func (list List[T]) At(index int) (T, bool) {
	if index < 0 || index >= len(list.values) {
		var zero T
		return zero, false
	}
	return list.values[index], true
}

// Values returns a caller-owned copy in source order.
func (list List[T]) Values() []T {
	return append([]T(nil), list.values...)
}

// Entry is one named immutable map entry.
type Entry[T any] struct {
	Name  string
	Value T
}

// Map is an immutable insertion-ordered string map.
type Map[T any] struct {
	entries []Entry[T]
}

// NewMap copies entries and rejects duplicate or invalid UTF-8 names.
func NewMap[T any](entries []Entry[T]) (Map[T], error) {
	owned := append([]Entry[T](nil), entries...)
	names := make(map[string]struct{}, len(owned))
	for _, entry := range owned {
		if !utf8.ValidString(entry.Name) {
			return Map[T]{}, fmt.Errorf("%w: name is not valid UTF-8", ErrInvalidCollection)
		}
		if _, duplicate := names[entry.Name]; duplicate {
			return Map[T]{}, fmt.Errorf("%w: duplicate name", ErrInvalidCollection)
		}
		names[entry.Name] = struct{}{}
	}
	return Map[T]{entries: owned}, nil
}

// Len returns the number of map entries.
func (ordered Map[T]) Len() int {
	return len(ordered.entries)
}

// Names returns caller-owned names in source order.
func (ordered Map[T]) Names() []string {
	names := make([]string, len(ordered.entries))
	for index, entry := range ordered.entries {
		names[index] = entry.Name
	}
	return names
}

// Entries returns a caller-owned entry slice in source order.
func (ordered Map[T]) Entries() []Entry[T] {
	return append([]Entry[T](nil), ordered.entries...)
}

// Lookup returns a value by exact, case-sensitive name.
func (ordered Map[T]) Lookup(name string) (T, bool) {
	for _, entry := range ordered.entries {
		if entry.Name == name {
			return entry.Value, true
		}
	}
	var zero T
	return zero, false
}
