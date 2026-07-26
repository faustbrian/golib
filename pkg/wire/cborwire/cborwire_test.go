package cborwire_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/cborwire"
	"github.com/fxamacker/cbor/v2"
)

type shipment struct {
	ID      int    `cbor:"id"`
	Status  string `cbor:"status"`
	Carrier string `cbor:"carrier"`
}

func TestDecodeFixture(t *testing.T) {
	t.Parallel()
	var got shipment
	if err := cborwire.Decode(readFixture(t, "shipment.cbor.hex"), &got, cborwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got != (shipment{ID: 42, Status: "ready", Carrier: "postnord"}) {
		t.Fatalf("Decode() = %#v", got)
	}
}

func TestDecodeRejectsMalformedTrailingAndDuplicateData(t *testing.T) {
	t.Parallel()
	for name, payload := range map[string][]byte{
		"malformed":     {0x65, 'x'},
		"trailing item": {0x01, 0x02},
		"duplicate key": {0xa2, 0x61, 'a', 0x01, 0x61, 'a', 0x02},
	} {
		t.Run(name, func(t *testing.T) {
			var got any
			assertKind(t, cborwire.Decode(payload, &got, cborwire.DecodeOptions{}), wire.ErrParse)
		})
	}
}

func TestDecodeDefinesTagsAndIndefiniteLengthBehavior(t *testing.T) {
	t.Parallel()
	taggedEpoch := []byte{0xc1, 0x00}
	var timestamp time.Time
	assertKind(t, cborwire.Decode(taggedEpoch, &timestamp, cborwire.DecodeOptions{}), wire.ErrUnsupportedFormat)
	if err := cborwire.Decode(taggedEpoch, &timestamp, cborwire.DecodeOptions{AllowTags: true}); err != nil {
		t.Fatalf("Decode() tagged time error = %v", err)
	}
	if !timestamp.Equal(time.Unix(0, 0).UTC()) {
		t.Fatalf("Decode() tagged time = %v", timestamp)
	}

	indefinite := []byte{0x9f, 0x01, 0x02, 0xff}
	var values []int
	assertKind(t, cborwire.Decode(indefinite, &values, cborwire.DecodeOptions{}), wire.ErrUnsupportedFormat)
	if err := cborwire.Decode(indefinite, &values, cborwire.DecodeOptions{AllowIndefiniteLength: true}); err != nil {
		t.Fatalf("Decode() indefinite error = %v", err)
	}
	if len(values) != 2 || values[0] != 1 || values[1] != 2 {
		t.Fatalf("Decode() indefinite = %#v", values)
	}
}

func TestDecodePreservesSimpleValuesAndBignums(t *testing.T) {
	t.Parallel()

	var simple cbor.SimpleValue
	if err := cborwire.Decode([]byte{0xf0}, &simple, cborwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if simple != 16 {
		t.Fatalf("simple value = %d, want 16", simple)
	}

	bignum := []byte{0xc2, 0x42, 0x01, 0x00}
	var integer big.Int
	assertKind(t, cborwire.Decode(bignum, &integer, cborwire.DecodeOptions{}), wire.ErrUnsupportedFormat)
	if err := cborwire.Decode(bignum, &integer, cborwire.DecodeOptions{AllowTags: true}); err != nil {
		t.Fatal(err)
	}
	if integer.Cmp(big.NewInt(256)) != 0 {
		t.Fatalf("bignum = %s, want 256", &integer)
	}
}

func TestDecodeEnforcesUnknownFieldsNumericAndResourceLimits(t *testing.T) {
	t.Parallel()
	unknown := []byte{0xa1, 0x67, 'u', 'n', 'k', 'n', 'o', 'w', 'n', 0xf5}
	var got shipment
	assertKind(t, cborwire.Decode(unknown, &got, cborwire.DecodeOptions{DisallowUnknownFields: true}), wire.ErrValidation)
	var signed int64
	assertKind(t, cborwire.Decode([]byte{0x1b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, &signed, cborwire.DecodeOptions{}), wire.ErrValidation)

	deep := []byte{0x81, 0x81, 0x81, 0x81, 0x81, 0x00}
	var nested any
	assertKind(t, cborwire.Decode(deep, &nested, cborwire.DecodeOptions{MaxNestedLevels: 4}), wire.ErrSizeLimit)
	largeArray := append([]byte{0x98, 0x11}, bytes.Repeat([]byte{0x00}, 17)...)
	assertKind(t, cborwire.Decode(largeArray, &nested, cborwire.DecodeOptions{MaxArrayElements: 16}), wire.ErrSizeLimit)
	largeMap := []byte{0xb1}
	for index := byte(0); index < 17; index++ {
		largeMap = append(largeMap, index, index)
	}
	assertKind(t, cborwire.Decode(largeMap, &nested, cborwire.DecodeOptions{MaxMapPairs: 16}), wire.ErrSizeLimit)
}

func TestDecodeRejectsInvalidTargetAndOptions(t *testing.T) {
	t.Parallel()
	var typedNil *shipment
	for _, test := range []struct {
		target  any
		options cborwire.DecodeOptions
		kind    error
	}{
		{target: nil, kind: wire.ErrTarget},
		{target: shipment{}, kind: wire.ErrTarget},
		{target: typedNil, kind: wire.ErrTarget},
		{target: &shipment{}, options: cborwire.DecodeOptions{MaxBytes: -1}, kind: wire.ErrValidation},
		{target: &shipment{}, options: cborwire.DecodeOptions{MaxNestedLevels: 3}, kind: wire.ErrValidation},
		{target: &shipment{}, options: cborwire.DecodeOptions{MaxArrayElements: 1}, kind: wire.ErrValidation},
		{target: &shipment{}, options: cborwire.DecodeOptions{MaxMapPairs: 1}, kind: wire.ErrValidation},
	} {
		assertKind(t, cborwire.Decode([]byte{0xa0}, test.target, test.options), test.kind)
	}
}

func TestDecodeReaderEnforcesLimitsAndClassifiesReadFailures(t *testing.T) {
	t.Parallel()
	var got shipment
	assertKind(t, cborwire.DecodeReader(bytes.NewReader(readFixture(t, "shipment.cbor.hex")), &got, cborwire.DecodeOptions{MaxBytes: 4}), wire.ErrSizeLimit)
	assertKind(t, cborwire.DecodeReader(errorReader{}, &got, cborwire.DecodeOptions{}), wire.ErrParse)
	assertKind(t, cborwire.DecodeReader(nil, &got, cborwire.DecodeOptions{}), wire.ErrValidation)
	if err := cborwire.DecodeReader(bytes.NewReader([]byte{0xa0}), &got, cborwire.DecodeOptions{MaxBytes: math.MaxInt64}); err != nil {
		t.Fatalf("DecodeReader() maximum limit error = %v", err)
	}
}

func TestEncodeUsesExplicitDeterministicProfiles(t *testing.T) {
	t.Parallel()
	value := map[string]int{"z": 2, "a": 1}
	first, err := cborwire.Encode(value, cborwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := cborwire.Encode(value, cborwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || hex.EncodeToString(first) != "a2616101617a02" {
		t.Fatalf("Encode() = %x and %x", first, second)
	}
	for _, profile := range []cborwire.DeterministicProfile{cborwire.CoreDeterministic, cborwire.CTAP2Deterministic} {
		if _, err := cborwire.Encode(value, cborwire.EncodeOptions{Profile: profile}); err != nil {
			t.Fatalf("Encode() profile %v error = %v", profile, err)
		}
	}
	_, err = cborwire.Encode(value, cborwire.EncodeOptions{Profile: 99})
	assertKind(t, err, wire.ErrValidation)
	preferredInteger, err := cborwire.Encode(uint64(23), cborwire.EncodeOptions{})
	if err != nil || !bytes.Equal(preferredInteger, []byte{0x17}) {
		t.Fatalf("Encode() preferred integer = %x, %v", preferredInteger, err)
	}
	preferredFloat, err := cborwire.Encode(1.5, cborwire.EncodeOptions{})
	if err != nil || !bytes.Equal(preferredFloat, []byte{0xf9, 0x3e, 0x00}) {
		t.Fatalf("Encode() preferred float = %x, %v", preferredFloat, err)
	}
}

func TestEncodeDefinesTagAndUnsupportedBehavior(t *testing.T) {
	t.Parallel()
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	_, err := cborwire.Encode(cyclic, cborwire.EncodeOptions{})
	assertKind(t, err, wire.ErrEncode)
	timestamp := time.Unix(0, 0).UTC()
	plain, err := cborwire.Encode(timestamp, cborwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) == 0 || plain[0] == 0xc1 {
		t.Fatalf("Encode() unexpectedly tagged time: %x", plain)
	}
	tagged, err := cborwire.Encode(timestamp, cborwire.EncodeOptions{AllowTags: true, TimeTag: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(tagged) == 0 || tagged[0] != 0xc1 {
		t.Fatalf("Encode() tagged time = %x", tagged)
	}
	_, err = cborwire.Encode(cbor.Tag{Number: 42, Content: "value"}, cborwire.EncodeOptions{})
	assertKind(t, err, wire.ErrUnsupportedFormat)
	_, err = cborwire.Encode(failingMarshaler{}, cborwire.EncodeOptions{})
	assertKind(t, err, wire.ErrEncode)
	_, err = cborwire.Encode(timestamp, cborwire.EncodeOptions{TimeTag: true})
	assertKind(t, err, wire.ErrValidation)
}

func TestEncodeWriterWritesAndClassifiesFailures(t *testing.T) {
	t.Parallel()
	value := map[string]int{"status": 1}
	want, err := cborwire.Encode(value, cborwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := cborwire.EncodeWriter(&output, value, cborwire.EncodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("EncodeWriter() = %x, want %x", output.Bytes(), want)
	}
	assertKind(t, cborwire.EncodeWriter(errorWriter{}, value, cborwire.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, cborwire.EncodeWriter(shortWriter{}, value, cborwire.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, cborwire.EncodeWriter(nil, value, cborwire.EncodeOptions{}), wire.ErrValidation)
	assertKind(t, cborwire.EncodeWriter(&bytes.Buffer{}, cbor.Tag{Number: 42}, cborwire.EncodeOptions{}), wire.ErrUnsupportedFormat)
}

func FuzzDecode(f *testing.F) {
	f.Add(readFixture(f, "shipment.cbor.hex"))
	f.Add([]byte{0x65, 'x'})
	f.Add([]byte{})
	f.Add([]byte{0x01, 0x02})
	f.Add([]byte{0xa2, 0x61, 'a', 0x01, 0x61, 'a', 0x02})
	f.Add([]byte{0x9f, 0x01, 0xff})
	f.Add([]byte{0xc2, 0x42, 0x01, 0x00})
	f.Add([]byte{0x9a, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{0x81, 0x81, 0x81, 0x81, 0x81, 0x00})
	f.Fuzz(func(t *testing.T, payload []byte) {
		var target any
		_ = cborwire.Decode(payload, &target, cborwire.DecodeOptions{MaxBytes: 64 << 10})
	})
}

func BenchmarkDecode(b *testing.B) {
	payload := readFixture(b, "shipment.cbor.hex")
	b.ReportAllocs()
	for b.Loop() {
		var target shipment
		if err := cborwire.Decode(payload, &target, cborwire.DecodeOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	value := shipment{ID: 42, Status: "ready", Carrier: "postnord"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := cborwire.Encode(value, cborwire.EncodeOptions{}); err != nil {
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
	payload, err := hex.DecodeString(strings.TrimSpace(string(encoded)))
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
	if !errors.As(err, &wireErr) || wireErr.Format != wire.FormatCBOR {
		t.Fatalf("error = %#v, want CBOR *wire.Error", err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("fixture reader failed") }

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("fixture writer failed") }

type shortWriter struct{}

func (shortWriter) Write(payload []byte) (int, error) { return len(payload) - 1, nil }

type failingMarshaler struct{}

func (failingMarshaler) MarshalCBOR() ([]byte, error) {
	return nil, errors.New("fixture marshal failed")
}
