package postgres_test

import (
	"database/sql/driver"
	"errors"
	"testing"

	localized "github.com/faustbrian/golib/pkg/localized"
	"github.com/faustbrian/golib/pkg/localized/postgres"
	"github.com/jackc/pgx/v5/pgtype"
)

func textFixture(t *testing.T) localized.Text {
	t.Helper()
	value, err := localized.NewText(
		localized.Entry{Locale: mustLocale(t, "fi"), Text: "Hei"},
		localized.Entry{Locale: mustLocale(t, "en"), Text: "Hello"},
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestSQLTextValueAndScanRoundTrip(t *testing.T) {
	t.Parallel()
	value := textFixture(t)
	wrapper := postgres.NewText(value)

	databaseValue, err := wrapper.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	if got := string(databaseValue.([]byte)); got != `{"en":"Hello","fi":"Hei"}` {
		t.Fatalf("Value() = %s", got)
	}

	var scanned postgres.Text
	if err := scanned.Scan(databaseValue); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if !scanned.Valid || !scanned.Localized.Equal(value) {
		t.Fatalf("Scan() = %+v", scanned)
	}
	if err := scanned.Scan(string(databaseValue.([]byte))); err != nil || !scanned.Localized.Equal(value) {
		t.Fatalf("Scan(string) = %+v, %v", scanned, err)
	}
}

func TestSQLTextHasExplicitNullAndFailureSemantics(t *testing.T) {
	t.Parallel()
	invalid := postgres.Text{}
	value, err := invalid.Value()
	if err != nil || value != nil {
		t.Fatalf("invalid Value() = %v, %v", value, err)
	}

	scanned := postgres.NewText(textFixture(t))
	if err := scanned.Scan(nil); err != nil || scanned.Valid || !scanned.Localized.IsEmpty() {
		t.Fatalf("Scan(nil) = %+v, %v", scanned, err)
	}
	before := postgres.NewText(textFixture(t))
	if err := before.Scan(42); !errors.Is(err, postgres.ErrUnsupportedDatabaseType) {
		t.Fatalf("Scan(int) error = %v", err)
	}
	if !before.Valid || before.Localized.IsEmpty() {
		t.Fatal("failed Scan mutated receiver")
	}

	var _ driver.Valuer = postgres.Text{}
	var _ interface{ Scan(any) error } = (*postgres.Text)(nil)
}

func TestPGXJSONBCodecUsesCanonicalTextEncoding(t *testing.T) {
	t.Parallel()
	value := textFixture(t)
	codec := postgres.JSONBCodec()
	typeMap := pgtype.NewMap()

	encodePlan := codec.PlanEncode(typeMap, pgtype.JSONBOID, pgtype.TextFormatCode, value)
	if encodePlan == nil {
		t.Fatal("PlanEncode() = nil")
	}
	encoded, err := encodePlan.Encode(value, nil)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if got := string(encoded); got != `{"en":"Hello","fi":"Hei"}` {
		t.Fatalf("Encode() = %s", got)
	}

	var decoded localized.Text
	scanPlan := codec.PlanScan(typeMap, pgtype.JSONBOID, pgtype.TextFormatCode, &decoded)
	if scanPlan == nil {
		t.Fatal("PlanScan() = nil")
	}
	if err := scanPlan.Scan(encoded, &decoded); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if !decoded.Equal(value) {
		t.Fatalf("decoded = %v", decoded.Entries())
	}
}

func TestNormalizedRowsRoundTripDeterministically(t *testing.T) {
	t.Parallel()
	value := textFixture(t)
	rows := postgres.Rows(value)
	if len(rows) != 2 || rows[0].Locale != "en" || rows[1].Locale != "fi" {
		t.Fatalf("Rows() = %+v", rows)
	}
	rows[0].Text = "changed"
	if got, _ := value.Get(mustLocale(t, "en")); got != "Hello" {
		t.Fatalf("Rows() aliased value: %q", got)
	}

	restored, err := postgres.FromRows(rows)
	if err != nil {
		t.Fatalf("FromRows() error = %v", err)
	}
	if got, _ := restored.Get(mustLocale(t, "en")); got != "changed" {
		t.Fatalf("restored en = %q", got)
	}
}

func TestPostgresBoundaryFailuresAndGenericJSONB(t *testing.T) {
	t.Parallel()

	if got := postgres.ErrUnsupportedDatabaseType.Error(); got != "localized postgres: unsupported database type" {
		t.Fatalf("Error() = %q", got)
	}
	var nilTarget *postgres.Text
	if err := nilTarget.Scan(nil); !errors.Is(err, postgres.ErrUnsupportedDatabaseType) {
		t.Fatalf("nil Scan() error = %v", err)
	}

	codec := postgres.JSONBCodec()
	var nilText *localized.Text
	encoded, err := codec.Marshal(nilText)
	if err != nil || string(encoded) != "null" {
		t.Fatalf("Marshal(nil Text) = %s, %v", encoded, err)
	}
	value := textFixture(t)
	encoded, err = codec.Marshal(&value)
	if err != nil || string(encoded) != `{"en":"Hello","fi":"Hei"}` {
		t.Fatalf("Marshal(*Text) = %s, %v", encoded, err)
	}
	encoded, err = codec.Marshal(map[string]int{"count": 1})
	if err != nil || string(encoded) != `{"count":1}` {
		t.Fatalf("Marshal(generic) = %s, %v", encoded, err)
	}
	if _, err := codec.Marshal(make(chan int)); err == nil {
		t.Fatal("Marshal(unsupported) error = nil")
	}
	var generic map[string]int
	if err := codec.Unmarshal([]byte(`{"count":1}`), &generic); err != nil || generic["count"] != 1 {
		t.Fatalf("Unmarshal(generic) = %v, %v", generic, err)
	}
	if err := codec.Unmarshal([]byte(`{`), &generic); err == nil {
		t.Fatal("Unmarshal(malformed generic) error = nil")
	}
	var localizedTarget localized.Text
	if err := codec.Unmarshal([]byte(`null`), &localizedTarget); !errors.Is(err, localized.ErrNullValue) {
		t.Fatalf("Unmarshal(localized null) error = %v", err)
	}

	for _, rows := range [][]postgres.Row{
		{{Locale: "en_", Text: "bad"}},
		{{Locale: "en-", Text: "bad"}},
	} {
		if _, err := postgres.FromRows(rows); !errors.Is(err, localized.ErrInvalidLocale) {
			t.Fatalf("FromRows(%v) error = %v", rows, err)
		}
	}
}
