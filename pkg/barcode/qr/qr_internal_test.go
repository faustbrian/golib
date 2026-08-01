package qr

import (
	"errors"
	"reflect"
	"testing"

	unixcoding "github.com/unixdj/qr/coding"
)

func TestModeAndCorrectionMappings(t *testing.T) {
	modes := map[Mode]unixcoding.Mode{
		Auto: unixcoding.Byte, Numeric: unixcoding.Numeric,
		Alphanumeric: unixcoding.Alphanumeric, Byte: unixcoding.Byte,
		Kanji: unixcoding.Kanji,
	}
	for input, want := range modes {
		if got := codingMode(input); got != want {
			t.Fatalf("codingMode(%v) = %v, want %v", input, got, want)
		}
	}
	levels := map[ErrorCorrection]unixcoding.Level{
		DefaultErrorCorrection: unixcoding.M,
		Low:                    unixcoding.L, Medium: unixcoding.M,
		Quartile: unixcoding.Q, High: unixcoding.H,
	}
	for input, want := range levels {
		if got := codingLevel(input); got != want {
			t.Fatalf("codingLevel(%v) = %v, want %v", input, got, want)
		}
	}
	if got := codingMode(Mode(99)); got != unixcoding.Byte {
		t.Fatalf("codingMode(99) = %v, want byte", got)
	}
	if got := codingLevel(ErrorCorrection(99)); got != unixcoding.M {
		t.Fatalf("codingLevel(99) = %v, want M", got)
	}
}

func TestOptimizedModeClassifiesSegmentPlans(t *testing.T) {
	tests := []struct {
		name     string
		segments []unixcoding.Segment
		want     Mode
	}{
		{name: "empty", want: Auto},
		{name: "numeric", segments: []unixcoding.Segment{{Mode: unixcoding.Numeric}}, want: Numeric},
		{name: "alphanumeric FNC1", segments: []unixcoding.Segment{{Mode: unixcoding.FNC1Alpha}}, want: Alphanumeric},
		{name: "kanji", segments: []unixcoding.Segment{{Mode: unixcoding.ShiftJISKanji}}, want: Kanji},
		{name: "byte", segments: []unixcoding.Segment{{Mode: unixcoding.Latin1}}, want: Byte},
		{name: "metadata only", segments: []unixcoding.Segment{{Mode: unixcoding.ECI}}, want: Auto},
		{name: "unknown metadata", segments: []unixcoding.Segment{{Mode: unixcoding.Mode(255)}}, want: Auto},
		{name: "metadata before data", segments: []unixcoding.Segment{{Mode: unixcoding.ECI}, {Mode: unixcoding.Numeric}}, want: Numeric},
		{name: "mixed", segments: []unixcoding.Segment{{Mode: unixcoding.Numeric}, {Mode: unixcoding.Byte}}, want: Auto},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := optimizedMode(tt.segments); got != tt.want {
				t.Fatalf("optimizedMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestECIAssignmentRejectsNegativeValue(t *testing.T) {
	if _, err := encodeECI(-1); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("encodeECI(-1) error = %v", err)
	}
}

func TestECIAssignmentEncodingMatchesWidthBoundaries(t *testing.T) {
	tests := []struct {
		assignment int
		want       []byte
	}{
		{assignment: 0, want: []byte{0}},
		{assignment: 127, want: []byte{0x7f}},
		{assignment: 128, want: []byte{0x80, 0x80}},
		{assignment: 16_383, want: []byte{0xbf, 0xff}},
		{assignment: 16_384, want: []byte{0xc0, 0x40, 0x00}},
		{assignment: 999_999, want: []byte{0xcf, 0x42, 0x3f}},
	}
	for _, test := range tests {
		got, err := encodeECI(test.assignment)
		if err != nil {
			t.Fatalf("encodeECI(%d) error = %v", test.assignment, err)
		}
		if string(test.want) != got {
			t.Fatalf("encodeECI(%d) = %x, want %x",
				test.assignment, []byte(got), test.want)
		}
	}
	if _, err := encodeECI(1_000_000); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("encodeECI(1000000) error = %v", err)
	}
}

func TestStructuredPayloadExcludesControlSegments(t *testing.T) {
	segments := []unixcoding.Segment{
		{Mode: unixcoding.StructAppend, Text: "header"},
		{Mode: unixcoding.ECI, Text: "eci"},
		{Mode: unixcoding.Byte, Text: "payload"},
	}
	if got := string(structuredPayload(segments)); got != "payload" {
		t.Fatalf("structuredPayload() = %q, want payload", got)
	}
}

func TestExtendedSegmentsEncodeControlMetadata(t *testing.T) {
	header := &StructuredAppend{Index: 1, Total: 3, Parity: 0xaa}
	segments := extendedSegments([]byte("12"), Options{
		Mode:                     Numeric,
		ECI:                      26,
		FNC1:                     FNC1Second,
		FNC1ApplicationIndicator: 7,
		StructuredAppend:         header,
	})
	want := []unixcoding.Segment{
		{Mode: unixcoding.StructAppend, Text: string([]byte{0x12, 0xaa})},
		{Mode: unixcoding.FNC1Second, Text: string([]byte{7})},
		{Mode: unixcoding.ECI, Text: string([]byte{26})},
		{Mode: unixcoding.Numeric, Text: "12"},
	}
	if !reflect.DeepEqual(segments, want) {
		t.Fatalf("extendedSegments() = %#v, want %#v", segments, want)
	}

	segments = extendedSegments([]byte("A"), Options{Mode: Byte, FNC1: FNC1First})
	want = []unixcoding.Segment{{Mode: unixcoding.FNC1First}, {Mode: unixcoding.Byte, Text: "A"}}
	if !reflect.DeepEqual(segments, want) {
		t.Fatalf("first-position segments = %#v, want %#v", segments, want)
	}

	segments = extendedSegments([]byte("A"), Options{Mode: Byte})
	want = []unixcoding.Segment{{Mode: unixcoding.Byte, Text: "A"}}
	if !reflect.DeepEqual(segments, want) {
		t.Fatalf("plain segments = %#v, want %#v", segments, want)
	}
}

func TestEncodeSegmentsRejectsInvalidVersionAndCapacity(t *testing.T) {
	if _, _, err := encodeSegments(nil, 0, Medium, 0, false); err == nil {
		t.Fatal("encodeSegments(version 0) unexpectedly succeeded")
	}
	segment := unixcoding.Segment{Mode: unixcoding.Byte, Text: string(make([]byte, 200))}
	if _, _, err := encodeSegments([]unixcoding.Segment{segment}, 1, High, 0, false); err == nil {
		t.Fatal("encodeSegments(over capacity) unexpectedly succeeded")
	}
	invalid := unixcoding.Segment{Mode: unixcoding.Numeric, Text: "A"}
	if _, _, err := encodeSegments([]unixcoding.Segment{invalid}, 1, Medium, 0, false); err == nil {
		t.Fatal("encodeSegments(invalid numeric segment) unexpectedly succeeded")
	}
}
