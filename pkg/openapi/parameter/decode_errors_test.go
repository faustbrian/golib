package parameter_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/parameter"
)

func TestDecodeRequiresExplicitEmptyValuePolicy(t *testing.T) {
	t.Parallel()

	options := parameter.Options{
		Version:  mustVersion(t, "3.2.0"),
		Location: parameter.Query,
		Style:    parameter.Form,
	}
	if _, err := parameter.Decode("color", "color=", parameter.Primitive, options); !errors.Is(err, parameter.ErrAmbiguousValue) {
		t.Fatalf("ambiguous empty error = %v", err)
	}
	options.EmptyDecoding = parameter.EmptyAsValue
	value, err := parameter.Decode("color", "color=", parameter.Primitive, options)
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := value.Text(); !ok || text != "" {
		t.Fatalf("empty scalar = %#v", value)
	}
	options.EmptyDecoding = parameter.EmptyAsNull
	value, err = parameter.Decode("color", "color=", parameter.Primitive, options)
	if err != nil || value.Kind() != jsonvalue.NullKind {
		t.Fatalf("undefined scalar = %#v, %v", value, err)
	}
	options.EmptyDecoding = parameter.EmptyAsCollection
	value, err = parameter.Decode("color", "color=", parameter.Array, options)
	if err != nil {
		t.Fatal(err)
	}
	if elements, ok := value.Elements(); !ok || len(elements) != 0 {
		t.Fatalf("empty array = %#v", value)
	}
	value, err = parameter.Decode("color", "color=", parameter.Object, options)
	if err != nil {
		t.Fatal(err)
	}
	if members, ok := value.Members(); !ok || len(members) != 0 {
		t.Fatalf("empty object = %#v", value)
	}
	options.EmptyDecoding = parameter.EmptyAsValue
	value, err = parameter.Decode("color", "color=", parameter.Array, options)
	if err != nil {
		t.Fatal(err)
	}
	if elements, _ := value.Elements(); len(elements) != 1 {
		t.Fatalf("single empty item = %#v", value)
	}
	matrix := parameter.Options{
		Version: options.Version, Location: parameter.Path, Style: parameter.Matrix,
	}
	value, err = parameter.Decode("color", ";color", parameter.Primitive, matrix)
	if err != nil || value.Kind() != jsonvalue.NullKind {
		t.Fatalf("matrix undefined value = %#v, %v", value, err)
	}
	options.Explode = true
	options.EmptyDecoding = parameter.RejectEmptyAmbiguity
	for _, shape := range []parameter.Shape{parameter.Array, parameter.Object} {
		value, err = parameter.Decode("color", "", shape, options)
		if err != nil {
			t.Fatal(err)
		}
		if shape == parameter.Array {
			elements, _ := value.Elements()
			if len(elements) != 0 {
				t.Fatalf("exploded empty array = %#v", value)
			}
		} else {
			members, _ := value.Members()
			if len(members) != 0 {
				t.Fatalf("exploded empty object = %#v", value)
			}
		}
	}
}

func TestDecodeRejectsMalformedOrAmbiguousSerializations(t *testing.T) {
	t.Parallel()

	version := mustVersion(t, "3.2.0")
	tests := []struct {
		name    string
		raw     string
		shape   parameter.Shape
		options parameter.Options
	}{
		{name: "matrix prefix", raw: "color=blue", shape: parameter.Primitive, options: parameter.Options{Version: version, Location: parameter.Path, Style: parameter.Matrix}},
		{name: "matrix name", raw: ";other=blue", shape: parameter.Primitive, options: parameter.Options{Version: version, Location: parameter.Path, Style: parameter.Matrix}},
		{name: "matrix undefined name", raw: ";other", shape: parameter.Primitive, options: parameter.Options{Version: version, Location: parameter.Path, Style: parameter.Matrix}},
		{name: "matrix undefined escape", raw: ";%zz", shape: parameter.Primitive, options: parameter.Options{Version: version, Location: parameter.Path, Style: parameter.Matrix}},
		{name: "label prefix", raw: "blue", shape: parameter.Primitive, options: parameter.Options{Version: version, Location: parameter.Path, Style: parameter.Label}},
		{name: "form assignment", raw: "color", shape: parameter.Primitive, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.Form}},
		{name: "delimited name", raw: "other=blue%20black", shape: parameter.Array, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.SpaceDelimited}},
		{name: "odd object", raw: "color=R,100,G", shape: parameter.Object, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.Form}},
		{name: "empty object name", raw: "color=,100", shape: parameter.Object, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.Form}},
		{name: "escaped object name", raw: "color=%zz,100", shape: parameter.Object, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.Form}},
		{name: "escaped object value", raw: "color=R,%FF", shape: parameter.Object, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.Form}},
		{name: "escaped array value", raw: "color=%FF,blue", shape: parameter.Array, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.Form}},
		{name: "missing pair", raw: "R", shape: parameter.Object, options: parameter.Options{Version: version, Location: parameter.Header, Style: parameter.Simple, Explode: true}},
		{name: "empty pair name", raw: "=100", shape: parameter.Object, options: parameter.Options{Version: version, Location: parameter.Header, Style: parameter.Simple, Explode: true}},
		{name: "invalid pair value UTF-8", raw: "R=" + string([]byte{0xff}), shape: parameter.Object, options: parameter.Options{Version: version, Location: parameter.Header, Style: parameter.Simple, Explode: true}},
		{name: "duplicate pair", raw: "R=100,R=200", shape: parameter.Object, options: parameter.Options{Version: version, Location: parameter.Header, Style: parameter.Simple, Explode: true}},
		{name: "repeated name", raw: "other=blue", shape: parameter.Array, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.Form, Explode: true}},
		{name: "repeated value", raw: "color=%FF", shape: parameter.Array, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.Form, Explode: true}},
		{name: "bad percent", raw: "color=%zz", shape: parameter.Primitive, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.Form}},
		{name: "nested deep object", raw: "color%5BR%5D%5Bx%5D=100", shape: parameter.Object, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.DeepObject}},
		{name: "deep missing assignment", raw: "color%5BR%5D", shape: parameter.Object, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.DeepObject}},
		{name: "deep wrong name", raw: "other%5BR%5D=100", shape: parameter.Object, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.DeepObject}},
		{name: "deep empty property", raw: "color%5B%5D=100", shape: parameter.Object, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.DeepObject}},
		{name: "deep value", raw: "color%5BR%5D=%FF", shape: parameter.Object, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.DeepObject}},
		{name: "invalid UTF-8", raw: "color=%FF", shape: parameter.Primitive, options: parameter.Options{Version: version, Location: parameter.Query, Style: parameter.Form}},
		{name: "primitive delimiters", raw: "blue,black", shape: parameter.Primitive, options: parameter.Options{Version: version, Location: parameter.Header, Style: parameter.Simple}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := parameter.Decode("color", test.raw, test.shape, test.options); err == nil {
				t.Fatal("malformed serialization was accepted")
			}
		})
	}
}

func TestDecodeRejectsInvalidShapeNameAndOptions(t *testing.T) {
	t.Parallel()

	options := parameter.Options{
		Version:  mustVersion(t, "3.2.0"),
		Location: parameter.Query,
		Style:    parameter.Form,
	}
	for _, test := range []struct {
		name  string
		shape parameter.Shape
		style parameter.Style
	}{
		{name: "", shape: parameter.Primitive, style: parameter.Form},
		{name: "color", shape: parameter.Shape(255), style: parameter.Form},
		{name: "color", shape: parameter.Primitive, style: parameter.Style("unknown")},
	} {
		options.Style = test.style
		if _, err := parameter.Decode(test.name, "color=blue", test.shape, options); !errors.Is(err, parameter.ErrInvalidOptions) {
			t.Fatalf("invalid decode input error = %v", err)
		}
	}
}
