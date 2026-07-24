package encoding

import (
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/money"
)

func FuzzVersionedJSON(f *testing.F) {
	f.Add([]byte(`{"version":1,"amount":"1.00","currency":"EUR","context":{"kind":"default","scale":2}}`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"version":2}`))
	f.Fuzz(func(t *testing.T, input []byte) {
		value, err := UnmarshalJSON(input)
		if err != nil {
			return
		}
		encoded, err := MarshalJSON(value)
		if err != nil {
			t.Fatalf("MarshalJSON() error = %v", err)
		}
		roundTrip, err := UnmarshalJSON(encoded)
		if err != nil {
			t.Fatalf("canonical UnmarshalJSON() error = %v", err)
		}
		equal, err := value.Equal(roundTrip)
		if err != nil || !equal {
			t.Fatalf("round trip = %s, want %s", roundTrip, value)
		}
	})
}

func FuzzPostgreSQLNumeric(f *testing.F) {
	for _, seed := range []string{"0.00", "-1.25", "9007199254740993.10", "NaN", "1e3"} {
		f.Add(seed)
	}
	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := money.DefaultContext(euro)
	f.Fuzz(func(t *testing.T, input string) {
		value, err := ScanNumeric(input, euro, monetaryContext)
		if err != nil {
			return
		}
		persisted, err := NumericValue(value)
		if err != nil || persisted != value.Amount().String() {
			t.Fatalf("NumericValue() = %v, %v", persisted, err)
		}
	})
}
