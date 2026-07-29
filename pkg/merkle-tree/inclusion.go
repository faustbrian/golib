package merkletree

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"math/bits"
)

const defaultMaxProofElements = uint64(64)

const defaultMaxRetainedNodes = uint64(1<<20 - 1)

// ProofLimits bounds work derived from proof metadata. Every field must be
// nonzero. ProofLimits is copied by value and is immutable during an
// operation.
type ProofLimits struct {
	MaxElements       uint64
	MaxTraversalDepth uint64
	MaxLeafBytes      uint64
}

// DefaultProofLimits permits at most 64 sibling elements and 64 traversal
// levels, sufficient for every uint64-indexed binary tree.
func DefaultProofLimits() ProofLimits {
	return ProofLimits{
		MaxElements:       defaultMaxProofElements,
		MaxTraversalDepth: defaultMaxProofElements,
		MaxLeafBytes:      defaultMaxLeafBytes,
	}
}

func (limits ProofLimits) validate() error {
	if limits.MaxElements == 0 ||
		limits.MaxTraversalDepth == 0 ||
		limits.MaxLeafBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

// SnapshotLimits bounds both raw-leaf construction and the immutable node
// tree retained for later proof generation. Every field must be valid.
type SnapshotLimits struct {
	Construction     Limits
	MaxRetainedNodes uint64
}

// DefaultSnapshotLimits permits at most 2^19 leaves and 2^20-1 retained
// nodes, while retaining the default per-leaf and aggregate byte bounds.
func DefaultSnapshotLimits() SnapshotLimits {
	construction := DefaultLimits()
	construction.MaxLeaves = (defaultMaxRetainedNodes + 1) / 2

	return SnapshotLimits{
		Construction:     construction,
		MaxRetainedNodes: defaultMaxRetainedNodes,
	}
}

func (limits SnapshotLimits) validate() error {
	if err := limits.Construction.validate(); err != nil {
		return err
	}
	if limits.MaxRetainedNodes == 0 {
		return ErrInvalidLimits
	}
	if limits.MaxRetainedNodes > uint64(^uint(0)>>1) {
		return ErrInvalidLimits
	}

	return nil
}

const noSnapshotNode = ^uint64(0)

// Snapshot is an immutable tree-size snapshot retaining its node tree for
// logarithmic proof generation. It is safe for concurrent read-only use. The
// zero value fails with ErrInvalidSnapshot.
type Snapshot struct {
	profile  Profile
	root     Root
	nodes    []snapshotNode
	rootNode uint64
}

type snapshotNode struct {
	digest [sha256.Size]byte
	size   uint64
	left   uint64
	right  uint64
}

// NewSnapshot constructs an immutable snapshot from ordered raw leaves.
// Caller-owned bytes are not retained.
func NewSnapshot(
	ctx context.Context,
	profile Profile,
	leaves []RawLeaf,
	limits SnapshotLimits,
) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, ErrInvalidContext
	}
	if err := limits.validate(); err != nil {
		return Snapshot{}, err
	}
	treeSize := uint64(len(leaves))
	if treeSize > limits.Construction.MaxLeaves {
		return Snapshot{}, &ResourceError{
			Kind:   ResourceLeaves,
			Limit:  limits.Construction.MaxLeaves,
			Actual: treeSize,
		}
	}
	nodeCount := snapshotNodeCount(treeSize)
	if nodeCount > limits.MaxRetainedNodes {
		return Snapshot{}, &ResourceError{
			Kind:   ResourceRetainedNodes,
			Limit:  limits.MaxRetainedNodes,
			Actual: nodeCount,
		}
	}

	root, err := ComputeRoot(ctx, profile, leaves, limits.Construction)
	if err != nil {
		return Snapshot{}, err
	}

	if len(leaves) == 0 {
		return Snapshot{
			profile:  profile,
			root:     root,
			rootNode: noSnapshotNode,
		}, nil
	}

	nodes := make([]snapshotNode, 0, int(nodeCount))
	stack := make([]uint64, 0, bits.Len64(uint64(len(leaves))))
	for index, leaf := range leaves {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}

		nodes = append(nodes, snapshotNode{
			digest: hashLeaf(leaf.value),
			size:   1,
			left:   noSnapshotNode,
			right:  noSnapshotNode,
		})
		stack = append(stack, uint64(len(nodes)-1))
		completedSize := uint64(index) + 1
		for completedSize&1 == 0 {
			if err := ctx.Err(); err != nil {
				return Snapshot{}, err
			}

			right := stack[len(stack)-1]
			left := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			nodes = append(nodes, newSnapshotBranch(nodes, left, right))
			stack = append(stack, uint64(len(nodes)-1))
			completedSize >>= 1
		}
	}

	rootNode := stack[len(stack)-1]
	for index := len(stack) - 2; index >= 0; index-- {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}

		nodes = append(nodes, newSnapshotBranch(nodes, stack[index], rootNode))
		rootNode = uint64(len(nodes) - 1)
	}

	return Snapshot{
		profile:  profile,
		root:     root,
		nodes:    nodes,
		rootNode: rootNode,
	}, nil
}

// Root returns the immutable root identity bound to the snapshot.
func (snapshot Snapshot) Root() (Root, error) {
	if err := snapshot.validate(); err != nil {
		return Root{}, err
	}

	return snapshot.root, nil
}

func (snapshot Snapshot) validate() error {
	if err := snapshot.profile.validate(); err != nil {
		return ErrInvalidSnapshot
	}
	if snapshot.root.profileID != snapshot.profile.id ||
		snapshot.root.profileVersion != snapshot.profile.version ||
		snapshot.root.algorithm != snapshot.profile.algorithm ||
		snapshot.root.digest.algorithm != snapshot.profile.algorithm {
		return ErrInvalidSnapshot
	}
	if snapshot.root.treeSize == 0 {
		if len(snapshot.nodes) != 0 || snapshot.rootNode != noSnapshotNode {
			return ErrInvalidSnapshot
		}

		return nil
	}
	if snapshot.rootNode >= uint64(len(snapshot.nodes)) {
		return ErrInvalidSnapshot
	}
	node := snapshot.nodes[snapshot.rootNode]
	if node.size != snapshot.root.treeSize ||
		node.digest != snapshot.root.digest.value {
		return ErrInvalidSnapshot
	}

	return nil
}

// InclusionProof authenticates the leaf at index under this exact snapshot.
// Sibling digests are ordered from the leaf toward the root.
func (snapshot Snapshot) InclusionProof(
	ctx context.Context,
	index uint64,
	limits ProofLimits,
) (InclusionProof, error) {
	if ctx == nil {
		return InclusionProof{}, ErrInvalidContext
	}
	if err := snapshot.validate(); err != nil {
		return InclusionProof{}, err
	}
	if err := limits.validate(); err != nil {
		return InclusionProof{}, err
	}
	if err := ctx.Err(); err != nil {
		return InclusionProof{}, err
	}

	treeSize := snapshot.root.treeSize
	if index >= treeSize {
		return InclusionProof{}, ErrIndexOutOfRange
	}

	elementCount := auditPathLength(index, treeSize)
	if elementCount > limits.MaxElements {
		return InclusionProof{}, &ResourceError{
			Kind:   ResourceProofElements,
			Limit:  limits.MaxElements,
			Actual: elementCount,
		}
	}
	depth := uint64(bits.Len64(treeSize))
	if depth > limits.MaxTraversalDepth {
		return InclusionProof{}, &ResourceError{
			Kind:   ResourceTraversalDepth,
			Limit:  limits.MaxTraversalDepth,
			Actual: depth,
		}
	}

	siblings := make([]Digest, 0, elementCount)
	leafDigest, err := snapshot.appendAuditPath(
		ctx,
		snapshot.rootNode,
		index,
		&siblings,
	)
	if err != nil {
		return InclusionProof{}, err
	}

	return InclusionProof{
		profileID:      snapshot.profile.id,
		profileVersion: snapshot.profile.version,
		algorithm:      snapshot.profile.algorithm,
		root:           snapshot.root,
		treeSize:       treeSize,
		leafIndex:      index,
		leafDigest: newDigest(
			snapshot.profile.algorithm,
			leafDigest,
		),
		siblings: siblings,
	}, nil
}

func (snapshot Snapshot) appendAuditPath(
	ctx context.Context,
	nodeIndex uint64,
	index uint64,
	siblings *[]Digest,
) ([sha256.Size]byte, error) {
	if err := ctx.Err(); err != nil {
		return [sha256.Size]byte{}, err
	}
	node := snapshot.nodes[nodeIndex]
	if node.size == 1 {
		return node.digest, nil
	}

	left := snapshot.nodes[node.left]
	if index < left.size {
		leafDigest, err := snapshot.appendAuditPath(
			ctx,
			node.left,
			index,
			siblings,
		)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		*siblings = append(
			*siblings,
			newDigest(snapshot.profile.algorithm, snapshot.nodes[node.right].digest),
		)

		return leafDigest, nil
	}

	leafDigest, err := snapshot.appendAuditPath(
		ctx,
		node.right,
		index-left.size,
		siblings,
	)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	*siblings = append(
		*siblings,
		newDigest(snapshot.profile.algorithm, left.digest),
	)

	return leafDigest, nil
}

func newSnapshotBranch(
	nodes []snapshotNode,
	left uint64,
	right uint64,
) snapshotNode {
	return snapshotNode{
		digest: hashBranch(nodes[left].digest, nodes[right].digest),
		size:   nodes[left].size + nodes[right].size,
		left:   left,
		right:  right,
	}
}

// InclusionProof is an immutable proof bound to an exact operation, profile,
// version, algorithm, root, tree size, leaf index, and leaf digest.
type InclusionProof struct {
	profileID      ProfileID
	profileVersion uint16
	algorithm      HashAlgorithm
	root           Root
	treeSize       uint64
	leafIndex      uint64
	leafDigest     Digest
	siblings       []Digest
}

// ProfileID returns the proof's stable profile identity.
func (proof InclusionProof) ProfileID() ProfileID { return proof.profileID }

// ProfileVersion returns the proof's profile version.
func (proof InclusionProof) ProfileVersion() uint16 { return proof.profileVersion }

// Algorithm returns the proof's hash algorithm.
func (proof InclusionProof) Algorithm() HashAlgorithm { return proof.algorithm }

// Root returns the immutable root identity bound to the proof.
func (proof InclusionProof) Root() Root { return proof.root }

// TreeSize returns the exact snapshot size bound to the proof.
func (proof InclusionProof) TreeSize() uint64 { return proof.treeSize }

// LeafIndex returns the zero-based leaf index bound to the proof.
func (proof InclusionProof) LeafIndex() uint64 { return proof.leafIndex }

// LeafDigest returns the domain-separated digest bound to the proof.
func (proof InclusionProof) LeafDigest() Digest { return proof.leafDigest }

// Siblings returns an independent copy of the ordered sibling digests.
func (proof InclusionProof) Siblings() []Digest {
	result := make([]Digest, len(proof.siblings))
	copy(result, proof.siblings)

	return result
}

// VerifyInclusion independently verifies proof against leaf and its bound
// root. It returns ErrMalformedProof for structural defects and
// ErrVerificationFailed for a well-formed proof that does not authenticate.
func VerifyInclusion(
	ctx context.Context,
	proof InclusionProof,
	leaf RawLeaf,
	limits ProofLimits,
) error {
	if ctx == nil {
		return ErrInvalidContext
	}
	if err := limits.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	elementCount := uint64(len(proof.siblings))
	if elementCount > limits.MaxElements {
		return &ResourceError{
			Kind:   ResourceProofElements,
			Limit:  limits.MaxElements,
			Actual: elementCount,
		}
	}
	depth := uint64(bits.Len64(proof.treeSize))
	if depth > limits.MaxTraversalDepth {
		return &ResourceError{
			Kind:   ResourceTraversalDepth,
			Limit:  limits.MaxTraversalDepth,
			Actual: depth,
		}
	}
	leafBytes := uint64(len(leaf.value))
	if leafBytes > limits.MaxLeafBytes {
		return &ResourceError{
			Kind:   ResourceLeafBytes,
			Limit:  limits.MaxLeafBytes,
			Actual: leafBytes,
		}
	}
	if err := proof.validate(); err != nil {
		return err
	}
	if elementCount != auditPathLength(proof.leafIndex, proof.treeSize) {
		return ErrMalformedProof
	}

	leafDigest := hashLeaf(leaf.value)
	if subtle.ConstantTimeCompare(leafDigest[:], proof.leafDigest.value[:]) != 1 {
		return ErrVerificationFailed
	}

	current, _, err := reconstructInclusionRoot(
		ctx,
		proof.leafIndex,
		proof.treeSize,
		leafDigest,
		proof.siblings,
	)
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(current[:], proof.root.digest.value[:]) != 1 {
		return ErrVerificationFailed
	}

	return nil
}

func reconstructInclusionRoot(
	ctx context.Context,
	index uint64,
	size uint64,
	leafDigest [sha256.Size]byte,
	siblings []Digest,
) ([sha256.Size]byte, uint64, error) {
	if err := ctx.Err(); err != nil {
		return [sha256.Size]byte{}, 0, err
	}
	if size == 1 {
		return leafDigest, 0, nil
	}

	split := largestPowerOfTwoBelow(size)
	if index < split {
		left, consumed, err := reconstructInclusionRoot(
			ctx,
			index,
			split,
			leafDigest,
			siblings,
		)
		if err != nil {
			return [sha256.Size]byte{}, 0, err
		}

		return hashBranch(left, siblings[consumed].value), consumed + 1, nil
	}

	right, consumed, err := reconstructInclusionRoot(
		ctx,
		index-split,
		size-split,
		leafDigest,
		siblings,
	)
	if err != nil {
		return [sha256.Size]byte{}, 0, err
	}

	return hashBranch(siblings[consumed].value, right), consumed + 1, nil
}

func (proof InclusionProof) validate() error {
	if proof.algorithm != HashSHA256 {
		return ErrUnsupportedAlgorithm
	}
	profile := Profile{
		id:        proof.profileID,
		version:   proof.profileVersion,
		algorithm: proof.algorithm,
	}
	if err := profile.validate(); err != nil {
		return err
	}
	if proof.treeSize == 0 || proof.leafIndex >= proof.treeSize {
		return ErrMalformedProof
	}
	if proof.root.profileID != proof.profileID ||
		proof.root.profileVersion != proof.profileVersion ||
		proof.root.algorithm != proof.algorithm ||
		proof.root.treeSize != proof.treeSize ||
		proof.root.digest.algorithm != proof.algorithm ||
		proof.leafDigest.algorithm != proof.algorithm {
		return ErrMalformedProof
	}
	for _, sibling := range proof.siblings {
		if sibling.algorithm != proof.algorithm {
			return ErrMalformedProof
		}
	}

	return nil
}

func auditPathLength(index, size uint64) uint64 {
	var length uint64
	for range uint64(64) {
		if size <= 1 {
			return length
		}

		split := largestPowerOfTwoBelow(size)
		if index < split {
			size = split
		} else {
			index -= split
			size -= split
		}
		length++
	}

	return length
}

func largestPowerOfTwoBelow(value uint64) uint64 {
	return uint64(1) << (bits.Len64(value-1) - 1)
}

func newDigest(
	algorithm HashAlgorithm,
	value [sha256.Size]byte,
) Digest {
	return Digest{algorithm: algorithm, value: value}
}

func snapshotNodeCount(treeSize uint64) uint64 {
	if treeSize == 0 {
		return 0
	}

	return 2*treeSize - 1
}
