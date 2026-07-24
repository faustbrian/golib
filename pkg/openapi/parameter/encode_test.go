package parameter_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parameter"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestEncodeMatchesOpenAPI32StyleExamples(t *testing.T) {
	t.Parallel()

	version := mustVersion(t, "3.2.0")
	primitive := stringValue(t, "blue")
	array := arrayValue(t, stringValue(t, "blue"), stringValue(t, "black"), stringValue(t, "brown"))
	object := objectValue(t,
		jsonvalue.Member{Name: "R", Value: numberValue(t, "100")},
		jsonvalue.Member{Name: "G", Value: numberValue(t, "200")},
		jsonvalue.Member{Name: "B", Value: numberValue(t, "150")},
	)
	tests := []struct {
		name     string
		style    parameter.Style
		location parameter.Location
		explode  bool
		value    jsonvalue.Value
		want     string
	}{
		{name: "matrix primitive", style: parameter.Matrix, location: parameter.Path, value: primitive, want: ";color=blue"},
		{name: "matrix array", style: parameter.Matrix, location: parameter.Path, value: array, want: ";color=blue,black,brown"},
		{name: "matrix array exploded", style: parameter.Matrix, location: parameter.Path, explode: true, value: array, want: ";color=blue;color=black;color=brown"},
		{name: "matrix object", style: parameter.Matrix, location: parameter.Path, value: object, want: ";color=R,100,G,200,B,150"},
		{name: "matrix object exploded", style: parameter.Matrix, location: parameter.Path, explode: true, value: object, want: ";R=100;G=200;B=150"},
		{name: "label array", style: parameter.Label, location: parameter.Path, value: array, want: ".blue,black,brown"},
		{name: "label array exploded", style: parameter.Label, location: parameter.Path, explode: true, value: array, want: ".blue.black.brown"},
		{name: "label primitive", style: parameter.Label, location: parameter.Path, value: primitive, want: ".blue"},
		{name: "label object", style: parameter.Label, location: parameter.Path, value: object, want: ".R,100,G,200,B,150"},
		{name: "label object exploded", style: parameter.Label, location: parameter.Path, explode: true, value: object, want: ".R=100.G=200.B=150"},
		{name: "simple primitive", style: parameter.Simple, location: parameter.Header, value: primitive, want: "blue"},
		{name: "simple array", style: parameter.Simple, location: parameter.Header, explode: true, value: array, want: "blue,black,brown"},
		{name: "simple object", style: parameter.Simple, location: parameter.Header, value: object, want: "R,100,G,200,B,150"},
		{name: "simple object exploded", style: parameter.Simple, location: parameter.Header, explode: true, value: object, want: "R=100,G=200,B=150"},
		{name: "form array", style: parameter.Form, location: parameter.Query, value: array, want: "color=blue,black,brown"},
		{name: "form primitive", style: parameter.Form, location: parameter.Query, value: primitive, want: "color=blue"},
		{name: "form object", style: parameter.Form, location: parameter.Query, value: object, want: "color=R,100,G,200,B,150"},
		{name: "form array exploded", style: parameter.Form, location: parameter.Query, explode: true, value: array, want: "color=blue&color=black&color=brown"},
		{name: "form object exploded", style: parameter.Form, location: parameter.Query, explode: true, value: object, want: "R=100&G=200&B=150"},
		{name: "space delimited", style: parameter.SpaceDelimited, location: parameter.Query, value: array, want: "color=blue%20black%20brown"},
		{name: "space delimited object", style: parameter.SpaceDelimited, location: parameter.Query, value: object, want: "color=R%20100%20G%20200%20B%20150"},
		{name: "pipe delimited", style: parameter.PipeDelimited, location: parameter.Query, value: array, want: "color=blue%7Cblack%7Cbrown"},
		{name: "pipe delimited object", style: parameter.PipeDelimited, location: parameter.Query, value: object, want: "color=R%7C100%7CG%7C200%7CB%7C150"},
		{name: "deep object", style: parameter.DeepObject, location: parameter.Query, explode: true, value: object, want: "color%5BR%5D=100&color%5BG%5D=200&color%5BB%5D=150"},
		{name: "cookie array", style: parameter.Cookie, location: parameter.CookieLocation, value: array, want: "color=blue,black,brown"},
		{name: "cookie primitive", style: parameter.Cookie, location: parameter.CookieLocation, value: primitive, want: "color=blue"},
		{name: "cookie object", style: parameter.Cookie, location: parameter.CookieLocation, value: object, want: "color=R,100,G,200,B,150"},
		{name: "cookie object exploded", style: parameter.Cookie, location: parameter.CookieLocation, explode: true, value: object, want: "R=100; G=200; B=150"},
		{name: "cookie array exploded", style: parameter.Cookie, location: parameter.CookieLocation, explode: true, value: array, want: "color=blue; color=black; color=brown"},
		{name: "undefined matrix", style: parameter.Matrix, location: parameter.Path, value: jsonvalue.Null(), want: ";color"},
		{name: "undefined label", style: parameter.Label, location: parameter.Path, value: jsonvalue.Null(), want: "."},
		{name: "undefined simple", style: parameter.Simple, location: parameter.Header, value: jsonvalue.Null(), want: ""},
		{name: "undefined form", style: parameter.Form, location: parameter.Query, value: jsonvalue.Null(), want: "color="},
		{name: "undefined cookie", style: parameter.Cookie, location: parameter.CookieLocation, value: jsonvalue.Null(), want: "color="},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := parameter.Encode("color", test.value, parameter.Options{
				Version: version, Location: test.location,
				Style: test.style, Explode: test.explode,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("Encode() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestEncodeAllowReservedAppliesOnlyToValues(t *testing.T) {
	t.Parallel()

	value := objectValue(t, jsonvalue.Member{
		Name:  "R",
		Value: stringValue(t, "a/b?c"),
	})
	got, err := parameter.Encode("color", value, parameter.Options{
		Version:       mustVersion(t, "3.2.0"),
		Location:      parameter.Query,
		Style:         parameter.DeepObject,
		AllowReserved: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "color%5BR%5D=a/b?c" {
		t.Fatalf("Encode() = %q", got)
	}
}

func TestEncodeEscapesGenericSyntaxInPathParameters(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		for _, test := range []struct {
			style parameter.Style
			want  string
		}{
			{style: parameter.Simple, want: "a%2Fb%3Fc%23d"},
			{style: parameter.Label, want: ".a%2Fb%3Fc%23d"},
			{style: parameter.Matrix, want: ";id=a%2Fb%3Fc%23d"},
		} {
			got, err := parameter.Encode(
				"id",
				stringValue(t, "a/b?c#d"),
				parameter.Options{
					Version:  mustVersion(t, version),
					Location: parameter.Path,
					Style:    test.style,
				},
			)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("version %s style %s encoding = %q",
					version, test.style, got)
			}
		}
	}
}

func TestEncodePassesHeaderValuesThroughUnchanged(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.1.2", "3.2.0"} {
		got, err := parameter.Encode(
			"X-Value",
			stringValue(t, `a/b?c=%2F "quoted"`),
			parameter.Options{
				Version:  mustVersion(t, version),
				Location: parameter.Header,
				Style:    parameter.Simple,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		if got != `a/b?c=%2F "quoted"` {
			t.Fatalf("Encode() = %q", got)
		}
	}
	got, err := parameter.Encode(
		"value", stringValue(t, `a/b?c=%2F "quoted"`),
		parameter.Options{
			Version:  mustVersion(t, "3.2.0"),
			Location: parameter.CookieLocation,
			Style:    parameter.Cookie,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != `value=a/b?c=%2F "quoted"` {
		t.Fatalf("cookie Encode() = %q", got)
	}
}

func TestQueryEncodingPreservesDelimitersAndEscapesNames(t *testing.T) {
	t.Parallel()

	got, err := parameter.Encode(
		"weird name",
		arrayValue(t, stringValue(t, "a/b"), stringValue(t, "c+d")),
		parameter.Options{
			Version:  mustVersion(t, "3.2.0"),
			Location: parameter.Query,
			Style:    parameter.Form,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != "weird%20name=a%2Fb,c%2Bd" {
		t.Fatalf("Encode() = %q", got)
	}
}

func TestQueryEncodingAppliesFormRulesToNames(t *testing.T) {
	t.Parallel()

	got, err := parameter.Encode(
		"~", stringValue(t, "AZaz09-._~@"), parameter.Options{
			Version: mustVersion(t, "3.2.0"), Location: parameter.Query,
			Style: parameter.Form,
		},
	)
	if err != nil || got != "%7E=AZaz09-._%7E%40" {
		t.Fatalf("query endpoint encoding = %q, %v", got, err)
	}
}

func mustVersion(t *testing.T, raw string) specversion.Version {
	t.Helper()
	version, err := specversion.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func stringValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.String(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func numberValue(t *testing.T, raw string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Number(raw)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func arrayValue(t *testing.T, values ...jsonvalue.Value) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Array(values)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func objectValue(t *testing.T, members ...jsonvalue.Member) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Object(members)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
