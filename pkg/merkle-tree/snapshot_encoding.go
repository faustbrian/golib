package merkletree

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"math"
	"math/bits"
)

const (
	encodingObjectSnapshot = byte(5)

	encodedSnapshotBase     = encodingHeaderSize + 8 + 8 + sha256.Size + 8 + 8
	encodedSnapshotNodeSize = uint64(sha256.Size + 8 + 8 + 8)
	defaultSnapshotBytes    = uint64(64 << 20)
)

// SnapshotPersistenceLimits bounds canonical snapshot decoding and structural
// validation. Every field must be nonzero. Values are copied and remain
// immutable during an operation.
type SnapshotPersistenceLimits struct {
	MaxEncodedBytes   uint64
	MaxLeaves         uint64
	MaxTotalLeafBytes uint64
	MaxRetainedNodes  uint64
	MaxTraversalDepth uint64
	MaxNodeReads      uint64
	MaxTemporaryBytes uint64
}

// DefaultSnapshotPersistenceLimits permits the default in-memory snapshot
// size, every uint64 tree depth, and 64 MiB each of encoded and temporary data.
func DefaultSnapshotPersistenceLimits() SnapshotPersistenceLimits {
	snapshotLimits := DefaultSnapshotLimits()

	return SnapshotPersistenceLimits{
		MaxEncodedBytes:   defaultSnapshotBytes,
		MaxLeaves:         snapshotLimits.Construction.MaxLeaves,
		MaxTotalLeafBytes: snapshotLimits.Construction.MaxTotalBytes,
		MaxRetainedNodes:  snapshotLimits.MaxRetainedNodes,
		MaxTraversalDepth: defaultMaxProofElements,
		MaxNodeReads:      snapshotLimits.MaxRetainedNodes,
		MaxTemporaryBytes: defaultSnapshotBytes,
	}
}

func (limits SnapshotPersistenceLimits) validate() error {
	if limits.MaxEncodedBytes == 0 ||
		limits.MaxLeaves == 0 ||
		limits.MaxTotalLeafBytes == 0 ||
		limits.MaxRetainedNodes == 0 ||
		limits.MaxTraversalDepth == 0 ||
		limits.MaxNodeReads == 0 ||
		limits.MaxTemporaryBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

// MarshalBinary returns the version-1 canonical persisted-snapshot encoding.
// It includes retained node digests and byte-accounting metadata, never raw
// leaf bytes.
func (snapshot Snapshot) MarshalBinary() ([]byte, error) {
	if err := snapshot.validate(); err != nil {
		return nil, err
	}
	if err := validateSnapshotStructure(
		context.Background(),
		snapshot,
		math.MaxUint64,
		math.MaxUint64,
	); err != nil {
		return nil, ErrInvalidSnapshot
	}
	size, _ := encodedVectorSize(
		encodedSnapshotBase,
		uint64(len(snapshot.nodes)),
		encodedSnapshotNodeSize,
	)

	result := make([]byte, size)
	appendEncodingHeader(result, encodingObjectSnapshot, snapshot.profile)
	offset := encodingHeaderSize
	binary.BigEndian.PutUint64(result[offset:offset+8], snapshot.root.treeSize)
	offset += 8
	binary.BigEndian.PutUint64(result[offset:offset+8], snapshot.totalBytes)
	offset += 8
	copy(result[offset:offset+sha256.Size], snapshot.root.digest.value[:])
	offset += sha256.Size
	binary.BigEndian.PutUint64(
		result[offset:offset+8],
		uint64(len(snapshot.nodes)),
	)
	offset += 8
	binary.BigEndian.PutUint64(result[offset:offset+8], snapshot.rootNode)
	offset += 8
	for _, node := range snapshot.nodes {
		copy(result[offset:offset+sha256.Size], node.digest[:])
		offset += sha256.Size
		binary.BigEndian.PutUint64(result[offset:offset+8], node.size)
		offset += 8
		binary.BigEndian.PutUint64(result[offset:offset+8], node.left)
		offset += 8
		binary.BigEndian.PutUint64(result[offset:offset+8], node.right)
		offset += 8
	}

	return result, nil
}

// ParseSnapshot decodes and validates one complete version-1 persisted
// snapshot. Successful results own their retained nodes and are safe for
// concurrent read-only use.
func ParseSnapshot(
	ctx context.Context,
	data []byte,
	limits SnapshotPersistenceLimits,
) (Snapshot, error) {
	if ctx == nil {
		return Snapshot{}, ErrInvalidContext
	}
	if err := limits.validate(); err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	dataSize := uint64(len(data))
	if dataSize > limits.MaxEncodedBytes {
		return Snapshot{}, &ResourceError{
			Kind:   ResourceEncodedBytes,
			Limit:  limits.MaxEncodedBytes,
			Actual: dataSize,
		}
	}
	if len(data) < encodedSnapshotBase {
		return Snapshot{}, ErrMalformedEncoding
	}
	profile, err := parseEncodingHeader(data, encodingObjectSnapshot)
	if err != nil {
		return Snapshot{}, err
	}

	offset := encodingHeaderSize
	treeSize := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	totalBytes := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	var rootDigest [sha256.Size]byte
	copy(rootDigest[:], data[offset:offset+sha256.Size])
	offset += sha256.Size
	nodeCount := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8
	rootNode := binary.BigEndian.Uint64(data[offset : offset+8])
	offset += 8

	if treeSize > limits.MaxLeaves {
		return Snapshot{}, &ResourceError{
			Kind:   ResourceLeaves,
			Limit:  limits.MaxLeaves,
			Actual: treeSize,
		}
	}
	if totalBytes > limits.MaxTotalLeafBytes {
		return Snapshot{}, &ResourceError{
			Kind:   ResourceTotalBytes,
			Limit:  limits.MaxTotalLeafBytes,
			Actual: totalBytes,
		}
	}
	if nodeCount > limits.MaxRetainedNodes {
		return Snapshot{}, &ResourceError{
			Kind:   ResourceRetainedNodes,
			Limit:  limits.MaxRetainedNodes,
			Actual: nodeCount,
		}
	}
	if nodeCount > limits.MaxNodeReads {
		return Snapshot{}, &ResourceError{
			Kind:   ResourceNodeReads,
			Limit:  limits.MaxNodeReads,
			Actual: nodeCount,
		}
	}
	depth := uint64(bits.Len64(treeSize))
	if depth > limits.MaxTraversalDepth {
		return Snapshot{}, &ResourceError{
			Kind:   ResourceTraversalDepth,
			Limit:  limits.MaxTraversalDepth,
			Actual: depth,
		}
	}
	temporaryBytes, sizeErr := encodedVectorSize(
		0,
		nodeCount,
		encodedSnapshotNodeSize,
	)
	if sizeErr != nil {
		return Snapshot{}, ErrMalformedEncoding
	}
	if uint64(temporaryBytes) > limits.MaxTemporaryBytes {
		return Snapshot{}, &ResourceError{
			Kind:   ResourceTemporaryBytes,
			Limit:  limits.MaxTemporaryBytes,
			Actual: uint64(temporaryBytes),
		}
	}
	size, sizeErr := encodedVectorSize(
		encodedSnapshotBase,
		nodeCount,
		encodedSnapshotNodeSize,
	)
	if sizeErr != nil || size != len(data) {
		return Snapshot{}, ErrMalformedEncoding
	}
	if !validSnapshotNodeCount(treeSize, nodeCount) {
		return Snapshot{}, ErrMalformedEncoding
	}

	nodes := make([]snapshotNode, int(nodeCount))
	for index := range nodes {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		copy(nodes[index].digest[:], data[offset:offset+sha256.Size])
		offset += sha256.Size
		nodes[index].size = binary.BigEndian.Uint64(data[offset : offset+8])
		offset += 8
		nodes[index].left = binary.BigEndian.Uint64(data[offset : offset+8])
		offset += 8
		nodes[index].right = binary.BigEndian.Uint64(data[offset : offset+8])
		offset += 8
	}

	snapshot := Snapshot{
		profile:    profile,
		root:       newRoot(profile, treeSize, rootDigest),
		nodes:      nodes,
		rootNode:   rootNode,
		totalBytes: totalBytes,
	}
	if err := validateSnapshotStructure(
		ctx,
		snapshot,
		limits.MaxTraversalDepth,
		limits.MaxNodeReads,
	); err != nil {
		var resourceErr *ResourceError
		if errors.As(err, &resourceErr) ||
			errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return Snapshot{}, err
		}

		return Snapshot{}, ErrMalformedEncoding
	}

	return snapshot, nil
}

// ResumeBuilder creates an independent mutable builder from a validated
// snapshot. expectedTotalLeafBytes must come from caller-trusted state because
// the selected Merkle profile does not commit raw leaf lengths. Old raw leaf
// bytes are not recovered or retained.
func ResumeBuilder(
	ctx context.Context,
	snapshot Snapshot,
	expectedTotalLeafBytes uint64,
	limits SnapshotLimits,
) (*Builder, error) {
	if ctx == nil {
		return nil, ErrInvalidContext
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	if err := snapshot.validate(); err != nil {
		return nil, err
	}
	if snapshot.totalBytes != expectedTotalLeafBytes {
		return nil, ErrSnapshotAccountingMismatch
	}
	if snapshot.root.treeSize > limits.Construction.MaxLeaves {
		return nil, &ResourceError{
			Kind:   ResourceLeaves,
			Limit:  limits.Construction.MaxLeaves,
			Actual: snapshot.root.treeSize,
		}
	}
	if snapshot.totalBytes > limits.Construction.MaxTotalBytes {
		return nil, &ResourceError{
			Kind:   ResourceTotalBytes,
			Limit:  limits.Construction.MaxTotalBytes,
			Actual: snapshot.totalBytes,
		}
	}
	nodeCount := snapshotNodeCount(snapshot.root.treeSize)
	if nodeCount > limits.MaxRetainedNodes {
		return nil, &ResourceError{
			Kind:   ResourceRetainedNodes,
			Limit:  limits.MaxRetainedNodes,
			Actual: nodeCount,
		}
	}
	if err := validateSnapshotStructure(
		ctx,
		snapshot,
		defaultMaxProofElements,
		limits.MaxRetainedNodes,
	); err != nil {
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}

		return nil, ErrInvalidSnapshot
	}

	builder := &Builder{
		initialized: true,
		profile:     snapshot.profile,
		limits:      limits,
		treeSize:    snapshot.root.treeSize,
		totalBytes:  snapshot.totalBytes,
	}
	if snapshot.root.treeSize == 0 {
		return builder, nil
	}

	builder.nodes = make([]snapshotNode, 0, int(nodeCount))
	builder.frontier = make(
		[]uint64,
		0,
		bits.Len64(snapshot.root.treeSize),
	)
	var completed uint64
	for _, node := range snapshot.nodes {
		if node.size != 1 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		builder.nodes = append(builder.nodes, node)
		builder.frontier = append(
			builder.frontier,
			uint64(len(builder.nodes)-1),
		)
		completed++
		for merged := completed; merged&1 == 0; merged >>= 1 {
			right := builder.frontier[len(builder.frontier)-1]
			left := builder.frontier[len(builder.frontier)-2]
			builder.frontier = builder.frontier[:len(builder.frontier)-2]
			builder.nodes = append(
				builder.nodes,
				newSnapshotBranch(builder.nodes, left, right),
			)
			builder.frontier = append(
				builder.frontier,
				uint64(len(builder.nodes)-1),
			)
		}
	}
	return builder, nil
}

func validateSnapshotStructure(
	ctx context.Context,
	snapshot Snapshot,
	maxDepth uint64,
	maxNodeReads uint64,
) error {
	treeSize := snapshot.root.treeSize
	if !validSnapshotNodeCount(treeSize, uint64(len(snapshot.nodes))) {
		return ErrInvalidSnapshot
	}
	if treeSize == 0 {
		if snapshot.rootNode != noSnapshotNode || snapshot.totalBytes != 0 {
			return ErrInvalidSnapshot
		}

		empty := sha256.Sum256(nil)
		if subtle.ConstantTimeCompare(
			snapshot.root.digest.value[:],
			empty[:],
		) != 1 {
			return ErrInvalidSnapshot
		}

		return nil
	}
	if snapshot.rootNode != uint64(len(snapshot.nodes)-1) {
		return ErrInvalidSnapshot
	}
	rootNode := snapshot.nodes[snapshot.rootNode]
	if rootNode.size != treeSize {
		return ErrInvalidSnapshot
	}
	if subtle.ConstantTimeCompare(
		rootNode.digest[:],
		snapshot.root.digest.value[:],
	) != 1 {
		return ErrInvalidSnapshot
	}

	type pendingNode struct {
		index uint64
		start uint64
		depth uint64
	}
	stack := []pendingNode{{index: snapshot.rootNode}}
	var reads uint64
	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		reads++
		if reads > maxNodeReads {
			return &ResourceError{
				Kind:   ResourceNodeReads,
				Limit:  maxNodeReads,
				Actual: reads,
			}
		}
		if current.depth > maxDepth {
			return ErrInvalidSnapshot
		}
		node := snapshot.nodes[current.index]
		if node.size == 1 {
			if current.index != current.start ||
				node.left != noSnapshotNode ||
				node.right != noSnapshotNode {
				return ErrInvalidSnapshot
			}

			continue
		}
		leftSize := largestPowerOfTwoBelow(node.size)
		rightSize := node.size - leftSize
		rightNodes := snapshotNodeCount(rightSize)
		wantRight := current.index - 1
		wantLeft := current.index - rightNodes - 1
		if node.left != wantLeft || node.right != wantRight {
			return ErrInvalidSnapshot
		}
		left := snapshot.nodes[wantLeft]
		right := snapshot.nodes[wantRight]
		if left.size != leftSize {
			return ErrInvalidSnapshot
		}
		if right.size != rightSize {
			return ErrInvalidSnapshot
		}
		digest := hashBranch(left.digest, right.digest)
		if subtle.ConstantTimeCompare(node.digest[:], digest[:]) != 1 {
			return ErrInvalidSnapshot
		}
		stack = append(
			stack,
			pendingNode{
				index: wantRight,
				start: current.start + snapshotNodeCount(leftSize),
				depth: current.depth + 1,
			},
			pendingNode{
				index: wantLeft,
				start: current.start,
				depth: current.depth + 1,
			},
		)
	}
	return nil
}

func validSnapshotNodeCount(treeSize, nodeCount uint64) bool {
	if treeSize == 0 {
		return nodeCount == 0
	}
	if treeSize > math.MaxUint64/2+1 {
		return false
	}

	return nodeCount == snapshotNodeCount(treeSize)
}
