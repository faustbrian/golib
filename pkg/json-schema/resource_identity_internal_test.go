package jsonschema

import (
	"context"
	"errors"
	"testing"
)

func TestResourceIndexFailuresPropagateAcrossDeferredPaths(t *testing.T) {
	t.Parallel()

	const raw = `{"unknown":{"$id":"%"}}`
	root, err := decodeJSON(context.Background(), []byte(raw), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	compiler := newSchemaCompiler(
		context.Background(), root, Draft202012, DefaultLimits(), nil,
		len(raw), false, false, standardFormats(), nil,
	)
	if _, err := compiler.resolveReference(root, "#/unknown"); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("got %v, want deferred indexing error", err)
	}
	if _, err := compiler.compile(root); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("got %v, want stored indexing error", err)
	}
	compiler.indexResources(root, compiler.bases[root], "", Draft202012)
}

func TestLoadedResourceCannotReplaceAnIndexedResource(t *testing.T) {
	t.Parallel()

	const raw = `{"$id":"https://example.test/root"}`
	root, err := decodeJSON(context.Background(), []byte(raw), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	loader := ResourceLoaderFunc(func(context.Context, string) ([]byte, error) {
		return []byte(raw), nil
	})
	compiler := newSchemaCompiler(
		context.Background(), root, Draft202012, DefaultLimits(), loader,
		len(raw), false, false, standardFormats(), nil,
	)
	if _, err := compiler.loadResource("https://example.test/other"); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("got %v, want duplicate resource error", err)
	}
}

func TestBundledMetaSchemaLoadingPropagatesCancellation(t *testing.T) {
	t.Parallel()

	root := &jsonValue{kind: kindObject, object: make(map[string]*jsonValue)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	compiler := newSchemaCompiler(
		ctx, root, Draft202012, DefaultLimits(), nil, 2, false, false,
		standardFormats(), nil,
	)
	if _, err := compiler.loadResource(string(Draft202012)); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context cancellation", err)
	}
}
