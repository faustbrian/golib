package reference_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
	"github.com/faustbrian/golib/pkg/openapi/reference"
)

type staticResolver struct {
	resource reference.Resource
}

func (resolver staticResolver) Resolve(
	context.Context,
	string,
) (reference.Resource, error) {
	return resolver.resource, nil
}

type cancelDuringTraversal struct {
	context.Context
	calls int
}

func (ctx *cancelDuringTraversal) Err() error {
	ctx.calls++
	if ctx.calls > 1 {
		return context.Canceled
	}
	return nil
}

func TestResolveRejectsInvalidResourceAndURIInputs(t *testing.T) {
	t.Parallel()

	valid := reference.Resource{Root: mustObject(t, nil)}
	tests := []struct {
		name      string
		resource  reference.Resource
		reference string
	}{
		{name: "invalid root", resource: reference.Resource{}, reference: "#"},
		{name: "malformed base", resource: reference.Resource{
			RetrievalURI: "%", Root: valid.Root,
		}, reference: "#"},
		{name: "fragmented base", resource: reference.Resource{
			RetrievalURI: "https://api.example.test/root#fragment", Root: valid.Root,
		}, reference: "#"},
		{name: "malformed reference", resource: valid, reference: "%"},
		{name: "malformed fragment", resource: valid, reference: "#%ff"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := reference.Resolve(
				context.Background(), test.resource, test.reference, nil,
				reference.DefaultLimits(),
			); err == nil {
				t.Fatal("invalid resolution input was accepted")
			}
		})
	}
	if _, err := reference.Resolve(
		context.Background(), valid, "%", nil, reference.DefaultLimits(),
	); !errors.Is(err, reference.ErrInvalidReference) {
		t.Fatalf("malformed reference error = %v", err)
	}
}

func TestResolveRootAndConcreteResolver(t *testing.T) {
	t.Parallel()

	root := mustObject(t, nil)
	target, err := reference.Resolve(
		context.Background(), reference.Resource{Root: root}, "#", nil,
		reference.DefaultLimits(),
	)
	if err != nil || target.Value.Kind() != jsonvalue.ObjectKind {
		t.Fatalf("root target = %#v, %v", target, err)
	}
	resolver := staticResolver{resource: reference.Resource{
		RetrievalURI: "https://cdn.example.test/other.json",
		Root:         root,
	}}
	if _, err := reference.Resolve(
		context.Background(),
		reference.Resource{
			RetrievalURI: "https://api.example.test/root.json",
			Root:         root,
		},
		"other.json",
		resolver,
		reference.DefaultLimits(),
	); err != nil {
		t.Fatal(err)
	}
}

func TestResolvePropagatesCancellationAndResolverFailure(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root:         mustObject(t, nil),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reference.Resolve(
		ctx, base, "#", nil, reference.DefaultLimits(),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}

	want := errors.New("resolver failed")
	resolver := reference.ResolverFunc(func(
		context.Context,
		string,
	) (reference.Resource, error) {
		return reference.Resource{}, want
	})
	if _, err := reference.Resolve(
		context.Background(), base, "other.json", resolver,
		reference.DefaultLimits(),
	); !errors.Is(err, want) {
		t.Fatalf("resolver failure = %v", err)
	}

	invalid := reference.ResolverFunc(func(
		context.Context,
		string,
	) (reference.Resource, error) {
		return reference.Resource{}, nil
	})
	if _, err := reference.Resolve(
		context.Background(), base, "other.json", invalid,
		reference.DefaultLimits(),
	); err == nil {
		t.Fatal("invalid external resource was accepted")
	}
}

func TestResolveErrorsDoNotExposeResourceIdentifiersOrResolverDetails(t *testing.T) {
	t.Parallel()

	base := reference.Resource{
		RetrievalURI: "https://api.example.test/root.json",
		Root:         mustObject(t, nil),
	}
	const requested = "https://private.example.test/customer-token.json?secret=value"
	if _, err := reference.Resolve(
		context.Background(), base, requested, nil, reference.DefaultLimits(),
	); !errors.Is(err, reference.ErrExternalResolutionDisabled) ||
		strings.Contains(err.Error(), "customer-token") ||
		strings.Contains(err.Error(), "secret") {
		t.Fatalf("disabled external resolution error = %v", err)
	}

	want := errors.New("private resolver backend detail")
	resolver := reference.ResolverFunc(func(
		context.Context,
		string,
	) (reference.Resource, error) {
		return reference.Resource{}, want
	})
	if _, err := reference.Resolve(
		context.Background(), base, requested, resolver,
		reference.DefaultLimits(),
	); !errors.Is(err, want) || !errors.Is(err, reference.ErrResourceAccess) ||
		strings.Contains(err.Error(), "customer-token") ||
		strings.Contains(err.Error(), want.Error()) {
		t.Fatalf("external resolver error = %v", err)
	}
}

func TestResolveParseErrorsDoNotExposeResourceIdentifiers(t *testing.T) {
	t.Parallel()

	root := mustObject(t, nil)
	for _, test := range []struct {
		name      string
		base      reference.Resource
		reference string
	}{
		{
			name: "base",
			base: reference.Resource{
				RetrievalURI: "https://customer-token.example.test/%zz",
				Root:         root,
			},
			reference: "#",
		},
		{
			name:      "reference",
			base:      reference.Resource{Root: root},
			reference: "https://customer-token.example.test/%zz",
		},
	} {
		_, err := reference.Resolve(
			context.Background(), test.base, test.reference, nil,
			reference.DefaultLimits(),
		)
		if err == nil || strings.Contains(err.Error(), "customer-token") {
			t.Fatalf("%s URI parse error = %v", test.name, err)
		}
	}
}

func TestResolveAnchorErrorsDoNotExposeAnchorNames(t *testing.T) {
	t.Parallel()

	const name = "private-customer-token"
	missing := reference.Resource{Root: mustObject(t, nil)}
	if _, err := reference.Resolve(
		context.Background(), missing, "#"+name, nil,
		reference.DefaultLimits(),
	); !errors.Is(err, reference.ErrTargetNotFound) ||
		strings.Contains(err.Error(), name) {
		t.Fatalf("missing anchor error = %v", err)
	}

	anchor := mustObject(t, []jsonvalue.Member{
		{Name: "$anchor", Value: mustString(t, name)},
	})
	duplicate := reference.Resource{
		Root: mustArray(t, []jsonvalue.Value{anchor, anchor}),
	}
	if _, err := reference.Resolve(
		context.Background(), duplicate, "#"+name, nil,
		reference.DefaultLimits(),
	); !errors.Is(err, reference.ErrDuplicateAnchor) ||
		strings.Contains(err.Error(), name) {
		t.Fatalf("duplicate anchor error = %v", err)
	}
}

func TestResolveRejectsEveryZeroLimit(t *testing.T) {
	t.Parallel()

	base := reference.Resource{Root: mustObject(t, nil)}
	for _, mutate := range []func(*reference.Limits){
		func(limits *reference.Limits) { limits.MaxTraversalDepth = 0 },
		func(limits *reference.Limits) { limits.MaxTraversalNodes = 0 },
		func(limits *reference.Limits) { limits.MaxReferenceDepth = 0 },
	} {
		limits := reference.DefaultLimits()
		mutate(&limits)
		if _, err := reference.Resolve(
			context.Background(), base, "#", nil, limits,
		); !errors.Is(err, reference.ErrLimitExceeded) {
			t.Fatalf("zero limit error = %v", err)
		}
	}
}

func TestResolveChainHandlesTerminalAndMalformedTargets(t *testing.T) {
	t.Parallel()

	base := reference.Resource{Root: mustObject(t, []jsonvalue.Member{
		{Name: "terminal", Value: mustString(t, "done")},
		{Name: "invalid", Value: mustObject(t, []jsonvalue.Member{
			{Name: "$ref", Value: jsonvalue.Boolean(true)},
		})},
	})}
	chain, err := reference.ResolveChain(
		context.Background(), base, "#/terminal", nil,
		reference.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if chain.Circular() || len(chain.Targets()) != 1 {
		t.Fatalf("terminal chain = %#v", chain)
	}
	if _, err := reference.ResolveChain(
		context.Background(), base, "#/invalid", nil,
		reference.DefaultLimits(),
	); !errors.Is(err, reference.ErrInvalidReference) ||
		!strings.Contains(err.Error(), "at target 0") {
		t.Fatalf("invalid chained reference error = %v", err)
	}
	//nolint:staticcheck // This assertion verifies the nil-context contract.
	if _, err := reference.ResolveChain(
		//lint:ignore SA1012 This assertion verifies the nil-context contract.
		nil, base, "#/terminal", nil, reference.DefaultLimits(),
	); err == nil {
		t.Fatal("nil chain context was accepted")
	}
	limits := reference.DefaultLimits()
	limits.MaxReferenceDepth = 0
	if _, err := reference.ResolveChain(
		context.Background(), base, "#/terminal", nil, limits,
	); !errors.Is(err, reference.ErrLimitExceeded) {
		t.Fatalf("invalid chain limit error = %v", err)
	}
	if _, err := reference.ResolveChain(
		context.Background(), base, "#/missing", nil,
		reference.DefaultLimits(),
	); !errors.Is(err, reference.ErrTargetNotFound) {
		t.Fatalf("chain target error = %v", err)
	}
}

func TestResolveAnchorsAcrossArraysAndReportsAmbiguity(t *testing.T) {
	t.Parallel()

	start := mustObject(t, []jsonvalue.Member{
		{Name: "$anchor", Value: mustString(t, "start")},
		{Name: "$ref", Value: mustString(t, "#finish")},
	})
	finish := mustObject(t, []jsonvalue.Member{
		{Name: "$dynamicAnchor", Value: mustString(t, "finish")},
	})
	root := mustArray(t, []jsonvalue.Value{start, finish, mustString(t, "scalar")})
	base := reference.Resource{Root: root}
	chain, err := reference.ResolveChain(
		context.Background(), base, "#start", nil, reference.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if chain.Circular() || len(chain.Targets()) != 2 {
		t.Fatalf("anchor chain = %#v", chain)
	}
	if _, err := reference.Resolve(
		context.Background(), base, "#missing", nil, reference.DefaultLimits(),
	); !errors.Is(err, reference.ErrTargetNotFound) {
		t.Fatalf("missing anchor error = %v", err)
	}

	duplicate := reference.Resource{Root: mustArray(t, []jsonvalue.Value{finish, finish})}
	if _, err := reference.Resolve(
		context.Background(), duplicate, "#finish", nil,
		reference.DefaultLimits(),
	); !errors.Is(err, reference.ErrDuplicateAnchor) {
		t.Fatalf("duplicate anchor error = %v", err)
	}
}

func TestResolveCancelsDuringAnchorTraversal(t *testing.T) {
	t.Parallel()

	ctx := &cancelDuringTraversal{Context: context.Background()}
	_, err := reference.Resolve(
		ctx,
		reference.Resource{Root: mustObject(t, []jsonvalue.Member{
			{Name: "child", Value: mustObject(t, nil)},
		})},
		"#missing",
		nil,
		reference.DefaultLimits(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("traversal cancellation error = %v", err)
	}
}
