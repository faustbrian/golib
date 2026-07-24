package reference_test

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/faustbrian/golib/pkg/openrpc/reference"
)

func TestMemoryStoreOwnsDocumentsAndEnforcesBounds(t *testing.T) {
	t.Parallel()

	documents := map[string][]byte{"https://example.com/schema.json": []byte(`{"type":"string"}`)}
	store, err := reference.NewMemoryStore(documents)
	if err != nil {
		t.Fatal(err)
	}
	documents["https://example.com/schema.json"][0] = '['
	loaded, err := store.Load(context.Background(), "https://example.com/schema.json", 1024)
	if err != nil {
		t.Fatal(err)
	}
	if loaded[0] != '{' {
		t.Fatal("NewMemoryStore retained caller-owned bytes")
	}
	loaded[0] = '['
	again, err := store.Load(context.Background(), "https://example.com/schema.json", 1024)
	if err != nil || again[0] != '{' {
		t.Fatal("Load exposed mutable store bytes")
	}
	if _, err := store.Load(context.Background(), "https://example.com/schema.json", 1); !errors.Is(err, reference.ErrStoreLimit) {
		t.Fatalf("limit error = %v", err)
	}
}

func TestFSStoreMapsOnlyURIsBelowExplicitBase(t *testing.T) {
	t.Parallel()

	store, err := reference.NewFSStore(fstest.MapFS{
		"schemas/value.json": {Data: []byte(`{"value":true}`)},
	}, "https://example.com/assets/")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), "https://example.com/assets/schemas/value.json", 1024)
	if err != nil || string(loaded) != `{"value":true}` {
		t.Fatalf("Load = %s, error = %v", loaded, err)
	}
	for _, uri := range []string{
		"https://other.example/assets/schemas/value.json",
		"https://example.com/private.json",
		"https://example.com/assets/%2e%2e/private.json",
		"https://example.com/assets/schemas%2fvalue.json",
	} {
		if _, err := store.Load(context.Background(), uri, 1024); !errors.Is(err, reference.ErrStoreURI) {
			t.Errorf("Load(%q) error = %v", uri, err)
		}
	}
	if _, err := store.Load(context.Background(), "https://example.com/assets/schemas/value.json", 1); !errors.Is(err, reference.ErrStoreLimit) {
		t.Fatalf("limit error = %v", err)
	}
}

func TestStoresHonorCancellationAndInvalidConfiguration(t *testing.T) {
	t.Parallel()

	if _, err := reference.NewMemoryStore(map[string][]byte{"relative": []byte(`{}`)}); !errors.Is(err, reference.ErrStoreURI) {
		t.Fatalf("NewMemoryStore error = %v", err)
	}
	if _, err := reference.NewFSStore(nil, "relative/"); !errors.Is(err, reference.ErrStorePolicy) {
		t.Fatalf("NewFSStore error = %v", err)
	}
	store, err := reference.NewMemoryStore(map[string][]byte{"https://example.com/a": []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Load(ctx, "https://example.com/a", 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestStoreFuncAdaptsExplicitLoadContract(t *testing.T) {
	t.Parallel()

	store := reference.StoreFunc(func(_ context.Context, uri string, maximum int) ([]byte, error) {
		if uri != "https://example.com/schema.json" || maximum != 128 {
			t.Fatalf("Load(%q, %d)", uri, maximum)
		}
		return []byte(`{"type":"string"}`), nil
	})
	loaded, err := store.Load(context.Background(), "https://example.com/schema.json", 128)
	if err != nil || string(loaded) != `{"type":"string"}` {
		t.Fatalf("Load() = %s, %v", loaded, err)
	}
}
