package compose

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
	openrpcparse "github.com/faustbrian/golib/pkg/openrpc/parse"
)

func TestFilterCopiesOptionalMetadataAndSanitizesFailures(t *testing.T) {
	t.Parallel()

	document := composeDocument(t, `{
		"$schema":"https://spec.open-rpc.org/meta-schema",
		"openrpc":"1.4.1",
		"info":{"title":"Rich","version":"1"},
		"externalDocs":{"url":"https://example.com"},
		"servers":[{"url":"https://example.com"}],
		"methods":[{"name":"one","params":[]}],
		"components":{"schemas":{}}
	}`)
	predicate := MethodPredicateFunc(func(context.Context, openrpc.Method) (bool, error) {
		return true, nil
	})
	if _, err := FilterMethods(context.Background(), document, predicate, DefaultFilterOptions()); err != nil {
		t.Fatal(err)
	}
	options := DefaultFilterOptions()
	options.MaxMethods = 1
	if _, err := FilterMethods(context.Background(), document, predicate, options); err != nil {
		t.Fatalf("exact method limit error = %v", err)
	}
	options.MaxMethods = 0
	if _, err := FilterMethods(context.Background(), document, predicate, options); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("zero method limit error = %v", err)
	}
	if _, err := FilterMethods(context.Background(), openrpc.Document{}, predicate, DefaultFilterOptions()); !errors.Is(err, ErrInvalidFilter) {
		t.Fatalf("zero document error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancelling := MethodPredicateFunc(func(context.Context, openrpc.Method) (bool, error) {
		cancel()
		return false, errors.New("private")
	})
	if _, err := FilterMethods(ctx, document, cancelling, DefaultFilterOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("policy cancellation error = %v", err)
	}
	ctx = &countingContext{Context: context.Background(), remaining: 1}
	if _, err := FilterMethods(ctx, document, predicate, DefaultFilterOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("filter loop cancellation error = %v", err)
	}
}

func TestMergeCoversReferencesMetadataAndEveryRegistryConflict(t *testing.T) {
	t.Parallel()

	rich := composeDocument(t, `{
		"$schema":"https://spec.open-rpc.org/meta-schema",
		"openrpc":"1.4.1","info":{"title":"Rich","version":"1"},
		"externalDocs":{"url":"https://example.com"},
		"servers":[],"methods":[{"$ref":"#/components/methods/a"}],
		"components":{"schemas":{}}
	}`)
	options := DefaultMergeOptions()
	options.MaxDocuments = 1
	options.MaxMethods = 1
	if _, err := Merge(context.Background(), []openrpc.Document{rich}, options); err != nil {
		t.Fatalf("exact merge limits error = %v", err)
	}
	for _, invalid := range []MergeOptions{
		{Conflict: ConflictError, MaxDocuments: 0, MaxMethods: 1, MaxComponents: 1},
		{Conflict: ConflictError, MaxDocuments: 1, MaxMethods: 0, MaxComponents: 1},
		{Conflict: ConflictError, MaxDocuments: 1, MaxMethods: 1, MaxComponents: 0},
	} {
		if _, err := Merge(context.Background(), []openrpc.Document{rich}, invalid); !errors.Is(err, ErrInvalidMerge) {
			t.Errorf("invalid merge options %+v error = %v", invalid, err)
		}
	}
	if _, err := Merge(context.Background(), []openrpc.Document{rich, rich}, DefaultMergeOptions()); !errors.Is(err, ErrMergeConflict) {
		t.Fatalf("reference conflict error = %v", err)
	}
	options = DefaultMergeOptions()
	options.Conflict = KeepFirst
	if _, err := Merge(context.Background(), []openrpc.Document{rich, rich}, options); err != nil {
		t.Fatal(err)
	}

	registries := []string{
		`"schemas":{"same":true}`,
		`"links":{"same":{}}`,
		`"errors":{"same":{"code":-32000,"message":"error"}}`,
		`"examples":{"same":{"name":"same","value":null}}`,
		`"examplePairings":{"same":{"name":"same","params":[]}}`,
		`"contentDescriptors":{"same":{"name":"same","schema":true}}`,
		`"tags":{"same":{"name":"same"}}`,
	}
	for _, registry := range registries {
		document := composeDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[],"components":{`+registry+`}}`)
		components, present := document.Components()
		if !present {
			t.Fatal("components absent")
		}
		accumulator := componentAccumulator{}
		if _, err := accumulator.merge(components, ConflictError); err != nil {
			t.Fatal(err)
		}
		if _, err := accumulator.merge(components, ConflictError); !errors.Is(err, ErrMergeConflict) {
			t.Errorf("registry %s conflict error = %v", registry, err)
		}
	}
	oneComponent := composeDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[],"components":{"schemas":{"one":true}}}`)
	options = DefaultMergeOptions()
	options.MaxComponents = 1
	if _, err := Merge(context.Background(), []openrpc.Document{oneComponent}, options); err != nil {
		t.Fatalf("exact component limit error = %v", err)
	}
}

func TestOverlayCoversInvalidValuesAndPostPatchLimits(t *testing.T) {
	t.Parallel()

	if _, err := NewOverlay([]byte(`{`), jsonvalue.DefaultPolicy()); !errors.Is(err, ErrInvalidOverlay) {
		t.Fatalf("malformed overlay error = %v", err)
	}
	if _, err := ApplyOverlays(context.Background(), openrpc.Document{}, nil, DefaultOverlayOptions()); !errors.Is(err, ErrOverlayResult) {
		t.Fatalf("zero document error = %v", err)
	}
	document := composeDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[]}`)
	zeroOutput := DefaultOverlayOptions()
	zeroOutput.MaxOutputBytes = 0
	if _, err := ApplyOverlays(context.Background(), document, nil, zeroOutput); !errors.Is(err, ErrInvalidOverlay) {
		t.Fatalf("zero output limit error = %v", err)
	}
	if _, err := ApplyOverlays(context.Background(), document, []Overlay{{}}, DefaultOverlayOptions()); !errors.Is(err, ErrInvalidOverlay) {
		t.Fatalf("zero overlay error = %v", err)
	}
	nonObject := Overlay{patch: composeValue(t, `[]`)}
	if _, err := ApplyOverlays(context.Background(), document, []Overlay{nonObject}, DefaultOverlayOptions()); !errors.Is(err, ErrInvalidOverlay) {
		t.Fatalf("non-object overlay error = %v", err)
	}
	patch, err := NewOverlay([]byte(`{"info":{"description":"long output"}}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	options := DefaultOverlayOptions()
	base, err := openrpc.MarshalCanonical(document)
	if err != nil {
		t.Fatal(err)
	}
	options.MaxOutputBytes = len(base)
	if _, err := ApplyOverlays(context.Background(), document, []Overlay{patch}, options); !errors.Is(err, ErrOverlayLimit) {
		t.Fatalf("post-patch output error = %v", err)
	}
	options = DefaultOverlayOptions()
	options.MaxActions = 1
	options.MaxOutputBytes = len(base)
	emptyPatch, err := NewOverlay([]byte(`{}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyOverlays(context.Background(), document, []Overlay{emptyPatch}, options); err != nil {
		t.Fatalf("exact overlay limits error = %v", err)
	}
	if _, err := decodeOverlayJSON([]byte(`{`)); err == nil {
		t.Fatal("malformed JSON decoded")
	}
	if _, err := decodeOverlayJSON([]byte(`{} {}`)); !errors.Is(err, ErrInvalidOverlay) {
		t.Fatalf("trailing JSON error = %v", err)
	}
	invalid := DefaultOverlayOptions()
	invalid.Validation.MaxDiagnostics = 0
	if _, err := ApplyOverlays(context.Background(), document, nil, invalid); !errors.Is(err, ErrOverlayResult) {
		t.Fatalf("validation error = %v", err)
	}
}

func TestRenameCoversEveryRejectedMappingAndExactLimit(t *testing.T) {
	t.Parallel()

	document := composeDocument(t, `{
		"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[],
		"components":{"schemas":{"Value":true}}
	}`)
	tests := []map[ComponentKind]map[string]string{
		{"unknown": {"Value": "Other"}},
		{SchemaComponents: {"": "Other"}},
		{SchemaComponents: {"Value": ""}},
		{SchemaComponents: {"Missing": "Other"}},
	}
	for _, renames := range tests {
		if _, err := RenameComponents(context.Background(), document, renames, DefaultRenameOptions()); !errors.Is(err, ErrInvalidRename) {
			t.Errorf("renames %#v error = %v", renames, err)
		}
	}
	options := DefaultRenameOptions()
	options.MaxRenames = 1
	encoded, err := openrpc.MarshalCanonical(document)
	if err != nil {
		t.Fatal(err)
	}
	options.MaxOutputBytes = len(encoded)
	if _, err := RenameComponents(context.Background(), document, map[ComponentKind]map[string]string{SchemaComponents: {"Value": "Value"}}, options); err != nil {
		t.Fatalf("self rename at exact limits error = %v", err)
	}
	zeroOutput := DefaultRenameOptions()
	zeroOutput.MaxOutputBytes = 0
	if _, err := RenameComponents(context.Background(), document, nil, zeroOutput); !errors.Is(err, ErrInvalidRename) {
		t.Fatalf("zero rename output limit error = %v", err)
	}
	limited := DefaultRenameOptions()
	limited.MaxRenames = 1
	if _, err := RenameComponents(context.Background(), document, map[ComponentKind]map[string]string{SchemaComponents: {"Value": "Other", "Missing": "Third"}}, limited); !errors.Is(err, ErrRenameLimit) {
		t.Fatalf("rename count error = %v", err)
	}
	limited = DefaultRenameOptions()
	limited.MaxOutputBytes = len(encoded)
	if _, err := RenameComponents(context.Background(), document, map[ComponentKind]map[string]string{SchemaComponents: {"Value": "MuchLongerValue"}}, limited); !errors.Is(err, ErrRenameLimit) {
		t.Fatalf("rename output error = %v", err)
	}
	invalid := DefaultRenameOptions()
	invalid.Parse.JSON.MaxBytes = 0
	if _, err := RenameComponents(context.Background(), document, nil, invalid); !errors.Is(err, ErrInvalidRename) {
		t.Fatalf("rename parse error = %v", err)
	}
	invalid = DefaultRenameOptions()
	invalid.Validation.MaxDiagnostics = 0
	if _, err := RenameComponents(context.Background(), document, nil, invalid); !errors.Is(err, ErrInvalidRename) {
		t.Fatalf("rename validation error = %v", err)
	}
	if _, err := RenameComponents(context.Background(), openrpc.Document{}, nil, DefaultRenameOptions()); !errors.Is(err, ErrInvalidRename) {
		t.Fatalf("zero document error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RenameComponents(ctx, document, nil, DefaultRenameOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("rename cancellation error = %v", err)
	}
	for _, kind := range []ComponentKind{
		SchemaComponents, LinkComponents, ErrorComponents, ExampleComponents,
		ExamplePairingComponents, ContentDescriptorComponents, TagComponents,
	} {
		if !validComponentKind(kind) {
			t.Errorf("validComponentKind(%q) = false", kind)
		}
	}
}

func TestLoopCancellationIsObserved(t *testing.T) {
	t.Parallel()

	document := composeDocument(t, `{"openrpc":"1.4.1","info":{"title":"x","version":"1"},"methods":[]}`)
	ctx := &countingContext{Context: context.Background(), remaining: 1}
	if _, err := Merge(ctx, []openrpc.Document{document}, DefaultMergeOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("merge loop cancellation error = %v", err)
	}
	ctx = &countingContext{Context: context.Background(), remaining: 1}
	overlay, err := NewOverlay([]byte(`{}`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyOverlays(ctx, document, []Overlay{overlay}, DefaultOverlayOptions()); !errors.Is(err, context.Canceled) {
		t.Fatalf("overlay loop cancellation error = %v", err)
	}
}

type countingContext struct {
	context.Context
	remaining int
}

func (ctx *countingContext) Err() error {
	ctx.remaining--
	if ctx.remaining < 0 {
		return context.Canceled
	}
	return nil
}

func (ctx *countingContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func composeDocument(t *testing.T, source string) openrpc.Document {
	t.Helper()
	options := openrpcparse.DefaultOptions()
	options.UnknownFields = openrpcparse.PreserveUnknownFields
	parsed, err := openrpcparse.Decode([]byte(strings.TrimSpace(source)), options)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Document()
}

func composeValue(t *testing.T, source string) jsonvalue.Value {
	t.Helper()
	value, err := jsonvalue.Parse([]byte(source), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	return value
}
