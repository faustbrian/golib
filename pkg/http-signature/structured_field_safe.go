package httpsignature

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/dunglas/httpsfv"
)

var (
	errStructuredFieldParse   = errors.New("structured field parser rejected input")
	errStructuredFieldRFC8941 = errors.New("structured field value is not RFC 8941")
)

// The Structured Fields dependency has historically panicked on some malformed
// extension syntax. These narrow boundaries convert dependency panics from
// untrusted HTTP fields into ordinary parse failures without hiding panics in
// this package's own semantic validation.
func unmarshalStructuredDictionary(values []string) (dictionary *httpsfv.Dictionary, err error) {
	defer func() {
		if recover() != nil {
			dictionary, err = nil, errStructuredFieldParse
		}
	}()
	combined := normalizeStructuredFieldOWS(combineStructuredFieldLines(values))
	return httpsfv.UnmarshalDictionary(normalizeRFC8941BinaryValues(combined))
}

func unmarshalStructuredList(values []string) (list httpsfv.List, err error) {
	defer func() {
		if recover() != nil {
			list, err = nil, errStructuredFieldParse
		}
	}()
	combined := normalizeStructuredFieldOWS(combineStructuredFieldLines(values))
	return httpsfv.UnmarshalList(normalizeRFC8941BinaryValues(combined))
}

func unmarshalStructuredItem(values []string) (item httpsfv.Item, err error) {
	defer func() {
		if recover() != nil {
			item, err = httpsfv.Item{}, errStructuredFieldParse
		}
	}()
	combined := normalizeStructuredFieldOWS(combineStructuredFieldLines(values))
	return httpsfv.UnmarshalItem(normalizeRFC8941BinaryValues(combined))
}

// RFC 8941 Section 4.2 requires multiple field lines to be combined by
// appending a comma and one space before parsing. Supplying the lines directly
// to httpsfv is not equivalent: that dependency joins them with a bare comma,
// which changes values when a line boundary occurs inside a quoted string.
func combineStructuredFieldLines(values []string) []string {
	switch len(values) {
	case 0, 1:
		return values
	default:
		return []string{strings.Join(values, ", ")}
	}
}

// RFC 8941 Section 4.2.7 requires parsers to synthesize absent base64 padding
// and recommends accepting non-zero pad bits. httpsfv v1.1.0 delegates only to
// padded base64 decoding, so canonicalize byte-sequence bare items before
// parsing. The boundary check prevents token and String contents from being
// reinterpreted as byte sequences.
func normalizeRFC8941BinaryValues(values []string) []string {
	var normalized []string
	for index, value := range values {
		normalizedValue := normalizeRFC8941BinaryValue(value)
		if normalizedValue != value {
			if normalized == nil {
				normalized = append([]string(nil), values...)
			}
			normalized[index] = normalizedValue
		}
	}
	if normalized == nil {
		return values
	}

	return normalized
}

func normalizeRFC8941BinaryValue(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	segmentStart := 0
	quoted := false
	escaped := false
	changed := false
	for index := 0; index < len(value); index++ {
		character := value[index]
		if quoted {
			switch {
			case escaped:
				escaped = false
			case character == '\\':
				escaped = true
			case character == '"':
				quoted = false
			}
		} else if character == '"' {
			quoted = true
		} else if character == ':' && isRFC8941BareItemBoundary(value, index) {
			closingOffset := strings.IndexByte(value[index+1:], ':')
			switch closingOffset {
			case -1:
			default:
				closing := index + 1 + closingOffset
				encoded := value[index+1 : closing]
				unpadded := strings.TrimRight(encoded, "=")
				if !strings.ContainsRune(unpadded, '=') && validRFC8941Base64Alphabet(unpadded) {
					decoded, err := base64.RawStdEncoding.DecodeString(unpadded)
					if err == nil {
						canonical := base64.StdEncoding.EncodeToString(decoded)
						if len(encoded) <= len(canonical) && canonical != encoded {
							result.WriteString(value[segmentStart : index+1])
							result.WriteString(canonical)
							result.WriteByte(':')
							segmentStart = closing + 1
							changed = true
						}
					}
				}
				index = closing
			}
		}
	}
	if !changed {
		return value
	}
	result.WriteString(value[segmentStart:])

	return result.String()
}

func validRFC8941Base64Alphabet(value string) bool {
	for index := range len(value) {
		character := value[index]
		switch {
		case character >= 'A' && character <= 'Z':
		case character >= 'a' && character <= 'z':
		case character >= '0' && character <= '9':
		case character == '+', character == '/':
		default:
			return false
		}
	}
	return true
}

func isRFC8941BareItemBoundary(value string, index int) bool {
	if index == 0 {
		return true
	}

	return strings.ContainsRune(" \t=(,", rune(value[index-1]))
}

func marshalRFC8941(value httpsfv.StructuredFieldValue) (string, error) {
	if !isRFC8941StructuredField(value) {
		return "", errStructuredFieldRFC8941
	}
	serialized, err := httpsfv.Marshal(value)
	if err != nil {
		return "", err
	}

	return restoreRFC8941IntegralDecimals(serialized), nil
}

// httpsfv v1.1.0 rounds through binary floating point before deciding whether
// a decimal is integral. Some exact integral decimals therefore serialize with
// an empty fractional part ("1.") instead of RFC 8941's required "1.0". The
// dependency has already validated and serialized the typed model here; this
// scanner repairs only an empty fraction at a numeric bare-item boundary and
// deliberately skips quoted strings and token text.
func restoreRFC8941IntegralDecimals(serialized string) string {
	var result strings.Builder
	result.Grow(len(serialized))
	quoted := false
	escaped := false
	for index := 0; index < len(serialized); index++ {
		character := serialized[index]
		if quoted {
			result.WriteByte(character)
			switch {
			case escaped:
				escaped = false
			case character == '\\':
				escaped = true
			case character == '"':
				quoted = false
			}
			continue
		}
		if character == '"' {
			quoted = true
			result.WriteByte(character)
			continue
		}
		result.WriteByte(character)
		if character == '.' && isEmptyRFC8941DecimalFraction(serialized, index) {
			result.WriteByte('0')
		}
	}

	return result.String()
}

func isEmptyRFC8941DecimalFraction(value string, decimalPoint int) bool {
	if decimalPoint == 0 || value[decimalPoint-1] < '0' || value[decimalPoint-1] > '9' {
		return false
	}
	if decimalPoint+1 < len(value) && !strings.ContainsRune("; ,)", rune(value[decimalPoint+1])) {
		return false
	}

	start := decimalPoint
	for start > 0 && value[start-1] >= '0' && value[start-1] <= '9' {
		start--
	}
	if start > 0 && value[start-1] == '-' {
		start--
	}
	return start == 0 || strings.ContainsRune("=(, ", rune(value[start-1]))
}
