package localizedwire_test

import (
	"errors"
	"testing"

	localized "github.com/faustbrian/golib/pkg/localized"
	"github.com/faustbrian/golib/pkg/localized/localizedwire"
	"github.com/faustbrian/golib/pkg/wire/jsonwire"
	"github.com/faustbrian/golib/pkg/wire/msgpackwire"
	"github.com/faustbrian/golib/pkg/wire/tomlwire"
	"github.com/faustbrian/golib/pkg/wire/yamlwire"
)

func wireFixture(t *testing.T) localized.Text {
	t.Helper()
	value, err := localized.TextFromMap(map[string]string{"en-US": "Hello", "fi": "Hei"})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestGoWireDecodersRejectMalformedPayloads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		decode func([]byte) (localized.Text, error)
		input  []byte
	}{
		{"json", func(data []byte) (localized.Text, error) {
			return localizedwire.DecodeJSON(data, jsonwire.DecodeOptions{})
		}, []byte(`{`)},
		{"yaml", func(data []byte) (localized.Text, error) {
			return localizedwire.DecodeYAML(data, yamlwire.DecodeOptions{})
		}, []byte("en: [")},
		{"toml", func(data []byte) (localized.Text, error) {
			return localizedwire.DecodeTOML(data, tomlwire.DecodeOptions{})
		}, []byte("en = [")},
		{"messagepack", func(data []byte) (localized.Text, error) {
			return localizedwire.DecodeMessagePack(data, msgpackwire.DecodeOptions{})
		}, []byte{0xc1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.decode(test.input); err == nil {
				t.Fatal("decode error = nil")
			}
		})
	}

	if _, err := localizedwire.DecodeYAML([]byte("en_: bad\n"), yamlwire.DecodeOptions{}); !errors.Is(err, localized.ErrInvalidLocale) {
		t.Fatalf("semantic decode error = %v", err)
	}
}

func TestGoWireAdaptersRoundTripLocalizedText(t *testing.T) {
	t.Parallel()
	value := wireFixture(t)
	tests := []struct {
		name   string
		encode func(localized.Text) ([]byte, error)
		decode func([]byte) (localized.Text, error)
	}{
		{"json", func(value localized.Text) ([]byte, error) {
			return localizedwire.EncodeJSON(value, jsonwire.EncodeOptions{})
		}, func(data []byte) (localized.Text, error) {
			return localizedwire.DecodeJSON(data, jsonwire.DecodeOptions{})
		}},
		{"yaml", func(value localized.Text) ([]byte, error) {
			return localizedwire.EncodeYAML(value, yamlwire.EncodeOptions{})
		}, func(data []byte) (localized.Text, error) {
			return localizedwire.DecodeYAML(data, yamlwire.DecodeOptions{})
		}},
		{"toml", func(value localized.Text) ([]byte, error) {
			return localizedwire.EncodeTOML(value, tomlwire.EncodeOptions{})
		}, func(data []byte) (localized.Text, error) {
			return localizedwire.DecodeTOML(data, tomlwire.DecodeOptions{})
		}},
		{"messagepack", func(value localized.Text) ([]byte, error) {
			return localizedwire.EncodeMessagePack(value, msgpackwire.EncodeOptions{})
		}, func(data []byte) (localized.Text, error) {
			return localizedwire.DecodeMessagePack(data, msgpackwire.DecodeOptions{})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := test.encode(value)
			if err != nil {
				t.Fatalf("encode error = %v", err)
			}
			decoded, err := test.decode(encoded)
			if err != nil {
				t.Fatalf("decode error = %v", err)
			}
			if !decoded.Equal(value) {
				t.Fatalf("decoded = %v", decoded.Entries())
			}
		})
	}
}

func TestGoWireJSONIsCanonicalAndDecodeOwnsPayload(t *testing.T) {
	t.Parallel()
	encoded, err := localizedwire.EncodeJSON(wireFixture(t), jsonwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"en-US":"Hello","fi":"Hei"}` {
		t.Fatalf("JSON = %s", encoded)
	}
	decoded, err := localizedwire.DecodeJSON(encoded, jsonwire.DecodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range encoded {
		encoded[i] = 'x'
	}
	if got, _ := decoded.Get(mustLocale(t, "en-US")); got != "Hello" {
		t.Fatalf("decoded aliased payload: %q", got)
	}
}
