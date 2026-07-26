package localized_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	localized "github.com/faustbrian/golib/pkg/localized"
)

func TestTextJSONIsCanonicalAndRoundTripsEmptyValues(t *testing.T) {
	t.Parallel()

	value, err := localized.NewText(
		localized.Entry{Locale: mustLocale(t, "fi"), Text: ""},
		localized.Entry{Locale: mustLocale(t, "en-US"), Text: "Hello\nworld"},
	)
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if got, want := string(encoded), `{"en-US":"Hello\nworld","fi":""}`; got != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}

	var decoded localized.Text
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !decoded.Equal(value) {
		t.Fatalf("round trip = %v, want %v", decoded.Entries(), value.Entries())
	}
}

func TestDecodeJSONBoundaryFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   []byte
		options localized.DecodeOptions
		want    error
	}{
		{"invalid mode", []byte(`{}`), localized.DecodeOptions{Mode: localized.JSONMode(255)}, localized.ErrInvalidPolicy},
		{"negative input limit", []byte(`{}`), localized.DecodeOptions{MaxInputBytes: -1}, localized.ErrLimitExceeded},
		{"empty input", nil, localized.DecodeOptions{}, localized.ErrInvalidEncoding},
		{"invalid first token", []byte(`{x`), localized.DecodeOptions{}, localized.ErrInvalidEncoding},
		{"missing key", []byte(`{"`), localized.DecodeOptions{}, localized.ErrInvalidEncoding},
		{"missing value", []byte(`{"en":`), localized.DecodeOptions{}, localized.ErrInvalidEncoding},
		{"mismatched close", []byte(`{"en":"x"]`), localized.DecodeOptions{}, localized.ErrInvalidEncoding},
		{"missing close", []byte(`{"en":"x"`), localized.DecodeOptions{}, localized.ErrInvalidEncoding},
		{"malformed trailing", []byte(`{} {`), localized.DecodeOptions{}, localized.ErrInvalidEncoding},
		{"parsed locale failure", []byte(`{"en-":"x"}`), localized.DecodeOptions{}, localized.ErrInvalidLocale},
		{"long locale", []byte(`{"` + strings.Repeat("a", 256) + `":"x"}`), localized.DecodeOptions{}, localized.ErrLimitExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := localized.DecodeJSON(test.input, test.options)
			if !errors.Is(err, test.want) {
				t.Fatalf("DecodeJSON() error = %v, want %v", err, test.want)
			}
		})
	}

	var target *localized.Text
	if err := target.UnmarshalJSON([]byte(`{}`)); !errors.Is(err, localized.ErrInvalidEncoding) {
		t.Fatalf("nil UnmarshalJSON() error = %v", err)
	}
}

func TestTextZeroJSONIsEmptyObject(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(localized.Text{})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{}` {
		t.Fatalf("Marshal(zero) = %s", encoded)
	}
}

func TestTextJSONRejectsHostileAndAmbiguousInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []byte
		want  error
	}{
		{"null", []byte(`null`), localized.ErrNullValue},
		{"array", []byte(`[]`), localized.ErrInvalidEncoding},
		{"non-string", []byte(`{"en":1}`), localized.ErrInvalidEncoding},
		{"invalid locale", []byte(`{"not_a_tag":"value"}`), localized.ErrInvalidLocale},
		{"canonical duplicate", []byte(`{"en-US":"one","EN-us":"two"}`), localized.ErrDuplicateLocale},
		{"invalid UTF-8", []byte{'{', '"', 'e', 'n', '"', ':', '"', 0xff, '"', '}'}, localized.ErrInvalidUTF8},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var value localized.Text
			err := json.Unmarshal(test.input, &value)
			if !errors.Is(err, test.want) {
				t.Fatalf("Unmarshal() error = %v, want %v", err, test.want)
			}
			if !value.IsEmpty() {
				t.Fatalf("failed decode mutated receiver: %v", value.Entries())
			}
		})
	}
	if _, err := localized.DecodeJSON([]byte(`{"en":"one"} true`), localized.DecodeOptions{}); !errors.Is(err, localized.ErrTrailingInput) {
		t.Fatalf("DecodeJSON(trailing) error = %v, want ErrTrailingInput", err)
	}
}

func TestDecodeJSONEnforcesInputAndValueLimits(t *testing.T) {
	t.Parallel()

	_, err := localized.DecodeJSON([]byte(`{"en":"four"}`), localized.DecodeOptions{
		MaxInputBytes: 32,
		Limits:        localized.Limits{MaxLocales: 1, MaxTagBytes: 8, MaxTextBytes: 3, MaxTotalBytes: 3},
	})
	if !errors.Is(err, localized.ErrLimitExceeded) {
		t.Fatalf("DecodeJSON() value error = %v", err)
	}

	_, err = localized.DecodeJSON([]byte(`{"en":"one"}`), localized.DecodeOptions{MaxInputBytes: 4})
	if !errors.Is(err, localized.ErrLimitExceeded) {
		t.Fatalf("DecodeJSON() input error = %v", err)
	}
}

func TestDecodeJSONPermissiveModeAcceptsNullAndLegacySeparators(t *testing.T) {
	t.Parallel()

	zero, err := localized.DecodeJSON([]byte(`null`), localized.DecodeOptions{Mode: localized.PermissiveJSON})
	if err != nil || !zero.IsEmpty() {
		t.Fatalf("DecodeJSON(null) = %v, %v", zero.Entries(), err)
	}
	value, err := localized.DecodeJSON([]byte(`{"en_US":"Hello"}`), localized.DecodeOptions{Mode: localized.PermissiveJSON})
	if err != nil {
		t.Fatalf("DecodeJSON(legacy) error = %v", err)
	}
	if got, ok := value.Get(mustLocale(t, "en-US")); !ok || got != "Hello" {
		t.Fatalf("Get(en-US) = %q, %v", got, ok)
	}
}
