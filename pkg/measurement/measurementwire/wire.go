// Package measurementwire adapts Quantity to bounded wire JSON and XML
// codecs while preserving decimal and unit metadata.
package measurementwire

import (
	"encoding/xml"
	"fmt"

	measurement "github.com/faustbrian/golib/pkg/measurement"
	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/jsonwire"
	"github.com/faustbrian/golib/pkg/wire/xmlwire"
)

// Options bounds encoded and decoded documents.
type Options struct {
	MaxBytes int64
}

// Encode serializes quantity in an explicitly selected supported format.
func Encode(quantity measurement.Quantity, format wire.Format, options Options) ([]byte, error) {
	switch format {
	case wire.FormatJSON:
		return jsonwire.Encode(quantity, jsonwire.EncodeOptions{MaxBytes: options.MaxBytes})
	case wire.FormatXML:
		return xmlwire.Encode(quantity, xmlwire.EncodeOptions{MaxBytes: options.MaxBytes})
	case wire.FormatSOAP, wire.FormatYAML, wire.FormatTOML, wire.FormatMessagePack,
		wire.FormatCBOR, wire.FormatBSON:
		return nil, unsupported("encode", format)
	default:
		return nil, unsupported("encode", format)
	}
}

// Decode parses exactly one bounded quantity document in an explicitly
// selected supported format.
func Decode(payload []byte, format wire.Format, options Options) (measurement.Quantity, error) {
	var quantity measurement.Quantity
	var err error
	switch format {
	case wire.FormatJSON:
		err = jsonwire.Decode(payload, &quantity, jsonwire.DecodeOptions{
			MaxBytes:              options.MaxBytes,
			DisallowUnknownFields: true,
		})
	case wire.FormatXML:
		err = xmlwire.Decode(payload, &quantity, xmlwire.DecodeOptions{
			MaxBytes:     options.MaxBytes,
			ExpectedRoot: xml.Name{Local: "quantity"},
		})
	case wire.FormatSOAP, wire.FormatYAML, wire.FormatTOML, wire.FormatMessagePack,
		wire.FormatCBOR, wire.FormatBSON:
		err = unsupported("decode", format)
	default:
		err = unsupported("decode", format)
	}
	if err != nil {
		return measurement.Quantity{}, err
	}

	return quantity, nil
}

func unsupported(operation string, format wire.Format) error {
	return &wire.Error{
		Kind:   wire.ErrorKindUnsupported,
		Format: format,
		Op:     operation,
		Err:    fmt.Errorf("measurement adapter supports JSON and XML"),
	}
}
