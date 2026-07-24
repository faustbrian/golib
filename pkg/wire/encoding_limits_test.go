package wire_test

import (
	"bytes"
	"errors"
	"math"
	"strings"
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

type outputLimitCase struct {
	name   string
	encode func(int64) ([]byte, error)
}

type outputLimitXML struct {
	Value string `xml:"value"`
}

func outputLimitCases(value string) []outputLimitCase {
	return []outputLimitCase{
		{name: "json", encode: func(limit int64) ([]byte, error) {
			return jsonwire.Encode(map[string]string{"value": value}, jsonwire.EncodeOptions{MaxBytes: limit})
		}},
		{name: "xml", encode: func(limit int64) ([]byte, error) {
			return xmlwire.Encode(outputLimitXML{Value: value}, xmlwire.EncodeOptions{MaxBytes: limit})
		}},
		{name: "soap typed", encode: func(limit int64) ([]byte, error) {
			return soap.Encode(soap.Version12, nil, outputLimitXML{Value: value}, soap.EncodeOptions{MaxBytes: limit})
		}},
		{name: "soap raw", encode: func(limit int64) ([]byte, error) {
			return soap.MarshalWithOptions(soap.Version12, nil, []byte("<value>"+value+"</value>"), soap.MarshalOptions{MaxBytes: limit})
		}},
		{name: "soap fault", encode: func(limit int64) ([]byte, error) {
			return soap.MarshalFaultWithOptions(
				soap.Fault{Version: soap.Version12, Code: "env:Receiver", Reason: value},
				soap.MarshalOptions{MaxBytes: limit},
			)
		}},
		{name: "yaml", encode: func(limit int64) ([]byte, error) {
			return yamlwire.Encode(map[string]string{"value": value}, yamlwire.EncodeOptions{MaxBytes: limit})
		}},
		{name: "toml", encode: func(limit int64) ([]byte, error) {
			return tomlwire.Encode(map[string]string{"value": value}, tomlwire.EncodeOptions{MaxBytes: limit})
		}},
		{name: "msgpack", encode: func(limit int64) ([]byte, error) {
			return msgpackwire.Encode(map[string]string{"value": value}, msgpackwire.EncodeOptions{MaxBytes: limit})
		}},
		{name: "cbor", encode: func(limit int64) ([]byte, error) {
			return cborwire.Encode(map[string]string{"value": value}, cborwire.EncodeOptions{MaxBytes: limit})
		}},
		{name: "bson", encode: func(limit int64) ([]byte, error) {
			return bsonwire.Encode(bsonwire.D{{Key: "value", Value: value}}, bsonwire.EncodeOptions{MaxBytes: limit})
		}},
	}
}

func TestEveryEncoderEnforcesOutputLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		encode func() error
	}{
		{name: "json", encode: func() error {
			_, err := jsonwire.Encode(map[string]string{"value": "large"}, jsonwire.EncodeOptions{MaxBytes: 1})
			return err
		}},
		{name: "xml", encode: func() error { _, err := xmlwire.Encode(xmlValue{V: 1}, xmlwire.EncodeOptions{MaxBytes: 1}); return err }},
		{name: "soap typed", encode: func() error {
			_, err := soap.Encode(soap.Version12, nil, xmlValue{V: 1}, soap.EncodeOptions{MaxBytes: 1})
			return err
		}},
		{name: "soap raw", encode: func() error {
			_, err := soap.MarshalWithOptions(soap.Version12, nil, []byte("<v>1</v>"), soap.MarshalOptions{MaxBytes: 1})
			return err
		}},
		{name: "soap fault", encode: func() error {
			_, err := soap.MarshalFaultWithOptions(soap.Fault{Version: soap.Version12, Code: "env:Receiver", Reason: "failed"}, soap.MarshalOptions{MaxBytes: 1})
			return err
		}},
		{name: "yaml", encode: func() error {
			_, err := yamlwire.Encode(map[string]string{"value": "large"}, yamlwire.EncodeOptions{MaxBytes: 1})
			return err
		}},
		{name: "toml", encode: func() error {
			_, err := tomlwire.Encode(map[string]string{"value": "large"}, tomlwire.EncodeOptions{MaxBytes: 1})
			return err
		}},
		{name: "msgpack", encode: func() error {
			_, err := msgpackwire.Encode(map[string]string{"value": "large"}, msgpackwire.EncodeOptions{MaxBytes: 1})
			return err
		}},
		{name: "cbor", encode: func() error {
			_, err := cborwire.Encode(map[string]string{"value": "large"}, cborwire.EncodeOptions{MaxBytes: 1})
			return err
		}},
		{name: "bson", encode: func() error {
			_, err := bsonwire.Encode(bsonwire.D{{Key: "value", Value: "large"}}, bsonwire.EncodeOptions{MaxBytes: 1})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.encode(); !errors.Is(err, wire.ErrSizeLimit) {
				t.Fatalf("error = %v, want wire.ErrSizeLimit", err)
			}
		})
	}
}

func TestEncodeWriterDoesNotExceedOutputLimit(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	err := jsonwire.EncodeWriter(&output, map[string]string{"value": "large"}, jsonwire.EncodeOptions{MaxBytes: 4})
	if !errors.Is(err, wire.ErrSizeLimit) {
		t.Fatalf("error = %v, want wire.ErrSizeLimit", err)
	}
	if output.Len() != 0 {
		t.Fatalf("writer received %d bytes after encode limit failure", output.Len())
	}
}

func TestEveryEncoderHonorsExactNegativeAndMaximumOutputLimits(t *testing.T) {
	t.Parallel()

	for _, test := range outputLimitCases("bounded") {
		t.Run(test.name, func(t *testing.T) {
			want, err := test.encode(math.MaxInt64)
			if err != nil {
				t.Fatalf("maximum limit: %v", err)
			}
			got, err := test.encode(int64(len(want)))
			if err != nil {
				t.Fatalf("exact limit: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("exact output = %x, want %x", got, want)
			}
			if _, err := test.encode(int64(len(want) - 1)); !errors.Is(err, wire.ErrSizeLimit) {
				t.Fatalf("boundary error = %v, want wire.ErrSizeLimit", err)
			}
			if _, err := test.encode(-1); !errors.Is(err, wire.ErrValidation) {
				t.Fatalf("negative error = %v, want wire.ErrValidation", err)
			}
		})
	}
}

func TestEveryEncoderUsesBoundedDefaultOutput(t *testing.T) {
	t.Parallel()

	large := strings.Repeat("x", int(jsonwire.DefaultMaxBytes)+1)
	for _, test := range outputLimitCases(large) {
		t.Run(test.name, func(t *testing.T) {
			if _, err := test.encode(0); !errors.Is(err, wire.ErrSizeLimit) {
				t.Fatalf("default limit error = %v, want wire.ErrSizeLimit", err)
			}
		})
	}
}
