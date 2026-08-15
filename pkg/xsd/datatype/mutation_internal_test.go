package datatype

import (
	"errors"
	"math/big"
	"reflect"
	"testing"
	"unicode/utf8"
)

func TestCalendarLexicalBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, lexical string
		valid         bool
	}{
		{name: "dateTime", lexical: "2024-02-29T23:59:59", valid: true},
		{name: "dateTime", lexical: "2024-02-29T23:59:60"},
		{name: "dateTime", lexical: "2024-02-29T23:59:59T00:00:00"},
		{name: "gYearMonth", lexical: "2024-01", valid: true},
		{name: "gYearMonth", lexical: "2024-12", valid: true},
		{name: "gYearMonth", lexical: "2024-00"},
		{name: "gYearMonth", lexical: "2024-13"},
		{name: "gYearMonth", lexical: "invalid"},
		{name: "gYear", lexical: "0001", valid: true},
		{name: "gYear", lexical: "invalid"},
		{name: "gMonthDay", lexical: "--01-01", valid: true},
		{name: "gMonthDay", lexical: "invalid"},
		{name: "gDay", lexical: "---01", valid: true},
		{name: "gDay", lexical: "---31", valid: true},
		{name: "gDay", lexical: "---00"},
		{name: "gDay", lexical: "---32"},
		{name: "gDay", lexical: "invalid"},
		{name: "gMonth", lexical: "--01", valid: true},
		{name: "gMonth", lexical: "--12", valid: true},
		{name: "gMonth", lexical: "--00"},
		{name: "gMonth", lexical: "--13"},
		{name: "gMonth", lexical: "invalid"},
	}
	for _, test := range tests {
		if got := validCalendarLexical(test.name, test.lexical); got != test.valid {
			t.Errorf("validCalendarLexical(%q, %q) = %t, want %t", test.name, test.lexical, got, test.valid)
		}
	}
}

func TestCalendarHelperBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		lexical string
		core    string
		valid   bool
	}{
		{lexical: "short", core: "short", valid: true},
		{lexical: "valueZ", core: "value", valid: true},
		{lexical: "2024+14:00", core: "2024", valid: true},
		{lexical: "2024-14:00", core: "2024", valid: true},
		{lexical: "2024+13:59", core: "2024", valid: true},
		{lexical: "2024+14:01"},
		{lexical: "2024+15:00"},
		{lexical: "2024+00:60"},
		{lexical: "2024+aa:00"},
		{lexical: "2024+00:aa"},
		{lexical: "2024x00:00", core: "2024x00:00", valid: true},
		{lexical: "2024+00000", core: "2024+00000", valid: true},
		{lexical: "+00:00", core: "", valid: true},
	} {
		core, valid := stripTimezone(test.lexical)
		if core != test.core || valid != test.valid {
			t.Errorf("stripTimezone(%q) = %q, %t; want %q, %t", test.lexical, core, valid, test.core, test.valid)
		}
	}
	for _, test := range []struct {
		lexical string
		valid   bool
	}{
		{lexical: "P1Y", valid: true},
		{lexical: "PT1S", valid: true},
		{lexical: "P1YT1S", valid: true},
		{lexical: "P"},
		{lexical: "PT"},
		{lexical: "P1YT"},
		{lexical: "invalid"},
	} {
		if got := validDuration(test.lexical); got != test.valid {
			t.Errorf("validDuration(%q) = %t, want %t", test.lexical, got, test.valid)
		}
	}

	for _, test := range []struct {
		lexical string
		valid   bool
	}{
		{lexical: "00:00:00", valid: true},
		{lexical: "23:59:59", valid: true},
		{lexical: "24:00:00", valid: true},
		{lexical: "24:00:00.0"},
		{lexical: "24:00:01"},
		{lexical: "24:01:00"},
		{lexical: "25:00:00"},
		{lexical: "23:60:00"},
		{lexical: "23:00:60"},
		{lexical: "invalid"},
	} {
		if got := validTime(test.lexical); got != test.valid {
			t.Errorf("validTime(%q) = %t, want %t", test.lexical, got, test.valid)
		}
	}

	for _, test := range []struct {
		year  string
		valid bool
	}{
		{year: "0001", valid: true},
		{year: "12345", valid: true},
		{year: "0000"},
		{year: "01234"},
		{year: "123"},
	} {
		if got := validYear(test.year); got != test.valid {
			t.Errorf("validYear(%q) = %t, want %t", test.year, got, test.valid)
		}
	}

	for _, test := range []struct {
		month, day string
		leap       bool
		valid      bool
	}{
		{month: "01", day: "01", valid: true},
		{month: "01", day: "31", valid: true},
		{month: "01", day: "32"},
		{month: "02", day: "28", valid: true},
		{month: "02", day: "29", leap: true, valid: true},
		{month: "02", day: "29"},
		{month: "00", day: "01"},
		{month: "13", day: "01"},
		{month: "01", day: "00"},
	} {
		if got := validMonthDay(test.month, test.day, test.leap); got != test.valid {
			t.Errorf("validMonthDay(%q, %q, %t) = %t, want %t", test.month, test.day, test.leap, got, test.valid)
		}
	}

	for _, year := range []struct {
		value string
		leap  bool
	}{{"2000", true}, {"2004", true}, {"1900", false}, {"2001", false}} {
		if got := leapYear(year.value); got != year.leap {
			t.Errorf("leapYear(%q) = %t, want %t", year.value, got, year.leap)
		}
	}

	for _, test := range []struct {
		value        string
		minimum      int
		maximum      int
		withinBounds bool
	}{{"1", 1, 12, true}, {"12", 1, 12, true}, {"0", 1, 12, false}, {"13", 1, 12, false}, {"x", 1, 12, false}} {
		if got := numberIn(test.value, test.minimum, test.maximum); got != test.withinBounds {
			t.Errorf("numberIn(%q, %d, %d) = %t, want %t", test.value, test.minimum, test.maximum, got, test.withinBounds)
		}
	}
}

func TestDecimalInternalBoundaries(t *testing.T) {
	t.Parallel()

	if _, err := ParseDecimal(string(make([]byte, maxDecimalLexicalBytes))); errors.Is(err, ErrLimitExceeded) {
		t.Fatal("ParseDecimal rejected the exact lexical byte limit")
	}
	if _, err := ParseDecimal(string(make([]byte, maxDecimalLexicalBytes+1))); err == nil {
		t.Fatal("ParseDecimal accepted input beyond the lexical byte limit")
	}

	for _, test := range []struct {
		lexical, canonical string
		scale              int
	}{
		{lexical: "10.0100", canonical: "10.01", scale: 2},
		{lexical: "10.1000", canonical: "10.1", scale: 1},
		{lexical: "-0.000", canonical: "0.0", scale: 0},
		{lexical: "0.001", canonical: "0.001", scale: 3},
		{lexical: "+1", canonical: "1.0", scale: 0},
	} {
		value, err := ParseDecimal(test.lexical)
		if err != nil {
			t.Fatalf("ParseDecimal(%q): %v", test.lexical, err)
		}
		if value.String() != test.canonical || value.FractionDigits() != test.scale {
			t.Errorf("ParseDecimal(%q) = %q/%d, want %q/%d", test.lexical, value.String(), value.FractionDigits(), test.canonical, test.scale)
		}
	}

	for _, lexical := range []string{"1..0", "1.0.0", "..", ".-1", "1a", "a1"} {
		if _, err := ParseDecimal(lexical); err == nil {
			t.Errorf("ParseDecimal(%q) succeeded", lexical)
		}
	}

	for _, test := range []struct {
		left, right string
		comparison  int
	}{{"1.2", "1.20", 0}, {"1.2", "1.21", -1}, {"1.21", "1.2", 1}, {"-1.2", "-1.19", -1}} {
		left, _ := ParseDecimal(test.left)
		right, _ := ParseDecimal(test.right)
		if got := left.Compare(right); got != test.comparison {
			t.Errorf("Compare(%s, %s) = %d, want %d", test.left, test.right, got, test.comparison)
		}
	}

	minimum, _ := ParseDecimal("1")
	maximum, _ := ParseDecimal("3")
	for _, test := range []struct {
		name   string
		facets DecimalFacets
		value  string
		valid  bool
	}{
		{name: "min-inclusive-equal", facets: DecimalFacets{MinInclusive: &minimum}, value: "1", valid: true},
		{name: "min-inclusive-below", facets: DecimalFacets{MinInclusive: &minimum}, value: "0"},
		{name: "min-exclusive-equal", facets: DecimalFacets{MinExclusive: &minimum}, value: "1"},
		{name: "min-exclusive-above", facets: DecimalFacets{MinExclusive: &minimum}, value: "2", valid: true},
		{name: "max-inclusive-equal", facets: DecimalFacets{MaxInclusive: &maximum}, value: "3", valid: true},
		{name: "max-inclusive-above", facets: DecimalFacets{MaxInclusive: &maximum}, value: "4"},
		{name: "max-exclusive-equal", facets: DecimalFacets{MaxExclusive: &maximum}, value: "3"},
		{name: "max-exclusive-below", facets: DecimalFacets{MaxExclusive: &maximum}, value: "2", valid: true},
		{name: "total-equal", facets: DecimalFacets{TotalDigits: 2}, value: "12", valid: true},
		{name: "total-above", facets: DecimalFacets{TotalDigits: 2}, value: "123"},
		{name: "fraction-equal", facets: DecimalFacets{FractionDigits: intTestPointer(2)}, value: "1.23", valid: true},
		{name: "fraction-above", facets: DecimalFacets{FractionDigits: intTestPointer(2)}, value: "1.234"},
	} {
		value, _ := ParseDecimal(test.value)
		if got := test.facets.Validate(value) == nil; got != test.valid {
			t.Errorf("%s validity = %t, want %t", test.name, got, test.valid)
		}
	}
}

func TestIntegerExactLexicalLimit(t *testing.T) {
	t.Parallel()

	if _, err := ParseInteger("1" + string(make([]byte, maxDecimalLexicalBytes-1))); errors.Is(err, ErrLimitExceeded) {
		t.Fatal("ParseInteger rejected the exact lexical byte limit")
	}
	if _, err := ParseInteger("1" + string(make([]byte, maxDecimalLexicalBytes))); err == nil {
		t.Fatal("ParseInteger accepted input beyond the lexical byte limit")
	}
}

func TestLexicalHelperBoundaries(t *testing.T) {
	t.Parallel()

	if compact, ok := removeXMLWhitespace(" Y\tQ\r=\n= "); !ok || compact != "YQ==" {
		t.Fatalf("removeXMLWhitespace() = %q, %t", compact, ok)
	}
	if compact, ok := removeXMLWhitespace("YéQ=="); ok || compact != "" {
		t.Fatalf("removeXMLWhitespace(non-ASCII) = %q, %t", compact, ok)
	}
	if compact, ok := removeXMLWhitespace("Y\u0080Q=="); ok || compact != "" {
		t.Fatalf("removeXMLWhitespace(U+0080) = %q, %t", compact, ok)
	}

	for _, test := range []struct {
		value   string
		nmtoken bool
		valid   bool
	}{
		{value: "first second", valid: true},
		{value: "first 1second"},
		{value: "first second!"},
		{value: "1first second", nmtoken: true, valid: true},
		{value: "1first second!", nmtoken: true},
		{value: ""},
	} {
		if got := validNameList(test.value, test.nmtoken); got != test.valid {
			t.Errorf("validNameList(%q, %t) = %t, want %t", test.value, test.nmtoken, got, test.valid)
		}
	}

	startRanges := []runeRange{
		{'A', 'Z'}, {'a', 'z'}, {0xC0, 0xD6}, {0xD8, 0xF6}, {0xF8, 0x2FF},
		{0x370, 0x37D}, {0x37F, 0x1FFF}, {0x200C, 0x200D}, {0x2070, 0x218F},
		{0x2C00, 0x2FEF}, {0x3001, 0xD7FF}, {0xF900, 0xFDCF}, {0xFDF0, 0xFFFD},
		{0x10000, 0xEFFFF},
	}
	for _, boundary := range startRanges {
		for _, character := range []rune{boundary.first, boundary.last} {
			if !xmlNameStart(character, false) {
				t.Errorf("xmlNameStart(U+%04X) = false at an inclusive boundary", character)
			}
		}
	}
	for _, character := range []rune{'@', '[', '`', '{', 0xBF, 0xD7, 0xF7, 0x300, 0x36F, 0x37E, 0x2000, 0x200B, 0x200E, 0x206F, 0x2190, 0x2BFF, 0x2FF0, 0x3000, 0xD800, 0xF8FF, 0xFDD0, 0x10000 - 1, 0xF0000} {
		if xmlNameStart(character, false) {
			t.Errorf("xmlNameStart(U+%04X) = true outside the XML NameStartChar ranges", character)
		}
	}
	if !xmlNameStart(':', true) || xmlNameStart(':', false) || !xmlNameStart('_', false) {
		t.Fatal("xmlNameStart colon/underscore policy is incorrect")
	}

	for _, character := range []rune{'-', '.', '0', '9', 0xB7, 0x0300, 0x036F, 0x203F, 0x2040} {
		if !xmlNameCharacter(character, false) {
			t.Errorf("xmlNameCharacter(U+%04X) = false at an inclusive boundary", character)
		}
	}
	for _, character := range []rune{'/', ':', 0xB6, 0xB8, 0x203E, 0x2041} {
		if xmlNameCharacter(character, false) {
			t.Errorf("xmlNameCharacter(U+%04X) = true outside the XML NameChar ranges", character)
		}
	}
}

func TestOrderedDurationAndCalendarBoundaries(t *testing.T) {
	t.Parallel()

	for _, lexical := range []string{"invalid", "P", "PT"} {
		if _, ok := parseOrderedDuration(lexical); ok {
			t.Errorf("parseOrderedDuration(%q) succeeded", lexical)
		}
	}
	positive, ok := parseOrderedDuration("P1Y2M3DT4H5M6.5S")
	if !ok || positive.sign != 1 || positive.years.String() != "1" || positive.months.String() != "2" ||
		positive.days.String() != "3" || positive.hours.String() != "4" || positive.minutes.String() != "5" ||
		positive.seconds.RatString() != "13/2" {
		t.Fatalf("parseOrderedDuration(positive) = %#v, %t", positive, ok)
	}
	negative, ok := parseOrderedDuration("-P1Y2M3DT4H5M6.5S")
	if !ok || negative.sign != -1 {
		t.Fatalf("parseOrderedDuration(negative).sign = %d, %t", negative.sign, ok)
	}
	months, seconds := orderedDurationComponents(negative)
	if months.String() != "-14" || seconds.RatString() != "-547813/2" {
		t.Fatalf("orderedDurationComponents() = %s, %s", months, seconds)
	}
	months, seconds = orderedDurationComponents(positive)
	if months.String() != "14" || seconds.RatString() != "547813/2" {
		t.Fatalf("orderedDurationComponents(positive) = %s, %s", months, seconds)
	}
	for _, test := range []struct {
		left, right string
		comparison  int
		comparable  bool
	}{
		{left: "invalid", right: "P1D"},
		{left: "P1D", right: "invalid"},
		{left: "P1D", right: "P1D", comparable: true},
		{left: "P1D", right: "P2D", comparison: -1, comparable: true},
		{left: "P2D", right: "P1D", comparison: 1, comparable: true},
		{left: "P1M", right: "P30D"},
	} {
		comparison, comparable := compareOrderedDurations(test.left, test.right)
		if comparison != test.comparison || comparable != test.comparable {
			t.Errorf("compareOrderedDurations(%q, %q) = %d, %t; want %d, %t", test.left, test.right, comparison, comparable, test.comparison, test.comparable)
		}
	}

	for _, test := range []struct {
		value, divisor string
		quotient, rem  string
	}{{"13", "12", "1", "1"}, {"-1", "12", "-1", "11"}, {"-12", "12", "-1", "0"}, {"-13", "12", "-2", "11"}} {
		value, _ := new(big.Int).SetString(test.value, 10)
		divisor, _ := new(big.Int).SetString(test.divisor, 10)
		quotient, remainder := orderedFloorDivMod(value, divisor.Int64())
		if quotient.String() != test.quotient || remainder.String() != test.rem {
			t.Errorf("orderedFloorDivMod(%s, %s) = %s, %s", test.value, test.divisor, quotient, remainder)
		}
	}
	for _, test := range []struct {
		year string
		days string
	}{{"0", "0"}, {"1", "366"}, {"4", "1461"}, {"100", "36525"}, {"400", "146097"}, {"1900", "693961"}, {"2000", "730485"}, {"-1", "-365"}} {
		year, _ := new(big.Int).SetString(test.year, 10)
		if got := orderedDaysBeforeYear(year).String(); got != test.days {
			t.Errorf("orderedDaysBeforeYear(%s) = %s, want %s", test.year, got, test.days)
		}
	}

	for _, test := range []struct {
		year string
		leap bool
	}{{"2000", true}, {"2004", true}, {"1900", false}, {"2001", false}, {"0", true}, {"-1", false}} {
		year, _ := new(big.Int).SetString(test.year, 10)
		if got := orderedBigYearIsLeap(year); got != test.leap {
			t.Errorf("orderedBigYearIsLeap(%s) = %t, want %t", test.year, got, test.leap)
		}
	}
	year2000 := big.NewInt(2000)
	if got := orderedDaysBeforeMonth(year2000, 2); got != 31 {
		t.Errorf("orderedDaysBeforeMonth(February) = %d", got)
	}
	if got := orderedDaysBeforeMonth(year2000, 3); got != 60 {
		t.Errorf("orderedDaysBeforeMonth(March leap) = %d", got)
	}
	if got := orderedDaysBeforeMonth(big.NewInt(1900), 3); got != 59 {
		t.Errorf("orderedDaysBeforeMonth(March common) = %d", got)
	}

	for _, test := range []struct {
		lexical       string
		core          string
		present       bool
		offsetSeconds int64
	}{
		{lexical: "2000Z", core: "2000", present: true},
		{lexical: "2000+14:00", core: "2000", present: true, offsetSeconds: 14 * 3600},
		{lexical: "2000-05:30", core: "2000", present: true, offsetSeconds: -(5*3600 + 30*60)},
		{lexical: "2000", core: "2000"},
		{lexical: "short", core: "short"},
		{lexical: "+00:00", core: "", present: true},
	} {
		core, present, offset := orderedCalendarTimezone(test.lexical)
		if core != test.core || present != test.present || offset != test.offsetSeconds {
			t.Errorf("orderedCalendarTimezone(%q) = %q, %t, %d", test.lexical, core, present, offset)
		}
	}
	if got := orderedCalendarYear("-", "0001").String(); got != "0" {
		t.Errorf("orderedCalendarYear(-0001) = %s", got)
	}
	if got := orderedCalendarYear("", "0001").String(); got != "1" {
		t.Errorf("orderedCalendarYear(0001) = %s", got)
	}
	if got := orderedCalendarSeconds(big.NewInt(0), 1, 1, 0, 0, new(big.Rat)); got.Sign() != 0 {
		t.Errorf("orderedCalendarSeconds(epoch) = %s", got)
	}
	if got := orderedCalendarSeconds(big.NewInt(0), 1, 2, 1, 1, big.NewRat(3, 2)); got.RatString() != "180123/2" {
		t.Errorf("orderedCalendarSeconds(day and clock) = %s", got)
	}

	for _, test := range []struct {
		left, right string
		comparison  int
		comparable  bool
	}{
		{left: "2000-01-01T00:00:00Z", right: "2000-01-01T00:00:00Z", comparable: true},
		{left: "1999-12-31T23:59:59Z", right: "2000-01-01T00:00:00Z", comparison: -1, comparable: true},
		{left: "2000-01-01T00:00:01Z", right: "2000-01-01T00:00:00Z", comparison: 1, comparable: true},
		{left: "1999-12-31T09:59:59Z", right: "2000-01-01T00:00:00", comparison: -1, comparable: true},
		{left: "1999-12-31T10:00:00Z", right: "2000-01-01T00:00:00"},
		{left: "2000-01-01T14:00:00Z", right: "2000-01-01T00:00:00"},
		{left: "2000-01-01T14:00:01Z", right: "2000-01-01T00:00:00", comparison: 1, comparable: true},
		{left: "2000-01-01T00:00:00", right: "1999-12-31T09:59:59Z", comparison: 1, comparable: true},
		{left: "2000-01-01T00:00:00", right: "1999-12-31T10:00:00Z"},
		{left: "2000-01-01T00:00:00", right: "2000-01-01T14:00:00Z"},
		{left: "2000-01-01T00:00:00", right: "2000-01-01T14:00:01Z", comparison: -1, comparable: true},
	} {
		comparison, comparable := compareOrderedCalendars("dateTime", test.left, test.right)
		if comparison != test.comparison || comparable != test.comparable {
			t.Errorf("compareOrderedCalendars(%q, %q) = %d, %t; want %d, %t", test.left, test.right, comparison, comparable, test.comparison, test.comparable)
		}
	}
}

func TestPatternTranslatorAndRuneSetBoundaries(t *testing.T) {
	t.Parallel()

	translator := patternTranslator{source: "^abc"}
	if translator.consume('a') || translator.index != 0 {
		t.Fatal("consume advanced on a mismatch")
	}
	if !translator.consume('^') || translator.index != 1 {
		t.Fatal("consume did not advance on a match")
	}
	for _, source := range []string{"[a", "[a-[b]"} {
		translator := patternTranslator{source: source}
		if _, err := translator.parseClass(); err == nil {
			t.Errorf("parseClass(%q) succeeded", source)
		}
	}
	memberTranslator := patternTranslator{source: "xé", index: 1}
	member, literal, isLiteral, err := memberTranslator.parseClassMember()
	if err != nil || memberTranslator.index != len("xé") || literal != 'é' || !isLiteral || !reflect.DeepEqual(member, runeSet{{'é', 'é'}}) {
		t.Errorf("parseClassMember(multibyte) = %v, %q, %t, index %d, error %v", member, literal, isLiteral, memberTranslator.index, err)
	}
	for _, test := range []struct {
		source string
		set    runeSet
	}{
		{source: "[a]", set: runeSet{{'a', 'a'}}},
		{source: "[^a]", set: xmlUniverse.subtract(runeSet{{'a', 'a'}})},
		{source: "[a-c]", set: runeSet{{'a', 'c'}}},
		{source: "[a-a]", set: runeSet{{'a', 'a'}}},
		{source: "[a-z-[b-y]]", set: runeSet{{'a', 'a'}, {'z', 'z'}}},
	} {
		translator := patternTranslator{source: test.source}
		set, err := translator.parseClass()
		if err != nil || translator.index != len(test.source) || !reflect.DeepEqual(set, test.set) {
			t.Errorf("parseClass(%q) = %v, index %d, error %v; want %v", test.source, set, translator.index, err, test.set)
		}
	}

	for _, test := range []struct {
		source    string
		set       runeSet
		literal   rune
		wantError bool
	}{
		{source: `\n`, literal: '\n'},
		{source: `\r`, literal: '\r'},
		{source: `\t`, literal: '\t'},
		{source: `\-`, literal: '-'},
		{source: `\i`, set: xmlNameStartSet},
		{source: `\c`, set: xmlNameChar},
		{source: `\p{Lu}`, set: categorySet("Lu")},
		{source: `\q`, wantError: true},
		{source: `\`, wantError: true},
	} {
		translator := patternTranslator{source: test.source}
		set, literal, isLiteral, err := translator.parseEscape()
		if (err != nil) != test.wantError {
			t.Errorf("parseEscape(%q) error = %v", test.source, err)
			continue
		}
		wantLiteral := test.set == nil && !test.wantError
		if err == nil && (literal != test.literal || isLiteral != wantLiteral || !reflect.DeepEqual(set, test.set)) {
			t.Errorf("parseEscape(%q) = %v, %d; want %v, %d", test.source, set, literal, test.set, test.literal)
		}
	}
	if !withinByteLimit(8, 8) || withinByteLimit(9, 8) {
		t.Fatal("withinByteLimit does not include only the exact upper bound")
	}

	for _, test := range []struct {
		set, other runeSet
		union      runeSet
		intersect  runeSet
		subtract   runeSet
	}{
		{
			set:       runeSet{{1, 3}, {8, 10}},
			other:     runeSet{{3, 8}},
			union:     runeSet{{1, 10}},
			intersect: runeSet{{3, 3}, {8, 8}},
			subtract:  runeSet{{1, 2}, {9, 10}},
		},
		{
			set:       runeSet{{1, 10}},
			other:     runeSet{{0, 1}, {4, 6}, {10, 12}},
			union:     runeSet{{0, 12}},
			intersect: runeSet{{1, 1}, {4, 6}, {10, 10}},
			subtract:  runeSet{{2, 3}, {7, 9}},
		},
	} {
		if got := test.set.union(test.other); !reflect.DeepEqual(got, test.union) {
			t.Errorf("union(%v, %v) = %v, want %v", test.set, test.other, got, test.union)
		}
		if got := test.set.intersect(test.other); !reflect.DeepEqual(got, test.intersect) {
			t.Errorf("intersect(%v, %v) = %v, want %v", test.set, test.other, got, test.intersect)
		}
		if got := test.set.subtract(test.other); !reflect.DeepEqual(got, test.subtract) {
			t.Errorf("subtract(%v, %v) = %v, want %v", test.set, test.other, got, test.subtract)
		}
	}

	for _, test := range []struct {
		set  runeSet
		want runeSet
	}{
		{set: nil, want: nil},
		{set: runeSet{{2, 2}}, want: runeSet{{2, 2}}},
		{set: runeSet{{2, 2}, {1, 1}}, want: runeSet{{1, 2}}},
		{set: runeSet{{5, 6}, {1, 2}, {3, 4}, {8, 9}, {8, 10}, {8, 10}}, want: runeSet{{1, 6}, {8, 10}}},
	} {
		if got := test.set.normalized(); !reflect.DeepEqual(got, test.want) {
			t.Errorf("normalized(%v) = %v, want %v", test.set, got, test.want)
		}
	}
	if got := (runeSet{{'a', 'a'}, {'c', 'e'}}).regexp(); got != `[\x{61}\x{63}-\x{65}]` {
		t.Errorf("regexp() = %q", got)
	}

	if count, ok := boundedRangeCount(0, utf8.MaxRune, 1, utf8.MaxRune+1); !ok || count != utf8.MaxRune+1 {
		t.Fatalf("boundedRangeCount(exact limit) = %d, %t", count, ok)
	}
}

func intTestPointer(value int) *int { return &value }
