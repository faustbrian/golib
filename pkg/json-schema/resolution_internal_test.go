package jsonschema

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"testing"
)

func TestReferenceResolverRejectsMalformedPointersAndSources(t *testing.T) {
	t.Parallel()

	const raw = `{"array":[true],"scalar":1,"unknown":{"title":"value"}}`
	root, err := decodeJSON(context.Background(), []byte(raw), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	compiler := newSchemaCompiler(
		context.Background(), root, Draft202012, DefaultLimits(), nil,
		len(raw), false, false, standardFormats(), nil,
	)
	for _, reference := range []string{
		"%", "https://example.test/?%", "#/missing", "#/array/01", "#/array/-1", "#/array/1",
		"#/array/2", "#/scalar/child",
	} {
		if _, err := compiler.resolveReference(root, reference); err == nil {
			t.Errorf("%q: expected reference error", reference)
		}
	}
	if _, err := compiler.resolveReference(&jsonValue{kind: kindObject}, "#"); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("got %v, want source-base error", err)
	}
	target, err := compiler.resolveReference(root, "#/unknown")
	if err != nil || target != root.object["unknown"] || compiler.bases[target] == nil {
		t.Fatalf("unindexed target was not indexed: target=%p err=%v", target, err)
	}
	delete(compiler.bases, target)
	compiler.bases[root] = nil
	compiler.bases[root.object["array"]] = &url.URL{}
	if _, err := compiler.resolveReference(root.object["array"], "#/unknown"); err != nil {
		t.Fatalf("resolve reference with an empty root base: %v", err)
	}
	if compiler.bases[target] == nil {
		t.Fatal("empty root base fallback left target base nil")
	}
	compiler.bases[root] = &url.URL{}
	if target, name, err := compiler.resolveDynamicReference(root, "#"); err != nil || target != root || name != "" {
		t.Fatalf("got target=%p name=%q err=%v, want root without dynamic name", target, name, err)
	}
	compiler.dynamicAnchors["#other"] = root
	if err := compiler.compileDynamicAnchors("wanted"); err != nil {
		t.Fatalf("compile unrelated dynamic anchors: %v", err)
	}

	fallback := &schemaCompiler{
		dialect:         Draft7,
		resourceFor:     map[*jsonValue]string{root: ""},
		schemaResources: make(map[string]*schemaResource),
	}
	if actual := fallback.dialectFor(root); actual != Draft7 {
		t.Fatalf("got %s, want Draft 7", actual)
	}
}

func TestResourceLoadingClassifiesEveryFailureBoundary(t *testing.T) {
	t.Parallel()

	newCompiler := func(loader ResourceLoader) *schemaCompiler {
		root := &jsonValue{kind: kindObject, object: make(map[string]*jsonValue)}
		return newSchemaCompiler(
			context.Background(), root, Draft202012, DefaultLimits(), loader,
			2, false, false, standardFormats(), nil,
		)
	}

	compiler := newCompiler(ResourceLoaderFunc(func(
		context.Context, string,
	) ([]byte, error) {
		return nil, fs.ErrPermission
	}))
	if _, err := compiler.loadResource("https://example.test/schema"); !errors.Is(err, fs.ErrPermission) || !errors.Is(err, ErrResourceUnavailable) {
		t.Fatalf("got %v, want wrapped loader error", err)
	}

	compiler = newCompiler(ResourceLoaderFunc(func(
		context.Context, string,
	) ([]byte, error) {
		return []byte(`{`), nil
	}))
	if _, err := compiler.loadResource("https://example.test/schema"); !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("got %v, want ErrInvalidJSON", err)
	}

	compiler = newCompiler(ResourceLoaderFunc(func(
		context.Context, string,
	) ([]byte, error) {
		return []byte(`true`), nil
	}))
	compiler.limits.MaxTotalSchemaBytes = compiler.loadedBytes
	if _, err := compiler.loadResource("https://example.test/schema"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want byte limit", err)
	}

	compiler = newCompiler(ResourceLoaderFunc(func(
		context.Context, string,
	) ([]byte, error) {
		return []byte(`true`), nil
	}))
	if _, err := compiler.loadResource("%"); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("got %v, want invalid identifier", err)
	}

	compiler = newCompiler(ResourceLoaderFunc(func(
		context.Context, string,
	) ([]byte, error) {
		return []byte(`true`), nil
	}))
	compiler.limits.MaxSchemaResources = compiler.loadedResources
	if _, err := compiler.loadResource("https://example.test/schema"); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("got %v, want resource limit", err)
	}

	parsed, err := url.Parse("https://example.test/schema")
	if err != nil {
		t.Fatal(err)
	}
	compiler = newCompiler(ResourceLoaderFunc(func(
		context.Context, string,
	) ([]byte, error) {
		return []byte(`true`), nil
	}))
	resourcesBefore := compiler.loadedResources
	bytesBefore := compiler.loadedBytes
	compiler.limits.MaxTotalSchemaBytes = bytesBefore + len(`true`)
	loaded, err := compiler.loadResource(parsed.String())
	if err != nil || loaded.kind != kindBoolean || compiler.loadedResources != resourcesBefore+1 {
		t.Fatalf("got %#v, err=%v", loaded, err)
	}
	if compiler.loadedBytes != bytesBefore+len(`true`) {
		t.Fatalf("got %d loaded bytes", compiler.loadedBytes)
	}

	compiler = newCompiler(ResourceLoaderFunc(func(
		context.Context, string,
	) ([]byte, error) {
		return nil, nil
	}))
	compiler.limits.MaxTotalSchemaBytes = compiler.loadedBytes
	if _, err := compiler.loadResource("https://example.test/empty"); !errors.Is(err, ErrInvalidJSON) {
		t.Fatalf("got %v, want empty resource parse failure", err)
	}
}
