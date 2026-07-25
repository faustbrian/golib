// Package valuecheck validates reflection values before recursive encoding.
package valuecheck

import (
	"errors"
	"reflect"
)

// MaxDepth is the maximum traversed application-value depth.
const MaxDepth = 1_000

// ErrCycle identifies a cyclic application value.
var ErrCycle = errors.New("cyclic value")

// ErrDepth identifies an application value that exceeds MaxDepth.
var ErrDepth = errors.New("value nesting limit exceeded")

// Validate rejects cycles and excessive nesting before a codec recursively
// traverses an application-owned value.
func Validate(value any) error {
	return walk(reflect.ValueOf(value), make(map[identity]struct{}), 0)
}

type identity struct {
	kind    reflect.Kind
	typeOf  reflect.Type
	pointer uintptr
}

func walk(value reflect.Value, path map[identity]struct{}, depth int) error {
	if !value.IsValid() {
		return nil
	}
	if depth > MaxDepth {
		return ErrDepth
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return walk(value.Elem(), path, depth)
	}

	var current identity
	switch value.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if value.IsNil() {
			return nil
		}
		current = identity{kind: value.Kind(), typeOf: value.Type(), pointer: value.Pointer()}
		if current.pointer != 0 {
			if _, exists := path[current]; exists {
				return ErrCycle
			}
			path[current] = struct{}{}
			defer delete(path, current)
		}
	}

	switch value.Kind() {
	case reflect.Pointer:
		return walk(value.Elem(), path, depth+1)
	case reflect.Map:
		iterator := value.MapRange()
		for iterator.Next() {
			if err := walk(iterator.Key(), path, depth+1); err != nil {
				return err
			}
			if err := walk(iterator.Value(), path, depth+1); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for index := range value.Len() {
			if err := walk(value.Index(index), path, depth+1); err != nil {
				return err
			}
		}
	case reflect.Struct:
		for index := range value.NumField() {
			if !value.Type().Field(index).IsExported() {
				continue
			}
			if err := walk(value.Field(index), path, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
