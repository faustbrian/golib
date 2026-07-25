package parameter_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parameter"
)

func TestSwagger20CollectionFormatsRoundTrip(t *testing.T) {
	t.Parallel()

	value := arrayValue(t,
		stringValue(t, "blue"),
		stringValue(t, "black&white"),
	)
	tests := []struct {
		name     string
		location parameter.Location
		format   parameter.CollectionFormat
		want     string
	}{
		{name: "default csv query", location: parameter.Query, want: "color=blue,black%26white"},
		{name: "csv path", location: parameter.Path, format: parameter.CollectionCSV, want: "blue,black%26white"},
		{name: "ssv form data", location: parameter.FormData, format: parameter.CollectionSSV, want: "color=blue%20black%26white"},
		{name: "tsv header", location: parameter.Header, format: parameter.CollectionTSV, want: "blue%09black%26white"},
		{name: "pipes query", location: parameter.Query, format: parameter.CollectionPipes, want: "color=blue%7Cblack%26white"},
		{name: "multi query", location: parameter.Query, format: parameter.CollectionMulti, want: "color=blue&color=black%26white"},
		{name: "multi form data", location: parameter.FormData, format: parameter.CollectionMulti, want: "color=blue&color=black%26white"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := parameter.Swagger20Options{
				Location: test.location,
				Format:   test.format,
			}
			encoded, err := parameter.EncodeSwagger20("color", value, options)
			if err != nil {
				t.Fatal(err)
			}
			if encoded != test.want {
				t.Fatalf("EncodeSwagger20() = %q, want %q", encoded, test.want)
			}
			decoded, err := parameter.DecodeSwagger20("color", encoded, parameter.Array, options)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(decoded, value) {
				t.Fatalf("DecodeSwagger20() = %#v, want %#v", decoded, value)
			}
		})
	}
}

func TestSwagger20PrimitiveAndUndefinedValues(t *testing.T) {
	t.Parallel()

	query := parameter.Swagger20Options{Location: parameter.Query}
	encoded, err := parameter.EncodeSwagger20("count", stringValue(t, "10"), query)
	if err != nil || encoded != "count=10" {
		t.Fatalf("primitive encode = %q, %v", encoded, err)
	}
	decoded, err := parameter.DecodeSwagger20("count", encoded, parameter.Primitive, query)
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := decoded.Text(); !ok || text != "10" {
		t.Fatalf("primitive decode = %#v", decoded)
	}
	encoded, err = parameter.EncodeSwagger20("optional", jsonvalue.Null(), query)
	if err != nil || encoded != "optional=" {
		t.Fatalf("null encode = %q, %v", encoded, err)
	}
	query.EmptyDecoding = parameter.EmptyAsNull
	decoded, err = parameter.DecodeSwagger20("optional", encoded, parameter.Primitive, query)
	if err != nil || decoded.Kind() != jsonvalue.NullKind {
		t.Fatalf("null decode = %#v, %v", decoded, err)
	}
}

func TestSwagger20CodecRejectsUndefinedCombinations(t *testing.T) {
	t.Parallel()

	array := arrayValue(t, stringValue(t, "one"), stringValue(t, "two"))
	primitive := stringValue(t, "one")
	tests := []struct {
		name    string
		value   jsonvalue.Value
		shape   parameter.Shape
		options parameter.Swagger20Options
	}{
		{name: "missing name", value: array, shape: parameter.Array, options: parameter.Swagger20Options{Location: parameter.Query}},
		{name: "cookie location", value: array, shape: parameter.Array, options: parameter.Swagger20Options{Location: parameter.CookieLocation}},
		{name: "multi path", value: array, shape: parameter.Array, options: parameter.Swagger20Options{Location: parameter.Path, Format: parameter.CollectionMulti}},
		{name: "object", value: objectValue(t), shape: parameter.Object, options: parameter.Swagger20Options{Location: parameter.Query}},
		{name: "primitive pipes", value: primitive, shape: parameter.Primitive, options: parameter.Swagger20Options{Location: parameter.Query, Format: parameter.CollectionPipes}},
		{name: "unknown format", value: array, shape: parameter.Array, options: parameter.Swagger20Options{Location: parameter.Query, Format: "unknown"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			name := "value"
			if test.name == "missing name" {
				name = ""
			}
			if _, err := parameter.EncodeSwagger20(name, test.value, test.options); !errors.Is(err, parameter.ErrInvalidOptions) {
				t.Fatalf("encode error = %v", err)
			}
			if _, err := parameter.DecodeSwagger20(name, "value=one", test.shape, test.options); !errors.Is(err, parameter.ErrInvalidOptions) {
				t.Fatalf("decode error = %v", err)
			}
		})
	}
}

func TestSwagger20CodecEnforcesSyntaxAndLimits(t *testing.T) {
	t.Parallel()

	options := parameter.Swagger20Options{
		Location: parameter.Query,
		Limits:   parameter.Limits{MaxBytes: 12, MaxItems: 1},
	}
	array := arrayValue(t, stringValue(t, "one"), stringValue(t, "two"))
	if _, err := parameter.EncodeSwagger20("value", array, options); !errors.Is(err, parameter.ErrLimitExceeded) {
		t.Fatalf("encode limit error = %v", err)
	}
	byteOptions := options
	byteOptions.Limits.MaxItems = 10
	if _, err := parameter.EncodeSwagger20("value", stringValue(t, "café"), byteOptions); !errors.Is(err, parameter.ErrLimitExceeded) {
		t.Fatalf("encode byte limit error = %v", err)
	}
	if _, err := parameter.DecodeSwagger20("value", "value=too-long", parameter.Primitive, options); !errors.Is(err, parameter.ErrLimitExceeded) {
		t.Fatalf("decode byte limit error = %v", err)
	}
	options.Limits.MaxBytes = 100
	if _, err := parameter.DecodeSwagger20("value", "value=one,two", parameter.Array, options); !errors.Is(err, parameter.ErrLimitExceeded) {
		t.Fatalf("decode item limit error = %v", err)
	}
	options.Limits.MaxItems = 10
	for _, raw := range []string{"wrong=one", "value=%GG", "value=one&other=two"} {
		if _, err := parameter.DecodeSwagger20("value", raw, parameter.Array, options); !errors.Is(err, parameter.ErrMalformedEncoding) {
			t.Fatalf("DecodeSwagger20(%q) error = %v", raw, err)
		}
	}
}

func TestSwagger20CodecAcceptsExactLimitsAndLocationEncoding(t *testing.T) {
	t.Parallel()

	options := parameter.Swagger20Options{
		Location: parameter.Query,
		Limits:   parameter.Limits{MaxBytes: len("value=x"), MaxItems: 1},
	}
	value := stringValue(t, "x")
	encoded, err := parameter.EncodeSwagger20("value", value, options)
	if err != nil || encoded != "value=x" {
		t.Fatalf("exact Swagger encode = %q, %v", encoded, err)
	}
	if _, err = parameter.DecodeSwagger20(
		"value", encoded, parameter.Primitive, options,
	); err != nil {
		t.Fatalf("exact Swagger decode error = %v", err)
	}
	array := arrayValue(t, value)
	if _, err = parameter.EncodeSwagger20("value", array, options); err != nil {
		t.Fatalf("exact Swagger item encode error = %v", err)
	}
	if _, err = parameter.DecodeSwagger20(
		"value", encoded, parameter.Array, options,
	); err != nil {
		t.Fatalf("exact Swagger item decode error = %v", err)
	}

	for _, test := range []struct {
		location parameter.Location
		want     string
	}{
		{location: parameter.FormData, want: "value=%7E"},
		{location: parameter.Header, want: "~"},
	} {
		got, encodeErr := parameter.EncodeSwagger20(
			"value", stringValue(t, "~"),
			parameter.Swagger20Options{Location: test.location},
		)
		if encodeErr != nil || got != test.want {
			t.Fatalf("location %q encoding = %q, %v", test.location, got, encodeErr)
		}
	}
}

func TestSwagger20DecodeRejectsItemAmplificationBeforeMalformedTokens(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		raw    string
		format parameter.CollectionFormat
	}{
		{name: "csv", raw: "value=ok,%", format: parameter.CollectionCSV},
		{name: "multi", raw: "value=ok&value=%", format: parameter.CollectionMulti},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			options := parameter.Swagger20Options{
				Location: parameter.Query,
				Format:   test.format,
				Limits:   parameter.Limits{MaxBytes: 100, MaxItems: 1},
			}
			if _, err := parameter.DecodeSwagger20(
				"value", test.raw, parameter.Array, options,
			); !errors.Is(err, parameter.ErrLimitExceeded) {
				t.Fatalf("amplified decode error = %v, want item limit", err)
			}
		})
	}
}

func TestSwagger20CodecReportsUnsupportedAndMalformedValues(t *testing.T) {
	t.Parallel()

	query := parameter.Swagger20Options{Location: parameter.Query}
	if _, err := parameter.EncodeSwagger20("value", jsonvalue.Value{}, query); !errors.Is(err, parameter.ErrUnsupportedValue) {
		t.Fatalf("invalid value error = %v", err)
	}
	nested := arrayValue(t, arrayValue(t, stringValue(t, "nested")))
	if _, err := parameter.EncodeSwagger20("value", nested, query); !errors.Is(err, parameter.ErrUnsupportedValue) {
		t.Fatalf("nested value error = %v", err)
	}

	multi := parameter.Swagger20Options{
		Location:      parameter.Query,
		Format:        parameter.CollectionMulti,
		EmptyDecoding: parameter.EmptyAsCollection,
	}
	decoded, err := parameter.DecodeSwagger20("value", "", parameter.Array, multi)
	if err != nil {
		t.Fatal(err)
	}
	if elements, ok := decoded.Elements(); !ok || len(elements) != 0 {
		t.Fatalf("empty multi = %#v", decoded)
	}
	for _, raw := range []string{"value", "wrong=one", "value=%GG", "value=%FF"} {
		if _, err := parameter.DecodeSwagger20("value", raw, parameter.Array, multi); !errors.Is(err, parameter.ErrMalformedEncoding) {
			t.Fatalf("multi DecodeSwagger20(%q) error = %v", raw, err)
		}
	}
	for _, test := range []struct {
		location parameter.Location
		raw      string
	}{
		{location: parameter.Query, raw: "value"},
		{location: parameter.Header, raw: "%GG"},
		{location: parameter.Header, raw: "%FF"},
	} {
		if _, err := parameter.DecodeSwagger20("value", test.raw, parameter.Array, parameter.Swagger20Options{
			Location: test.location,
		}); !errors.Is(err, parameter.ErrMalformedEncoding) {
			t.Fatalf("DecodeSwagger20(%q) error = %v", test.raw, err)
		}
	}
}
