package diff_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/diff"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
	"github.com/faustbrian/golib/pkg/openrpc/reference"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

func TestCompareClassifiesMethodAndParameterCompatibility(t *testing.T) {
	t.Parallel()

	before := document(t,
		method(t, "removed", openrpc.ParamStructureByName, descriptor(t, "id", true)),
		method(t, "changed", openrpc.ParamStructureByName, descriptor(t, "value", false)),
	)
	after := document(t,
		method(t, "added", openrpc.ParamStructureByName),
		method(t, "changed", openrpc.ParamStructureByName,
			descriptor(t, "value", false),
			descriptor(t, "required", true),
		),
	)
	report := diff.Compare(context.Background(), before, after, diff.DefaultOptions())
	want := []diff.Change{
		{Code: diff.CodeMethodAdded, Classification: diff.Additive, Pointer: "#/methods/added", Message: "method was added"},
		{Code: diff.CodeParameterAddedRequired, Classification: diff.Breaking, Pointer: "#/methods/changed/params/required", Message: "required parameter was added"},
		{Code: diff.CodeMethodRemoved, Classification: diff.Breaking, Pointer: "#/methods/removed", Message: "method was removed"},
	}
	if !reflect.DeepEqual(report.Changes(), want) {
		t.Fatalf("changes = %#v, want %#v", report.Changes(), want)
	}
	if report.Compatible() {
		t.Fatal("breaking report was compatible")
	}
}

func TestCompareHonorsByPositionOrdering(t *testing.T) {
	t.Parallel()

	before := document(t, method(t, "ordered", openrpc.ParamStructureByPosition,
		descriptor(t, "first", true), descriptor(t, "second", true),
	))
	after := document(t, method(t, "ordered", openrpc.ParamStructureByPosition,
		descriptor(t, "second", true), descriptor(t, "first", true),
	))
	changes := diff.Compare(context.Background(), before, after, diff.DefaultOptions()).Changes()
	if len(changes) != 1 || changes[0].Code != diff.CodeParameterOrderChanged || changes[0].Classification != diff.Breaking {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestCompareIgnoresJSONSchemaObjectOrdering(t *testing.T) {
	t.Parallel()

	before := document(t, method(t, "same", openrpc.ParamStructureByName,
		descriptorSchema(t, "value", `{"type":"string","minLength":1}`),
	))
	after := document(t, method(t, "same", openrpc.ParamStructureByName,
		descriptorSchema(t, "value", `{ "minLength": 1, "type": "string" }`),
	))
	if changes := diff.Compare(context.Background(), before, after, diff.DefaultOptions()).Changes(); len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestCompareClassifiesResultsErrorsServersLinksExamplesAndComponents(t *testing.T) {
	t.Parallel()

	before := parsedDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"Diff","version":"1"},
		"servers":[{"url":"https://old.example"}],
		"methods":[{
			"name":"read","params":[],
			"result":{"name":"result","schema":{"type":"string"}},
			"errors":[{"code":1,"message":"old"}],
			"links":[{"method":"read"}],
			"examples":[{"name":"old","params":[]}]
		}],
		"components":{
			"schemas":{"Value":{"type":"string"}},
			"tags":{"Old":{"name":"old"}}
		}
	}`)
	after := parsedDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"Diff","version":"1"},
		"servers":[{"url":"https://new.example"}],
		"methods":[{
			"name":"read","params":[],
			"errors":[{"code":2,"message":"new"}],
			"links":[{"method":"other"}],
			"examples":[]
		}],
		"components":{
			"schemas":{"Value":{"type":"integer"}},
			"examples":{"New":{"name":"new","value":1}}
		}
	}`)
	report := diff.Compare(context.Background(), before, after, diff.DefaultOptions())
	seen := make(map[diff.Code]bool)
	for _, change := range report.Changes() {
		seen[change.Code] = true
	}
	for _, code := range []diff.Code{
		diff.CodeResultRemoved,
		diff.CodeErrorsChanged,
		diff.CodeServersChanged,
		diff.CodeLinksChanged,
		diff.CodeExamplesChanged,
		diff.CodeSchemaChanged,
		diff.CodeComponentRemoved,
		diff.CodeComponentAdded,
	} {
		if !seen[code] {
			t.Fatalf("changes omitted %s: %#v", code, report.Changes())
		}
	}
}

func TestCompareIgnoresOrderingOfUnorderedMethodCollections(t *testing.T) {
	t.Parallel()

	before := parsedDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"Diff","version":"1"},
		"methods":[{"name":"read","params":[],
			"errors":[{"code":1,"message":"one"},{"code":2,"message":"two"}],
			"links":[{"method":"one"},{"method":"two"}],
			"examples":[{"name":"one","params":[]},{"name":"two","params":[]}]
		}]
	}`)
	after := parsedDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"Diff","version":"1"},
		"methods":[{"name":"read","params":[],
			"errors":[{"code":2,"message":"two"},{"code":1,"message":"one"}],
			"links":[{"method":"two"},{"method":"one"}],
			"examples":[{"name":"two","params":[]},{"name":"one","params":[]}]
		}]
	}`)
	if changes := diff.Compare(
		context.Background(), before, after, diff.DefaultOptions(),
	).Changes(); len(changes) != 0 {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestCompareResolvedUsesReferenceTargetSemantics(t *testing.T) {
	t.Parallel()

	before := parsedDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"Diff","version":"1"},
		"methods":[{"$ref":"#/x-method"}],
		"x-method":{"name":"read","params":[],
			"result":{"name":"result","schema":{"type":"string"}}}
	}`)
	after := parsedDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"Diff","version":"1"},
		"methods":[{"$ref":"#/x-method"}],
		"x-method":{"name":"read","params":[],
			"result":{"name":"result","schema":{"type":"integer"}}}
	}`)
	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	report := diff.CompareResolved(
		context.Background(), before, after,
		"https://example.com/before.json", "https://example.com/after.json",
		resolver, validate.DefaultResolvedOptions(), diff.DefaultOptions(),
	)
	changes := report.Changes()
	if len(changes) != 1 || changes[0].Code != diff.CodeResultChanged ||
		changes[0].Pointer != "#/methods/read/result" {
		t.Fatalf("changes = %#v, error = %v", changes, report.Err())
	}
}

func TestCompareResolvedPreservesUnresolvedSourcePointers(t *testing.T) {
	t.Parallel()

	before := parsedDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"Diff","version":"1"},
		"methods":[{"$ref":"missing.json#/method"}]
	}`)
	after := parsedDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"Diff","version":"1"},
		"methods":[]
	}`)
	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	report := diff.CompareResolved(
		context.Background(), before, after,
		"https://example.com/before.json", "https://example.com/after.json",
		resolver, validate.DefaultResolvedOptions(), diff.DefaultOptions(),
	)
	changes := report.Changes()
	if len(changes) != 1 || changes[0].Code != diff.CodeUnresolvedReference ||
		changes[0].Pointer != "#/methods/0" || report.Err() != nil {
		t.Fatalf("changes = %#v, error = %v", changes, report.Err())
	}
}

func TestCompareBoundsChangesAndCancellation(t *testing.T) {
	t.Parallel()

	before := document(t, method(t, "one", openrpc.ParamStructureByName), method(t, "two", openrpc.ParamStructureByName))
	after := document(t)
	options := diff.DefaultOptions()
	options.MaxChanges = 1
	report := diff.Compare(context.Background(), before, after, options)
	if len(report.Changes()) != 1 || !report.Truncated() {
		t.Fatalf("report = %#v, truncated = %t", report.Changes(), report.Truncated())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report = diff.Compare(ctx, before, after, diff.DefaultOptions())
	if !errors.Is(report.Err(), context.Canceled) {
		t.Fatalf("cancellation error = %v", report.Err())
	}
	report = diff.Compare(context.Background(), before, after, diff.Options{})
	if !errors.Is(report.Err(), diff.ErrInvalidOptions) {
		t.Fatalf("options error = %v", report.Err())
	}
}

func document(t *testing.T, methods ...openrpc.Method) openrpc.Document {
	t.Helper()
	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	info, err := openrpc.NewInfo(openrpc.InfoInput{Title: "Diff", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	unions := make([]openrpc.MethodOrReference, len(methods))
	for index, method := range methods {
		unions[index] = openrpc.MethodValue(method)
	}
	document, err := openrpc.NewDocument(openrpc.DocumentInput{Version: version, Info: &info, Methods: unions})
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func method(t *testing.T, name string, structure openrpc.ParamStructure, params ...openrpc.ContentDescriptor) openrpc.Method {
	t.Helper()
	unions := make([]openrpc.ContentDescriptorOrReference, len(params))
	for index, param := range params {
		unions[index] = openrpc.ContentDescriptorValue(param)
	}
	method, err := openrpc.NewMethod(openrpc.MethodInput{Name: name, Params: unions, ParamStructure: &structure})
	if err != nil {
		t.Fatal(err)
	}
	return method
}

func descriptor(t *testing.T, name string, required bool) openrpc.ContentDescriptor {
	t.Helper()
	schema, err := jsonschema.Parse([]byte(`{"type":"string"}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := openrpc.NewContentDescriptor(openrpc.ContentDescriptorInput{
		Name: name, Schema: &schema, Required: &required,
	})
	if err != nil {
		t.Fatal(err)
	}
	return descriptor
}

func descriptorSchema(t *testing.T, name string, source string) openrpc.ContentDescriptor {
	t.Helper()
	schema, err := jsonschema.Parse([]byte(source), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := openrpc.NewContentDescriptor(openrpc.ContentDescriptorInput{Name: name, Schema: &schema})
	if err != nil {
		t.Fatal(err)
	}
	return descriptor
}

func parsedDocument(t *testing.T, source string) openrpc.Document {
	t.Helper()
	result, err := openrpcparse.Decode([]byte(source), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	return result.Document()
}
