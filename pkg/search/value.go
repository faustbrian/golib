package search

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"time"
)

const (
	// MaxFieldNameBytes bounds individual field names before backend encoding.
	MaxFieldNameBytes = 256
	// MaxNumberBytes bounds an exact decimal representation.
	MaxNumberBytes = 256
)

var (
	errInvalidNumber   = errors.New("search: invalid exact number")
	errInvalidGeoPoint = errors.New("search: invalid geo point")
	exactNumberPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)
)

// ValueKind identifies a lossless search document value representation.
type ValueKind uint8

const (
	KindNull ValueKind = iota
	KindString
	KindNumber
	KindBool
	KindTime
	KindGeo
	KindArray
	KindObject
)

// GeoPoint is a WGS84 latitude and longitude pair.
type GeoPoint struct {
	Latitude  float64
	Longitude float64
}

// Value is an immutable, JSON-compatible document value. Numbers retain their
// decimal spelling and timestamps are normalized to UTC without losing
// nanoseconds. A missing field is represented only by absence from a field map;
// NullValue represents an explicitly present null.
type Value struct {
	kind   ValueKind
	text   string
	truth  bool
	point  GeoPoint
	array  []Value
	object map[string]Value
}

// NullValue returns an explicit null value.
func NullValue() Value { return Value{kind: KindNull} }

// StringValue returns a Unicode string value. Empty strings remain distinct
// from null and missing fields.
func StringValue(value string) Value { return Value{kind: KindString, text: value} }

// NumberValue parses a bounded, exact base-10 number without converting it to
// binary floating point.
func NumberValue(value string) (Value, error) {
	if len(value) == 0 || len(value) > MaxNumberBytes || !exactNumberPattern.MatchString(value) {
		return Value{}, errInvalidNumber
	}

	return Value{kind: KindNumber, text: value}, nil
}

// BoolValue returns a Boolean value.
func BoolValue(value bool) Value { return Value{kind: KindBool, truth: value} }

// TimeValue returns a timestamp normalized to UTC while retaining nanoseconds.
func TimeValue(value time.Time) Value {
	return Value{kind: KindTime, text: value.UTC().Format(time.RFC3339Nano)}
}

// GeoValue validates and returns a WGS84 point.
func GeoValue(latitude, longitude float64) (Value, error) {
	if math.IsNaN(latitude) || math.IsInf(latitude, 0) || latitude < -90 || latitude > 90 ||
		math.IsNaN(longitude) || math.IsInf(longitude, 0) || longitude < -180 || longitude > 180 {
		return Value{}, errInvalidGeoPoint
	}

	return Value{kind: KindGeo, point: GeoPoint{Latitude: latitude, Longitude: longitude}}, nil
}

// ArrayValue copies the supplied values and returns an array value.
func ArrayValue(values []Value) Value {
	return Value{kind: KindArray, array: cloneValues(values)}
}

// ObjectValue copies the supplied fields and returns a nested object value.
func ObjectValue(fields map[string]Value) Value {
	return Value{kind: KindObject, object: cloneFields(fields)}
}

// Kind reports the value's representation.
func (v Value) Kind() ValueKind { return v.kind }

// MarshalJSON implements json.Marshaler without converting exact numbers to
// float64.
func (v Value) MarshalJSON() ([]byte, error) {
	switch v.kind {
	case KindNull:
		return []byte("null"), nil
	case KindString, KindTime:
		return json.Marshal(v.text)
	case KindNumber:
		return []byte(v.text), nil
	case KindBool:
		return json.Marshal(v.truth)
	case KindGeo:
		return json.Marshal(struct {
			Latitude  float64 `json:"lat"`
			Longitude float64 `json:"lon"`
		}{Latitude: v.point.Latitude, Longitude: v.point.Longitude})
	case KindArray:
		return json.Marshal(v.array)
	case KindObject:
		return json.Marshal(v.object)
	default:
		return nil, fmt.Errorf("search: unknown value kind %d", v.kind)
	}
}

func cloneFields(fields map[string]Value) map[string]Value {
	if fields == nil {
		return nil
	}
	result := make(map[string]Value, len(fields))
	for name, value := range fields {
		result[name] = value.clone()
	}
	return result
}

func cloneValues(values []Value) []Value {
	if values == nil {
		return nil
	}
	result := make([]Value, len(values))
	for index, value := range values {
		result[index] = value.clone()
	}
	return result
}

func (v Value) clone() Value {
	v.array = cloneValues(v.array)
	v.object = cloneFields(v.object)
	return v
}
