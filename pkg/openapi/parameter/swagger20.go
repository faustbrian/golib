package parameter

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

// CollectionFormat identifies a Swagger 2.0 array serialization format.
type CollectionFormat string

const (
	// CollectionCSV separates array items with commas and is the default.
	CollectionCSV CollectionFormat = "csv"
	// CollectionSSV separates array items with spaces.
	CollectionSSV CollectionFormat = "ssv"
	// CollectionTSV separates array items with tabs.
	CollectionTSV CollectionFormat = "tsv"
	// CollectionPipes separates array items with vertical bars.
	CollectionPipes CollectionFormat = "pipes"
	// CollectionMulti emits one query or formData parameter per array item.
	CollectionMulti CollectionFormat = "multi"
)

// Swagger20Options selects Swagger 2.0 parameter serialization behavior.
type Swagger20Options struct {
	// Location is the Parameter Object's in value.
	Location Location
	// Format selects the array collectionFormat. An empty value means csv.
	Format CollectionFormat
	// EmptyDecoding resolves text that cannot distinguish empty from undefined.
	EmptyDecoding EmptyDecoding
	// Limits bounds generated or consumed data. Zero fields use defaults.
	Limits Limits
}

// EncodeSwagger20 serializes a primitive or array parameter according to the
// Swagger 2.0 collectionFormat rules.
func EncodeSwagger20(
	name string,
	value jsonvalue.Value,
	options Swagger20Options,
) (string, error) {
	limits, validLimits := effectiveLimits(options.Limits)
	format, validFormat := swaggerFormat(options.Format)
	if name == "" || !validLimits || !validFormat ||
		!validSwaggerValue(value.Kind(), options.Location, format) {
		return "", ErrInvalidOptions
	}
	if value.Kind() == jsonvalue.InvalidKind {
		return "", ErrUnsupportedValue
	}
	if collectionSize(value) > limits.MaxItems {
		return "", ErrLimitExceeded
	}

	encoded, err := encodeSwaggerValue(name, value, options.Location, format)
	if err != nil {
		return "", err
	}
	if len(encoded) > limits.MaxBytes {
		return "", ErrLimitExceeded
	}
	return encoded, nil
}

// DecodeSwagger20 parses one isolated Swagger 2.0 primitive or array
// serialization. Scalar tokens remain strings for caller-owned coercion.
func DecodeSwagger20(
	name string,
	raw string,
	shape Shape,
	options Swagger20Options,
) (jsonvalue.Value, error) {
	limits, validLimits := effectiveLimits(options.Limits)
	format, validFormat := swaggerFormat(options.Format)
	kind, validShape := shapeKind(shape)
	if name == "" || !validLimits || !validFormat || !validShape ||
		!validSwaggerValue(kind, options.Location, format) {
		return jsonvalue.Value{}, ErrInvalidOptions
	}
	if len(raw) > limits.MaxBytes {
		return jsonvalue.Value{}, ErrLimitExceeded
	}

	value, err := decodeSwaggerValue(name, raw, shape, options, format, limits.MaxItems)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	return value, nil
}

func swaggerFormat(format CollectionFormat) (CollectionFormat, bool) {
	if format == "" {
		return CollectionCSV, true
	}
	switch format {
	case CollectionCSV, CollectionSSV, CollectionTSV, CollectionPipes, CollectionMulti:
		return format, true
	default:
		return "", false
	}
}

func validSwaggerValue(kind jsonvalue.Kind, location Location, format CollectionFormat) bool {
	if location != Path && location != Query && location != Header && location != FormData {
		return false
	}
	if format == CollectionMulti && location != Query && location != FormData {
		return false
	}
	switch kind {
	case jsonvalue.InvalidKind:
		return true
	case jsonvalue.NullKind, jsonvalue.StringKind, jsonvalue.NumberKind, jsonvalue.BooleanKind:
		return format == CollectionCSV
	case jsonvalue.ArrayKind:
		return true
	default:
		return false
	}
}

func encodeSwaggerValue(
	name string,
	value jsonvalue.Value,
	location Location,
	format CollectionFormat,
) (string, error) {
	prefix := swaggerPrefix(name, location)
	if value.Kind() == jsonvalue.NullKind {
		return prefix, nil
	}
	if value.Kind() != jsonvalue.ArrayKind {
		scalar, err := swaggerScalar(value, location)
		return prefix + scalar, err
	}

	elements, _ := value.Elements()
	values := make([]string, len(elements))
	for index, element := range elements {
		scalar, err := swaggerScalar(element, location)
		if err != nil {
			return "", err
		}
		values[index] = scalar
	}
	if format == CollectionMulti {
		parts := make([]string, len(values))
		for index, value := range values {
			parts[index] = prefix + value
		}
		return strings.Join(parts, "&"), nil
	}
	return prefix + strings.Join(values, swaggerDelimiter(format)), nil
}

func decodeSwaggerValue(
	name string,
	raw string,
	shape Shape,
	options Swagger20Options,
	format CollectionFormat,
	maxItems int,
) (jsonvalue.Value, error) {
	if format == CollectionMulti {
		return decodeSwaggerMulti(name, raw, options, maxItems)
	}
	payload := raw
	if options.Location == Query || options.Location == FormData {
		if strings.Contains(raw, "&") {
			return jsonvalue.Value{}, ErrMalformedEncoding
		}
		var err error
		payload, err = swaggerNamed(name, raw)
		if err != nil {
			return jsonvalue.Value{}, err
		}
	}
	parts, err := splitEncoded(payload, swaggerDelimiter(format), maxItems)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	decoder := valueDecoder{options: Options{
		Location:      swaggerTokenLocation(options.Location),
		EmptyDecoding: options.EmptyDecoding,
	}, maxItems: maxItems}
	return decoder.parts(parts, shape, false)
}

func decodeSwaggerMulti(
	name string,
	raw string,
	options Swagger20Options,
	maxItems int,
) (jsonvalue.Value, error) {
	if raw == "" {
		decoder := valueDecoder{
			options: Options{EmptyDecoding: options.EmptyDecoding}, maxItems: maxItems,
		}
		return decoder.empty(Array)
	}
	parts, err := splitNonEmpty(raw, "&", maxItems)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	values := make([]jsonvalue.Value, len(parts))
	for index, part := range parts {
		payload, err := swaggerNamed(name, part)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		value, err := swaggerString(payload)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		values[index] = value
	}
	return makeArray(values)
}

func swaggerPrefix(name string, location Location) string {
	if location == Query || location == FormData {
		return percentEncode(name, false, true) + "="
	}
	return ""
}

func swaggerDelimiter(format CollectionFormat) string {
	switch format {
	case CollectionSSV:
		return "%20"
	case CollectionTSV:
		return "%09"
	case CollectionPipes:
		return "%7C"
	default:
		return ","
	}
}

func swaggerScalar(value jsonvalue.Value, location Location) (string, error) {
	encoder := valueEncoder{options: Options{Location: swaggerTokenLocation(location)}}
	return encoder.scalar(value)
}

func swaggerString(raw string) (jsonvalue.Value, error) {
	decoded, err := swaggerToken(raw)
	if err != nil {
		return jsonvalue.Value{}, ErrMalformedEncoding
	}
	value, err := jsonvalue.String(decoded)
	if err != nil {
		return jsonvalue.Value{}, fmt.Errorf("%w: decoded string", ErrMalformedEncoding)
	}
	return value, nil
}

func swaggerNamed(name string, raw string) (string, error) {
	rawName, payload, ok := strings.Cut(raw, "=")
	if !ok {
		return "", ErrMalformedEncoding
	}
	decodedName, err := swaggerToken(rawName)
	if err != nil || decodedName != name {
		return "", ErrMalformedEncoding
	}
	return payload, nil
}

func swaggerToken(raw string) (string, error) {
	return url.QueryUnescape(raw)
}

func swaggerTokenLocation(location Location) Location {
	if location == FormData {
		return Query
	}
	return location
}
