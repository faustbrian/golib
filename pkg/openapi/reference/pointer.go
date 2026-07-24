// Package reference implements bounded OpenAPI URI-reference and JSON Pointer
// resolution without implicit external input/output.
package reference

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

// ErrInvalidPointer reports malformed RFC 6901 syntax.
var ErrInvalidPointer = errors.New("invalid JSON Pointer")

// ErrTargetNotFound reports a valid pointer that does not identify a value.
var ErrTargetNotFound = errors.New("JSON Pointer target not found")

// ErrInvalidFragment reports malformed URI-fragment encoding.
var ErrInvalidFragment = errors.New("invalid reference fragment")

// Pointer is an immutable RFC 6901 JSON Pointer. Its zero value identifies the
// root value.
type Pointer struct {
	tokens []string
}

// ParsePointer parses the plain JSON Pointer representation.
func ParsePointer(value string) (Pointer, error) {
	if value == "" {
		return Pointer{}, nil
	}
	if value[0] != '/' {
		return Pointer{}, fmt.Errorf("%w: non-empty pointer must start with slash", ErrInvalidPointer)
	}
	rawTokens := strings.Split(value[1:], "/")
	tokens := make([]string, len(rawTokens))
	for index, raw := range rawTokens {
		decoded, err := decodePointerToken(raw)
		if err != nil {
			return Pointer{}, fmt.Errorf("%w: token %d: %v", ErrInvalidPointer, index, err)
		}
		tokens[index] = decoded
	}
	return Pointer{tokens: tokens}, nil
}

// Tokens returns a caller-owned decoded token sequence.
func (pointer Pointer) Tokens() []string {
	return append([]string(nil), pointer.tokens...)
}

// String returns the canonical plain JSON Pointer representation.
func (pointer Pointer) String() string {
	if len(pointer.tokens) == 0 {
		return ""
	}
	var result strings.Builder
	for _, token := range pointer.tokens {
		result.WriteByte('/')
		result.WriteString(strings.NewReplacer("~", "~0", "/", "~1").Replace(token))
	}
	return result.String()
}

// Evaluate resolves the pointer against an immutable JSON semantic value.
func (pointer Pointer) Evaluate(root jsonvalue.Value) (jsonvalue.Value, error) {
	current := root
	for index, token := range pointer.tokens {
		switch current.Kind() {
		case jsonvalue.ObjectKind:
			child, exists := current.Lookup(token)
			if !exists {
				return jsonvalue.Value{}, targetError(index)
			}
			current = child
		case jsonvalue.ArrayKind:
			position, ok := arrayIndex(token)
			if !ok {
				return jsonvalue.Value{}, targetError(index)
			}
			elements, _ := current.Elements()
			if position >= len(elements) {
				return jsonvalue.Value{}, targetError(index)
			}
			current = elements[position]
		default:
			return jsonvalue.Value{}, targetError(index)
		}
	}
	return current, nil
}

func decodePointerToken(value string) (string, error) {
	var result strings.Builder
	for index := 0; index < len(value); index++ {
		if value[index] != '~' {
			result.WriteByte(value[index])
			continue
		}
		if index+1 >= len(value) {
			return "", errors.New("trailing tilde escape")
		}
		index++
		switch value[index] {
		case '0':
			result.WriteByte('~')
		case '1':
			result.WriteByte('/')
		default:
			return "", errors.New("unknown tilde escape")
		}
	}
	return result.String(), nil
}

func arrayIndex(token string) (int, bool) {
	if token == "" || token == "-" || (len(token) > 1 && token[0] == '0') {
		return 0, false
	}
	for _, character := range token {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	index, err := strconv.ParseUint(token, 10, strconv.IntSize)
	if err != nil || index > uint64(^uint(0)>>1) {
		return 0, false
	}
	return int(index), true
}

func targetError(token int) error {
	return fmt.Errorf("%w at token %d", ErrTargetNotFound, token)
}

// FragmentKind identifies the interpretation of a decoded URI fragment.
type FragmentKind uint8

const (
	// FragmentRoot identifies an absent or empty fragment.
	FragmentRoot FragmentKind = iota
	// FragmentPointer identifies a JSON Pointer fragment.
	FragmentPointer
	// FragmentAnchor identifies a plain-name anchor fragment.
	FragmentAnchor
)

// Fragment is an immutable decoded URI fragment.
type Fragment struct {
	kind    FragmentKind
	pointer Pointer
	anchor  string
}

// ParseFragment percent-decodes and classifies a URI fragment without the '#'.
func ParseFragment(raw string) (Fragment, error) {
	return parseFragment(raw, 0)
}

func parseFragment(raw string, maximumPointerTokens int) (Fragment, error) {
	decoded, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(decoded) {
		return Fragment{}, fmt.Errorf("%w: malformed percent encoding", ErrInvalidFragment)
	}
	if decoded == "" {
		return Fragment{kind: FragmentRoot}, nil
	}
	if decoded[0] != '/' {
		return Fragment{kind: FragmentAnchor, anchor: decoded}, nil
	}
	if maximumPointerTokens > 0 &&
		strings.Count(decoded, "/") > maximumPointerTokens {
		return Fragment{}, fmt.Errorf("%w: pointer tokens", ErrLimitExceeded)
	}
	pointer, err := ParsePointer(decoded)
	if err != nil {
		return Fragment{}, fmt.Errorf("%w: %v", ErrInvalidFragment, err)
	}
	return Fragment{kind: FragmentPointer, pointer: pointer}, nil
}

// Kind returns the fragment interpretation.
func (fragment Fragment) Kind() FragmentKind {
	return fragment.kind
}

// Pointer returns the parsed pointer or the root pointer for other kinds.
func (fragment Fragment) Pointer() Pointer {
	return fragment.pointer
}

// Anchor returns the decoded anchor or an empty string for other kinds.
func (fragment Fragment) Anchor() string {
	return fragment.anchor
}
