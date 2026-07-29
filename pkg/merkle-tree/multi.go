package merkletree

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"math/bits"
	"slices"
	"sort"
)

const (
	defaultMaxMultiProofLeaves   = uint64(1 << 16)
	defaultMaxMultiProofElements = uint64(1 << 19)
)

// MultiProofLimits bounds work and raw-leaf bytes consumed by multi-inclusion
// proof generation and verification. Every field must be nonzero. Values are
// copied and remain immutable during an operation.
type MultiProofLimits struct {
	MaxLeaves         uint64
	MaxElements       uint64
	MaxTraversalDepth uint64
	MaxLeafBytes      uint64
	MaxTotalLeafBytes uint64
}

// DefaultMultiProofLimits permits 2^16 selected leaves, 2^19 frontier nodes,
// 64 traversal levels, 16 MiB per leaf, and 1 GiB of total leaf bytes.
func DefaultMultiProofLimits() MultiProofLimits {
	return MultiProofLimits{
		MaxLeaves:         defaultMaxMultiProofLeaves,
		MaxElements:       defaultMaxMultiProofElements,
		MaxTraversalDepth: defaultMaxProofElements,
		MaxLeafBytes:      defaultMaxLeafBytes,
		MaxTotalLeafBytes: defaultMaxTotalBytes,
	}
}

func (limits MultiProofLimits) validate() error {
	if limits.MaxLeaves == 0 ||
		limits.MaxElements == 0 ||
		limits.MaxTraversalDepth == 0 ||
		limits.MaxLeafBytes == 0 ||
		limits.MaxTotalLeafBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

// MultiInclusionProof is an immutable package-defined proof authenticating a
// canonical sorted set of leaves under one exact root identity. Frontier nodes
// are ordered by a left-to-right depth-first traversal of unselected subtrees.
type MultiInclusionProof struct {
	profileID      ProfileID
	profileVersion uint16
	algorithm      HashAlgorithm
	root           Root
	treeSize       uint64
	leafIndexes    []uint64
	leafDigests    []Digest
	frontier       []Digest
}

// ProfileID returns the proof's stable root-profile identity.
func (proof MultiInclusionProof) ProfileID() ProfileID {
	return proof.profileID
}

// ProfileVersion returns the proof's root-profile version.
func (proof MultiInclusionProof) ProfileVersion() uint16 {
	return proof.profileVersion
}

// Algorithm returns the proof's hash algorithm.
func (proof MultiInclusionProof) Algorithm() HashAlgorithm {
	return proof.algorithm
}

// Root returns the immutable root identity bound to the proof.
func (proof MultiInclusionProof) Root() Root {
	return proof.root
}

// TreeSize returns the exact snapshot size bound to the proof.
func (proof MultiInclusionProof) TreeSize() uint64 {
	return proof.treeSize
}

// LeafIndexes returns an independent copy of the canonical ascending indexes.
func (proof MultiInclusionProof) LeafIndexes() []uint64 {
	return slices.Clone(proof.leafIndexes)
}

// LeafDigests returns independent proof-bound leaf digests in index order.
func (proof MultiInclusionProof) LeafDigests() []Digest {
	return slices.Clone(proof.leafDigests)
}

// Frontier returns an independent copy of the canonical frontier-node order.
func (proof MultiInclusionProof) Frontier() []Digest {
	return slices.Clone(proof.frontier)
}

// MultiInclusionProof generates the unique package-defined minimal frontier
// for indexes under this snapshot. Input order is ignored; returned indexes
// are canonical ascending order. Duplicate indexes are rejected.
func (snapshot Snapshot) MultiInclusionProof(
	ctx context.Context,
	indexes []uint64,
	limits MultiProofLimits,
) (MultiInclusionProof, error) {
	if ctx == nil {
		return MultiInclusionProof{}, ErrInvalidContext
	}
	if err := snapshot.validate(); err != nil {
		return MultiInclusionProof{}, err
	}
	if err := limits.validate(); err != nil {
		return MultiInclusionProof{}, err
	}
	if err := ctx.Err(); err != nil {
		return MultiInclusionProof{}, err
	}

	indexCount := uint64(len(indexes))
	if indexCount == 0 {
		return MultiInclusionProof{}, ErrInvalidLeafIndexes
	}
	if indexCount > limits.MaxLeaves {
		return MultiInclusionProof{}, &ResourceError{
			Kind:   ResourceLeaves,
			Limit:  limits.MaxLeaves,
			Actual: indexCount,
		}
	}

	treeSize := snapshot.root.treeSize
	canonicalIndexes := slices.Clone(indexes)
	slices.Sort(canonicalIndexes)
	for index, leafIndex := range canonicalIndexes {
		if leafIndex >= treeSize {
			return MultiInclusionProof{}, ErrIndexOutOfRange
		}
		if index != 0 && canonicalIndexes[index-1] == leafIndex {
			return MultiInclusionProof{}, ErrInvalidLeafIndexes
		}
	}

	depth := uint64(bits.Len64(treeSize))
	if depth > limits.MaxTraversalDepth {
		return MultiInclusionProof{}, &ResourceError{
			Kind:   ResourceTraversalDepth,
			Limit:  limits.MaxTraversalDepth,
			Actual: depth,
		}
	}

	proof := MultiInclusionProof{
		profileID:      snapshot.profile.id,
		profileVersion: snapshot.profile.version,
		algorithm:      snapshot.profile.algorithm,
		root:           snapshot.root,
		treeSize:       treeSize,
		leafIndexes:    canonicalIndexes,
		leafDigests:    make([]Digest, 0, len(canonicalIndexes)),
	}
	if err := snapshot.appendMultiProof(
		ctx,
		snapshot.rootNode,
		0,
		canonicalIndexes,
		limits.MaxElements,
		&proof.leafDigests,
		&proof.frontier,
	); err != nil {
		return MultiInclusionProof{}, err
	}

	return proof, nil
}

func (snapshot Snapshot) appendMultiProof(
	ctx context.Context,
	nodeIndex uint64,
	start uint64,
	indexes []uint64,
	maxElements uint64,
	leafDigests *[]Digest,
	frontier *[]Digest,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	node := snapshot.nodes[nodeIndex]
	if len(indexes) == 0 {
		elementCount := uint64(len(*frontier))
		if elementCount == maxElements {
			return &ResourceError{
				Kind:   ResourceProofElements,
				Limit:  maxElements,
				Actual: elementCount + 1,
			}
		}
		*frontier = append(
			*frontier,
			newDigest(snapshot.profile.algorithm, node.digest),
		)

		return nil
	}
	if node.size == 1 {
		*leafDigests = append(
			*leafDigests,
			newDigest(snapshot.profile.algorithm, node.digest),
		)

		return nil
	}

	left := snapshot.nodes[node.left]
	splitIndex := sort.Search(
		len(indexes),
		func(index int) bool {
			return indexes[index] >= start+left.size
		},
	)
	if err := snapshot.appendMultiProof(
		ctx,
		node.left,
		start,
		indexes[:splitIndex],
		maxElements,
		leafDigests,
		frontier,
	); err != nil {
		return err
	}

	return snapshot.appendMultiProof(
		ctx,
		node.right,
		start+left.size,
		indexes[splitIndex:],
		maxElements,
		leafDigests,
		frontier,
	)
}

// VerifyMultiInclusion independently verifies raw leaves, proof identity, and
// the canonical multi-proof frontier. leaves must correspond to LeafIndexes
// order. Structural defects return ErrMalformedProof; authentication failures
// return ErrVerificationFailed.
func VerifyMultiInclusion(
	ctx context.Context,
	proof MultiInclusionProof,
	leaves []RawLeaf,
	limits MultiProofLimits,
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

	leafCount := uint64(len(proof.leafIndexes))
	if leafCount > limits.MaxLeaves {
		return &ResourceError{
			Kind:   ResourceLeaves,
			Limit:  limits.MaxLeaves,
			Actual: leafCount,
		}
	}
	elementCount := uint64(len(proof.frontier))
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
	if err := proof.validate(); err != nil {
		return err
	}
	if len(leaves) != len(proof.leafIndexes) {
		return ErrVerificationFailed
	}

	var totalLeafBytes uint64
	for index, leaf := range leaves {
		if err := ctx.Err(); err != nil {
			return err
		}
		leafBytes := uint64(len(leaf.value))
		if leafBytes > limits.MaxLeafBytes {
			return &ResourceError{
				Kind:   ResourceLeafBytes,
				Limit:  limits.MaxLeafBytes,
				Actual: leafBytes,
			}
		}
		if leafBytes > limits.MaxTotalLeafBytes-totalLeafBytes {
			return &ResourceError{
				Kind:   ResourceTotalBytes,
				Limit:  limits.MaxTotalLeafBytes,
				Actual: saturatedAdd(totalLeafBytes, leafBytes),
			}
		}
		totalLeafBytes += leafBytes

		digest := hashLeaf(leaf.value)
		if subtle.ConstantTimeCompare(
			digest[:],
			proof.leafDigests[index].value[:],
		) != 1 {
			return ErrVerificationFailed
		}
	}

	frontierIndex := 0
	rootDigest, err := reconstructMultiRoot(
		ctx,
		0,
		proof.treeSize,
		proof.leafIndexes,
		proof.leafDigests,
		proof.frontier,
		&frontierIndex,
	)
	if err != nil {
		return err
	}
	if frontierIndex != len(proof.frontier) {
		return ErrMalformedProof
	}
	if subtle.ConstantTimeCompare(
		rootDigest[:],
		proof.root.digest.value[:],
	) != 1 {
		return ErrVerificationFailed
	}

	return nil
}

func reconstructMultiRoot(
	ctx context.Context,
	start uint64,
	size uint64,
	indexes []uint64,
	leafDigests []Digest,
	frontier []Digest,
	frontierIndex *int,
) ([sha256.Size]byte, error) {
	if err := ctx.Err(); err != nil {
		return [sha256.Size]byte{}, err
	}
	if len(indexes) == 0 {
		if *frontierIndex >= len(frontier) {
			return [sha256.Size]byte{}, ErrMalformedProof
		}
		digest := frontier[*frontierIndex].value
		*frontierIndex++

		return digest, nil
	}
	if size == 1 {
		return leafDigests[0].value, nil
	}

	split := largestPowerOfTwoBelow(size)
	splitIndex := sort.Search(
		len(indexes),
		func(index int) bool {
			return indexes[index] >= start+split
		},
	)
	left, err := reconstructMultiRoot(
		ctx,
		start,
		split,
		indexes[:splitIndex],
		leafDigests[:splitIndex],
		frontier,
		frontierIndex,
	)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	right, err := reconstructMultiRoot(
		ctx,
		start+split,
		size-split,
		indexes[splitIndex:],
		leafDigests[splitIndex:],
		frontier,
		frontierIndex,
	)
	if err != nil {
		return [sha256.Size]byte{}, err
	}

	return hashBranch(left, right), nil
}

func (proof MultiInclusionProof) validate() error {
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
	if proof.treeSize == 0 ||
		proof.root.treeSize != proof.treeSize ||
		!rootMatchesMultiProofIdentity(proof.root, proof) ||
		len(proof.leafIndexes) == 0 ||
		len(proof.leafIndexes) != len(proof.leafDigests) {
		return ErrMalformedProof
	}
	for index, leafIndex := range proof.leafIndexes {
		if leafIndex >= proof.treeSize ||
			index != 0 && proof.leafIndexes[index-1] >= leafIndex ||
			proof.leafDigests[index].algorithm != proof.algorithm {
			return ErrMalformedProof
		}
	}
	for _, node := range proof.frontier {
		if node.algorithm != proof.algorithm {
			return ErrMalformedProof
		}
	}

	return nil
}

func rootMatchesMultiProofIdentity(
	root Root,
	proof MultiInclusionProof,
) bool {
	return root.profileID == proof.profileID &&
		root.profileVersion == proof.profileVersion &&
		root.algorithm == proof.algorithm &&
		root.digest.algorithm == proof.algorithm
}
