package reference

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

func TestResolveFragmentRejectsInvalidInternalKind(t *testing.T) {
	t.Parallel()

	_, err := resolveFragment(
		context.Background(),
		jsonvalue.Null(),
		Fragment{kind: FragmentKind(255)},
		DefaultLimits(),
	)
	if !errors.Is(err, ErrInvalidFragment) {
		t.Fatalf("invalid fragment kind error = %v", err)
	}
}

func TestOpenAPI32SelfCanonicalizationDefensiveBranches(t *testing.T) {
	t.Parallel()

	value := func(members ...jsonvalue.Member) jsonvalue.Value {
		result, err := jsonvalue.Object(members)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	text := func(raw string) jsonvalue.Value {
		result, err := jsonvalue.String(raw)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	for _, test := range []struct {
		name      string
		resource  Resource
		canonical string
	}{
		{name: "missing version", resource: Resource{Root: value()}},
		{name: "non-string version", resource: Resource{Root: value(
			jsonvalue.Member{Name: "openapi", Value: jsonvalue.Boolean(true)},
		)}},
		{name: "other version", resource: Resource{Root: value(
			jsonvalue.Member{Name: "openapi", Value: text("3.1.2")},
		)}},
		{name: "missing self", resource: Resource{Root: value(
			jsonvalue.Member{Name: "openapi", Value: text("3.2.0")},
		)}},
		{name: "non-string self", resource: Resource{Root: value(
			jsonvalue.Member{Name: "openapi", Value: text("3.2.0")},
			jsonvalue.Member{Name: "$self", Value: jsonvalue.Boolean(true)},
		)}},
		{name: "invalid base", resource: Resource{
			RetrievalURI: "https://example.test/root#fragment",
			Root: value(
				jsonvalue.Member{Name: "openapi", Value: text("3.2.0")},
				jsonvalue.Member{Name: "$self", Value: text("child")},
			),
		}},
		{name: "invalid self", resource: Resource{
			RetrievalURI: "https://example.test/root",
			Root: value(
				jsonvalue.Member{Name: "openapi", Value: text("3.2.0")},
				jsonvalue.Member{Name: "$self", Value: text("%zz")},
			),
		}},
		{name: "canonical fallback", resource: Resource{
			CanonicalURI: "https://example.test/root/openapi.json",
			Root: value(
				jsonvalue.Member{Name: "openapi", Value: text("3.2.0")},
				jsonvalue.Member{Name: "$self", Value: text("../canonical.json")},
			),
		}, canonical: "https://example.test/canonical.json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := withOpenAPI32Self(test.resource)
			want := test.canonical
			if want == "" {
				want = test.resource.CanonicalURI
			}
			if got.CanonicalURI != want {
				t.Fatalf("canonical URI = %q, want %q", got.CanonicalURI, want)
			}
		})
	}
}

func TestLimitsAcceptMinimumPositiveValues(t *testing.T) {
	t.Parallel()

	if err := (Limits{
		MaxTraversalDepth: 1, MaxTraversalNodes: 1, MaxReferenceDepth: 1,
	}).validate(); err != nil {
		t.Fatalf("minimum limits error = %v", err)
	}
}

func TestTraversalChildrenFitExactNodeAndDepthBudgets(t *testing.T) {
	t.Parallel()

	limits := Limits{
		MaxTraversalDepth: 3, MaxTraversalNodes: 6,
	}
	for _, test := range []struct {
		name     string
		children int
		visited  int
		queued   int
		depth    int
		want     bool
	}{
		{name: "leaf at limit", visited: 6, depth: 3, want: true},
		{name: "exact remaining nodes", children: 2, visited: 2, queued: 2, depth: 2, want: true},
		{name: "node overflow", children: 3, visited: 2, queued: 2, depth: 2},
		{name: "visited exhausted", children: 1, visited: 6, depth: 2},
		{name: "queue exhausted", children: 1, visited: 4, queued: 2, depth: 2},
		{name: "exact depth", children: 1, visited: 1, depth: 3},
	} {
		if got := childrenFitBudget(
			test.children, test.visited, test.queued, test.depth,
			limits.MaxTraversalNodes, limits.MaxTraversalDepth,
		); got != test.want {
			t.Fatalf("%s fit = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestItemsFitExactAndExhaustedBudgets(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		count   int
		used    int
		maximum int
		want    bool
	}{
		{name: "empty exact budget", maximum: 1, used: 1, want: true},
		{name: "exact remaining budget", count: 2, used: 1, maximum: 3, want: true},
		{name: "one beyond remaining budget", count: 2, used: 2, maximum: 3},
		{name: "already beyond budget", used: 2, maximum: 1},
	} {
		if got := itemsFitBudget(test.count, test.used, test.maximum); got != test.want {
			t.Fatalf("%s fit = %t, want %t", test.name, got, test.want)
		}
	}
}

func TestSameResourceMatchesEachIdentityContract(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		requested string
		base      Resource
		want      bool
	}{
		{name: "canonical", requested: "canonical",
			base: Resource{CanonicalURI: "canonical", RetrievalURI: "retrieval"},
			want: true},
		{name: "retrieval", requested: "retrieval",
			base: Resource{CanonicalURI: "canonical", RetrievalURI: "retrieval"},
			want: true},
		{name: "anonymous", want: true},
		{name: "requested only", requested: "requested"},
		{name: "empty against canonical", base: Resource{CanonicalURI: "canonical"}},
		{name: "empty against retrieval", base: Resource{RetrievalURI: "retrieval"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := sameResource(test.requested, test.base); got != test.want {
				t.Fatalf("sameResource() = %t, want %t", got, test.want)
			}
		})
	}
}
