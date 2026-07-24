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

func TestEveryDecoderClassifiesInvalidTargets(t *testing.T) {
	t.Parallel()

	soapEnvelope, err := soap.Parse([]byte(`<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"><soap:Body><value/></soap:Body></soap:Envelope>`), soap.ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		decode func(any) error
	}{
		{name: "json", decode: func(target any) error { return jsonwire.Decode([]byte(`{}`), target, jsonwire.DecodeOptions{}) }},
		{name: "xml", decode: func(target any) error { return xmlwire.Decode([]byte(`<value/>`), target, xmlwire.DecodeOptions{}) }},
		{name: "soap", decode: soapEnvelope.DecodeBody},
		{name: "yaml", decode: func(target any) error { return yamlwire.Decode([]byte(`{}`), target, yamlwire.DecodeOptions{}) }},
		{name: "toml", decode: func(target any) error { return tomlwire.Decode(nil, target, tomlwire.DecodeOptions{}) }},
		{name: "msgpack", decode: func(target any) error { return msgpackwire.Decode([]byte{0x80}, target, msgpackwire.DecodeOptions{}) }},
		{name: "cbor", decode: func(target any) error { return cborwire.Decode([]byte{0xa0}, target, cborwire.DecodeOptions{}) }},
		{name: "bson", decode: func(target any) error {
			return bsonwire.Decode([]byte{5, 0, 0, 0, 0}, target, bsonwire.DecodeOptions{})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var typedNil *roundTripDocument
			for _, target := range []any{nil, typedNil, roundTripDocument{}} {
				if err := test.decode(target); !errors.Is(err, wire.ErrTarget) {
					t.Fatalf("target %#v error = %v, want wire.ErrTarget", target, err)
				}
			}
		})
	}
}
