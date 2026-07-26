package reference_test

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

func TestResolveInternalReferenceWithoutExternalIO(t *testing.T) {
	t.Parallel()

	root := mustObject(t, []jsonvalue.Member{
		{Name: "components", Value: mustObject(t, []jsonvalue.Member{
			{Name: "schemas", Value: mustObject(t, []jsonvalue.Member{
				{Name: "Pet", Value: mustString(t, "target")},
			})},
		})},
	})
	resource := reference.Resource{
		RetrievalURI: "https://api.example.test/openapi.json",
		Root:         root,
	}
	target, err := reference.Resolve(
		context.Background(),
		resource,
		"#/components/schemas/Pet",
		nil,
		reference.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if text, ok := target.Value.Text(); !ok || text != "target" {
		t.Fatalf("resolved %#v", target.Value)
	}
	if target.RequestedURI != "https://api.example.test/openapi.json" {
		t.Fatalf("requested URI = %q", target.RequestedURI)
	}
}

func TestResolveUsesOpenAPI32SelfAsBase(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://retrieval.example.test/openapi.json",
		Root: bundleValue(t, `{
			"openapi":"3.2.0","$self":"https://canonical.example.test/api/root.json",
			"paths":{}
		}`),
	}
	var requested string
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		requested = identifier
		return reference.Resource{RetrievalURI: identifier, Root: mustObject(t, nil)}, nil
	})
	if _, err := reference.Resolve(
		context.Background(), base, "models.json", resolver,
		reference.DefaultLimits(),
	); err != nil {
		t.Fatal(err)
	}
	if requested != "https://canonical.example.test/api/models.json" {
		t.Fatalf("requested = %q", requested)
	}
	target, err := reference.Resolve(
		context.Background(),
		base,
		"https://retrieval.example.test/openapi.json#",
		nil,
		reference.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if target.Value.Kind() != jsonvalue.ObjectKind ||
		target.RequestedURI != "https://retrieval.example.test/openapi.json" {
		t.Fatalf("retrieval alias target = %#v", target)
	}
}

func TestResolveExternalReferenceThroughExplicitResolver(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/descriptions/root.json",
		CanonicalURI: "https://canonical.example.test/apis/root.json",
		Root:         mustObject(t, nil),
	}
	called := false
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		called = true
		if identifier != "https://canonical.example.test/models/pet.json" {
			t.Fatalf("identifier = %q", identifier)
		}
		return reference.Resource{
			RetrievalURI: "https://cdn.example.test/pet-v2.json",
			CanonicalURI: "https://schemas.example.test/pet",
			Root: mustObject(t, []jsonvalue.Member{
				{Name: "$defs", Value: mustObject(t, []jsonvalue.Member{
					{Name: "Pet", Value: mustString(t, "external")},
				})},
			}),
		}, nil
	})
	target, err := reference.Resolve(
		context.Background(),
		base,
		"../models/pet.json#/$defs/Pet",
		resolver,
		reference.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("explicit resolver was not called")
	}
	if target.Resource.RetrievalURI == target.Resource.CanonicalURI {
		t.Fatal("retrieval and canonical URI were conflated")
	}
}

func TestResolveSearchesEntireExternalDocumentForAnchor(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/openapi.json",
		Root:         mustObject(t, nil),
	}
	resolver := reference.ResolverFunc(func(
		_ context.Context,
		identifier string,
	) (reference.Resource, error) {
		return reference.Resource{
			RetrievalURI: identifier,
			Root: mustObject(t, []jsonvalue.Member{
				{Name: "unrelated", Value: mustString(t, "first")},
				{Name: "$defs", Value: mustObject(t, []jsonvalue.Member{
					{Name: "Deep", Value: mustObject(t, []jsonvalue.Member{
						{Name: "$anchor", Value: mustString(t, "deep")},
						{Name: "type", Value: mustString(t, "integer")},
					})},
				})},
			}),
		}, nil
	})
	target, err := reference.Resolve(
		context.Background(), base, "schemas.json#deep", resolver,
		reference.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	typeValue, exists := target.Value.Lookup("type")
	if !exists {
		t.Fatal("deep anchor target was not found")
	}
	if text, valid := typeValue.Text(); !valid || text != "integer" {
		t.Fatalf("deep anchor target type = %q, %t", text, valid)
	}
}

func TestResolveAnchorsAndExternalPolicy(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root: mustObject(t, []jsonvalue.Member{
			{Name: "$defs", Value: mustObject(t, []jsonvalue.Member{
				{Name: "Pet", Value: mustObject(t, []jsonvalue.Member{
					{Name: "$anchor", Value: mustString(t, "pet")},
					{Name: "type", Value: mustString(t, "object")},
				})},
			})},
		}),
	}
	target, err := reference.Resolve(
		context.Background(), base, "#pet", nil, reference.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok := target.Value.Lookup("type"); !ok {
		t.Fatal("anchor target was not resolved")
	} else if text, _ := value.Text(); text != "object" {
		t.Fatalf("anchor target type = %q", text)
	}

	_, err = reference.Resolve(
		context.Background(), base, "other.json", nil, reference.DefaultLimits(),
	)
	if !errors.Is(err, reference.ErrExternalResolutionDisabled) {
		t.Fatalf("external resolution error = %v", err)
	}
}

func TestResolveEnforcesContextAndTraversalLimit(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root: mustObject(t, []jsonvalue.Member{
			{Name: "nested", Value: mustObject(t, []jsonvalue.Member{
				{Name: "$anchor", Value: mustString(t, "deep")},
			})},
		}),
	}
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if _, err := reference.Resolve(
		//lint:ignore SA1012 This assertion verifies the nil-context contract.
		nil, base, "#", nil, reference.DefaultLimits(),
	); err == nil {
		t.Fatal("nil context was accepted")
	}
	limits := reference.DefaultLimits()
	limits.MaxTraversalNodes = 1
	if _, err := reference.Resolve(
		context.Background(), base, "#deep", nil, limits,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("traversal limit error = %v", err)
	}
	limits = reference.DefaultLimits()
	limits.MaxTraversalDepth = 1
	if _, err := reference.Resolve(
		context.Background(), base, "#/nested/$anchor", nil, limits,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("pointer depth limit error = %v", err)
	}
	if _, err := reference.Resolve(
		context.Background(), base, "#/nested/~2malformed", nil, limits,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("pre-parse pointer depth limit error = %v", err)
	}

	var nilResolver reference.ResolverFunc
	if _, err := reference.Resolve(
		context.Background(), base, "other.json", nilResolver,
		reference.DefaultLimits(),
	); !errors.Is(err, reference.ErrExternalResolutionDisabled) {
		t.Fatalf("typed nil resolver error = %v", err)
	}
}

func TestResolveAcceptsExactPointerAndAnchorTraversalLimits(t *testing.T) {
	t.Parallel()

	root := mustObject(t, []jsonvalue.Member{
		{Name: "child", Value: mustObject(t, []jsonvalue.Member{
			{Name: "$anchor", Value: mustString(t, "target")},
		})},
	})
	base := reference.Resource{Root: root}
	pointerLimits := reference.Limits{
		MaxTraversalDepth: 1, MaxTraversalNodes: 2, MaxReferenceDepth: 1,
	}
	if _, err := reference.Resolve(
		context.Background(), base, "#/child", nil, pointerLimits,
	); err != nil {
		t.Fatalf("exact pointer limits error = %v", err)
	}
	pointerLimits.MaxTraversalNodes = 1
	if _, err := reference.Resolve(
		context.Background(), base, "#/child", nil, pointerLimits,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("pointer node limit error = %v", err)
	}
	anchorLimits := reference.Limits{
		MaxTraversalDepth: 2, MaxTraversalNodes: 3, MaxReferenceDepth: 1,
	}
	if _, err := reference.Resolve(
		context.Background(), base, "#target", nil, anchorLimits,
	); err != nil {
		t.Fatalf("exact anchor limits error = %v", err)
	}

	nestedArray := mustArray(t, []jsonvalue.Value{
		mustArray(t, []jsonvalue.Value{mustObject(t, []jsonvalue.Member{
			{Name: "$anchor", Value: mustString(t, "deep-array")},
		})}),
	})
	shallowLimits := anchorLimits
	shallowLimits.MaxTraversalDepth = 1
	shallowLimits.MaxTraversalNodes = 10
	if _, err := reference.Resolve(
		context.Background(), reference.Resource{Root: nestedArray},
		"#deep-array", nil, shallowLimits,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("array anchor depth error = %v", err)
	}
}

func TestResolveRejectsWideAnchorSearchBeforeCopyingChildren(t *testing.T) {
	elements := make([]jsonvalue.Value, 4096)
	for index := range elements {
		elements[index] = jsonvalue.Null()
	}
	base := reference.Resource{Root: mustArray(t, elements)}
	limits := reference.DefaultLimits()
	limits.MaxTraversalNodes = 1
	const repetitions = 16
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	for range repetitions {
		if _, err := reference.Resolve(
			context.Background(), base, "#missing", nil, limits,
		); !errors.Is(err, reference.ErrLimitExceeded) {
			t.Fatalf("wide anchor error = %v", err)
		}
	}
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if allocated := (after.TotalAlloc - before.TotalAlloc) / repetitions; allocated > 64<<10 {
		t.Fatalf("wide rejected anchor allocated %d bytes per operation", allocated)
	}
}

func TestResolveChainReportsLegalReferenceCycle(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root: mustObject(t, []jsonvalue.Member{
			{Name: "components", Value: mustObject(t, []jsonvalue.Member{
				{Name: "schemas", Value: mustObject(t, []jsonvalue.Member{
					{Name: "A", Value: mustObject(t, []jsonvalue.Member{
						{Name: "$ref", Value: mustString(t, "#/components/schemas/B")},
					})},
					{Name: "B", Value: mustObject(t, []jsonvalue.Member{
						{Name: "$ref", Value: mustString(t, "#/components/schemas/A")},
					})},
				})},
			})},
		}),
	}
	chain, err := reference.ResolveChain(
		context.Background(),
		base,
		"#/components/schemas/A",
		nil,
		reference.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !chain.Circular() {
		t.Fatal("reference cycle was not reported")
	}
	targets := chain.Targets()
	if len(targets) != 2 {
		t.Fatalf("chain targets = %d, want 2", len(targets))
	}
	targets[0] = reference.Target{}
	if chain.Targets()[0].Value.Kind() == jsonvalue.InvalidKind {
		t.Fatal("chain exposed mutable target storage")
	}
}

func TestResolveChainEnforcesReferenceDepth(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		Root: mustObject(t, []jsonvalue.Member{
			{Name: "a", Value: mustObject(t, []jsonvalue.Member{
				{Name: "$ref", Value: mustString(t, "#/b")},
			})},
			{Name: "b", Value: mustObject(t, nil)},
		}),
	}
	limits := reference.DefaultLimits()
	limits.MaxReferenceDepth = 1
	_, err := reference.ResolveChain(
		context.Background(), base, "#/a", nil, limits,
	)
	if !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("reference depth error = %v", err)
	}
}
