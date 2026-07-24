package wire_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/bsonwire"
	"github.com/faustbrian/golib/pkg/wire/cborwire"
	"github.com/faustbrian/golib/pkg/wire/jsonwire"
	"github.com/faustbrian/golib/pkg/wire/msgpackwire"
	"github.com/faustbrian/golib/pkg/wire/yamlwire"
	"github.com/fxamacker/cbor/v2"
	"github.com/vmihailenco/msgpack/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.yaml.in/yaml/v4"
)

func TestJSONRejectsInvalidUTF8AcceptedByStandardLibrary(t *testing.T) {
	t.Parallel()

	payload := []byte{'{', '"', 'v', '"', ':', '"', 0xff, '"', '}'}
	var dependency map[string]string
	if err := json.Unmarshal(payload, &dependency); err != nil {
		t.Fatalf("encoding/json unexpectedly rejected differential: %v", err)
	}
	var target map[string]string
	if err := jsonwire.Decode(payload, &target, jsonwire.DecodeOptions{}); !errors.Is(err, wire.ErrParse) {
		t.Fatalf("wire error = %v, want wire.ErrParse", err)
	}
}

func TestMessagePackRejectsDuplicateAcceptedByDependency(t *testing.T) {
	t.Parallel()

	payload := []byte{0x82, 0xa1, 'a', 0x01, 0xa1, 'a', 0x02}
	var dependency map[string]int
	if err := msgpack.Unmarshal(payload, &dependency); err != nil || dependency["a"] != 2 {
		t.Fatalf("dependency result = %#v, %v", dependency, err)
	}
	var target map[string]int
	if err := msgpackwire.Decode(payload, &target, msgpackwire.DecodeOptions{}); !errors.Is(err, wire.ErrParse) {
		t.Fatalf("wire error = %v, want wire.ErrParse", err)
	}
}

func TestCBORRejectsDuplicateAcceptedByDependencyDefault(t *testing.T) {
	t.Parallel()

	payload := []byte{0xa2, 0x61, 'a', 0x01, 0x61, 'a', 0x02}
	var dependency map[string]int
	if err := cbor.Unmarshal(payload, &dependency); err != nil || dependency["a"] != 2 {
		t.Fatalf("dependency result = %#v, %v", dependency, err)
	}
	var target map[string]int
	if err := cborwire.Decode(payload, &target, cborwire.DecodeOptions{}); !errors.Is(err, wire.ErrParse) {
		t.Fatalf("wire error = %v, want wire.ErrParse", err)
	}
}

func TestBSONRejectsDuplicatePreservedByDependency(t *testing.T) {
	t.Parallel()

	payload := []byte{0x13, 0x00, 0x00, 0x00, 0x10, 'a', 0x00, 0x01, 0x00, 0x00, 0x00, 0x10, 'a', 0x00, 0x02, 0x00, 0x00, 0x00, 0x00}
	var dependency bson.D
	if err := bson.Unmarshal(payload, &dependency); err != nil || len(dependency) != 2 {
		t.Fatalf("dependency result = %#v, %v", dependency, err)
	}
	var target bsonwire.D
	if err := bsonwire.Decode(payload, &target, bsonwire.DecodeOptions{}); !errors.Is(err, wire.ErrParse) {
		t.Fatalf("wire error = %v, want wire.ErrParse", err)
	}
}

func TestYAMLRepairsDependencyBlockIndentDifferential(t *testing.T) {
	t.Parallel()

	value := struct {
		Text string `yaml:"text"`
	}{Text: "\t\n0"}
	dependency, err := yaml.Dump(value, yaml.WithV4Defaults(), yaml.WithLineWidth(-1))
	if err != nil {
		t.Fatal(err)
	}
	var dependencyTarget any
	if err := yaml.Load(dependency, &dependencyTarget, yaml.WithV4Defaults()); err == nil {
		t.Fatalf("dependency unexpectedly accepted its implicit block output %q", dependency)
	}

	payload, err := yamlwire.Encode(value, yamlwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var target struct {
		Text string `yaml:"text"`
	}
	if err := yamlwire.Decode(payload, &target, yamlwire.DecodeOptions{}); err != nil || target.Text != value.Text {
		t.Fatalf("wire result = %#v, %v; payload %q", target, err, payload)
	}
}
