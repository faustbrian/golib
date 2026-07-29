package merkletree

import (
	"context"
	"crypto/sha256"
	"math/bits"
)

// RootBuilder incrementally constructs a root while retaining only the
// logarithmic frontier of completed subtrees. It cannot generate proofs.
// RootBuilder owns its mutations, is not safe for concurrent use, and leaves
// synchronization to the caller. The zero value fails with
// ErrInvalidRootBuilder.
type RootBuilder struct {
	initialized bool
	profile     Profile
	limits      Limits
	frontier    [][sha256.Size]byte
	treeSize    uint64
	totalBytes  uint64
}

// NewRootBuilder validates profile and limits before returning an empty
// streaming builder. Options are copied by value and remain immutable.
func NewRootBuilder(
	profile Profile,
	limits Limits,
) (*RootBuilder, error) {
	if err := profile.validate(); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}

	return &RootBuilder{
		initialized: true,
		profile:     profile,
		limits:      limits,
	}, nil
}

// Append atomically appends one raw leaf. On cancellation or failure the
// builder remains unchanged.
func (builder *RootBuilder) Append(
	ctx context.Context,
	leaf RawLeaf,
) error {
	return builder.AppendBatch(ctx, []RawLeaf{leaf})
}

// AppendBatch atomically appends ordered raw leaves without retaining leaf
// bytes or the full node tree. An empty batch is a successful no-op.
func (builder *RootBuilder) AppendBatch(
	ctx context.Context,
	leaves []RawLeaf,
) error {
	if ctx == nil {
		return ErrInvalidContext
	}
	if err := builder.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	batchSize := uint64(len(leaves))
	if batchSize > builder.limits.MaxLeaves-builder.treeSize {
		return &ResourceError{
			Kind:   ResourceLeaves,
			Limit:  builder.limits.MaxLeaves,
			Actual: saturatedAdd(builder.treeSize, batchSize),
		}
	}

	totalBytes := builder.totalBytes
	for _, leaf := range leaves {
		if err := ctx.Err(); err != nil {
			return err
		}
		leafBytes := uint64(len(leaf.value))
		if leafBytes > builder.limits.MaxLeafBytes {
			return &ResourceError{
				Kind:   ResourceLeafBytes,
				Limit:  builder.limits.MaxLeafBytes,
				Actual: leafBytes,
			}
		}
		if leafBytes > builder.limits.MaxTotalBytes-totalBytes {
			return &ResourceError{
				Kind:   ResourceTotalBytes,
				Limit:  builder.limits.MaxTotalBytes,
				Actual: saturatedAdd(totalBytes, leafBytes),
			}
		}
		totalBytes += leafBytes
	}
	if batchSize == 0 {
		return nil
	}

	treeSize := builder.treeSize + batchSize
	frontier := append(
		make([][sha256.Size]byte, 0, bits.Len64(treeSize)),
		builder.frontier...,
	)
	completedSize := builder.treeSize
	for _, leaf := range leaves {
		if err := ctx.Err(); err != nil {
			return err
		}
		frontier = append(frontier, hashLeaf(leaf.value))
		completedSize++
		for mergedSize := completedSize; mergedSize&1 == 0; mergedSize >>= 1 {
			if err := ctx.Err(); err != nil {
				return err
			}
			right := frontier[len(frontier)-1]
			left := frontier[len(frontier)-2]
			frontier = frontier[:len(frontier)-2]
			frontier = append(frontier, hashBranch(left, right))
		}
	}

	builder.frontier = frontier
	builder.treeSize = treeSize
	builder.totalBytes = totalBytes

	return nil
}

// Root returns the immutable root identity at the builder's exact current
// size. It folds a copy-free read of the logarithmic frontier and does not
// mutate builder state.
func (builder *RootBuilder) Root(ctx context.Context) (Root, error) {
	if ctx == nil {
		return Root{}, ErrInvalidContext
	}
	if err := builder.validate(); err != nil {
		return Root{}, err
	}
	if err := ctx.Err(); err != nil {
		return Root{}, err
	}
	if builder.treeSize == 0 {
		empty := sha256.Sum256(nil)

		return newRoot(builder.profile, 0, empty), nil
	}

	digest := builder.frontier[len(builder.frontier)-1]
	for index := len(builder.frontier) - 2; index >= 0; index-- {
		if err := ctx.Err(); err != nil {
			return Root{}, err
		}
		digest = hashBranch(builder.frontier[index], digest)
	}

	return newRoot(builder.profile, builder.treeSize, digest), nil
}

func (builder *RootBuilder) validate() error {
	if builder == nil ||
		!builder.initialized ||
		builder.profile.validate() != nil ||
		builder.limits.validate() != nil ||
		builder.treeSize > builder.limits.MaxLeaves ||
		builder.totalBytes > builder.limits.MaxTotalBytes {
		return ErrInvalidRootBuilder
	}
	if builder.treeSize == 0 {
		if len(builder.frontier) != 0 {
			return ErrInvalidRootBuilder
		}

		return nil
	}
	if len(builder.frontier) != bits.OnesCount64(builder.treeSize) {
		return ErrInvalidRootBuilder
	}

	return nil
}
