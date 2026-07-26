package international_test

import (
	"database/sql"
	"database/sql/driver"
	"encoding"
	"encoding/json"
	"errors"
	"testing"

	international "github.com/faustbrian/golib/pkg/international"
	"github.com/faustbrian/golib/pkg/international/country"
	"github.com/faustbrian/golib/pkg/international/currency"
	intlLanguage "github.com/faustbrian/golib/pkg/international/language"
	"github.com/faustbrian/golib/pkg/international/locale"
	"github.com/faustbrian/golib/pkg/international/phone"
	"github.com/faustbrian/golib/pkg/international/postal"
	"github.com/faustbrian/golib/pkg/international/subdivision"
)

type encodedValue interface {
	comparable
	encoding.TextMarshaler
	json.Marshaler
	driver.Valuer
}

type decodedPointer[T any] interface {
	*T
	encoding.TextUnmarshaler
	json.Unmarshaler
	sql.Scanner
}

func TestCanonicalEncodingRoundTripsAndNullSemantics(t *testing.T) {
	t.Parallel()

	finlandValue, finlandErr := country.Parse("FI")
	finland := mustParse(t, finlandValue, finlandErr)
	finlandAlpha3Value, finlandAlpha3Err := country.ParseAlpha3("FIN")
	finlandAlpha3 := mustParse(t, finlandAlpha3Value, finlandAlpha3Err)
	finlandNumericValue, finlandNumericErr := country.ParseNumeric("246")
	finlandNumeric := mustParse(t, finlandNumericValue, finlandNumericErr)
	languageValue, languageErr := intlLanguage.Parse("fi")
	languageCode := mustParse(t, languageValue, languageErr)
	localeValue, localeErr := locale.Parse("EN-us")
	localeTag := mustParse(t, localeValue, localeErr)
	euroValue, euroErr := currency.Parse("EUR")
	euro := mustParse(t, euroValue, euroErr)
	euroNumericValue, euroNumericErr := currency.ParseNumeric("978")
	euroNumeric := mustParse(t, euroNumericValue, euroNumericErr)
	californiaValue, californiaErr := subdivision.Parse("US-CA")
	california := mustParse(t, californiaValue, californiaErr)
	numberValue, numberErr := phone.Parse("+1 650 253 0000 ext. 123", phone.ParseOptions{})
	number := mustParse(t, numberValue, numberErr)
	callingCodeValue, callingCodeErr := phone.ParseCallingCode("+358")
	callingCode := mustParse(t, callingCodeValue, callingCodeErr)
	postalValue, postalErr := postal.Parse("00100", finland)
	postalCode := mustParse(t, postalValue, postalErr)

	tests := []func(*testing.T){
		func(t *testing.T) { exerciseCodec[country.Code, *country.Code](t, finland, "FI") },
		func(t *testing.T) { exerciseCodec[country.Alpha3, *country.Alpha3](t, finlandAlpha3, "FIN") },
		func(t *testing.T) { exerciseCodec[country.Numeric, *country.Numeric](t, finlandNumeric, "246") },
		func(t *testing.T) { exerciseCodec[intlLanguage.Code, *intlLanguage.Code](t, languageCode, "fi") },
		func(t *testing.T) { exerciseCodec[locale.Tag, *locale.Tag](t, localeTag, "EN-us") },
		func(t *testing.T) { exerciseCodec[currency.Code, *currency.Code](t, euro, "EUR") },
		func(t *testing.T) { exerciseCodec[currency.Numeric, *currency.Numeric](t, euroNumeric, "978") },
		func(t *testing.T) { exerciseCodec[subdivision.Code, *subdivision.Code](t, california, "US-CA") },
		func(t *testing.T) { exerciseCodec[phone.Number, *phone.Number](t, number, "+16502530000;ext=123") },
		func(t *testing.T) { exerciseCodec[phone.CallingCode, *phone.CallingCode](t, callingCode, "+358") },
		func(t *testing.T) { exerciseCodec[postal.Code, *postal.Code](t, postalCode, "FI\t00100") },
	}
	for index, test := range tests {
		t.Run(string(rune('a'+index)), test)
	}
}

func TestPrivatePersistenceRepresentationsRejectAmbiguity(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"+16502530000;ext=",
		"+16502530000;ext=1;ext=2",
		"+16502530000;other=1",
		"+999;ext=1",
		"+16502530000;ext=abc",
	} {
		var number phone.Number
		if err := number.UnmarshalText([]byte(input)); err == nil {
			t.Fatalf("phone UnmarshalText(%q) succeeded", input)
		}
	}

	for _, input := range []string{"00100", "FI\t00100\textra", "ZZ\t00100"} {
		var code postal.Code
		if err := code.UnmarshalText([]byte(input)); err == nil {
			t.Fatalf("postal UnmarshalText(%q) succeeded", input)
		}
	}
}

func exerciseCodec[T encodedValue, P decodedPointer[T]](t *testing.T, value T, text string) {
	t.Helper()

	encodedText, err := value.MarshalText()
	if err != nil || string(encodedText) != text {
		t.Fatalf("MarshalText() = %q, %v, want %q", encodedText, err, text)
	}
	var fromText T
	if err := P(&fromText).UnmarshalText(encodedText); err != nil || fromText != value {
		t.Fatalf("UnmarshalText() = %#v, %v, want %#v", fromText, err, value)
	}

	encodedJSON, err := value.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON() error = %v", err)
	}
	wantJSON, _ := json.Marshal(text)
	if string(encodedJSON) != string(wantJSON) {
		t.Fatalf("MarshalJSON() = %s, want %s", encodedJSON, wantJSON)
	}
	var fromJSON T
	if err := P(&fromJSON).UnmarshalJSON(encodedJSON); err != nil || fromJSON != value {
		t.Fatalf("UnmarshalJSON() = %#v, %v, want %#v", fromJSON, err, value)
	}

	databaseValue, err := value.Value()
	if err != nil || databaseValue != text {
		t.Fatalf("Value() = %#v, %v, want %q", databaseValue, err, text)
	}
	var fromDatabase T
	if err := P(&fromDatabase).Scan([]byte(text)); err != nil || fromDatabase != value {
		t.Fatalf("Scan() = %#v, %v, want %#v", fromDatabase, err, value)
	}

	var zero T
	if _, err := zero.MarshalText(); !errors.Is(err, international.ErrInvalid) {
		t.Fatalf("zero MarshalText() error = %v, want ErrInvalid", err)
	}
	zeroJSON, err := zero.MarshalJSON()
	if err != nil || string(zeroJSON) != "null" {
		t.Fatalf("zero MarshalJSON() = %s, %v, want null", zeroJSON, err)
	}
	zeroDatabase, err := zero.Value()
	if err != nil || zeroDatabase != nil {
		t.Fatalf("zero Value() = %#v, %v, want nil", zeroDatabase, err)
	}

	fromJSON = value
	if err := P(&fromJSON).UnmarshalJSON([]byte("null")); err != nil || fromJSON != zero {
		t.Fatalf("UnmarshalJSON(null) = %#v, %v, want zero", fromJSON, err)
	}
	fromDatabase = value
	if err := P(&fromDatabase).Scan(nil); err != nil || fromDatabase != zero {
		t.Fatalf("Scan(nil) = %#v, %v, want zero", fromDatabase, err)
	}

	unchanged := value
	if err := P(&unchanged).Scan(42); err == nil || unchanged != value {
		t.Fatalf("Scan(int) = %#v, %v, want unchanged error", unchanged, err)
	}
	if err := P(&unchanged).UnmarshalText(nil); err == nil || unchanged != value {
		t.Fatalf("UnmarshalText(empty) = %#v, %v, want unchanged error", unchanged, err)
	}
	if err := P(&unchanged).UnmarshalJSON([]byte("42")); err == nil || unchanged != value {
		t.Fatalf("UnmarshalJSON(number) = %#v, %v, want unchanged error", unchanged, err)
	}
}

func mustParse[T any](t *testing.T, value T, err error) T {
	t.Helper()
	if err != nil {
		t.Fatalf("parse fixture error = %v", err)
	}
	return value
}
