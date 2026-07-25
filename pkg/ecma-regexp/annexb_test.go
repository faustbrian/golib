package ecmascript_test

import (
	"context"
	"testing"

	ecmascript "github.com/faustbrian/golib/pkg/ecma-regexp"
)

func TestAnnexBLegacyDecimalAndOctalEscapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		input   []uint16
	}{
		{pattern: `^\1$`, input: []uint16{1}},
		{pattern: `^\11$`, input: []uint16{'\t'}},
		{pattern: `^(a)\11$`, input: []uint16{'a', '\t'}},
		{pattern: `^\377$`, input: []uint16{0xFF}},
		{pattern: `^\400$`, input: []uint16{' ', '0'}},
		{pattern: `^\8$`, input: []uint16{'8'}},
		{pattern: `^\08$`, input: []uint16{0, '8'}},
		{pattern: `^[\1]$`, input: []uint16{1}},
		{pattern: `^[[](a)\1$`, input: []uint16{'[', 'a', 'a'}},
	}

	for _, test := range tests {
		program, err := ecmascript.Compile(test.pattern, "", ecmascript.DefaultCompileOptions())
		if err != nil {
			t.Errorf("Compile(%q) error = %v", test.pattern, err)
			continue
		}
		_, matched, err := program.MatchUTF16(
			context.Background(),
			ecmascript.UTF16FromUnits(test.input),
			ecmascript.DefaultMatchOptions(),
		)
		if err != nil || !matched {
			t.Errorf("MatchUTF16(%q) = _, %t, %v", test.pattern, matched, err)
		}
	}
}

func TestAnnexBIsExplicitAndUnicodeModesRemainStrict(t *testing.T) {
	t.Parallel()

	if !ecmascript.DefaultCompileOptions().Parse.AnnexB {
		t.Fatal("DefaultCompileOptions() disabled Annex B")
	}
	for _, pattern := range []string{`\1`, `\8`, `\01`} {
		if _, err := ecmascript.Compile(pattern, "u", ecmascript.DefaultCompileOptions()); err == nil {
			t.Errorf("Compile(%q, u) accepted a legacy escape", pattern)
		}
	}

	options := ecmascript.DefaultCompileOptions()
	options.Parse.AnnexB = false
	if _, err := ecmascript.Compile(`\1`, "", options); err == nil {
		t.Fatal("Compile() accepted a legacy escape with Annex B disabled")
	}
}

func TestAnnexBExtendedAtomsAndClassControlEscapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		pattern string
		input   []uint16
	}{
		{pattern: `^\c!$`, input: []uint16{'\\', 'c', '!'}},
		{pattern: `^[\c0]$`, input: []uint16{16}},
		{pattern: `^[\c_]$`, input: []uint16{31}},
		{pattern: `^{$`, input: []uint16{'{'}},
		{pattern: `^}$`, input: []uint16{'}'}},
		{pattern: `^]$`, input: []uint16{']'}},
		{pattern: `^(?=a)+a$`, input: []uint16{'a'}},
		{pattern: `^\k$`, input: []uint16{'k'}},
		{pattern: `^\k<x>(?<x>a)$`, input: []uint16{'a'}},
	}

	for _, test := range tests {
		program, err := ecmascript.Compile(test.pattern, "", ecmascript.DefaultCompileOptions())
		if err != nil {
			t.Errorf("Compile(%q) error = %v", test.pattern, err)
			continue
		}
		_, matched, err := program.MatchUTF16(
			context.Background(),
			ecmascript.UTF16FromUnits(test.input),
			ecmascript.DefaultMatchOptions(),
		)
		if err != nil || !matched {
			t.Errorf("MatchUTF16(%q) = _, %t, %v", test.pattern, matched, err)
		}
	}

	for _, test := range []struct {
		pattern string
		flags   string
	}{
		{pattern: `(?=a)+`, flags: "u"},
		{pattern: `(?<=a)+`, flags: ""},
		{pattern: `(?<=a)+`, flags: "u"},
		{pattern: `(?<x>a)\k`, flags: ""},
	} {
		if _, err := ecmascript.Compile(test.pattern, test.flags, ecmascript.DefaultCompileOptions()); err == nil {
			t.Errorf("Compile(%q, %q) accepted invalid syntax", test.pattern, test.flags)
		}
	}
}

func TestAnnexBClassSetRangeIsUnionWithHyphen(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{`^[a-\d]$`, `^[\d-a]$`, `^[\d-\s]$`} {
		program, err := ecmascript.Compile(pattern, "", ecmascript.DefaultCompileOptions())
		if err != nil {
			t.Fatalf("Compile(%q) error = %v", pattern, err)
		}
		for _, input := range []string{"1", "-"} {
			_, matched, err := program.Match(context.Background(), input, ecmascript.DefaultMatchOptions())
			if err != nil || !matched {
				t.Errorf("Match(%q, %q) = _, %t, %v", pattern, input, matched, err)
			}
		}
		if _, err := ecmascript.Compile(pattern, "u", ecmascript.DefaultCompileOptions()); err == nil {
			t.Errorf("Compile(%q, u) accepted a class-set range", pattern)
		}
	}
}
