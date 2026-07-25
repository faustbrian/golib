package jsonschema_test

import (
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonschema"
)

func TestNeedsExplicitDialectDistinguishesSchemaResourceUse(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw  string
		use  jsonschema.SchemaResourceUse
		want bool
	}{
		{raw: `{}`, use: jsonschema.CompleteOpenAPIDocumentSchema},
		{raw: `{}`, use: jsonschema.StandaloneSchema, want: true},
		{raw: `{}`, use: jsonschema.IncompleteOpenAPIDocumentSchema, want: true},
		{raw: `{"$schema":"https://spec.openapis.org/oas/3.1/dialect/base"}`,
			use: jsonschema.StandaloneSchema},
		{raw: `true`, use: jsonschema.StandaloneSchema, want: true},
		{raw: `false`, use: jsonschema.CompleteOpenAPIDocumentSchema},
	} {
		actual, err := jsonschema.NeedsExplicitDialect(
			mustValue(t, test.raw), test.use,
		)
		if err != nil || actual != test.want {
			t.Fatalf("NeedsExplicitDialect(%s, %v) = %t, %v, want %t",
				test.raw, test.use, actual, err, test.want)
		}
	}
}

func TestNeedsExplicitDialectRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		raw string
		use jsonschema.SchemaResourceUse
	}{
		{raw: `1`, use: jsonschema.StandaloneSchema},
		{raw: `{}`, use: 99},
		{raw: `{"$schema":null}`, use: jsonschema.StandaloneSchema},
		{raw: `{"$schema":""}`, use: jsonschema.StandaloneSchema},
		{raw: `{"$schema":"relative"}`, use: jsonschema.StandaloneSchema},
		{raw: `{"$schema":"https://example.test/dialect#fragment"}`,
			use: jsonschema.StandaloneSchema},
	} {
		_, err := jsonschema.NeedsExplicitDialect(mustValue(t, test.raw), test.use)
		if !errors.Is(err, jsonschema.ErrInvalidDialectAdvisory) {
			t.Fatalf("NeedsExplicitDialect(%s, %v) error = %v",
				test.raw, test.use, err)
		}
	}
}
