package ksuid_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	identifier "github.com/faustbrian/golib/pkg/identifier"
	identifierksuid "github.com/faustbrian/golib/pkg/identifier/ksuid"
	segmentksuid "github.com/segmentio/ksuid"
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

func TestReferenceVectorAndDifferentialParsing(t *testing.T) {
	const text = "0ujtsYcgvSTl8PAuAdqWYSMnLOv"
	id, err := identifierksuid.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := segmentksuid.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	if id.String() != text || id.Bytes() != [20]byte(reference) {
		t.Fatalf("Parse() = %s, %x", id, id.Bytes())
	}
	inspection := id.Inspect()
	if inspection.Family != identifier.FamilyKSUID || !inspection.HasTime || !inspection.Sortable ||
		inspection.Timestamp.Unix() != 1507608047 {
		t.Fatalf("Inspect() = %+v", inspection)
	}
}

func TestParserRejectsMalformedAndNonCanonicalValues(t *testing.T) {
	for _, input := range []string{
		"", "0ujtsYcgvSTl8PAuAdqWYSMnLO", "0ujtsYcgvSTl8PAuAdqWYSMnLOv0",
		"0ujtsYcgvSTl8PAuAdqWYSMnLO!", "aWgEPTl1tmebfsQzFP4bxwgy80W",
	} {
		if _, err := identifierksuid.Parse(input); !errors.Is(err, identifier.ErrInvalid) {
			t.Errorf("Parse(%q) error = %v", input, err)
		}
	}
}

func TestGeneratorIsMonotonicAndReportsFailures(t *testing.T) {
	instant := time.Unix(1507608047, 0)
	clock := &clockSequence{times: []time.Time{instant, instant, instant.Add(-time.Second)}}
	payload, _ := hex.DecodeString("b5a1cd34b5f99d1154fb6853345c9735")
	generator := identifierksuid.NewGenerator(clock, bytes.NewReader(payload))

	first, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	second, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	if first.String() != "0ujtsYcgvSTl8PAuAdqWYSMnLOv" || first.Compare(second) >= 0 {
		t.Fatalf("generated KSUIDs = %s, %s", first, second)
	}
	if _, err := generator.New(); !errors.Is(err, identifier.ErrClockRollback) {
		t.Fatalf("rollback error = %v", err)
	}

	if _, err := identifierksuid.NewGenerator(
		&clockSequence{times: []time.Time{instant}}, failingReader{},
	).New(); !errors.Is(err, identifier.ErrEntropy) {
		t.Fatalf("entropy error = %v", err)
	}

	overflow := identifierksuid.NewGenerator(
		&clockSequence{times: []time.Time{instant, instant}},
		bytes.NewReader(bytes.Repeat([]byte{0xff}, 16)),
	)
	if _, err := overflow.New(); err != nil {
		t.Fatal(err)
	}
	if _, err := overflow.New(); !errors.Is(err, identifier.ErrOverflow) {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestGeneratorAcceptsTimestampAndCarryBoundaries(t *testing.T) {
	const epoch = int64(1_400_000_000)
	for _, seconds := range []int64{epoch, epoch + int64(^uint32(0))} {
		generator := identifierksuid.NewGenerator(
			&clockSequence{times: []time.Time{time.Unix(seconds, 0)}},
			bytes.NewReader(make([]byte, 16)),
		)
		id, err := generator.New()
		if err != nil || id.Inspect().Timestamp.Unix() != seconds {
			t.Fatalf("timestamp %d = %s, %v", seconds, id, err)
		}
	}

	payload := append([]byte{0}, bytes.Repeat([]byte{0xff}, 15)...)
	instant := time.Unix(epoch+1, 0)
	generator := identifierksuid.NewGenerator(
		&clockSequence{times: []time.Time{instant, instant}}, bytes.NewReader(payload),
	)
	if _, err := generator.New(); err != nil {
		t.Fatal(err)
	}
	carried, err := generator.New()
	if err != nil {
		t.Fatal(err)
	}
	carriedBytes := carried.Bytes()
	if carriedBytes[4] != 1 || !bytes.Equal(carriedBytes[5:], make([]byte, 15)) {
		t.Fatalf("KSUID carry = %x", carriedBytes[4:])
	}
}

func TestSerializationAndSQLRoundTrips(t *testing.T) {
	original, _ := identifierksuid.Parse("0ujtsYcgvSTl8PAuAdqWYSMnLOv")
	if original.LogValue().String() != "[REDACTED]" {
		t.Fatal("KSUID log value was not redacted")
	}
	text, _ := original.MarshalText()
	var decoded identifierksuid.ID
	if err := decoded.UnmarshalText(text); err != nil || decoded != original {
		t.Fatalf("text round trip = %s, %v", decoded, err)
	}
	binary, _ := original.MarshalBinary()
	if len(binary) != 20 {
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
	var id identifierksuid.ID
	for name, decode := range map[string]func() error{
		"text":   func() error { return id.UnmarshalText([]byte("bad")) },
		"binary": func() error { return id.UnmarshalBinary(make([]byte, 19)) },
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
