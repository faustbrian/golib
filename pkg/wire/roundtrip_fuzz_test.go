package wire_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/wire/bsonwire"
	"github.com/faustbrian/golib/pkg/wire/cborwire"
	"github.com/faustbrian/golib/pkg/wire/jsonwire"
	"github.com/faustbrian/golib/pkg/wire/msgpackwire"
	"github.com/faustbrian/golib/pkg/wire/soap"
	"github.com/faustbrian/golib/pkg/wire/tomlwire"
	"github.com/faustbrian/golib/pkg/wire/xmlwire"
	"github.com/faustbrian/golib/pkg/wire/yamlwire"
)

type roundTripDocument struct {
	Text   string `json:"text" xml:"text" yaml:"text" toml:"text" msgpack:"text" cbor:"text" bson:"text"`
	Number int64  `json:"number" xml:"number" yaml:"number" toml:"number" msgpack:"number" cbor:"number" bson:"number"`
	Flag   bool   `json:"flag" xml:"flag" yaml:"flag" toml:"flag" msgpack:"flag" cbor:"flag" bson:"flag"`
}

func FuzzRoundTrip(f *testing.F) {
	f.Add("shipment <ready> & safe", int64(42), true)
	f.Add(string([]byte{'b', 'a', 'd', 0xff}), int64(-1), false)
	f.Add("\x00\t\n\r", int64(0), true)

	f.Fuzz(func(t *testing.T, text string, number int64, flag bool) {
		if len(text) > 1024 {
			text = text[:1024]
		}
		text = strings.ToValidUTF8(text, "�")
		text = strings.Map(func(value rune) rune {
			if value == '\t' || value == '\n' || value == '\r' ||
				(value >= 0x20 && value <= 0xd7ff) ||
				(value >= 0xe000 && value <= 0xfffd) ||
				(value >= 0x10000 && value <= 0x10ffff) {
				return value
			}
			return -1
		}, text)

		want := roundTripDocument{Text: text, Number: number, Flag: flag}
		codecs := []struct {
			name      string
			roundTrip func(*roundTripDocument) error
		}{
			{name: "json", roundTrip: func(got *roundTripDocument) error {
				payload, err := jsonwire.Encode(want, jsonwire.EncodeOptions{})
				if err != nil {
					return err
				}
				return jsonwire.Decode(payload, got, jsonwire.DecodeOptions{})
			}},
			{name: "xml", roundTrip: func(got *roundTripDocument) error {
				payload, err := xmlwire.Encode(want, xmlwire.EncodeOptions{})
				if err != nil {
					return err
				}
				return xmlwire.Decode(payload, got, xmlwire.DecodeOptions{})
			}},
			{name: "soap", roundTrip: func(got *roundTripDocument) error {
				payload, err := soap.Encode(soap.Version12, nil, want, soap.EncodeOptions{})
				if err != nil {
					return err
				}
				envelope, err := soap.Parse(payload, soap.ParseOptions{})
				if err != nil {
					return err
				}
				return envelope.DecodeBody(got)
			}},
			{name: "yaml", roundTrip: func(got *roundTripDocument) error {
				payload, err := yamlwire.Encode(want, yamlwire.EncodeOptions{})
				if err != nil {
					return err
				}
				return yamlwire.Decode(payload, got, yamlwire.DecodeOptions{})
			}},
			{name: "toml", roundTrip: func(got *roundTripDocument) error {
				payload, err := tomlwire.Encode(want, tomlwire.EncodeOptions{})
				if err != nil {
					return err
				}
				return tomlwire.Decode(payload, got, tomlwire.DecodeOptions{})
			}},
			{name: "msgpack", roundTrip: func(got *roundTripDocument) error {
				payload, err := msgpackwire.Encode(want, msgpackwire.EncodeOptions{})
				if err != nil {
					return err
				}
				return msgpackwire.Decode(payload, got, msgpackwire.DecodeOptions{})
			}},
			{name: "cbor", roundTrip: func(got *roundTripDocument) error {
				payload, err := cborwire.Encode(want, cborwire.EncodeOptions{})
				if err != nil {
					return err
				}
				return cborwire.Decode(payload, got, cborwire.DecodeOptions{})
			}},
			{name: "bson", roundTrip: func(got *roundTripDocument) error {
				payload, err := bsonwire.Encode(want, bsonwire.EncodeOptions{})
				if err != nil {
					return err
				}
				return bsonwire.Decode(payload, got, bsonwire.DecodeOptions{})
			}},
		}

		for _, codec := range codecs {
			t.Run(codec.name, func(t *testing.T) {
				var got roundTripDocument
				if err := codec.roundTrip(&got); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("round trip = %#v, want %#v", got, want)
				}
			})
		}
	})
}
