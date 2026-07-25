package encoding_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/money"
	moneyencoding "github.com/faustbrian/golib/pkg/money/encoding"
)

func TestVersionedJSONAndSQLRoundTripHistoricMoneyExactly(t *testing.T) {
	t.Parallel()

	markka, err := currency.ParseWithOptions("FIM", currency.ParseOptions{AllowHistoric: true})
	if err != nil {
		t.Fatalf("currency.ParseWithOptions(FIM) error = %v", err)
	}
	monetaryContext, _ := money.CustomContext(2)
	value, _ := money.Parse("9007199254740993.10", markka, monetaryContext)

	data, err := moneyencoding.MarshalJSON(value)
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	want := `{"version":1,"amount":"9007199254740993.10","currency":"FIM","context":{"kind":"custom","scale":2}}`
	if string(data) != want {
		t.Fatalf("MarshalJSON() = %s", data)
	}
	decoded, err := moneyencoding.UnmarshalJSON(data)
	if err != nil {
		t.Fatalf("UnmarshalJSON() error = %v", err)
	}
	equal, err := decoded.Equal(value)
	if err != nil || !equal {
		t.Fatalf("JSON round trip = %s, %v", decoded, err)
	}

	sqlValue := moneyencoding.SQLMoney{Money: value}
	persisted, err := sqlValue.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	if _, ok := persisted.(string); !ok {
		t.Fatalf("Value() returned unsupported type %T", persisted)
	}
	var scanned moneyencoding.SQLMoney
	if err := scanned.Scan(persisted); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	equal, err = scanned.Money.Equal(value)
	if err != nil || !equal {
		t.Fatalf("SQL round trip = %s, %v", scanned.Money, err)
	}
}

func TestVersionedJSONRejectsDuplicateKeysAtEveryLevel(t *testing.T) {
	t.Parallel()

	inputs := []string{
		`{"version":1,"version":1,"amount":"1.00","currency":"EUR","context":{"kind":"default","scale":2}}`,
		`{"version":1,"amount":"1.00","amount":"2.00","currency":"EUR","context":{"kind":"default","scale":2}}`,
		`{"version":1,"amount":"1.00","currency":"EUR","context":{"kind":"default","kind":"custom","scale":2}}`,
	}
	for _, input := range inputs {
		if _, err := moneyencoding.UnmarshalJSON([]byte(input)); !errors.Is(err, moneyencoding.ErrInvalidEncoding) {
			t.Errorf("UnmarshalJSON(%s) error = %v", input, err)
		}
	}
}
