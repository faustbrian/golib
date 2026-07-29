package merkletree

import (
	"context"
	"crypto/sha256"
	"math/bits"
)

// Builder incrementally constructs one ordered Merkle tree while retaining
// sufficient nodes for snapshots and proofs. A Builder owns its mutations and
// is not safe for concurrent use. Snapshot values returned by Builder are
// immutable and remain safe for concurrent read-only use after later appends.
// The zero value fails with ErrInvalidBuilder.
type Builder struct {
	initialized bool
	profile     Profile
	limits      SnapshotLimits
	nodes       []snapshotNode
	frontier    []uint64
	treeSize    uint64
	totalBytes  uint64
}

// NewBuilder validates profile and limits before returning an empty mutable
// builder. Options are copied by value and cannot change after construction.
func NewBuilder(profile Profile, limits SnapshotLimits) (*Builder, error) {
	if err := profile.validate(); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}

	return &Builder{
		initialized: true,
		profile:     profile,
		limits:      limits,
	}, nil
}

// Append atomically appends one raw leaf. On cancellation, invalid input, or a
// resource-limit failure, the builder remains unchanged.
func (builder *Builder) Append(ctx context.Context, leaf RawLeaf) error {
	return builder.AppendBatch(ctx, []RawLeaf{leaf})
}

// AppendBatch atomically appends ordered raw leaves. It validates the complete
// batch before modifying builder state. An empty batch is a successful no-op.
func (builder *Builder) AppendBatch(
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
	if batchSize > builder.limits.Construction.MaxLeaves-builder.treeSize {
		return &ResourceError{
			Kind:   ResourceLeaves,
			Limit:  builder.limits.Construction.MaxLeaves,
			Actual: saturatedAdd(builder.treeSize, batchSize),
		}
	}

	totalBytes := builder.totalBytes
	for _, leaf := range leaves {
		if err := ctx.Err(); err != nil {
			return err
		}
		leafBytes := uint64(len(leaf.value))
		if leafBytes > builder.limits.Construction.MaxLeafBytes {
			return &ResourceError{
				Kind:   ResourceLeafBytes,
				Limit:  builder.limits.Construction.MaxLeafBytes,
				Actual: leafBytes,
			}
		}
		if leafBytes >
			builder.limits.Construction.MaxTotalBytes-totalBytes {
			return &ResourceError{
				Kind:  ResourceTotalBytes,
				Limit: builder.limits.Construction.MaxTotalBytes,
				Actual: saturatedAdd(
					totalBytes,
					leafBytes,
				),
			}
		}
		totalBytes += leafBytes
	}

	treeSize := builder.treeSize + batchSize
	nodeCount := snapshotNodeCount(treeSize)
	if nodeCount > builder.limits.MaxRetainedNodes {
		return &ResourceError{
			Kind:   ResourceRetainedNodes,
			Limit:  builder.limits.MaxRetainedNodes,
			Actual: nodeCount,
		}
	}
	if batchSize == 0 {
		return nil
	}

	frontier := append(
		make([]uint64, 0, bits.Len64(treeSize)),
		builder.frontier...,
	)
	baseNode := uint64(len(builder.nodes))
	newNodes := make([]snapshotNode, 0, len(leaves))
	completedSize := builder.treeSize
	for _, leaf := range leaves {
		if err := ctx.Err(); err != nil {
			return err
		}

		newNodes = append(newNodes, snapshotNode{
			digest: hashLeaf(leaf.value),
			size:   1,
			left:   noSnapshotNode,
			right:  noSnapshotNode,
		})
		frontier = append(frontier, baseNode+uint64(len(newNodes)-1))
		completedSize++
		for mergedSize := completedSize; mergedSize&1 == 0; mergedSize >>= 1 {
			if err := ctx.Err(); err != nil {
				return err
			}

			right := frontier[len(frontier)-1]
			left := frontier[len(frontier)-2]
			frontier = frontier[:len(frontier)-2]
			newNodes = append(
				newNodes,
				newBuilderBranch(builder.nodes, newNodes, baseNode, left, right),
			)
			frontier = append(
				frontier,
				baseNode+uint64(len(newNodes)-1),
			)
		}
	}

	builder.nodes = append(builder.nodes, newNodes...)
	builder.frontier = frontier
	builder.treeSize = treeSize
	builder.totalBytes = totalBytes

	return nil
}

func newBuilderBranch(
	existing []snapshotNode,
	added []snapshotNode,
	base uint64,
	left uint64,
	right uint64,
) snapshotNode {
	node := func(index uint64) snapshotNode {
		if index < base {
			return existing[index]
		}

		return added[index-base]
	}
	leftNode := node(left)
	rightNode := node(right)

	return snapshotNode{
		digest: hashBranch(leftNode.digest, rightNode.digest),
		size:   leftNode.size + rightNode.size,
		left:   left,
		right:  right,
	}
}

// Snapshot returns an immutable copy at the builder's exact current tree size.
// It checks ctx while copying retained nodes and never aliases mutable builder
// storage.
func (builder *Builder) Snapshot(ctx context.Context) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, ErrInvalidContext
	}
	if err := builder.validate(); err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	if builder.treeSize == 0 {
		empty := sha256.Sum256(nil)

		return Snapshot{
			profile:    builder.profile,
			root:       newRoot(builder.profile, 0, empty),
			rootNode:   noSnapshotNode,
			totalBytes: builder.totalBytes,
		}, nil
	}

	nodes := make([]snapshotNode, len(builder.nodes), int(
		snapshotNodeCount(builder.treeSize),
	))
	for index, node := range builder.nodes {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		nodes[index] = node
	}
	frontier := make([]uint64, len(builder.frontier))
	copy(frontier, builder.frontier)

	rootNode := frontier[len(frontier)-1]
	for index := len(frontier) - 2; index >= 0; index-- {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		nodes = append(nodes, newSnapshotBranch(nodes, frontier[index], rootNode))
		rootNode = uint64(len(nodes) - 1)
	}
	rootDigest := nodes[rootNode].digest

	return Snapshot{
		profile:    builder.profile,
		root:       newRoot(builder.profile, builder.treeSize, rootDigest),
		nodes:      nodes,
		rootNode:   rootNode,
		totalBytes: builder.totalBytes,
	}, nil
}

func (builder *Builder) validate() error {
	if builder == nil ||
		!builder.initialized ||
		builder.profile.validate() != nil ||
		builder.limits.validate() != nil ||
		builder.treeSize > builder.limits.Construction.MaxLeaves ||
		builder.totalBytes > builder.limits.Construction.MaxTotalBytes ||
		snapshotNodeCount(builder.treeSize) >
			builder.limits.MaxRetainedNodes {
		return ErrInvalidBuilder
	}
	if builder.treeSize == 0 {
		if len(builder.nodes) != 0 || len(builder.frontier) != 0 {
			return ErrInvalidBuilder
		}

		return nil
	}
	if len(builder.nodes) == 0 ||
		len(builder.frontier) != bits.OnesCount64(builder.treeSize) {
		return ErrInvalidBuilder
	}
	for _, nodeIndex := range builder.frontier {
		if nodeIndex >= uint64(len(builder.nodes)) {
			return ErrInvalidBuilder
		}
	}

	return nil
}
