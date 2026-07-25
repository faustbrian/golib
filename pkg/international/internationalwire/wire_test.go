package internationalwire_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/international/internationalwire"
	"github.com/faustbrian/golib/pkg/wire"
)

type document struct {
	Country  country.Code  `json:"country" xml:"country" yaml:"country" toml:"country" msgpack:"country" cbor:"country" bson:"country"`
	Currency currency.Code `json:"currency" xml:"currency" yaml:"currency" toml:"currency" msgpack:"currency" cbor:"currency" bson:"currency"`
}

func TestSupportedFormatsRoundTripStrictScalars(t *testing.T) {
	t.Parallel()
	countryCode, _ := country.Parse("FI")
	currencyCode, _ := currency.Parse("EUR")
	want := document{Country: countryCode, Currency: currencyCode}
	formats := []wire.Format{wire.FormatJSON, wire.FormatXML, wire.FormatYAML,
		wire.FormatTOML, wire.FormatMessagePack}
	for _, format := range formats {
		t.Run(string(format), func(t *testing.T) {
			payload, err := internationalwire.Encode(format, want)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			var got document
			if err := internationalwire.Decode(format, payload, &got); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if got != want {
				t.Fatalf("round trip = %#v, want %#v", got, want)
			}
		})
	}
}

func TestUnsupportedFormatIsExplicit(t *testing.T) {
	t.Parallel()
	for _, format := range []wire.Format{wire.FormatSOAP, wire.FormatCBOR, wire.FormatBSON, wire.Format("unknown")} {
		if _, err := internationalwire.Encode(format, document{}); !errors.Is(err, internationalwire.ErrUnsupportedFormat) {
			t.Fatalf("Encode(%s) error = %v", format, err)
		}
		if err := internationalwire.Decode(format, nil, &document{}); !errors.Is(err, internationalwire.ErrUnsupportedFormat) {
			t.Fatalf("Decode(%s) error = %v", format, err)
		}
	}
}
