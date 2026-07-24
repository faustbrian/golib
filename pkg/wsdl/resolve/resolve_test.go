package resolve_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/wsdl/resolve"
)

func TestMemoryReturnsOwnedResourceCopies(t *testing.T) {
	t.Parallel()

	resolver, err := resolve.NewMemory(map[string][]byte{
		"https://example.test/service.wsdl": []byte("original"),
	})
	if err != nil {
		t.Fatalf("NewMemory() error = %v", err)
	}
	request := resolve.Request{URI: "https://example.test/service.wsdl"}
	resource, err := resolver.Resolve(context.Background(), request)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	resource.Content[0] = 'X'
	again, err := resolver.Resolve(context.Background(), request)
	if err != nil || string(again.Content) != "original" {
		t.Fatalf("second Resolve() = %#v, %v", again, err)
	}
}

func TestResolversRejectInvalidMissingAndDeniedResources(t *testing.T) {
	t.Parallel()

	invalid := []string{"relative.wsdl", "https://example.test/a.wsdl#part", "://bad"}
	for _, identity := range invalid {
		if _, err := resolve.NewMemory(map[string][]byte{identity: nil}); err == nil {
			t.Fatalf("NewMemory(%q) error = nil", identity)
		}
	}
	resolver, err := resolve.NewMemory(nil)
	if err != nil {
		t.Fatalf("NewMemory() error = %v", err)
	}
	request := resolve.Request{URI: "https://example.test/missing.wsdl"}
	if _, err := resolver.Resolve(context.Background(), request); !errors.Is(err, resolve.ErrNotFound) {
		t.Fatalf("Resolve(missing) error = %v", err)
	}
	if _, err := resolve.Deny().Resolve(context.Background(), request); !errors.Is(err, resolve.ErrAccessDenied) {
		t.Fatalf("Deny.Resolve() error = %v", err)
	}
}

func TestResolverChainHonorsCancellationAndErrorSemantics(t *testing.T) {
	t.Parallel()

	memory, err := resolve.NewMemory(map[string][]byte{
		"https://example.test/service.wsdl": []byte("service"),
	})
	if err != nil {
		t.Fatalf("NewMemory() error = %v", err)
	}
	request := resolve.Request{URI: "https://example.test/service.wsdl"}
	resource, err := resolve.Chain(nil, memory).Resolve(context.Background(), request)
	if err != nil || string(resource.Content) != "service" {
		t.Fatalf("Chain.Resolve() = %#v, %v", resource, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := memory.Resolve(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve(canceled) error = %v", err)
	}
	if _, err := resolve.Chain(resolve.Deny(), memory).Resolve(
		context.Background(),
		request,
	); !errors.Is(err, resolve.ErrAccessDenied) {
		t.Fatalf("Chain(deny).Resolve() error = %v", err)
	}
}

func TestResolversCoverNilCancellationAndExhaustion(t *testing.T) {
	t.Parallel()

	request := resolve.Request{URI: "https://example.test/service.wsdl"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := resolve.Deny().Resolve(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("Deny.Resolve(canceled) error = %v", err)
	}
	var memory *resolve.Memory
	if _, err := memory.Resolve(context.Background(), request); !errors.Is(err, resolve.ErrAccessDenied) {
		t.Fatalf("nil Memory.Resolve() error = %v", err)
	}
	if _, err := resolve.Chain().Resolve(context.Background(), request); !errors.Is(err, resolve.ErrNotFound) {
		t.Fatalf("empty Chain.Resolve() error = %v", err)
	}
	if _, err := resolve.Chain().Resolve(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("Chain.Resolve(canceled) error = %v", err)
	}
}
