package reference

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

var (
	// ErrInvalidReference reports a malformed or empty URI reference.
	ErrInvalidReference = errors.New("reference: invalid URI reference")
	// ErrReferenceLimit reports a URI-reference resource-policy violation.
	ErrReferenceLimit = errors.New("reference: URI reference limit exceeded")
	// ErrReferencePolicy reports non-positive URI-reference limits.
	ErrReferencePolicy = errors.New("reference: invalid URI reference policy")
	// ErrInvalidBase reports a malformed or non-absolute resolution base.
	ErrInvalidBase = errors.New("reference: invalid resolution base")
)

// Policy bounds URI-reference parsing.
type Policy struct {
	MaxLength int
}

// DefaultPolicy returns a finite URI-reference bound suitable for OpenRPC
// documents.
func DefaultPolicy() Policy {
	return Policy{MaxLength: 16 << 10}
}

// Kind describes whether a URI reference stays within its containing document
// or names another document with a relative or absolute URI.
type Kind uint8

const (
	// Internal identifies a fragment-only reference.
	Internal Kind = 1
	// ExternalRelative identifies a relative reference to another document.
	ExternalRelative Kind = 2
	// ExternalAbsolute identifies an absolute reference to another document.
	ExternalAbsolute Kind = 3
)

// Reference is an immutable, bounded URI reference.
type Reference struct {
	raw         string
	uri         *url.URL
	fragment    string
	hasFragment bool
	kind        Kind
	policy      Policy
}

// Parse validates and classifies one URI reference without performing I/O.
func Parse(input string, policy Policy) (Reference, error) {
	if policy.MaxLength <= 0 {
		return Reference{}, ErrReferencePolicy
	}
	if len(input) > policy.MaxLength {
		return Reference{}, ErrReferenceLimit
	}
	if input == "" || !utf8.ValidString(input) || containsURIControl(input) {
		return Reference{}, ErrInvalidReference
	}
	parsed, err := url.Parse(input)
	if err != nil {
		return Reference{}, ErrInvalidReference
	}

	fragment, hasFragment := "", false
	if delimiter := strings.IndexByte(input, '#'); delimiter >= 0 {
		fragment, hasFragment = input[delimiter+1:], true
	}
	kind := ExternalRelative
	if input[0] == '#' {
		kind = Internal
	} else if parsed.IsAbs() {
		kind = ExternalAbsolute
	}

	return Reference{
		raw:         input,
		uri:         parsed,
		fragment:    fragment,
		hasFragment: hasFragment,
		kind:        kind,
		policy:      policy,
	}, nil
}

// Kind returns the reference classification.
func (reference Reference) Kind() Kind {
	return reference.kind
}

// String returns the original URI-reference spelling.
func (reference Reference) String() string {
	return reference.raw
}

// ResolveAgainst applies RFC 3986 reference resolution to an absolute base.
// It does not retrieve the resulting resource.
func (reference Reference) ResolveAgainst(base string) (Reference, error) {
	if reference.uri == nil {
		return Reference{}, ErrInvalidReference
	}
	if base == "" || !utf8.ValidString(base) || containsURIControl(base) {
		return Reference{}, ErrInvalidBase
	}
	parsedBase, err := url.Parse(base)
	if err != nil || !parsedBase.IsAbs() {
		return Reference{}, ErrInvalidBase
	}
	parsedBase.Fragment = ""
	parsedBase.RawFragment = ""
	resolved := parsedBase.ResolveReference(reference.uri)

	result, err := Parse(resolved.String(), reference.policy)
	if err != nil {
		return Reference{}, fmt.Errorf("%w: resolved reference", err)
	}
	return result, nil
}

// TargetPointer parses the reference fragment as an RFC 6901 JSON Pointer.
// A reference without a fragment and an empty fragment both identify the root.
func (reference Reference) TargetPointer(policy PointerPolicy) (Pointer, error) {
	if reference.uri == nil {
		return Pointer{}, ErrInvalidReference
	}
	if !reference.hasFragment || reference.fragment == "" {
		return ParsePointer("", policy)
	}
	pointer, err := ParseFragment("#"+reference.fragment, policy)
	if err != nil {
		return Pointer{}, fmt.Errorf("%w: fragment is not a JSON Pointer", ErrInvalidReference)
	}
	return pointer, nil
}

func containsURIControl(input string) bool {
	for _, character := range input {
		if character <= 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}
