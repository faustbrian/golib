package wire_test

import (
	"errors"
	"strings"
	"testing"

	wire "github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/bsonwire"
	"github.com/faustbrian/golib/pkg/wire/cborwire"
	"github.com/faustbrian/golib/pkg/wire/jsonwire"
	"github.com/faustbrian/golib/pkg/wire/msgpackwire"
	"github.com/faustbrian/golib/pkg/wire/soap"
	"github.com/faustbrian/golib/pkg/wire/tomlwire"
	"github.com/faustbrian/golib/pkg/wire/xmlwire"
	"github.com/faustbrian/golib/pkg/wire/yamlwire"
)

func TestDecodeErrorsDoNotEchoSensitiveValues(t *testing.T) {
	t.Parallel()

	const secret = "credential-0123456789-abcdefghijklmnopqrstuvwxyz"
	soapPayload := []byte(`<Envelope xmlns="http://schemas.xmlsoap.org/soap/envelope/"><Body><credential>` + secret + `</credential><broken>`)
	tests := []struct {
		name   string
		decode func() error
	}{
		{name: "json", decode: func() error {
			return jsonwire.Decode([]byte(`{"credential":"`+secret+`","broken":`), new(any), jsonwire.DecodeOptions{})
		}},
		{name: "xml", decode: func() error {
			return xmlwire.Decode([]byte(`<root><credential>`+secret+`</credential><broken>`), new(any), xmlwire.DecodeOptions{})
		}},
		{name: "soap", decode: func() error { _, err := soap.Parse(soapPayload, soap.ParseOptions{}); return err }},
		{name: "yaml", decode: func() error {
			return yamlwire.Decode([]byte("credential: "+secret+"\nbroken: [\n"), new(any), yamlwire.DecodeOptions{})
		}},
		{name: "toml", decode: func() error {
			return tomlwire.Decode([]byte("credential = \""+secret+"\"\nbroken = [\n"), new(any), tomlwire.DecodeOptions{})
		}},
		{name: "msgpack", decode: func() error {
			return msgpackwire.Decode(append([]byte{0xd9, 0xff}, secret...), new(any), msgpackwire.DecodeOptions{})
		}},
		{name: "cbor", decode: func() error {
			return cborwire.Decode(append([]byte{0x78, 0xff}, secret...), new(any), cborwire.DecodeOptions{})
		}},
		{name: "bson", decode: func() error {
			return bsonwire.Decode(append([]byte{0xff, 0xff, 0xff, 0x7f}, secret...), new(any), bsonwire.DecodeOptions{})
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := test.decode()
			if err == nil {
				t.Fatal("decode returned nil error")
			}
			var classified *wire.Error
			if !errors.As(err, &classified) {
				t.Fatalf("error %T is not a *wire.Error", err)
			}
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error contains sensitive value: %v", err)
			}
		})
	}
}
