package compile

import (
	"context"
	"errors"
	"testing"

	xsd "github.com/faustbrian/golib/pkg/xsd"
	"github.com/faustbrian/golib/pkg/xsd/resolve"
)

func TestLoadResourceBranches(t *testing.T) {
	t.Parallel()

	const explicitURI = "https://example.test/explicit.xsd"
	const canonicalURI = "https://example.test/catalog.xsd"
	cachedDocument := &xsd.Document{SystemID: explicitURI}
	state := loadState(t, funcResolver(func(context.Context, resolve.Request) (resolve.Resource, error) {
		t.Fatal("resolver called for an explicit cached resource")
		return resolve.Resource{}, nil
	}), 1024)
	state.resources[explicitURI] = resourceDocument{document: cachedDocument}
	document, identity, err := state.load(context.Background(), xsd.SchemaReference{URI: explicitURI})
	if err != nil || document != cachedDocument || identity != explicitURI {
		t.Fatalf("cached load() = %#v, %q, %v", document, identity, err)
	}

	want := errors.New("resolver failed")
	state = loadState(t, funcResolver(func(context.Context, resolve.Request) (resolve.Resource, error) {
		return resolve.Resource{}, want
	}), 1024)
	if _, _, err := state.load(context.Background(), xsd.SchemaReference{URI: explicitURI}); !errors.Is(err, want) {
		t.Fatalf("resolver load() error = %v", err)
	}

	state = loadState(t, staticResolver(resolve.Resource{URI: canonicalURI}), 1024)
	if _, _, err := state.load(context.Background(), xsd.SchemaReference{URI: explicitURI}); !errors.Is(err, ErrResourceIdentity) {
		t.Fatalf("identity mismatch error = %v", err)
	}

	state = loadState(t, staticResolver(resolve.Resource{URI: "relative.xsd"}), 1024)
	if _, _, err := state.load(context.Background(), xsd.SchemaReference{}); !errors.Is(err, ErrResourceIdentity) {
		t.Fatalf("invalid catalog identity error = %v", err)
	}

	catalogDocument := &xsd.Document{SystemID: canonicalURI}
	state = loadState(t, staticResolver(resolve.Resource{URI: canonicalURI}), 1024)
	state.resources[canonicalURI] = resourceDocument{document: catalogDocument}
	document, identity, err = state.load(context.Background(), xsd.SchemaReference{})
	if err != nil || document != catalogDocument || identity != canonicalURI {
		t.Fatalf("catalog cached load() = %#v, %q, %v", document, identity, err)
	}

	content := []byte(`<schema xmlns="http://www.w3.org/2001/XMLSchema"/>`)
	state = loadState(t, staticResolver(resolve.Resource{URI: explicitURI, Content: content}), 1)
	if _, _, err := state.load(context.Background(), xsd.SchemaReference{URI: explicitURI}); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("byte limit error = %v", err)
	}

	state = loadState(t, staticResolver(resolve.Resource{URI: explicitURI, Content: []byte(`<schema`)}), 1024)
	if _, _, err := state.load(context.Background(), xsd.SchemaReference{URI: explicitURI}); err == nil {
		t.Fatal("load() accepted malformed XML")
	}

	state = loadState(t, staticResolver(resolve.Resource{URI: explicitURI, Content: content}), 1024)
	document, identity, err = state.load(context.Background(), xsd.SchemaReference{URI: explicitURI})
	if err != nil || document.SystemID != explicitURI || identity != explicitURI ||
		state.resources[explicitURI].document != document {
		t.Fatalf("successful load() = %#v, %q, %v", document, identity, err)
	}
}

func loadState(t *testing.T, resolver resolve.Resolver, maxBytes int64) *compileState {
	t.Helper()
	compiler, err := New(Options{Resolver: resolver, Limits: Limits{MaxBytes: maxBytes}})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return &compileState{
		compiler:  compiler,
		resources: map[string]resourceDocument{},
	}
}

type funcResolver func(context.Context, resolve.Request) (resolve.Resource, error)

func (r funcResolver) Resolve(ctx context.Context, request resolve.Request) (resolve.Resource, error) {
	return r(ctx, request)
}

func staticResolver(resource resolve.Resource) resolve.Resolver {
	return funcResolver(func(context.Context, resolve.Request) (resolve.Resource, error) {
		return resource, nil
	})
}
