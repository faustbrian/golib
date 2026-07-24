// Package jsonvalue preserves arbitrary JSON values without numeric coercion,
// key reordering, or shared mutable byte storage.
package jsonvalue

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

var (
	// ErrInvalidPolicy reports a non-positive resource limit.
	ErrInvalidPolicy = errors.New("jsonvalue: invalid resource policy")
	// ErrByteLimit reports input larger than the configured byte bound.
	ErrByteLimit = errors.New("jsonvalue: byte limit exceeded")
	// ErrDepthLimit reports nesting deeper than the configured bound.
	ErrDepthLimit = errors.New("jsonvalue: depth limit exceeded")
	// ErrTokenLimit reports more JSON tokens than the configured bound.
	ErrTokenLimit = errors.New("jsonvalue: token limit exceeded")
	// ErrInvalidUTF8 reports JSON text that is not valid UTF-8.
	ErrInvalidUTF8 = errors.New("jsonvalue: invalid UTF-8")
	// ErrDuplicateName reports an ambiguous duplicate object member name.
	ErrDuplicateName = errors.New("jsonvalue: duplicate object member")
	// ErrTrailingData reports another JSON value after the first value.
	ErrTrailingData = errors.New("jsonvalue: trailing JSON data")
	// ErrInvalidJSON reports malformed JSON syntax or an invalid zero Value.
	ErrInvalidJSON = errors.New("jsonvalue: invalid JSON")
)

// Policy bounds work performed while validating a JSON value.
type Policy struct {
	MaxBytes  int
	MaxDepth  int
	MaxTokens int
}

// DefaultPolicy returns conservative bounds suitable for document fields and
// complete Draft 7 schemas. Callers handling smaller inputs should lower them.
func DefaultPolicy() Policy {
	return Policy{
		MaxBytes:  16 << 20,
		MaxDepth:  256,
		MaxTokens: 2_000_000,
	}
}

// Value is an immutable, syntactically valid JSON value. It retains the exact
// validated bytes, including insignificant whitespace, object order, and
// number lexemes.
type Value struct {
	raw []byte
}

// Parse validates one JSON value under policy and takes an owned copy of the
// exact input. Object member names must be unique.
func Parse(input []byte, policy Policy) (Value, error) {
	if policy.MaxBytes <= 0 || policy.MaxDepth <= 0 || policy.MaxTokens <= 0 {
		return Value{}, ErrInvalidPolicy
	}
	if len(input) > policy.MaxBytes {
		return Value{}, ErrByteLimit
	}
	if !utf8.Valid(input) {
		return Value{}, ErrInvalidUTF8
	}

	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	parser := tokenParser{decoder: decoder, policy: policy}
	if err := parser.readValue(0); err != nil {
		return Value{}, err
	}

	_, err := decoder.Token()
	if err == nil {
		return Value{}, ErrTrailingData
	}
	if !errors.Is(err, io.EOF) {
		return Value{}, ErrInvalidJSON
	}

	return Value{raw: append([]byte(nil), input...)}, nil
}

// Bytes returns an owned copy of the exact JSON input.
func (value Value) Bytes() []byte {
	return append([]byte(nil), value.raw...)
}

// MarshalJSON implements json.Marshaler without exposing internal storage.
func (value Value) MarshalJSON() ([]byte, error) {
	if len(value.raw) == 0 {
		return nil, ErrInvalidJSON
	}
	return value.Bytes(), nil
}

type tokenParser struct {
	decoder *json.Decoder
	policy  Policy
	tokens  int
}

func (parser *tokenParser) readValue(depth int) error {
	token, err := parser.nextToken()
	if err != nil {
		return err
	}

	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}

	if delimiter == '{' {
		return parser.readObject(depth + 1)
	}
	// At a value position encoding/json only returns an opening object or
	// array delimiter.
	return parser.readArray(depth + 1)
}

func (parser *tokenParser) readObject(depth int) error {
	if depth > parser.policy.MaxDepth {
		return ErrDepthLimit
	}

	names := make(map[string]struct{})
	for parser.decoder.More() {
		token, err := parser.nextToken()
		if err != nil {
			return err
		}
		// encoding/json guarantees object-name tokens are strings while More
		// reports another member.
		name := token.(string)
		if _, duplicate := names[name]; duplicate {
			return ErrDuplicateName
		}
		names[name] = struct{}{}

		if err := parser.readValue(depth); err != nil {
			return err
		}
	}

	return parser.readClosing()
}

func (parser *tokenParser) readArray(depth int) error {
	if depth > parser.policy.MaxDepth {
		return ErrDepthLimit
	}
	for parser.decoder.More() {
		if err := parser.readValue(depth); err != nil {
			return err
		}
	}
	return parser.readClosing()
}

func (parser *tokenParser) readClosing() error {
	// encoding/json guarantees the token following More()==false is the
	// matching closing delimiter.
	_, err := parser.nextToken()
	return err
}

func (parser *tokenParser) nextToken() (json.Token, error) {
	parser.tokens++
	if parser.tokens > parser.policy.MaxTokens {
		return nil, ErrTokenLimit
	}
	token, err := parser.decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("%w", ErrInvalidJSON)
	}
	return token, nil
}
