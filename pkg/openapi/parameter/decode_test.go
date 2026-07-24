package parameter_test

import (
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parameter"
)

func TestDecodeOpenAPI32StyleExamples(t *testing.T) {
	t.Parallel()

	version := mustVersion(t, "3.2.0")
	array := arrayValue(t, stringValue(t, "blue"), stringValue(t, "black"), stringValue(t, "brown"))
	object := objectValue(t,
		jsonvalue.Member{Name: "R", Value: stringValue(t, "100")},
		jsonvalue.Member{Name: "G", Value: stringValue(t, "200")},
		jsonvalue.Member{Name: "B", Value: stringValue(t, "150")},
	)
	tests := []struct {
		name     string
		raw      string
		shape    parameter.Shape
		style    parameter.Style
		location parameter.Location
		explode  bool
		want     jsonvalue.Value
	}{
		{name: "matrix primitive", raw: ";color=blue", shape: parameter.Primitive, style: parameter.Matrix, location: parameter.Path, want: stringValue(t, "blue")},
		{name: "matrix array", raw: ";color=blue,black,brown", shape: parameter.Array, style: parameter.Matrix, location: parameter.Path, want: array},
		{name: "matrix array exploded", raw: ";color=blue;color=black;color=brown", shape: parameter.Array, style: parameter.Matrix, location: parameter.Path, explode: true, want: array},
		{name: "matrix object", raw: ";color=R,100,G,200,B,150", shape: parameter.Object, style: parameter.Matrix, location: parameter.Path, want: object},
		{name: "matrix object exploded", raw: ";R=100;G=200;B=150", shape: parameter.Object, style: parameter.Matrix, location: parameter.Path, explode: true, want: object},
		{name: "label primitive", raw: ".blue", shape: parameter.Primitive, style: parameter.Label, location: parameter.Path, want: stringValue(t, "blue")},
		{name: "label array", raw: ".blue,black,brown", shape: parameter.Array, style: parameter.Label, location: parameter.Path, want: array},
		{name: "label array exploded", raw: ".blue.black.brown", shape: parameter.Array, style: parameter.Label, location: parameter.Path, explode: true, want: array},
		{name: "label object", raw: ".R,100,G,200,B,150", shape: parameter.Object, style: parameter.Label, location: parameter.Path, want: object},
		{name: "label object exploded", raw: ".R=100.G=200.B=150", shape: parameter.Object, style: parameter.Label, location: parameter.Path, explode: true, want: object},
		{name: "simple primitive", raw: "blue", shape: parameter.Primitive, style: parameter.Simple, location: parameter.Header, want: stringValue(t, "blue")},
		{name: "simple array", raw: "blue,black,brown", shape: parameter.Array, style: parameter.Simple, location: parameter.Header, want: array},
		{name: "simple object", raw: "R,100,G,200,B,150", shape: parameter.Object, style: parameter.Simple, location: parameter.Header, want: object},
		{name: "simple object exploded", raw: "R=100,G=200,B=150", shape: parameter.Object, style: parameter.Simple, location: parameter.Header, explode: true, want: object},
		{name: "form primitive", raw: "color=blue", shape: parameter.Primitive, style: parameter.Form, location: parameter.Query, want: stringValue(t, "blue")},
		{name: "form array", raw: "color=blue,black,brown", shape: parameter.Array, style: parameter.Form, location: parameter.Query, want: array},
		{name: "form array exploded", raw: "color=blue&color=black&color=brown", shape: parameter.Array, style: parameter.Form, location: parameter.Query, explode: true, want: array},
		{name: "form object", raw: "color=R,100,G,200,B,150", shape: parameter.Object, style: parameter.Form, location: parameter.Query, want: object},
		{name: "form object exploded", raw: "R=100&G=200&B=150", shape: parameter.Object, style: parameter.Form, location: parameter.Query, explode: true, want: object},
		{name: "space delimited", raw: "color=blue%20black%20brown", shape: parameter.Array, style: parameter.SpaceDelimited, location: parameter.Query, want: array},
		{name: "space delimited object", raw: "color=R%20100%20G%20200%20B%20150", shape: parameter.Object, style: parameter.SpaceDelimited, location: parameter.Query, want: object},
		{name: "pipe delimited", raw: "color=blue%7Cblack%7Cbrown", shape: parameter.Array, style: parameter.PipeDelimited, location: parameter.Query, want: array},
		{name: "pipe delimited object", raw: "color=R%7c100%7CG%7c200%7CB%7C150", shape: parameter.Object, style: parameter.PipeDelimited, location: parameter.Query, want: object},
		{name: "deep object", raw: "color%5BR%5D=100&color%5BG%5D=200&color%5BB%5D=150", shape: parameter.Object, style: parameter.DeepObject, location: parameter.Query, want: object},
		{name: "cookie primitive", raw: "color=blue", shape: parameter.Primitive, style: parameter.Cookie, location: parameter.CookieLocation, want: stringValue(t, "blue")},
		{name: "cookie array", raw: "color=blue,black,brown", shape: parameter.Array, style: parameter.Cookie, location: parameter.CookieLocation, want: array},
		{name: "cookie array exploded", raw: "color=blue; color=black; color=brown", shape: parameter.Array, style: parameter.Cookie, location: parameter.CookieLocation, explode: true, want: array},
		{name: "cookie object", raw: "color=R,100,G,200,B,150", shape: parameter.Object, style: parameter.Cookie, location: parameter.CookieLocation, want: object},
		{name: "cookie object exploded", raw: "R=100; G=200; B=150", shape: parameter.Object, style: parameter.Cookie, location: parameter.CookieLocation, explode: true, want: object},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := parameter.Decode("color", test.raw, test.shape, parameter.Options{
				Version:  version,
				Location: test.location,
				Style:    test.style,
				Explode:  test.explode,
			})
			if err != nil {
				t.Fatal(err)
			}
			gotJSON, _ := got.MarshalJSON()
			wantJSON, _ := test.want.MarshalJSON()
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("Decode() = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

func TestDecodePassesHeaderValuesThroughUnchanged(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"3.1.2", "3.2.0"} {
		value, err := parameter.Decode(
			"X-Value",
			`a/b?c=%2F "quoted"`,
			parameter.Primitive,
			parameter.Options{
				Version:  mustVersion(t, version),
				Location: parameter.Header,
				Style:    parameter.Simple,
			},
		)
		if err != nil {
			t.Fatal(err)
		}
		actual, _ := value.Text()
		if actual != `a/b?c=%2F "quoted"` {
			t.Fatalf("Decode() = %q", actual)
		}
	}
	value, err := parameter.Decode(
		"value", `value=a/b?c=%2F "quoted"`, parameter.Primitive,
		parameter.Options{
			Version:  mustVersion(t, "3.2.0"),
			Location: parameter.CookieLocation,
			Style:    parameter.Cookie,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := value.Text()
	if actual != `a/b?c=%2F "quoted"` {
		t.Fatalf("cookie Decode() = %q", actual)
	}
}

func TestQueryDecodingAppliesFormURLRules(t *testing.T) {
	t.Parallel()

	value, err := parameter.Decode(
		"value", "value=a+b%2Fc", parameter.Primitive,
		parameter.Options{
			Version:  mustVersion(t, "3.2.0"),
			Location: parameter.Query,
			Style:    parameter.Form,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := value.Text()
	if actual != "a b/c" {
		t.Fatalf("Decode() = %q", actual)
	}
}
