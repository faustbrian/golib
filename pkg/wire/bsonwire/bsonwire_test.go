package bsonwire_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/bsonwire"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type event struct {
	ID        bsonwire.ObjectID `bson:"_id"`
	CreatedAt time.Time         `bson:"created_at"`
	Attempts  int32             `bson:"attempts"`
}

func TestDecodeFixturePreservesObjectIDDatetimeAndNumericWidth(t *testing.T) {
	t.Parallel()
	var got event
	payload := readFixture(t, "event.bson.hex")
	if err := bsonwire.Decode(payload, &got, bsonwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got.ID.Hex() != "00112233445566778899aabb" || !got.CreatedAt.Equal(time.Unix(0, 0).UTC()) || got.Attempts != 42 {
		t.Fatalf("Decode() = %#v", got)
	}
	var untyped bsonwire.M
	if err := bsonwire.Decode(payload, &untyped, bsonwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := untyped["attempts"].(int32); !ok {
		t.Fatalf("Decode() attempts type = %T, want int32", untyped["attempts"])
	}
}

func TestRoundTripPreservesDecimalBinarySubtypeAndRegex(t *testing.T) {
	t.Parallel()

	decimal, err := bson.ParseDecimal128("123.45")
	if err != nil {
		t.Fatal(err)
	}
	value := bsonwire.D{
		{Key: "decimal", Value: decimal},
		{Key: "binary", Value: bsonwire.Binary{Subtype: 0x80, Data: []byte{0x01, 0x02}}},
		{Key: "regex", Value: bsonwire.Regex{Pattern: "^wire$", Options: "i"}},
	}
	payload, err := bsonwire.Encode(value, bsonwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got bsonwire.M
	if err := bsonwire.Decode(payload, &got, bsonwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if got["decimal"] != decimal {
		t.Fatalf("decimal = %#v, want %#v", got["decimal"], decimal)
	}
	if binary, ok := got["binary"].(bsonwire.Binary); !ok || binary.Subtype != 0x80 || !bytes.Equal(binary.Data, []byte{0x01, 0x02}) {
		t.Fatalf("binary = %#v", got["binary"])
	}
	if regex, ok := got["regex"].(bsonwire.Regex); !ok || regex.Pattern != "^wire$" || regex.Options != "i" {
		t.Fatalf("regex = %#v", got["regex"])
	}
}

func TestDecodeRawDocumentPreservesBytes(t *testing.T) {
	t.Parallel()
	payload := readFixture(t, "event.bson.hex")
	var raw bsonwire.Raw
	if err := bsonwire.Decode(payload, &raw, bsonwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, payload) {
		t.Fatalf("Decode() raw = %x, want %x", raw, payload)
	}
}

func TestDecodeRejectsMalformedTrailingDuplicateAndScalarData(t *testing.T) {
	t.Parallel()
	duplicate := mustHex(t, "13000000106100010000001061000200000000")
	for name, payload := range map[string][]byte{
		"malformed length": {0x05, 0x00, 0x00},
		"invalid document": {0x05, 0x00, 0x00, 0x00, 0x01},
		"trailing data":    append(readFixture(t, "event.bson.hex"), 0x00),
		"duplicate key":    duplicate,
		"scalar bytes":     {0x2a, 0x00, 0x00, 0x00},
	} {
		t.Run(name, func(t *testing.T) {
			var got any
			assertKind(t, bsonwire.Decode(payload, &got, bsonwire.DecodeOptions{}), wire.ErrParse)
		})
	}
	var allowed bsonwire.D
	if err := bsonwire.Decode(duplicate, &allowed, bsonwire.DecodeOptions{AllowDuplicateKeys: true}); err != nil {
		t.Fatalf("Decode() allowed duplicate error = %v", err)
	}
	if len(allowed) != 2 {
		t.Fatalf("Decode() allowed duplicate = %#v", allowed)
	}
}

func TestDecodeRejectsNestedDuplicateKeys(t *testing.T) {
	t.Parallel()
	nestedDuplicate := mustHex(t, "20000000036e6573746564001300000010610001000000106100020000000000")
	var got bsonwire.D
	assertKind(t, bsonwire.Decode(nestedDuplicate, &got, bsonwire.DecodeOptions{}), wire.ErrParse)
	if err := bsonwire.Decode(nestedDuplicate, &got, bsonwire.DecodeOptions{AllowDuplicateKeys: true}); err != nil {
		t.Fatalf("Decode() allowed nested duplicate error = %v", err)
	}

	arrayPayload, err := bsonwire.Encode(
		bsonwire.D{{Key: "items", Value: []any{bsonwire.D{{Key: "a", Value: 1}, {Key: "a", Value: 2}}}}},
		bsonwire.EncodeOptions{AllowDuplicateKeys: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	assertKind(t, bsonwire.Decode(arrayPayload, &got, bsonwire.DecodeOptions{}), wire.ErrParse)
}

func TestDecodeProvidesExplicitInteroperabilityOptions(t *testing.T) {
	t.Parallel()
	var objectID struct {
		ID string `bson:"_id"`
	}
	if err := bsonwire.Decode(readFixture(t, "event.bson.hex"), &objectID, bsonwire.DecodeOptions{ObjectIDAsHexString: true}); err != nil {
		t.Fatal(err)
	}
	if objectID.ID != "00112233445566778899aabb" {
		t.Fatalf("Decode() object ID = %q", objectID.ID)
	}

	doubleDoc := mustHex(t, "140000000176616c7565001f85eb51b81e094000")
	var exact struct {
		Value int `bson:"value"`
	}
	assertKind(t, bsonwire.Decode(doubleDoc, &exact, bsonwire.DecodeOptions{}), wire.ErrValidation)
	if err := bsonwire.Decode(doubleDoc, &exact, bsonwire.DecodeOptions{AllowTruncatingDoubles: true}); err != nil {
		t.Fatal(err)
	}
	if exact.Value != 3 {
		t.Fatalf("Decode() truncated value = %d", exact.Value)
	}
}

func TestDecodeRejectsInvalidTargetAndOptions(t *testing.T) {
	t.Parallel()
	var typedNil *event
	for _, test := range []struct {
		target  any
		options bsonwire.DecodeOptions
		kind    error
	}{
		{target: nil, kind: wire.ErrTarget},
		{target: event{}, kind: wire.ErrTarget},
		{target: typedNil, kind: wire.ErrTarget},
		{target: &event{}, options: bsonwire.DecodeOptions{MaxBytes: -1}, kind: wire.ErrValidation},
	} {
		assertKind(t, bsonwire.Decode(readFixture(t, "event.bson.hex"), test.target, test.options), test.kind)
	}
}

func TestDecodeReaderEnforcesLimitsAndClassifiesReadFailures(t *testing.T) {
	t.Parallel()
	var got event
	assertKind(t, bsonwire.DecodeReader(bytes.NewReader(readFixture(t, "event.bson.hex")), &got, bsonwire.DecodeOptions{MaxBytes: 4}), wire.ErrSizeLimit)
	assertKind(t, bsonwire.DecodeReader(errorReader{}, &got, bsonwire.DecodeOptions{}), wire.ErrParse)
	assertKind(t, bsonwire.DecodeReader(nil, &got, bsonwire.DecodeOptions{}), wire.ErrValidation)
	if err := bsonwire.DecodeReader(bytes.NewReader(readFixture(t, "event.bson.hex")), &got, bsonwire.DecodeOptions{MaxBytes: math.MaxInt64}); err != nil {
		t.Fatalf("DecodeReader() maximum limit error = %v", err)
	}
}

func TestEncodeOrderedDocumentsAreDeterministic(t *testing.T) {
	t.Parallel()
	value := bsonwire.D{{Key: "a", Value: int64(1)}, {Key: "z", Value: int64(2)}}
	first, err := bsonwire.Encode(value, bsonwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := bsonwire.Encode(value, bsonwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("Encode() not deterministic: %x != %x", first, second)
	}
	var decoded bsonwire.D
	if err := bsonwire.Decode(first, &decoded, bsonwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if decoded[0].Key != "a" || decoded[1].Key != "z" {
		t.Fatalf("Decode() order = %#v", decoded)
	}
}

func TestEncodeSupportsIntegerAndStructTagOptions(t *testing.T) {
	t.Parallel()
	minimized, err := bsonwire.Encode(bsonwire.D{{Key: "value", Value: int64(42)}}, bsonwire.EncodeOptions{MinimizeIntegerWidth: true})
	if err != nil {
		t.Fatal(err)
	}
	var value bsonwire.M
	if err := bsonwire.Decode(minimized, &value, bsonwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := value["value"].(int32); !ok {
		t.Fatalf("Encode() minimized type = %T, want int32", value["value"])
	}
	tagged := struct {
		Status string `json:"status"`
	}{Status: "ok"}
	payload, err := bsonwire.Encode(tagged, bsonwire.EncodeOptions{UseJSONStructTags: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := bsonwire.Decode(payload, &value, bsonwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if value["status"] != "ok" {
		t.Fatalf("Encode() JSON tag = %#v", value)
	}
}

func TestEncodeRejectsScalarDuplicateAndEncodeFailures(t *testing.T) {
	t.Parallel()
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	_, err := bsonwire.Encode(cyclic, bsonwire.EncodeOptions{})
	assertKind(t, err, wire.ErrEncode)
	_, err = bsonwire.Encode(42, bsonwire.EncodeOptions{})
	assertKind(t, err, wire.ErrUnsupportedFormat)
	_, err = bsonwire.Encode(nil, bsonwire.EncodeOptions{})
	assertKind(t, err, wire.ErrUnsupportedFormat)
	duplicate := bsonwire.D{{Key: "a", Value: 1}, {Key: "a", Value: 2}}
	_, err = bsonwire.Encode(duplicate, bsonwire.EncodeOptions{})
	assertKind(t, err, wire.ErrValidation)
	if _, err := bsonwire.Encode(duplicate, bsonwire.EncodeOptions{AllowDuplicateKeys: true}); err != nil {
		t.Fatalf("Encode() allowed duplicate error = %v", err)
	}
	_, err = bsonwire.Encode(failingMarshaler{}, bsonwire.EncodeOptions{})
	assertKind(t, err, wire.ErrEncode)
	value := &event{Attempts: 1}
	if _, err := bsonwire.Encode(value, bsonwire.EncodeOptions{}); err != nil {
		t.Fatalf("Encode() pointer document error = %v", err)
	}
}

func TestEncodeRawDocumentPreservesBytes(t *testing.T) {
	t.Parallel()
	raw := bsonwire.Raw(readFixture(t, "event.bson.hex"))
	got, err := bsonwire.Encode(raw, bsonwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Fatalf("Encode() raw = %x, want %x", got, raw)
	}
}

func TestEncodeWriterWritesAndClassifiesFailures(t *testing.T) {
	t.Parallel()
	value := bsonwire.D{{Key: "status", Value: "ok"}}
	want, err := bsonwire.Encode(value, bsonwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := bsonwire.EncodeWriter(&output, value, bsonwire.EncodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("EncodeWriter() = %x, want %x", output.Bytes(), want)
	}
	assertKind(t, bsonwire.EncodeWriter(errorWriter{}, value, bsonwire.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, bsonwire.EncodeWriter(shortWriter{}, value, bsonwire.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, bsonwire.EncodeWriter(nil, value, bsonwire.EncodeOptions{}), wire.ErrValidation)
	assertKind(t, bsonwire.EncodeWriter(&bytes.Buffer{}, 42, bsonwire.EncodeOptions{}), wire.ErrUnsupportedFormat)
}

func FuzzDecode(f *testing.F) {
	f.Add(readFixture(f, "event.bson.hex"))
	f.Add([]byte{0x05, 0x00, 0x00})
	f.Add([]byte{})
	f.Add([]byte{0xff, 0xff, 0xff, 0x7f, 0x00})
	f.Add([]byte{0x05, 0x00, 0x00, 0x00, 0x01})
	f.Add(mustHex(f, "13000000106100010000001061000200000000"))
	f.Add(append(readFixture(f, "event.bson.hex"), 0x00))
	f.Fuzz(func(t *testing.T, payload []byte) {
		var target any
		_ = bsonwire.Decode(payload, &target, bsonwire.DecodeOptions{MaxBytes: 64 << 10})
	})
}

func BenchmarkDecode(b *testing.B) {
	payload := readFixture(b, "event.bson.hex")
	b.ReportAllocs()
	for b.Loop() {
		var target event
		if err := bsonwire.Decode(payload, &target, bsonwire.DecodeOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	value := event{ID: bsonwire.ObjectID{1}, CreatedAt: time.Unix(0, 0).UTC(), Attempts: 42}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := bsonwire.Encode(value, bsonwire.EncodeOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func readFixture(tb testing.TB, name string) []byte {
	tb.Helper()
	encoded, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		tb.Fatal(err)
	}
	return mustHex(tb, strings.TrimSpace(string(encoded)))
}

func mustHex(tb testing.TB, value string) []byte {
	tb.Helper()
	payload, err := hex.DecodeString(value)
	if err != nil {
		tb.Fatal(err)
	}
	return payload
}

func assertKind(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want errors.Is(_, %v)", err, target)
	}
	var wireErr *wire.Error
	if !errors.As(err, &wireErr) || wireErr.Format != wire.FormatBSON {
		t.Fatalf("error = %#v, want BSON *wire.Error", err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("fixture reader failed") }

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("fixture writer failed") }

type shortWriter struct{}

func (shortWriter) Write(payload []byte) (int, error) { return len(payload) - 1, nil }

type failingMarshaler struct{}

func (failingMarshaler) MarshalBSON() ([]byte, error) {
	return nil, errors.New("fixture marshal failed")
}
