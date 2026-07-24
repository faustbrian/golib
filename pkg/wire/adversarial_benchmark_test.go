package wire_test

import (
	"bytes"
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

func BenchmarkRejectAdversarialDecode(b *testing.B) {
	jsonDeep := []byte(strings.Repeat("[", 10_001) + "0" + strings.Repeat("]", 10_001))
	xmlDeep := []byte(strings.Repeat("<a>", 33) + strings.Repeat("</a>", 33))
	soapDeep := []byte(`<soap:Envelope xmlns:soap="http://www.w3.org/2003/05/soap-envelope"><soap:Body>` + strings.Repeat("<a>", 31) + strings.Repeat("</a>", 31) + `</soap:Body></soap:Envelope>`)
	yamlAliases := []byte("base: &base value\none: *base\ntwo: *base\n")
	cborArray := append([]byte{0x98, 0x11}, bytes.Repeat([]byte{0x00}, 17)...)

	tests := []struct {
		name   string
		decode func() error
	}{
		{name: "json-depth", decode: func() error { return jsonwire.Decode(jsonDeep, new(any), jsonwire.DecodeOptions{}) }},
		{name: "xml-depth", decode: func() error { return xmlwire.Decode(xmlDeep, new(any), xmlwire.DecodeOptions{MaxDepth: 32}) }},
		{name: "soap-depth", decode: func() error { _, err := soap.Parse(soapDeep, soap.ParseOptions{MaxDepth: 32}); return err }},
		{name: "yaml-aliases", decode: func() error { return yamlwire.Decode(yamlAliases, new(any), yamlwire.DecodeOptions{MaxAliases: 1}) }},
		{name: "toml-bytes", decode: func() error {
			return tomlwire.Decode([]byte("value = 'oversized'\n"), new(any), tomlwire.DecodeOptions{MaxBytes: 8})
		}},
		{name: "msgpack-length", decode: func() error {
			return msgpackwire.Decode([]byte{0xdd, 0xff, 0xff, 0xff, 0xff}, new(any), msgpackwire.DecodeOptions{})
		}},
		{name: "cbor-array", decode: func() error {
			return cborwire.Decode(cborArray, new(any), cborwire.DecodeOptions{MaxArrayElements: 16})
		}},
		{name: "bson-length", decode: func() error {
			return bsonwire.Decode([]byte{0xff, 0xff, 0xff, 0x7f, 0x00}, new(any), bsonwire.DecodeOptions{})
		}},
	}

	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := test.decode(); err == nil {
					b.Fatal("Decode() error = nil")
				}
			}
		})
	}
}

func BenchmarkRejectCyclicEncode(b *testing.B) {
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	tests := []struct {
		name   string
		encode func() error
	}{
		{name: "json", encode: func() error { _, err := jsonwire.Encode(cyclic, jsonwire.EncodeOptions{}); return err }},
		{name: "xml", encode: func() error { _, err := xmlwire.Encode(cyclic, xmlwire.EncodeOptions{}); return err }},
		{name: "soap", encode: func() error { _, err := soap.Encode(soap.Version12, nil, cyclic, soap.EncodeOptions{}); return err }},
		{name: "yaml", encode: func() error { _, err := yamlwire.Encode(cyclic, yamlwire.EncodeOptions{}); return err }},
		{name: "toml", encode: func() error { _, err := tomlwire.Encode(cyclic, tomlwire.EncodeOptions{}); return err }},
		{name: "msgpack", encode: func() error { _, err := msgpackwire.Encode(cyclic, msgpackwire.EncodeOptions{}); return err }},
		{name: "cbor", encode: func() error { _, err := cborwire.Encode(cyclic, cborwire.EncodeOptions{}); return err }},
		{name: "bson", encode: func() error { _, err := bsonwire.Encode(cyclic, bsonwire.EncodeOptions{}); return err }},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := test.encode(); err == nil {
					b.Fatal("Encode() error = nil")
				}
			}
		})
	}
}

func TestForgedBinaryLengthsHaveBoundedAllocationCounts(t *testing.T) {
	tests := []struct {
		name   string
		decode func() error
	}{
		{name: "msgpack", decode: func() error {
			return msgpackwire.Decode([]byte{0xdd, 0xff, 0xff, 0xff, 0xff}, new(any), msgpackwire.DecodeOptions{})
		}},
		{name: "cbor", decode: func() error {
			return cborwire.Decode([]byte{0x9a, 0xff, 0xff, 0xff, 0xff}, new(any), cborwire.DecodeOptions{})
		}},
		{name: "bson", decode: func() error {
			return bsonwire.Decode([]byte{0xff, 0xff, 0xff, 0x7f, 0x00}, new(any), bsonwire.DecodeOptions{})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			allocations := testing.AllocsPerRun(100, func() {
				if err := test.decode(); err == nil {
					t.Fatal("Decode() error = nil")
				}
			})
			if allocations > 200 {
				t.Fatalf("allocations = %.0f, want at most 200", allocations)
			}
		})
	}
}
