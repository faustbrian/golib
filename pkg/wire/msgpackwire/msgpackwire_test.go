package msgpackwire_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/wire"
	"github.com/faustbrian/golib/pkg/wire/msgpackwire"
	"github.com/vmihailenco/msgpack/v5"
)

type shipment struct {
	ID      int    `msgpack:"id"`
	Status  string `msgpack:"status"`
	Carrier string `msgpack:"carrier"`
}

type EmbeddedNumber struct {
	Value uint8
}

type NamedNumber uint8

type CustomStructNumber struct {
	Value uint8
}

func (number CustomStructNumber) MarshalMsgpack() ([]byte, error) {
	return msgpack.Marshal(number.Value)
}

func (number *CustomStructNumber) UnmarshalMsgpack(payload []byte) error {
	return msgpack.Unmarshal(payload, &number.Value)
}

func TestDecodeFixture(t *testing.T) {
	t.Parallel()
	var got shipment
	if err := msgpackwire.Decode(readFixture(t, "shipment.msgpack.hex"), &got, msgpackwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if got != (shipment{ID: 42, Status: "ready", Carrier: "postnord"}) {
		t.Fatalf("Decode() = %#v", got)
	}
}

func TestDecodeRejectsMalformedTrailingAndUnknownExtensions(t *testing.T) {
	t.Parallel()
	for name, payload := range map[string][]byte{
		"malformed":              {0xd9, 0x05, 'x'},
		"truncated array header": {0xdc},
		"truncated map header":   {0xde},
		"missing map key":        {0x81},
		"missing map value":      {0x81, 0xa1, 'a'},
		"trailing object":        {0x01, 0x02},
		"unknown extension":      {0xd4, 0x2a, 0x00},
	} {
		t.Run(name, func(t *testing.T) {
			var got any
			kind := wire.ErrParse
			if name == "unknown extension" {
				kind = wire.ErrUnsupportedFormat
			}
			assertKind(t, msgpackwire.Decode(payload, &got, msgpackwire.DecodeOptions{}), kind)
		})
	}
}

func TestDecodeRejectsImpossibleCollectionLengthsBeforeAllocation(t *testing.T) {
	t.Parallel()

	for name, payload := range map[string][]byte{
		"map":   {0xdf, 0xff, 0xff, 0xff, 0xff},
		"array": {0xdd, 0xff, 0xff, 0xff, 0xff},
	} {
		t.Run(name, func(t *testing.T) {
			var target any
			assertKind(t, msgpackwire.Decode(payload, &target, msgpackwire.DecodeOptions{}), wire.ErrSizeLimit)
		})
	}
}

func TestDecodeEnforcesDefaultStructuralLimits(t *testing.T) {
	t.Parallel()

	deep := append(bytes.Repeat([]byte{0x91}, msgpackwire.DefaultMaxNestedLevels+1), 0xc0)
	assertKind(t, msgpackwire.Decode(deep, new(any), msgpackwire.DecodeOptions{}), wire.ErrSizeLimit)
	deepMap := append(bytes.Repeat([]byte{0x81, 0xc0}, msgpackwire.DefaultMaxNestedLevels+1), 0xc0)
	assertKind(t, msgpackwire.Decode(deepMap, new(any), msgpackwire.DecodeOptions{}), wire.ErrSizeLimit)

	largeArray := []byte{0xdd, 0x00, 0x02, 0x00, 0x01}
	largeArray = append(largeArray, bytes.Repeat([]byte{0xc0}, msgpackwire.DefaultMaxArrayElements+1)...)
	assertKind(t, msgpackwire.Decode(largeArray, new(any), msgpackwire.DecodeOptions{}), wire.ErrSizeLimit)

	largeMap := []byte{0xdf, 0x00, 0x01, 0x00, 0x01}
	largeMap = append(largeMap, bytes.Repeat([]byte{0xc0}, 2*(msgpackwire.DefaultMaxMapPairs+1))...)
	assertKind(t, msgpackwire.Decode(largeMap, new(any), msgpackwire.DecodeOptions{}), wire.ErrSizeLimit)
}

func TestDecodeValidatesStructuralLimitOptions(t *testing.T) {
	t.Parallel()

	for _, options := range []msgpackwire.DecodeOptions{
		{MaxNestedLevels: -1},
		{MaxArrayElements: -1},
		{MaxMapPairs: -1},
	} {
		assertKind(t, msgpackwire.Decode([]byte{0x80}, new(any), options), wire.ErrValidation)
	}

	var target any
	if err := msgpackwire.Decode([]byte{0x91, 0xc0}, &target, msgpackwire.DecodeOptions{
		MaxNestedLevels:  1,
		MaxArrayElements: 1,
		MaxMapPairs:      1,
	}); err != nil {
		t.Fatalf("Decode() with explicit structural limits error = %v", err)
	}
}

func TestDecodeDefinesMapKeyBehavior(t *testing.T) {
	t.Parallel()
	payload := []byte{0x81, 0x01, 0xa3, 'o', 'n', 'e'}
	var untyped any
	assertKind(t, msgpackwire.Decode(payload, &untyped, msgpackwire.DecodeOptions{}), wire.ErrUnsupportedFormat)
	var typed map[int]string
	if err := msgpackwire.Decode(payload, &typed, msgpackwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() typed map error = %v", err)
	}
	if typed[1] != "one" {
		t.Fatalf("Decode() typed map = %#v", typed)
	}
	compositeKey := []byte{0x81, 0x91, 0x01, 0xa1, 'x'}
	assertKind(t, msgpackwire.Decode(compositeKey, &untyped, msgpackwire.DecodeOptions{}), wire.ErrUnsupportedFormat)
}

func TestDecodeRejectsDuplicateMapKeysByDefault(t *testing.T) {
	t.Parallel()

	payload := []byte{0x82, 0xa1, 'a', 0x01, 0xa1, 'a', 0x02}
	var target map[string]int
	assertKind(t, msgpackwire.Decode(payload, &target, msgpackwire.DecodeOptions{}), wire.ErrParse)
	if err := msgpackwire.Decode(payload, &target, msgpackwire.DecodeOptions{AllowDuplicateKeys: true}); err != nil {
		t.Fatalf("Decode() with duplicate opt-in error = %v", err)
	}
	if target["a"] != 2 {
		t.Fatalf("Decode() duplicate value = %d, want 2", target["a"])
	}

	nested := []byte{0x81, 0xa1, 'n', 0x82, 0x01, 0x01, 0x01, 0x02}
	assertKind(t, msgpackwire.Decode(nested, new(any), msgpackwire.DecodeOptions{}), wire.ErrParse)
	compositeKey := []byte{0x81, 0x91, 0x82, 0xa1, 'a', 0x01, 0xa1, 'a', 0x02, 0xc0}
	assertKind(t, msgpackwire.Decode(compositeKey, new(any), msgpackwire.DecodeOptions{}), wire.ErrParse)
}

func TestDecodePreservesTimestampAndIntegerWidth(t *testing.T) {
	t.Parallel()
	wantTime := time.Date(2026, 7, 14, 9, 30, 0, 123000000, time.UTC)
	payload, err := msgpackwire.Encode(wantTime, msgpackwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var gotTime time.Time
	if err := msgpackwire.Decode(payload, &gotTime, msgpackwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !gotTime.Equal(wantTime) {
		t.Fatalf("Decode() time = %v, want %v", gotTime, wantTime)
	}

	integer := []byte{0xcd, 0x00, 0x2a}
	var exact any
	if err := msgpackwire.Decode(integer, &exact, msgpackwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := exact.(uint16); !ok {
		t.Fatalf("Decode() integer type = %T, want uint16", exact)
	}
	var loose any
	if err := msgpackwire.Decode(integer, &loose, msgpackwire.DecodeOptions{NormalizeNumericWidths: true}); err != nil {
		t.Fatal(err)
	}
	if _, ok := loose.(uint64); !ok {
		t.Fatalf("Decode() loose integer type = %T, want uint64", loose)
	}
	var narrow uint8
	assertKind(t, msgpackwire.Decode([]byte{0xcd, 0x01, 0x2c}, &narrow, msgpackwire.DecodeOptions{}), wire.ErrValidation)
	assertKind(t, msgpackwire.Decode([]byte{0xff}, &narrow, msgpackwire.DecodeOptions{}), wire.ErrValidation)
	if err := msgpackwire.Decode([]byte{0x01}, &narrow, msgpackwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() positive signed integer error = %v", err)
	}
	var signed int64
	if err := msgpackwire.Decode([]byte{0xcd, 0x00, 0x2a}, &signed, msgpackwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() unsigned into signed error = %v", err)
	}
	assertKind(t, msgpackwire.Decode([]byte{0xcf, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, &signed, msgpackwire.DecodeOptions{}), wire.ErrValidation)
	var narrowFloat float32
	floatPayload, err := msgpackwire.Encode(1.1, msgpackwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertKind(t, msgpackwire.Decode(floatPayload, &narrowFloat, msgpackwire.DecodeOptions{}), wire.ErrValidation)
	exactFloat, err := msgpackwire.Encode(float32(1.5), msgpackwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := msgpackwire.Decode(exactFloat, &narrowFloat, msgpackwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() exact float error = %v", err)
	}
	var widenedFloat float64
	if err := msgpackwire.Decode([]byte{0x01}, &widenedFloat, msgpackwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() integer float error = %v", err)
	}
}

func TestDecodeValidatesNestedNumericAssignments(t *testing.T) {
	t.Parallel()
	type nested struct {
		Values  map[string]uint8 `msgpack:"values"`
		Items   []int8           `msgpack:"items"`
		Default uint8            `msgpack:"default"`
		Ignore  uint8            `msgpack:"-"`
		_       uint8
	}
	value := map[string]any{
		"values":  map[string]any{"ok": uint16(42)},
		"items":   []any{int16(1), int16(2)},
		"default": uint16(3),
	}
	payload, err := msgpackwire.Encode(value, msgpackwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var got nested
	if err := msgpackwire.Decode(payload, &got, msgpackwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() nested payload %x error = %v", payload, err)
	}
	if got.Values["ok"] != 42 || !reflect.DeepEqual(got.Items, []int8{1, 2}) || got.Default != 3 {
		t.Fatalf("Decode() nested = %#v", got)
	}

	overflow, err := msgpackwire.Encode(map[string]any{"items": []any{int16(300)}}, msgpackwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertKind(t, msgpackwire.Decode(overflow, &got, msgpackwire.DecodeOptions{}), wire.ErrValidation)
	mapOverflow, err := msgpackwire.Encode(map[string]any{"values": map[string]any{"bad": uint16(300)}}, msgpackwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertKind(t, msgpackwire.Decode(mapOverflow, &got, msgpackwire.DecodeOptions{}), wire.ErrValidation)
	type untagged struct{ Plain int8 }
	plainPayload, err := msgpackwire.Encode(map[string]any{"Plain": int16(7)}, msgpackwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var plain untagged
	if err := msgpackwire.Decode(plainPayload, &plain, msgpackwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() untagged error = %v", err)
	}
	var wrongMap map[string]uint8
	assertKind(t, msgpackwire.Decode([]byte{0x01}, &wrongMap, msgpackwire.DecodeOptions{}), wire.ErrValidation)
	var wrongSlice []uint8
	assertKind(t, msgpackwire.Decode([]byte{0x01}, &wrongSlice, msgpackwire.DecodeOptions{}), wire.ErrValidation)
	var wrongStruct untagged
	assertKind(t, msgpackwire.Decode([]byte{0x01}, &wrongStruct, msgpackwire.DecodeOptions{}), wire.ErrValidation)
	var optional *uint8
	if err := msgpackwire.Decode([]byte{0xc0}, &optional, msgpackwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() nil error = %v", err)
	}
}

func TestDecodeValidatesTypedMapNumericAssignments(t *testing.T) {
	t.Parallel()

	valueOverflow, err := msgpackwire.Encode(
		map[uint16]uint16{1: 300},
		msgpackwire.EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	var narrowValues map[uint8]uint8
	assertKind(t, msgpackwire.Decode(valueOverflow, &narrowValues, msgpackwire.DecodeOptions{}), wire.ErrValidation)

	keyOverflow, err := msgpackwire.Encode(
		map[uint16]string{300: "invalid"},
		msgpackwire.EncodeOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	var narrowKeys map[uint8]string
	assertKind(t, msgpackwire.Decode(keyOverflow, &narrowKeys, msgpackwire.DecodeOptions{}), wire.ErrValidation)

	compositeKeyOverflow := []byte{0x81, 0x91, 0xcd, 0x01, 0x2c, 0xa1, 'x'}
	var narrowCompositeKeys map[[1]uint8]string
	assertKind(t, msgpackwire.Decode(compositeKeyOverflow, &narrowCompositeKeys, msgpackwire.DecodeOptions{}), wire.ErrValidation)
}

func TestDecodeValidatesArrayStructNumericAssignments(t *testing.T) {
	t.Parallel()

	type wide struct {
		Value uint16
	}
	type narrow struct {
		Value uint8
	}
	payload, err := msgpackwire.Encode(
		wide{Value: 300},
		msgpackwire.EncodeOptions{StructAsArray: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	var target narrow
	assertKind(t, msgpackwire.Decode(payload, &target, msgpackwire.DecodeOptions{}), wire.ErrValidation)
}

func TestDecodeValidatesEmbeddedStructNumericAssignments(t *testing.T) {
	t.Parallel()

	type wideValue struct {
		Value uint16
	}
	type narrowValue struct {
		Value uint8
	}
	type wide struct {
		wideValue
	}
	type narrow struct {
		narrowValue
	}
	for name, options := range map[string]msgpackwire.EncodeOptions{
		"map":   {},
		"array": {StructAsArray: true},
	} {
		t.Run(name, func(t *testing.T) {
			payload, err := msgpackwire.Encode(wide{wideValue{Value: 300}}, options)
			if err != nil {
				t.Fatal(err)
			}
			var target narrow
			assertKind(t, msgpackwire.Decode(payload, &target, msgpackwire.DecodeOptions{}), wire.ErrValidation)
		})
	}
}

func TestDecodeFollowsMessagePackStructFieldRules(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		value  any
		target any
	}{
		"explicit pointer inline": {
			value: map[string]any{"Value": uint16(300)},
			target: &struct {
				*EmbeddedNumber `msgpack:",inline"`
			}{},
		},
		"implicit inline with option": {
			value: map[string]any{"Value": uint16(300)},
			target: &struct {
				EmbeddedNumber `msgpack:",omitempty"`
			}{},
		},
		"no inline": {
			value: map[string]any{
				"EmbeddedNumber": map[string]any{"Value": uint16(300)},
			},
			target: &struct {
				EmbeddedNumber `msgpack:",noinline"`
			}{},
		},
		"named numeric field": {
			value:  map[string]any{"NamedNumber": uint16(300)},
			target: &struct{ NamedNumber }{},
		},
	} {
		t.Run(name, func(t *testing.T) {
			payload, err := msgpackwire.Encode(test.value, msgpackwire.EncodeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			assertKind(t, msgpackwire.Decode(payload, test.target, msgpackwire.DecodeOptions{}), wire.ErrValidation)
		})
	}

	value := struct{ CustomStructNumber }{CustomStructNumber{Value: 42}}
	payload, err := msgpackwire.Encode(value, msgpackwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct{ CustomStructNumber }
	if err := msgpackwire.Decode(payload, &decoded, msgpackwire.DecodeOptions{}); err != nil {
		t.Fatalf("Decode() custom embedded field error = %v", err)
	}
	if decoded.Value != 42 {
		t.Fatalf("Decode() custom embedded field = %#v", decoded)
	}
}

func TestDecodeSupportsArrayStructsAndUnknownFieldStrictness(t *testing.T) {
	t.Parallel()
	payload, err := msgpackwire.Encode(shipment{ID: 42, Status: "ready"}, msgpackwire.EncodeOptions{StructAsArray: true})
	if err != nil {
		t.Fatal(err)
	}
	var got shipment
	if err := msgpackwire.Decode(payload, &got, msgpackwire.DecodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if got.ID != 42 || got.Status != "ready" {
		t.Fatalf("Decode() = %#v", got)
	}
	unknown := []byte{0x81, 0xa7, 'u', 'n', 'k', 'n', 'o', 'w', 'n', 0xc3}
	assertKind(t, msgpackwire.Decode(unknown, &got, msgpackwire.DecodeOptions{DisallowUnknownFields: true}), wire.ErrValidation)
}

func TestDecodeRejectsInvalidTargetAndOptions(t *testing.T) {
	t.Parallel()
	var typedNil *shipment
	for _, test := range []struct {
		target  any
		options msgpackwire.DecodeOptions
		kind    error
	}{
		{target: nil, kind: wire.ErrTarget},
		{target: shipment{}, kind: wire.ErrTarget},
		{target: typedNil, kind: wire.ErrTarget},
		{target: &shipment{}, options: msgpackwire.DecodeOptions{MaxBytes: -1}, kind: wire.ErrValidation},
	} {
		assertKind(t, msgpackwire.Decode([]byte{0x80}, test.target, test.options), test.kind)
	}
}

func TestDecodeReaderEnforcesLimitsAndClassifiesReadFailures(t *testing.T) {
	t.Parallel()
	var got shipment
	assertKind(t, msgpackwire.DecodeReader(bytes.NewReader(readFixture(t, "shipment.msgpack.hex")), &got, msgpackwire.DecodeOptions{MaxBytes: 4}), wire.ErrSizeLimit)
	assertKind(t, msgpackwire.DecodeReader(errorReader{}, &got, msgpackwire.DecodeOptions{}), wire.ErrParse)
	assertKind(t, msgpackwire.DecodeReader(nil, &got, msgpackwire.DecodeOptions{}), wire.ErrValidation)
	if err := msgpackwire.DecodeReader(bytes.NewReader([]byte{0x80}), &got, msgpackwire.DecodeOptions{MaxBytes: math.MaxInt64}); err != nil {
		t.Fatalf("DecodeReader() maximum limit error = %v", err)
	}
}

func TestEncodeIsDeterministicAndConfigurable(t *testing.T) {
	t.Parallel()
	value := map[string]any{"z": int64(2), "a": int64(1)}
	first, err := msgpackwire.Encode(value, msgpackwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := msgpackwire.Encode(value, msgpackwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("Encode() not deterministic: %x != %x", first, second)
	}
	if got, want := hex.EncodeToString(first), "82a161d30000000000000001a17ad30000000000000002"; got != want {
		t.Fatalf("Encode() = %s, want %s", got, want)
	}
	compact, err := msgpackwire.Encode(value, msgpackwire.EncodeOptions{CompactIntegers: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hex.EncodeToString(compact), "82a16101a17a02"; got != want {
		t.Fatalf("Encode() compact = %s, want %s", got, want)
	}
	compactFloat, err := msgpackwire.Encode(float64(1), msgpackwire.EncodeOptions{CompactFloats: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := hex.EncodeToString(compactFloat), "01"; got != want {
		t.Fatalf("Encode() compact float = %s, want %s", got, want)
	}
}

func TestEncodeClassifiesUnsupportedAndEncodeFailures(t *testing.T) {
	t.Parallel()
	cyclic := map[string]any{}
	cyclic["self"] = cyclic
	_, err := msgpackwire.Encode(cyclic, msgpackwire.EncodeOptions{})
	assertKind(t, err, wire.ErrEncode)
	_, err = msgpackwire.Encode(make(chan int), msgpackwire.EncodeOptions{})
	assertKind(t, err, wire.ErrUnsupportedFormat)
	_, err = msgpackwire.Encode(failingMarshaler{}, msgpackwire.EncodeOptions{})
	assertKind(t, err, wire.ErrEncode)
}

func TestEncodeWriterWritesAndClassifiesFailures(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	value := map[string]int{"status": 1}
	want, err := msgpackwire.Encode(value, msgpackwire.EncodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := msgpackwire.EncodeWriter(&output, value, msgpackwire.EncodeOptions{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), want) {
		t.Fatalf("EncodeWriter() = %x, want %x", output.Bytes(), want)
	}
	assertKind(t, msgpackwire.EncodeWriter(errorWriter{}, value, msgpackwire.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, msgpackwire.EncodeWriter(shortWriter{}, value, msgpackwire.EncodeOptions{}), wire.ErrWrite)
	assertKind(t, msgpackwire.EncodeWriter(nil, value, msgpackwire.EncodeOptions{}), wire.ErrValidation)
	assertKind(t, msgpackwire.EncodeWriter(&bytes.Buffer{}, make(chan int), msgpackwire.EncodeOptions{}), wire.ErrUnsupportedFormat)
}

func FuzzDecode(f *testing.F) {
	f.Add(readFixture(f, "shipment.msgpack.hex"))
	f.Add([]byte{0xd9, 0x05, 'x'})
	f.Add([]byte{0xdf, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{0x82, 0xa1, 'a', 0x01, 0xa1, 'a', 0x02})
	f.Add(append(bytes.Repeat([]byte{0x91}, msgpackwire.DefaultMaxNestedLevels+1), 0xc0))
	f.Add([]byte{})
	f.Add([]byte{0xc0})
	f.Add([]byte{0x01, 0x02})
	f.Add([]byte{0xd4, 0x2a, 0x00})
	f.Add([]byte{0xdd, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, payload []byte) {
		var target any
		_ = msgpackwire.Decode(payload, &target, msgpackwire.DecodeOptions{MaxBytes: 64 << 10})
	})
}

func BenchmarkDecode(b *testing.B) {
	payload := readFixture(b, "shipment.msgpack.hex")
	b.ReportAllocs()
	for b.Loop() {
		var target shipment
		if err := msgpackwire.Decode(payload, &target, msgpackwire.DecodeOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	value := shipment{ID: 42, Status: "ready", Carrier: "postnord"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := msgpackwire.Encode(value, msgpackwire.EncodeOptions{}); err != nil {
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
	if !errors.As(err, &wireErr) || wireErr.Format != wire.FormatMessagePack {
		t.Fatalf("error = %#v, want MessagePack *wire.Error", err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("fixture reader failed") }

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("fixture writer failed") }

type shortWriter struct{}

func (shortWriter) Write(payload []byte) (int, error) { return len(payload) - 1, nil }

type failingMarshaler struct{}

func (failingMarshaler) MarshalMsgpack() ([]byte, error) {
	return nil, errors.New("fixture marshal failed")
}
