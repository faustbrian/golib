package datatype

import (
	"reflect"
	"testing"
	"unicode"
)

func TestCalendarValidatorRejectsUnknownPrimitive(t *testing.T) {
	t.Parallel()

	if validCalendarLexical("unknown", "2024") {
		t.Fatal("validCalendarLexical() accepted an unknown primitive")
	}
}

func TestRangeTableSetExpandsStrides(t *testing.T) {
	t.Parallel()

	table := &unicode.RangeTable{
		R16: []unicode.Range16{{Lo: 0x20, Hi: 0x24, Stride: 2}},
		R32: []unicode.Range32{{Lo: 0x10000, Hi: 0x10004, Stride: 2}},
	}
	want := runeSet{
		{0x20, 0x20}, {0x22, 0x22}, {0x24, 0x24},
		{0x10000, 0x10000}, {0x10002, 0x10002}, {0x10004, 0x10004},
	}
	if got := rangeTableSet(table); !reflect.DeepEqual(got, want) {
		t.Fatalf("rangeTableSet() = %v, want %v", got, want)
	}
}
