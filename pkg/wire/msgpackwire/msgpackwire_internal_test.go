package msgpackwire

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/vmihailenco/msgpack/v5"
)

func TestDecodeNumericMapRejectsTruncation(t *testing.T) {
	t.Parallel()

	for name, payload := range map[string][]byte{
		"header":        {0xde},
		"missing key":   {0x81},
		"missing value": {0x81, 0xa1, 'a'},
	} {
		t.Run(name, func(t *testing.T) {
			decoder := msgpack.NewDecoder(bytes.NewReader(payload))
			if _, err := decodeNumericMap(decoder); err == nil {
				t.Fatal("decodeNumericMap() error = nil")
			}
		})
	}
}

func TestValidateMessagePackMapBoundaries(t *testing.T) {
	t.Parallel()

	limits := structuralLimits{maxNestedLevels: 1, maxArrayElements: 1, maxMapPairs: 1}
	for name, payload := range map[string][]byte{
		"exact pair and depth": {0x81, 0x01, 0x02},
		"nested key":           {0x81, 0x91, 0x01, 0x02},
	} {
		t.Run(name, func(t *testing.T) {
			decoder := msgpack.NewDecoder(bytes.NewReader(payload))
			err := validateMessagePackValue(decoder, limits, 0)
			if name == "exact pair and depth" && err != nil {
				t.Fatalf("validateMessagePackValue() exact boundary error = %v", err)
			}
			if name == "nested key" && !errors.Is(err, errStructuralLimit) {
				t.Fatalf("validateMessagePackValue() nested key error = %v", err)
			}
		})
	}
}

func TestValidateNumericFitBoundaries(t *testing.T) {
	t.Parallel()

	type pair struct {
		First  uint8
		Second uint8
	}
	for _, test := range []struct {
		name      string
		source    any
		target    reflect.Type
		wantError bool
	}{
		{name: "maximum signed integer", source: uint64(math.MaxInt64), target: reflect.TypeFor[int64]()},
		{name: "zero unsigned integer", source: int64(0), target: reflect.TypeFor[uint64]()},
		{name: "float32 overflow", source: math.MaxFloat64, target: reflect.TypeFor[float32](), wantError: true},
		{name: "float32 precision", source: 1.1, target: reflect.TypeFor[float32](), wantError: true},
		{name: "float64 precision", source: 1.1, target: reflect.TypeFor[float64]()},
		{name: "short struct array", source: []any{uint16(1)}, target: reflect.TypeFor[pair]()},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateNumericFit(test.source, test.target)
			if test.wantError && err == nil {
				t.Fatal("validateNumericFit() error = nil")
			}
			if !test.wantError && err != nil {
				t.Fatalf("validateNumericFit() error = %v", err)
			}
		})
	}
}

func TestNumericStructFieldsPreserveFieldTraversal(t *testing.T) {
	t.Parallel()

	type embedded struct{ Embedded uint8 }
	type fixture struct {
		Ignored  uint8 `msgpack:"-"`
		embedded `msgpack:",inline"`
		hidden   uint8
		Visible  uint8
	}
	_ = fixture{hidden: 1}
	fields := numericStructFields(reflect.TypeFor[fixture]())
	want := []numericStructField{
		{name: "Embedded", target: reflect.TypeFor[uint8]()},
		{name: "Visible", target: reflect.TypeFor[uint8]()},
	}
	if !reflect.DeepEqual(fields, want) {
		t.Fatalf("numericStructFields() = %#v, want %#v", fields, want)
	}
}
