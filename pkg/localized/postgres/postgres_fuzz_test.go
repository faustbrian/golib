package postgres_test

import (
	"testing"

	localized "github.com/faustbrian/golib/pkg/localized"
	"github.com/faustbrian/golib/pkg/localized/postgres"
)

func FuzzSQLScan(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"en":"Hello"}`))
	f.Add([]byte(`null`))
	f.Add([]byte{0xff})
	f.Fuzz(func(t *testing.T, input []byte) {
		var value postgres.Text
		if err := value.Scan(input); err != nil {
			return
		}
		databaseValue, err := value.Value()
		if err != nil || databaseValue == nil {
			t.Fatalf("Value() = %v, %v", databaseValue, err)
		}
		var roundTrip postgres.Text
		if err := roundTrip.Scan(databaseValue); err != nil {
			t.Fatal(err)
		}
		if !roundTrip.Valid || !roundTrip.Localized.Equal(value.Localized) {
			t.Fatal("round trip changed value")
		}
	})
}

func FuzzPGXJSONBCodec(f *testing.F) {
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"en":"Hello","fi":""}`))
	f.Add([]byte(`null`))
	f.Add([]byte{0xff})
	codec := postgres.JSONBCodec()
	f.Fuzz(func(t *testing.T, input []byte) {
		var value localized.Text
		if err := codec.Unmarshal(input, &value); err != nil {
			return
		}
		encoded, err := codec.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var roundTrip localized.Text
		if err := codec.Unmarshal(encoded, &roundTrip); err != nil || !roundTrip.Equal(value) {
			t.Fatalf("round trip error = %v", err)
		}
	})
}
