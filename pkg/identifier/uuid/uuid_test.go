package uuid_test

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type clockSequence struct {
	times []time.Time
	next  int
}

func (clock *clockSequence) Now() time.Time {
	value := clock.times[clock.next]
	if clock.next < len(clock.times)-1 {
		clock.next++
	}

	return value
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestParseOfficialUUIDVectorsAndInspectTime(t *testing.T) {
	vectors := []struct {
		text     string
		version  int
		hasTime  bool
		sortable bool
		time     time.Time
	}{
		{
			text:    "f81d4fae-7dec-11d0-a765-00a0c91e6bf6",
			version: 1,
			hasTime: true,
			time:    time.Date(1997, 2, 3, 17, 43, 12, 216875000, time.UTC),
		},
		{
			text:     "017f22e2-79b0-7cc3-98c4-dc0c0c07398f",
			version:  7,
			hasTime:  true,
			sortable: true,
			time:     time.UnixMilli(1645557742000).UTC(),
		},
		{
			text:    "00000000-0000-8000-8000-000000000000",
			version: 8,
		},
	}

	for _, vector := range vectors {
		t.Run(vector.text, func(t *testing.T) {
			id, err := identifieruuid.Parse(vector.text)
			if err != nil {
				t.Fatalf("Parse(): %v", err)
			}
			if id.String() != vector.text || id.Version() != vector.version {
				t.Fatalf("parsed UUID = %s v%d", id, id.Version())
			}

			inspection := id.Inspect()
			if inspection.Family != identifier.FamilyUUID ||
				inspection.HasTime != vector.hasTime ||
				inspection.Sortable != vector.sortable ||
				!inspection.Timestamp.Equal(vector.time) {
				t.Fatalf("Inspect() = %+v", inspection)
			}
		})
	}
}

func TestParseRejectsNonCanonicalAndInvalidUUIDs(t *testing.T) {
	inputs := []string{
		"",
		"F81D4FAE-7DEC-11D0-A765-00A0C91E6BF6",
		"{f81d4fae-7dec-11d0-a765-00a0c91e6bf6}",
		"f81d4fae7dec11d0a76500a0c91e6bf6",
		"f81d4fae-7dec-01d0-a765-00a0c91e6bf6",
		"f81d4fae-7dec-11d0-6765-00a0c91e6bf6",
		"f81d4fae-7dec-91d0-a765-00a0c91e6bf6",
		"f81d4fae-7dec-11d0-a765-00a0c91e6bfz",
	}

	for _, input := range inputs {
		if _, err := identifieruuid.Parse(input); !errors.Is(err, identifier.ErrInvalid) {
			t.Errorf("Parse(%q) error = %v, want ErrInvalid", input, err)
		}
	}
}

func TestV4GenerationOwnsEntropyAndSetsRFCBits(t *testing.T) {
	generator := identifieruuid.NewV4Generator(bytes.NewReader(bytes.Repeat([]byte{0xff}, 16)))
	id, err := generator.New()
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if got, want := id.String(), "ffffffff-ffff-4fff-bfff-ffffffffffff"; got != want {
		t.Fatalf("New() = %s, want %s", got, want)
	}

	if _, err := identifieruuid.NewV4Generator(failingReader{}).New(); !errors.Is(err, identifier.ErrEntropy) {
		t.Fatalf("entropy error = %v", err)
	}
}

func TestV7GenerationIsMonotonicAndRejectsClockRollback(t *testing.T) {
	instant := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	clock := &clockSequence{times: []time.Time{instant, instant, instant.Add(-time.Millisecond)}}
	generator := identifieruuid.NewV7Generator(clock, bytes.NewReader(make([]byte, 10)))

	first, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	second, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}

	if got, want := first.String(), "018cc251-f400-7000-8000-000000000000"; got != want {
		t.Fatalf("first UUID = %s, want %s", got, want)
	}
	if first.Compare(second) >= 0 {
		t.Fatalf("UUIDs are not monotonic: %s, %s", first, second)
	}
	if _, err := generator.New(); !errors.Is(err, identifier.ErrClockRollback) {
		t.Fatalf("rollback error = %v", err)
	}
}

func TestV7GenerationReportsEntropyAndMonotonicOverflow(t *testing.T) {
	instant := time.UnixMilli(1)
	if _, err := identifieruuid.NewV7Generator(
		&clockSequence{times: []time.Time{instant}}, failingReader{},
	).New(); !errors.Is(err, identifier.ErrEntropy) {
		t.Fatalf("entropy error = %v", err)
	}

	generator := identifieruuid.NewV7Generator(
		&clockSequence{times: []time.Time{instant, instant}},
		bytes.NewReader(bytes.Repeat([]byte{0xff}, 10)),
	)
	if _, err := generator.New(); err != nil {
		t.Fatal(err)
	}
	if _, err := generator.New(); !errors.Is(err, identifier.ErrOverflow) {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestV7GenerationAcceptsTimestampAndCarryBoundaries(t *testing.T) {
	for _, milliseconds := range []int64{0, 1<<48 - 1} {
		generator := identifieruuid.NewV7Generator(
			&clockSequence{times: []time.Time{time.UnixMilli(milliseconds)}},
			bytes.NewReader(make([]byte, 10)),
		)
		id, err := generator.New()
		if err != nil || id.Inspect().Timestamp.UnixMilli() != milliseconds {
			t.Fatalf("timestamp %d = %s, %v", milliseconds, id, err)
		}
	}

	entropy := append([]byte{0, 0, 0}, bytes.Repeat([]byte{0xff}, 7)...)
	generator := identifieruuid.NewV7Generator(
		&clockSequence{times: []time.Time{time.UnixMilli(1), time.UnixMilli(1)}},
		bytes.NewReader(entropy),
	)
	if _, err := generator.New(); err != nil {
		t.Fatal(err)
	}
	carried, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	carriedBytes := carried.Bytes()
	if carriedBytes[8] != 0x81 || !bytes.Equal(carriedBytes[9:], make([]byte, 7)) {
		t.Fatalf("UUIDv7 carry = %x", carriedBytes[8:])
	}
}

func TestUUIDSerializationAndDatabaseRoundTrips(t *testing.T) {
	original, _ := identifieruuid.Parse("017f22e2-79b0-7cc3-98c4-dc0c0c07398f")
	if original.LogValue().String() != "[REDACTED]" {
		t.Fatal("UUID log value was not redacted")
	}

	text, _ := original.MarshalText()
	var fromText identifieruuid.ID
	if err := fromText.UnmarshalText(text); err != nil || fromText != original {
		t.Fatalf("text round trip = %s, %v", fromText, err)
	}

	binary, _ := original.MarshalBinary()
	if len(binary) != 16 {
		t.Fatalf("binary length = %d", len(binary))
	}
	var fromBinary identifieruuid.ID
	if err := fromBinary.UnmarshalBinary(binary); err != nil || fromBinary != original {
		t.Fatalf("binary round trip = %s, %v", fromBinary, err)
	}

	data, _ := json.Marshal(original)
	var fromJSON identifieruuid.ID
	if err := json.Unmarshal(data, &fromJSON); err != nil || fromJSON != original {
		t.Fatalf("JSON round trip = %s, %v", fromJSON, err)
	}

	value, err := original.Value()
	if err != nil || value != driver.Value(original.String()) {
		t.Fatalf("Value() = %v, %v", value, err)
	}

	for _, source := range []any{original.String(), []byte(original.String()), binary} {
		var scanned identifieruuid.ID
		if scanErr := scanned.Scan(source); scanErr != nil || scanned != original {
			t.Fatalf("Scan(%T) = %s, %v", source, scanned, scanErr)
		}
	}

	pgValue, err := original.UUIDValue()
	if err != nil || !pgValue.Valid || pgValue.Bytes != original.Bytes() {
		t.Fatalf("UUIDValue() = %+v, %v", pgValue, err)
	}
	var fromPG identifieruuid.ID
	if err := fromPG.ScanUUID(pgtype.UUID{Bytes: original.Bytes(), Valid: true}); err != nil || fromPG != original {
		t.Fatalf("PostgreSQL round trip = %s, %v", fromPG, err)
	}
}

func TestUUIDDecodersRejectBadTypesAndLengths(t *testing.T) {
	var id identifieruuid.ID
	for name, decode := range map[string]func() error{
		"text":      func() error { return id.UnmarshalText([]byte("bad")) },
		"binary":    func() error { return id.UnmarshalBinary(make([]byte, 15)) },
		"json":      func() error { return json.Unmarshal([]byte("42"), &id) },
		"scan type": func() error { return id.Scan(42) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := decode(); err == nil {
				t.Fatal("expected error")
			}
		})
	}

	if err := id.Scan(nil); err != nil || !id.IsZero() {
		t.Fatalf("Scan(nil) = %s, %v", id, err)
	}
	if err := id.ScanUUID(pgtype.UUID{}); err != nil || !id.IsZero() {
		t.Fatalf("ScanUUID(NULL) = %s, %v", id, err)
	}
	value, err := id.Value()
	if err != nil || value != nil {
		t.Fatalf("zero Value() = %v, %v", value, err)
	}
}
