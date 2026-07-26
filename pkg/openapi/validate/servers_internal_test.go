package validate

import (
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestServerLiteralRuneIntervalsIncludeExactEndpoints(t *testing.T) {
	t.Parallel()

	for _, interval := range serverLiteralRuneIntervals {
		if !validServerLiteralRune(interval.first) {
			t.Errorf("first rune U+%04X was rejected", interval.first)
		}
		if !validServerLiteralRune(interval.last) {
			t.Errorf("last rune U+%04X was rejected", interval.last)
		}
	}
	for _, character := range []rune{
		0, 0x20, 0x22, 0x25, 0x3c, 0x3e, 0x5c, 0x5e, 0x60, 0x7b,
		0x7d, 0x7f, 0x9f, 0xd800, 0xdfff, 0xfdd0, 0xffff,
		0x1fffe, 0xe0000, 0xe0fff, 0x10fffe,
	} {
		if validServerLiteralRune(character) {
			t.Errorf("excluded rune U+%04X was accepted", character)
		}
	}
}

func TestOpenAPI32ServerURLTemplateLexingBoundaries(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		value string
		valid bool
	}{
		{value: ""},
		{value: "!", valid: true},
		{value: "https://example.test/{name}", valid: true},
		{value: "%20!", valid: true},
		{value: "%20 "},
		{value: "%2"},
		{value: "%2G"},
		{value: "{name"},
		{value: string([]byte{0xff})},
		{value: "é", valid: true},
	} {
		if got := validServerURLTemplate32(test.value); got != test.valid {
			t.Errorf("validServerURLTemplate32(%q) = %t", test.value, got)
		}
	}

	for _, test := range []struct {
		value string
		valid bool
		count int
	}{
		{value: "plain", valid: true},
		{value: "{one}", valid: true, count: 1},
		{value: "{one}{two}", valid: true, count: 2},
		{value: "{"},
		{value: "}"},
		{value: "{}"},
		{value: "{{one}}"},
	} {
		variables, valid := serverTemplateVariables(test.value)
		if valid != test.valid || len(variables) != test.count {
			t.Errorf("serverTemplateVariables(%q) = %#v, %t", test.value, variables, valid)
		}
	}
}

func TestServerDialectDecisionsAreExact(t *testing.T) {
	t.Parallel()

	server := testValidationValue(t, `{
		"url":"https://{region}.{region}.{region}.example.test?debug=1",
		"variables":{"region":{"default":"eu"},"unused":{"default":"x"}}
	}`)
	for _, test := range []struct {
		dialect specversion.Dialect
		codes   map[string]int
	}{
		{
			dialect: specversion.DialectOAS30,
			codes:   map[string]int{"openapi.server.variable.unused": 1},
		},
		{
			dialect: specversion.DialectOAS31,
			codes: map[string]int{
				"openapi.server.url.query-or-fragment": 1,
				"openapi.server.variable.unused":       1,
			},
		},
		{
			dialect: specversion.DialectOAS32,
			codes: map[string]int{
				"openapi.server.url.query-or-fragment": 1,
				"openapi.server.variable.duplicate":    2,
				"openapi.server.variable.unused":       1,
			},
		},
	} {
		diagnostics := validateServer(server, "/server", "3.x", test.dialect)
		got := make(map[string]int)
		for _, diagnostic := range diagnostics {
			got[diagnostic.Code]++
		}
		if len(got) != len(test.codes) {
			t.Errorf("dialect %q diagnostics = %#v", test.dialect, diagnostics)
			continue
		}
		for code, count := range test.codes {
			if got[code] != count {
				t.Errorf("dialect %q code %q = %d", test.dialect, code, got[code])
			}
		}
	}
}
