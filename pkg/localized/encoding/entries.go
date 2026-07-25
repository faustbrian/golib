// Package encoding provides alternate canonical localized representations.
package encoding

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/international/locale"
	localized "github.com/faustbrian/golib/pkg/localized"
)

const defaultMaxInputBytes = 9 << 20

// Entry is the stable entry-array wire representation.
type Entry struct {
	Locale string `json:"locale"`
	Text   string `json:"text"`
}

// DecodeOptions bounds entry-array input and constructed values.
type DecodeOptions struct {
	MaxInputBytes int
	Limits        localized.Limits
}

// MarshalEntries encodes deterministic canonical entry order.
func MarshalEntries(value localized.Text) ([]byte, error) {
	entries := value.Entries()
	wire := make([]Entry, len(entries))
	for i, entry := range entries {
		wire[i] = Entry{Locale: entry.Locale.String(), Text: entry.Text}
	}
	return json.Marshal(wire)
}

// UnmarshalEntries strictly decodes one bounded entry array.
func UnmarshalEntries(data []byte, options DecodeOptions) (localized.Text, error) {
	if !utf8.Valid(data) {
		return localized.Text{}, localized.ErrInvalidUTF8
	}
	maxInput := options.MaxInputBytes
	if maxInput == 0 {
		maxInput = defaultMaxInputBytes
	}
	if maxInput < 0 || len(data) > maxInput {
		return localized.Text{}, localized.ErrLimitExceeded
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return localized.Text{}, localized.ErrNullValue
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var wire []Entry
	if err := decoder.Decode(&wire); err != nil {
		return localized.Text{}, localized.ErrInvalidEncoding
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return localized.Text{}, localized.ErrTrailingInput
		}
		return localized.Text{}, localized.ErrInvalidEncoding
	}
	entries := make([]localized.Entry, 0, len(wire))
	for _, entry := range wire {
		if strings.ContainsAny(entry.Locale, "_ \t\r\n") {
			return localized.Text{}, localized.ErrInvalidLocale
		}
		tag, err := locale.Parse(entry.Locale)
		if err != nil {
			return localized.Text{}, localized.ErrInvalidLocale
		}
		entries = append(entries, localized.Entry{Locale: tag, Text: entry.Text})
	}
	return localized.NewTextWithLimits(options.Limits, entries...)
}
