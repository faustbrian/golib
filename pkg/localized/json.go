package localized

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/international/locale"
)

// JSONMode controls legacy compatibility during decoding.
type JSONMode uint8

const (
	// StrictJSON accepts only a JSON object with strict BCP 47 keys.
	StrictJSON JSONMode = iota
	// PermissiveJSON additionally accepts null and underscore locale separators.
	PermissiveJSON
)

const defaultMaxJSONBytes = 9437184

// DecodeOptions bounds JSON parsing and selects strictness.
type DecodeOptions struct {
	Mode          JSONMode
	MaxInputBytes int
	Limits        Limits
}

// EncodeJSON returns the deterministic canonical JSON object representation.
func EncodeJSON(value Text) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.Grow(2 + value.Len()*8)
	buffer.WriteByte('{')
	for i, entry := range value.entries {
		if i > 0 {
			buffer.WriteByte(',')
		}
		key, _ := json.Marshal(entry.Locale.String())
		text, _ := json.Marshal(entry.Text)
		buffer.Write(key)
		buffer.WriteByte(':')
		buffer.Write(text)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

// DecodeJSON parses one bounded localized JSON object without partial results.
func DecodeJSON(data []byte, options DecodeOptions) (Text, error) {
	if !utf8.Valid(data) {
		return Text{}, ErrInvalidUTF8
	}
	if options.Mode > PermissiveJSON {
		return Text{}, ErrInvalidPolicy
	}
	maxInput := options.MaxInputBytes
	if maxInput == 0 {
		maxInput = defaultMaxJSONBytes
	}
	if maxInput < 0 || len(data) > maxInput {
		return Text{}, fmt.Errorf("%w: parser input", ErrLimitExceeded)
	}
	limits := options.Limits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	trimmed := bytes.TrimSpace(data)
	if bytes.Equal(trimmed, []byte("null")) {
		if options.Mode == PermissiveJSON {
			return Text{}, nil
		}
		return Text{}, ErrNullValue
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	first, err := decoder.Token()
	if err != nil {
		return Text{}, ErrInvalidEncoding
	}
	delimiter, ok := first.(json.Delim)
	if !ok || delimiter != '{' {
		return Text{}, ErrInvalidEncoding
	}

	entries := make([]Entry, 0)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return Text{}, ErrInvalidEncoding
		}
		raw := keyToken.(string)
		tag, err := parseJSONLocale(raw, options.Mode)
		if err != nil {
			return Text{}, err
		}
		valueToken, err := decoder.Token()
		if err != nil {
			return Text{}, ErrInvalidEncoding
		}
		value, ok := valueToken.(string)
		if !ok {
			return Text{}, ErrInvalidEncoding
		}
		entries = append(entries, Entry{Locale: tag, Text: value})
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return Text{}, ErrInvalidEncoding
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Text{}, ErrTrailingInput
		}
		return Text{}, ErrInvalidEncoding
	}
	return NewTextWithLimits(limits, entries...)
}

func parseJSONLocale(raw string, mode JSONMode) (locale.Tag, error) {
	if mode == StrictJSON && strings.ContainsAny(raw, "_ \t\r\n") {
		return locale.Tag{}, ErrInvalidLocale
	}
	if mode == PermissiveJSON {
		raw = strings.ReplaceAll(raw, "_", "-")
	}
	if len(raw) > defaultMaxTagBytes {
		return locale.Tag{}, fmt.Errorf("%w: tag bytes", ErrLimitExceeded)
	}
	tag, err := locale.Parse(raw)
	if err != nil {
		return locale.Tag{}, ErrInvalidLocale
	}
	return tag, nil
}

// MarshalJSON implements json.Marshaler with canonical object encoding.
func (t Text) MarshalJSON() ([]byte, error) { return EncodeJSON(t) }

// UnmarshalJSON implements json.Unmarshaler using strict bounded decoding.
func (t *Text) UnmarshalJSON(data []byte) error {
	if t == nil {
		return ErrInvalidEncoding
	}
	decoded, err := DecodeJSON(data, DecodeOptions{})
	if err != nil {
		return err
	}
	*t = decoded
	return nil
}
