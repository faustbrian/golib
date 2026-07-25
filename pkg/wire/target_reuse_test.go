package wire_test

import (
	"reflect"
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

type mutationSource struct {
	First  string `json:"first" xml:"first" yaml:"first" toml:"first" msgpack:"first" cbor:"first" bson:"first"`
	Number string `json:"number" xml:"number" yaml:"number" toml:"number" msgpack:"number" cbor:"number" bson:"number"`
}

type mutationTarget struct {
	First  string `json:"first" xml:"first" yaml:"first" toml:"first" msgpack:"first" cbor:"first" bson:"first"`
	Number int    `json:"number" xml:"number" yaml:"number" toml:"number" msgpack:"number" cbor:"number" bson:"number"`
}

type reusableCodec struct {
	name         string
	encode       func(roundTripDocument) ([]byte, error)
	decode       func([]byte, any) error
	encodeBroken func(mutationSource) ([]byte, error)
}

func reusableCodecs() []reusableCodec {
	return []reusableCodec{
		{name: "json", encode: func(value roundTripDocument) ([]byte, error) { return jsonwire.Encode(value, jsonwire.EncodeOptions{}) }, decode: func(payload []byte, target any) error {
			return jsonwire.Decode(payload, target, jsonwire.DecodeOptions{})
		}, encodeBroken: func(value mutationSource) ([]byte, error) { return jsonwire.Encode(value, jsonwire.EncodeOptions{}) }},
		{name: "xml", encode: func(value roundTripDocument) ([]byte, error) { return xmlwire.Encode(value, xmlwire.EncodeOptions{}) }, decode: func(payload []byte, target any) error {
			return xmlwire.Decode(payload, target, xmlwire.DecodeOptions{})
		}, encodeBroken: func(value mutationSource) ([]byte, error) { return xmlwire.Encode(value, xmlwire.EncodeOptions{}) }},
		{name: "soap", encode: func(value roundTripDocument) ([]byte, error) {
			return soap.Encode(soap.Version12, nil, value, soap.EncodeOptions{})
		}, decode: func(payload []byte, target any) error {
			envelope, err := soap.Parse(payload, soap.ParseOptions{})
			if err != nil {
				return err
			}
			return envelope.DecodeBody(target)
		}, encodeBroken: func(value mutationSource) ([]byte, error) {
			return soap.Encode(soap.Version12, nil, value, soap.EncodeOptions{})
		}},
		{name: "yaml", encode: func(value roundTripDocument) ([]byte, error) { return yamlwire.Encode(value, yamlwire.EncodeOptions{}) }, decode: func(payload []byte, target any) error {
			return yamlwire.Decode(payload, target, yamlwire.DecodeOptions{})
		}, encodeBroken: func(value mutationSource) ([]byte, error) { return yamlwire.Encode(value, yamlwire.EncodeOptions{}) }},
		{name: "toml", encode: func(value roundTripDocument) ([]byte, error) { return tomlwire.Encode(value, tomlwire.EncodeOptions{}) }, decode: func(payload []byte, target any) error {
			return tomlwire.Decode(payload, target, tomlwire.DecodeOptions{})
		}, encodeBroken: func(value mutationSource) ([]byte, error) { return tomlwire.Encode(value, tomlwire.EncodeOptions{}) }},
		{name: "msgpack", encode: func(value roundTripDocument) ([]byte, error) {
			return msgpackwire.Encode(value, msgpackwire.EncodeOptions{})
		}, decode: func(payload []byte, target any) error {
			return msgpackwire.Decode(payload, target, msgpackwire.DecodeOptions{})
		}, encodeBroken: func(value mutationSource) ([]byte, error) {
			return msgpackwire.Encode(value, msgpackwire.EncodeOptions{})
		}},
		{name: "cbor", encode: func(value roundTripDocument) ([]byte, error) { return cborwire.Encode(value, cborwire.EncodeOptions{}) }, decode: func(payload []byte, target any) error {
			return cborwire.Decode(payload, target, cborwire.DecodeOptions{})
		}, encodeBroken: func(value mutationSource) ([]byte, error) { return cborwire.Encode(value, cborwire.EncodeOptions{}) }},
		{name: "bson", encode: func(value roundTripDocument) ([]byte, error) { return bsonwire.Encode(value, bsonwire.EncodeOptions{}) }, decode: func(payload []byte, target any) error {
			return bsonwire.Decode(payload, target, bsonwire.DecodeOptions{})
		}, encodeBroken: func(value mutationSource) ([]byte, error) { return bsonwire.Encode(value, bsonwire.EncodeOptions{}) }},
	}
}

func TestEveryDecoderSupportsSuccessfulTargetReuse(t *testing.T) {
	t.Parallel()

	values := []roundTripDocument{
		{Text: "first", Number: 1, Flag: true},
		{Text: "second", Number: -2, Flag: false},
	}
	for _, codec := range reusableCodecs() {
		t.Run(codec.name, func(t *testing.T) {
			target := roundTripDocument{Text: "stale", Number: 99, Flag: true}
			for _, want := range values {
				payload, err := codec.encode(want)
				if err != nil {
					t.Fatal(err)
				}
				if err := codec.decode(payload, &target); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(target, want) {
					t.Fatalf("reused target = %#v, want %#v", target, want)
				}
			}
		})
	}
}

func TestEveryDecoderTreatsFailedTargetAsIndeterminate(t *testing.T) {
	t.Parallel()

	source := mutationSource{First: "assigned-before-error", Number: "not-an-integer"}
	var atomic, partial int
	for _, codec := range reusableCodecs() {
		t.Run(codec.name, func(t *testing.T) {
			payload, err := codec.encodeBroken(source)
			if err != nil {
				t.Fatal(err)
			}
			initial := mutationTarget{First: "stale", Number: 42}
			target := initial
			if err := codec.decode(payload, &target); err == nil {
				t.Fatal("Decode() error = nil")
			}
			if target == initial {
				atomic++
			} else {
				partial++
			}
		})
	}
	if atomic == 0 || partial == 0 {
		t.Fatalf("failed decodes produced %d unchanged and %d partial targets; want evidence of both", atomic, partial)
	}
}
