package validate

import (
	"context"
	"testing"

	"github.com/faustbrian/golib/pkg/openapi/reference"
	"github.com/faustbrian/golib/pkg/openapi/specversion"
)

func TestExternalOperationCollectorDeduplicatesAnchorTargets(t *testing.T) {
	t.Parallel()

	shared, err := reference.ParseFragment("Shared")
	if err != nil {
		t.Fatal(err)
	}
	other, err := reference.ParseFragment("Other")
	if err != nil {
		t.Fatal(err)
	}
	collector := externalOperationCollector{seen: make(map[string]struct{})}
	target := reference.Target{
		Resource: reference.Resource{CanonicalURI: "https://example.test/api"},
		Fragment: shared,
	}
	if collector.visited(target) {
		t.Fatal("first anchor target was already visited")
	}
	if collector.visited(reference.Target{
		Resource: target.Resource,
		Fragment: other,
	}) {
		t.Fatal("distinct anchor target was deduplicated")
	}
	if !collector.visited(target) {
		t.Fatal("repeated anchor target was not deduplicated")
	}
}

func TestExternalOperationCollectorSkipsRepeatedPathItemTarget(t *testing.T) {
	t.Parallel()

	target := reference.Resource{
		CanonicalURI: "https://example.test/path-item.json",
		Root:         testValidationValue(t, `{"get":{"responses":{}}}`),
	}
	collector := externalOperationCollector{
		ctx:     context.Background(),
		dialect: specversion.DialectOAS31,
		resolver: reference.ResolverFunc(func(
			context.Context, string,
		) (reference.Resource, error) {
			return target, nil
		}),
		limits: reference.DefaultLimits(),
		seen:   make(map[string]struct{}),
	}
	pathItem := testValidationValue(t, `{"$ref":"https://example.test/path-item.json"}`)
	base := reference.Resource{Root: testValidationValue(t, `{}`)}
	collector.pathItemReference(base, pathItem, "/paths/~1value")
	count := len(collector.operations)
	collector.pathItemReference(base, pathItem, "/paths/~1value")
	if len(collector.operations) != count {
		t.Fatalf("repeated path item added operations: %d -> %d", count, len(collector.operations))
	}
	if count == 0 {
		t.Fatal("first path item did not add an operation")
	}
}

func TestExternalOperationCollectorUsesCanonicalResourceIdentity(t *testing.T) {
	t.Parallel()

	collector := externalOperationCollector{seen: make(map[string]struct{})}
	first := reference.Target{
		RequestedURI: "https://one.example.test/api",
		Resource: reference.Resource{
			CanonicalURI: "https://canonical.example.test/api",
			RetrievalURI: "https://one.example.test/api",
		},
	}
	second := reference.Target{
		RequestedURI: "https://two.example.test/api",
		Resource: reference.Resource{
			CanonicalURI: "https://canonical.example.test/api",
			RetrievalURI: "https://two.example.test/api",
		},
	}
	if collector.visited(first) || !collector.visited(second) {
		t.Fatal("canonical aliases were not deduplicated")
	}
}

func TestExternalOperationCollectorUsesRetrievalResourceIdentity(t *testing.T) {
	t.Parallel()

	collector := externalOperationCollector{seen: make(map[string]struct{})}
	first := reference.Target{
		RequestedURI: "https://example.test/request-one",
		Resource: reference.Resource{
			RetrievalURI: "https://example.test/retrieved",
		},
	}
	second := reference.Target{
		RequestedURI: "https://example.test/request-two",
		Resource: reference.Resource{
			RetrievalURI: "https://example.test/retrieved",
		},
	}
	if collector.visited(first) || !collector.visited(second) {
		t.Fatal("retrieval aliases were not deduplicated")
	}
}
