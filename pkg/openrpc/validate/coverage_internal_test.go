package validate

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
	"github.com/faustbrian/golib/pkg/openrpc/reference"
)

func TestMetaSchemaInternalFailurePaths(t *testing.T) {
	value := mustJSONValue(t, `{}`)
	if report := MetaSchema(explicitNilContext(), value, 1); !errors.Is(report.Err(), ErrMetaSchemaPolicy) {
		t.Fatalf("nil context error = %v", report.Err())
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if report := MetaSchema(canceled, value, 1); !errors.Is(report.Err(), context.Canceled) {
		t.Fatalf("canceled context error = %v", report.Err())
	}

	metaSchemaOnce = sync.Once{}
	metaSchemaOnce.Do(func() { metaSchemaError = errors.New("unavailable") })
	if report := MetaSchema(context.Background(), value, 1); !errors.Is(report.Err(), ErrMetaSchemaUnavailable) {
		t.Fatalf("unavailable error = %v", report.Err())
	}
	metaSchemaOnce = sync.Once{}
	metaSchemaError = nil
	metaSchemaValidator = jsonschema.Validator{}
	MetaSchema(context.Background(), value, 1)
}

func TestMetaSchemaHelpersCoverMalformedAndPartialShapes(t *testing.T) {
	t.Parallel()

	if _, err := draft7CompatibleMetaSchema([]byte(`{`)); err == nil {
		t.Fatal("malformed meta-schema succeeded")
	}
	valid := openrpc.MetaSchema()
	for _, test := range []struct {
		name      string
		schema    []byte
		companion []byte
	}{
		{name: "schema decode", schema: []byte(`{`), companion: valid},
		{name: "companion decode", schema: valid, companion: []byte(`{`)},
		{name: "schema parse", schema: []byte(`null`), companion: valid},
		{name: "companion parse", schema: valid, companion: []byte(`null`)},
		{name: "compile", schema: []byte(`{"$ref":"#/missing"}`), companion: []byte(`{}`)},
	} {
		if _, err := compileMetaSchemaBytes(test.schema, test.companion); err == nil {
			t.Errorf("%s succeeded", test.name)
		}
	}

	for _, value := range []any{
		nil,
		map[string]any{},
		map[string]any{"definitions": true},
		map[string]any{"definitions": map[string]any{"serverObject": true}},
		map[string]any{"definitions": map[string]any{"serverObject": map[string]any{"properties": true}}},
		map[string]any{"definitions": map[string]any{"serverObject": map[string]any{"properties": map[string]any{"url": true}}}},
	} {
		alignServerURLWithNormativeSemantics(value)
	}
	document := map[string]any{
		"$schema":  "https://meta.json-schema.tools/",
		"$ref":     "https://meta.json-schema.tools/definitions/value",
		"children": []any{map[string]any{"$schema": "other"}},
	}
	rewriteMetaDialect(document)
	if document["$schema"] != "http://json-schema.org/draft-07/schema#" ||
		document["$ref"] != "https://meta.json-schema.tools/definitions/value" {
		t.Fatalf("rewritten document = %#v", document)
	}
}

func TestResolvedPathSelectionAndFailures(t *testing.T) {
	t.Parallel()

	wantSelected := [][]string{
		{"methods", "0"},
		{"methods", "0", "result"},
		{"methods", "0", "params", "1"},
		{"methods", "0", "examples", "1", "result"},
		{"methods", "0", "examples", "1", "params", "2"},
		{"components", "examplePairings", "x", "result"},
		{"components", "examplePairings", "x", "params", "0"},
	}
	for _, path := range wantSelected {
		if !openRPCReferencePath(path) {
			t.Errorf("path not selected: %#v", path)
		}
	}
	for _, path := range [][]string{
		nil, {"methods", "-1"}, {"methods", "00"},
		{"methods", "0", "name"}, {"methods", "0", "params", "x"},
		{"methods", "0", "examples", "0", "params", "x"},
		{"components", "schemas", "x", "result"},
		{"components", "examplePairings", "x", "other"},
		{"components", "examplePairings", "x", "params", "x"},
	} {
		if openRPCReferencePath(path) {
			t.Errorf("path selected: %#v", path)
		}
	}
	if arrayIndex("-1") || arrayIndex("01") || arrayIndex("x") || !arrayIndex("0") {
		t.Fatal("array index classification failed")
	}
	if !oneOf("b", "a", "b") || oneOf("c", "a", "b") {
		t.Fatal("oneOf classification failed")
	}
	for _, report := range []Report{resolutionFailure("#/x"), resolvedDocumentFailure()} {
		if report.Valid() || len(report.Diagnostics()) != 1 {
			t.Fatalf("failure report = %#v", report)
		}
	}
	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, report := ResolveDocument(context.Background(), openrpc.Document{}, "https://example.com/root", resolver, DefaultResolvedOptions()); report.Valid() {
		t.Fatal("zero resolved document succeeded")
	}
	document := mustDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[]}`)
	if _, report := ResolveDocument(explicitNilContext(), document, "https://example.com/root", resolver, DefaultResolvedOptions()); report.Valid() {
		t.Fatal("nil resolved context succeeded")
	}
	if _, report := ResolveDocument(context.Background(), document, "https://example.com/root", nil, DefaultResolvedOptions()); report.Valid() {
		t.Fatal("nil resolver succeeded")
	}
	options := DefaultResolvedOptions()
	options.Parse.JSON.MaxBytes = 1
	if _, report := ResolveDocument(context.Background(), document, "https://example.com/root", resolver, options); report.Valid() {
		t.Fatal("bounded resolved parse succeeded")
	}
	if report := ResolvedDocument(context.Background(), document, "relative", resolver, DefaultResolvedOptions()); report.Valid() {
		t.Fatal("relative resolved base succeeded")
	}
}

func TestResolvedSchemaTraversalAndReportMerging(t *testing.T) {
	t.Parallel()

	value := map[string]any{
		"$id":          "nested/",
		"$ref":         "first.json#/x",
		"allOf":        []any{map[string]any{"$ref": "second.json"}},
		"items":        []any{map[string]any{"$ref": "third.json"}},
		"properties":   map[string]any{"x": map[string]any{"$ref": "#local"}},
		"dependencies": map[string]any{"x": map[string]any{"$ref": "fourth.json"}},
		"not":          map[string]any{"$ref": "fifth.json"},
	}
	locations := make([]schemaReferenceLocation, 0)
	walkSchemaReferenceValues(value, "#/schema", "https://example.com/root", &locations)
	if len(locations) != 5 {
		t.Fatalf("locations = %#v", locations)
	}
	walkSchemaReferenceValues(true, "#", "relative", &locations)
	walkSchemaArrayReferences(true, "#", "relative", &locations)
	invalidBase := make([]schemaReferenceLocation, 0)
	walkSchemaReferenceValues(map[string]any{"$id": "child", "$ref": "%"}, "#", "relative", &invalidBase)
	if len(invalidBase) != 1 || invalidBase[0].ref != "%" {
		t.Fatalf("invalid base locations = %#v", invalidBase)
	}
	if _, err := resolveSchemaURI("relative", "child"); !errors.Is(err, reference.ErrInvalidBase) {
		t.Fatalf("relative schema base error = %v", err)
	}
	if _, err := resolveSchemaURI("https://example.com/root", "%"); err == nil {
		t.Fatal("invalid schema reference succeeded")
	}

	base := Report{diagnostics: []Diagnostic{{Code: "a", Pointer: "#/a", Severity: SeverityError}}}
	additional := Report{diagnostics: []Diagnostic{
		{Code: "a", Pointer: "#/a", Severity: SeverityError},
		{Code: "b", Pointer: "#/b", Severity: SeverityError},
	}}
	options := DefaultOptions()
	merged := mergeValidationReports(base, additional, options)
	if len(merged.diagnostics) != 2 || merged.truncated {
		t.Fatalf("merged report = %#v", merged)
	}
	options.MaxDiagnostics = 1
	if merged := mergeValidationReports(base, additional, options); !merged.truncated {
		t.Fatalf("bounded merged report = %#v", merged)
	}
	options = DefaultOptions()
	options.Mode = FailFast
	if merged := mergeValidationReports(base, additional, options); !reflect.DeepEqual(merged, base) {
		t.Fatalf("fail-fast base report = %#v", merged)
	}
	empty := mergeValidationReports(Report{}, additional, options)
	if len(empty.diagnostics) != 1 {
		t.Fatalf("fail-fast additional report = %#v", empty)
	}
}

func TestResolvedSchemaDocumentLocationsAndFailures(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"x","version":"1"},
		"methods":[
			{"$ref":"#/components/methods/ignored"},
			{"name":"one","params":[
				{"$ref":"#/components/contentDescriptors/ignored"},
				{"name":"input","schema":{"anyOf":[{"$ref":"b.json"},{"$ref":"a.json"}]}}
			],"result":{"$ref":"#/components/contentDescriptors/ignored"}},
			{"name":"two","params":[],"result":{"name":"output","schema":{"$ref":"result.json"}}}
		],
		"components":{
			"schemas":{"One":{"$ref":"schema.json"}},
			"contentDescriptors":{"One":{"name":"one","schema":{"$ref":"descriptor.json"}}}
		}
	}`)
	locations := externalSchemaReferences(document, "https://example.com/root")
	if len(locations) != 5 {
		t.Fatalf("external schema locations = %#v", locations)
	}
	for index := 1; index < len(locations); index++ {
		if locations[index-1].pointer > locations[index].pointer {
			t.Fatalf("locations are not sorted: %#v", locations)
		}
	}
	badLocations := make([]schemaReferenceLocation, 0)
	appendSchemaReferences("#", jsonschema.Schema{}, "https://example.com/root", &badLocations)
	if len(badLocations) != 0 {
		t.Fatalf("zero schema locations = %#v", badLocations)
	}

	encoded, err := openrpc.MarshalCanonical(document)
	if err != nil {
		t.Fatal(err)
	}
	root, err := jsonvalue.Parse(encoded, jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if report := validateResolvedSchemas(context.Background(), document, root, "https://example.com/root", resolver, DefaultOptions()); report.Valid() || report.diagnostics[0].Code != CodeReferenceResolution {
		t.Fatalf("resource resolution report = %#v", report)
	}
	store, err := reference.NewMemoryStore(map[string][]byte{
		"https://example.com/a.json":          []byte(`[]`),
		"https://example.com/b.json":          []byte(`{}`),
		"https://example.com/result.json":     []byte(`{}`),
		"https://example.com/schema.json":     []byte(`{}`),
		"https://example.com/descriptor.json": []byte(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	policy := reference.DefaultResolvePolicy()
	policy.AllowExternal = true
	policy.AllowedSchemes = []string{"https"}
	policy.AllowedHosts = []string{"example.com"}
	resolver, err = reference.NewResolver(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	if report := validateResolvedSchemas(context.Background(), document, root, "https://example.com/root", resolver, DefaultOptions()); report.Valid() || report.diagnostics[0].Code != CodeInvalidSchema {
		t.Fatalf("invalid resource schema report = %#v", report)
	}
}

func TestSchemaInternalErrorAndRewritePaths(t *testing.T) {
	t.Parallel()

	valid := mustSchema(t, `{"$ref":"#/components/schemas/Thing"}`)
	component := mustSchema(t, `{"type":"string"}`)
	unit, err := schemaCompilationUnit(valid, map[string]jsonschema.Schema{"Thing": component}, false)
	if err != nil || len(unit.Bytes()) == 0 {
		t.Fatalf("compilation unit = %s, %v", unit.Bytes(), err)
	}
	if _, err := schemaCompilationUnit(jsonschema.Schema{}, nil, false); err == nil {
		t.Fatal("zero target schema succeeded")
	}
	if _, err := schemaCompilationUnit(component, map[string]jsonschema.Schema{"bad": {}}, false); err == nil {
		t.Fatal("zero component schema succeeded")
	}
	invalidReference := mustSchema(t, `{"$ref":"\n"}`)
	if _, err := schemaCompilationUnit(component, map[string]jsonschema.Schema{"bad": invalidReference}, false); err == nil {
		t.Fatal("invalid component reference succeeded")
	}
	if _, err := schemaCompilationUnit(invalidReference, nil, false); err == nil {
		t.Fatal("invalid target reference succeeded")
	}
	for _, input := range []string{"bad\nref", "bad ref", "bad\x7fref", "%", string([]byte{0xff})} {
		if validSchemaReference(input) {
			t.Errorf("invalid schema reference accepted: %q", input)
		}
	}
	if !validSchemaReference("") || !validSchemaReference("#/ok") {
		t.Fatal("valid schema reference rejected")
	}
	object := map[string]any{
		"external":  map[string]any{"$ref": "child.json"},
		"component": map[string]any{"$ref": "#/components/schemas/X"},
		"array":     []any{map[string]any{"$ref": "#/local"}},
	}
	if err := rewriteSchemaReferences(object, false); err != nil {
		t.Fatal(err)
	}
	if _, exists := object["external"].(map[string]any)["$ref"]; exists ||
		object["component"].(map[string]any)["$ref"] != "#/definitions/X" {
		t.Fatalf("rewritten schema = %#v", object)
	}
	if err := rewriteSchemaReferences(map[string]any{"$ref": "\n"}, true); err == nil {
		t.Fatal("invalid nested schema reference succeeded")
	}
	for _, value := range []any{
		map[string]any{"child": map[string]any{"$ref": "\n"}},
		[]any{map[string]any{"$ref": "\n"}},
	} {
		if err := rewriteSchemaReferences(value, true); err == nil {
			t.Fatalf("invalid recursive reference succeeded: %#v", value)
		}
	}
	engine := validator{ctx: context.Background(), options: DefaultOptions()}
	engine.stop = true
	engine.validateSchema("#", component, nil, "", nil, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine = validator{ctx: ctx, options: DefaultOptions()}
	engine.validateSchema("#", component, nil, "", nil, false)
}

func TestValidatorReferenceUnionsAndLoopStops(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"x","version":"1"},
		"methods":[{"$ref":"#/valid"}, {
			"name":"all","params":[{"$ref":"%"}],
			"tags":[{"$ref":"%"}],"result":{"$ref":"%"},
			"errors":[{"$ref":"%"}],"links":[{"$ref":"%"}],
			"examples":[{"$ref":"%"}]
		}]
	}`)
	engine := validator{ctx: context.Background(), options: DefaultOptions()}
	engine.validateMethodReferences(document.Methods())
	if len(engine.diagnostics) != 6 {
		t.Fatalf("union reference diagnostics = %#v", engine.diagnostics)
	}
	validReference, err := openrpc.NewReference("#/ok")
	if err != nil {
		t.Fatal(err)
	}
	engine.validateReference("#", validReference)
	engine.stop = true
	engine.validateReference("#", validReference)

	method, inline := document.Methods()[1].Method()
	if !inline {
		t.Fatal("method is not inline")
	}
	engine.stop = false
	engine.validateParameters(0, method)
	engine.validateErrors(0, method)
	engine.validateLinks("#", func() ([]openrpc.LinkOrReference, bool) { return nil, false }, nil)
	engine.validateLinks("#", func() ([]openrpc.LinkOrReference, bool) {
		return []openrpc.LinkOrReference{{}}, true
	}, nil)
	engine.stop = false
	engine.ctx = canceledContext()
	engine.validateParameters(0, method)
	engine.stop = false
	engine.validateErrors(0, method)
	engine.stop = false
	engine.validateLinks("#", func() ([]openrpc.LinkOrReference, bool) {
		return []openrpc.LinkOrReference{{}}, true
	}, nil)
}

func TestDocumentCoversReferencedUnionsServersAndComponents(t *testing.T) {
	t.Parallel()

	document := mustDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"x","version":"1"},
		"methods":[{
			"name":"one","params":[{"$ref":"%"}],"tags":[{"$ref":"%"}],
			"errors":[{"$ref":"%"}],
			"links":[{"$ref":"%"},{"method":"one","server":{"url":"https://example.com"}}],
			"examples":[{"$ref":"%"}],
			"servers":[{"url":"https://example.com"}]
		}],
		"components":{
			"links":{"one":{"method":"one","server":{"url":"https://example.com"}}},
			"examplePairings":{"one":{"name":"one","params":[{"$ref":"%"}]}}
		}
	}`)
	report := Document(context.Background(), document, DefaultOptions())
	if report.Valid() || len(report.diagnostics) != 6 {
		t.Fatalf("comprehensive document report = %#v", report)
	}

	invalidMetadata := mustDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"x","version":"1","contact":{"email":"bad"}},
		"methods":[{"name":"one","params":[]}]
	}`)
	options := DefaultOptions()
	options.Mode = FailFast
	if report := Document(context.Background(), invalidMetadata, options); len(report.diagnostics) != 1 {
		t.Fatalf("metadata fail-fast report = %#v", report)
	}
	componentStop := mustDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[],
		"components":{"links":{"a":{"method":"missing"},"b":{"method":"missing"}}}
	}`)
	if report := Document(context.Background(), componentStop, options); len(report.diagnostics) != 1 {
		t.Fatalf("component fail-fast report = %#v", report)
	}

	engine := validator{ctx: context.Background(), options: DefaultOptions()}
	engine.validateMethodReferences([]openrpc.MethodOrReference{{}})
	engine.validateParameters(0, openrpc.Method{})
	engine.validateErrors(0, openrpc.Method{})
}

func TestValidatorServerAndRuntimeValueBranches(t *testing.T) {
	t.Parallel()

	engine := validator{ctx: context.Background(), options: DefaultOptions()}
	for _, input := range []struct {
		url       string
		variables map[string]openrpc.ServerVariable
	}{
		{url: "https://example.com"},
		{url: "https://${"},
		{url: "https://${missing}"},
	} {
		server, err := openrpc.NewServer(openrpc.ServerInput{URL: input.url, Variables: input.variables})
		if err != nil {
			t.Fatal(err)
		}
		engine.validateServer("#/server", server)
	}
	defaultValue := "text"
	variable, err := openrpc.NewServerVariable(openrpc.ServerVariableInput{Default: &defaultValue})
	if err != nil {
		t.Fatal(err)
	}
	server, err := openrpc.NewServer(openrpc.ServerInput{
		URL: "https://${host.value}", Variables: map[string]openrpc.ServerVariable{"host": variable},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.validateServer("#/server", server)
	oversizedDefault := string(make([]byte, 600_000))
	oversizedVariable, err := openrpc.NewServerVariable(openrpc.ServerVariableInput{Default: &oversizedDefault})
	if err != nil {
		t.Fatal(err)
	}
	server, err = openrpc.NewServer(openrpc.ServerInput{
		URL: "${host}", Variables: map[string]openrpc.ServerVariable{"host": oversizedVariable},
	})
	if err != nil {
		t.Fatal(err)
	}
	engine.validateServer("#/server", server)

	largeDefault := strings.Repeat("a", 600_000)
	largeVariable, err := openrpc.NewServerVariable(openrpc.ServerVariableInput{Default: &largeDefault})
	if err != nil {
		t.Fatal(err)
	}
	server, err = openrpc.NewServer(openrpc.ServerInput{
		URL: "${host}${host}", Variables: map[string]openrpc.ServerVariable{"host": largeVariable},
	})
	if err != nil {
		t.Fatal(err)
	}
	largeEngine := validator{ctx: context.Background(), options: DefaultOptions()}
	largeEngine.validateServer("#/server", server)
	if len(largeEngine.diagnostics) != 1 || largeEngine.diagnostics[0].Code != CodeInvalidRuntimeExpression {
		t.Fatalf("large server diagnostics = %#v", largeEngine.diagnostics)
	}
	link, err := openrpc.NewLink(openrpc.LinkInput{Server: &server})
	if err != nil {
		t.Fatal(err)
	}
	engine.validateLink("#/link", link, nil)

	engine.validateRuntimeValue("#/zero", jsonvalue.Value{})
	validRuntimeEngine := validator{ctx: context.Background(), options: DefaultOptions()}
	validRuntimeEngine.validateRuntimeValue("#/valid", mustJSONValue(t, `"plain"`))
	if len(validRuntimeEngine.diagnostics) != 0 {
		t.Fatalf("valid runtime diagnostics = %#v", validRuntimeEngine.diagnostics)
	}
	engine.walkRuntimeValue("#/plain", "plain")
	engine.walkRuntimeValue("#/invalid", "${bad.}")
	engine.walkRuntimeValue("#/array", []any{"${bad.}"})
	engine.walkRuntimeValue("#/object", map[string]any{"a/b": "${bad.}"})
	engine.stop = true
	engine.walkRuntimeValue("#", "${bad.}")
	if len(engine.diagnostics) < 5 {
		t.Fatalf("runtime diagnostics = %#v", engine.diagnostics)
	}
}

func TestValidatorInternalBoundaries(t *testing.T) {
	t.Parallel()

	warning := Report{diagnostics: []Diagnostic{{Severity: SeverityWarning}}}
	if !warning.Valid() || (Report{diagnostics: []Diagnostic{{Severity: SeverityError}}}).Valid() {
		t.Fatal("report severity classification failed")
	}
	if Document(explicitNilContext(), openrpc.Document{}, DefaultOptions()).Valid() {
		t.Fatal("nil validation context succeeded")
	}
	invalidMode := DefaultOptions()
	invalidMode.Mode = Mode(99)
	if Document(context.Background(), openrpc.Document{}, invalidMode).Valid() {
		t.Fatal("invalid validation mode succeeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine := validator{ctx: ctx, options: DefaultOptions()}
	if !engine.canceled() {
		t.Fatal("canceled engine continued")
	}
	if !engine.canceled() || len(engine.diagnostics) != 1 {
		t.Fatalf("canceled engine = %#v", engine)
	}
	engine.add(Diagnostic{Code: "ignored"})
	if len(engine.diagnostics) != 1 {
		t.Fatal("stopped engine accepted diagnostic")
	}

	engine = validator{ctx: context.Background(), options: Options{Mode: Collect, MaxDiagnostics: 1}}
	engine.add(Diagnostic{Code: "first"})
	engine.add(Diagnostic{Code: "second"})
	if !engine.stop || !engine.truncated || len(engine.report().diagnostics) != 1 {
		t.Fatalf("bounded engine = %#v", engine)
	}
	engine = validator{ctx: context.Background(), options: Options{Mode: FailFast, MaxDiagnostics: 2}}
	engine.add(Diagnostic{Code: "first"})
	if !engine.stop {
		t.Fatal("fail-fast engine did not stop")
	}
	if pointer("a/b", 2, true) != "#/a~1b/2" || escape("a~b") != "a~0b" || itoa(-1) != "-1" {
		t.Fatal("pointer helpers failed")
	}
	if got := sortedNames(map[string]int{"b": 1, "a": 2}); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("sorted names = %#v", got)
	}
	if !conciseSentence("One sentence.") || !conciseSentence(strings.Repeat("a", 200)) ||
		conciseSentence("") || conciseSentence("First. Second") || conciseSentence("!! A") ||
		conciseSentence(string(make([]rune, 201))) || conciseSentence("line\nbreak") {
		t.Fatal("concise sentence boundaries failed")
	}
	if !validEmail("a@example.com") || validEmail("Name <a@example.com>") ||
		!validAbsoluteURI("https://example.com") || validAbsoluteURI("relative") {
		t.Fatal("format helpers failed")
	}
}

func mustJSONValue(t *testing.T, input string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Parse([]byte(input), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustSchema(t *testing.T, input string) jsonschema.Schema {
	t.Helper()
	schema, err := jsonschema.Parse([]byte(input), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func mustDocument(t *testing.T, input string) openrpc.Document {
	t.Helper()
	parsed, err := openrpcparse.Decode([]byte(input), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Document()
}

func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func explicitNilContext() context.Context { return nil }
