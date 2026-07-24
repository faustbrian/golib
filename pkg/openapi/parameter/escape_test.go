package parameter_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parameter"
)

func TestEscapeAmbiguousDelimitersDefinesReversibleConvention(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		style   parameter.Style
		value   string
		escaped string
	}{
		{style: parameter.SpaceDelimited, value: "a b%20", escaped: "a%20b%2520"},
		{style: parameter.PipeDelimited, value: "a|b%7C", escaped: "a%7Cb%257C"},
		{style: parameter.DeepObject, value: "a[b]%5B", escaped: "a%5Bb%5D%255B"},
	} {
		escaped, err := parameter.EscapeAmbiguousDelimiters(
			test.value, test.style, 100,
		)
		if err != nil {
			t.Fatal(err)
		}
		if escaped != test.escaped {
			t.Fatalf("EscapeAmbiguousDelimiters(%q) = %q, want %q",
				test.value, escaped, test.escaped)
		}
		actual, err := parameter.UnescapeAmbiguousDelimiters(
			escaped, test.style, 100,
		)
		if err != nil {
			t.Fatal(err)
		}
		if actual != test.value {
			t.Fatalf("round trip = %q, want %q", actual, test.value)
		}
	}
}

func TestAmbiguousDelimiterConventionSurvivesStandardCodec(t *testing.T) {
	t.Parallel()

	escaped, err := parameter.EscapeAmbiguousDelimiters(
		"a b", parameter.SpaceDelimited, 100,
	)
	if err != nil {
		t.Fatal(err)
	}
	options := parameter.Options{
		Version: mustVersion(t, "3.2.0"), Location: parameter.Query,
		Style: parameter.SpaceDelimited,
	}
	encoded, err := parameter.Encode(
		"value", arrayValue(t, stringValue(t, escaped), stringValue(t, "c")),
		options,
	)
	if err != nil {
		t.Fatal(err)
	}
	if encoded != "value=a%2520b%20c" {
		t.Fatalf("encoded = %q", encoded)
	}
	decoded, err := parameter.Decode("value", encoded, parameter.Array, options)
	if err != nil {
		t.Fatal(err)
	}
	elements, _ := decoded.Elements()
	actual, err := parameter.UnescapeAmbiguousDelimiters(
		mustText(t, elements[0]), parameter.SpaceDelimited, 100,
	)
	if err != nil || actual != "a b" {
		t.Fatalf("unescaped = %q, %v", actual, err)
	}
}

func TestAmbiguousDelimiterEscapingPreservesUnknownTripletsAndCase(t *testing.T) {
	t.Parallel()

	actual, err := parameter.UnescapeAmbiguousDelimiters(
		"a%7cb%25%2F%2", parameter.PipeDelimited, 100,
	)
	if err != nil || actual != "a|b%%2F%2" {
		t.Fatalf("unescaped = %q, %v", actual, err)
	}
}

func TestAmbiguousDelimiterEscapingValidatesInputsAndBounds(t *testing.T) {
	t.Parallel()

	invalidUTF8 := string([]byte{0xff})
	for _, test := range []struct {
		name   string
		value  string
		style  parameter.Style
		limit  int
		decode bool
		want   error
	}{
		{name: "style", style: parameter.Form, limit: 1,
			want: parameter.ErrInvalidDelimiterEscape},
		{name: "maximum", style: parameter.SpaceDelimited,
			want: parameter.ErrInvalidDelimiterEscape},
		{name: "UTF-8", value: invalidUTF8, style: parameter.SpaceDelimited,
			limit: 1, want: parameter.ErrInvalidDelimiterEscape},
		{name: "input limit", value: "ab", style: parameter.SpaceDelimited,
			limit: 1, want: parameter.ErrDelimiterEscapeLimit},
		{name: "expansion limit", value: "a b", style: parameter.SpaceDelimited,
			limit: 4, want: parameter.ErrDelimiterEscapeLimit},
		{name: "delimiter expansion limit", value: "a ",
			style: parameter.SpaceDelimited, limit: 3,
			want: parameter.ErrDelimiterEscapeLimit},
		{name: "decode style", style: parameter.Form, limit: 1, decode: true,
			want: parameter.ErrInvalidDelimiterEscape},
		{name: "decode maximum", style: parameter.SpaceDelimited, decode: true,
			want: parameter.ErrInvalidDelimiterEscape},
		{name: "decode UTF-8", value: invalidUTF8,
			style: parameter.SpaceDelimited, limit: 1, decode: true,
			want: parameter.ErrInvalidDelimiterEscape},
		{name: "decode limit", value: "%20", style: parameter.SpaceDelimited,
			limit: 2, decode: true, want: parameter.ErrDelimiterEscapeLimit},
	} {
		var err error
		if test.decode {
			_, err = parameter.UnescapeAmbiguousDelimiters(
				test.value, test.style, test.limit,
			)
		} else {
			_, err = parameter.EscapeAmbiguousDelimiters(
				test.value, test.style, test.limit,
			)
		}
		if !errors.Is(err, test.want) {
			t.Fatalf("%s error = %v, want %v", test.name, err, test.want)
		}
	}

	for _, operation := range []func(string, parameter.Style, int) (string, error){
		parameter.EscapeAmbiguousDelimiters,
		parameter.UnescapeAmbiguousDelimiters,
	} {
		actual, err := operation("a b", parameter.SpaceDelimited, 5)
		if err != nil || actual != "a%20b" && actual != "a b" {
			t.Fatalf("exact bound = %q, %v", actual, err)
		}
	}

	for _, test := range []struct {
		name   string
		value  string
		limit  int
		decode bool
		want   string
	}{
		{name: "escape exact input", value: "abc", limit: 3, want: "abc"},
		{name: "escape exact delimiter", value: "a ", limit: 4,
			want: "a%20"},
		{name: "escape minimum", value: "a", limit: 1, want: "a"},
		{name: "decode exact input", value: "abc", limit: 3,
			decode: true, want: "abc"},
		{name: "decode minimum", value: "a", limit: 1,
			decode: true, want: "a"},
	} {
		var actual string
		var err error
		if test.decode {
			actual, err = parameter.UnescapeAmbiguousDelimiters(
				test.value, parameter.SpaceDelimited, test.limit,
			)
		} else {
			actual, err = parameter.EscapeAmbiguousDelimiters(
				test.value, parameter.SpaceDelimited, test.limit,
			)
		}
		if err != nil || actual != test.want {
			t.Fatalf("%s = %q, %v, want %q", test.name, actual, err, test.want)
		}
	}
}

func mustText(t *testing.T, value jsonvalue.Value) string {
	t.Helper()
	text, valid := value.Text()
	if !valid {
		t.Fatalf("value is not text: %#v", value)
	}
	return text
}
