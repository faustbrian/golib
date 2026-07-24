package validate

import (
	"context"
	"testing"

	openapi "github.com/faustbrian/golib/pkg/openapi"
	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestLinkValidatorDefensiveTraversalStates(t *testing.T) {
	t.Parallel()

	root := testValidationValue(t, `{
		"components":{
			"parameters":{"Known":{"name":"id","in":"query"}},
			"links":{"Known":{
				"operationId":"target","requestBody":"$request.body",
				"parameters":{"id":"$request.query.id"}
			}}
		}
	}`)
	validator := linkValidator{
		version: "3.1.2", dialect: openapi.DialectOAS31,
		operationIDs:      map[string]int{"target": 1},
		operationPointers: map[string][]string{"target": {"/paths/~1target/get"}},
		operations:        map[string]struct{}{"/paths/~1target/get": {}},
		parameters: map[string]map[string]struct{}{
			"/paths/~1target/get": {requestParameterKey("query", "id"): {}},
		},
		ctx:      context.Background(),
		resource: reference.Resource{Root: root},
		limits:   reference.DefaultLimits(),
	}
	for _, value := range []jsonvalue.Value{
		jsonvalue.Boolean(true),
		testValidationValue(t, `{"$ref":"#/components/callbacks/Missing"}`),
	} {
		validator.collectCallbackOperationIDs(value, "/callback", func(operationLocation) {})
		validator.visitCallback(value, "/callback")
	}

	parameters := validator.declaredParameters(
		testValidationValue(t, `{"parameters":[
			{"$ref":"#/components/parameters/Missing"},
			{"in":"query"}
		]}`),
		testValidationValue(t, `{}`),
	)
	if len(parameters) != 0 {
		t.Fatalf("malformed declared parameters = %#v", parameters)
	}

	validator.validateResponseLinkExpressionsAtUse(
		testValidationValue(t, `{"links":{"value":false}}`),
		validator.resource, "/response", map[string]struct{}{},
	)
	validator.visitResponseWithParameters(
		testValidationValue(t, `{
			"links":{"known":{"$ref":"#/components/links/Known"}}
		}`),
		"/response", map[string]struct{}{},
	)
	beforeNilSource := len(validator.diagnostics)
	validator.visitResponseWithParameters(
		testValidationValue(t, `{
			"links":{"known":{"$ref":"#/components/links/Known"}}
		}`),
		"/response", nil,
	)
	if len(validator.diagnostics) != beforeNilSource {
		t.Fatalf("unknown source parameters produced diagnostics: %#v", validator.diagnostics)
	}
	validator.validateLinkExpressionsAtUse(
		testValidationValue(t, `{"requestBody":"$request.body"}`),
		"/link", map[string]struct{}{},
	)
	validator.validateLinkParameterName("id", "/parameter", nil, false)
	before := len(validator.diagnostics)
	validator.validateLinkParameterName(
		"id", "/parameter",
		map[string]struct{}{requestParameterKey("query", "id"): {}}, true,
	)
	if len(validator.diagnostics) != before {
		t.Fatalf("single target parameter was ambiguous: %#v", validator.diagnostics)
	}
	validator.validateLinkParameterName(
		"id", "/parameter",
		map[string]struct{}{
			requestParameterKey("query", "id"):  {},
			requestParameterKey("header", "ID"): {},
		}, true,
	)
	if len(validator.diagnostics) != before+1 ||
		validator.diagnostics[len(validator.diagnostics)-1].Code !=
			"openapi.link.parameter.ambiguous" {
		t.Fatalf("ambiguous target parameters = %#v", validator.diagnostics)
	}
	for _, test := range []struct {
		operationRef string
		pointer      string
		valid        bool
	}{
		{operationRef: "#/paths/~1pets/get", pointer: "/paths/~1pets/get", valid: true},
		{operationRef: "#anchor"},
		{operationRef: "#/%"},
	} {
		pointer, valid := internalOperationPointer(test.operationRef)
		if pointer != test.pointer || valid != test.valid {
			t.Errorf("internalOperationPointer(%q) = %q, %t", test.operationRef, pointer, valid)
		}
	}
	for _, test := range []struct {
		location  string
		candidate string
		name      string
		matches   bool
	}{
		{location: "query", candidate: "id", name: "id", matches: true},
		{location: "query", candidate: "ID", name: "id"},
		{location: "header", candidate: "ID", name: "id", matches: true},
	} {
		if got := linkParameterNameMatches(
			test.location, test.candidate, test.name,
		); got != test.matches {
			t.Errorf("link parameter match for %#v = %t", test, got)
		}
	}
	if _, known := validator.linkTargetParameters(
		"#anchor", true, "", false,
	); known {
		t.Fatal("anchor operation reference had known parameters")
	}
}

func TestLinkObjectResolutionRejectsMalformedTargets(t *testing.T) {
	t.Parallel()

	root := testValidationValue(t, `{"scalar":1}`)
	validator := linkValidator{
		ctx: context.Background(), resource: reference.Resource{Root: root},
		limits: reference.DefaultLimits(),
	}
	for _, value := range []jsonvalue.Value{
		jsonvalue.Boolean(true),
		testValidationValue(t, `{"$ref":true}`),
		testValidationValue(t, `{"$ref":"#/scalar"}`),
	} {
		if _, _, ok := validator.resolveObjectFrom(validator.resource, value); ok {
			t.Fatalf("malformed link target resolved: %#v", value)
		}
	}
}
