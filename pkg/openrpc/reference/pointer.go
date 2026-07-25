// Package reference provides JSON Pointer, URI reference, and bounded OpenRPC
// reference-resolution primitives without implicit I/O.
package reference

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

var (
	// ErrInvalidPointer reports malformed RFC 6901 syntax or escaping.
	ErrInvalidPointer = errors.New("reference: invalid JSON Pointer")
	// ErrPointerLimit reports a pointer resource-policy violation.
	ErrPointerLimit = errors.New("reference: JSON Pointer limit exceeded")
	// ErrPointerPolicy reports non-positive pointer limits.
	ErrPointerPolicy = errors.New("reference: invalid JSON Pointer policy")
	// ErrPointerTarget reports a missing member, invalid array index, or scalar
	// traversal while evaluating a valid pointer.
	ErrPointerTarget = errors.New("reference: JSON Pointer target not found")
)

// PointerPolicy bounds pointer parsing and array-index work.
type PointerPolicy struct {
	MaxLength      int
	MaxTokens      int
	MaxIndexDigits int
}

// DefaultPointerPolicy returns finite bounds suitable for OpenRPC documents.
func DefaultPointerPolicy() PointerPolicy {
	return PointerPolicy{MaxLength: 16 << 10, MaxTokens: 256, MaxIndexDigits: 19}
}

// Pointer is an immutable RFC 6901 JSON Pointer.
type Pointer struct {
	tokens         []string
	maxIndexDigits int
}

// ParsePointer parses the plain JSON Pointer representation. The empty string
// identifies the complete document.
func ParsePointer(input string, policy PointerPolicy) (Pointer, error) {
	if policy.MaxLength <= 0 || policy.MaxTokens <= 0 || policy.MaxIndexDigits <= 0 {
		return Pointer{}, ErrPointerPolicy
	}
	if len(input) > policy.MaxLength {
		return Pointer{}, ErrPointerLimit
	}
	if input == "" {
		return Pointer{maxIndexDigits: policy.MaxIndexDigits}, nil
	}
	if !utf8.ValidString(input) || input[0] != '/' {
		return Pointer{}, ErrInvalidPointer
	}
	encoded := strings.Split(input[1:], "/")
	if len(encoded) > policy.MaxTokens {
		return Pointer{}, ErrPointerLimit
	}
	tokens := make([]string, len(encoded))
	for index, token := range encoded {
		decoded, err := decodeToken(token)
		if err != nil {
			return Pointer{}, err
		}
		tokens[index] = decoded
	}
	return Pointer{tokens: tokens, maxIndexDigits: policy.MaxIndexDigits}, nil
}

// ParseFragment parses the URI fragment representation of a JSON Pointer.
func ParseFragment(input string, policy PointerPolicy) (Pointer, error) {
	if input == "" || input[0] != '#' {
		return Pointer{}, ErrInvalidPointer
	}
	decoded, err := url.PathUnescape(input[1:])
	if err != nil || !utf8.ValidString(decoded) {
		return Pointer{}, ErrInvalidPointer
	}
	return ParsePointer(decoded, policy)
}

// Tokens returns an owned decoded-token snapshot.
func (pointer Pointer) Tokens() []string {
	return append([]string(nil), pointer.tokens...)
}

// String returns the canonical plain JSON Pointer representation.
func (pointer Pointer) String() string {
	if len(pointer.tokens) == 0 {
		return ""
	}
	encoded := make([]string, len(pointer.tokens))
	for index, token := range pointer.tokens {
		token = strings.ReplaceAll(token, "~", "~0")
		encoded[index] = strings.ReplaceAll(token, "/", "~1")
	}
	return "/" + strings.Join(encoded, "/")
}

// Evaluate resolves the pointer against one immutable JSON value. Returned
// target bytes retain the target value's exact number lexemes and member data.
func (pointer Pointer) Evaluate(document jsonvalue.Value, policy jsonvalue.Policy) (jsonvalue.Value, error) {
	if len(pointer.tokens) == 0 {
		return document, nil
	}
	current := document.Bytes()
	for _, token := range pointer.tokens {
		trimmed := bytes.TrimSpace(current)
		if len(trimmed) == 0 {
			return jsonvalue.Value{}, ErrPointerTarget
		}
		switch trimmed[0] {
		case '{':
			var object map[string]json.RawMessage
			_ = json.Unmarshal(trimmed, &object)
			next, ok := object[token]
			if !ok {
				return jsonvalue.Value{}, ErrPointerTarget
			}
			current = next
		case '[':
			index, err := pointer.arrayIndex(token)
			if err != nil {
				return jsonvalue.Value{}, err
			}
			var array []json.RawMessage
			_ = json.Unmarshal(trimmed, &array)
			if index >= len(array) {
				return jsonvalue.Value{}, ErrPointerTarget
			}
			current = array[index]
		default:
			return jsonvalue.Value{}, ErrPointerTarget
		}
	}
	target, err := jsonvalue.Parse(current, policy)
	if err != nil {
		return jsonvalue.Value{}, err
	}
	return target, nil
}

func (pointer Pointer) arrayIndex(token string) (int, error) {
	if token == "" || token == "-" ||
		(len(token) > 1 && token[0] == '0') {
		return 0, ErrPointerTarget
	}
	if len(token) > pointer.maxIndexDigits {
		return 0, ErrPointerLimit
	}
	for _, character := range token {
		if character < '0' || character > '9' {
			return 0, ErrPointerTarget
		}
	}
	index, err := strconv.Atoi(token)
	if err != nil {
		return 0, ErrPointerLimit
	}
	return index, nil
}

func decodeToken(token string) (string, error) {
	var decoded strings.Builder
	decoded.Grow(len(token))
	for index := 0; index < len(token); index++ {
		if token[index] != '~' {
			decoded.WriteByte(token[index])
			continue
		}
		if index+1 >= len(token) {
			return "", ErrInvalidPointer
		}
		index++
		switch token[index] {
		case '0':
			decoded.WriteByte('~')
		case '1':
			decoded.WriteByte('/')
		default:
			return "", ErrInvalidPointer
		}
	}
	return decoded.String(), nil
}
