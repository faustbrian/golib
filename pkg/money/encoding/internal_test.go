package encoding

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/international/currency"
	"github.com/faustbrian/golib/pkg/money"
)

func TestEveryContextEncodingAndPersistenceBoundary(t *testing.T) {
	t.Parallel()

	euro, _ := currency.Parse("EUR")
	defaultContext, _ := money.DefaultContext(euro)
	custom, _ := money.CustomContext(3)
	cash, _ := money.CashContext(2, 5)
	contexts := []struct {
		context money.Context
		amount  string
	}{
		{context: defaultContext, amount: "1.00"},
		{context: custom, amount: "1.000"},
		{context: cash, amount: "1.00"},
		{context: money.AutomaticContext(), amount: "1.234"},
	}
	for _, test := range contexts {
		value, err := money.Parse(test.amount, euro, test.context)
		if err != nil {
			t.Fatalf("money.Parse() error = %v", err)
		}
		data, err := MarshalText(value)
		if err != nil {
			t.Fatalf("MarshalText() error = %v", err)
		}
		roundTrip, err := UnmarshalText(data)
		if err != nil {
			t.Fatalf("UnmarshalText() error = %v", err)
		}
		equal, err := value.Equal(roundTrip)
		if err != nil || !equal {
			t.Fatalf("round trip = %s, want %s", roundTrip, value)
		}
	}

	value, _ := money.Parse("1.00", euro, defaultContext)
	numeric, err := NumericValue(value)
	if err != nil || numeric != "1.00" {
		t.Fatalf("NumericValue() = %v, %v", numeric, err)
	}
	for _, source := range []any{"1.00", []byte("1.00")} {
		scanned, err := ScanNumeric(source, euro, defaultContext)
		if err != nil || scanned.String() != "1.00 EUR" {
			t.Fatalf("ScanNumeric(%T) = %s, %v", source, scanned, err)
		}
	}
	var sqlValue SQLMoney
	data, _ := MarshalJSON(value)
	if err := sqlValue.Scan(data); err != nil || sqlValue.Money.String() != "1.00 EUR" {
		t.Fatalf("SQLMoney.Scan([]byte) = %s, %v", sqlValue.Money, err)
	}
}

func TestEncodingInternalInvariantsAndDelimiterValidation(t *testing.T) {
	t.Parallel()

	if got := mustContextKind(money.ContextCash); got != "cash" {
		t.Fatalf("mustContextKind(cash) = %q", got)
	}
	func() {
		defer func() {
			if recovered := recover(); recovered != "money encoding: internal context invariant violated" {
				t.Fatalf("panic = %#v", recovered)
			}
		}()
		mustContextKind(money.ContextKind(255))
	}()
	if err := validateClosingDelimiter('{', json.Delim('}')); err != nil {
		t.Fatalf("valid closing delimiter error = %v", err)
	}
	if err := validateClosingDelimiter('{', json.Delim(']')); !errors.Is(err, ErrInvalidEncoding) {
		t.Fatalf("mismatched closing delimiter error = %v", err)
	}
}

func TestEncodingRejectsEveryInvalidShape(t *testing.T) {
	t.Parallel()

	invalid := [][]byte{
		nil,
		[]byte(strings.Repeat("x", MaxEncodedBytes+1)),
		[]byte(`null`),
		[]byte(`{`),
		[]byte(`{"amount":`),
		[]byte(`{"amount":"1",`),
		[]byte(`[`),
		[]byte(`[1,`),
		[]byte(`[]`),
		[]byte(`[{"amount":"1"}]`),
		[]byte(`{"version":1,"amount":"1.00","currency":"EUR","context":{"kind":"default","scale":2}} trailing`),
		[]byte(`{"version":1,"amount":"1.00","currency":"EUR","unknown":true,"context":{"kind":"default","scale":2}}`),
		[]byte(`{"version":2,"amount":"1.00","currency":"EUR","context":{"kind":"default","scale":2}}`),
		[]byte(`{"version":1,"amount":"","currency":"EUR","context":{"kind":"default","scale":2}}`),
		[]byte(`{"version":1,"amount":"1.00","currency":"ZZZ","context":{"kind":"custom","scale":2}}`),
		[]byte(`{"version":1,"amount":"bad","currency":"EUR","context":{"kind":"custom","scale":2}}`),
		[]byte(`{"version":1,"amount":"1.00","currency":"EUR","context":{"kind":"unknown","scale":2}}`),
		[]byte(`{"version":1,"amount":"1.00","currency":"EUR","context":{"kind":"default","scale":3}}`),
		[]byte(`{"version":1,"amount":"1.00","currency":"EUR","context":{"kind":"default","scale":2,"cash_step":5}}`),
		[]byte(`{"version":1,"amount":"1.00","currency":"EUR","context":{"kind":"custom","scale":2,"cash_step":5}}`),
		[]byte(`{"version":1,"amount":"1.00","currency":"EUR","context":{"kind":"cash","scale":2}}`),
		[]byte(`{"version":1,"amount":"1.00","currency":"EUR","context":{"kind":"automatic","scale":2,"cash_step":5}}`),
		[]byte(`{"version":1,"amount":"1.0","currency":"EUR","context":{"kind":"automatic","scale":2}}`),
	}
	for _, input := range invalid {
		if _, err := UnmarshalJSON(input); !errors.Is(err, ErrInvalidEncoding) {
			t.Errorf("UnmarshalJSON(%q) error = %v", input, err)
		}
	}

	if _, err := MarshalJSON(money.Money{}); !errors.Is(err, money.ErrInvalidMoney) {
		t.Errorf("MarshalJSON(zero) error = %v", err)
	}
	if _, err := NumericValue(money.Money{}); !errors.Is(err, money.ErrInvalidMoney) {
		t.Errorf("NumericValue(zero) error = %v", err)
	}
	if _, err := ScanNumeric(1, currency.Code{}, money.Context{}); !errors.Is(err, ErrInvalidEncoding) {
		t.Errorf("ScanNumeric(type) error = %v", err)
	}
	euro, _ := currency.Parse("EUR")
	monetaryContext, _ := money.DefaultContext(euro)
	if _, err := ScanNumeric(strings.Repeat("9", money.MaxAmountDigits+3), euro, monetaryContext); !errors.Is(err, ErrInvalidEncoding) {
		t.Errorf("ScanNumeric(limit) error = %v", err)
	}
	if _, err := ScanNumeric("bad", euro, monetaryContext); err == nil {
		t.Error("ScanNumeric(bad) accepted invalid amount")
	}

	invalidSQL := SQLMoney{}
	if _, err := invalidSQL.Value(); !errors.Is(err, money.ErrInvalidMoney) {
		t.Errorf("SQLMoney.Value(zero) error = %v", err)
	}
	if err := invalidSQL.Scan(1); !errors.Is(err, ErrInvalidEncoding) {
		t.Errorf("SQLMoney.Scan(type) error = %v", err)
	}
	if err := invalidSQL.Scan("bad"); err == nil {
		t.Error("SQLMoney.Scan(bad) accepted invalid data")
	}
	var nilSQL *SQLMoney
	if err := nilSQL.Scan("bad"); !errors.Is(err, ErrInvalidEncoding) {
		t.Errorf("nil SQLMoney.Scan() error = %v", err)
	}
	if _, err := contextKind(0); !errors.Is(err, money.ErrInvalidContext) {
		t.Errorf("contextKind(0) error = %v", err)
	}
	if wrapContextError(money.ErrInvalidContext) == nil || wrapContextError(nil) != nil {
		t.Error("wrapContextError contract failed")
	}
}
