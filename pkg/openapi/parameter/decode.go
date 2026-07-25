package parameter

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

// ErrMalformedEncoding reports text outside the selected parameter grammar.
var ErrMalformedEncoding = errors.New("malformed parameter encoding")

// ErrAmbiguousValue reports an empty serialization that needs caller policy.
var ErrAmbiguousValue = errors.New("ambiguous empty parameter value")

// EmptyDecoding selects how Decode interprets an empty serialized payload.
type EmptyDecoding uint8

const (
	// RejectEmptyAmbiguity rejects empty payloads that have multiple meanings.
	RejectEmptyAmbiguity EmptyDecoding = iota
	// EmptyAsValue chooses an empty scalar or one empty array item.
	EmptyAsValue
	// EmptyAsCollection chooses an empty array or object.
	EmptyAsCollection
	// EmptyAsNull chooses the undefined JSON null value.
	EmptyAsNull
)

// Shape identifies the schema-level value shape needed to reverse an
// otherwise ambiguous parameter serialization.
type Shape uint8

const (
	// Primitive selects a scalar string result.
	Primitive Shape = 1
	// Array selects an ordered array of scalar strings.
	Array Shape = 2
	// Object selects an ordered object with scalar string properties.
	Object Shape = 3
)

// Decode parses one isolated parameter serialization. Scalar tokens remain
// strings; schema-aware type coercion is deliberately left to the caller.
func Decode(
	name string,
	raw string,
	shape Shape,
	options Options,
) (jsonvalue.Value, error) {
	kind, ok := shapeKind(shape)
	limits, validLimits := effectiveLimits(options.Limits)
	if name == "" || !ok || !validLimits || !validOptions(kind, options) {
		return jsonvalue.Value{}, ErrInvalidOptions
	}
	if len(raw) > limits.MaxBytes {
		return jsonvalue.Value{}, ErrLimitExceeded
	}
	decoder := valueDecoder{options: options, maxItems: limits.MaxItems}
	var value jsonvalue.Value
	var err error
	switch options.Style {
	case Matrix:
		value, err = decoder.matrix(name, raw, shape)
	case Label:
		value, err = decoder.label(raw, shape)
	case Simple:
		value, err = decoder.flat(raw, shape, ",", options.Explode)
	case Form:
		value, err = decoder.form(name, raw, shape, "&")
	case SpaceDelimited:
		value, err = decoder.delimited(name, raw, shape, "%20")
	case PipeDelimited:
		value, err = decoder.delimited(name, raw, shape, "%7C")
	case DeepObject:
		value, err = decoder.deepObject(name, raw)
	case Cookie:
		value, err = decoder.form(name, raw, shape, "; ")
	default:
		return jsonvalue.Value{}, ErrInvalidOptions
	}
	if err != nil {
		return jsonvalue.Value{}, err
	}
	return value, nil
}

type valueDecoder struct {
	options  Options
	maxItems int
}

func shapeKind(shape Shape) (jsonvalue.Kind, bool) {
	switch shape {
	case Primitive:
		return jsonvalue.StringKind, true
	case Array:
		return jsonvalue.ArrayKind, true
	case Object:
		return jsonvalue.ObjectKind, true
	default:
		return jsonvalue.InvalidKind, false
	}
}

func (decoder valueDecoder) matrix(name string, raw string, shape Shape) (jsonvalue.Value, error) {
	if !strings.HasPrefix(raw, ";") {
		return jsonvalue.Value{}, ErrMalformedEncoding
	}
	body := strings.TrimPrefix(raw, ";")
	if !strings.Contains(body, "=") {
		decoded, err := decoder.token(body)
		if err != nil || decoded != name {
			return jsonvalue.Value{}, ErrMalformedEncoding
		}
		return jsonvalue.Null(), nil
	}
	if decoder.options.Explode && shape == Array {
		return decoder.repeated(name, body, ";")
	}
	if decoder.options.Explode && shape == Object {
		return decoder.pairs(body, ";")
	}
	payload, err := decoder.named(name, body)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	return decoder.flat(payload, shape, ",", false)
}

func (decoder valueDecoder) label(raw string, shape Shape) (jsonvalue.Value, error) {
	if !strings.HasPrefix(raw, ".") {
		return jsonvalue.Value{}, ErrMalformedEncoding
	}
	delimiter := ","
	if decoder.options.Explode && (shape == Array || shape == Object) {
		delimiter = "."
	}
	return decoder.flat(strings.TrimPrefix(raw, "."), shape, delimiter, decoder.options.Explode)
}

func (decoder valueDecoder) form(name string, raw string, shape Shape, separator string) (jsonvalue.Value, error) {
	if decoder.options.Explode && shape == Array {
		return decoder.repeated(name, raw, separator)
	}
	if decoder.options.Explode && shape == Object {
		return decoder.pairs(raw, separator)
	}
	payload, err := decoder.named(name, raw)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	return decoder.flat(payload, shape, ",", false)
}

func (decoder valueDecoder) delimited(
	name string,
	raw string,
	shape Shape,
	delimiter string,
) (jsonvalue.Value, error) {
	payload, err := decoder.named(name, raw)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	return decoder.flatEncoded(payload, shape, delimiter)
}

func (decoder valueDecoder) deepObject(name string, raw string) (jsonvalue.Value, error) {
	parts, err := splitNonEmpty(raw, "&", decoder.maxItems)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	members := make([]jsonvalue.Member, 0, len(parts))
	for _, part := range parts {
		rawName, rawValue, ok := strings.Cut(part, "=")
		if !ok {
			return jsonvalue.Value{}, ErrMalformedEncoding
		}
		decodedName, err := decoder.token(rawName)
		if err != nil || !strings.HasPrefix(decodedName, name+"[") ||
			!strings.HasSuffix(decodedName, "]") {
			return jsonvalue.Value{}, ErrMalformedEncoding
		}
		property := strings.TrimSuffix(strings.TrimPrefix(decodedName, name+"["), "]")
		if property == "" || strings.ContainsAny(property, "[]") {
			return jsonvalue.Value{}, ErrMalformedEncoding
		}
		value, err := decoder.string(rawValue)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		members = append(members, jsonvalue.Member{Name: property, Value: value})
	}
	return makeObject(members)
}

func (decoder valueDecoder) named(name string, raw string) (string, error) {
	rawName, payload, ok := strings.Cut(raw, "=")
	if !ok {
		return "", ErrMalformedEncoding
	}
	decoded, err := decoder.token(rawName)
	if err != nil || decoded != name {
		return "", ErrMalformedEncoding
	}
	return payload, nil
}

func (decoder valueDecoder) repeated(name string, raw string, separator string) (jsonvalue.Value, error) {
	parts, err := splitNonEmpty(raw, separator, decoder.maxItems)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	values := make([]jsonvalue.Value, 0, len(parts))
	for _, part := range parts {
		payload, err := decoder.named(name, part)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		value, err := decoder.string(payload)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		values = append(values, value)
	}
	return makeArray(values)
}

func (decoder valueDecoder) pairs(raw string, separator string) (jsonvalue.Value, error) {
	parts, err := splitNonEmpty(raw, separator, decoder.maxItems)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	members := make([]jsonvalue.Member, 0, len(parts))
	for _, part := range parts {
		rawName, rawValue, ok := strings.Cut(part, "=")
		if !ok {
			return jsonvalue.Value{}, ErrMalformedEncoding
		}
		name, err := decoder.token(rawName)
		if err != nil || name == "" {
			return jsonvalue.Value{}, ErrMalformedEncoding
		}
		value, err := decoder.string(rawValue)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		members = append(members, jsonvalue.Member{Name: name, Value: value})
	}
	return makeObject(members)
}

func (decoder valueDecoder) flat(
	raw string,
	shape Shape,
	delimiter string,
	exploded bool,
) (jsonvalue.Value, error) {
	parts, err := splitNonEmpty(raw, delimiter, decoder.maximumParts(shape, exploded))
	if err != nil {
		return jsonvalue.Value{}, err
	}
	if raw == "" {
		parts = []string{""}
	}
	return decoder.parts(parts, shape, exploded)
}

func (decoder valueDecoder) flatEncoded(
	raw string,
	shape Shape,
	delimiter string,
) (jsonvalue.Value, error) {
	parts, err := splitEncoded(raw, delimiter, decoder.maximumParts(shape, false))
	if err != nil {
		return jsonvalue.Value{}, err
	}
	return decoder.parts(parts, shape, false)
}

func (decoder valueDecoder) maximumParts(shape Shape, exploded bool) int {
	if shape != Object || exploded || decoder.maxItems > int(^uint(0)>>1)/2 {
		return decoder.maxItems
	}
	return decoder.maxItems * 2
}

func (decoder valueDecoder) parts(
	parts []string,
	shape Shape,
	exploded bool,
) (jsonvalue.Value, error) {
	if len(parts) == 1 && parts[0] == "" {
		return decoder.empty(shape)
	}
	if shape == Primitive {
		if len(parts) != 1 {
			return jsonvalue.Value{}, ErrMalformedEncoding
		}
		return decoder.string(parts[0])
	}
	if shape == Array {
		values := make([]jsonvalue.Value, len(parts))
		for index, part := range parts {
			value, err := decoder.string(part)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			values[index] = value
		}
		return makeArray(values)
	}
	if exploded {
		return decoder.pairs(strings.Join(parts, ","), ",")
	}
	if len(parts)%2 != 0 {
		return jsonvalue.Value{}, ErrMalformedEncoding
	}
	var members []jsonvalue.Member
	for index := 0; index < len(parts); index += 2 {
		name, err := decoder.token(parts[index])
		if err != nil || name == "" {
			return jsonvalue.Value{}, ErrMalformedEncoding
		}
		value, err := decoder.string(parts[index+1])
		if err != nil {
			return jsonvalue.Value{}, err
		}
		members = append(members, jsonvalue.Member{Name: name, Value: value})
	}
	return makeObject(members)
}

func (decoder valueDecoder) empty(shape Shape) (jsonvalue.Value, error) {
	switch decoder.options.EmptyDecoding {
	case EmptyAsNull:
		return jsonvalue.Null(), nil
	case EmptyAsValue:
		empty, _ := jsonvalue.String("")
		if shape == Primitive {
			return empty, nil
		}
		if shape == Array {
			return makeArray([]jsonvalue.Value{empty})
		}
	case EmptyAsCollection:
		if shape == Array {
			return makeArray(nil)
		}
		if shape == Object {
			return makeObject(nil)
		}
	}
	return jsonvalue.Value{}, ErrAmbiguousValue
}

func (decoder valueDecoder) string(raw string) (jsonvalue.Value, error) {
	decoded, err := decoder.token(raw)
	if err != nil {
		return jsonvalue.Value{}, ErrMalformedEncoding
	}
	value, err := jsonvalue.String(decoded)
	if err != nil {
		return jsonvalue.Value{}, fmt.Errorf("%w: decoded string", ErrMalformedEncoding)
	}
	return value, nil
}

func (decoder valueDecoder) token(raw string) (string, error) {
	if decoder.options.Style == Cookie || rawHeaderValue(decoder.options) {
		return raw, nil
	}
	if decoder.options.Location == Query {
		return url.QueryUnescape(raw)
	}
	return url.PathUnescape(raw)
}

func makeArray(values []jsonvalue.Value) (jsonvalue.Value, error) {
	value, _ := jsonvalue.Array(values)
	return value, nil
}

func makeObject(members []jsonvalue.Member) (jsonvalue.Value, error) {
	value, err := jsonvalue.Object(members)
	if err != nil {
		return jsonvalue.Value{}, fmt.Errorf("%w: object", ErrMalformedEncoding)
	}
	return value, nil
}

func splitNonEmpty(raw string, separator string, maximum int) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	if strings.Count(raw, separator)+1 > maximum {
		return nil, ErrLimitExceeded
	}
	return strings.Split(raw, separator), nil
}

func splitEncoded(raw string, delimiter string, maximum int) ([]string, error) {
	switch delimiter {
	case "":
		return []string{raw}, nil
	}
	var parts []string
	start := 0
	for {
		if len(parts) == maximum {
			return nil, ErrLimitExceeded
		}
		index := indexASCIIFold(raw, delimiter, start)
		if index < 0 {
			return append(parts, raw[start:]), nil
		}
		parts = append(parts, raw[start:index])
		start = index + len(delimiter)
	}
}

func indexASCIIFold(value string, needle string, start int) int {
	maximum := len(value) - len(needle)
	if start > maximum {
		return -1
	}
	for offset := range maximum - start + 1 {
		index := start + offset
		if strings.EqualFold(value[index:index+len(needle)], needle) {
			return index
		}
	}
	return -1
}
