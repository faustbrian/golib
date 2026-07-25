package wire_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/bsonwire"
	"github.com/faustbrian/golib/pkg/wire/cborwire"
	"github.com/faustbrian/golib/pkg/wire/jsonwire"
	"github.com/faustbrian/golib/pkg/wire/msgpackwire"
	"github.com/faustbrian/golib/pkg/wire/soap"
	"github.com/faustbrian/golib/pkg/wire/tomlwire"
	"github.com/faustbrian/golib/pkg/wire/xmlwire"
	"github.com/faustbrian/golib/pkg/wire/yamlwire"
)

type inputShapeCodec struct {
	name         string
	decode       func([]byte) error
	emptyValid   bool
	spaceValid   bool
	truncated    []byte
	concatenated []byte
}

func TestEveryDecoderDefinesEmptyWhitespaceTruncatedAndConcatenatedInput(t *testing.T) {
	t.Parallel()

	emptyBSON := []byte{5, 0, 0, 0, 0}
	soapEnvelope := []byte(`<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"><soap:Body/></soap:Envelope>`)
	codecs := []inputShapeCodec{
		{name: "json", decode: func(payload []byte) error { return jsonwire.Decode(payload, new(any), jsonwire.DecodeOptions{}) }, truncated: []byte(`{"value":`), concatenated: []byte(`{}[]`)},
		{name: "xml", decode: func(payload []byte) error { return xmlwire.Decode(payload, new(any), xmlwire.DecodeOptions{}) }, truncated: []byte(`<root>`), concatenated: []byte(`<one/><two/>`)},
		{name: "soap", decode: func(payload []byte) error { _, err := soap.Parse(payload, soap.ParseOptions{}); return err }, truncated: soapEnvelope[:len(soapEnvelope)-1], concatenated: append(append([]byte{}, soapEnvelope...), soapEnvelope...)},
		{name: "yaml", decode: func(payload []byte) error { return yamlwire.Decode(payload, new(any), yamlwire.DecodeOptions{}) }, truncated: []byte("value: [\n"), concatenated: []byte("first: 1\n---\nsecond: 2\n")},
		{name: "toml", decode: func(payload []byte) error { return tomlwire.Decode(payload, new(any), tomlwire.DecodeOptions{}) }, emptyValid: true, spaceValid: true, truncated: []byte("value = [\n"), concatenated: []byte("value = 1\nvalue = 2\n")},
		{name: "msgpack", decode: func(payload []byte) error { return msgpackwire.Decode(payload, new(any), msgpackwire.DecodeOptions{}) }, spaceValid: true, truncated: []byte{0xd9}, concatenated: []byte{0x01, 0x02}},
		{name: "cbor", decode: func(payload []byte) error { return cborwire.Decode(payload, new(any), cborwire.DecodeOptions{}) }, spaceValid: true, truncated: []byte{0x65, 'x'}, concatenated: []byte{0x01, 0x02}},
		{name: "bson", decode: func(payload []byte) error { return bsonwire.Decode(payload, new(any), bsonwire.DecodeOptions{}) }, truncated: []byte{5, 0, 0}, concatenated: append(append([]byte{}, emptyBSON...), emptyBSON...)},
	}

	for _, codec := range codecs {
		t.Run(codec.name, func(t *testing.T) {
			for _, shape := range []struct {
				name    string
				payload []byte
				valid   bool
			}{
				{name: "empty", payload: nil, valid: codec.emptyValid},
				{name: "whitespace", payload: []byte{' '}, valid: codec.spaceValid},
				{name: "truncated", payload: codec.truncated},
				{name: "concatenated", payload: codec.concatenated},
			} {
				t.Run(shape.name, func(t *testing.T) {
					err := codec.decode(shape.payload)
					if shape.valid {
						if err != nil {
							t.Fatalf("Decode() error = %v", err)
						}
						return
					}
					if !errors.Is(err, wire.ErrParse) {
						t.Fatalf("Decode() error = %v, want wire.ErrParse", err)
					}
				})
			}
		})
	}
}
