package postgres

import (
	"errors"
	"testing"
	"time"

	calendar "github.com/faustbrian/golib/pkg/calendar"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestDateScannerAndValuerBranches(t *testing.T) {
	want := calendar.MustDate(2024, time.February, 29)
	for _, source := range []any{[]byte("2024-02-29"), time.Date(2024, 2, 29, 14, 0, 0, 0, time.FixedZone("x", 3600))} {
		var value Date
		if err := value.Scan(source); err != nil || value.CalendarDate() != want {
			t.Fatalf("Scan(%T) = %s, %v", source, value.CalendarDate(), err)
		}
	}
	var nilDate *Date
	if err := nilDate.Scan("2024-02-29"); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("nil Scan error = %v", err)
	}
	if err := nilDate.ScanDate(pgtype.Date{Valid: true}); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("nil ScanDate error = %v", err)
	}
	var value Date
	if err := value.ScanDate(pgtype.Date{}); !errors.Is(err, ErrNull) {
		t.Fatalf("null ScanDate error = %v", err)
	}
	if err := value.ScanDate(pgtype.Date{Valid: true, Time: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("range ScanDate error = %v", err)
	}
	if _, err := value.DateValue(); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("zero DateValue error = %v", err)
	}
}

func TestInfinityDateEverySQLState(t *testing.T) {
	finiteDate := calendar.MustDate(2025, time.January, 2)
	finite := NewFiniteDate(finiteDate)
	if value, err := finite.Value(); err != nil || value != "2025-01-02" {
		t.Fatalf("finite Value = %#v, %v", value, err)
	}
	if _, err := (InfinityDate{}).Value(); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("zero finite Value error = %v", err)
	}
	if _, err := NewInfinityDate(InfinityKind(2)).Value(); !errors.Is(err, ErrInfinity) {
		t.Fatalf("unknown Value error = %v", err)
	}
	for _, source := range []any{[]byte("infinity"), []byte("-infinity"), []byte("2025-01-02"), time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)} {
		var decoded InfinityDate
		if err := decoded.Scan(source); err != nil {
			t.Fatalf("Scan(%T) error = %v", source, err)
		}
	}
	var decoded InfinityDate
	if err := decoded.Scan(nil); !errors.Is(err, ErrNull) {
		t.Fatalf("null Scan error = %v", err)
	}
	if err := decoded.Scan(42); err == nil {
		t.Fatal("unsupported Scan succeeded")
	}
	var nilInfinity *InfinityDate
	if err := nilInfinity.Scan("infinity"); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("nil infinity Scan error = %v", err)
	}
	if err := nilInfinity.ScanDate(pgtype.Date{Valid: true}); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("nil infinity ScanDate error = %v", err)
	}
}

func TestInfinityDateEveryPGXState(t *testing.T) {
	finiteDate := calendar.MustDate(2025, time.January, 2)
	states := []pgtype.Date{
		{Valid: true, InfinityModifier: pgtype.NegativeInfinity},
		{Valid: true, InfinityModifier: pgtype.Infinity},
		{Valid: true, Time: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)},
	}
	for _, state := range states {
		var decoded InfinityDate
		if err := decoded.ScanDate(state); err != nil {
			t.Fatalf("ScanDate(%#v) error = %v", state, err)
		}
		encoded, err := decoded.DateValue()
		if err != nil || !encoded.Valid || encoded.InfinityModifier != state.InfinityModifier {
			t.Fatalf("DateValue() = %#v, %v", encoded, err)
		}
		if state.InfinityModifier == pgtype.Finite && decoded.Date() != finiteDate {
			t.Fatalf("finite date = %s", decoded.Date())
		}
	}
	var decoded InfinityDate
	if err := decoded.ScanDate(pgtype.Date{}); !errors.Is(err, ErrNull) {
		t.Fatalf("null pgx error = %v", err)
	}
	if err := decoded.ScanDate(pgtype.Date{Valid: true, Time: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("range pgx error = %v", err)
	}
	if err := decoded.ScanDate(pgtype.Date{Valid: true, InfinityModifier: pgtype.InfinityModifier(99)}); !errors.Is(err, ErrInfinity) {
		t.Fatalf("unknown pgx error = %v", err)
	}
	if _, err := (InfinityDate{}).DateValue(); !errors.Is(err, calendar.ErrInvalidDate) {
		t.Fatalf("zero DateValue error = %v", err)
	}
	if _, err := NewInfinityDate(InfinityKind(2)).DateValue(); !errors.Is(err, ErrInfinity) {
		t.Fatalf("unknown DateValue error = %v", err)
	}
}

func TestNativePGXDateCodecRoundTrip(t *testing.T) {
	t.Parallel()

	type roundTripper interface {
		pgtype.DateValuer
		pgtype.DateScanner
	}
	values := []roundTripper{
		func() roundTripper { value := NewDate(calendar.MustDate(2024, time.February, 29)); return &value }(),
		func() roundTripper { value := NewInfinityDate(NegativeInfinity); return &value }(),
		func() roundTripper { value := NewInfinityDate(PositiveInfinity); return &value }(),
	}
	codec := pgtype.NewMap()
	for _, value := range values {
		encoded, err := codec.Encode(pgtype.DateOID, pgtype.BinaryFormatCode, value, nil)
		if err != nil {
			t.Fatalf("native encode %T: %v", value, err)
		}
		switch value := value.(type) {
		case *Date:
			var decoded Date
			if err := codec.Scan(pgtype.DateOID, pgtype.BinaryFormatCode, encoded, &decoded); err != nil || decoded.CalendarDate().String() != "2024-02-29" {
				t.Fatalf("native finite scan = %s, %v", decoded.CalendarDate(), err)
			}
		case *InfinityDate:
			var decoded InfinityDate
			if err := codec.Scan(pgtype.DateOID, pgtype.BinaryFormatCode, encoded, &decoded); err != nil || decoded.Kind() != value.Kind() {
				t.Fatalf("native infinity scan = %d, %v", decoded.Kind(), err)
			}
		}
	}
}
