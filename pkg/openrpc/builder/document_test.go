package builder_test

import (
	"errors"
	"sync"
	"testing"

	openrpc "github.com/faustbrian/golib/pkg/openrpc"
	"github.com/faustbrian/golib/pkg/openrpc/builder"
	"github.com/faustbrian/golib/pkg/openrpc/jsonschema"
	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

func TestDocumentBuilderIsImmutableAndDeterministic(t *testing.T) {
	t.Parallel()

	base, err := builder.NewDocument(version(t), info(t))
	if err != nil {
		t.Fatal(err)
	}
	withZulu, err := base.WithMethod(method(t, "zulu"))
	if err != nil {
		t.Fatal(err)
	}
	complete, err := withZulu.WithMethod(method(t, "alpha"))
	if err != nil {
		t.Fatal(err)
	}
	baseDocument, err := base.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(baseDocument.Methods()) != 0 {
		t.Fatal("WithMethod mutated the prior builder")
	}
	document, err := complete.Build()
	if err != nil {
		t.Fatal(err)
	}
	methods := document.Methods()
	first, _ := methods[0].Method()
	second, _ := methods[1].Method()
	if first.Name() != "alpha" || second.Name() != "zulu" {
		t.Fatalf("method order = %q, %q", first.Name(), second.Name())
	}
	if _, err := complete.WithMethod(method(t, "alpha")); !errors.Is(err, builder.ErrDuplicateMethod) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestMethodRegistryIsConcurrentAndReturnsSortedSnapshots(t *testing.T) {
	t.Parallel()

	registry := builder.NewMethodRegistry()
	names := []string{"zulu", "echo", "alpha", "bravo"}
	var group sync.WaitGroup
	group.Add(len(names))
	for _, name := range names {
		go func() {
			defer group.Done()
			if err := registry.Register(method(t, name)); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	methods := registry.Methods()
	for index, want := range []string{"alpha", "bravo", "echo", "zulu"} {
		if methods[index].Name() != want {
			t.Fatalf("Methods()[%d] = %q", index, methods[index].Name())
		}
	}
	if err := registry.Register(method(t, "alpha")); !errors.Is(err, builder.ErrDuplicateMethod) {
		t.Fatalf("duplicate error = %v", err)
	}
}

func TestBuilderRejectsInvalidInputs(t *testing.T) {
	t.Parallel()

	if _, err := builder.NewDocument(openrpc.Version{}, openrpc.Info{}); !errors.Is(err, builder.ErrInvalidBuilder) {
		t.Fatalf("NewDocument error = %v", err)
	}
	var zero builder.Document
	if _, err := zero.Build(); !errors.Is(err, builder.ErrInvalidBuilder) {
		t.Fatalf("Build error = %v", err)
	}
	var registry *builder.MethodRegistry
	if err := registry.Register(method(t, "method")); !errors.Is(err, builder.ErrInvalidRegistry) {
		t.Fatalf("Register error = %v", err)
	}
}

func TestDocumentBuilderPreservesEveryOptionalRootField(t *testing.T) {
	t.Parallel()

	base, err := builder.NewDocument(version(t), info(t))
	if err != nil {
		t.Fatal(err)
	}
	reference, err := openrpc.NewReference("#/components/contentDescriptors/Input")
	if err != nil {
		t.Fatal(err)
	}
	server, err := openrpc.NewServer(openrpc.ServerInput{URL: "https://example.com/rpc"})
	if err != nil {
		t.Fatal(err)
	}
	docs, err := openrpc.NewExternalDocumentation(openrpc.ExternalDocumentationInput{URL: "https://example.com/docs"})
	if err != nil {
		t.Fatal(err)
	}
	components, err := openrpc.NewComponents(openrpc.ComponentsInput{Schemas: map[string]jsonschema.Schema{}})
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonvalue.Parse([]byte(`true`), jsonvalue.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	extensions, err := openrpc.NewExtensions(openrpc.Field{Name: "x-builder", Value: value})
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := openrpc.NewUnknownFields(openrpc.Field{Name: "future", Value: value})
	if err != nil {
		t.Fatal(err)
	}

	complete, err := base.WithMethodReference(reference)
	if err != nil {
		t.Fatal(err)
	}
	complete, err = complete.WithServers([]openrpc.Server{server})
	if err != nil {
		t.Fatal(err)
	}
	complete, err = complete.WithComponents(components)
	if err != nil {
		t.Fatal(err)
	}
	complete, err = complete.WithSchemaURI("https://example.com/schema")
	if err != nil {
		t.Fatal(err)
	}
	complete, err = complete.WithExternalDocs(docs)
	if err != nil {
		t.Fatal(err)
	}
	complete, err = complete.WithFields(extensions, unknown)
	if err != nil {
		t.Fatal(err)
	}
	document, err := complete.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Methods()) != 1 || document.Extensions().Len() != 1 ||
		document.UnknownFields().Len() != 1 {
		t.Fatal("builder lost optional root fields")
	}
	if _, present := document.Servers(); !present {
		t.Fatal("explicit servers reported absent")
	}
	if _, present := document.Components(); !present {
		t.Fatal("components reported absent")
	}
	if _, present := document.ExternalDocs(); !present {
		t.Fatal("external docs reported absent")
	}
	if uri, present := document.SchemaURI(); !present || uri != "https://example.com/schema" {
		t.Fatalf("SchemaURI() = (%q, %t)", uri, present)
	}
}

func TestDocumentBuilderSortsReferencesAndRejectsZeroValues(t *testing.T) {
	t.Parallel()

	base, err := builder.NewDocument(version(t), info(t))
	if err != nil {
		t.Fatal(err)
	}
	zulu, err := openrpc.NewReference("#/zulu")
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := openrpc.NewReference("#/alpha")
	if err != nil {
		t.Fatal(err)
	}
	withReferences, err := base.WithMethodReference(zulu)
	if err != nil {
		t.Fatal(err)
	}
	withReferences, err = withReferences.WithMethodReference(alpha)
	if err != nil {
		t.Fatal(err)
	}
	document, err := withReferences.Build()
	if err != nil {
		t.Fatal(err)
	}
	methods := document.Methods()
	first, _ := methods[0].Reference()
	second, _ := methods[1].Reference()
	if first.Ref() != "#/alpha" || second.Ref() != "#/zulu" {
		t.Fatalf("reference order = %q, %q", first.Ref(), second.Ref())
	}

	var zero builder.Document
	invalidOperations := []func() error{
		func() error { _, callErr := zero.WithMethod(method(t, "method")); return callErr },
		func() error { _, callErr := zero.WithMethodReference(alpha); return callErr },
		func() error { _, callErr := zero.WithServers(nil); return callErr },
		func() error { _, callErr := zero.WithComponents(openrpc.Components{}); return callErr },
		func() error { _, callErr := zero.WithSchemaURI("https://example.com"); return callErr },
		func() error { _, callErr := zero.WithExternalDocs(openrpc.ExternalDocumentation{}); return callErr },
		func() error { _, callErr := zero.WithFields(openrpc.Fields{}, openrpc.Fields{}); return callErr },
		func() error { _, callErr := base.WithMethod(openrpc.Method{}); return callErr },
		func() error { _, callErr := base.WithMethodReference(openrpc.Reference{}); return callErr },
		func() error { _, callErr := base.WithSchemaURI(""); return callErr },
		func() error { _, callErr := base.WithExternalDocs(openrpc.ExternalDocumentation{}); return callErr },
	}
	for index, operation := range invalidOperations {
		if err := operation(); !errors.Is(err, builder.ErrInvalidBuilder) {
			t.Fatalf("operation %d error = %v", index, err)
		}
	}
}

func TestMethodRegistrySupportsZeroValueAndNilSnapshots(t *testing.T) {
	t.Parallel()

	var registry builder.MethodRegistry
	if err := registry.Register(method(t, "method")); err != nil {
		t.Fatal(err)
	}
	if methods := registry.Methods(); len(methods) != 1 || methods[0].Name() != "method" {
		t.Fatalf("Methods() = %#v", methods)
	}
	if err := registry.Register(openrpc.Method{}); !errors.Is(err, builder.ErrInvalidRegistry) {
		t.Fatalf("zero method error = %v", err)
	}
	var nilRegistry *builder.MethodRegistry
	if methods := nilRegistry.Methods(); methods != nil {
		t.Fatalf("nil Methods() = %#v", methods)
	}
}

func version(t *testing.T) openrpc.Version {
	t.Helper()
	version, err := openrpc.ParseVersion("1.4.1")
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func info(t *testing.T) openrpc.Info {
	t.Helper()
	info, err := openrpc.NewInfo(openrpc.InfoInput{Title: "Builder", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func method(t *testing.T, name string) openrpc.Method {
	t.Helper()
	method, err := openrpc.NewMethod(openrpc.MethodInput{
		Name: name, Params: []openrpc.ContentDescriptorOrReference{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return method
}
