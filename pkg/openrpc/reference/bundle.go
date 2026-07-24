package reference

import (
	"context"
	"errors"

	"github.com/faustbrian/golib/pkg/openrpc/jsonvalue"
)

// ResourceBundle is a lossless, immutable OpenRPC reference bundle. It keeps
// external resources under their absolute document URIs rather than rewriting
// JSON Schema identifiers or inventing document extension fields.
type ResourceBundle struct {
	root      jsonvalue.Value
	baseURI   string
	resources map[string]jsonvalue.Value
}

// Bundle collects every transitively referenced external document through the
// explicitly configured resolver. Root and resource bytes remain unchanged.
func Bundle(
	ctx context.Context,
	resolver *Resolver,
	root jsonvalue.Value,
	base string,
) (ResourceBundle, error) {
	if resolver == nil || ctx == nil {
		return ResourceBundle{}, ErrResolvePolicy
	}
	rootURI, err := absoluteDocumentURI(base)
	if err != nil {
		return ResourceBundle{}, err
	}
	references, _, err := resourceReferences(
		root, rootURI, resolver.policy.MaxDepth, resolver.policy.MaxReferences,
	)
	if err != nil {
		if errors.Is(err, ErrResolveLimit) {
			return ResourceBundle{}, ErrResolveLimit
		}
		return ResourceBundle{}, ErrInvalidDocument
	}
	inputs := make([]string, 0, len(references))
	for _, reference := range references {
		resolved, _ := resolveResourceReference(reference.base, reference.ref)
		inputs = append(inputs, resolved.String())
	}
	resources, err := resolver.Resources(ctx, root, rootURI, inputs)
	if err != nil {
		return ResourceBundle{}, err
	}
	return ResourceBundle{root: root, baseURI: rootURI, resources: resources}, nil
}

// Root returns the exact immutable root document value.
func (bundle ResourceBundle) Root() jsonvalue.Value { return bundle.root }

// BaseURI returns the absolute root document URI without a fragment.
func (bundle ResourceBundle) BaseURI() string { return bundle.baseURI }

// Resources returns an owned map of immutable external document values.
func (bundle ResourceBundle) Resources() map[string]jsonvalue.Value {
	resources := make(map[string]jsonvalue.Value, len(bundle.resources))
	for uri, value := range bundle.resources {
		resources[uri] = value
	}
	return resources
}

// Store returns an immutable in-memory store for deterministic offline
// resolution of the bundled external resources.
func (bundle ResourceBundle) Store() (*MemoryStore, error) {
	documents := make(map[string][]byte, len(bundle.resources))
	for uri, value := range bundle.resources {
		documents[uri] = value.Bytes()
	}
	return NewMemoryStore(documents)
}
