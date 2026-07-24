package gs1_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/barcode/gs1"
)

func TestParseBracketedElementString(t *testing.T) {
	elements, err := gs1.ParseBracketed("(01)09501101530003(17)270101(10)ABC123", gs1.ParseLimits{})
	if err != nil {
		t.Fatalf("ParseBracketed() error = %v", err)
	}
	got := elements.Elements()
	if len(got) != 3 {
		t.Fatalf("len(Elements()) = %d, want 3", len(got))
	}
	if got[0].AI != "01" || got[0].Value != "09501101530003" || got[0].Title != "GTIN" {
		t.Fatalf("first element = %+v", got[0])
	}
	got[0].Value = "changed"
	if elements.Elements()[0].Value != "09501101530003" {
		t.Fatal("Elements() aliases mutable output")
	}
}

func TestParseRawElementStringUsesPredefinedLengthsAndFNC1(t *testing.T) {
	elements, err := gs1.ParseRaw("01095011015300031727010110ABC123", gs1.ParseLimits{})
	if err != nil {
		t.Fatalf("ParseRaw() error = %v", err)
	}
	if got := elements.Elements(); len(got) != 3 || got[2].AI != "10" || got[2].Value != "ABC123" {
		t.Fatalf("Elements() = %+v", got)
	}

	elements, err = gs1.ParseRaw("010950110153000310ABC\x1d17270101", gs1.ParseLimits{})
	if err != nil {
		t.Fatalf("ParseRaw(FNC1) error = %v", err)
	}
	if got := elements.Elements(); len(got) != 3 || got[2].AI != "17" {
		t.Fatalf("Elements(FNC1) = %+v", got)
	}
}

func TestElementStringSerializesRawAndBracketedSyntax(t *testing.T) {
	elements, err := gs1.ParseBracketed("(01)09501101530003(10)ABC(17)270101", gs1.ParseLimits{})
	if err != nil {
		t.Fatalf("ParseBracketed() error = %v", err)
	}
	if got := elements.Raw(); got != "010950110153000310ABC\x1d17270101" {
		t.Fatalf("Raw() = %q", got)
	}
	if got := elements.Bracketed(); got != "(01)09501101530003(10)ABC(17)270101" {
		t.Fatalf("Bracketed() = %q", got)
	}
}

func TestParserUsesPinnedDictionaryRanges(t *testing.T) {
	elements, err := gs1.ParseBracketed("(01)09501101530003(3102)001234", gs1.ParseLimits{})
	if err != nil {
		t.Fatalf("ParseBracketed(range AI) error = %v", err)
	}
	if got := elements.Elements()[1].Title; got != "NET WEIGHT (kg)" {
		t.Fatalf("range AI title = %q", got)
	}
	if count := gs1.DefinitionCount(); count < 200 {
		t.Fatalf("DefinitionCount() = %d, want at least 200", count)
	}
}

func TestParserEnforcesRequiredAndExcludedAssociations(t *testing.T) {
	valid := []string{
		"(00)106141411234567897(02)09501101530003(37)1",
		"(01)09501101530003(21)SERIAL(250)SECONDARY",
		"(01)09501101530003(3100)000001",
	}
	for _, input := range valid {
		if _, err := gs1.ParseBracketed(input, gs1.ParseLimits{}); err != nil {
			t.Fatalf("ParseBracketed(%q) error = %v", input, err)
		}
	}
	invalid := []string{
		"(02)09501101530003",
		"(01)09501101530003(37)1",
		"(01)09501101530003(3100)000001(3101)000002",
		"(01)09501101530003(250)SECONDARY",
	}
	for _, input := range invalid {
		if _, err := gs1.ParseBracketed(input, gs1.ParseLimits{}); !errors.Is(err, gs1.ErrInvalidElement) {
			t.Fatalf("ParseBracketed(%q) error = %v", input, err)
		}
	}
}

func TestParserRejectsUnknownInvalidAndOverLimitData(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		limits gs1.ParseLimits
		want   error
	}{
		{name: "unknown AI", input: "(04)123", want: gs1.ErrUnknownAI},
		{name: "bad check digit", input: "(01)09501101530004", want: gs1.ErrInvalidElement},
		{name: "non numeric", input: "(17)27x101", want: gs1.ErrInvalidElement},
		{name: "invalid date", input: "(01)09501101530003(17)271332", want: gs1.ErrInvalidElement},
		{name: "value too long", input: "(10)123456789012345678901", want: gs1.ErrInvalidElement},
		{name: "input limit", input: "(10)ABC", limits: gs1.ParseLimits{MaxInputBytes: 6}, want: gs1.ErrLimitExceeded},
		{name: "element limit", input: "(10)A(21)B", limits: gs1.ParseLimits{MaxElements: 1}, want: gs1.ErrLimitExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := gs1.ParseBracketed(tt.input, tt.limits); !errors.Is(err, tt.want) {
				t.Fatalf("ParseBracketed() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestParsersRejectMalformedStructureAndLimits(t *testing.T) {
	bracketed := []string{
		"10)ABC",
		"(10ABC",
		"(04)ABC",
		"(10)ABC(",
	}
	for _, input := range bracketed {
		if _, err := gs1.ParseBracketed(input, gs1.ParseLimits{}); err == nil {
			t.Fatalf("ParseBracketed(%q) unexpectedly succeeded", input)
		}
	}

	raw := []string{
		"\x1d10ABC",
		"04ABC",
		"010123",
		"0209501101530003",
		"10ABC\x1d",
		"10\x1d17270101",
	}
	for _, input := range raw {
		if _, err := gs1.ParseRaw(input, gs1.ParseLimits{}); err == nil {
			t.Fatalf("ParseRaw(%q) unexpectedly succeeded", input)
		}
	}

	limits := []gs1.ParseLimits{
		{MaxInputBytes: -1},
		{MaxElements: -1},
		{MaxElements: 0, MaxInputBytes: 1},
	}
	for _, limit := range limits {
		if _, err := gs1.ParseRaw("10ABC", limit); err == nil {
			t.Fatalf("ParseRaw(%+v) unexpectedly succeeded", limit)
		}
	}
	if _, err := gs1.ParseRaw("", gs1.ParseLimits{}); !errors.Is(err, gs1.ErrInvalidElement) {
		t.Fatalf("ParseRaw(empty) error = %v", err)
	}
	if _, err := gs1.ParseRaw("10A\x1d21B", gs1.ParseLimits{MaxElements: 1}); !errors.Is(err, gs1.ErrLimitExceeded) {
		t.Fatalf("ParseRaw(element limit) error = %v", err)
	}
}
