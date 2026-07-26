package reference

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"slices"

	"github.com/faustbrian/golib/pkg/openapi/jsonvalue"
)

// ErrExternalResolutionDisabled reports a non-local reference without an
// explicitly configured resolver.
var ErrExternalResolutionDisabled = errors.New("external reference resolution disabled")

// ErrLimitExceeded reports bounded reference traversal exhaustion.
var ErrLimitExceeded = errors.New("reference resolution limit exceeded")

// ErrDuplicateAnchor reports an ambiguous anchor within one resource.
var ErrDuplicateAnchor = errors.New("duplicate reference anchor")

// Resource keeps retrieval identity, canonical identity, and content distinct.
type Resource struct {
	RetrievalURI string
	CanonicalURI string
	Root         jsonvalue.Value
}

// Resolver retrieves one explicitly authorized resource identifier.
type Resolver interface {
	Resolve(context.Context, string) (Resource, error)
}

// ResolverFunc adapts a function to Resolver.
type ResolverFunc func(context.Context, string) (Resource, error)

// Resolve implements Resolver.
func (resolver ResolverFunc) Resolve(
	ctx context.Context,
	identifier string,
) (Resource, error) {
	return resolver(ctx, identifier)
}

// Limits bounds in-resource traversal during resolution.
type Limits struct {
	MaxTraversalDepth int
	MaxTraversalNodes int
	MaxReferenceDepth int
}

// DefaultLimits returns conservative limits suitable for untrusted documents.
func DefaultLimits() Limits {
	return Limits{
		MaxTraversalDepth: 256,
		MaxTraversalNodes: 100_000,
		MaxReferenceDepth: 128,
	}
}

func (limits Limits) validate() error {
	if limits.MaxTraversalDepth < 1 {
		return fmt.Errorf("%w: traversal depth must be positive", ErrLimitExceeded)
	}
	if limits.MaxTraversalNodes < 1 {
		return fmt.Errorf("%w: traversal nodes must be positive", ErrLimitExceeded)
	}
	if limits.MaxReferenceDepth < 1 {
		return fmt.Errorf("%w: reference depth must be positive", ErrLimitExceeded)
	}
	return nil
}

// Target is one resolved value with its complete resource provenance.
type Target struct {
	RequestedURI string
	Resource     Resource
	Fragment     Fragment
	Value        jsonvalue.Value
}

// Resolve resolves one URI-reference against a resource. It performs no
// external I/O unless resolver is non-nil and the reference leaves the base
// resource.
func Resolve(
	ctx context.Context,
	base Resource,
	rawReference string,
	resolver Resolver,
	limits Limits,
) (Target, error) {
	if ctx == nil {
		return Target{}, errors.New("resolve reference: nil context")
	}
	if err := ctx.Err(); err != nil {
		return Target{}, err
	}
	if err := limits.validate(); err != nil {
		return Target{}, err
	}
	if base.Root.Kind() == jsonvalue.InvalidKind {
		return Target{}, errors.New("resolve reference: invalid base resource")
	}
	base = withOpenAPI32Self(base)
	baseIdentifier := base.CanonicalURI
	if baseIdentifier == "" {
		baseIdentifier = base.RetrievalURI
	}
	baseURL, err := parseBaseURI(baseIdentifier)
	if err != nil {
		return Target{}, err
	}
	referenceURL, err := url.Parse(rawReference)
	if err != nil {
		return Target{}, fmt.Errorf("%w: invalid URI-reference", ErrInvalidReference)
	}
	resolved := baseURL.ResolveReference(referenceURL)
	fragment, err := parseFragment(
		resolved.EscapedFragment(), limits.MaxTraversalDepth,
	)
	if err != nil {
		return Target{}, err
	}
	resolved.Fragment = ""
	resolved.RawFragment = ""
	requested := resolved.String()
	resource := base
	if !sameResource(requested, base) {
		if resolverIsNil(resolver) {
			return Target{}, ErrExternalResolutionDisabled
		}
		resource, err = resolver.Resolve(ctx, requested)
		if err != nil {
			return Target{}, externalResolverError{cause: err}
		}
		if resource.Root.Kind() == jsonvalue.InvalidKind {
			return Target{}, errors.New("external resolver returned an invalid resource")
		}
		resource = withOpenAPI32Self(resource)
	}
	value, err := resolveFragment(ctx, resource.Root, fragment, limits)
	if err != nil {
		return Target{}, err
	}
	return Target{
		RequestedURI: requested,
		Resource:     resource,
		Fragment:     fragment,
		Value:        value,
	}, nil
}

func parseBaseURI(identifier string) (*url.URL, error) {
	if identifier == "" {
		return &url.URL{}, nil
	}
	parsed, err := url.Parse(identifier)
	if err != nil {
		return nil, errors.New("invalid base URI")
	}
	if parsed.Fragment != "" {
		return nil, errors.New("invalid base URI: fragment is not allowed")
	}
	return parsed, nil
}

type externalResolverError struct {
	cause error
}

func (resolverError externalResolverError) Error() string {
	return "resolve external resource: " + ErrResourceAccess.Error()
}

func (resolverError externalResolverError) Unwrap() []error {
	return []error{ErrResourceAccess, resolverError.cause}
}

func sameResource(requested string, base Resource) bool {
	if requested == "" {
		return base.CanonicalURI == "" && base.RetrievalURI == ""
	}
	return requested == base.CanonicalURI || requested == base.RetrievalURI
}

func withOpenAPI32Self(resource Resource) Resource {
	version, exists := resource.Root.Lookup("openapi")
	if !exists {
		return resource
	}
	rawVersion, valid := version.Text()
	if !valid || rawVersion != "3.2.0" {
		return resource
	}
	self, exists := resource.Root.Lookup("$self")
	if !exists {
		return resource
	}
	raw, valid := self.Text()
	if !valid {
		return resource
	}
	baseIdentifier := resource.RetrievalURI
	if baseIdentifier == "" {
		baseIdentifier = resource.CanonicalURI
	}
	base, err := parseBaseURI(baseIdentifier)
	if err != nil {
		return resource
	}
	reference, err := url.Parse(raw)
	if err != nil {
		return resource
	}
	resource.CanonicalURI = base.ResolveReference(reference).String()
	return resource
}

func resolveFragment(
	ctx context.Context,
	root jsonvalue.Value,
	fragment Fragment,
	limits Limits,
) (jsonvalue.Value, error) {
	switch fragment.Kind() {
	case FragmentRoot:
		return root, nil
	case FragmentPointer:
		tokens := fragment.Pointer().Tokens()
		if len(tokens) > limits.MaxTraversalDepth ||
			len(tokens)+1 > limits.MaxTraversalNodes {
			return jsonvalue.Value{}, ErrLimitExceeded
		}
		return fragment.Pointer().Evaluate(root)
	case FragmentAnchor:
		return findAnchor(ctx, root, fragment.Anchor(), limits)
	default:
		return jsonvalue.Value{}, ErrInvalidFragment
	}
}

func resolverIsNil(resolver Resolver) bool {
	if resolver == nil {
		return true
	}
	value := reflect.ValueOf(resolver)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

type traversalNode struct {
	value jsonvalue.Value
	depth int
}

func findAnchor(
	ctx context.Context,
	root jsonvalue.Value,
	name string,
	limits Limits,
) (jsonvalue.Value, error) {
	stack := []traversalNode{{value: root}}
	visited := 0
	var match jsonvalue.Value
	found := false
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return jsonvalue.Value{}, err
		}
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		visited++
		if declaresAnchor(node.value, name) {
			if found {
				return jsonvalue.Value{}, ErrDuplicateAnchor
			}
			match = node.value
			found = true
		}
		childCount, _ := node.value.Length()
		if !childrenFitBudget(
			childCount, visited, len(stack), node.depth,
			limits.MaxTraversalNodes, limits.MaxTraversalDepth,
		) {
			return jsonvalue.Value{}, ErrLimitExceeded
		}
		children := childValues(node.value)
		for _, child := range slices.Backward(children) {
			stack = append(stack, traversalNode{
				value: child,
				depth: node.depth + 1,
			})
		}
	}
	if !found {
		return jsonvalue.Value{}, ErrTargetNotFound
	}
	return match, nil
}

func childrenFitBudget(
	childCount int,
	visited int,
	queued int,
	depth int,
	maxNodes int,
	maxDepth int,
) bool {
	if childCount == 0 {
		return true
	}
	if depth >= maxDepth {
		return false
	}
	return itemsFitBudget(childCount, visited+queued, maxNodes)
}

func itemsFitBudget(count int, used int, maximum int) bool {
	if used > maximum {
		return false
	}
	return count <= maximum-used
}

func declaresAnchor(value jsonvalue.Value, name string) bool {
	if value.Kind() != jsonvalue.ObjectKind {
		return false
	}
	for _, keyword := range []string{"$anchor", "$dynamicAnchor"} {
		declaration, exists := value.Lookup(keyword)
		if !exists {
			continue
		}
		declared, ok := declaration.Text()
		if ok && declared == name {
			return true
		}
	}
	return false
}

func childValues(value jsonvalue.Value) []jsonvalue.Value {
	switch value.Kind() {
	case jsonvalue.ArrayKind:
		values, _ := value.Elements()
		return values
	case jsonvalue.ObjectKind:
		members, _ := value.Members()
		values := make([]jsonvalue.Value, len(members))
		for index, member := range members {
			values[index] = member.Value
		}
		return values
	default:
		return nil
	}
}
