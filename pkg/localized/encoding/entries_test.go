package encoding_test

import (
	"errors"
	"testing"

	localized "github.com/faustbrian/golib/pkg/localized"
	localizedencoding "github.com/faustbrian/golib/pkg/localized/encoding"
)

func TestEntryArrayIsStableAndRoundTrips(t *testing.T) {
	t.Parallel()
	value, _ := localized.TextFromMap(map[string]string{"fi": "Hei", "en-US": "Hello"})
	encoded, err := localizedencoding.MarshalEntries(value)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(encoded), `[{"locale":"en-US","text":"Hello"},{"locale":"fi","text":"Hei"}]`; got != want {
		t.Fatalf("MarshalEntries() = %s", got)
	}
	decoded, err := localizedencoding.UnmarshalEntries(encoded, localizedencoding.DecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Equal(value) {
		t.Fatalf("decoded = %v", decoded.Entries())
	}
}

func TestEntryArrayDetectsCanonicalDuplicatesAndUnknownFields(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		input string
		want  error
	}{
		{"duplicate", `[{"locale":"en-US","text":"one"},{"locale":"EN-us","text":"two"}]`, localized.ErrDuplicateLocale},
		{"unknown", `[{"locale":"en","text":"one","content":"leak"}]`, localized.ErrInvalidEncoding},
		{"null", `null`, localized.ErrNullValue},
		{"invalid locale", `[{"locale":"en_US","text":"one"}]`, localized.ErrInvalidLocale},
		{"trailing", `[] {}`, localized.ErrTrailingInput},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := localizedencoding.UnmarshalEntries([]byte(test.input), localizedencoding.DecodeOptions{})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEntryArrayDecodeIsBoundedAndPreservesPresentEmpty(t *testing.T) {
	t.Parallel()
	decoded, err := localizedencoding.UnmarshalEntries([]byte(`[{"locale":"fi","text":""}]`), localizedencoding.DecodeOptions{MaxInputBytes: 64, Limits: localized.DefaultLimits()})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := decoded.Get(mustLocale(t, "fi")); !ok || got != "" {
		t.Fatalf("Get(fi) = %q, %v", got, ok)
	}
	if _, err := localizedencoding.UnmarshalEntries([]byte(`[]`), localizedencoding.DecodeOptions{MaxInputBytes: 1}); !errors.Is(err, localized.ErrLimitExceeded) {
		t.Fatalf("limit error = %v", err)
	}
}

func TestEntryArrayRejectsMalformedBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []byte
		want  error
	}{
		{"invalid UTF-8", []byte{0xff}, localized.ErrInvalidUTF8},
		{"negative input limit", []byte(`[]`), localized.ErrLimitExceeded},
		{"invalid JSON", []byte(`[`), localized.ErrInvalidEncoding},
		{"malformed trailing", []byte(`[] {`), localized.ErrInvalidEncoding},
		{"invalid parsed locale", []byte(`[{"locale":"en-","text":"x"}]`), localized.ErrInvalidLocale},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := localizedencoding.DecodeOptions{}
			if test.name == "negative input limit" {
				options.MaxInputBytes = -1
			}
			_, err := localizedencoding.UnmarshalEntries(test.input, options)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
