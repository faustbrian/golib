package reference_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/reference"
)

func TestBundlePreservesRootAndTransitiveResourcesByURI(t *testing.T) {
	t.Parallel()

	root := parseValue(t, `{
		"schema":{"$ref":"schemas.json#/Input"},
		"method":{"$ref":"methods.json#/read"}
	}`)
	store := &recordingStore{documents: map[string][]byte{
		"https://example.com/schemas.json": []byte(`{"Input":{"$ref":"types.json#/Text"}}`),
		"https://example.com/types.json":   []byte(`{"Text":{"type":"string"}}`),
		"https://example.com/methods.json": []byte(`{"read":{"name":"read","params":[]}}`),
	}}
	policy := reference.DefaultResolvePolicy()
	policy.AllowExternal = true
	policy.AllowedSchemes = []string{"https"}
	policy.AllowedHosts = []string{"example.com"}
	resolver, err := reference.NewResolver(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := reference.Bundle(
		context.Background(), resolver, root, "https://example.com/openrpc.json",
	)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.BaseURI() != "https://example.com/openrpc.json" ||
		string(bundle.Root().Bytes()) != string(root.Bytes()) {
		t.Fatalf("bundle base = %q, root = %s", bundle.BaseURI(), bundle.Root().Bytes())
	}
	resources := bundle.Resources()
	if len(resources) != 3 {
		t.Fatalf("resources = %#v", resources)
	}
	delete(resources, "https://example.com/schemas.json")
	if len(bundle.Resources()) != 3 {
		t.Fatal("Resources exposed mutable bundle storage")
	}
	offline, err := bundle.Store()
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := offline.Load(
		context.Background(), "https://example.com/types.json", 1<<20,
	)
	if err != nil || string(loaded) != `{"Text":{"type":"string"}}` {
		t.Fatalf("offline resource = %s, error = %v", loaded, err)
	}
}

func TestBundleBoundsRootReferenceFanoutBeforeLoading(t *testing.T) {
	t.Parallel()

	root := parseValue(t, `{
		"first":{"$ref":"first.json"},
		"second":{"$ref":"second.json"},
		"third":{"$ref":"third.json"}
	}`)
	store := &recordingStore{documents: map[string][]byte{}}
	policy := reference.DefaultResolvePolicy()
	policy.MaxReferences = 2
	resolver, err := reference.NewResolver(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reference.Bundle(
		context.Background(), resolver, root,
		"https://example.com/openrpc.json",
	)
	if !errors.Is(err, reference.ErrResolveLimit) {
		t.Fatalf("root fan-out error = %v", err)
	}
	if len(store.loads) != 0 {
		t.Fatalf("store loads = %#v", store.loads)
	}
}
