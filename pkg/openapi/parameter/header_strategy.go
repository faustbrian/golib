package parameter

import (
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/http/httpguts"
)

var (
	// ErrInvalidHeaderValue reports malformed header advice input or bounds.
	ErrInvalidHeaderValue = errors.New("invalid header value")
	// ErrHeaderValueLimit reports header advice byte-bound exhaustion.
	ErrHeaderValueLimit = errors.New("header value limit exceeded")
)

// HeaderEncodingStrategy identifies an interoperable Header Object shape.
type HeaderEncodingStrategy uint8

const (
	// HeaderSchema uses the Header Object schema field.
	HeaderSchema HeaderEncodingStrategy = iota
	// HeaderTextPlainContent uses content with the text/plain media type.
	HeaderTextPlainContent
)

// RecommendHeaderEncoding selects text/plain content when a representative
// value contains header parameters or characters that are not URI-safe.
func RecommendHeaderEncoding(
	value string,
	maxBytes int,
) (HeaderEncodingStrategy, error) {
	if maxBytes < 1 || !utf8.ValidString(value) ||
		!httpguts.ValidHeaderFieldValue(value) {
		return HeaderSchema, ErrInvalidHeaderValue
	}
	if len(value) > maxBytes {
		return HeaderSchema, ErrHeaderValueLimit
	}
	if hasHeaderParameter(value) || !headerValueIsURISafe(value) {
		return HeaderTextPlainContent, nil
	}
	return HeaderSchema, nil
}

func hasHeaderParameter(value string) bool {
	quoted := false
	escaped := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if quoted {
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == '"' {
				quoted = false
			}
			continue
		}
		if character == '"' {
			quoted = true
			continue
		}
		if character != ';' {
			continue
		}
		start := index + 1
		for start < len(value) &&
			(value[start] == ' ' || value[start] == '\t') {
			start++
		}
		end := start
		for end < len(value) && value[end] != '=' &&
			value[end] != ' ' && value[end] != '\t' {
			end++
		}
		name := value[start:end]
		for end < len(value) && (value[end] == ' ' || value[end] == '\t') {
			end++
		}
		if end < len(value) && value[end] == '=' &&
			httpguts.ValidHeaderFieldName(name) {
			return true
		}
	}
	return false
}

func headerValueIsURISafe(value string) bool {
	const safe = "ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"abcdefghijklmnopqrstuvwxyz0123456789-._~:/?#[]@!$&'()*+,;="
	for index := 0; index < len(value); index++ {
		character := value[index]
		if strings.IndexByte(safe, character) >= 0 {
			continue
		}
		if character == '%' && index+2 < len(value) &&
			hexDigit(value[index+1]) && hexDigit(value[index+2]) {
			index += 2
			continue
		}
		return false
	}
	return true
}

func hexDigit(value byte) bool {
	return value >= '0' && value <= '9' ||
		value >= 'A' && value <= 'F' ||
		value >= 'a' && value <= 'f'
}
