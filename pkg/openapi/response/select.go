// Package response selects response definitions for concrete HTTP statuses.
package response

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

var (
	// ErrInvalidResponses reports a non-object Responses value.
	ErrInvalidResponses = errors.New("invalid Responses Object")
	// ErrInvalidStatus reports a status outside the OpenAPI HTTP status range.
	ErrInvalidStatus = errors.New("invalid HTTP response status")
	// ErrInvalidResponseHeaders reports invalid Response Object header inputs.
	ErrInvalidResponseHeaders = errors.New("invalid response headers")
	// ErrResponseHeaderLimit reports a response header-count bound.
	ErrResponseHeaderLimit = errors.New("response header limit exceeded")
	// ErrInvalidSetCookieValues reports invalid Set-Cookie field values or
	// bounds.
	ErrInvalidSetCookieValues = errors.New("invalid Set-Cookie values")
	// ErrSetCookieLimit reports a Set-Cookie value-count or byte bound.
	ErrSetCookieLimit = errors.New("Set-Cookie value limit exceeded")
	// ErrInvalidLinksetHeader reports an application/linkset document that
	// cannot be represented safely as an HTTP field value.
	ErrInvalidLinksetHeader = errors.New("invalid linkset header value")
	// ErrLinksetHeaderLimit reports an application/linkset byte bound.
	ErrLinksetHeaderLimit = errors.New("linkset header value limit exceeded")
)

// MatchKind identifies the response-key class selected for a status.
type MatchKind string

const (
	// MatchExact identifies an exact three-digit response key.
	MatchExact MatchKind = "exact"
	// MatchRange identifies an uppercase wildcard response key such as 2XX.
	MatchRange MatchKind = "range"
	// MatchDefault identifies the default response key.
	MatchDefault MatchKind = "default"
)

// Match is one immutable response selection.
type Match struct {
	Key   string
	Kind  MatchKind
	Value jsonvalue.Value
}

// Header retains one applicable Header or Reference Object.
type Header struct {
	Name  string
	Value jsonvalue.Value
}

// Select returns the response for status using exact, range, then default
// precedence. It performs no reference resolution or I/O.
func Select(responses jsonvalue.Value, status int) (Match, bool, error) {
	if responses.Kind() != jsonvalue.ObjectKind {
		return Match{}, false, ErrInvalidResponses
	}
	if status < 100 || status > 599 {
		return Match{}, false, fmt.Errorf("%w: %d", ErrInvalidStatus, status)
	}
	for _, candidate := range []struct {
		key  string
		kind MatchKind
	}{
		{key: strconv.Itoa(status), kind: MatchExact},
		{key: fmt.Sprintf("%dXX", status/100), kind: MatchRange},
		{key: "default", kind: MatchDefault},
	} {
		value, exists := responses.Lookup(candidate.key)
		if exists {
			return Match{
				Key: candidate.key, Kind: candidate.kind, Value: value,
			}, true, nil
		}
	}
	return Match{}, false, nil
}

// Headers returns bounded Response Object headers while ignoring Content-Type
// case-insensitively as required by OpenAPI and HTTP field-name semantics.
func Headers(value jsonvalue.Value, maxHeaders int) ([]Header, error) {
	if value.Kind() != jsonvalue.ObjectKind || maxHeaders < 1 {
		return nil, ErrInvalidResponseHeaders
	}
	headers, exists := value.Lookup("headers")
	if !exists {
		return nil, nil
	}
	members, valid := headers.Members()
	if !valid {
		return nil, ErrInvalidResponseHeaders
	}
	result := make([]Header, 0, min(len(members), maxHeaders))
	for _, member := range members {
		if strings.EqualFold(member.Name, "Content-Type") {
			continue
		}
		if member.Value.Kind() != jsonvalue.ObjectKind {
			return nil, ErrInvalidResponseHeaders
		}
		if len(result) >= maxHeaders {
			return nil, ErrResponseHeaderLimit
		}
		result = append(result, Header{Name: member.Name, Value: member.Value})
	}
	return result, nil
}

// SetCookieValues returns independent, pre-encoded Set-Cookie field values.
// It neither adds the field name nor performs percent, base64, or other
// escaping. Each returned slice element must be emitted as a separate field.
func SetCookieValues(
	values []string,
	maxValues int,
	maxBytes int,
) ([]string, error) {
	if maxValues < 1 || maxBytes < 1 {
		return nil, ErrInvalidSetCookieValues
	}
	if len(values) > maxValues {
		return nil, ErrSetCookieLimit
	}
	result := make([]string, 0, len(values))
	total := 0
	for _, value := range values {
		if !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n") {
			return nil, ErrInvalidSetCookieValues
		}
		if len(value) > maxBytes-total {
			return nil, ErrSetCookieLimit
		}
		total += len(value)
		result = append(result, value)
	}
	return result, nil
}

// LinksetHeaderValue converts a bounded application/linkset document to the
// HTTP Link field-value form required when OpenAPI uses that media type in a
// Header Object. RFC 9264 permits document newlines but requires their removal
// or replacement before the value is emitted as an HTTP field.
func LinksetHeaderValue(document []byte, maxBytes int) (string, error) {
	if maxBytes < 1 {
		return "", ErrInvalidLinksetHeader
	}
	if len(document) > maxBytes {
		return "", ErrLinksetHeaderLimit
	}
	var value strings.Builder
	value.Grow(len(document))
	for index := 0; index < len(document); index++ {
		character := document[index]
		if character == '\r' {
			value.WriteByte(' ')
			continue
		}
		if character == '\n' {
			if index > 0 && document[index-1] == '\r' {
				continue
			}
			value.WriteByte(' ')
			continue
		}
		if character != '\t' && (character < 0x20 || character > 0x7e) {
			return "", ErrInvalidLinksetHeader
		}
		value.WriteByte(character)
	}
	return value.String(), nil
}
