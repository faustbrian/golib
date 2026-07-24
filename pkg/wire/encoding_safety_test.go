package wire_test

import (
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
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

func TestMessagePackCyclicEncodeReturnsError(t *testing.T) {
	if os.Getenv("GO_WIRE_CYCLIC_MSGPACK") == "1" {
		value := map[string]any{}
		value["self"] = value
		_, err := msgpackwire.Encode(value, msgpackwire.EncodeOptions{})
		if errors.Is(err, wire.ErrEncode) {
			return
		}
		os.Exit(2)
	}

	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMessagePackCyclicEncodeReturnsError$")
	command.Env = append(os.Environ(), "GO_WIRE_CYCLIC_MSGPACK=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("cyclic encode did not return a classified error: %v\n%s", err, output)
	}
}

func TestAllEncodePathsRejectCyclicValues(t *testing.T) {
	t.Parallel()

	value := map[string]any{}
	value["self"] = value
	tests := []struct {
		name string
		err  func() error
		kind error
	}{
		{name: "json", err: func() error { _, err := jsonwire.Encode(value, jsonwire.EncodeOptions{}); return err }, kind: wire.ErrValidation},
		{name: "xml", err: func() error { _, err := xmlwire.Encode(value, xmlwire.EncodeOptions{}); return err }, kind: wire.ErrValidation},
		{name: "soap", err: func() error { _, err := soap.Encode(soap.Version12, nil, value, soap.EncodeOptions{}); return err }, kind: wire.ErrValidation},
		{name: "yaml", err: func() error { _, err := yamlwire.Encode(value, yamlwire.EncodeOptions{}); return err }, kind: wire.ErrEncode},
		{name: "toml", err: func() error { _, err := tomlwire.Encode(value, tomlwire.EncodeOptions{}); return err }, kind: wire.ErrEncode},
		{name: "msgpack", err: func() error { _, err := msgpackwire.Encode(value, msgpackwire.EncodeOptions{}); return err }, kind: wire.ErrEncode},
		{name: "cbor", err: func() error { _, err := cborwire.Encode(value, cborwire.EncodeOptions{}); return err }, kind: wire.ErrEncode},
		{name: "bson", err: func() error { _, err := bsonwire.Encode(value, bsonwire.EncodeOptions{}); return err }, kind: wire.ErrEncode},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.err(); !errors.Is(err, test.kind) {
				t.Fatalf("error = %v, want %v", err, test.kind)
			}
		})
	}
}

func TestAllWriterPathsRejectZeroProgress(t *testing.T) {
	t.Parallel()

	writer := zeroWriter{}
	tests := []struct {
		name string
		err  error
	}{
		{name: "json", err: jsonwire.EncodeWriter(writer, map[string]int{"v": 1}, jsonwire.EncodeOptions{})},
		{name: "xml", err: xmlwire.EncodeWriter(writer, xmlValue{V: 1}, xmlwire.EncodeOptions{})},
		{name: "soap typed", err: soap.EncodeWriter(writer, soap.Version12, nil, xmlValue{V: 1}, soap.EncodeOptions{})},
		{name: "soap raw", err: soap.MarshalWriter(writer, soap.Version12, nil, []byte("<v>1</v>"))},
		{name: "soap fault", err: soap.MarshalFaultWriter(writer, soap.Fault{Version: soap.Version12, Code: "env:Receiver", Reason: "failed"})},
		{name: "yaml", err: yamlwire.EncodeWriter(writer, map[string]int{"v": 1}, yamlwire.EncodeOptions{})},
		{name: "toml", err: tomlwire.EncodeWriter(writer, map[string]int{"v": 1}, tomlwire.EncodeOptions{})},
		{name: "msgpack", err: msgpackwire.EncodeWriter(writer, map[string]int{"v": 1}, msgpackwire.EncodeOptions{})},
		{name: "cbor", err: cborwire.EncodeWriter(writer, map[string]int{"v": 1}, cborwire.EncodeOptions{})},
		{name: "bson", err: bsonwire.EncodeWriter(writer, bsonwire.D{{Key: "v", Value: 1}}, bsonwire.EncodeOptions{})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !errors.Is(test.err, wire.ErrWrite) {
				t.Fatalf("error = %v, want wire.ErrWrite", test.err)
			}
		})
	}
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

type xmlValue struct {
	XMLName xml.Name `xml:"value"`
	V       int      `xml:"v"`
}
