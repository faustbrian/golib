package measurement_test

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/math/decimal"
	measurement "github.com/faustbrian/golib/pkg/measurement"
)

func TestQuantityJSONPreservesDecimalAndUnitMetadata(t *testing.T) {
	t.Parallel()

	original := measurement.MustNew(decimal.MustParse("9007199254740993.125"), measurement.Kilogram)
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if got, want := string(data), `{"value":"9007199254740993.125","unit":"kg"}`; got != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}

	var decoded measurement.Quantity
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got := decoded.String(); got != original.String() {
		t.Fatalf("round trip = %q, want %q", got, original)
	}
	if err := json.Unmarshal([]byte(`{"value":1.25,"unit":"kg"}`), &decoded); !errors.Is(err, measurement.ErrInvalidQuantity) {
		t.Fatalf("numeric JSON value error = %v", err)
	}
}

func TestQuantityCodecsRejectAmbiguousFields(t *testing.T) {
	t.Parallel()

	jsonPayloads := []string{
		`{"value":"1","value":"2","unit":"m"}`,
		`{"value":"1","unit":"m","unit":"kg"}`,
	}
	for _, payload := range jsonPayloads {
		var quantity measurement.Quantity
		if err := json.Unmarshal([]byte(payload), &quantity); !errors.Is(err, measurement.ErrInvalidQuantity) {
			t.Fatalf("json.Unmarshal(%s) error = %v, want ErrInvalidQuantity", payload, err)
		}
	}

	xmlPayloads := []string{
		`<quantity><value>1</value><value>2</value><unit>m</unit></quantity>`,
		`<quantity><value>1</value><unit>m</unit><unit>kg</unit></quantity>`,
		`<quantity><value>1</value><unit>m</unit><unknown>x</unknown></quantity>`,
	}
	for _, payload := range xmlPayloads {
		var quantity measurement.Quantity
		if err := xml.Unmarshal([]byte(payload), &quantity); !errors.Is(err, measurement.ErrInvalidQuantity) {
			t.Fatalf("xml.Unmarshal(%s) error = %v, want ErrInvalidQuantity", payload, err)
		}
	}
}

func TestQuantityXMLPreservesDecimalAndUnitMetadata(t *testing.T) {
	t.Parallel()

	original := measurement.MustNew(decimal.MustParse("12.50"), measurement.Centimetre)
	data, err := xml.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if got, want := string(data), `<quantity><value>12.50</value><unit>cm</unit></quantity>`; got != want {
		t.Fatalf("Marshal() = %s, want %s", got, want)
	}

	var decoded measurement.Quantity
	if err := xml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got := decoded.String(); got != original.String() {
		t.Fatalf("round trip = %q, want %q", got, original)
	}
}

func TestQuantitySQLValueAndScannerRoundTrip(t *testing.T) {
	t.Parallel()

	original := measurement.MustNew(decimal.MustParse("3.25"), measurement.Litre)
	value, err := original.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	var decoded measurement.Quantity
	if err := decoded.Scan(value); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if got := decoded.String(); got != original.String() {
		t.Fatalf("round trip = %q, want %q", got, original)
	}
	if err := decoded.Scan(123); !errors.Is(err, measurement.ErrInvalidQuantity) {
		t.Fatalf("Scan(int) error = %v", err)
	}
}

func TestFormatRequiresExplicitTargetAndRounding(t *testing.T) {
	t.Parallel()

	quantity := measurement.MustNew(decimal.New(1), measurement.Metre)
	formatted, err := quantity.Format(measurement.FormatOptions{
		Unit:       measurement.Foot,
		Conversion: measurement.RoundedConversion(3, decimal.HalfEven),
		Scale:      2,
		Rounding:   decimal.HalfEven,
		Separator:  " ",
	})
	if err != nil {
		t.Fatalf("Format() error = %v", err)
	}
	if formatted != "3.28 ft" {
		t.Fatalf("Format() = %q, want %q", formatted, "3.28 ft")
	}
}

func TestDimensionsJSONAndXMLRoundTripAllUnits(t *testing.T) {
	t.Parallel()

	original, err := measurement.NewDimensions(
		measurement.MustNew(decimal.MustParse("1.2"), measurement.Metre),
		measurement.MustNew(decimal.New(80), measurement.Centimetre),
		measurement.MustNew(decimal.New(600), measurement.Millimetre),
		2,
	)
	if err != nil {
		t.Fatalf("NewDimensions() error = %v", err)
	}

	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("JSON Marshal() error = %v", err)
	}
	var fromJSON measurement.Dimensions
	if err := json.Unmarshal(jsonData, &fromJSON); err != nil {
		t.Fatalf("JSON Unmarshal() error = %v", err)
	}
	if fromJSON.Length().String() != "1.2 m" || fromJSON.Width().String() != "80 cm" ||
		fromJSON.Height().String() != "600 mm" || fromJSON.Quantity() != 2 {
		t.Fatalf("JSON round trip lost metadata: %s %s %s x%d", fromJSON.Length(), fromJSON.Width(), fromJSON.Height(), fromJSON.Quantity())
	}

	xmlData, err := xml.Marshal(original)
	if err != nil {
		t.Fatalf("XML Marshal() error = %v", err)
	}
	var fromXML measurement.Dimensions
	if err := xml.Unmarshal(xmlData, &fromXML); err != nil {
		t.Fatalf("XML Unmarshal() error = %v", err)
	}
	if fromXML.Length().String() != "1.2 m" || fromXML.Width().String() != "80 cm" ||
		fromXML.Height().String() != "600 mm" || fromXML.Quantity() != 2 {
		t.Fatalf("XML round trip lost metadata: %s %s %s x%d", fromXML.Length(), fromXML.Width(), fromXML.Height(), fromXML.Quantity())
	}

	if err := json.Unmarshal([]byte(`{"length":{"value":"1","unit":"m"},"width":{"value":"1","unit":"m"},"height":{"value":"1","unit":"m"},"quantity":0}`), &fromJSON); !errors.Is(err, measurement.ErrInvalidQuantity) {
		t.Fatalf("invalid dimensions error = %v", err)
	}
}

func TestDimensionsCodecsRejectAmbiguousFields(t *testing.T) {
	t.Parallel()

	quantity := `{"value":"1","unit":"m"}`
	jsonPayloads := []string{
		`{"length":` + quantity + `,"length":` + quantity + `,"width":` + quantity + `,"height":` + quantity + `,"quantity":1}`,
		`{"length":` + quantity + `,"width":` + quantity + `,"height":` + quantity + `,"quantity":1,"quantity":2}`,
	}
	for _, payload := range jsonPayloads {
		var dimensions measurement.Dimensions
		if err := json.Unmarshal([]byte(payload), &dimensions); !errors.Is(err, measurement.ErrInvalidQuantity) {
			t.Fatalf("json.Unmarshal(%s) error = %v, want ErrInvalidQuantity", payload, err)
		}
	}

	xmlPayloads := []string{
		`<dimensions><length><value>1</value><unit>m</unit></length><length><value>1</value><unit>m</unit></length><width><value>1</value><unit>m</unit></width><height><value>1</value><unit>m</unit></height><quantity>1</quantity></dimensions>`,
		`<dimensions><length><value>1</value><unit>m</unit></length><width><value>1</value><unit>m</unit></width><height><value>1</value><unit>m</unit></height><quantity>1</quantity><unknown>x</unknown></dimensions>`,
	}
	for _, payload := range xmlPayloads {
		var dimensions measurement.Dimensions
		if err := xml.Unmarshal([]byte(payload), &dimensions); !errors.Is(err, measurement.ErrInvalidQuantity) {
			t.Fatalf("xml.Unmarshal(%s) error = %v, want ErrInvalidQuantity", payload, err)
		}
	}
}
