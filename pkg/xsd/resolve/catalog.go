package resolve

import (
	"context"
	"fmt"
	"net/url"
)

// Catalog maps import namespaces without schema locations to absolute resource
// identities, then delegates byte loading to another explicit resolver.
type Catalog struct {
	namespaces map[string]string
	resolver   Resolver
}

// NewCatalog validates and copies namespace mappings. The delegated resolver
// remains responsible for the mapped resource capability and byte ownership.
func NewCatalog(namespaces map[string]string, resolver Resolver) (*Catalog, error) {
	if resolver == nil {
		return nil, fmt.Errorf("xsd resolve: catalog resolver is required")
	}
	owned := make(map[string]string, len(namespaces))
	for namespace, identity := range namespaces {
		uri, err := url.Parse(identity)
		if err != nil || !uri.IsAbs() || uri.Fragment != "" {
			return nil, fmt.Errorf("xsd resolve: invalid catalog URI %q", identity)
		}
		owned[namespace] = uri.String()
	}
	return &Catalog{namespaces: owned, resolver: resolver}, nil
}

// Resolve delegates explicit identities unchanged and maps locationless
// imports by their requested namespace.
func (c *Catalog) Resolve(ctx context.Context, request Request) (Resource, error) {
	if err := ctx.Err(); err != nil {
		return Resource{}, err
	}
	if request.URI != "" {
		return c.resolver.Resolve(ctx, request)
	}
	if request.Kind != KindImport {
		return Resource{}, fmt.Errorf("%w: locationless %s", ErrNotFound, request.Kind)
	}
	identity, ok := c.namespaces[request.Namespace]
	if !ok {
		return Resource{}, fmt.Errorf("%w: namespace %s", ErrNotFound, request.Namespace)
	}
	request.URI = identity
	resource, err := c.resolver.Resolve(ctx, request)
	if err != nil {
		return Resource{}, err
	}
	if resource.URI != identity {
		return Resource{}, fmt.Errorf(
			"xsd resolve: catalog requested %q, received %q",
			identity,
			resource.URI,
		)
	}
	return resource, nil
}
