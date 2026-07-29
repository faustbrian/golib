package merkletree

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"math/bits"
	"sort"
)

const (
	encodingObjectInclusion   = byte(2)
	encodingObjectConsistency = byte(3)
	encodingObjectMulti       = byte(4)

	encodedInclusionBase   = encodingHeaderSize + 8 + sha256.Size + 8 + sha256.Size + 8
	encodedConsistencyBase = encodingHeaderSize + 8 + sha256.Size + 8 + sha256.Size + 8
	encodedMultiPrefix     = encodingHeaderSize + 8 + sha256.Size + 8
	encodedMultiSuffix     = 8
	encodedMultiMinimum    = 66
)

// MarshalBinary returns the version-1 canonical inclusion-proof encoding.
func (proof InclusionProof) MarshalBinary() ([]byte, error) {
	if err := validateInclusionEncoding(proof); err != nil {
		return nil, err
	}
	size, _ := encodedVectorSize(
		encodedInclusionBase,
		uint64(len(proof.siblings)),
		sha256.Size,
	)

	result := make([]byte, size)
	appendEncodingHeader(
		result,
		encodingObjectInclusion,
		profileFromInclusionProof(proof),
	)
	offset := encodingHeaderSize
	binary.BigEndian.PutUint64(result[offset:offset+8], proof.treeSize)
	offset += 8
	copy(result[offset:offset+sha256.Size], proof.root.digest.value[:])
	offset += sha256.Size
	binary.BigEndian.PutUint64(result[offset:offset+8], proof.leafIndex)
	offset += 8
	copy(result[offset:offset+sha256.Size], proof.leafDigest.value[:])
	offset += sha256.Size
	binary.BigEndian.PutUint64(result[offset:offset+8], uint64(len(proof.siblings)))
	offset += 8
	for _, sibling := range proof.siblings {
		copy(result[offset:offset+sha256.Size], sibling.value[:])
		offset += sha256.Size
	}

	return result, nil
}

// ParseInclusionProof decodes one complete version-1 canonical inclusion
// proof. It validates canonical structure without authenticating a raw leaf.
func ParseInclusionProof(
	ctx context.Context,
	data []byte,
	encodingLimits EncodingLimits,
	proofLimits ProofLimits,
) (InclusionProof, error) {
	if err := validateProofDecodeInputs(
		ctx,
		data,
		encodingLimits,
		proofLimits.validate(),
	); err != nil {
		return InclusionProof{}, err
	}
	if len(data) < encodedInclusionBase {
		return InclusionProof{}, ErrMalformedEncoding
	}
	profile, err := parseEncodingHeader(data, encodingObjectInclusion)
	if err != nil {
		return InclusionProof{}, err
	}

	offset := encodingHeaderSize
	treeSize := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	var rootDigest [sha256.Size]byte
	copy(rootDigest[:], data[offset:offset+sha256.Size])
	offset += sha256.Size
	leafIndex := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	var leafDigest [sha256.Size]byte
	copy(leafDigest[:], data[offset:offset+sha256.Size])
	offset += sha256.Size
	elementCount := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	if elementCount > proofLimits.MaxElements {
		return InclusionProof{}, &ResourceError{
			Kind:   ResourceProofElements,
			Limit:  proofLimits.MaxElements,
			Actual: elementCount,
		}
	}
	depth := uint64(bits.Len64(treeSize))
	if depth > proofLimits.MaxTraversalDepth {
		return InclusionProof{}, &ResourceError{
			Kind:   ResourceTraversalDepth,
			Limit:  proofLimits.MaxTraversalDepth,
			Actual: depth,
		}
	}
	size, err := encodedVectorSize(
		encodedInclusionBase,
		elementCount,
		sha256.Size,
	)
	if err != nil || size != len(data) {
		return InclusionProof{}, ErrMalformedEncoding
	}

	proof := InclusionProof{
		profileID:      profile.id,
		profileVersion: profile.version,
		algorithm:      profile.algorithm,
		root:           newRoot(profile, treeSize, rootDigest),
		treeSize:       treeSize,
		leafIndex:      leafIndex,
		leafDigest:     newDigest(profile.algorithm, leafDigest),
		siblings:       make([]Digest, int(elementCount)),
	}
	for index := range proof.siblings {
		if err := ctx.Err(); err != nil {
			return InclusionProof{}, err
		}
		var digest [sha256.Size]byte
		copy(digest[:], data[offset:offset+sha256.Size])
		offset += sha256.Size
		proof.siblings[index] = newDigest(profile.algorithm, digest)
	}
	if err := validateInclusionEncoding(proof); err != nil {
		return InclusionProof{}, err
	}

	return proof, nil
}

// MarshalBinary returns the version-1 canonical consistency-proof encoding.
func (proof ConsistencyProof) MarshalBinary() ([]byte, error) {
	if err := validateConsistencyEncoding(proof); err != nil {
		return nil, err
	}
	size, _ := encodedVectorSize(
		encodedConsistencyBase,
		uint64(len(proof.nodes)),
		sha256.Size,
	)

	result := make([]byte, size)
	appendEncodingHeader(
		result,
		encodingObjectConsistency,
		profileFromConsistencyProof(proof),
	)
	offset := encodingHeaderSize
	binary.BigEndian.PutUint64(result[offset:offset+8], proof.olderTreeSize)
	offset += 8
	copy(result[offset:offset+sha256.Size], proof.olderRoot.digest.value[:])
	offset += sha256.Size
	binary.BigEndian.PutUint64(result[offset:offset+8], proof.newerTreeSize)
	offset += 8
	copy(result[offset:offset+sha256.Size], proof.newerRoot.digest.value[:])
	offset += sha256.Size
	binary.BigEndian.PutUint64(result[offset:offset+8], uint64(len(proof.nodes)))
	offset += 8
	for _, node := range proof.nodes {
		copy(result[offset:offset+sha256.Size], node.value[:])
		offset += sha256.Size
	}

	return result, nil
}

// ParseConsistencyProof decodes one complete version-1 canonical consistency
// proof. It validates canonical structure without authenticating both roots.
func ParseConsistencyProof(
	ctx context.Context,
	data []byte,
	encodingLimits EncodingLimits,
	proofLimits ConsistencyProofLimits,
) (ConsistencyProof, error) {
	if err := validateProofDecodeInputs(
		ctx,
		data,
		encodingLimits,
		proofLimits.validate(),
	); err != nil {
		return ConsistencyProof{}, err
	}
	if len(data) < encodedConsistencyBase {
		return ConsistencyProof{}, ErrMalformedEncoding
	}
	profile, err := parseEncodingHeader(data, encodingObjectConsistency)
	if err != nil {
		return ConsistencyProof{}, err
	}

	offset := encodingHeaderSize
	olderSize := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	var olderDigest [sha256.Size]byte
	copy(olderDigest[:], data[offset:offset+sha256.Size])
	offset += sha256.Size
	newerSize := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	var newerDigest [sha256.Size]byte
	copy(newerDigest[:], data[offset:offset+sha256.Size])
	offset += sha256.Size
	elementCount := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	if elementCount > proofLimits.MaxElements {
		return ConsistencyProof{}, &ResourceError{
			Kind:   ResourceProofElements,
			Limit:  proofLimits.MaxElements,
			Actual: elementCount,
		}
	}
	depth := uint64(bits.Len64(newerSize))
	if depth > proofLimits.MaxTraversalDepth {
		return ConsistencyProof{}, &ResourceError{
			Kind:   ResourceTraversalDepth,
			Limit:  proofLimits.MaxTraversalDepth,
			Actual: depth,
		}
	}
	size, err := encodedVectorSize(
		encodedConsistencyBase,
		elementCount,
		sha256.Size,
	)
	if err != nil || size != len(data) {
		return ConsistencyProof{}, ErrMalformedEncoding
	}

	proof := ConsistencyProof{
		profileID:      profile.id,
		profileVersion: profile.version,
		algorithm:      profile.algorithm,
		olderRoot:      newRoot(profile, olderSize, olderDigest),
		newerRoot:      newRoot(profile, newerSize, newerDigest),
		olderTreeSize:  olderSize,
		newerTreeSize:  newerSize,
		nodes:          make([]Digest, int(elementCount)),
	}
	for index := range proof.nodes {
		if err := ctx.Err(); err != nil {
			return ConsistencyProof{}, err
		}
		var digest [sha256.Size]byte
		copy(digest[:], data[offset:offset+sha256.Size])
		offset += sha256.Size
		proof.nodes[index] = newDigest(profile.algorithm, digest)
	}
	if err := validateConsistencyEncoding(proof); err != nil {
		return ConsistencyProof{}, err
	}

	return proof, nil
}

// MarshalBinary returns the version-1 canonical multi-inclusion-proof
// encoding.
func (proof MultiInclusionProof) MarshalBinary() ([]byte, error) {
	if err := validateMultiEncoding(proof); err != nil {
		return nil, err
	}
	size, _ := encodedMultiSize(
		uint64(len(proof.leafIndexes)),
		uint64(len(proof.frontier)),
	)

	result := make([]byte, size)
	appendEncodingHeader(
		result,
		encodingObjectMulti,
		profileFromMultiProof(proof),
	)
	offset := encodingHeaderSize
	binary.BigEndian.PutUint64(result[offset:offset+8], proof.treeSize)
	offset += 8
	copy(result[offset:offset+sha256.Size], proof.root.digest.value[:])
	offset += sha256.Size
	binary.BigEndian.PutUint64(
		result[offset:offset+8],
		uint64(len(proof.leafIndexes)),
	)
	offset += 8
	for index, leafIndex := range proof.leafIndexes {
		binary.BigEndian.PutUint64(result[offset:offset+8], leafIndex)
		offset += 8
		copy(
			result[offset:offset+sha256.Size],
			proof.leafDigests[index].value[:],
		)
		offset += sha256.Size
	}
	binary.BigEndian.PutUint64(
		result[offset:offset+8],
		uint64(len(proof.frontier)),
	)
	offset += 8
	for _, node := range proof.frontier {
		copy(result[offset:offset+sha256.Size], node.value[:])
		offset += sha256.Size
	}

	return result, nil
}

// ParseMultiInclusionProof decodes one complete version-1 canonical
// multi-inclusion proof. It validates canonical structure without
// authenticating caller-supplied raw leaves.
func ParseMultiInclusionProof(
	ctx context.Context,
	data []byte,
	encodingLimits EncodingLimits,
	proofLimits MultiProofLimits,
) (MultiInclusionProof, error) {
	if err := validateProofDecodeInputs(
		ctx,
		data,
		encodingLimits,
		proofLimits.validate(),
	); err != nil {
		return MultiInclusionProof{}, err
	}
	if len(data) < encodedMultiMinimum {
		return MultiInclusionProof{}, ErrMalformedEncoding
	}
	profile, err := parseEncodingHeader(data, encodingObjectMulti)
	if err != nil {
		return MultiInclusionProof{}, err
	}

	offset := encodingHeaderSize
	treeSize := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	var rootDigest [sha256.Size]byte
	copy(rootDigest[:], data[offset:offset+sha256.Size])
	offset += sha256.Size
	leafCount := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	if leafCount > proofLimits.MaxLeaves {
		return MultiInclusionProof{}, &ResourceError{
			Kind:   ResourceLeaves,
			Limit:  proofLimits.MaxLeaves,
			Actual: leafCount,
		}
	}
	depth := uint64(bits.Len64(treeSize))
	if depth > proofLimits.MaxTraversalDepth {
		return MultiInclusionProof{}, &ResourceError{
			Kind:   ResourceTraversalDepth,
			Limit:  proofLimits.MaxTraversalDepth,
			Actual: depth,
		}
	}
	frontierOffset, err := encodedVectorSize(
		encodedMultiPrefix,
		leafCount,
		8+sha256.Size,
	)
	if err != nil ||
		frontierOffset > len(data)-encodedMultiSuffix {
		return MultiInclusionProof{}, ErrMalformedEncoding
	}
	frontierCount := binary.BigEndian.Uint64(
		data[frontierOffset : frontierOffset+8],
	)
	if frontierCount > proofLimits.MaxElements {
		return MultiInclusionProof{}, &ResourceError{
			Kind:   ResourceProofElements,
			Limit:  proofLimits.MaxElements,
			Actual: frontierCount,
		}
	}
	size, err := encodedMultiSize(leafCount, frontierCount)
	if err != nil || size != len(data) {
		return MultiInclusionProof{}, ErrMalformedEncoding
	}

	proof := MultiInclusionProof{
		profileID:      profile.id,
		profileVersion: profile.version,
		algorithm:      profile.algorithm,
		root:           newRoot(profile, treeSize, rootDigest),
		treeSize:       treeSize,
		leafIndexes:    make([]uint64, int(leafCount)),
		leafDigests:    make([]Digest, int(leafCount)),
		frontier:       make([]Digest, int(frontierCount)),
	}
	for index := range proof.leafIndexes {
		if err := ctx.Err(); err != nil {
			return MultiInclusionProof{}, err
		}
		proof.leafIndexes[index] = binary.BigEndian.Uint64(
			data[offset : offset+8],
		)
		offset += 8
		var digest [sha256.Size]byte
		copy(digest[:], data[offset:offset+sha256.Size])
		offset += sha256.Size
		proof.leafDigests[index] = newDigest(profile.algorithm, digest)
	}
	offset += 8
	for index := range proof.frontier {
		if err := ctx.Err(); err != nil {
			return MultiInclusionProof{}, err
		}
		var digest [sha256.Size]byte
		copy(digest[:], data[offset:offset+sha256.Size])
		offset += sha256.Size
		proof.frontier[index] = newDigest(profile.algorithm, digest)
	}
	if err := validateMultiEncoding(proof); err != nil {
		return MultiInclusionProof{}, err
	}

	return proof, nil
}

func validateProofDecodeInputs(
	ctx context.Context,
	data []byte,
	encodingLimits EncodingLimits,
	proofLimitError error,
) error {
	if ctx == nil {
		return ErrInvalidContext
	}
	if err := encodingLimits.validate(); err != nil {
		return err
	}
	if proofLimitError != nil {
		return proofLimitError
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dataSize := uint64(len(data))
	if dataSize > encodingLimits.MaxBytes {
		return &ResourceError{
			Kind:   ResourceEncodedBytes,
			Limit:  encodingLimits.MaxBytes,
			Actual: dataSize,
		}
	}

	return nil
}

func validateInclusionEncoding(proof InclusionProof) error {
	if err := proof.validate(); err != nil {
		return err
	}
	if uint64(len(proof.siblings)) !=
		auditPathLength(proof.leafIndex, proof.treeSize) {
		return ErrMalformedProof
	}
	return nil
}

func validateConsistencyEncoding(proof ConsistencyProof) error {
	if err := proof.validate(); err != nil {
		return err
	}
	if uint64(len(proof.nodes)) != consistencyPathLength(
		proof.olderTreeSize,
		proof.newerTreeSize,
	) {
		return ErrMalformedProof
	}
	if proof.olderTreeSize == proof.newerTreeSize &&
		subtle.ConstantTimeCompare(
			proof.olderRoot.digest.value[:],
			proof.newerRoot.digest.value[:],
		) != 1 {
		return ErrVerificationFailed
	}
	return nil
}

func validateMultiEncoding(proof MultiInclusionProof) error {
	if err := proof.validate(); err != nil {
		return err
	}
	if uint64(len(proof.frontier)) != multiFrontierCount(
		0,
		proof.treeSize,
		proof.leafIndexes,
	) {
		return ErrMalformedProof
	}
	return nil
}

func multiFrontierCount(
	start uint64,
	size uint64,
	indexes []uint64,
) uint64 {
	if len(indexes) == 0 {
		return 1
	}
	if size == 1 {
		return 0
	}

	split := largestPowerOfTwoBelow(size)
	splitIndex := sort.Search(
		len(indexes),
		func(index int) bool {
			return indexes[index] >= start+split
		},
	)

	return multiFrontierCount(start, split, indexes[:splitIndex]) +
		multiFrontierCount(
			start+split,
			size-split,
			indexes[splitIndex:],
		)
}

func encodedVectorSize(
	base int,
	count uint64,
	elementSize uint64,
) (int, error) {
	high, product := bits.Mul64(count, elementSize)
	size, carry := bits.Add64(uint64(base), product, 0)
	if high != 0 || carry != 0 || size > uint64(maxInt()) {
		return 0, ErrMalformedEncoding
	}

	return int(size), nil
}

func encodedMultiSize(leafCount, frontierCount uint64) (int, error) {
	prefix, err := encodedVectorSize(
		encodedMultiPrefix,
		leafCount,
		8+sha256.Size,
	)
	if err != nil {
		return 0, err
	}

	return encodedVectorSize(
		prefix+encodedMultiSuffix,
		frontierCount,
		sha256.Size,
	)
}

func maxInt() int {
	return int(^uint(0) >> 1)
}

func profileFromInclusionProof(proof InclusionProof) Profile {
	return Profile{
		id:        proof.profileID,
		version:   proof.profileVersion,
		algorithm: proof.algorithm,
	}
}

func profileFromConsistencyProof(proof ConsistencyProof) Profile {
	return Profile{
		id:        proof.profileID,
		version:   proof.profileVersion,
		algorithm: proof.algorithm,
	}
}

func profileFromMultiProof(proof MultiInclusionProof) Profile {
	return Profile{
		id:        proof.profileID,
		version:   proof.profileVersion,
		algorithm: proof.algorithm,
	}
}
