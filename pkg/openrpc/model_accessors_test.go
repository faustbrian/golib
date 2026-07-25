package openrpc_test

import (
	"encoding/json"
	"os"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/parse"
)

func TestCompleteModelAccessorsAndUnionCases(t *testing.T) {
	t.Parallel()

	input, err := os.ReadFile("parse/testdata/complete-openrpc.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parse.Decode(input, parse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	document := parsed.Document()
	if encoded, err := openrpc.MarshalCanonical(document); err != nil || !json.Valid(encoded) {
		t.Fatalf("MarshalCanonical() = %s, %v", encoded, err)
	}
	if len(openrpc.MetaSchema()) == 0 || len(openrpc.JSONSchemaToolsMetaSchema()) == 0 {
		t.Fatal("embedded specification resources were empty")
	}
	if document.Version().String() != "1.4.1" || document.Extensions().Len() != 1 ||
		document.UnknownFields().Len() != 0 {
		t.Fatalf("document metadata was not preserved")
	}
	if schemaURI, explicit := document.SchemaURI(); !explicit || schemaURI != "https://meta.open-rpc.org/" {
		t.Fatalf("SchemaURI() = (%q, %t)", schemaURI, explicit)
	}

	info := document.Info()
	if info.Title() != "Complete API" || info.Version() != "1.0.0" ||
		optionalString(t, info.Description) != "**Complete** API description" ||
		optionalString(t, info.TermsOfService) != "https://example.com/terms" ||
		info.Extensions().Len() != 1 || info.UnknownFields().Len() != 0 {
		t.Fatal("Info accessors lost fields")
	}
	contact, present := info.Contact()
	if !present || optionalString(t, contact.Name) != "API team" ||
		optionalString(t, contact.Email) != "api@example.com" ||
		optionalString(t, contact.URL) != "https://example.com/contact" ||
		contact.Extensions().Len() != 1 || contact.UnknownFields().Len() != 0 {
		t.Fatal("Contact accessors lost fields")
	}
	license, present := info.License()
	if !present || optionalString(t, license.Name) != "Apache-2.0" ||
		optionalString(t, license.URL) != "https://www.apache.org/licenses/LICENSE-2.0" ||
		license.Extensions().Len() != 1 || license.UnknownFields().Len() != 0 {
		t.Fatal("License accessors lost fields")
	}
	docs, present := document.ExternalDocs()
	if !present || docs.URL() != "https://example.com/docs" ||
		optionalString(t, docs.Description) != "External documentation" ||
		docs.Extensions().Len() != 1 || docs.UnknownFields().Len() != 0 {
		t.Fatal("ExternalDocumentation accessors lost fields")
	}

	servers, present := document.Servers()
	if !present || len(servers) != 1 || len(document.EffectiveServers()) != 1 {
		t.Fatalf("Servers() = (%#v, %t)", servers, present)
	}
	assertCompleteServer(t, servers[0])

	if document.MethodCount() != 1 {
		t.Fatalf("MethodCount() = %d", document.MethodCount())
	}
	methodUnion := document.Methods()[0]
	method, inline := methodUnion.Method()
	if !inline {
		t.Fatal("method was not inline")
	}
	if _, referenceCase := methodUnion.Reference(); referenceCase {
		t.Fatal("inline method reported a reference")
	}
	if method.Name() != "things.read" ||
		optionalString(t, method.Summary) != "Read a thing" ||
		optionalString(t, method.Description) != "Reads one thing" ||
		method.Extensions().Len() != 1 || method.UnknownFields().Len() != 0 {
		t.Fatal("Method accessors lost fields")
	}
	if structure, explicit := method.ParamStructure(); !explicit || structure != openrpc.ParamStructureByName {
		t.Fatalf("ParamStructure() = (%q, %t)", structure, explicit)
	}
	if deprecated, explicit := method.Deprecated(); !explicit || deprecated || method.DeprecatedOrDefault() {
		t.Fatalf("Deprecated() = (%t, %t)", deprecated, explicit)
	}
	methodServers, present := method.Servers()
	if !present || len(methodServers) != 1 {
		t.Fatalf("method Servers() = (%#v, %t)", methodServers, present)
	}
	assertCompleteServer(t, methodServers[0])

	tags, present := method.Tags()
	if !present || len(tags) != 1 {
		t.Fatalf("Tags() = (%#v, %t)", tags, present)
	}
	tag, inline := tags[0].Tag()
	if !inline || tag.Name() != "things" ||
		optionalString(t, tag.Description) != "Thing operations" ||
		tag.Extensions().Len() != 1 || tag.UnknownFields().Len() != 0 {
		t.Fatal("Tag accessors lost fields")
	}
	if _, present := tag.ExternalDocs(); !present {
		t.Fatal("tag external docs reported absent")
	}

	parameters := method.Params()
	if len(parameters) != 1 {
		t.Fatalf("Params() = %#v", parameters)
	}
	descriptor, inline := parameters[0].Descriptor()
	if !inline {
		t.Fatal("parameter was not inline")
	}
	assertCompleteDescriptor(t, descriptor, true)
	resultUnion, present := method.Result()
	if !present {
		t.Fatal("result reported absent")
	}
	result, inline := resultUnion.Descriptor()
	if !inline {
		t.Fatal("result was not inline")
	}
	assertCompleteDescriptor(t, result, false)

	errorsList, present := method.Errors()
	if !present || len(errorsList) != 1 {
		t.Fatalf("Errors() = (%#v, %t)", errorsList, present)
	}
	methodError, inline := errorsList[0].Error()
	if !inline || methodError.Code().String() != "-31999" ||
		methodError.Message() != "Thing unavailable" || methodError.Extensions().Len() != 0 {
		t.Fatal("Error accessors lost fields")
	}
	if data, present := methodError.Data(); !present || !json.Valid(data.Bytes()) {
		t.Fatalf("Data() = (%s, %t)", data.Bytes(), present)
	}

	links, present := method.Links()
	if !present || len(links) != 1 {
		t.Fatalf("Links() = (%#v, %t)", links, present)
	}
	link, inline := links[0].Link()
	if !inline || optionalString(t, link.Summary) != "Next thing" ||
		optionalString(t, link.Description) != "Reads the next thing" ||
		optionalString(t, link.Method) != "things.read" || link.Extensions().Len() != 1 {
		t.Fatal("Link accessors lost fields")
	}
	if name, present := link.Name(); !present || !json.Valid(name.Bytes()) {
		t.Fatalf("link Name() = (%s, %t)", name.Bytes(), present)
	}
	if params, present := link.Params(); !present || !json.Valid(params.Bytes()) {
		t.Fatalf("link Params() = (%s, %t)", params.Bytes(), present)
	}
	if server, present := link.Server(); !present || server.URL() != "https://example.com/rpc" {
		t.Fatalf("link Server() = (%#v, %t)", server, present)
	}

	pairings, present := method.Examples()
	if !present || len(pairings) != 1 {
		t.Fatalf("Examples() = (%#v, %t)", pairings, present)
	}
	pairing, inline := pairings[0].ExamplePairing()
	if !inline || pairing.Name() != "read example" ||
		optionalString(t, pairing.Description) != "Complete request and response" ||
		pairing.UnknownFields().Len() != 1 {
		t.Fatal("ExamplePairing accessors lost fields")
	}
	if len(pairing.Params()) != 1 {
		t.Fatal("pairing params reported absent")
	}
	example, inline := pairing.Params()[0].Example()
	if !inline {
		t.Fatal("pairing parameter was not inline")
	}
	assertCompleteExample(t, example, "id")
	resultExample, present := pairing.Result()
	if !present {
		t.Fatal("pairing result reported absent")
	}
	example, inline = resultExample.Example()
	if !inline {
		t.Fatal("pairing result was not inline")
	}
	assertCompleteExample(t, example, "thing")
	if _, present := method.ExternalDocs(); !present {
		t.Fatal("method external docs reported absent")
	}

	assertCompleteComponents(t, document)
	assertReferenceUnionCases(t)
}

func assertCompleteServer(t *testing.T, server openrpc.Server) {
	t.Helper()
	if server.URL() == "" || optionalString(t, server.Name) == "" ||
		optionalString(t, server.Summary) == "" ||
		optionalString(t, server.Description) == "" || server.Extensions().Len() != 1 {
		t.Fatal("Server accessors lost fields")
	}
	variables, present := server.Variables()
	if !present {
		t.Fatal("server variables reported absent")
	}
	for _, variable := range variables {
		if variable.Default() == "" || optionalString(t, variable.Description) == "" {
			t.Fatal("ServerVariable accessors lost fields")
		}
		if values, present := variable.Enum(); !present || len(values) == 0 {
			t.Fatalf("Enum() = (%#v, %t)", values, present)
		}
		_ = variable.UnknownFields()
	}
}

func assertCompleteDescriptor(t *testing.T, descriptor openrpc.ContentDescriptor, required bool) {
	t.Helper()
	if descriptor.Name() == "" || optionalString(t, descriptor.Summary) == "" ||
		optionalString(t, descriptor.Description) == "" || descriptor.Extensions().Len() != 1 {
		t.Fatal("ContentDescriptor accessors lost fields")
	}
	if value, present := descriptor.Required(); !present || value != required ||
		descriptor.RequiredOrDefault() != required {
		t.Fatalf("Required() = (%t, %t)", value, present)
	}
	if value, present := descriptor.Deprecated(); !present || value || descriptor.DeprecatedOrDefault() {
		t.Fatalf("Deprecated() = (%t, %t)", value, present)
	}
	schema := descriptor.Schema()
	if len(schema.Bytes()) == 0 || len(schema.Value().Bytes()) == 0 {
		t.Fatal("Schema accessors returned empty values")
	}
	if encoded, err := schema.MarshalJSON(); err != nil || len(encoded) == 0 {
		t.Fatalf("Schema.MarshalJSON() = %s, %v", encoded, err)
	}
}

func assertCompleteExample(t *testing.T, example openrpc.Example, name string) {
	t.Helper()
	if example.Name() != name || optionalString(t, example.Summary) == "" ||
		optionalString(t, example.Description) == "" || len(example.Value().Bytes()) == 0 ||
		example.UnknownFields().Len() != 1 {
		t.Fatal("Example accessors lost fields")
	}
}

func assertCompleteComponents(t *testing.T, document openrpc.Document) {
	t.Helper()
	components, present := document.Components()
	if !present || components.UnknownFields().Len() != 1 {
		t.Fatalf("Components() = (%#v, %t)", components, present)
	}
	if values, present := components.Schemas(); !present || len(values) != 1 {
		t.Fatalf("Schemas() = (%#v, %t)", values, present)
	}
	if values, present := components.Links(); !present || len(values) != 1 {
		t.Fatalf("Links() = (%#v, %t)", values, present)
	}
	if values, present := components.Errors(); !present || len(values) != 1 {
		t.Fatalf("Errors() = (%#v, %t)", values, present)
	}
	if values, present := components.Examples(); !present || len(values) != 1 {
		t.Fatalf("Examples() = (%#v, %t)", values, present)
	}
	if values, present := components.ExamplePairings(); !present || len(values) != 1 {
		t.Fatalf("ExamplePairings() = (%#v, %t)", values, present)
	}
	if values, present := components.ContentDescriptors(); !present || len(values) != 1 {
		t.Fatalf("ContentDescriptors() = (%#v, %t)", values, present)
	}
	if values, present := components.Tags(); !present || len(values) != 1 {
		t.Fatalf("Tags() = (%#v, %t)", values, present)
	}
}

func assertReferenceUnionCases(t *testing.T) {
	t.Helper()
	reference, err := openrpc.NewReference("#/components/schemas/Identifier")
	if err != nil || reference.Ref() == "" {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		ok   bool
	}{
		{"descriptor", referenceCase(openrpc.ContentDescriptorReference(reference).Reference())},
		{"tag", referenceCase(openrpc.TagReference(reference).Reference())},
		{"error", referenceCase(openrpc.ErrorReference(reference).Reference())},
		{"link", referenceCase(openrpc.LinkReference(reference).Reference())},
		{"example", referenceCase(openrpc.ExampleReference(reference).Reference())},
		{"pairing", referenceCase(openrpc.ExamplePairingReference(reference).Reference())},
		{"method", referenceCase(openrpc.MethodReference(reference).Reference())},
	}
	for _, test := range cases {
		if !test.ok {
			t.Errorf("%s reference case reported absent", test.name)
		}
	}
}

func referenceCase(_ openrpc.Reference, present bool) bool { return present }

func optionalString(t *testing.T, getter func() (string, bool)) string {
	t.Helper()
	value, present := getter()
	if !present {
		t.Fatal("optional string reported absent")
	}
	return value
}
