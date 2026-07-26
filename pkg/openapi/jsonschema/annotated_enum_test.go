package jsonschema_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

func TestRecognizeAnnotatedEnumPreservesValuesAndAnnotations(t *testing.T) {
	t.Parallel()

	cases, recognized, err := jsonschema.RecognizeAnnotatedEnum(
		mustValue(t, `{"oneOf":[
			{"const":"pending","title":"Pending","description":"Not started"},
			{"const":2,"title":"Complete"}
		]}`),
		jsonschema.AnnotatedEnumOptions{},
	)
	if err != nil || !recognized || len(cases) != 2 {
		t.Fatalf("annotated enum = %#v, %t, %v", cases, recognized, err)
	}
	if text, valid := cases[0].Value.Text(); !valid || text != "pending" {
		t.Fatalf("first value = %#v", cases[0].Value)
	}
	if title, present := cases[0].Title.Value(); !present || title != "Pending" {
		t.Fatalf("first title = %q, %t", title, present)
	}
	if description, present := cases[0].Description.Value(); !present || description != "Not started" {
		t.Fatalf("first description = %q, %t", description, present)
	}
	if _, present := cases[1].Description.Value(); present ||
		cases[1].Description.Present() {
		t.Fatal("absent description was synthesized")
	}
}

func TestRecognizeAnnotatedEnumAcceptsAnyOfAtExactLimit(t *testing.T) {
	t.Parallel()

	cases, recognized, err := jsonschema.RecognizeAnnotatedEnum(
		mustValue(t, `{"anyOf":[{"const":null},{"const":true}]}`),
		jsonschema.AnnotatedEnumOptions{MaxCases: 2},
	)
	if err != nil || !recognized || len(cases) != 2 {
		t.Fatalf("annotated enum = %#v, %t, %v", cases, recognized, err)
	}
	if cases[0].Value.Kind() != jsonvalue.NullKind {
		t.Fatalf("first value kind = %v", cases[0].Value.Kind())
	}
}

func TestRecognizeAnnotatedEnumDeclinesNonEnumApplicators(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		`{}`,
		`{"oneOf":[1]}`,
		`{"oneOf":[{"title":"Missing const"}]}`,
		`{"oneOf":[{"type":"string","const":"one"}]}`,
		`{"anyOf":[{"const":"one"}],"oneOf":[{"const":"two"}]}`,
	} {
		cases, recognized, err := jsonschema.RecognizeAnnotatedEnum(
			mustValue(t, raw), jsonschema.AnnotatedEnumOptions{},
		)
		if err != nil || recognized || cases != nil {
			t.Fatalf("RecognizeAnnotatedEnum(%s) = %#v, %t, %v",
				raw, cases, recognized, err)
		}
	}
}

func TestRecognizeAnnotatedEnumRejectsInvalidInputsAndBounds(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		raw     string
		options jsonschema.AnnotatedEnumOptions
		want    error
	}{
		{name: "non-object", raw: `[]`, want: jsonschema.ErrInvalidAnnotatedEnum},
		{name: "non-array", raw: `{"oneOf":{}}`,
			want: jsonschema.ErrInvalidAnnotatedEnum},
		{name: "empty", raw: `{"oneOf":[]}`,
			want: jsonschema.ErrInvalidAnnotatedEnum},
		{name: "invalid title", raw: `{"oneOf":[{"const":1,"title":1}]}`,
			want: jsonschema.ErrInvalidAnnotatedEnum},
		{name: "invalid description",
			raw:  `{"oneOf":[{"const":1,"description":null}]}`,
			want: jsonschema.ErrInvalidAnnotatedEnum},
		{name: "negative limit", raw: `{"oneOf":[{"const":1}]}`,
			options: jsonschema.AnnotatedEnumOptions{MaxCases: -1},
			want:    jsonschema.ErrInvalidAnnotatedEnumOptions},
		{name: "limit", raw: `{"oneOf":[{"const":1},{"const":2}]}`,
			options: jsonschema.AnnotatedEnumOptions{MaxCases: 1},
			want:    jsonschema.ErrAnnotatedEnumLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := jsonschema.RecognizeAnnotatedEnum(
				mustValue(t, test.raw), test.options,
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}
