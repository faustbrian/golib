package validate

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestLinksetMediaTypeSchemaRejectsIncompleteShapes(t *testing.T) {
	t.Parallel()

	validRelation := `{"type":"array","items":{"type":"object",` +
		`"required":["href"],"properties":{"href":{"type":"string"}}}}`
	for _, mediaType := range []string{
		`{}`,
		`{"schema":{"type":"object","required":["linkset"]}}`,
		`{"schema":{"type":"object","required":["linkset"],` +
			`"properties":{"linkset":{"type":"string"}}}}`,
		`{"schema":{"type":"object","required":["linkset"],` +
			`"properties":{"linkset":{"type":"array"}}}}`,
		`{"schema":{"type":"object","required":["linkset"],` +
			`"properties":{"linkset":{"type":"array","items":{"type":"string"}}}}}`,
		`{"schema":{"type":"object","required":["linkset"],` +
			`"properties":{"linkset":{"type":"array","items":{` +
			`"type":"object","allOf":[1]}}}}}`,
		`{"schema":{"type":"object","required":["linkset"],` +
			`"properties":{"linkset":{"type":"array","items":{` +
			`"type":"object","properties":{"anchor":{"type":"integer"}},` +
			`"additionalProperties":` + validRelation + `}}}}}`,
		`{"schema":{"type":"object","required":["linkset"],` +
			`"properties":{"linkset":{"type":"array","items":{` +
			`"type":"object","properties":{"next":{"type":"string"}}}}}}}`,
		`{"schema":{"type":"object","required":["linkset"],` +
			`"properties":{"linkset":{"type":"array","items":{` +
			`"type":"object","additionalProperties":{"type":"string"}}}}}}`,
		`{"schema":{"type":"object","required":["linkset"],` +
			`"properties":{"linkset":{"type":"array","items":{` +
			`"type":"object","additionalProperties":{"type":"array"}}}}}}`,
		`{"schema":{"type":"object","required":["linkset"],` +
			`"properties":{"linkset":{"type":"array","items":{` +
			`"type":"object","additionalProperties":{"type":"array",` +
			`"items":{"type":"string"}}}}}}}`,
		`{"schema":{"type":"object","required":["linkset"],` +
			`"properties":{"linkset":{"type":"array","items":{` +
			`"type":"object","additionalProperties":{"type":"array",` +
			`"items":{"type":"object","properties":{"href":{"type":"string"}}}}}}}}}`,
		`{"schema":{"type":"object","required":["linkset"],` +
			`"properties":{"linkset":{"type":"array","items":{` +
			`"type":"object","additionalProperties":{"type":"array",` +
			`"items":{"type":"object","required":["href"]}}}}}}}`,
		`{"schema":{"type":"object","required":["linkset"],` +
			`"properties":{"linkset":{"type":"array","items":{` +
			`"type":"object","additionalProperties":{"type":"array",` +
			`"items":{"type":"object","required":["href"],` +
			`"properties":{"href":{"type":"integer"}}}}}}}}}`,
	} {
		value := testValidationValue(t, mediaType)
		if validLinksetMediaTypeSchema(
			context.Background(),
			reference.Resource{Root: value},
			value,
			DefaultOptions(),
		) {
			t.Fatalf("invalid linkset schema accepted: %s", mediaType)
		}
	}
	validExplicit := testValidationValue(t, `{"schema":{
		"type":"object","required":["linkset"],"properties":{"linkset":{
			"type":"array","items":{"type":"object","properties":{
				"anchor":{"type":"string"},"next":`+validRelation+`
			}}
		}}
	}}`)
	if !validLinksetMediaTypeSchema(
		context.Background(),
		reference.Resource{Root: validExplicit},
		validExplicit,
		DefaultOptions(),
	) {
		t.Fatal("valid explicit-relation linkset schema was rejected")
	}
	root := testValidationValue(t, `{}`)
	for _, schema := range []string{
		`{"$ref":"#/missing"}`,
		`{"type":"object"}`,
		`{"type":"object","required":["other"]}`,
	} {
		if resolvedSchemaRequiresProperty(
			context.Background(),
			reference.Resource{Root: root},
			testValidationValue(t, schema),
			"linkset",
			DefaultOptions(),
		) {
			t.Fatalf("schema unexpectedly required linkset: %s", schema)
		}
	}
}
