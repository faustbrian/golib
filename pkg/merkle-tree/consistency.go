package merkletree

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"math/bits"
)

const (
	defaultMaxConsistencyElements = uint64(65)
	defaultMaxConsistencyDepth    = uint64(64)
)

// ConsistencyProofLimits bounds work derived from consistency-proof metadata.
// Every field must be nonzero. Values are copied and remain immutable during
// an operation.
type ConsistencyProofLimits struct {
	MaxElements       uint64
	MaxTraversalDepth uint64
}

// DefaultConsistencyProofLimits permits the RFC 9162 upper bound of 65 proof
// nodes and every traversal depth representable by uint64 tree sizes.
func DefaultConsistencyProofLimits() ConsistencyProofLimits {
	return ConsistencyProofLimits{
		MaxElements:       defaultMaxConsistencyElements,
		MaxTraversalDepth: defaultMaxConsistencyDepth,
	}
}

func (limits ConsistencyProofLimits) validate() error {
	if limits.MaxElements == 0 || limits.MaxTraversalDepth == 0 {
		return ErrInvalidLimits
	}

	return nil
}

// ConsistencyProof is an immutable RFC 9162 append-only proof bound to the
// older and newer root identities. Nodes are ordered as produced by the RFC
// SUBPROOF algorithm.
type ConsistencyProof struct {
	profileID      ProfileID
	profileVersion uint16
	algorithm      HashAlgorithm
	olderRoot      Root
	newerRoot      Root
	olderTreeSize  uint64
	newerTreeSize  uint64
	nodes          []Digest
}

// ProfileID returns the proof's stable profile identity.
func (proof ConsistencyProof) ProfileID() ProfileID { return proof.profileID }

// ProfileVersion returns the proof's profile version.
func (proof ConsistencyProof) ProfileVersion() uint16 {
	return proof.profileVersion
}

// Algorithm returns the proof's hash algorithm.
func (proof ConsistencyProof) Algorithm() HashAlgorithm {
	return proof.algorithm
}

// OlderRoot returns the immutable older root bound to the proof.
func (proof ConsistencyProof) OlderRoot() Root { return proof.olderRoot }

// NewerRoot returns the immutable newer root bound to the proof.
func (proof ConsistencyProof) NewerRoot() Root { return proof.newerRoot }

// OlderTreeSize returns the exact older tree size.
func (proof ConsistencyProof) OlderTreeSize() uint64 {
	return proof.olderTreeSize
}

// NewerTreeSize returns the exact newer tree size.
func (proof ConsistencyProof) NewerTreeSize() uint64 {
	return proof.newerTreeSize
}

// Nodes returns an independent copy of the ordered consistency path.
func (proof ConsistencyProof) Nodes() []Digest {
	result := make([]Digest, len(proof.nodes))
	copy(result, proof.nodes)

	return result
}

// ConsistencyProof generates the unique minimal RFC 9162 consistency proof
// from older to this snapshot. older must identify a prefix no larger than the
// snapshot. A zero-sized prefix is valid only for an equal empty snapshot.
// Equal roots produce an empty proof.
func (snapshot Snapshot) ConsistencyProof(
	ctx context.Context,
	older Root,
	limits ConsistencyProofLimits,
) (ConsistencyProof, error) {
	if ctx == nil {
		return ConsistencyProof{}, ErrInvalidContext
	}
	if err := snapshot.validate(); err != nil {
		return ConsistencyProof{}, err
	}
	if err := limits.validate(); err != nil {
		return ConsistencyProof{}, err
	}
	if err := ctx.Err(); err != nil {
		return ConsistencyProof{}, err
	}

	olderSize := older.treeSize
	newerSize := snapshot.root.treeSize
	if olderSize > newerSize || olderSize == 0 && newerSize != 0 {
		return ConsistencyProof{}, ErrInvalidTreeSize
	}
	if !rootsShareProfile(older, snapshot.root) {
		return ConsistencyProof{}, ErrIncompatibleRoot
	}

	elementCount := consistencyPathLength(olderSize, newerSize)
	if elementCount > limits.MaxElements {
		return ConsistencyProof{}, &ResourceError{
			Kind:   ResourceProofElements,
			Limit:  limits.MaxElements,
			Actual: elementCount,
		}
	}
	depth := uint64(bits.Len64(newerSize))
	if depth > limits.MaxTraversalDepth {
		return ConsistencyProof{}, &ResourceError{
			Kind:   ResourceTraversalDepth,
			Limit:  limits.MaxTraversalDepth,
			Actual: depth,
		}
	}

	proof := ConsistencyProof{
		profileID:      snapshot.profile.id,
		profileVersion: snapshot.profile.version,
		algorithm:      snapshot.profile.algorithm,
		olderRoot:      older,
		newerRoot:      snapshot.root,
		olderTreeSize:  olderSize,
		newerTreeSize:  newerSize,
		nodes:          make([]Digest, 0, elementCount),
	}
	if olderSize == newerSize {
		if subtle.ConstantTimeCompare(
			older.digest.value[:],
			snapshot.root.digest.value[:],
		) != 1 {
			return ConsistencyProof{}, ErrVerificationFailed
		}

		return proof, nil
	}

	prefixDigest, err := snapshot.appendConsistencySubproof(
		ctx,
		snapshot.rootNode,
		olderSize,
		true,
		&proof.nodes,
	)
	if err != nil {
		return ConsistencyProof{}, err
	}
	if subtle.ConstantTimeCompare(
		prefixDigest[:],
		older.digest.value[:],
	) != 1 {
		return ConsistencyProof{}, ErrVerificationFailed
	}

	return proof, nil
}

func (snapshot Snapshot) appendConsistencySubproof(
	ctx context.Context,
	nodeIndex uint64,
	olderSize uint64,
	complete bool,
	nodes *[]Digest,
) ([sha256.Size]byte, error) {
	if err := ctx.Err(); err != nil {
		return [sha256.Size]byte{}, err
	}
	node := snapshot.nodes[nodeIndex]
	if olderSize == node.size {
		if !complete {
			*nodes = append(
				*nodes,
				newDigest(snapshot.profile.algorithm, node.digest),
			)
		}

		return node.digest, nil
	}

	left := snapshot.nodes[node.left]
	if olderSize <= left.size {
		prefix, err := snapshot.appendConsistencySubproof(
			ctx,
			node.left,
			olderSize,
			complete,
			nodes,
		)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		*nodes = append(
			*nodes,
			newDigest(
				snapshot.profile.algorithm,
				snapshot.nodes[node.right].digest,
			),
		)

		return prefix, nil
	}

	rightPrefix, err := snapshot.appendConsistencySubproof(
		ctx,
		node.right,
		olderSize-left.size,
		false,
		nodes,
	)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	*nodes = append(
		*nodes,
		newDigest(snapshot.profile.algorithm, left.digest),
	)

	return hashBranch(left.digest, rightPrefix), nil
}

// VerifyConsistency independently verifies proof's append-only relationship.
// It returns ErrMalformedProof for structural defects and
// ErrVerificationFailed when well-formed nodes do not authenticate both roots.
func VerifyConsistency(
	ctx context.Context,
	proof ConsistencyProof,
	limits ConsistencyProofLimits,
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

	elementCount := uint64(len(proof.nodes))
	if elementCount > limits.MaxElements {
		return &ResourceError{
			Kind:   ResourceProofElements,
			Limit:  limits.MaxElements,
			Actual: elementCount,
		}
	}
	depth := uint64(bits.Len64(proof.newerTreeSize))
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
	if proof.olderTreeSize == proof.newerTreeSize {
		if elementCount != 0 {
			return ErrMalformedProof
		}
		if subtle.ConstantTimeCompare(
			proof.olderRoot.digest.value[:],
			proof.newerRoot.digest.value[:],
		) != 1 {
			return ErrVerificationFailed
		}

		return nil
	}

	fn := proof.olderTreeSize - 1
	sn := proof.newerTreeSize - 1
	initialShift := bits.TrailingZeros64(^fn)
	fn >>= initialShift
	sn >>= initialShift

	pathIndex := 0
	var firstRoot, secondRoot [sha256.Size]byte
	if proof.olderTreeSize&(proof.olderTreeSize-1) == 0 {
		firstRoot = proof.olderRoot.digest.value
		secondRoot = firstRoot
	} else {
		firstRoot = proof.nodes[0].value
		secondRoot = firstRoot
		pathIndex = 1
	}

	for ; pathIndex < len(proof.nodes); pathIndex++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if sn == 0 {
			return ErrMalformedProof
		}
		node := proof.nodes[pathIndex].value
		if fn&1 == 1 || fn == sn {
			firstRoot = hashBranch(node, firstRoot)
			secondRoot = hashBranch(node, secondRoot)
			sharedShift := bits.TrailingZeros64(fn)
			fn >>= sharedShift
			sn >>= sharedShift
		} else {
			secondRoot = hashBranch(secondRoot, node)
		}
		fn >>= 1
		sn >>= 1
	}
	if sn != 0 {
		return ErrMalformedProof
	}
	if subtle.ConstantTimeCompare(
		firstRoot[:],
		proof.olderRoot.digest.value[:],
	) != 1 ||
		subtle.ConstantTimeCompare(
			secondRoot[:],
			proof.newerRoot.digest.value[:],
		) != 1 {
		return ErrVerificationFailed
	}

	return nil
}

func (proof ConsistencyProof) validate() error {
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
	if proof.olderTreeSize > proof.newerTreeSize ||
		proof.olderTreeSize == 0 && proof.newerTreeSize != 0 {
		return ErrMalformedProof
	}
	if proof.olderRoot.treeSize != proof.olderTreeSize ||
		proof.newerRoot.treeSize != proof.newerTreeSize ||
		!rootMatchesProofIdentity(proof.olderRoot, proof) ||
		!rootMatchesProofIdentity(proof.newerRoot, proof) {
		return ErrMalformedProof
	}
	for _, node := range proof.nodes {
		if node.algorithm != proof.algorithm {
			return ErrMalformedProof
		}
	}

	return nil
}

func rootsShareProfile(left, right Root) bool {
	return left.profileID == right.profileID &&
		left.profileVersion == right.profileVersion &&
		left.algorithm == right.algorithm &&
		left.digest.algorithm == left.algorithm &&
		right.digest.algorithm == right.algorithm
}

func rootMatchesProofIdentity(root Root, proof ConsistencyProof) bool {
	return root.profileID == proof.profileID &&
		root.profileVersion == proof.profileVersion &&
		root.algorithm == proof.algorithm &&
		root.digest.algorithm == proof.algorithm
}

func consistencyPathLength(olderSize, newerSize uint64) uint64 {
	if olderSize == newerSize {
		return 0
	}

	var length uint64
	complete := true
	for olderSize < newerSize {
		split := largestPowerOfTwoBelow(newerSize)
		if olderSize <= split {
			newerSize = split
		} else {
			olderSize -= split
			newerSize -= split
			complete = false
		}
		length++
		if length == 64 {
			if !complete {
				length++
			}

			return length
		}
	}
	if !complete {
		length++
	}

	return length
}
