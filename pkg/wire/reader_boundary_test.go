package wire_test

import (
	"errors"
	"io"
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

type endlessReader struct {
	read int
}

func (r *endlessReader) Read(payload []byte) (int, error) {
	for index := range payload {
		payload[index] = 'x'
	}
	r.read += len(payload)
	return len(payload), nil
}

func TestEveryReaderStopsAtOneByteBeyondLimit(t *testing.T) {
	t.Parallel()

	const maxBytes int64 = 8
	tests := []struct {
		name   string
		decode func(io.Reader) error
	}{
		{name: "json", decode: func(reader io.Reader) error {
			return jsonwire.DecodeReader(reader, new(any), jsonwire.DecodeOptions{MaxBytes: maxBytes})
		}},
		{name: "xml", decode: func(reader io.Reader) error {
			return xmlwire.DecodeReader(reader, new(any), xmlwire.DecodeOptions{MaxBytes: maxBytes})
		}},
		{name: "soap", decode: func(reader io.Reader) error {
			_, err := soap.ParseReader(reader, soap.ParseOptions{MaxBytes: maxBytes})
			return err
		}},
		{name: "yaml", decode: func(reader io.Reader) error {
			return yamlwire.DecodeReader(reader, new(any), yamlwire.DecodeOptions{MaxBytes: maxBytes})
		}},
		{name: "toml", decode: func(reader io.Reader) error {
			return tomlwire.DecodeReader(reader, new(any), tomlwire.DecodeOptions{MaxBytes: maxBytes})
		}},
		{name: "msgpack", decode: func(reader io.Reader) error {
			return msgpackwire.DecodeReader(reader, new(any), msgpackwire.DecodeOptions{MaxBytes: maxBytes})
		}},
		{name: "cbor", decode: func(reader io.Reader) error {
			return cborwire.DecodeReader(reader, new(any), cborwire.DecodeOptions{MaxBytes: maxBytes})
		}},
		{name: "bson", decode: func(reader io.Reader) error {
			return bsonwire.DecodeReader(reader, new(any), bsonwire.DecodeOptions{MaxBytes: maxBytes})
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &endlessReader{}
			err := test.decode(reader)
			if !errors.Is(err, wire.ErrSizeLimit) {
				t.Fatalf("error = %v, want wire.ErrSizeLimit", err)
			}
			if reader.read != int(maxBytes+1) {
				t.Fatalf("reader consumed %d bytes, want %d", reader.read, maxBytes+1)
			}
		})
	}
}
