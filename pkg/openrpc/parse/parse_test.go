package parse_test

import (
	"bytes"
	"errors"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	"github.com/faustbrian/golib/pkg/openrpc/parse"
)

func TestDecodeMinimalDocument(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Example","version":"1"},
		"methods":[]
	}`)
	result, err := parse.Decode(input, parse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	document := result.Document()
	if document.Version().String() != "1.4.1" || len(document.Methods()) != 0 {
		t.Fatalf("unexpected document: %#v", document)
	}
	if !bytes.Equal(result.PreservingJSON(), input) {
		t.Fatalf("PreservingJSON() = %q", result.PreservingJSON())
	}
}

func TestDecodeUnknownFieldsUsesExplicitMode(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Example","version":"1","future":true,"x-owner":null},
		"methods":[]
	}`)
	_, err := parse.Decode(input, parse.DefaultOptions())
	if !errors.Is(err, parse.ErrUnknownField) {
		t.Fatalf("strict error = %v, want ErrUnknownField", err)
	}

	options := parse.DefaultOptions()
	options.UnknownFields = parse.PreserveUnknownFields
	result, err := parse.Decode(input, options)
	if err != nil {
		t.Fatal(err)
	}
	info := result.Document().Info()
	if info.Extensions().Len() != 1 || info.UnknownFields().Len() != 1 {
		t.Fatalf("extensions=%d unknown=%d", info.Extensions().Len(), info.UnknownFields().Len())
	}
}

func TestDecodeAcceptsSchemaPermittedAdditionalPropertiesInStrictMode(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Additional properties","version":"1"},
		"servers":[{
			"url":"https://${host}",
			"variables":{"host":{"default":"example.com","vendorVariable":true}}
		}],
		"methods":[{
			"name":"example",
			"params":[],
			"examples":[{
				"name":"pairing",
				"params":[{"name":"input","value":1,"vendorExample":true}],
				"vendorPairing":true
			}]
		}],
		"components":{"vendorComponentKind":{"anything":true}}
	}`)
	result, err := parse.Decode(input, parse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := openrpc.MarshalCanonical(result.Document())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{
		`"vendorVariable":true`,
		`"vendorExample":true`,
		`"vendorPairing":true`,
		`"vendorComponentKind":{"anything":true}`,
	} {
		if !bytes.Contains(encoded, []byte(field)) {
			t.Fatalf("canonical output omitted %s: %s", field, encoded)
		}
	}
}

func TestDecodePropagatesStrictJSONAndVersionErrors(t *testing.T) {
	t.Parallel()

	options := parse.DefaultOptions()
	_, err := parse.Decode([]byte(`{"openrpc":"1.4.1","openrpc":"1.4.1"}`), options)
	if !errors.Is(err, jsonvalue.ErrDuplicateName) {
		t.Fatalf("duplicate error = %v", err)
	}

	_, err = parse.Decode([]byte(`{
		"openrpc":"1.5.0",
		"info":{"title":"Example","version":"1"},
		"methods":[]
	}`), options)
	if !errors.Is(err, openrpc.ErrUnsupportedVersion) {
		t.Fatalf("version error = %v", err)
	}
	var parseError *parse.Error
	if !errors.As(err, &parseError) || parseError.Error() == "" ||
		!errors.Is(parseError, openrpc.ErrUnsupportedVersion) {
		t.Fatalf("typed parse error = %#v", err)
	}
}

func TestDecodeEnforcesMethodLimit(t *testing.T) {
	t.Parallel()

	options := parse.DefaultOptions()
	options.MaxMethods = 1
	_, err := parse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Example","version":"1"},
		"methods":[{},{}]
	}`), options)
	if !errors.Is(err, parse.ErrMethodLimit) {
		t.Fatalf("error = %v, want ErrMethodLimit", err)
	}
}

func TestDecodeEnforcesServerVariableLimit(t *testing.T) {
	t.Parallel()

	options := parse.DefaultOptions()
	options.MaxServerVariables = 1
	_, err := parse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Example","version":"1"},
		"servers":[{"url":"https://${a}","variables":{
			"a":{"default":"a"},
			"b":{"default":"b"}
		}}],
		"methods":[]
	}`), options)
	if !errors.Is(err, parse.ErrServerVariableLimit) {
		t.Fatalf("error = %v, want ErrServerVariableLimit", err)
	}
}

func TestDecodePreservesOptionalRootObjects(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"openrpc":"1.4.1",
		"$schema":"https://example.com/openrpc-schema",
		"info":{"title":"Example","version":"1"},
		"externalDocs":{"url":"https://example.com/docs"},
		"servers":[{"url":"https://example.com"}],
		"methods":[],
		"components":{"schemas":{"Identifier":false}}
	}`)
	result, err := parse.Decode(input, parse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	document := result.Document()
	if uri, present := document.SchemaURI(); !present || uri != "https://example.com/openrpc-schema" {
		t.Fatalf("SchemaURI() = (%q, %t)", uri, present)
	}
	if _, present := document.ExternalDocs(); !present {
		t.Fatal("ExternalDocs reported absent")
	}
	servers, present := document.Servers()
	if !present || len(servers) != 1 || servers[0].URL() != "https://example.com" {
		t.Fatalf("Servers() = (%#v, %t)", servers, present)
	}
	components, present := document.Components()
	if !present {
		t.Fatal("Components reported absent")
	}
	schemas, present := components.Schemas()
	if !present || len(schemas) != 1 {
		t.Fatalf("Schemas() = (%#v, %t)", schemas, present)
	}
}

func TestDecodePreservesEveryMethodAndComponentField(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Complete","version":"1"},
		"methods":[{
			"name":"sample",
			"params":[],
			"servers":[{"url":"https://example.com"}],
			"tags":[{"name":"sample"}],
			"errors":[{"code":123456789012345678901234567890,"message":"failed"}],
			"links":[{"method":"next","params":{"id":"$params.id"}}],
			"examples":[{"name":"notification","params":[]}],
			"externalDocs":{"url":"https://example.com/method"}
		}],
		"components":{
			"schemas":{"Value":true},
			"links":{"Next":{"method":"next"}},
			"errors":{"Failure":{"code":-1,"message":"failed"}},
			"examples":{"Null":{"name":"null","value":null}},
			"examplePairings":{"Notice":{"name":"notice","params":[]}},
			"contentDescriptors":{"Value":{"name":"value","schema":false}},
			"tags":{"Sample":{"name":"sample"}}
		}
	}`)
	result, err := parse.Decode(input, parse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	method, ok := result.Document().Methods()[0].Method()
	if !ok {
		t.Fatal("method decoded as reference")
	}
	if values, present := method.Servers(); !present || len(values) != 1 {
		t.Fatalf("Servers() = (%#v, %t)", values, present)
	}
	if values, present := method.Tags(); !present || len(values) != 1 {
		t.Fatalf("Tags() = (%#v, %t)", values, present)
	}
	if values, present := method.Errors(); !present || len(values) != 1 {
		t.Fatalf("Errors() = (%#v, %t)", values, present)
	}
	if values, present := method.Links(); !present || len(values) != 1 {
		t.Fatalf("Links() = (%#v, %t)", values, present)
	}
	if values, present := method.Examples(); !present || len(values) != 1 {
		t.Fatalf("Examples() = (%#v, %t)", values, present)
	}
	if _, present := method.ExternalDocs(); !present {
		t.Fatal("ExternalDocs reported absent")
	}

	components, _ := result.Document().Components()
	if values, present := components.Links(); !present || len(values) != 1 {
		t.Fatalf("component Links() = (%#v, %t)", values, present)
	}
	if values, present := components.Errors(); !present || len(values) != 1 {
		t.Fatalf("component Errors() = (%#v, %t)", values, present)
	}
	if values, present := components.Examples(); !present || len(values) != 1 {
		t.Fatalf("component Examples() = (%#v, %t)", values, present)
	}
	if values, present := components.ExamplePairings(); !present || len(values) != 1 {
		t.Fatalf("component ExamplePairings() = (%#v, %t)", values, present)
	}
	if values, present := components.ContentDescriptors(); !present || len(values) != 1 {
		t.Fatalf("component ContentDescriptors() = (%#v, %t)", values, present)
	}
	if values, present := components.Tags(); !present || len(values) != 1 {
		t.Fatalf("component Tags() = (%#v, %t)", values, present)
	}
}

func TestDecodeAcceptsEveryReferenceUnion(t *testing.T) {
	t.Parallel()

	input := []byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"References","version":"1"},
		"methods":[
			{"$ref":"#/components/methods/Referenced"},
			{
				"name":"references",
				"params":[{"$ref":"#/components/contentDescriptors/Input"}],
				"result":{"$ref":"#/components/contentDescriptors/Output"},
				"tags":[{"$ref":"#/components/tags/Tag"}],
				"errors":[{"$ref":"#/components/errors/Failure"}],
				"links":[{"$ref":"#/components/links/Next"}],
				"examples":[{"$ref":"#/components/examplePairings/Pairing"}]
			}
		],
		"components":{
			"links":{"Next":{"method":"next"}},
			"errors":{"Failure":{"code":-1,"message":"failure"}},
			"examplePairings":{"Pairing":{"name":"pairing","params":[]}},
			"contentDescriptors":{
				"Input":{"name":"input","schema":true},
				"Output":{"name":"output","schema":true}
			},
			"tags":{"Tag":{"name":"tag"}}
		}
	}`)
	result, err := parse.Decode(input, parse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	methods := result.Document().Methods()
	if _, ok := methods[0].Reference(); !ok {
		t.Fatal("first method did not retain its reference case")
	}
	method, ok := methods[1].Method()
	if !ok {
		t.Fatal("second method did not retain its object case")
	}
	if _, ok := method.Params()[0].Reference(); !ok {
		t.Fatal("parameter did not retain its reference case")
	}
	if resultValue, present := method.Result(); !present {
		t.Fatal("result reported absent")
	} else if _, ok := resultValue.Reference(); !ok {
		t.Fatal("result did not retain its reference case")
	}
}

func TestDecodeReportsStructuralFieldErrorsAtStablePointers(t *testing.T) {
	t.Parallel()

	minimal := func(field string) []byte {
		return []byte(`{"openrpc":"1.4.1","info":{"title":"T","version":"1"},"methods":[],` + field + `}`)
	}
	tests := []struct {
		name    string
		input   []byte
		want    error
		pointer string
	}{
		{name: "root array", input: []byte(`[]`), want: parse.ErrInvalidObject, pointer: "#"},
		{name: "missing version", input: []byte(`{"info":{"title":"T","version":"1"},"methods":[]}`), want: openrpc.ErrMissingRequiredField, pointer: "#/openrpc"},
		{name: "version type", input: []byte(`{"openrpc":1,"info":{"title":"T","version":"1"},"methods":[]}`), want: parse.ErrInvalidObject, pointer: "#/openrpc"},
		{name: "missing info", input: []byte(`{"openrpc":"1.4.1","methods":[]}`), want: openrpc.ErrMissingRequiredField, pointer: "#/info"},
		{name: "info type", input: []byte(`{"openrpc":"1.4.1","info":[],"methods":[]}`), want: parse.ErrInvalidObject, pointer: "#/info"},
		{name: "title type", input: []byte(`{"openrpc":"1.4.1","info":{"title":1,"version":"1"},"methods":[]}`), want: parse.ErrInvalidObject, pointer: "#/info/title"},
		{name: "schema URI type", input: minimal(`"$schema":false`), want: parse.ErrInvalidObject, pointer: "#/$schema"},
		{name: "contact type", input: []byte(`{"openrpc":"1.4.1","info":{"title":"T","version":"1","contact":[]},"methods":[]}`), want: parse.ErrInvalidObject, pointer: "#/info/contact"},
		{name: "license type", input: []byte(`{"openrpc":"1.4.1","info":{"title":"T","version":"1","license":[]},"methods":[]}`), want: parse.ErrInvalidObject, pointer: "#/info/license"},
		{name: "external docs type", input: minimal(`"externalDocs":[]`), want: parse.ErrInvalidObject, pointer: "#/externalDocs"},
		{name: "servers type", input: minimal(`"servers":{}`), want: parse.ErrInvalidObject, pointer: "#/servers"},
		{name: "server type", input: minimal(`"servers":[false]`), want: parse.ErrInvalidObject, pointer: "#/servers/0"},
		{name: "server variables type", input: minimal(`"servers":[{"url":"https://example.com","variables":[]}]`), want: parse.ErrInvalidObject, pointer: "#/servers/0/variables"},
		{name: "methods type", input: []byte(`{"openrpc":"1.4.1","info":{"title":"T","version":"1"},"methods":{}}`), want: parse.ErrInvalidObject, pointer: "#/methods"},
		{name: "method type", input: []byte(`{"openrpc":"1.4.1","info":{"title":"T","version":"1"},"methods":[false]}`), want: parse.ErrInvalidObject, pointer: "#/methods/0"},
		{name: "components type", input: minimal(`"components":[]`), want: parse.ErrInvalidObject, pointer: "#/components"},
		{name: "schemas type", input: minimal(`"components":{"schemas":[]}`), want: parse.ErrInvalidObject, pointer: "#/components/schemas"},
		{name: "schema invalid JSON Schema", input: minimal(`"components":{"schemas":{"Broken":1}}`), want: jsonschema.ErrInvalidSchema, pointer: "#/components/schemas/Broken"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := parse.Decode(test.input, parse.DefaultOptions())
			if !errors.Is(err, test.want) {
				t.Fatalf("Decode error = %v, want %v", err, test.want)
			}
			var parseError *parse.Error
			if !errors.As(err, &parseError) || parseError.Pointer != test.pointer {
				t.Fatalf("Decode error = %#v, want pointer %q", err, test.pointer)
			}
		})
	}
}
