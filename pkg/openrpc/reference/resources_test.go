package reference_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/reference"
)

func TestResourcesLoadsTransitiveSchemaGraphOnceAndHonorsIDs(t *testing.T) {
	t.Parallel()

	store := &recordingStore{documents: map[string][]byte{
		"https://example.com/schemas.json": []byte(`{
			"$id":"https://schemas.example/root.json",
			"definitions":{
				"Local":{"$ref":"#/definitions/Local"},
				"Remote":{"$ref":"child.json#/Value"}
			}
		}`),
		"https://schemas.example/child.json": []byte(`{"Value":{"type":"string"}}`),
	}}
	policy := reference.DefaultResolvePolicy()
	policy.AllowExternal = true
	policy.AllowedSchemes = []string{"https"}
	policy.AllowedHosts = []string{"example.com", "schemas.example"}
	resolver, err := reference.NewResolver(store, policy)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := resolver.Resources(
		context.Background(), parseValue(t, `{}`),
		"https://example.com/openrpc.json",
		[]string{"schemas.json#/definitions/Remote", "schemas.json#/definitions/Local"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 2 || len(store.loads) != 2 {
		t.Fatalf("resources = %#v, loads = %#v", resources, store.loads)
	}
	if store.loads[0] != "https://example.com/schemas.json" ||
		store.loads[1] != "https://schemas.example/child.json" {
		t.Fatalf("loads = %#v", store.loads)
	}
}

func TestResourcesBoundsTransitiveReferenceFanout(t *testing.T) {
	t.Parallel()

	store := &recordingStore{documents: map[string][]byte{
		"https://example.com/parent.json": []byte(`{
			"first":{"$ref":"first.json"},
			"second":{"$ref":"second.json"}
		}`),
		"https://example.com/first.json":  []byte(`{}`),
		"https://example.com/second.json": []byte(`{}`),
	}}
	policy := reference.DefaultResolvePolicy()
	policy.AllowExternal = true
	policy.AllowedSchemes = []string{"https"}
	policy.AllowedHosts = []string{"example.com"}
	policy.MaxReferences = 2
	resolver, err := reference.NewResolver(store, policy)
	if err != nil {
		t.Fatal(err)
	}

	_, err = resolver.Resources(
		context.Background(), parseValue(t, `{}`),
		"https://example.com/openrpc.json", []string{"parent.json"},
	)
	if !errors.Is(err, reference.ErrResolveLimit) {
		t.Fatalf("transitive fan-out error = %v", err)
	}
	if len(store.loads) != 1 || store.loads[0] != "https://example.com/parent.json" {
		t.Fatalf("loads after fan-out rejection = %#v", store.loads)
	}
}

func TestResourcesBoundsReferencesAcrossLoadedDocuments(t *testing.T) {
	t.Parallel()

	store := &recordingStore{documents: map[string][]byte{
		"https://example.com/parent-a.json": []byte(`{"next":{"$ref":"leaf-a.json"}}`),
		"https://example.com/parent-b.json": []byte(`{"next":{"$ref":"leaf-b.json"}}`),
		"https://example.com/leaf-a.json":   []byte(`{}`),
		"https://example.com/leaf-b.json":   []byte(`{}`),
	}}
	policy := reference.DefaultResolvePolicy()
	policy.AllowExternal = true
	policy.AllowedSchemes = []string{"https"}
	policy.AllowedHosts = []string{"example.com"}
	policy.MaxReferences = 3
	resolver, err := reference.NewResolver(store, policy)
	if err != nil {
		t.Fatal(err)
	}

	_, err = resolver.Resources(
		context.Background(), parseValue(t, `{}`),
		"https://example.com/openrpc.json",
		[]string{"parent-a.json", "parent-b.json"},
	)
	if !errors.Is(err, reference.ErrResolveLimit) {
		t.Fatalf("cross-document reference limit error = %v", err)
	}
	if len(store.loads) != 2 {
		t.Fatalf("loads before cross-document limit = %#v", store.loads)
	}
}
