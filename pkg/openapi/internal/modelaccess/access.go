// Package modelaccess translates immutable JSON values into typed model
// fields without discarding invalid representations.
package modelaccess

import (
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	sharedmodel "github.com/faustbrian/golib/pkg/openapi/model"
)

// Decoder converts one non-null JSON value to a typed immutable value.
type Decoder[T any] func(jsonvalue.Value) (T, bool)

// Raw returns an untyped lossless field.
func Raw(object jsonvalue.Value, name string) sharedmodel.Field[jsonvalue.Value] {
	raw, state := lookup(object, name)
	switch state {
	case stateAbsent:
		return sharedmodel.Absent[jsonvalue.Value]()
	case stateNull:
		return sharedmodel.Null[jsonvalue.Value]()
	default:
		return sharedmodel.Present(raw)
	}
}

// String returns a typed string field or an invalid field retaining raw data.
func String(object jsonvalue.Value, name string) sharedmodel.Field[string] {
	return Scalar(object, name, func(raw jsonvalue.Value) (string, bool) {
		return raw.Text()
	})
}

// Boolean returns a typed boolean field or an invalid field retaining raw
// data.
func Boolean(object jsonvalue.Value, name string) sharedmodel.Field[bool] {
	return Scalar(object, name, func(raw jsonvalue.Value) (bool, bool) {
		return raw.Bool()
	})
}

// Scalar converts a scalar or object field through decoder.
func Scalar[T any](
	object jsonvalue.Value,
	name string,
	decoder Decoder[T],
) sharedmodel.Field[T] {
	raw, state := lookup(object, name)
	switch state {
	case stateAbsent:
		return sharedmodel.Absent[T]()
	case stateNull:
		return sharedmodel.Null[T]()
	}
	value, ok := decoder(raw)
	if !ok {
		return sharedmodel.Invalid[T](raw)
	}
	return sharedmodel.Present(value)
}

// List converts an array field element by element. Any mismatched element
// makes the field invalid while preserving the complete raw array.
func List[T any](
	object jsonvalue.Value,
	name string,
	decoder Decoder[T],
) sharedmodel.Field[sharedmodel.List[T]] {
	return Scalar(object, name, func(raw jsonvalue.Value) (sharedmodel.List[T], bool) {
		return ListValue(raw, decoder)
	})
}

// ListValue converts one raw array into an immutable typed list.
func ListValue[T any](raw jsonvalue.Value, decoder Decoder[T]) (sharedmodel.List[T], bool) {
	elements, ok := raw.Elements()
	if !ok {
		return sharedmodel.List[T]{}, false
	}
	values := make([]T, 0, len(elements))
	for _, element := range elements {
		value, valid := decoder(element)
		if !valid {
			return sharedmodel.List[T]{}, false
		}
		values = append(values, value)
	}
	return sharedmodel.NewList(values), true
}

// Map converts an object field member by member while preserving source order.
func Map[T any](
	object jsonvalue.Value,
	name string,
	decoder Decoder[T],
) sharedmodel.Field[sharedmodel.Map[T]] {
	return Scalar(object, name, func(raw jsonvalue.Value) (sharedmodel.Map[T], bool) {
		return MapValue(raw, decoder)
	})
}

// MapValue converts one raw object into an immutable typed ordered map.
func MapValue[T any](raw jsonvalue.Value, decoder Decoder[T]) (sharedmodel.Map[T], bool) {
	members, ok := raw.Members()
	if !ok {
		return sharedmodel.Map[T]{}, false
	}
	entries := make([]sharedmodel.Entry[T], 0, len(members))
	for _, member := range members {
		value, valid := decoder(member.Value)
		if !valid {
			return sharedmodel.Map[T]{}, false
		}
		entries = append(entries, sharedmodel.Entry[T]{Name: member.Name, Value: value})
	}
	result, err := sharedmodel.NewMap(entries)
	return result, err == nil
}

// Extensions returns all x- fields in source order.
func Extensions(object jsonvalue.Value) sharedmodel.Map[jsonvalue.Value] {
	members, ok := object.Members()
	if !ok {
		return sharedmodel.Map[jsonvalue.Value]{}
	}
	entries := make([]sharedmodel.Entry[jsonvalue.Value], 0)
	for _, member := range members {
		if strings.HasPrefix(member.Name, "x-") {
			entries = append(entries, sharedmodel.Entry[jsonvalue.Value]{
				Name:  member.Name,
				Value: member.Value,
			})
		}
	}
	result, _ := sharedmodel.NewMap(entries)
	return result
}

// PatternEntries returns typed patterned members in source order. Fixed and
// extension fields can be excluded without discarding invalid patterned
// values, which are represented as invalid Field entries.
func PatternEntries[T any](
	object jsonvalue.Value,
	excluded []string,
	include func(string) bool,
	decoder Decoder[T],
) sharedmodel.Map[sharedmodel.Field[T]] {
	members, ok := object.Members()
	if !ok {
		return sharedmodel.Map[sharedmodel.Field[T]]{}
	}
	excludedNames := make(map[string]struct{}, len(excluded))
	for _, name := range excluded {
		excludedNames[name] = struct{}{}
	}
	entries := make([]sharedmodel.Entry[sharedmodel.Field[T]], 0)
	for _, member := range members {
		if _, fixed := excludedNames[member.Name]; fixed || !include(member.Name) {
			continue
		}
		entries = append(entries, sharedmodel.Entry[sharedmodel.Field[T]]{
			Name:  member.Name,
			Value: typedValue(member.Value, decoder),
		})
	}
	result, _ := sharedmodel.NewMap(entries)
	return result
}

func typedValue[T any](raw jsonvalue.Value, decoder Decoder[T]) sharedmodel.Field[T] {
	if raw.Kind() == jsonvalue.NullKind {
		return sharedmodel.Null[T]()
	}
	value, ok := decoder(raw)
	if !ok {
		return sharedmodel.Invalid[T](raw)
	}
	return sharedmodel.Present(value)
}

type fieldState uint8

const (
	stateAbsent fieldState = iota
	stateNull
	stateValue
)

func lookup(object jsonvalue.Value, name string) (jsonvalue.Value, fieldState) {
	raw, present := object.Lookup(name)
	if !present {
		return jsonvalue.Value{}, stateAbsent
	}
	if raw.Kind() == jsonvalue.NullKind {
		return raw, stateNull
	}
	return raw, stateValue
}
