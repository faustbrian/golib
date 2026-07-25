package reference

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// ErrInvalidReference reports a $ref value that is not a string.
var ErrInvalidReference = errors.New("invalid reference value")

// Chain is an immutable sequence of recursively resolved reference targets.
// A circular chain ends immediately before its first repeated target.
type Chain struct {
	targets  []Target
	circular bool
}

// Targets returns a caller-owned copy in resolution order.
func (chain Chain) Targets() []Target {
	return append([]Target(nil), chain.targets...)
}

// Circular reports whether resolution stopped at a previously visited target.
func (chain Chain) Circular() bool {
	return chain.circular
}

// ResolveChain follows consecutive object $ref values with bounded depth.
// Legal cycles are reported in the result rather than treated as failures.
func ResolveChain(
	ctx context.Context,
	base Resource,
	rawReference string,
	resolver Resolver,
	limits Limits,
) (Chain, error) {
	if ctx == nil {
		return Chain{}, errors.New("resolve reference chain: nil context")
	}
	if err := limits.validate(); err != nil {
		return Chain{}, err
	}
	seen := make(map[string]struct{})
	chain := Chain{}
	currentBase := base
	currentReference := rawReference
	for {
		target, err := Resolve(ctx, currentBase, currentReference, resolver, limits)
		if err != nil {
			return Chain{}, err
		}
		identity := targetIdentity(target)
		if _, duplicate := seen[identity]; duplicate {
			chain.circular = true
			return chain, nil
		}
		seen[identity] = struct{}{}
		chain.targets = append(chain.targets, target)

		next, exists := target.Value.Lookup("$ref")
		if !exists {
			return chain, nil
		}
		raw, ok := next.Text()
		if !ok {
			return Chain{}, fmt.Errorf(
				"%w at target %d",
				ErrInvalidReference,
				len(chain.targets)-1,
			)
		}
		if len(chain.targets) >= limits.MaxReferenceDepth {
			return Chain{}, ErrLimitExceeded
		}
		currentBase = target.Resource
		currentReference = raw
	}
}

func targetIdentity(target Target) string {
	resource := target.Resource.CanonicalURI
	if resource == "" {
		resource = target.Resource.RetrievalURI
	}
	if resource == "" {
		resource = target.RequestedURI
	}
	fragment := target.Fragment.Pointer().String()
	if target.Fragment.Kind() == FragmentAnchor {
		fragment = target.Fragment.Anchor()
	}
	return resource + "\x00" + strconv.Itoa(int(target.Fragment.Kind())) + "\x00" + fragment
}
