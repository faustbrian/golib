package postgres

import (
	"math"
	"testing"
)

func TestCheckedNumericConversionsRejectOverflow(t *testing.T) {
	t.Parallel()

	if value, ok := int64FromUint64(math.MaxInt64); !ok || value != math.MaxInt64 {
		t.Fatalf("int64FromUint64(max) = (%d, %t)", value, ok)
	}
	if _, ok := int64FromUint64(math.MaxInt64 + 1); ok {
		t.Fatal("int64FromUint64() accepted overflow")
	}
	if value, ok := uint64FromInt64(math.MaxInt64); !ok || value != math.MaxInt64 {
		t.Fatalf("uint64FromInt64(max) = (%d, %t)", value, ok)
	}
	if _, ok := uint64FromInt64(-1); ok {
		t.Fatal("uint64FromInt64() accepted a negative value")
	}
	if value, ok := uint32FromInt64(math.MaxUint32); !ok || value != math.MaxUint32 {
		t.Fatalf("uint32FromInt64(max) = (%d, %t)", value, ok)
	}
	for _, value := range []int64{-1, math.MaxUint32 + 1} {
		if _, ok := uint32FromInt64(value); ok {
			t.Fatalf("uint32FromInt64(%d) accepted overflow", value)
		}
	}
	if value, ok := uint16FromInt64(math.MaxUint16); !ok || value != math.MaxUint16 {
		t.Fatalf("uint16FromInt64(max) = (%d, %t)", value, ok)
	}
	for _, value := range []int64{-1, math.MaxUint16 + 1} {
		if _, ok := uint16FromInt64(value); ok {
			t.Fatalf("uint16FromInt64(%d) accepted overflow", value)
		}
	}
	if value, ok := uint16FromInt16(math.MaxInt16); !ok || value != math.MaxInt16 {
		t.Fatalf("uint16FromInt16(max) = (%d, %t)", value, ok)
	}
	if _, ok := uint16FromInt16(-1); ok {
		t.Fatal("uint16FromInt16() accepted a negative value")
	}
}
