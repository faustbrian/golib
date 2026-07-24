package parameter_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parameter"
)

func TestEncodeRejectsUndefinedStyleCombinations(t *testing.T) {
	t.Parallel()

	oas31 := mustVersion(t, "3.1.2")
	oas32 := mustVersion(t, "3.2.0")
	array := arrayValue(t, stringValue(t, "value"))
	object := objectValue(t, jsonvalue.Member{Name: "key", Value: stringValue(t, "value")})
	tests := []parameter.Options{
		{Style: parameter.Form, Location: parameter.Query},
		{Version: mustVersion(t, "2.0"), Style: parameter.Form, Location: parameter.Query},
		{Version: oas32, Style: parameter.Matrix, Location: parameter.Query},
		{Version: oas32, Style: parameter.Matrix, Location: parameter.Path, AllowReserved: true},
		{Version: oas32, Style: parameter.SpaceDelimited, Location: parameter.Query, Explode: true},
		{Version: oas31, Style: parameter.DeepObject, Location: parameter.Query},
		{Version: oas31, Style: parameter.Cookie, Location: parameter.CookieLocation},
		{Version: oas32, Style: parameter.Style("unknown"), Location: parameter.Query},
	}
	for _, options := range tests {
		value := array
		if options.Style == parameter.DeepObject || options.Style == parameter.Cookie {
			value = object
		}
		if _, err := parameter.Encode("value", value, options); !errors.Is(err, parameter.ErrInvalidOptions) {
			t.Fatalf("options %#v error = %v", options, err)
		}
	}
	if _, err := parameter.Encode("", object, parameter.Options{
		Version: oas32, Location: parameter.Query, Style: parameter.DeepObject,
	}); !errors.Is(err, parameter.ErrInvalidOptions) {
		t.Fatalf("empty name error = %v", err)
	}
}

func TestEncodeRejectsNestedAndInvalidValues(t *testing.T) {
	t.Parallel()

	options := parameter.Options{
		Version:  mustVersion(t, "3.2.0"),
		Location: parameter.Query,
		Style:    parameter.Form,
	}
	if _, err := parameter.Encode("value", jsonvalue.Value{}, options); !errors.Is(err, parameter.ErrUnsupportedValue) {
		t.Fatalf("zero value error = %v", err)
	}
	nested := arrayValue(t, arrayValue(t, stringValue(t, "nested")))
	if _, err := parameter.Encode("value", nested, options); !errors.Is(err, parameter.ErrUnsupportedValue) {
		t.Fatalf("nested array error = %v", err)
	}
	options.Style = parameter.DeepObject
	options.Explode = true
	nestedObject := objectValue(t, jsonvalue.Member{
		Name: "nested", Value: objectValue(t),
	})
	if _, err := parameter.Encode("value", nestedObject, options); !errors.Is(err, parameter.ErrUnsupportedValue) {
		t.Fatalf("nested object error = %v", err)
	}
	for _, test := range []struct {
		style   parameter.Style
		value   jsonvalue.Value
		explode bool
	}{
		{style: parameter.Matrix, value: nested, explode: true},
		{style: parameter.Matrix, value: nestedObject, explode: true},
		{style: parameter.Label, value: nestedObject, explode: true},
		{style: parameter.Form, value: nested, explode: true},
		{style: parameter.Form, value: nestedObject, explode: true},
	} {
		location := parameter.Query
		if test.style == parameter.Matrix || test.style == parameter.Label {
			location = parameter.Path
		}
		if _, err := parameter.Encode("value", test.value, parameter.Options{
			Version:  options.Version,
			Location: location,
			Style:    test.style,
			Explode:  test.explode,
		}); !errors.Is(err, parameter.ErrUnsupportedValue) {
			t.Fatalf("nested %s error = %v", test.style, err)
		}
	}
}

func TestEncodePreservesScalarTypesAndEncodingPolicy(t *testing.T) {
	t.Parallel()

	version := mustVersion(t, "3.2.0")
	boolean := jsonvalue.Boolean(true)
	got, err := parameter.Encode("enabled", boolean, parameter.Options{
		Version: version, Location: parameter.Query, Style: parameter.Form,
	})
	if err != nil || got != "enabled=true" {
		t.Fatalf("boolean encoding = %q, %v", got, err)
	}
	got, err = parameter.Encode("na me", stringValue(t, "café~"), parameter.Options{
		Version: version, Location: parameter.Query, Style: parameter.Form,
	})
	if err != nil || got != "na%20me=caf%C3%A9%7E" {
		t.Fatalf("UTF-8 encoding = %q, %v", got, err)
	}
	got, err = parameter.Encode("raw", stringValue(t, "a/b?c"), parameter.Options{
		Version: version, Location: parameter.CookieLocation, Style: parameter.Cookie,
	})
	if err != nil || got != "raw=a/b?c" {
		t.Fatalf("cookie encoding = %q, %v", got, err)
	}
}
