package measurementwire_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
	"github.com/faustbrian/golib/pkg/measurement/measurementwire"
	"github.com/faustbrian/golib/pkg/wire"
)

func TestJSONAndXMLRoundTripsPreserveUnitMetadata(t *testing.T) {
	t.Parallel()

	original := measurement.MustNew(decimal.MustParse("12.50"), measurement.Kilogram)
	for _, format := range []wire.Format{wire.FormatJSON, wire.FormatXML} {
		format := format
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			payload, err := measurementwire.Encode(original, format, measurementwire.Options{MaxBytes: 1024})
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			decoded, err := measurementwire.Decode(payload, format, measurementwire.Options{MaxBytes: 1024})
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if decoded.String() != original.String() {
				t.Fatalf("round trip = %q, want %q", decoded, original)
			}
		})
	}
}

func TestAdapterRejectsUnsupportedFormatsAndOversizePayloads(t *testing.T) {
	t.Parallel()

	quantity := measurement.MustNew(decimal.New(1), measurement.Metre)
	if _, err := measurementwire.Encode(quantity, wire.FormatYAML, measurementwire.Options{}); !errors.Is(err, wire.ErrUnsupportedFormat) {
		t.Fatalf("Encode(YAML) error = %v", err)
	}
	if _, err := measurementwire.Decode(nil, wire.FormatYAML, measurementwire.Options{}); !errors.Is(err, wire.ErrUnsupportedFormat) {
		t.Fatalf("Decode(YAML) error = %v", err)
	}
	if _, err := measurementwire.Encode(quantity, wire.Format("unknown"), measurementwire.Options{}); !errors.Is(err, wire.ErrUnsupportedFormat) {
		t.Fatalf("Encode(unknown) error = %v", err)
	}
	if _, err := measurementwire.Decode(nil, wire.Format("unknown"), measurementwire.Options{}); !errors.Is(err, wire.ErrUnsupportedFormat) {
		t.Fatalf("Decode(unknown) error = %v", err)
	}
	if _, err := measurementwire.Decode([]byte(`{"value":"1","unit":"m"}`), wire.FormatJSON, measurementwire.Options{MaxBytes: 4}); !errors.Is(err, wire.ErrSizeLimit) {
		t.Fatalf("Decode(oversize) error = %v", err)
	}
}
