package ulid_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	identifierulid "github.com/faustbrian/golib/pkg/identifier/ulid"
	oklogulid "github.com/oklog/ulid/v2"
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

func TestOfficialVectorPreservesPostalCompatibleTextAndTime(t *testing.T) {
	const stored = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	id, err := identifierulid.Parse(stored)
	if err != nil {
		t.Fatalf("Parse(): %v", err)
	}
	if id.String() != stored {
		t.Fatalf("stored value changed to %q", id.String())
	}
	inspection := id.Inspect()
	if inspection.Family != identifier.FamilyULID || !inspection.HasTime || !inspection.Sortable ||
		inspection.Timestamp.UnixMilli() != 1469922850259 {
		t.Fatalf("Inspect() = %+v", inspection)
	}
}

func TestParserMatchesMaintainedULIDImplementation(t *testing.T) {
	inputs := []string{
		"00000000000000000000000000",
		"01ARZ3NDEKTSV4RRFFQ69G5FAV",
		"7ZZZZZZZZZZZZZZZZZZZZZZZZZ",
	}
	for _, input := range inputs {
		ours, err := identifierulid.Parse(input)
		if err != nil {
			t.Fatalf("Parse(%q): %v", input, err)
		}
		theirs, err := oklogulid.ParseStrict(input)
		if err != nil {
			t.Fatalf("reference ParseStrict(%q): %v", input, err)
		}
		if ours.Bytes() != [16]byte(theirs) || ours.String() != theirs.String() {
			t.Fatalf("differential mismatch for %q", input)
		}
	}
}

func TestParseRejectsAmbiguousAndNonCanonicalText(t *testing.T) {
	inputs := []string{
		"",
		"01arz3ndektsv4rrffq69g5fav",
		"01ARZ3NDEKTSV4RRFFQ69G5FAI",
		"01ARZ3NDEKTSV4RRFFQ69G5FAO",
		"01ARZ3NDEKTSV4RRFFQ69G5FAU",
		"81ARZ3NDEKTSV4RRFFQ69G5FAV",
		"01ARZ3NDEKTSV4RRFFQ69G5FA",
		"01ARZ3NDEKTSV4RRFFQ69G5FAV0",
	}
	for _, input := range inputs {
		if _, err := identifierulid.Parse(input); !errors.Is(err, identifier.ErrInvalid) {
			t.Errorf("Parse(%q) error = %v", input, err)
		}
	}
}

func TestGeneratorIsMonotonicAndRejectsRollback(t *testing.T) {
	instant := time.UnixMilli(1469922850259)
	clock := &clockSequence{times: []time.Time{instant, instant, instant.Add(-time.Millisecond)}}
	generator := identifierulid.NewGenerator(clock, bytes.NewReader(make([]byte, 10)))

	first, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	second, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := first.String(), "01ARZ3NDEK0000000000000000"; got != want {
		t.Fatalf("first = %s, want %s", got, want)
	}
	if first.Compare(second) >= 0 {
		t.Fatalf("ULIDs are not monotonic: %s, %s", first, second)
	}
	if _, err := generator.New(); !errors.Is(err, identifier.ErrClockRollback) {
		t.Fatalf("rollback error = %v", err)
	}
}

func TestGeneratorReportsEntropyAndOverflow(t *testing.T) {
	instant := time.UnixMilli(1)
	if _, err := identifierulid.NewGenerator(
		&clockSequence{times: []time.Time{instant}}, failingReader{},
	).New(); !errors.Is(err, identifier.ErrEntropy) {
		t.Fatalf("entropy error = %v", err)
	}

	generator := identifierulid.NewGenerator(
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

func TestGeneratorAcceptsTimestampAndCarryBoundaries(t *testing.T) {
	for _, milliseconds := range []int64{0, 1<<48 - 1} {
		generator := identifierulid.NewGenerator(
			&clockSequence{times: []time.Time{time.UnixMilli(milliseconds)}},
			bytes.NewReader(make([]byte, 10)),
		)
		id, err := generator.New()
		if err != nil || id.Inspect().Timestamp.UnixMilli() != milliseconds {
			t.Fatalf("timestamp %d = %s, %v", milliseconds, id, err)
		}
	}

	entropy := append([]byte{0}, bytes.Repeat([]byte{0xff}, 9)...)
	generator := identifierulid.NewGenerator(
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
	if carriedBytes[6] != 1 || !bytes.Equal(carriedBytes[7:], make([]byte, 9)) {
		t.Fatalf("entropy carry = %x", carriedBytes[6:])
	}
}

func TestSerializationAndSQLRoundTrips(t *testing.T) {
	original, _ := identifierulid.Parse("01ARZ3NDEKTSV4RRFFQ69G5FAV")
	if original.LogValue().String() != "[REDACTED]" {
		t.Fatal("ULID log value was not redacted")
	}

	text, _ := original.MarshalText()
	var decoded identifierulid.ID
	if err := decoded.UnmarshalText(text); err != nil || decoded != original {
		t.Fatalf("text round trip = %s, %v", decoded, err)
	}

	binary, _ := original.MarshalBinary()
	if len(binary) != 16 {
		t.Fatalf("binary length = %d", len(binary))
	}
	if err := decoded.UnmarshalBinary(binary); err != nil || decoded != original {
		t.Fatalf("binary round trip = %s, %v", decoded, err)
	}

	data, _ := json.Marshal(original)
	if err := json.Unmarshal(data, &decoded); err != nil || decoded != original {
		t.Fatalf("JSON round trip = %s, %v", decoded, err)
	}

	value, err := original.Value()
	if err != nil || value != original.String() {
		t.Fatalf("Value() = %v, %v", value, err)
	}
	for _, source := range []any{original.String(), []byte(original.String()), binary} {
		if err := decoded.Scan(source); err != nil || decoded != original {
			t.Fatalf("Scan(%T) = %s, %v", source, decoded, err)
		}
	}
}

func TestDecodersRejectInvalidValuesAndHandleNull(t *testing.T) {
	var id identifierulid.ID
	for name, decode := range map[string]func() error{
		"text":   func() error { return id.UnmarshalText([]byte("bad")) },
		"binary": func() error { return id.UnmarshalBinary(make([]byte, 15)) },
		"json":   func() error { return json.Unmarshal([]byte("42"), &id) },
		"scan":   func() error { return id.Scan(42) },
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
	value, err := id.Value()
	if err != nil || value != nil {
		t.Fatalf("zero Value() = %v, %v", value, err)
	}
}
