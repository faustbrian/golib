package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"
)

func TestUnicodeSourceParsers(t *testing.T) {
	t.Parallel()

	lines := dataLines([]byte(" # comment\n0041..005A ; Alpha # x\nA ; ; B\n"))
	if len(lines) != 2 ||
		!slices.Equal(lines[0], []string{"0041..005A", "Alpha"}) ||
		!slices.Equal(lines[1], []string{"A", "B"}) {
		t.Fatalf("dataLines() = %#v", lines)
	}

	properties := parseRangeProperties(
		[]byte("0041..005A ; Alpha\n0061 ; Alpha\n"),
	)
	if got := properties["Alpha"]; !slices.Equal(
		got,
		[]codeRange{{lo: 0x41, hi: 0x5A}, {lo: 0x61, hi: 0x61}},
	) {
		t.Fatalf("parseRangeProperties() = %#v", got)
	}

	propertyAliases := parsePropertyAliases(
		[]byte("Alpha ; Alphabetic ; Alpha\nbad\n"),
	)
	if !slices.Equal(
		propertyAliases["Alphabetic"],
		[]string{"Alpha", "Alphabetic", "Alpha"},
	) {
		t.Fatalf("parsePropertyAliases() = %#v", propertyAliases)
	}

	valueAliases := parseValueAliases([]byte(
		"gc ; Lu ; Uppercase_Letter\n" +
			"sc ; Latn ; Latin\n" +
			"age ; 1.1 ; V1_1\n",
	))
	if valueAliases["gc"]["Uppercase_Letter"] != "Lu" ||
		valueAliases["sc"]["Latn"] != "Latin" {
		t.Fatalf("parseValueAliases() = %#v", valueAliases)
	}
}

func TestUnicodeDataAndRangeAlgebra(t *testing.T) {
	t.Parallel()

	unicodeData := strings.Join([]string{
		"0041;LATIN CAPITAL LETTER A;Lu;0;L;;;;;N;;;0061;",
		"3400;<CJK Ideograph Extension A, First>;Lo;0;L;;;;;Y;;;;;",
		"3401;<CJK Ideograph Extension A, Last>;Lo;0;L;;;;;Y;;;;;",
	}, "\n")
	general, assigned, mirrored := parseUnicodeData([]byte(unicodeData))
	if len(general["L"]) != 2 ||
		!containsRange(assigned, codeRange{lo: 0x3400, hi: 0x3401}) ||
		!containsRange(mirrored, codeRange{lo: 0x3400, hi: 0x3401}) {
		t.Fatalf(
			"parseUnicodeData() = %#v, %#v, %#v",
			general,
			assigned,
			mirrored,
		)
	}

	merged := merge([]codeRange{
		{lo: 4, hi: 5},
		{lo: 1, hi: 2},
		{lo: 3, hi: 3},
	})
	if !slices.Equal(merged, []codeRange{{lo: 1, hi: 5}}) {
		t.Fatalf("merge() = %#v", merged)
	}
	if got := union(
		[]codeRange{{lo: 1, hi: 1}},
		[]codeRange{{lo: 2, hi: 2}},
	); !slices.Equal(got, []codeRange{{lo: 1, hi: 2}}) {
		t.Fatalf("union() = %#v", got)
	}
	complemented := complement([]codeRange{{lo: 1, hi: maxCodePoint - 1}})
	if !slices.Equal(
		complemented,
		[]codeRange{{lo: 0, hi: 0}, {lo: maxCodePoint, hi: maxCodePoint}},
	) {
		t.Fatalf("complement() = %#v", complemented)
	}
}

func TestBitAndAliasHelpers(t *testing.T) {
	t.Parallel()

	bits := make([]uint64, (maxCodePoint+64)/64)
	setRanges(bits, []codeRange{{lo: 63, hi: 65}}, true)
	setBit(bits, 64, false)
	if got := bitRanges(bits); !slices.Equal(
		got,
		[]codeRange{{lo: 63, hi: 63}, {lo: 65, hi: 65}},
	) {
		t.Fatalf("bitRanges() = %#v", got)
	}
	cloned := cloneBits(map[string][]uint64{"x": bits})
	cloned["x"][0] = 0
	if bits[0] == 0 {
		t.Fatal("cloneBits() retained source storage")
	}
	if got := uniqueValues(map[string]string{
		"a": "Latin",
		"b": "Greek",
		"c": "Latin",
	}); !slices.Equal(got, []string{"Greek", "Latin"}) {
		t.Fatalf("uniqueValues() = %#v", got)
	}
}

func TestCaseAndEmojiParsers(t *testing.T) {
	t.Parallel()

	folds := parseCaseFolding([]byte(
		"0041; C; 0061; # A\n" +
			"0042; F; 0062 0063; # ignored full fold\n",
	))
	if folds['A'] != 'a' || len(folds) != 1 {
		t.Fatalf("parseCaseFolding() = %#v", folds)
	}

	legacy := parseLegacyUpper([]byte(
		"0061;LATIN SMALL LETTER A;Ll;0;L;;;;;N;;;0041;;\n",
	))
	if legacy['a'] != 'A' {
		t.Fatalf("parseLegacyUpper() = %#v", legacy)
	}

	emoji := parseEmojiProperties(
		[]byte(
			"0030..0031 ; Basic_Emoji\n"+
				"1F1E6 1F1E7 ; RGI_Emoji_Flag_Sequence\n",
		),
		[]byte("1F468 200D 1F469 ; RGI_Emoji_ZWJ_Sequence\n"),
	)
	if len(emoji["Basic_Emoji"]) != 2 ||
		len(emoji["RGI_Emoji_Flag_Sequence"]) != 1 ||
		len(emoji["RGI_Emoji"]) != 4 {
		t.Fatalf("parseEmojiProperties() = %#v", emoji)
	}
	if unitsKey([]uint16{0x41, 0x1F}) != "0041001F" {
		t.Fatalf("unitsKey() returned an unstable key")
	}
}

func TestGenerateIsFormattedAndDeterministic(t *testing.T) {
	t.Parallel()

	tables := map[string][]codeRange{
		"bin:ASCII": {{lo: 0, hi: 0x7F}},
		"gc:Lu":     {{lo: 'A', hi: 'Z'}},
	}
	aliases := map[string]string{
		"bin:ASCII": "bin:ASCII",
		"gc:Lu":     "gc:Lu",
	}
	stringsByProperty := map[string][][]uint16{
		"RGI_Emoji": {{0xD83D, 0xDE00}},
	}
	folds := map[rune]rune{'A': 'a'}
	legacy := map[rune]rune{'a': 'A'}
	first, err := generate(
		tables,
		aliases,
		stringsByProperty,
		folds,
		legacy,
	)
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	second, err := generate(
		tables,
		aliases,
		stringsByProperty,
		folds,
		legacy,
	)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("generate() is not deterministic: %v", err)
	}
	if _, err := parser.ParseFile(
		token.NewFileSet(),
		"generated.go",
		first,
		parser.AllErrors,
	); err != nil {
		t.Fatalf("generated source error = %v", err)
	}

	_, err = generate(
		tables,
		map[string]string{"bad": "missing"},
		nil,
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("generate() accepted an alias to a missing table")
	}
}

func containsRange(ranges []codeRange, want codeRange) bool {
	return slices.Contains(ranges, want)
}
