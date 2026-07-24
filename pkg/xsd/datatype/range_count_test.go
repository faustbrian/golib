package datatype

import (
	"testing"
	"unicode"
)

func TestBoundedRangeCount(t *testing.T) {
	tests := []struct {
		name   string
		low    uint32
		high   uint32
		stride uint32
		limit  uint32
		count  uint32
		ok     bool
	}{
		{name: "single", low: 7, high: 7, stride: 1, limit: 1, count: 1, ok: true},
		{name: "strided", low: 10, high: 14, stride: 2, limit: 3, count: 3, ok: true},
		{name: "zero stride", low: 0, high: 1, stride: 0, limit: 2},
		{name: "reversed", low: 2, high: 1, stride: 1, limit: 2},
		{name: "over limit", low: 0, high: 10, stride: 1, limit: 10},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			count, ok := boundedRangeCount(
				test.low,
				test.high,
				test.stride,
				test.limit,
			)
			if count != test.count || ok != test.ok {
				t.Fatalf(
					"boundedRangeCount(%d, %d, %d, %d) = (%d, %t), want (%d, %t)",
					test.low,
					test.high,
					test.stride,
					test.limit,
					count,
					ok,
					test.count,
					test.ok,
				)
			}
		})
	}
}

func TestRangeTableSetRejectsInvalidRanges(t *testing.T) {
	tests := []struct {
		name  string
		table *unicode.RangeTable
	}{
		{
			name: "invalid 16-bit range",
			table: &unicode.RangeTable{
				R16: []unicode.Range16{{Lo: 1, Hi: 2, Stride: 0}},
			},
		},
		{
			name: "invalid 32-bit range",
			table: &unicode.RangeTable{
				R32: []unicode.Range32{{Lo: 2, Hi: 1, Stride: 1}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if result := rangeTableSet(test.table); result != nil {
				t.Fatalf("rangeTableSet() = %v, want nil", result)
			}
		})
	}
}
