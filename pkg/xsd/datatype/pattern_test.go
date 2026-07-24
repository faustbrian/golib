package datatype_test

import (
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/xsd/datatype"
)

func TestCompilePatternUsesXMLSchemaSemantics(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		pattern string
		value   string
		match   bool
	}{
		{pattern: "ab", value: "ab", match: true},
		{pattern: "ab", value: "zabz"},
		{pattern: "^ab$", value: "^ab$", match: true},
		{pattern: "^ab$", value: "x^ab$y"},
		{pattern: "^ab$", value: "ab"},
		{pattern: "a.b", value: "a b", match: true},
		{pattern: "a.b", value: "a\rb"},
		{pattern: "a.b", value: "a\nb"},
		{pattern: `[.^$]`, value: "^", match: true},
		{pattern: `\i\c*`, value: "prefix:name-1", match: true},
		{pattern: `[\i]+`, value: "Name", match: true},
		{pattern: `[\c]+`, value: "name-1", match: true},
		{pattern: `[\i-[:]]+`, value: "Name", match: true},
		{pattern: `[\i-[:]]+`, value: ":"},
		{pattern: `[\c-[:]]+`, value: "name-1", match: true},
		{pattern: `[\c-[:]]+`, value: ":"},
		{pattern: `\p{IsBasicLatin}+`, value: "ASCII", match: true},
		{pattern: `\p{IsBasicLatin}+`, value: "café"},
		{pattern: `\P{IsBasicLatin}+`, value: "é", match: true},
		{pattern: `\p{Lu}\p{Ll}+`, value: "Schema", match: true},
		{pattern: `\d+`, value: "١٢٣", match: true},
		{pattern: `\w+`, value: "word́", match: true},
		{pattern: `\w+`, value: "word!"},
		{pattern: `[a-z-[aeiou]]+`, value: "xsd", match: true},
		{pattern: `[a-z-[aeiou]]+`, value: "schema"},
		{pattern: `[^a-z-[aeiou]]+`, value: "AEIOU", match: true},
		{pattern: `[^a-z-[aeiou]]+`, value: "x"},
		{pattern: `[a-z-[d-f-[e]]]+`, value: "ace", match: true},
		{pattern: `[a-z-[d-f-[e]]]+`, value: "d"},
		{pattern: `[\p{L}-[A-Z]]+`, value: "Schema"},
		{pattern: `[\p{L}-[A-Z]]+`, value: "schema", match: true},
		{pattern: `[a\-c]+`, value: "a-c", match: true},
		{pattern: `[^a]+`, value: "b", match: true},
		{pattern: `\r\t`, value: "\r\t", match: true},
		{pattern: `\S+`, value: "non-space", match: true},
		{pattern: `\D+`, value: "letters", match: true},
		{pattern: `\W+`, value: "!?", match: true},
	} {
		compiled, err := datatype.CompilePattern(test.pattern)
		if err != nil {
			t.Fatalf("CompilePattern(%q) error = %v", test.pattern, err)
		}
		if got := compiled.MatchString(test.value); got != test.match {
			t.Fatalf("CompilePattern(%q).MatchString(%q) = %t; want %t", test.pattern, test.value, got, test.match)
		}
	}
}

func TestCompilePatternBoundsTranslationWork(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{
		strings.Repeat("a", 1<<20+1),
		strings.Repeat(`\p{L}`, 1000),
	} {
		if _, err := datatype.CompilePattern(pattern); err == nil {
			t.Fatalf("CompilePattern() accepted %d bytes of amplified input", len(pattern))
		}
	}
}

func TestCompilePatternRejectsInvalidExpressions(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{
		"[",
		"[]",
		`[a-]`,
		`[a-\q]`,
		`[-[a]]`,
		`[a-[]]`,
		`[a-[b]c]`,
		`[\p{L}-a]`,
		`[a-\p{L}]`,
		`[z-a]`,
		`[[a]`,
		`[\q]`,
		`\`,
		`\q`,
		`\pL`,
		`\p{L`,
		`\p{Unknown}`,
		`\p{IsNotABlock}`,
		string([]byte{0xff}),
		string([]byte{'[', 0xff, ']'}),
	} {
		if _, err := datatype.CompilePattern(pattern); err == nil {
			t.Fatalf("CompilePattern(%q) accepted an invalid expression", pattern)
		}
	}
}
