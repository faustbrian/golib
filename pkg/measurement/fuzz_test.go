package measurement_test

import (
	"encoding/json"
	"encoding/xml"
	"testing"

	measurement "github.com/faustbrian/golib/pkg/measurement"
)

func FuzzParseAndTextRoundTrip(f *testing.F) {
	for _, seed := range []string{"1 m", "-12.50 kg", "0 degC", "1e3 m", "", "1 unknown"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		quantity, err := measurement.Parse(input, measurement.SymbolProfile())
		if err != nil {
			return
		}
		text, err := quantity.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText() error = %v", err)
		}
		var decoded measurement.Quantity
		if err := decoded.UnmarshalText(text); err != nil {
			t.Fatalf("UnmarshalText(%q) error = %v", text, err)
		}
		if equal, err := quantity.Equal(decoded, measurement.ExactConversion()); err != nil || !equal || quantity.Unit() != decoded.Unit() {
			t.Fatalf("text round trip = %s, %v", decoded, err)
		}
	})
}

func FuzzQuantityJSON(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(`{"value":"1.25","unit":"m"}`),
		[]byte(`{"value":1.25,"unit":"m"}`),
		[]byte(`null`),
		{},
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		var quantity measurement.Quantity
		if err := json.Unmarshal(input, &quantity); err != nil {
			return
		}
		encoded, err := json.Marshal(quantity)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		var decoded measurement.Quantity
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("round-trip Unmarshal() error = %v", err)
		}
		if quantity.Unit() != decoded.Unit() || !quantity.Amount().Equal(decoded.Amount()) {
			t.Fatalf("JSON round trip = %s, want %s", decoded, quantity)
		}
	})
}

func FuzzQuantityXMLAndSQL(f *testing.F) {
	for _, seed := range []string{
		`<quantity><value>1.25</value><unit>m</unit></quantity>`,
		`<quantity><value>bad</value><unit>m</unit></quantity>`,
		``,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		var quantity measurement.Quantity
		if err := xml.Unmarshal([]byte(input), &quantity); err != nil {
			return
		}
		value, err := quantity.Value()
		if err != nil {
			t.Fatalf("Value() error = %v", err)
		}
		var scanned measurement.Quantity
		if err := scanned.Scan(value); err != nil {
			t.Fatalf("Scan() error = %v", err)
		}
		if quantity.Unit() != scanned.Unit() || !quantity.Amount().Equal(scanned.Amount()) {
			t.Fatalf("XML/SQL round trip = %s, want %s", scanned, quantity)
		}
	})
}
