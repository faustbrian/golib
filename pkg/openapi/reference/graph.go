package reference

import (
	"context"
	"fmt"
)

// Resolution connects one source occurrence to its recursively resolved
// target chain.
type Resolution struct {
	occurrence Occurrence
	chain      Chain
}

// Occurrence returns the source $ref member.
func (resolution Resolution) Occurrence() Occurrence {
	return resolution.occurrence
}

// Chain returns the immutable resolved target chain.
func (resolution Resolution) Chain() Chain {
	return resolution.chain
}

// ResolveAll inventories and resolves every $ref member in source order. It
// performs external I/O only through the supplied resolver.
func ResolveAll(
	ctx context.Context,
	resource Resource,
	resolver Resolver,
	limits Limits,
) ([]Resolution, error) {
	occurrences, err := Scan(ctx, resource.Root, limits)
	if err != nil {
		return nil, err
	}
	resolutions := make([]Resolution, 0, len(occurrences))
	for _, occurrence := range occurrences {
		chain, err := ResolveChain(
			ctx,
			resource,
			occurrence.Raw(),
			resolver,
			limits,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"resolve reference at %s: %w",
				occurrence.Pointer().String(),
				err,
			)
		}
		resolutions = append(resolutions, Resolution{
			occurrence: occurrence,
			chain:      chain,
		})
	}
	return resolutions, nil
}
