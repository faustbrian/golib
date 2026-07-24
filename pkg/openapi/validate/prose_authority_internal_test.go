package validate

import (
	"context"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	openapischema "github.com/faustbrian/golib/pkg/openapi/jsonschema"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

func TestSchemaProseRemainsAuthoritativeWhenInformationalSchemaPasses(
	t *testing.T,
) {
	t.Parallel()

	for _, versionText := range []string{"3.0.4", "3.1.1", "3.1.2", "3.2.0"} {
		versionText := versionText
		t.Run(versionText, func(t *testing.T) {
			t.Parallel()
			version, err := openapi.ParseVersion(versionText)
			if err != nil {
				t.Fatal(err)
			}
			document := validationDocument{
				version: version,
				raw: testValidationValue(t, `{
					"openapi":"`+versionText+`",
					"components":{"schemas":{"Container":{
						"type":"object","properties":{"value":{
							"type":"string","xml":{"namespace":"relative"}
						}}
					}}}
				}`),
			}
			options := DefaultOptions()
			options.schemaValidator = func(
				*openapischema.Compiler,
				context.Context,
				jsonvalue.Value,
			) (openapischema.OutputUnit, error) {
				return openapischema.OutputUnit{Valid: true}, nil
			}

			diagnostics, err := validateSchemas(
				context.Background(), document, options,
			)
			if err != nil {
				t.Fatal(err)
			}
			for _, diagnostic := range diagnostics {
				if diagnostic.Code == "openapi.xml.namespace.invalid" &&
					diagnostic.InstanceLocation ==
						"/components/schemas/Container/properties/value/xml/namespace" {
					return
				}
			}
			t.Fatalf("missing prose diagnostic: %#v", diagnostics)
		})
	}
}
