package parameter_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parameter"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestOptionsForAppliesParameterSerializationDefaults(t *testing.T) {
	t.Parallel()

	version := mustVersion(t, "3.1.2")
	for _, test := range []struct {
		location string
		style    parameter.Style
		explode  bool
	}{
		{location: "query", style: parameter.Form, explode: true},
		{location: "cookie", style: parameter.Form, explode: true},
		{location: "path", style: parameter.Simple, explode: false},
		{location: "header", style: parameter.Simple, explode: false},
	} {
		value := objectValue(t, jsonvalue.Member{
			Name: "in", Value: stringValue(t, test.location),
		})
		options, err := parameter.OptionsFor(version, value)
		if err != nil {
			t.Fatal(err)
		}
		if options.Location != parameter.Location(test.location) ||
			options.Style != test.style || options.Explode != test.explode {
			t.Fatalf("%s options = %#v", test.location, options)
		}
	}
}

func TestOptionsForPreservesExplicitParameterSerialization(t *testing.T) {
	t.Parallel()

	value := objectValue(t,
		jsonvalue.Member{Name: "in", Value: stringValue(t, "query")},
		jsonvalue.Member{Name: "style", Value: stringValue(t, "pipeDelimited")},
		jsonvalue.Member{Name: "explode", Value: jsonvalue.Boolean(false)},
		jsonvalue.Member{Name: "allowReserved", Value: jsonvalue.Boolean(true)},
	)
	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		options, err := parameter.OptionsFor(mustVersion(t, version), value)
		if err != nil {
			t.Fatal(err)
		}
		if options.Style != parameter.PipeDelimited || options.Explode ||
			!options.AllowReserved {
			t.Fatalf("version %s explicit options = %#v", version, options)
		}
		encoded, err := parameter.Encode(
			"tags",
			arrayValue(t, stringValue(t, "a"), stringValue(t, "b")),
			options,
		)
		if err != nil || encoded != "tags=a%7Cb" {
			t.Fatalf("version %s Encode() = %q, %v", version, encoded, err)
		}
	}
}

func TestOptionsForAppliesAllowEmptyValue(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		value := objectValue(t,
			jsonvalue.Member{Name: "in", Value: stringValue(t, "query")},
			jsonvalue.Member{Name: "allowEmptyValue", Value: jsonvalue.Boolean(true)},
		)
		options, err := parameter.OptionsFor(mustVersion(t, version), value)
		if err != nil {
			t.Fatal(err)
		}
		expected := parameter.EmptyAsValue
		switch version {
		case "3.0.4", "3.1.1", "3.1.2", "3.2.0":
			expected = parameter.EmptyAsNull
		}
		if options.EmptyDecoding != expected {
			t.Fatalf("version %s empty decoding = %v", version,
				options.EmptyDecoding)
		}
		decoded, err := parameter.Decode(
			"value", "value=", parameter.Primitive, options,
		)
		if err != nil {
			t.Fatal(err)
		}
		if expected == parameter.EmptyAsNull {
			if decoded.Kind() != jsonvalue.NullKind {
				t.Fatalf("version %s unused value = %#v", version, decoded)
			}
		} else if text, ok := decoded.Text(); !ok || text != "" {
			t.Fatalf("version %s empty value = %#v", version, decoded)
		}
	}
}

func TestOptionsForIgnoresAllowEmptyValueForUndefinedSerialization(t *testing.T) {
	t.Parallel()

	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		value := objectValue(t,
			jsonvalue.Member{Name: "in", Value: stringValue(t, "query")},
			jsonvalue.Member{Name: "style", Value: stringValue(t, "deepObject")},
			jsonvalue.Member{Name: "allowEmptyValue", Value: jsonvalue.Boolean(true)},
			jsonvalue.Member{Name: "schema", Value: objectValue(t,
				jsonvalue.Member{Name: "type", Value: stringValue(t, "string")},
			)},
		)
		options, err := parameter.OptionsFor(mustVersion(t, version), value)
		if err != nil {
			t.Fatal(err)
		}
		if options.EmptyDecoding != parameter.RejectEmptyAmbiguity {
			t.Errorf("version %s empty decoding = %v",
				version, options.EmptyDecoding)
		}
	}
}

func TestOptionsForResolvedSchemaAppliesAllowEmptyValueByResolvedShape(
	t *testing.T,
) {
	t.Parallel()

	parameterValue := objectValue(t,
		jsonvalue.Member{Name: "in", Value: stringValue(t, "query")},
		jsonvalue.Member{Name: "style", Value: stringValue(t, "deepObject")},
		jsonvalue.Member{Name: "explode", Value: jsonvalue.Boolean(true)},
		jsonvalue.Member{Name: "allowEmptyValue", Value: jsonvalue.Boolean(true)},
		jsonvalue.Member{Name: "schema", Value: objectValue(t,
			jsonvalue.Member{Name: "$ref", Value: stringValue(t, "schemas.json#/Value")},
		)},
	)
	for _, version := range []string{
		"3.0.0", "3.0.1", "3.0.2", "3.0.3", "3.0.4",
		"3.1.0", "3.1.1", "3.1.2", "3.2.0",
	} {
		version := version
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			stringOptions, err := parameter.OptionsForResolvedSchema(
				mustVersion(t, version),
				parameterValue,
				objectValue(t,
					jsonvalue.Member{Name: "type", Value: stringValue(t, "string")},
				),
			)
			if err != nil {
				t.Fatal(err)
			}
			if stringOptions.EmptyDecoding != parameter.RejectEmptyAmbiguity {
				t.Fatalf("string empty decoding = %v",
					stringOptions.EmptyDecoding)
			}

			objectOptions, err := parameter.OptionsForResolvedSchema(
				mustVersion(t, version),
				parameterValue,
				objectValue(t,
					jsonvalue.Member{Name: "type", Value: stringValue(t, "object")},
				),
			)
			if err != nil {
				t.Fatal(err)
			}
			want := parameter.EmptyAsValue
			if version == "3.0.4" || version == "3.1.1" ||
				version == "3.1.2" || version == "3.2.0" {
				want = parameter.EmptyAsNull
			}
			if objectOptions.EmptyDecoding != want {
				t.Fatalf("object empty decoding = %v, want %v",
					objectOptions.EmptyDecoding, want)
			}
		})
	}
}

func TestOptionsForRejectsInvalidParameterObjects(t *testing.T) {
	t.Parallel()

	version := mustVersion(t, "3.2.0")
	for _, value := range []jsonvalue.Value{
		jsonvalue.Null(),
		objectValue(t,
			jsonvalue.Member{Name: "in", Value: jsonvalue.Boolean(true)},
		),
		objectValue(t),
		objectValue(t,
			jsonvalue.Member{Name: "in", Value: stringValue(t, "querystring")},
		),
		objectValue(t,
			jsonvalue.Member{Name: "in", Value: stringValue(t, "header")},
			jsonvalue.Member{Name: "allowReserved", Value: jsonvalue.Boolean(true)},
		),
		objectValue(t,
			jsonvalue.Member{Name: "in", Value: stringValue(t, "header")},
			jsonvalue.Member{Name: "allowEmptyValue", Value: jsonvalue.Boolean(true)},
		),
		objectValue(t,
			jsonvalue.Member{Name: "in", Value: stringValue(t, "query")},
			jsonvalue.Member{Name: "style", Value: jsonvalue.Boolean(true)},
		),
		objectValue(t,
			jsonvalue.Member{Name: "in", Value: stringValue(t, "query")},
			jsonvalue.Member{Name: "style", Value: stringValue(t, "matrix")},
		),
		objectValue(t,
			jsonvalue.Member{Name: "in", Value: stringValue(t, "query")},
			jsonvalue.Member{Name: "explode", Value: stringValue(t, "true")},
		),
		objectValue(t,
			jsonvalue.Member{Name: "in", Value: stringValue(t, "query")},
			jsonvalue.Member{Name: "allowReserved", Value: stringValue(t, "true")},
		),
		objectValue(t,
			jsonvalue.Member{Name: "in", Value: stringValue(t, "query")},
			jsonvalue.Member{Name: "allowEmptyValue", Value: stringValue(t, "true")},
		),
	} {
		if _, err := parameter.OptionsFor(version, value); !errors.Is(
			err, parameter.ErrInvalidOptions,
		) {
			t.Fatalf("OptionsFor(%#v) error = %v", value, err)
		}
	}
	valid := objectValue(t,
		jsonvalue.Member{Name: "in", Value: stringValue(t, "query")},
	)
	if _, err := parameter.OptionsFor(specversion.Version{}, valid); !errors.Is(
		err, parameter.ErrInvalidOptions,
	) {
		t.Fatalf("zero version error = %v", err)
	}
	if _, err := parameter.OptionsForResolvedSchema(
		version, valid, jsonvalue.Null(),
	); !errors.Is(err, parameter.ErrInvalidOptions) {
		t.Fatalf("invalid resolved schema error = %v", err)
	}
	if _, err := parameter.OptionsForResolvedSchema(
		version, jsonvalue.Null(), objectValue(t),
	); !errors.Is(err, parameter.ErrInvalidOptions) {
		t.Fatalf("invalid parameter with resolved schema error = %v", err)
	}
	if _, err := parameter.OptionsForResolvedSchema(
		version, valid, jsonvalue.Boolean(true),
	); err != nil {
		t.Fatalf("boolean resolved schema error = %v", err)
	}
}

func TestOptionsForAppliesEmptyValueOnlyToDefinedShapes(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		style  string
		schema jsonvalue.Value
		want   parameter.EmptyDecoding
	}{
		{name: "non-object schema", schema: jsonvalue.Null(),
			want: parameter.EmptyAsNull},
		{name: "untyped schema", schema: objectValue(t),
			want: parameter.EmptyAsNull},
		{name: "non-string type", schema: objectValue(t,
			jsonvalue.Member{Name: "type", Value: arrayValue(t, stringValue(t, "string"))},
		), want: parameter.EmptyAsNull},
		{name: "unknown type", schema: objectValue(t,
			jsonvalue.Member{Name: "type", Value: stringValue(t, "custom")},
		), want: parameter.EmptyAsNull},
		{name: "array deep object", style: "deepObject", schema: objectValue(t,
			jsonvalue.Member{Name: "type", Value: stringValue(t, "array")},
		), want: parameter.RejectEmptyAmbiguity},
		{name: "object deep object", style: "deepObject", schema: objectValue(t,
			jsonvalue.Member{Name: "type", Value: stringValue(t, "object")},
		), want: parameter.EmptyAsNull},
		{name: "boolean form", schema: objectValue(t,
			jsonvalue.Member{Name: "type", Value: stringValue(t, "boolean")},
		), want: parameter.EmptyAsNull},
	} {
		t.Run(test.name, func(t *testing.T) {
			members := []jsonvalue.Member{
				{Name: "in", Value: stringValue(t, "query")},
				{Name: "allowEmptyValue", Value: jsonvalue.Boolean(true)},
				{Name: "schema", Value: test.schema},
			}
			if test.style != "" {
				members = append(members, jsonvalue.Member{
					Name: "style", Value: stringValue(t, test.style),
				})
			}
			options, err := parameter.OptionsFor(
				mustVersion(t, "3.2.0"), objectValue(t, members...),
			)
			if err != nil {
				t.Fatal(err)
			}
			if options.EmptyDecoding != test.want {
				t.Fatalf("empty decoding = %v, want %v",
					options.EmptyDecoding, test.want)
			}
		})
	}
}

func TestOptionsForAcceptsOpenAPI32CookieStyleOnly(t *testing.T) {
	t.Parallel()

	value := objectValue(t,
		jsonvalue.Member{Name: "in", Value: stringValue(t, "cookie")},
		jsonvalue.Member{Name: "style", Value: stringValue(t, "cookie")},
	)
	if _, err := parameter.OptionsFor(mustVersion(t, "3.2.0"), value); err != nil {
		t.Fatal(err)
	}
	if _, err := parameter.OptionsFor(mustVersion(t, "3.1.2"), value); !errors.Is(
		err, parameter.ErrInvalidOptions,
	) {
		t.Fatalf("OpenAPI 3.1 cookie style error = %v", err)
	}
}
