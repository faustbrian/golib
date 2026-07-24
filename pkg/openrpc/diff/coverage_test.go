package diff

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
	"github.com/faustbrian/golib/pkg/openrpc/reference"
	"github.com/faustbrian/golib/pkg/openrpc/validate"
)

func TestReportCompatibilityAndHelperFailures(t *testing.T) {
	t.Parallel()

	if !(Report{}).Compatible() {
		t.Fatal("empty report is incompatible")
	}
	if (Report{changes: []Change{{Classification: Conditional}}}).Compatible() {
		t.Fatal("conditional report was compatible")
	}
	if (Report{truncated: true}).Compatible() {
		t.Fatal("truncated report was compatible")
	}
	if (Report{err: errors.New("failed")}).Compatible() {
		t.Fatal("error report is compatible")
	}
	if (Report{changes: []Change{{Classification: Breaking}}}).Compatible() {
		t.Fatal("breaking report is compatible")
	}
	if sameJSONMultiset([]byte(`{}`), []byte(`[]`)) {
		t.Fatal("malformed collection compared equal")
	}
	if canonicalJSON([]byte(`{`)) != nil {
		t.Fatal("malformed JSON was canonicalized")
	}
	if rawDocument(openrpc.Document{}) != nil {
		t.Fatal("zero document produced raw JSON")
	}
	if sameParameterOrder(nil, []openrpc.ContentDescriptorOrReference{{}}) {
		t.Fatal("different parameter lengths compared equal")
	}
	if sameParameterOrder([]openrpc.ContentDescriptorOrReference{{}}, []openrpc.ContentDescriptorOrReference{{}}) {
		t.Fatal("references compared as ordered descriptors")
	}
}

func TestCompareCoversExactBoundsAndLoopCancellation(t *testing.T) {
	t.Parallel()

	before := diffDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"one","params":[]}]}`)
	after := diffDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"two","params":[]}],"components":{"schemas":{"one":true}}}`)
	emptyDocument := diffDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[]}`)
	for _, invalid := range []Options{
		{MaxChanges: 0, MaxMethods: 2, MaxComponents: 2},
		{MaxChanges: 2, MaxMethods: 0, MaxComponents: 2},
		{MaxChanges: 2, MaxMethods: 2, MaxComponents: 0},
	} {
		if report := Compare(context.Background(), emptyDocument, emptyDocument, invalid); !errors.Is(report.Err(), ErrInvalidOptions) {
			t.Errorf("invalid options %+v error = %v", invalid, report.Err())
		}
	}
	options := DefaultOptions()
	options.MaxMethods = 2
	options.MaxComponents = 1
	if report := Compare(context.Background(), before, after, options); report.Err() != nil {
		t.Fatalf("exact bounds error = %v", report.Err())
	}
	options.MaxMethods = 1
	if report := Compare(context.Background(), before, after, options); !errors.Is(report.Err(), ErrInvalidOptions) {
		t.Fatalf("method bound error = %v", report.Err())
	}
	options = DefaultOptions()
	options.MaxComponents = 0
	options.MaxMethods = 1
	options.MaxChanges = 1
	if report := Compare(context.Background(), before, before, options); !errors.Is(report.Err(), ErrInvalidOptions) {
		t.Fatalf("component option error = %v", report.Err())
	}
	ctx := &diffCountingContext{Context: context.Background(), remaining: 1}
	if report := Compare(ctx, before, after, DefaultOptions()); !errors.Is(report.Err(), context.Canceled) {
		t.Fatalf("loop cancellation error = %v", report.Err())
	}
	twoComponents := diffDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[],"components":{"schemas":{"one":true,"two":false}}}`)
	options = DefaultOptions()
	options.MaxComponents = 1
	if report := Compare(context.Background(), before, twoComponents, options); !errors.Is(report.Err(), ErrInvalidOptions) {
		t.Fatalf("component bound error = %v", report.Err())
	}
	withReferences := diffDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"$ref":"#/one"},{"$ref":"#/two"}]}`)
	if report := Compare(context.Background(), withReferences, emptyDocument, DefaultOptions()); len(report.Changes()) != 2 {
		t.Fatalf("method reference changes = %#v", report.Changes())
	}
	options = DefaultOptions()
	options.MaxChanges = 2
	if report := Compare(context.Background(), withReferences, emptyDocument, options); report.Truncated() || len(report.Changes()) != 2 {
		t.Fatalf("exact change limit report = %#v", report)
	}
	if report := Compare(context.Background(), emptyDocument, withReferences, DefaultOptions()); len(report.Changes()) != 2 {
		t.Fatalf("after reference changes = %#v", report.Changes())
	}
	mixed := diffDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"one","params":[]},{"$ref":"#/one"}]}`)
	options = DefaultOptions()
	options.MaxMethods = 3
	if report := Compare(context.Background(), mixed, mixed, options); !errors.Is(report.Err(), ErrInvalidOptions) {
		t.Fatalf("combined method bound error = %v", report.Err())
	}
}

func TestSurfaceHelpersCoverEveryChangeShape(t *testing.T) {
	t.Parallel()

	before := map[string]json.RawMessage{
		"result":   json.RawMessage(`true`),
		"servers":  json.RawMessage(`[]`),
		"errors":   json.RawMessage(`[]`),
		"links":    json.RawMessage(`[]`),
		"examples": json.RawMessage(`[]`),
	}
	after := map[string]json.RawMessage{
		"result":   json.RawMessage(`false`),
		"servers":  json.RawMessage(`[{}]`),
		"errors":   json.RawMessage(`[1]`),
		"links":    json.RawMessage(`[1]`),
		"examples": json.RawMessage(`[1]`),
	}
	if changes := compareMethodSurfaces("#/methods/x", before, after); len(changes) != 5 {
		t.Fatalf("surface changes = %#v", changes)
	}
	if changes := compareMethodSurfaces("#/methods/x", nil, map[string]json.RawMessage{"result": json.RawMessage(`true`)}); len(changes) != 1 || changes[0].Code != CodeResultAdded {
		t.Fatalf("added result changes = %#v", changes)
	}
	if changes := compareMethodSurfaces("#/methods/x", before, nil); len(changes) == 0 || changes[0].Code != CodeResultRemoved {
		t.Fatalf("removed result changes = %#v", changes)
	}
	if changes := appendFieldChange(nil, nil, map[string]json.RawMessage{"field": json.RawMessage(`1`)}, "field", CodeServersChanged, Breaking, "#", "changed"); len(changes) != 1 {
		t.Fatalf("field presence changes = %#v", changes)
	}
}

func TestParameterHelpersCoverRequiredSchemaAndReferences(t *testing.T) {
	t.Parallel()

	schema, err := jsonschema.Parse([]byte(`true`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	otherSchema, err := jsonschema.Parse([]byte(`false`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	optional, err := openrpc.NewContentDescriptor(openrpc.ContentDescriptorInput{Name: "value", Schema: &schema})
	if err != nil {
		t.Fatal(err)
	}
	required := true
	requiredValue, err := openrpc.NewContentDescriptor(openrpc.ContentDescriptorInput{Name: "value", Schema: &otherSchema, Required: &required})
	if err != nil {
		t.Fatal(err)
	}
	before, err := openrpc.NewMethod(openrpc.MethodInput{Name: "m", Params: []openrpc.ContentDescriptorOrReference{openrpc.ContentDescriptorValue(optional)}})
	if err != nil {
		t.Fatal(err)
	}
	after, err := openrpc.NewMethod(openrpc.MethodInput{Name: "m", Params: []openrpc.ContentDescriptorOrReference{openrpc.ContentDescriptorValue(requiredValue)}})
	if err != nil {
		t.Fatal(err)
	}
	changes := compareParameters("#/methods/m", before, after)
	if len(changes) != 2 {
		t.Fatalf("parameter changes = %#v", changes)
	}
	referenceValue, err := openrpc.NewReference("#/components/contentDescriptors/value")
	if err != nil {
		t.Fatal(err)
	}
	withReference, err := openrpc.NewMethod(openrpc.MethodInput{Name: "m", Params: []openrpc.ContentDescriptorOrReference{openrpc.ContentDescriptorReference(referenceValue)}})
	if err != nil {
		t.Fatal(err)
	}
	if changes := compareParameters("#/methods/m", withReference, withReference); len(changes) != 2 {
		t.Fatalf("reference changes = %#v", changes)
	}
	if !sameParameterOrder(before.Params(), before.Params()) {
		t.Fatal("identical parameter order differed")
	}
	empty, err := openrpc.NewMethod(openrpc.MethodInput{Name: "m", Params: []openrpc.ContentDescriptorOrReference{}})
	if err != nil {
		t.Fatal(err)
	}
	if changes := compareParameters("#/methods/m", empty, before); len(changes) != 1 || changes[0].Code != CodeParameterAddedOptional {
		t.Fatalf("optional addition changes = %#v", changes)
	}
	if changes := compareParameters("#/methods/m", before, empty); len(changes) != 1 || changes[0].Code != CodeParameterRemoved {
		t.Fatalf("parameter removal changes = %#v", changes)
	}
}

func TestResolvedComparisonCoversInvalidOptionsAndTruncation(t *testing.T) {
	t.Parallel()

	document := diffDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"$ref":"missing.json#/one"},{"$ref":"missing.json#/two"}]}`)
	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	resolvedOptions := validate.DefaultResolvedOptions()
	for _, invalidOptions := range []Options{
		{MaxChanges: 0, MaxMethods: 1, MaxComponents: 1},
		{MaxChanges: 1, MaxMethods: 0, MaxComponents: 1},
		{MaxChanges: 1, MaxMethods: 1, MaxComponents: 0},
	} {
		if report := CompareResolved(context.Background(), document, document, "memory:/root.json", "memory:/root.json", resolver, resolvedOptions, invalidOptions); !errors.Is(report.Err(), ErrInvalidOptions) {
			t.Fatalf("invalid resolved options error = %v", report.Err())
		}
	}
	options := DefaultOptions()
	options.MaxChanges = 1
	afterDocument := diffDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"valid","params":[]},{"$ref":"missing.json#/two"}]}`)
	report := CompareResolved(context.Background(), document, afterDocument, "memory:/root.json", "memory:/root.json", resolver, resolvedOptions, options)
	if !report.Truncated() || len(report.Changes()) != 1 {
		t.Fatalf("resolved report = %#v", report)
	}
	options.MaxChanges = 2
	report = CompareResolved(context.Background(), document, afterDocument, "memory:/root.json", "memory:/root.json", resolver, resolvedOptions, options)
	if report.Truncated() || len(report.Changes()) != 2 {
		t.Fatalf("exact unresolved limit report = %#v", report)
	}
	if report.Changes()[0].Pointer >= report.Changes()[1].Pointer {
		t.Fatalf("unresolved changes are not sorted: %#v", report.Changes())
	}
	invalid := diffDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[{"name":"same","params":[]},{"name":"same","params":[]}]}`)
	report = CompareResolved(context.Background(), invalid, invalid, "memory:/root.json", "memory:/root.json", resolver, resolvedOptions, DefaultOptions())
	if !errors.Is(report.Err(), ErrResolvedComparison) {
		t.Fatalf("invalid resolved document error = %v", report.Err())
	}
	validation := validate.Document(context.Background(), invalid, validate.DefaultOptions())
	if changes := resolutionChanges("before", validation); len(changes) != 0 {
		t.Fatalf("non-resolution diagnostics became changes: %#v", changes)
	}
}

type diffCountingContext struct {
	context.Context
	remaining int
}

func (ctx *diffCountingContext) Err() error {
	ctx.remaining--
	if ctx.remaining < 0 {
		return context.Canceled
	}
	return nil
}

func diffDocument(t *testing.T, source string) openrpc.Document {
	t.Helper()
	parsed, err := openrpcparse.Decode([]byte(source), openrpcparse.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Document()
}
