package resolve

import (
	"context"
	"errors"
	"fmt"
	"net/url"
)

var (
	// ErrAccessDenied is returned by resolvers that intentionally prohibit a
	// resource class. Deny returns this error for every request.
	ErrAccessDenied = errors.New("xsd resolve: access denied")
	// ErrNotFound means a resolver does not contain the requested identity.
	ErrNotFound = errors.New("xsd resolve: resource not found")
)

// Kind identifies the composition operation requesting a resource.
type Kind string

const (
	KindInclude  Kind = "include"
	KindImport   Kind = "import"
	KindRedefine Kind = "redefine"
)

// Request identifies a resource after base-URI resolution.
type Request struct {
	URI       string
	Namespace string
	Kind      Kind
}

// Resource is an owned schema resource. Resolver implementations must not
// reuse Content storage across calls.
type Resource struct {
	URI     string
	Content []byte
}

// Resolver obtains explicitly configured schema resources.
type Resolver interface {
	Resolve(context.Context, Request) (Resource, error)
}

// Deny returns the secure default resolver, which rejects every request.
func Deny() Resolver { return denyResolver{} }

type denyResolver struct{}

func (denyResolver) Resolve(ctx context.Context, request Request) (Resource, error) {
	if err := ctx.Err(); err != nil {
		return Resource{}, err
	}
	return Resource{}, fmt.Errorf("%w: %s", ErrAccessDenied, request.URI)
}

// Memory is an immutable collection of caller-supplied resources.
type Memory struct {
	resources map[string][]byte
}

// NewMemory copies resources into an immutable resolver. Keys must be valid,
// absolute resource URIs without fragments.
func NewMemory(resources map[string][]byte) (*Memory, error) {
	owned := make(map[string][]byte, len(resources))
	for identity, content := range resources {
		uri, err := url.Parse(identity)
		if err != nil || !uri.IsAbs() || uri.Fragment != "" {
			return nil, fmt.Errorf("xsd resolve: invalid resource URI %q", identity)
		}
		owned[uri.String()] = append([]byte(nil), content...)
	}
	return &Memory{resources: owned}, nil
}

// Resolve returns a fresh copy of the configured resource bytes.
func (r *Memory) Resolve(ctx context.Context, request Request) (Resource, error) {
	if err := ctx.Err(); err != nil {
		return Resource{}, err
	}
	content, ok := r.resources[request.URI]
	if !ok {
		return Resource{}, fmt.Errorf("%w: %s", ErrNotFound, request.URI)
	}
	return Resource{URI: request.URI, Content: append([]byte(nil), content...)}, nil
}

// Chain returns a resolver that tries children in order. Only ErrNotFound
// advances to the next resolver; access denial and operational errors stop.
func Chain(resolvers ...Resolver) Resolver {
	return chain{resolvers: append([]Resolver(nil), resolvers...)}
}

type chain struct {
	resolvers []Resolver
}

func (c chain) Resolve(ctx context.Context, request Request) (Resource, error) {
	for _, resolver := range c.resolvers {
		if resolver == nil {
			continue
		}
		resource, err := resolver.Resolve(ctx, request)
		if err == nil {
			return resource, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return Resource{}, err
		}
	}
	return Resource{}, fmt.Errorf("%w: %s", ErrNotFound, request.URI)
}
