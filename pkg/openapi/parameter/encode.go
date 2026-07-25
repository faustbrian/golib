// Package parameter implements version-aware OpenAPI parameter codecs without
// coupling serialization to an HTTP framework or router.
package parameter

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

// ErrInvalidOptions reports a style, location, or version combination that
// the selected OpenAPI specification does not define.
var ErrInvalidOptions = errors.New("invalid parameter serialization options")

// ErrUnsupportedValue reports a nested or otherwise undefined parameter value.
var ErrUnsupportedValue = errors.New("unsupported parameter value")

// ErrLimitExceeded reports codec input, output, or collection bounds.
var ErrLimitExceeded = errors.New("parameter codec limit exceeded")

// Limits bounds encoded bytes and top-level collection items.
type Limits struct {
	// MaxBytes bounds encoded output and serialized input bytes.
	MaxBytes int
	// MaxItems bounds top-level array items or object properties.
	MaxItems int
}

// DefaultLimits returns conservative bounds for untrusted parameter values.
func DefaultLimits() Limits {
	return Limits{MaxBytes: 1 << 20, MaxItems: 10_000}
}

// Location identifies the Parameter Object's in value.
type Location string

const (
	// Path identifies substitution into a path template.
	Path Location = "path"
	// Query identifies one query string parameter.
	Query Location = "query"
	// Header identifies one HTTP header field value.
	Header Location = "header"
	// FormData identifies a Swagger 2.0 formData parameter.
	FormData Location = "formData"
	// CookieLocation identifies one Cookie header parameter.
	CookieLocation Location = "cookie"
)

// Style identifies one OpenAPI parameter serialization style.
type Style string

const (
	// Matrix is the RFC 6570 path parameter style.
	Matrix Style = "matrix"
	// Label is the RFC 6570 label path style.
	Label Style = "label"
	// Simple is the path and header simple expansion style.
	Simple Style = "simple"
	// Form is the query and cookie form expansion style.
	Form Style = "form"
	// SpaceDelimited joins query array or object tokens with encoded spaces.
	SpaceDelimited Style = "spaceDelimited"
	// PipeDelimited joins query array or object tokens with encoded pipes.
	PipeDelimited Style = "pipeDelimited"
	// DeepObject emits bracketed query names for scalar object properties.
	DeepObject Style = "deepObject"
	// Cookie is the OpenAPI 3.2 Cookie header style without implicit escaping.
	Cookie Style = "cookie"
)

// Options selects one explicit version-specific serialization behavior.
type Options struct {
	// Version selects exact version-specific style semantics.
	Version specversion.Version
	// Location is the Parameter Object's in value.
	Location Location
	// Style selects the serialization grammar.
	Style Style
	// Explode emits separate array items or object properties where defined.
	Explode bool
	// AllowReserved preserves RFC 3986 reserved characters in query values.
	AllowReserved bool
	// EmptyDecoding resolves text that cannot distinguish empty from undefined.
	// It has no effect during encoding.
	EmptyDecoding EmptyDecoding
	// Limits bounds generated or consumed data. Zero fields use defaults.
	Limits Limits
}

// Encode serializes one semantic parameter value exactly as it appears in its
// path segment, query string, header value, or Cookie header value.
func Encode(name string, value jsonvalue.Value, options Options) (string, error) {
	limits, validLimits := effectiveLimits(options.Limits)
	if name == "" || !validLimits || !validOptions(value.Kind(), options) {
		return "", ErrInvalidOptions
	}
	if value.Kind() == jsonvalue.InvalidKind {
		return "", ErrUnsupportedValue
	}
	if collectionSize(value) > limits.MaxItems {
		return "", ErrLimitExceeded
	}
	encoder := valueEncoder{options: options}
	var encoded string
	var err error
	if value.Kind() == jsonvalue.NullKind {
		encoded = encoder.undefined(name)
	} else {
		switch options.Style {
		case Matrix:
			encoded, err = encoder.matrix(name, value)
		case Label:
			encoded, err = encoder.label(value)
		case Simple:
			encoded, err = encoder.simple(value)
		case Form:
			encoded, err = encoder.form(name, value, "&")
		case SpaceDelimited:
			encoded, err = encoder.delimited(name, value, "%20")
		case PipeDelimited:
			encoded, err = encoder.delimited(name, value, "%7C")
		case DeepObject:
			encoded, err = encoder.deepObject(name, value)
		case Cookie:
			encoded, err = encoder.cookie(name, value)
		default:
			return "", ErrInvalidOptions
		}
	}
	if err != nil {
		return "", err
	}
	if len(encoded) > limits.MaxBytes {
		return "", ErrLimitExceeded
	}
	return encoded, nil
}

func effectiveLimits(limits Limits) (Limits, bool) {
	if limits.MaxBytes < 0 || limits.MaxItems < 0 {
		return Limits{}, false
	}
	defaults := DefaultLimits()
	if limits.MaxBytes == 0 {
		limits.MaxBytes = defaults.MaxBytes
	}
	if limits.MaxItems == 0 {
		limits.MaxItems = defaults.MaxItems
	}
	return limits, true
}

func collectionSize(value jsonvalue.Value) int {
	if elements, ok := value.Elements(); ok {
		return len(elements)
	}
	if members, ok := value.Members(); ok {
		return len(members)
	}
	return 0
}

type valueEncoder struct {
	options Options
}

func validOptions(kind jsonvalue.Kind, options Options) bool {
	dialect := options.Version.Dialect()
	if dialect != specversion.DialectOAS30 &&
		dialect != specversion.DialectOAS31 &&
		dialect != specversion.DialectOAS32 {
		return false
	}
	if options.AllowReserved && options.Location != Query {
		return false
	}
	switch options.Style {
	case Matrix, Label:
		return options.Location == Path
	case Simple:
		return options.Location == Path || options.Location == Header
	case Form:
		return options.Location == Query || options.Location == CookieLocation
	case SpaceDelimited, PipeDelimited:
		return options.Location == Query && !options.Explode &&
			(kind == jsonvalue.ArrayKind || kind == jsonvalue.ObjectKind)
	case DeepObject:
		return options.Location == Query && kind == jsonvalue.ObjectKind &&
			(dialect == specversion.DialectOAS32 || options.Explode)
	case Cookie:
		return dialect == specversion.DialectOAS32 &&
			options.Location == CookieLocation
	default:
		return true
	}
}

func (encoder valueEncoder) undefined(name string) string {
	encodedName := encoder.name(name)
	switch encoder.options.Style {
	case Matrix:
		return ";" + encodedName
	case Label:
		return "."
	case Simple:
		return ""
	default:
		return encodedName + "="
	}
}

func (encoder valueEncoder) matrix(name string, value jsonvalue.Value) (string, error) {
	encodedName := encoder.name(name)
	if value.Kind() == jsonvalue.ArrayKind && encoder.options.Explode {
		values, err := encoder.array(value)
		if err != nil {
			return "", err
		}
		parts := make([]string, len(values))
		for index, item := range values {
			parts[index] = encodedName + "=" + item
		}
		return ";" + strings.Join(parts, ";"), nil
	}
	if value.Kind() == jsonvalue.ObjectKind && encoder.options.Explode {
		pairs, err := encoder.object(value, "=", ";")
		return ";" + pairs, err
	}
	flat, err := encoder.flat(value, ",")
	return ";" + encodedName + "=" + flat, err
}

func (encoder valueEncoder) label(value jsonvalue.Value) (string, error) {
	delimiter := ","
	if encoder.options.Explode {
		if value.Kind() == jsonvalue.ArrayKind {
			delimiter = "."
		} else if value.Kind() == jsonvalue.ObjectKind {
			pairs, err := encoder.object(value, "=", ".")
			return "." + pairs, err
		}
	}
	flat, err := encoder.flat(value, delimiter)
	return "." + flat, err
}

func (encoder valueEncoder) simple(value jsonvalue.Value) (string, error) {
	if value.Kind() == jsonvalue.ObjectKind && encoder.options.Explode {
		return encoder.object(value, "=", ",")
	}
	return encoder.flat(value, ",")
}

func (encoder valueEncoder) form(name string, value jsonvalue.Value, separator string) (string, error) {
	encodedName := encoder.name(name)
	if value.Kind() == jsonvalue.ArrayKind && encoder.options.Explode {
		values, err := encoder.array(value)
		if err != nil {
			return "", err
		}
		parts := make([]string, len(values))
		for index, item := range values {
			parts[index] = encodedName + "=" + item
		}
		return strings.Join(parts, separator), nil
	}
	if value.Kind() == jsonvalue.ObjectKind && encoder.options.Explode {
		return encoder.object(value, "=", separator)
	}
	flat, err := encoder.flat(value, ",")
	return encodedName + "=" + flat, err
}

func (encoder valueEncoder) delimited(name string, value jsonvalue.Value, delimiter string) (string, error) {
	flat, err := encoder.flat(value, delimiter)
	return encoder.name(name) + "=" + flat, err
}

func (encoder valueEncoder) deepObject(name string, value jsonvalue.Value) (string, error) {
	members, _ := value.Members()
	parts := make([]string, len(members))
	for index, member := range members {
		scalar, err := encoder.scalar(member.Value)
		if err != nil {
			return "", err
		}
		parts[index] = encoder.name(name+"["+member.Name+"]") + "=" + scalar
	}
	return strings.Join(parts, "&"), nil
}

func (encoder valueEncoder) cookie(name string, value jsonvalue.Value) (string, error) {
	return encoder.form(name, value, "; ")
}

func (encoder valueEncoder) flat(value jsonvalue.Value, delimiter string) (string, error) {
	switch value.Kind() {
	case jsonvalue.ArrayKind:
		values, err := encoder.array(value)
		return strings.Join(values, delimiter), err
	case jsonvalue.ObjectKind:
		return encoder.object(value, delimiter, delimiter)
	default:
		return encoder.scalar(value)
	}
}

func (encoder valueEncoder) array(value jsonvalue.Value) ([]string, error) {
	elements, _ := value.Elements()
	result := make([]string, len(elements))
	for index, element := range elements {
		scalar, err := encoder.scalar(element)
		if err != nil {
			return nil, err
		}
		result[index] = scalar
	}
	return result, nil
}

func (encoder valueEncoder) object(value jsonvalue.Value, assignment string, separator string) (string, error) {
	members, _ := value.Members()
	parts := make([]string, len(members))
	for index, member := range members {
		scalar, err := encoder.scalar(member.Value)
		if err != nil {
			return "", err
		}
		parts[index] = encoder.token(member.Name) + assignment + scalar
	}
	return strings.Join(parts, separator), nil
}

func (encoder valueEncoder) scalar(value jsonvalue.Value) (string, error) {
	var raw string
	switch value.Kind() {
	case jsonvalue.StringKind:
		raw, _ = value.Text()
	case jsonvalue.NumberKind:
		raw, _ = value.NumberText()
	case jsonvalue.BooleanKind:
		boolean, _ := value.Bool()
		raw = strconv.FormatBool(boolean)
	default:
		return "", fmt.Errorf("%w: nested or undefined value", ErrUnsupportedValue)
	}
	return encoder.token(raw), nil
}

func (encoder valueEncoder) token(value string) string {
	if encoder.options.Style == Cookie || rawHeaderValue(encoder.options) {
		return value
	}
	return percentEncode(value, encoder.options.AllowReserved, encoder.options.Location == Query)
}

func (encoder valueEncoder) name(value string) string {
	if encoder.options.Style == Cookie || rawHeaderValue(encoder.options) {
		return value
	}
	return percentEncode(value, false, encoder.options.Location == Query)
}

func rawHeaderValue(options Options) bool {
	return options.Location == Header &&
		(options.Version.String() == "3.1.2" ||
			options.Version.Dialect() == specversion.DialectOAS32)
}

func percentEncode(value string, allowReserved bool, form bool) string {
	const hexadecimal = "0123456789ABCDEF"
	var result strings.Builder
	for index := 0; index < len(value); index++ {
		character := value[index]
		if isUnreserved(character) && (!form || character != '~') ||
			allowReserved && strings.ContainsRune(":/?#[]@!$&'()*+,;=", rune(character)) {
			result.WriteByte(character)
			continue
		}
		result.WriteByte('%')
		result.WriteByte(hexadecimal[character>>4])
		result.WriteByte(hexadecimal[character&0x0f])
	}
	return result.String()
}

func isUnreserved(character byte) bool {
	const unreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"abcdefghijklmnopqrstuvwxyz0123456789-._~"
	return strings.IndexByte(unreserved, character) >= 0
}
