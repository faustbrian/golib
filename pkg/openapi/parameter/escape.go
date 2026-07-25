package parameter

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	// ErrInvalidDelimiterEscape reports an unsupported style or invalid input.
	ErrInvalidDelimiterEscape = errors.New("invalid parameter delimiter escape")
	// ErrDelimiterEscapeLimit reports delimiter escape byte-bound exhaustion.
	ErrDelimiterEscapeLimit = errors.New("parameter delimiter escape limit exceeded")
)

// EscapeAmbiguousDelimiters applies a reversible package convention before
// OpenAPI serialization. Literal delimiters and percent signs become percent
// triplets which the regular serializer subsequently percent-encodes again.
func EscapeAmbiguousDelimiters(
	value string,
	style Style,
	maxBytes int,
) (string, error) {
	delimiters, valid := ambiguousDelimiterCodes(style)
	if !valid || maxBytes < 1 || !utf8.ValidString(value) {
		return "", ErrInvalidDelimiterEscape
	}
	if len(value) > maxBytes {
		return "", ErrDelimiterEscapeLimit
	}
	var output strings.Builder
	output.Grow(len(value))
	for index := 0; index < len(value); index++ {
		character := value[index]
		code, escape := delimiters[character]
		if character == '%' {
			code, escape = "25", true
		}
		if escape {
			if maxBytes-output.Len() < 3 {
				return "", ErrDelimiterEscapeLimit
			}
			output.WriteByte('%')
			output.WriteString(code)
			continue
		}
		if output.Len() >= maxBytes {
			return "", ErrDelimiterEscapeLimit
		}
		output.WriteByte(character)
	}
	return output.String(), nil
}

// UnescapeAmbiguousDelimiters reverses EscapeAmbiguousDelimiters after the
// regular OpenAPI decoding step. Unrecognized percent triplets remain intact.
func UnescapeAmbiguousDelimiters(
	value string,
	style Style,
	maxBytes int,
) (string, error) {
	delimiters, valid := ambiguousDelimiterCodes(style)
	if !valid || maxBytes < 1 || !utf8.ValidString(value) {
		return "", ErrInvalidDelimiterEscape
	}
	if len(value) > maxBytes {
		return "", ErrDelimiterEscapeLimit
	}
	reverse := make(map[string]byte)
	for character, code := range delimiters {
		reverse[code] = character
	}
	reverse["25"] = '%'
	var output strings.Builder
	output.Grow(len(value))
	for index := 0; index < len(value); {
		if value[index] == '%' && index+2 < len(value) {
			code := strings.ToUpper(value[index+1 : index+3])
			if character, escaped := reverse[code]; escaped {
				output.WriteByte(character)
				index += 3
				continue
			}
		}
		output.WriteByte(value[index])
		index++
	}
	return output.String(), nil
}

func ambiguousDelimiterCodes(style Style) (map[byte]string, bool) {
	switch style {
	case SpaceDelimited:
		return map[byte]string{' ': "20"}, true
	case PipeDelimited:
		return map[byte]string{'|': "7C"}, true
	case DeepObject:
		return map[byte]string{'[': "5B", ']': "5D"}, true
	default:
		return nil, false
	}
}
