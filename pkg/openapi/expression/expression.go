// Package expression parses and evaluates OpenAPI runtime expressions and
// embedded callback or link expression templates against caller-owned data.
package expression

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/openapi/reference"
)

// ErrInvalid reports text outside the normative runtime-expression grammar.
var ErrInvalid = errors.New("invalid OpenAPI runtime expression")

// Kind identifies the top-level runtime value.
type Kind uint8

const (
	// UnknownKind identifies the zero Expression.
	UnknownKind Kind = iota
	// URL identifies $url.
	URL
	// Method identifies $method.
	Method
	// StatusCode identifies $statusCode.
	StatusCode
	// Request identifies a $request source.
	Request
	// Response identifies a $response source.
	Response
)

// Source identifies a request or response message location.
type Source uint8

const (
	// UnknownSource identifies an expression without a message source.
	UnknownSource Source = iota
	// Header identifies a named HTTP field.
	Header
	// Query identifies a query parameter.
	Query
	// Path identifies a path parameter.
	Path
	// Body identifies the complete body or a JSON Pointer within it.
	Body
)

// Expression is one immutable parsed runtime expression.
type Expression struct {
	raw     string
	kind    Kind
	source  Source
	name    string
	pointer reference.Pointer
}

// Parse parses one complete runtime expression.
func Parse(raw string) (Expression, error) {
	switch raw {
	case "$url":
		return Expression{raw: raw, kind: URL}, nil
	case "$method":
		return Expression{raw: raw, kind: Method}, nil
	case "$statusCode":
		return Expression{raw: raw, kind: StatusCode}, nil
	}
	for prefix, kind := range map[string]Kind{
		"$request.":  Request,
		"$response.": Response,
	} {
		if strings.HasPrefix(raw, prefix) {
			return parseSource(raw, strings.TrimPrefix(raw, prefix), kind)
		}
	}
	return Expression{}, fmt.Errorf("%w: unknown expression", ErrInvalid)
}

func parseSource(raw string, source string, kind Kind) (Expression, error) {
	for prefix, location := range map[string]Source{
		"header.": Header,
		"query.":  Query,
		"path.":   Path,
	} {
		if !strings.HasPrefix(source, prefix) {
			continue
		}
		name := strings.TrimPrefix(source, prefix)
		if location == Header {
			if !validHTTPToken(name) {
				return Expression{}, fmt.Errorf("%w: invalid header token", ErrInvalid)
			}
		} else if !validName(name) {
			return Expression{}, fmt.Errorf("%w: invalid parameter name", ErrInvalid)
		}
		return Expression{
			raw: raw, kind: kind, source: location, name: name,
		}, nil
	}
	if source == "body" {
		return Expression{raw: raw, kind: kind, source: Body}, nil
	}
	if strings.HasPrefix(source, "body#") {
		pointer, err := reference.ParsePointer(strings.TrimPrefix(source, "body#"))
		if err != nil {
			return Expression{}, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		return Expression{
			raw: raw, kind: kind, source: Body, pointer: pointer,
		}, nil
	}
	return Expression{}, fmt.Errorf("%w: unknown message source", ErrInvalid)
}

func validHTTPToken(value string) bool {
	if value == "" {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if (character >= '0' && character <= '9') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character)) {
			continue
		}
		return false
	}
	return true
}

func validName(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if character < 0x20 {
			return false
		}
	}
	return true
}

// Kind returns the top-level runtime value kind.
func (expression Expression) Kind() Kind {
	return expression.kind
}

// Source returns the request or response message location.
func (expression Expression) Source() Source {
	return expression.source
}

// Name returns the decoded header or parameter name.
func (expression Expression) Name() string {
	return expression.name
}

// Pointer returns the body JSON Pointer, whose zero value selects the body root.
func (expression Expression) Pointer() reference.Pointer {
	return expression.pointer
}

// String returns the exact parsed expression.
func (expression Expression) String() string {
	return expression.raw
}

// Part is either literal template text or one parsed expression.
type Part struct {
	literal    string
	expression Expression
	dynamic    bool
}

// Literal returns literal text or an empty string for an expression part.
func (part Part) Literal() string {
	return part.literal
}

// Expression returns the expression or its zero value for a literal part.
func (part Part) Expression() Expression {
	return part.expression
}

// Dynamic reports whether this part is an embedded expression.
func (part Part) Dynamic() bool {
	return part.dynamic
}

// Template is an immutable sequence of literal and expression parts.
type Template struct {
	parts []Part
}

// ParseTemplate parses expressions embedded between curly braces.
func ParseTemplate(raw string) (Template, error) {
	var parts []Part
	remaining := raw
	for remaining != "" {
		firstBrace := strings.IndexAny(remaining, "{}")
		if firstBrace >= 0 && remaining[firstBrace] == '}' {
			return Template{}, fmt.Errorf("%w: unmatched closing brace", ErrInvalid)
		}
		opening := strings.IndexByte(remaining, '{')
		if opening < 0 {
			parts = append(parts, Part{literal: remaining})
			break
		}
		if opening > 0 {
			parts = append(parts, Part{literal: remaining[:opening]})
		}
		remaining = remaining[opening+1:]
		closing := strings.IndexByte(remaining, '}')
		if closing == -1 {
			return Template{}, fmt.Errorf("%w: unmatched opening brace", ErrInvalid)
		}
		candidate := remaining[:closing]
		if candidate == "" || strings.ContainsAny(candidate, "{}") {
			return Template{}, fmt.Errorf("%w: malformed embedding", ErrInvalid)
		}
		parsed, err := Parse(candidate)
		if err != nil {
			return Template{}, err
		}
		parts = append(parts, Part{expression: parsed, dynamic: true})
		remaining = remaining[closing+1:]
	}
	return Template{parts: parts}, nil
}

// Parts returns a caller-owned copy in source order.
func (template Template) Parts() []Part {
	return append([]Part(nil), template.parts...)
}
