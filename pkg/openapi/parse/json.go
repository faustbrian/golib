// Package parse provides bounded, IO-free OpenAPI representation parsing.
package parse

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

var (
	// ErrInvalidJSON reports malformed or ambiguous JSON input.
	ErrInvalidJSON = errors.New("invalid JSON")
	// ErrDuplicateKey reports an ambiguous repeated object member.
	ErrDuplicateKey = errors.New("duplicate object member")
	// ErrLimitExceeded reports that caller-selected resource limits were hit.
	ErrLimitExceeded = errors.New("parse limit exceeded")
	// ErrInvalidLimits reports an unusable limit configuration.
	ErrInvalidLimits = errors.New("invalid parse limits")
)

// Limits bounds all independently controllable JSON parser growth axes.
type Limits struct {
	MaxBytes         int64
	MaxTokens        int
	MaxDepth         int
	MaxObjectMembers int
	MaxArrayItems    int
	MaxScalarBytes   int
	MaxTotalValues   int
}

// DefaultLimits returns conservative limits suitable for untrusted API
// descriptions. Callers should lower them for smaller expected documents.
func DefaultLimits() Limits {
	return Limits{
		MaxBytes:         16 * 1024 * 1024,
		MaxTokens:        1_000_000,
		MaxDepth:         256,
		MaxObjectMembers: 100_000,
		MaxArrayItems:    100_000,
		MaxScalarBytes:   1024 * 1024,
		MaxTotalValues:   500_000,
	}
}

// Error is a bounded JSON parser diagnostic. It never includes document
// contents or object member names.
type Error struct {
	Code   string
	Offset int64
	Kind   error
	Cause  error
}

func (parseError *Error) Error() string {
	if parseError == nil {
		return "parse: <nil>"
	}
	return fmt.Sprintf("parse: %s at byte %d", parseError.Code, parseError.Offset)
}

// Unwrap exposes both the stable classification and underlying reader or
// decoder cause.
func (parseError *Error) Unwrap() []error {
	if parseError == nil {
		return nil
	}
	causes := make([]error, 0, 2)
	if parseError.Kind != nil {
		causes = append(causes, parseError.Kind)
	}
	if parseError.Cause != nil {
		causes = append(causes, parseError.Cause)
	}
	return causes
}

// JSON parses exactly one JSON value without performing implicit IO.
func JSON(ctx context.Context, reader io.Reader, limits Limits) (jsonvalue.Value, error) {
	if ctx == nil {
		return jsonvalue.Value{}, &Error{Code: "invalid_context", Kind: ErrInvalidJSON}
	}
	if reader == nil {
		return jsonvalue.Value{}, &Error{Code: "nil_reader", Kind: ErrInvalidJSON}
	}
	if err := validateLimits(limits); err != nil {
		return jsonvalue.Value{}, err
	}
	if err := ctx.Err(); err != nil {
		return jsonvalue.Value{}, err
	}

	raw, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: reader}, limits.MaxBytes+1))
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return jsonvalue.Value{}, contextErr
		}
		return jsonvalue.Value{}, &Error{Code: "reader_failed", Kind: ErrInvalidJSON, Cause: err}
	}
	if int64(len(raw)) > limits.MaxBytes {
		return jsonvalue.Value{}, &Error{Code: "max_bytes", Offset: limits.MaxBytes, Kind: ErrLimitExceeded}
	}
	if !utf8.Valid(raw) {
		return jsonvalue.Value{}, &Error{Code: "invalid_utf8", Kind: ErrInvalidJSON}
	}
	if offset, invalid := unpairedSurrogateEscape(raw); invalid {
		return jsonvalue.Value{}, &Error{
			Code: "unpaired_unicode_surrogate", Offset: int64(offset), Kind: ErrInvalidJSON,
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	parser := jsonParser{ctx: ctx, decoder: decoder, limits: limits}
	value, err := parser.value(1)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("trailing JSON value")
		}
		return jsonvalue.Value{}, parser.wrap("trailing_data", ErrInvalidJSON, err)
	}

	return value, nil
}

func unpairedSurrogateEscape(raw []byte) (int, bool) {
	pairedLowOffset := -1
	backslashRunOdd := false
	for index, character := range raw {
		if character == '\\' {
			backslashRunOdd = !backslashRunOdd
			continue
		}
		if character != 'u' || !backslashRunOdd {
			backslashRunOdd = false
			continue
		}
		escapeOffset := index - 1
		backslashRunOdd = false
		if escapeOffset == pairedLowOffset {
			continue
		}
		value, valid := jsonHexQuad(raw[index+1:])
		if !valid {
			continue
		}
		if value >= 0xdc00 && value <= 0xdfff {
			return escapeOffset, true
		}
		if value < 0xd800 || value > 0xdbff {
			continue
		}
		if escapeOffset+12 > len(raw) || raw[escapeOffset+6] != '\\' || raw[escapeOffset+7] != 'u' {
			return escapeOffset, true
		}
		low, valid := jsonHexQuad(raw[escapeOffset+8:])
		if !valid || low < 0xdc00 || low > 0xdfff {
			return escapeOffset, true
		}
		pairedLowOffset = escapeOffset + 6
	}
	return 0, false
}

func jsonHexQuad(raw []byte) (uint16, bool) {
	if len(raw) < 4 {
		return 0, false
	}
	var decoded [2]byte
	if _, err := hex.Decode(decoded[:], raw[:4]); err != nil {
		return 0, false
	}
	return uint16(decoded[0])<<8 | uint16(decoded[1]), true
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-reader.ctx.Done():
		return 0, reader.ctx.Err()
	default:
		return reader.reader.Read(buffer)
	}
}

type jsonParser struct {
	ctx     context.Context
	decoder *json.Decoder
	limits  Limits
	tokens  int
	values  int
}

func (parser *jsonParser) value(depth int) (jsonvalue.Value, error) {
	if err := parser.ctx.Err(); err != nil {
		return jsonvalue.Value{}, err
	}
	if depth > parser.limits.MaxDepth {
		return jsonvalue.Value{}, parser.wrap("max_depth", ErrLimitExceeded, nil)
	}
	parser.values++
	if parser.values > parser.limits.MaxTotalValues {
		return jsonvalue.Value{}, parser.wrap("max_values", ErrLimitExceeded, nil)
	}

	token, err := parser.token()
	if err != nil {
		return jsonvalue.Value{}, err
	}
	if token == nil {
		return jsonvalue.Null(), nil
	}
	if boolean, ok := token.(bool); ok {
		return jsonvalue.Boolean(boolean), nil
	}
	if number, ok := token.(json.Number); ok {
		if len(number.String()) > parser.limits.MaxScalarBytes {
			return jsonvalue.Value{}, parser.wrap("max_scalar_bytes", ErrLimitExceeded, nil)
		}
		// Decoder.Token only returns syntactically valid JSON numbers.
		value, _ := jsonvalue.Number(number.String())
		return value, nil
	}
	if text, ok := token.(string); ok {
		if len(text) > parser.limits.MaxScalarBytes {
			return jsonvalue.Value{}, parser.wrap("max_scalar_bytes", ErrLimitExceeded, nil)
		}
		// The complete input was validated as UTF-8 before decoding.
		value, _ := jsonvalue.String(text)
		return value, nil
	}
	delimiter := token.(json.Delim)
	if delimiter == '{' {
		return parser.object(depth)
	}
	// At a value boundary Decoder.Token can only return an opening object or
	// array delimiter; malformed closing delimiters fail in token.
	return parser.array(depth)
}

func (parser *jsonParser) object(depth int) (jsonvalue.Value, error) {
	members := make([]jsonvalue.Member, 0)
	names := make(map[string]struct{})
	for parser.decoder.More() {
		if len(members) >= parser.limits.MaxObjectMembers {
			return jsonvalue.Value{}, parser.wrap("max_object_members", ErrLimitExceeded, nil)
		}
		nameToken, err := parser.token()
		if err != nil {
			return jsonvalue.Value{}, err
		}
		// Decoder.More within an object is followed by a string name token.
		name := nameToken.(string)
		if len(name) > parser.limits.MaxScalarBytes {
			return jsonvalue.Value{}, parser.wrap("max_scalar_bytes", ErrLimitExceeded, nil)
		}
		if _, duplicate := names[name]; duplicate {
			return jsonvalue.Value{}, parser.wrap("duplicate_key", ErrDuplicateKey, nil)
		}
		names[name] = struct{}{}

		value, err := parser.value(depth + 1)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		members = append(members, jsonvalue.Member{Name: name, Value: value})
	}
	if _, err := parser.token(); err != nil {
		return jsonvalue.Value{}, err
	}
	// Every member value is valid and duplicate names were rejected above.
	value, _ := jsonvalue.Object(members)
	return value, nil
}

func (parser *jsonParser) array(depth int) (jsonvalue.Value, error) {
	elements := make([]jsonvalue.Value, 0)
	for parser.decoder.More() {
		if len(elements) >= parser.limits.MaxArrayItems {
			return jsonvalue.Value{}, parser.wrap("max_array_items", ErrLimitExceeded, nil)
		}
		value, err := parser.value(depth + 1)
		if err != nil {
			return jsonvalue.Value{}, err
		}
		elements = append(elements, value)
	}
	if _, err := parser.token(); err != nil {
		return jsonvalue.Value{}, err
	}
	// Every element was constructed by this parser and is therefore valid.
	value, _ := jsonvalue.Array(elements)
	return value, nil
}

func (parser *jsonParser) token() (json.Token, error) {
	if err := parser.ctx.Err(); err != nil {
		return nil, err
	}
	parser.tokens++
	if parser.tokens > parser.limits.MaxTokens {
		return nil, parser.wrap("max_tokens", ErrLimitExceeded, nil)
	}
	token, err := parser.decoder.Token()
	if err != nil {
		return nil, parser.wrap("malformed_json", ErrInvalidJSON, err)
	}
	return token, nil
}

func (parser *jsonParser) wrap(code string, kind error, cause error) error {
	return &Error{Code: code, Offset: parser.decoder.InputOffset(), Kind: kind, Cause: cause}
}

func validateLimits(limits Limits) error {
	if limits.MaxBytes <= 0 || limits.MaxBytes == math.MaxInt64 ||
		limits.MaxTokens <= 0 || limits.MaxDepth <= 0 ||
		limits.MaxObjectMembers <= 0 || limits.MaxArrayItems <= 0 ||
		limits.MaxScalarBytes <= 0 || limits.MaxTotalValues <= 0 {
		return &Error{Code: "invalid_limits", Kind: ErrInvalidLimits}
	}
	return nil
}
