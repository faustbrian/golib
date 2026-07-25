package validate_test

import (
	"context"
	"reflect"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
	"github.com/faustbrian/golib/pkg/openrpc/reference"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

func TestCollectReportsDeterministicSemanticDiagnostics(t *testing.T) {
	t.Parallel()

	document := invalidSemanticDocument(t)
	report := validate.Document(context.Background(), document, validate.DefaultOptions())
	diagnostics := report.Diagnostics()
	wantCodes := []validate.Code{
		validate.CodeDuplicateMethodName,
		validate.CodeDuplicateParameterName,
		validate.CodeRequiredParameterOrder,
		validate.CodeDuplicateErrorCode,
		validate.CodeUnresolvedLinkMethod,
	}
	gotCodes := make([]validate.Code, len(diagnostics))
	for index, diagnostic := range diagnostics {
		gotCodes[index] = diagnostic.Code
		if diagnostic.Severity != validate.SeverityError || diagnostic.Pointer == "" || diagnostic.Specification == "" {
			t.Fatalf("incomplete diagnostic: %#v", diagnostic)
		}
	}
	if !reflect.DeepEqual(gotCodes, wantCodes) {
		t.Fatalf("codes = %#v, want %#v", gotCodes, wantCodes)
	}
	if diagnostics[0].Pointer != "#/methods/1/name" ||
		diagnostics[1].Pointer != "#/methods/0/params/1/name" {
		t.Fatalf("unexpected pointers: %#v", diagnostics)
	}

	returned := report.Diagnostics()
	returned[0].Code = "changed"
	if report.Diagnostics()[0].Code != validate.CodeDuplicateMethodName {
		t.Fatal("Diagnostics exposed mutable report storage")
	}
}

func TestFailFastUsesTheSameFirstRule(t *testing.T) {
	t.Parallel()

	document := invalidSemanticDocument(t)
	collect := validate.Document(context.Background(), document, validate.DefaultOptions())
	options := validate.DefaultOptions()
	options.Mode = validate.FailFast
	failFast := validate.Document(context.Background(), document, options)
	if len(failFast.Diagnostics()) != 1 {
		t.Fatalf("fail-fast diagnostics = %#v", failFast.Diagnostics())
	}
	if !reflect.DeepEqual(failFast.Diagnostics()[0], collect.Diagnostics()[0]) {
		t.Fatalf("fail-fast first = %#v, collect first = %#v", failFast.Diagnostics()[0], collect.Diagnostics()[0])
	}
}

func TestValidationEnforcesDiagnosticAndCancellationBounds(t *testing.T) {
	t.Parallel()

	document := invalidSemanticDocument(t)
	options := validate.DefaultOptions()
	options.MaxDiagnostics = 2
	report := validate.Document(context.Background(), document, options)
	if len(report.Diagnostics()) != 2 || !report.Truncated() {
		t.Fatalf("bounded report = %#v, truncated=%t", report.Diagnostics(), report.Truncated())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report = validate.Document(ctx, document, validate.DefaultOptions())
	if len(report.Diagnostics()) != 1 || report.Diagnostics()[0].Code != validate.CodeCanceled {
		t.Fatalf("canceled report = %#v", report.Diagnostics())
	}
}

func TestValidationBoundsGeneratedMethodWork(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1","info":{"title":"Bounded","version":"1"},
		"methods":[{"name":"one","params":[]},{"name":"two","params":[]}]
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	options := validate.DefaultOptions()
	options.MaxMethods = 1
	report := validate.Document(context.Background(), parsed.Document(), options)
	if report.Valid() || !report.Truncated() || len(report.Diagnostics()) != 1 ||
		report.Diagnostics()[0].Code != validate.CodeResourceLimit {
		t.Fatalf("bounded report = %#v, truncated = %t", report.Diagnostics(), report.Truncated())
	}

	options.MaxMethods = 2
	if report := validate.Document(context.Background(), parsed.Document(), options); !report.Valid() {
		t.Fatalf("exact method limit diagnostics = %#v", report.Diagnostics())
	}
	options.MaxMethods = 0
	if report := validate.Document(context.Background(), parsed.Document(), options); report.Diagnostics()[0].Code != validate.CodeInvalidOptions {
		t.Fatalf("invalid method options = %#v", report.Diagnostics())
	}
}

func TestValidationChecksRuntimeExpressionsAndServerBindings(t *testing.T) {
	t.Parallel()

	host := "example.com"
	variable, err := openrpc.NewServerVariable(openrpc.ServerVariableInput{Default: &host})
	if err != nil {
		t.Fatal(err)
	}
	server, err := openrpc.NewServer(openrpc.ServerInput{
		URL:       "https://${host}:${port}/",
		Variables: map[string]openrpc.ServerVariable{"host": variable},
	})
	if err != nil {
		t.Fatal(err)
	}
	params, err := jsonvalue.Parse([]byte(`{"id":"${result.}"}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	target := "linked"
	link, err := openrpc.NewLink(openrpc.LinkInput{Method: &target, Params: &params})
	if err != nil {
		t.Fatal(err)
	}
	method, err := openrpc.NewMethod(openrpc.MethodInput{
		Name:   "linked",
		Params: []openrpc.ContentDescriptorOrReference{},
		Links:  []openrpc.LinkOrReference{openrpc.LinkValue(link)},
	})
	if err != nil {
		t.Fatal(err)
	}
	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{Title: "Expressions", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version:    version,
		Info:       &info,
		Servers:    []openrpc.Server{server},
		HasServers: true,
		Methods:    []openrpc.MethodOrReference{openrpc.MethodValue(method)},
	})
	if err != nil {
		t.Fatal(err)
	}
	report := validate.Document(context.Background(), document, validate.DefaultOptions())
	want := []validate.Code{
		validate.CodeMissingServerVariable,
		validate.CodeInvalidRuntimeExpression,
	}
	got := make([]validate.Code, len(report.Diagnostics()))
	for index, diagnostic := range report.Diagnostics() {
		got[index] = diagnostic.Code
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codes = %#v, want %#v", got, want)
	}
}

func TestValidationChecksReferenceSyntaxAtEveryUnion(t *testing.T) {
	t.Parallel()

	invalid, err := openrpc.NewReference("%zz")
	if err != nil {
		t.Fatal(err)
	}
	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{Title: "References", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version: version,
		Info:    &info,
		Methods: []openrpc.MethodOrReference{openrpc.MethodReference(invalid)},
	})
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := validate.Document(context.Background(), document, validate.DefaultOptions()).Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != validate.CodeInvalidReference ||
		diagnostics[0].Pointer != "#/methods/0/$ref" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestValidationChecksExamplePairingReferenceSyntax(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"References","version":"1"},
		"methods":[{
			"name":"example",
			"params":[],
			"examples":[{
				"name":"invalid",
				"params":[{"$ref":"%zz"}],
				"result":{"$ref":"%zz"}
			}]
		}]
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := validate.Document(
		context.Background(), parsed.Document(), validate.DefaultOptions(),
	).Diagnostics()
	want := []string{
		"#/methods/0/examples/0/params/0/$ref",
		"#/methods/0/examples/0/result/$ref",
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for index, pointer := range want {
		if diagnostics[index].Code != validate.CodeInvalidReference ||
			diagnostics[index].Pointer != pointer {
			t.Fatalf("diagnostics[%d] = %#v", index, diagnostics[index])
		}
	}
}

func TestDocumentRejectsInvalidValidationOptions(t *testing.T) {
	t.Parallel()

	report := validate.Document(context.Background(), invalidSemanticDocument(t), validate.Options{})
	diagnostics := report.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != validate.CodeInvalidOptions ||
		diagnostics[0].Pointer != "#" || report.Valid() {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	options := validate.DefaultOptions()
	options.MaxDiagnostics = 0
	report = validate.Document(context.Background(), invalidSemanticDocument(t), options)
	if diagnostics := report.Diagnostics(); len(diagnostics) != 1 ||
		diagnostics[0].Code != validate.CodeInvalidOptions {
		t.Fatalf("zero diagnostic options = %#v", diagnostics)
	}
}

func TestValidationChecksNormativeEmailAndURIFormats(t *testing.T) {
	t.Parallel()

	email := "not-an-email"
	contactURL := "relative/contact"
	contact, err := openrpc.NewContact(openrpc.ContactInput{Email: &email, URL: &contactURL})
	if err != nil {
		t.Fatal(err)
	}
	terms := "relative/terms"
	licenseURL := "relative/license"
	license, err := openrpc.NewLicense(openrpc.LicenseInput{URL: &licenseURL})
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{
		Title: "Formats", Version: "1", Contact: &contact,
		TermsOfService: &terms, License: &license,
	})
	if err != nil {
		t.Fatal(err)
	}
	docs, err := openrpc.NewExternalDocumentation(openrpc.ExternalDocumentationInput{URL: "relative/docs"})
	if err != nil {
		t.Fatal(err)
	}
	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	document, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version: version, Info: &info, ExternalDocs: &docs,
		Methods: []openrpc.MethodOrReference{},
	})
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := validate.Document(context.Background(), document, validate.DefaultOptions()).Diagnostics()
	wantPointers := []string{
		"#/info/contact/email",
		"#/info/contact/url",
		"#/info/termsOfService",
		"#/info/license/url",
		"#/externalDocs/url",
	}
	if len(diagnostics) != len(wantPointers) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for index, pointer := range wantPointers {
		if diagnostics[index].Code != validate.CodeInvalidFormat || diagnostics[index].Pointer != pointer {
			t.Fatalf("diagnostics[%d] = %#v", index, diagnostics[index])
		}
	}
}

func TestValidationChecksExternalDocumentationAtEveryInlineLocation(t *testing.T) {
	t.Parallel()

	docs, err := openrpc.NewExternalDocumentation(openrpc.ExternalDocumentationInput{URL: "relative/docs"})
	if err != nil {
		t.Fatal(err)
	}
	methodTag, err := openrpc.NewTag(openrpc.TagInput{Name: "method", ExternalDocs: &docs})
	if err != nil {
		t.Fatal(err)
	}
	componentTag, err := openrpc.NewTag(openrpc.TagInput{Name: "component", ExternalDocs: &docs})
	if err != nil {
		t.Fatal(err)
	}
	method, err := openrpc.NewMethod(openrpc.MethodInput{
		Name:         "documented",
		Params:       []openrpc.ContentDescriptorOrReference{},
		Tags:         []openrpc.TagOrReference{openrpc.TagValue(methodTag)},
		ExternalDocs: &docs,
	})
	if err != nil {
		t.Fatal(err)
	}
	components, err := openrpc.NewComponents(openrpc.ComponentsInput{
		Tags: map[string]openrpc.Tag{"tag/name": componentTag},
	})
	if err != nil {
		t.Fatal(err)
	}
	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{Title: "Formats", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version:    version,
		Info:       &info,
		Methods:    []openrpc.MethodOrReference{openrpc.MethodValue(method)},
		Components: &components,
	})
	if err != nil {
		t.Fatal(err)
	}

	diagnostics := validate.Document(context.Background(), document, validate.DefaultOptions()).Diagnostics()
	want := []string{
		"#/methods/0/externalDocs/url",
		"#/methods/0/tags/0/externalDocs/url",
		"#/components/tags/tag~1name/externalDocs/url",
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for index, pointer := range want {
		if diagnostics[index].Code != validate.CodeInvalidFormat || diagnostics[index].Pointer != pointer {
			t.Fatalf("diagnostics[%d] = %#v", index, diagnostics[index])
		}
	}
}

func TestResolvedDocumentAppliesSemanticRulesThroughReferences(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Resolved","version":"1"},
		"methods":[
			{"$ref":"#/x-method"},
			{"name":"same","params":[]}
		],
		"x-method":{
			"name":"same",
			"params":[
				{"$ref":"#/components/contentDescriptors/value"},
				{"name":"value","schema":true,"required":true}
			],
			"errors":[
				{"$ref":"#/components/errors/problem"},
				{"code":1,"message":"duplicate"}
			],
			"links":[{"method":"same"}]
		},
		"components":{
			"contentDescriptors":{"value":{"name":"value","schema":true}},
			"errors":{"problem":{"code":1,"message":"problem"}}
		}
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	report := validate.ResolvedDocument(
		context.Background(), parsed.Document(),
		"https://example.com/openrpc.json", resolver,
		validate.DefaultResolvedOptions(),
	)
	wantCodes := []validate.Code{
		validate.CodeDuplicateMethodName,
		validate.CodeDuplicateParameterName,
		validate.CodeRequiredParameterOrder,
		validate.CodeDuplicateErrorCode,
		validate.CodeUnresolvedLinkMethod,
	}
	diagnostics := report.Diagnostics()
	if len(diagnostics) != len(wantCodes) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for index, code := range wantCodes {
		if diagnostics[index].Code != code {
			t.Fatalf("diagnostics[%d] = %#v", index, diagnostics[index])
		}
	}
}

func TestResolvedDocumentReportsResolutionFailureWithoutPayloads(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Resolved","version":"1"},
		"methods":[{"$ref":"missing.json#/method"}]
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	report := validate.ResolvedDocument(
		context.Background(), parsed.Document(),
		"https://example.com/openrpc.json", resolver,
		validate.DefaultResolvedOptions(),
	)
	diagnostics := report.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != validate.CodeReferenceResolution ||
		diagnostics[0].Pointer != "#/methods/0" || diagnostics[0].Message != "reference resolution failed" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestResolvedDocumentRejectsTargetsWithInvalidObjectShape(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Resolved","version":"1"},
		"methods":[{"$ref":"#/x-invalid"}],
		"x-invalid":true
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	report := validate.ResolvedDocument(
		context.Background(), parsed.Document(),
		"https://example.com/openrpc.json", resolver,
		validate.DefaultResolvedOptions(),
	)
	diagnostics := report.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != validate.CodeInvalidResolvedDocument ||
		diagnostics[0].Pointer != "#" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestResolvedDocumentPreservesRecursiveJSONSchemaReferences(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Recursive","version":"1"},
		"methods":[{
			"name":"walk",
			"params":[{"name":"node","schema":{"$ref":"#/components/schemas/Node"}}]
		}],
		"components":{"schemas":{"Node":{
			"type":"object",
			"properties":{"child":{"$ref":"#/components/schemas/Node"}}
		}}}
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	report := validate.ResolvedDocument(
		context.Background(), parsed.Document(),
		"https://example.com/openrpc.json", resolver,
		validate.DefaultResolvedOptions(),
	)
	if !report.Valid() {
		t.Fatalf("diagnostics = %#v", report.Diagnostics())
	}
}

func TestDocumentCompilesEveryDraft7SchemaObject(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Schemas","version":"1"},
		"methods":[{
			"name":"invalid",
			"params":[{"name":"input","schema":{"type":42}}],
			"result":{"name":"output","schema":{"type":42}}
		}],
		"components":{
			"schemas":{"Invalid":{"type":42}},
			"contentDescriptors":{"Invalid":{"name":"value","schema":{"type":42}}}
		}
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := validate.Document(
		context.Background(), parsed.Document(), validate.DefaultOptions(),
	).Diagnostics()
	want := []string{
		"#/components/schemas/Invalid",
		"#/components/contentDescriptors/Invalid/schema",
		"#/methods/0/params/0/schema",
		"#/methods/0/result/schema",
	}
	if len(diagnostics) != len(want) {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	for index, pointer := range want {
		if diagnostics[index].Code != validate.CodeInvalidSchema || diagnostics[index].Pointer != pointer {
			t.Fatalf("diagnostics[%d] = %#v", index, diagnostics[index])
		}
	}
}

func TestDocumentWarnsWhenErrorMessagesAreNotConciseSentences(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Errors","version":"1"},
		"methods":[{
			"name":"fail",
			"params":[],
			"errors":[{"code":1,"message":"First sentence. Second sentence."}]
		}],
		"components":{"errors":{"Shared":{
			"code":2,"message":"First line.\nSecond line."
		}}}
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	report := validate.Document(
		context.Background(), parsed.Document(), validate.DefaultOptions(),
	)
	diagnostics := report.Diagnostics()
	if len(diagnostics) != 2 || diagnostics[0].Code != validate.CodeErrorMessageNotConcise ||
		diagnostics[0].Severity != validate.SeverityWarning ||
		diagnostics[0].Pointer != "#/methods/0/errors/0/message" || !report.Valid() {
		t.Fatalf("report = %#v", diagnostics)
	}
	if diagnostics[1].Code != validate.CodeErrorMessageNotConcise ||
		diagnostics[1].Pointer != "#/components/errors/Shared/message" {
		t.Fatalf("report = %#v", diagnostics)
	}
}

func TestDocumentAllowsSchemaReferencesThatRequireExplicitExternalResolution(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"External schema","version":"1"},
		"methods":[{
			"name":"read",
			"params":[{"name":"input","schema":{"$ref":"schemas.json#/Input"}}]
		}]
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	report := validate.Document(
		context.Background(), parsed.Document(), validate.DefaultOptions(),
	)
	if !report.Valid() {
		t.Fatalf("diagnostics = %#v", report.Diagnostics())
	}
}

func TestResolvedDocumentCompilesExplicitExternalSchemaResources(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"External schema","version":"1"},
		"methods":[{
			"name":"read",
			"params":[{"name":"input","schema":{"$ref":"schemas.json#/definitions/Input"}}]
		}]
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	store, err := reference.NewMemoryStore(map[string][]byte{
		"https://example.com/schemas.json": []byte(`{
			"$schema":"http://json-schema.org/draft-07/schema#",
			"definitions":{"Input":{"$ref":"types.json#/definitions/Text"}}
		}`),
		"https://example.com/types.json": []byte(`{
			"$schema":"http://json-schema.org/draft-07/schema#",
			"definitions":{"Text":{"type":42}}
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	policy := reference.DefaultResolvePolicy()
	policy.AllowExternal = true
	policy.AllowedSchemes = []string{"https"}
	policy.AllowedHosts = []string{"example.com"}
	resolver, err := reference.NewResolver(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	report := validate.ResolvedDocument(
		context.Background(), parsed.Document(),
		"https://example.com/openrpc.json", resolver,
		validate.DefaultResolvedOptions(),
	)
	diagnostics := report.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != validate.CodeInvalidSchema ||
		diagnostics[0].Pointer != "#/methods/0/params/0/schema" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestResolvedDocumentAcceptsRecursiveExternalSchemaResources(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"External schema","version":"1"},
		"methods":[{
			"name":"read",
			"params":[{"name":"input","schema":{"$ref":"schemas.json#/definitions/Node"}}]
		}]
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	store, err := reference.NewMemoryStore(map[string][]byte{
		"https://example.com/schemas.json": []byte(`{
			"$schema":"http://json-schema.org/draft-07/schema#",
			"definitions":{"Node":{
				"type":"object",
				"properties":{"child":{"$ref":"#/definitions/Node"}}
			}}
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	policy := reference.DefaultResolvePolicy()
	policy.AllowExternal = true
	policy.AllowedSchemes = []string{"https"}
	policy.AllowedHosts = []string{"example.com"}
	resolver, err := reference.NewResolver(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	report := validate.ResolvedDocument(
		context.Background(), parsed.Document(),
		"https://example.com/openrpc.json", resolver,
		validate.DefaultResolvedOptions(),
	)
	if !report.Valid() {
		t.Fatalf("diagnostics = %#v", report.Diagnostics())
	}
}

func TestResolvedDocumentUsesNestedSchemaIDsAsReferenceBases(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Nested schema ID","version":"1"},
		"methods":[{
			"name":"read",
			"params":[{"name":"input","schema":{
				"$id":"https://schemas.example/nested/root.json",
				"properties":{"value":{"$ref":"child.json"}}
			}}]
		}]
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	store, err := reference.NewMemoryStore(map[string][]byte{
		"https://schemas.example/nested/child.json": []byte(`{"type":42}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	policy := reference.DefaultResolvePolicy()
	policy.AllowExternal = true
	policy.AllowedSchemes = []string{"https"}
	policy.AllowedHosts = []string{"schemas.example"}
	resolver, err := reference.NewResolver(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	report := validate.ResolvedDocument(
		context.Background(), parsed.Document(),
		"https://example.com/openrpc.json", resolver,
		validate.DefaultResolvedOptions(),
	)
	diagnostics := report.Diagnostics()
	if len(diagnostics) != 1 || diagnostics[0].Code != validate.CodeInvalidSchema ||
		diagnostics[0].Pointer != "#/methods/0/params/0/schema" {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestResolvedDocumentDoesNotResolveReferencesInsideSchemaAnnotations(t *testing.T) {
	t.Parallel()

	parsed, err := openrpcparse.Decode([]byte(`{
		"openrpc":"1.4.1",
		"info":{"title":"Schema annotations","version":"1"},
		"methods":[{
			"name":"read",
			"params":[{"name":"input","schema":{
				"type":"object",
				"examples":[{"$ref":"must-not-be-loaded.json"}]
			}}]
		}]
	}`), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	store, err := reference.NewMemoryStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	policy := reference.DefaultResolvePolicy()
	policy.AllowExternal = true
	policy.AllowedSchemes = []string{"https"}
	policy.AllowedHosts = []string{"example.com"}
	resolver, err := reference.NewResolver(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	report := validate.ResolvedDocument(
		context.Background(), parsed.Document(),
		"https://example.com/openrpc.json", resolver,
		validate.DefaultResolvedOptions(),
	)
	if !report.Valid() {
		t.Fatalf("diagnostics = %#v", report.Diagnostics())
	}
}

func invalidSemanticDocument(t *testing.T) openrpc.Document {
	t.Helper()

	schema, err := jsonschema.Parse([]byte(`true`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	optional := false
	required := true
	first, err := openrpc.NewContentDescriptor(openrpc.ContentDescriptorInput{
		Name: "value", Schema: &schema, Required: &optional,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := openrpc.NewContentDescriptor(openrpc.ContentDescriptorInput{
		Name: "value", Schema: &schema, Required: &required,
	})
	if err != nil {
		t.Fatal(err)
	}
	code, err := openrpc.ParseInteger("1")
	if err != nil {
		t.Fatal(err)
	}
	firstError, err := openrpc.NewError(openrpc.ErrorInput{Code: code, Message: "first"})
	if err != nil {
		t.Fatal(err)
	}
	secondError, err := openrpc.NewError(openrpc.ErrorInput{Code: code, Message: "second"})
	if err != nil {
		t.Fatal(err)
	}
	target := "missing"
	link, err := openrpc.NewLink(openrpc.LinkInput{Method: &target})
	if err != nil {
		t.Fatal(err)
	}
	firstMethod, err := openrpc.NewMethod(openrpc.MethodInput{
		Name: "duplicate",
		Params: []openrpc.ContentDescriptorOrReference{
			openrpc.ContentDescriptorValue(first),
			openrpc.ContentDescriptorValue(second),
		},
		Errors: []openrpc.ErrorOrReference{
			openrpc.ErrorValue(firstError),
			openrpc.ErrorValue(secondError),
		},
		Links: []openrpc.LinkOrReference{openrpc.LinkValue(link)},
	})
	if err != nil {
		t.Fatal(err)
	}
	secondMethod, err := openrpc.NewMethod(openrpc.MethodInput{
		Name:   "duplicate",
		Params: []openrpc.ContentDescriptorOrReference{},
	})
	if err != nil {
		t.Fatal(err)
	}
	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{Title: "Invalid", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := openrpc.NewDocument(openrpc.DocumentInput{
		Version: version,
		Info:    &info,
		Methods: []openrpc.MethodOrReference{
			openrpc.MethodValue(firstMethod),
			openrpc.MethodValue(secondMethod),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return document
}
