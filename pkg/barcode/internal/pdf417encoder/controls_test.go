package pdf417encoder

import (
	"reflect"
	"testing"
)

func TestEncodeECIRanges(t *testing.T) {
	tests := []struct {
		eci  int
		want []rune
	}{
		{eci: 0, want: []rune{927, 0}},
		{eci: 899, want: []rune{927, 899}},
		{eci: 900, want: []rune{926, 0, 0}},
		{eci: 810_899, want: []rune{926, 899, 899}},
		{eci: 810_900, want: []rune{925, 0}},
		{eci: 811_799, want: []rune{925, 899}},
	}
	for _, test := range tests {
		encoded, err := EncodeECI(test.eci)
		if err != nil {
			t.Fatalf("EncodeECI(%d) error = %v", test.eci, err)
		}
		if got := []rune(encoded); !reflect.DeepEqual(got, test.want) {
			t.Fatalf("EncodeECI(%d) = %v, want %v", test.eci, got, test.want)
		}
	}
	for _, value := range []int{-1, 811_800} {
		if _, err := EncodeECI(value); err == nil {
			t.Fatalf("EncodeECI(%d) succeeded", value)
		}
	}
}

func TestEncodeMacroControlBlock(t *testing.T) {
	segmentCount := 2
	timestamp := int64(1_700_000_000)
	fileSize := int64(42)
	checksum := 7
	encoded, err := encodeMacro(&Macro{
		SegmentIndex: 1, FileID: "129899", LastSegment: true,
		FileName: "FILE.TXT", SegmentCount: &segmentCount, Timestamp: &timestamp,
		Sender: "ALICE", Addressee: "BOB", FileSize: &fileSize, Checksum: &checksum,
	})
	if err != nil {
		t.Fatalf("encodeMacro() error = %v", err)
	}
	codewords := []rune(encoded)
	if got, want := codewords[:5], []rune{928, 111, 101, 129, 899}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mandatory codewords = %v, want %v", got, want)
	}
	if codewords[len(codewords)-1] != 922 {
		t.Fatalf("terminator = %d, want 922", codewords[len(codewords)-1])
	}
}

func TestEncodeMacroRejectsInvalidFields(t *testing.T) {
	negative := -1
	negative64 := int64(-1)
	for _, macro := range []*Macro{
		{SegmentIndex: -1, FileID: "001"},
		{SegmentIndex: 99_999, FileID: "001"},
		{FileID: ""},
		{FileID: "01"},
		{FileID: "900"},
		{FileID: "ABC"},
		{FileID: "001", FileName: "\xff"},
		{FileID: "001", SegmentCount: &negative},
		{FileID: "001", Timestamp: &negative64},
		{FileID: "001", Sender: "\xff"},
		{FileID: "001", Addressee: "\xff"},
		{FileID: "001", FileSize: &negative64},
		{FileID: "001", Checksum: &negative},
	} {
		if _, err := encodeMacro(macro); err == nil {
			t.Fatalf("encodeMacro(%+v) succeeded", macro)
		}
	}
	if encoded, err := encodeMacro(&Macro{FileID: "001"}); err != nil || encoded == "" {
		t.Fatalf("minimal macro = (%q, %v)", encoded, err)
	}
}
