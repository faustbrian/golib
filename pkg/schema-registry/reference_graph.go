package schemaregistry

import (
	"context"
	"errors"
	"fmt"
)

// ReferenceCoordinate identifies a provider subject version. It is not a
// portable schema identity.
type ReferenceCoordinate struct {
	Subject Subject
	Version Version
}

// ProviderReference is a named edge in a provider-coordinate graph.
type ProviderReference struct {
	Name   string
	Target ReferenceCoordinate
}

// ReferenceDocument is one provider-coordinate graph node.
type ReferenceDocument struct {
	Coordinate ReferenceCoordinate
	References []ProviderReference
}

// ReferenceResolver explicitly retrieves graph nodes. Implementations may use
// I/O only during BuildReferenceGraph; compiled schemas and bundles never call
// it implicitly.
type ReferenceResolver interface {
	ResolveReference(context.Context, ReferenceCoordinate) (ReferenceDocument, error)
}

// ReferenceGraph is an immutable validated provider-coordinate graph.
type ReferenceGraph struct {
	documents []ReferenceDocument
}

// BuildReferenceGraph resolves and validates a bounded graph synchronously.
func BuildReferenceGraph(
	ctx context.Context,
	roots []ReferenceCoordinate,
	resolver ReferenceResolver,
	limits GraphLimits,
) (ReferenceGraph, error) {
	if err := ctx.Err(); err != nil {
		return ReferenceGraph{}, err
	}
	if len(roots) == 0 || interfaceIsNil(resolver) {
		return ReferenceGraph{}, fmt.Errorf("%w: roots and resolver are required", ErrInvalidRequest)
	}
	if limits.MaxSchemas <= 0 || limits.MaxDepth <= 0 || limits.MaxReferences <= 0 {
		return ReferenceGraph{}, fmt.Errorf("%w: invalid graph limits", ErrInvalidRequest)
	}

	state := make(map[ReferenceCoordinate]uint8, limits.MaxSchemas)
	documents := make([]ReferenceDocument, 0, limits.MaxSchemas)
	referenceCount := 0
	var visit func(ReferenceCoordinate, int) error
	visit = func(coordinate ReferenceCoordinate, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := coordinate.validate(); err != nil {
			return err
		}
		if depth > limits.MaxDepth {
			return fmt.Errorf("%w: depth", ErrReferenceLimit)
		}
		switch state[coordinate] {
		case 1:
			return fmt.Errorf("%w: %s", ErrReferenceCycle, coordinate.Subject.Name)
		case 2:
			return nil
		}
		if len(documents) >= limits.MaxSchemas {
			return fmt.Errorf("%w: schemas", ErrReferenceLimit)
		}
		state[coordinate] = 1
		document, err := resolver.ResolveReference(ctx, coordinate)
		if err != nil {
			if errors.Is(err, ErrNotFound) || errors.Is(err, ErrReferenceMissing) {
				return fmt.Errorf("%w: %s: %w", ErrReferenceMissing, coordinate.Subject.Name, err)
			}
			return fmt.Errorf("resolve reference %s: %w", coordinate.Subject.Name, err)
		}
		if document.Coordinate != coordinate {
			return fmt.Errorf("%w: resolver coordinate mismatch", ErrInvalidSchema)
		}
		owned := cloneReferenceDocument(document)
		documents = append(documents, owned)
		for _, reference := range owned.References {
			if reference.Name == "" {
				return fmt.Errorf("%w: empty reference name", ErrInvalidSchema)
			}
			referenceCount++
			if referenceCount > limits.MaxReferences {
				return fmt.Errorf("%w: references", ErrReferenceLimit)
			}
			if err := visit(reference.Target, depth+1); err != nil {
				return err
			}
		}
		state[coordinate] = 2
		return nil
	}

	for _, root := range roots {
		if err := visit(root, 1); err != nil {
			return ReferenceGraph{}, err
		}
	}
	return ReferenceGraph{documents: documents}, nil
}

// Documents returns a deep copy in deterministic first-visit order.
func (graph ReferenceGraph) Documents() []ReferenceDocument {
	documents := make([]ReferenceDocument, len(graph.documents))
	for index, document := range graph.documents {
		documents[index] = cloneReferenceDocument(document)
	}
	return documents
}

func (coordinate ReferenceCoordinate) validate() error {
	if coordinate.Subject.Name == "" || !coordinate.Version.valid() {
		return fmt.Errorf("%w: reference coordinate", ErrInvalidRequest)
	}
	return nil
}

func cloneReferenceDocument(document ReferenceDocument) ReferenceDocument {
	return ReferenceDocument{
		Coordinate: document.Coordinate,
		References: append([]ProviderReference(nil), document.References...),
	}
}
