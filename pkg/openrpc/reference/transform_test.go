package reference_test

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openrpc/reference"
)

func TestDereferenceExpandsInternalAndExternalTargets(t *testing.T) {
	t.Parallel()

	root := parseValue(t, `{
		"components":{"schemas":{"Local":{"type":"string"}}},
		"local":{"$ref":"#/components/schemas/Local"},
		"external":{"$ref":"shared.json#/schema"}
	}`)
	store := &recordingStore{documents: map[string][]byte{
		"https://example.com/api/shared.json": []byte(`{
			"schema":{"type":"array","items":{"$ref":"#/item"}},
			"item":{"type":"integer"}
		}`),
	}}
	resolvePolicy := reference.DefaultResolvePolicy()
	resolvePolicy.AllowExternal = true
	resolvePolicy.AllowedSchemes = []string{"https"}
	resolvePolicy.AllowedHosts = []string{"example.com"}
	resolver, err := reference.NewResolver(store, resolvePolicy)
	if err != nil {
		t.Fatal(err)
	}

	result, err := reference.Dereference(
		context.Background(), resolver, root,
		"https://example.com/api/openrpc.json",
		reference.DefaultTransformPolicy(),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"components":{"schemas":{"Local":{"type":"string"}}},"external":{"items":{"type":"integer"},"type":"array"},"local":{"type":"string"}}`
	if got := string(result.Bytes()); got != want {
		t.Fatalf("Dereference = %s", got)
	}
	if len(store.loads) != 1 {
		t.Fatalf("loads = %#v", store.loads)
	}
}

func TestDereferenceRejectsCyclesAndTransformLimits(t *testing.T) {
	t.Parallel()

	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	root := parseValue(t, `{"a":{"next":{"$ref":"#/a"}}}`)
	_, cycleErr := reference.Dereference(
		context.Background(), resolver, root, "https://example.com/openrpc.json",
		reference.DefaultTransformPolicy(),
	)
	if !errors.Is(cycleErr, reference.ErrDereferenceCycle) {
		t.Fatalf("cycle error = %v", cycleErr)
	}
	var transformError *reference.TransformError
	if !errors.As(cycleErr, &transformError) || transformError.Error() == "" ||
		!errors.Is(transformError, reference.ErrDereferenceCycle) {
		t.Fatalf("typed transform error = %#v", cycleErr)
	}

	policy := reference.DefaultTransformPolicy()
	policy.MaxReferences = 1
	root = parseValue(t, `{"a":{"type":"string"},"x":{"$ref":"#/a"},"y":{"$ref":"#/a"}}`)
	if _, err := reference.Dereference(
		context.Background(), resolver, root, "https://example.com/openrpc.json", policy,
	); !errors.Is(err, reference.ErrTransformLimit) {
		t.Fatalf("reference limit error = %v", err)
	}

	policy = reference.DefaultTransformPolicy()
	policy.MaxOutputBytes = 1
	if _, err := reference.Dereference(
		context.Background(), resolver, parseValue(t, `{}`),
		"https://example.com/openrpc.json", policy,
	); !errors.Is(err, reference.ErrTransformLimit) {
		t.Fatalf("output limit error = %v", err)
	}
}

func TestDereferenceValidatesPolicyAndCancellation(t *testing.T) {
	t.Parallel()

	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	root := parseValue(t, `{}`)
	if _, err := reference.Dereference(
		context.Background(), resolver, root, "https://example.com/openrpc.json",
		reference.TransformPolicy{},
	); !errors.Is(err, reference.ErrTransformPolicy) {
		t.Fatalf("policy error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reference.Dereference(
		ctx, resolver, root, "https://example.com/openrpc.json",
		reference.DefaultTransformPolicy(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestDereferenceSelectedLeavesRejectedReferenceKindsUntouched(t *testing.T) {
	t.Parallel()

	resolver, err := reference.NewResolver(nil, reference.DefaultResolvePolicy())
	if err != nil {
		t.Fatal(err)
	}
	root := parseValue(t, `{
		"target":{"type":"string"},
		"value":{"$ref":"#/target"},
		"schema":{"$ref":"#/target"}
	}`)
	result, err := reference.DereferenceSelected(
		context.Background(), resolver, root,
		"https://example.com/openrpc.json",
		reference.DefaultTransformPolicy(),
		reference.SelectorFunc(func(path []string) bool {
			return len(path) == 1 && path[0] == "value"
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema":{"$ref":"#/target"},"target":{"type":"string"},"value":{"type":"string"}}`
	if got := string(result.Bytes()); got != want {
		t.Fatalf("DereferenceSelected = %s", got)
	}
}
