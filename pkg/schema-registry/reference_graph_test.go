package schemaregistry_test

import (
	"context"
	"errors"
	"testing"

	schemaregistry "github.com/faustbrian/golib/pkg/schema-registry"
)

type referenceResolverFunc func(
	context.Context,
	schemaregistry.ReferenceCoordinate,
) (schemaregistry.ReferenceDocument, error)

func (fn referenceResolverFunc) ResolveReference(
	ctx context.Context,
	coordinate schemaregistry.ReferenceCoordinate,
) (schemaregistry.ReferenceDocument, error) {
	return fn(ctx, coordinate)
}

func TestBuildReferenceGraphDetectsCyclesAndMissingReferences(t *testing.T) {
	t.Parallel()

	a := schemaregistry.ReferenceCoordinate{Subject: schemaregistry.Subject{Name: "a"}, Version: schemaregistry.Version{Number: 1}}
	b := schemaregistry.ReferenceCoordinate{Subject: schemaregistry.Subject{Name: "b"}, Version: schemaregistry.Version{Number: 1}}
	documents := map[schemaregistry.ReferenceCoordinate]schemaregistry.ReferenceDocument{
		a: {Coordinate: a, References: []schemaregistry.ProviderReference{{Name: "b.proto", Target: b}}},
		b: {Coordinate: b, References: []schemaregistry.ProviderReference{{Name: "a.proto", Target: a}}},
	}
	resolver := referenceResolverFunc(func(
		_ context.Context,
		coordinate schemaregistry.ReferenceCoordinate,
	) (schemaregistry.ReferenceDocument, error) {
		document, found := documents[coordinate]
		if !found {
			return schemaregistry.ReferenceDocument{}, schemaregistry.ErrReferenceMissing
		}
		return document, nil
	})

	_, err := schemaregistry.BuildReferenceGraph(
		context.Background(),
		[]schemaregistry.ReferenceCoordinate{a},
		resolver,
		schemaregistry.GraphLimits{MaxSchemas: 4, MaxDepth: 4, MaxReferences: 4},
	)
	if !errors.Is(err, schemaregistry.ErrReferenceCycle) {
		t.Fatalf("BuildReferenceGraph(cycle) error = %v, want ErrReferenceCycle", err)
	}

	delete(documents, b)
	_, err = schemaregistry.BuildReferenceGraph(
		context.Background(),
		[]schemaregistry.ReferenceCoordinate{a},
		resolver,
		schemaregistry.GraphLimits{MaxSchemas: 4, MaxDepth: 4, MaxReferences: 4},
	)
	if !errors.Is(err, schemaregistry.ErrReferenceMissing) {
		t.Fatalf("BuildReferenceGraph(missing) error = %v, want ErrReferenceMissing", err)
	}
}

func TestBuildReferenceGraphIsBoundedDeterministicAndCancelable(t *testing.T) {
	t.Parallel()

	root := schemaregistry.ReferenceCoordinate{Subject: schemaregistry.Subject{Name: "root"}, Version: schemaregistry.Version{Number: 1}}
	leaf := schemaregistry.ReferenceCoordinate{Subject: schemaregistry.Subject{Name: "leaf"}, Version: schemaregistry.Version{Number: 2}}
	resolver := referenceResolverFunc(func(
		_ context.Context,
		coordinate schemaregistry.ReferenceCoordinate,
	) (schemaregistry.ReferenceDocument, error) {
		if coordinate == root {
			return schemaregistry.ReferenceDocument{
				Coordinate: root,
				References: []schemaregistry.ProviderReference{{Name: "leaf", Target: leaf}},
			}, nil
		}
		return schemaregistry.ReferenceDocument{Coordinate: leaf}, nil
	})

	graph, err := schemaregistry.BuildReferenceGraph(
		context.Background(),
		[]schemaregistry.ReferenceCoordinate{root},
		resolver,
		schemaregistry.GraphLimits{MaxSchemas: 2, MaxDepth: 2, MaxReferences: 1},
	)
	if err != nil {
		t.Fatalf("BuildReferenceGraph() error = %v", err)
	}
	documents := graph.Documents()
	if len(documents) != 2 || documents[0].Coordinate != root || documents[1].Coordinate != leaf {
		t.Fatalf("Documents() = %+v, want root then leaf", documents)
	}
	documents[0].References[0].Name = "mutated"
	if graph.Documents()[0].References[0].Name != "leaf" {
		t.Fatal("Documents() returned aliased references")
	}

	_, err = schemaregistry.BuildReferenceGraph(
		context.Background(),
		[]schemaregistry.ReferenceCoordinate{root},
		resolver,
		schemaregistry.GraphLimits{MaxSchemas: 1, MaxDepth: 2, MaxReferences: 1},
	)
	if !errors.Is(err, schemaregistry.ErrReferenceLimit) {
		t.Fatalf("BuildReferenceGraph(limit) error = %v, want ErrReferenceLimit", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = schemaregistry.BuildReferenceGraph(
		ctx,
		[]schemaregistry.ReferenceCoordinate{root},
		resolver,
		schemaregistry.GraphLimits{MaxSchemas: 2, MaxDepth: 2, MaxReferences: 1},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildReferenceGraph(canceled) error = %v, want context.Canceled", err)
	}
}

func TestBuildReferenceGraphPreservesNonAbsenceResolverFailures(t *testing.T) {
	t.Parallel()

	root := schemaregistry.ReferenceCoordinate{Subject: schemaregistry.Subject{Name: "root"}, Version: schemaregistry.Version{Number: 1}}
	_, err := schemaregistry.BuildReferenceGraph(
		context.Background(),
		[]schemaregistry.ReferenceCoordinate{root},
		referenceResolverFunc(func(context.Context, schemaregistry.ReferenceCoordinate) (schemaregistry.ReferenceDocument, error) {
			return schemaregistry.ReferenceDocument{}, schemaregistry.ErrUnavailable
		}),
		schemaregistry.GraphLimits{MaxSchemas: 1, MaxDepth: 1, MaxReferences: 1},
	)
	if !errors.Is(err, schemaregistry.ErrUnavailable) || errors.Is(err, schemaregistry.ErrReferenceMissing) {
		t.Fatalf("BuildReferenceGraph() error = %v, want only ErrUnavailable", err)
	}
}

func TestBuildReferenceGraphRejectsAmbiguousVersionIdentity(t *testing.T) {
	t.Parallel()

	coordinate := schemaregistry.ReferenceCoordinate{
		Subject: schemaregistry.Subject{Name: "root"},
		Version: schemaregistry.Version{Number: 1, Opaque: "one"},
	}
	called := false
	_, err := schemaregistry.BuildReferenceGraph(
		context.Background(), []schemaregistry.ReferenceCoordinate{coordinate},
		referenceResolverFunc(func(context.Context, schemaregistry.ReferenceCoordinate) (schemaregistry.ReferenceDocument, error) {
			called = true
			return schemaregistry.ReferenceDocument{}, nil
		}),
		schemaregistry.GraphLimits{MaxSchemas: 1, MaxDepth: 1, MaxReferences: 1},
	)
	if !errors.Is(err, schemaregistry.ErrInvalidRequest) || called {
		t.Fatalf("BuildReferenceGraph() = %v, called=%t", err, called)
	}
}
