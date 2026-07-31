package committedtree

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	internalprofile "github.com/faustbrian/golib/pkg/verkle-tree/internal/profile"
)

const (
	storageNodeMagicBytes      = 4
	storageNodeProfileIDBytes  = 1
	storageNodeVersionBytes    = 2
	storageNodeEncodingBytes   = 2
	storageNodeKindBytes       = 1
	storageNodeDepthBytes      = 1
	storageCommitmentKindBytes = 1
	storageCommitmentBytes     = storageCommitmentKindBytes +
		backend.CommitmentSize
	storageNodeCountBytes  = 2
	storageNodeHeaderBytes = storageNodeMagicBytes +
		storageNodeProfileIDBytes +
		storageNodeVersionBytes +
		storageNodeEncodingBytes +
		storageNodeKindBytes +
		storageNodeDepthBytes +
		storageCommitmentBytes
	storageInternalEdgeBytes = 1 + sha256.Size
	storageStemBytes         = 31
	storageStemEntryBytes    = 1 + 32
	storageNodeWorkingBytes  = uint64(64)
)

var (
	storageNodeMagic = [storageNodeMagicBytes]byte{'V', 'K', 'N', 'D'}

	errInvalidStorageEncodingLimits = errors.New(
		"invalid committed-tree storage encoding limits",
	)
	errInvalidStorageImage = errors.New(
		"invalid committed-tree storage image",
	)
	errStorageEncodingResource = errors.New(
		"committed-tree storage encoding resource limit exceeded",
	)
)

// StorageEncodingLimits bounds canonical node encoding and content hashing.
// Every field must be positive and no field denotes an unbounded resource.
type StorageEncodingLimits struct {
	MaxNodes          uint32
	MaxNodeBytes      uint64
	MaxEncodedBytes   uint64
	MaxHashes         uint32
	MaxTemporaryBytes uint64
}

func (limits StorageEncodingLimits) validate() error {
	if limits.MaxNodes == 0 ||
		limits.MaxNodes > maxSupportedCount ||
		limits.MaxNodeBytes == 0 ||
		limits.MaxEncodedBytes == 0 ||
		limits.MaxHashes == 0 ||
		limits.MaxHashes > maxSupportedCount ||
		limits.MaxTemporaryBytes == 0 {
		return errInvalidStorageEncodingLimits
	}

	return nil
}

// StorageEncodingResource identifies one bounded node-encoding resource.
type StorageEncodingResource uint8

const (
	// StorageEncodingResourceNodes counts immutable logical nodes.
	StorageEncodingResourceNodes StorageEncodingResource = iota + 1

	// StorageEncodingResourceNodeBytes counts one canonical encoded node.
	StorageEncodingResourceNodeBytes

	// StorageEncodingResourceEncodedBytes counts all canonical node bytes.
	StorageEncodingResourceEncodedBytes

	// StorageEncodingResourceHashes counts content-address calculations.
	StorageEncodingResourceHashes

	// StorageEncodingResourceTemporaryBytes counts owned encodings, records,
	// and deterministic sorting scratch.
	StorageEncodingResourceTemporaryBytes
)

// StorageEncodingResourceError reports one rejected storage-encoding bound.
type StorageEncodingResourceError struct {
	Resource StorageEncodingResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *StorageEncodingResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		errStorageEncodingResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes StorageEncodingResourceError match the package resource
// sentinel.
func (err *StorageEncodingResourceError) Unwrap() error {
	return errStorageEncodingResource
}

// StorageNodeID is the SHA-256 content address of one complete canonical,
// profile-bound stored node.
type StorageNodeID [sha256.Size]byte

// StorageNode is one immutable content-addressed canonical node.
type StorageNode struct {
	id      StorageNodeID
	encoded []byte
}

// ID returns the node's exact content address.
func (node StorageNode) ID() StorageNodeID {
	return node.id
}

// Encoded returns a caller-owned copy of the canonical node bytes.
func (node StorageNode) Encoded() []byte {
	return append([]byte(nil), node.encoded...)
}

// StorageImage is one immutable complete set of nodes for an exact tree.
type StorageImage struct {
	root  StorageNodeID
	nodes []StorageNode
	valid bool
}

// RootID returns the content address of the image's root node.
func (image StorageImage) RootID() (StorageNodeID, error) {
	if !image.valid || len(image.nodes) == 0 {
		return StorageNodeID{}, errInvalidStorageImage
	}

	return image.root, nil
}

// Nodes returns owned canonical nodes in ascending content-address order.
func (image StorageImage) Nodes(ctx context.Context) ([]StorageNode, error) {
	if !image.valid || len(image.nodes) == 0 {
		return nil, errInvalidStorageImage
	}
	if err := checkContext(ctx); err != nil {
		return nil, err
	}

	nodes := make([]StorageNode, len(image.nodes))
	for index := range image.nodes {
		if err := checkContext(ctx); err != nil {
			return nil, err
		}
		nodes[index] = StorageNode{
			id:      image.nodes[index].id,
			encoded: image.nodes[index].Encoded(),
		}
	}

	return nodes, nil
}

// StorageImage canonically encodes every logical node, binds child references
// by content address, and returns a complete immutable storage image.
func (tree Tree) StorageImage(
	ctx context.Context,
	limits StorageEncodingLimits,
) (StorageImage, error) {
	if err := checkContext(ctx); err != nil {
		return StorageImage{}, err
	}
	if err := tree.validateStorageTree(); err != nil {
		return StorageImage{}, err
	}
	if err := limits.validate(); err != nil {
		return StorageImage{}, err
	}

	nodeCount := uint64(len(tree.nodes))
	if err := checkStorageEncodingResource(
		StorageEncodingResourceNodes,
		uint64(limits.MaxNodes),
		nodeCount,
	); err != nil {
		return StorageImage{}, err
	}
	if err := checkStorageEncodingResource(
		StorageEncodingResourceHashes,
		uint64(limits.MaxHashes),
		nodeCount,
	); err != nil {
		return StorageImage{}, err
	}

	sizes, total, err := tree.storageNodeSizes(ctx, limits)
	if err != nil {
		return StorageImage{}, err
	}
	// Profile fan-out and entry-count invariants cap every encoded node below
	// 9 KiB, while MaxNodes is capped below 2^31, so this arithmetic cannot
	// overflow uint64.
	working := 2*total + 4*nodeCount*storageNodeWorkingBytes
	if err := checkStorageEncodingResource(
		StorageEncodingResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		working,
	); err != nil {
		return StorageImage{}, err
	}
	if err := tree.validateStorageTopology(ctx); err != nil {
		return StorageImage{}, err
	}

	byBuildOrder := make([]StorageNode, len(tree.nodes))
	for index := range tree.nodes {
		if err := checkContext(ctx); err != nil {
			return StorageImage{}, err
		}
		encoded, encodeErr := tree.encodeStorageNode(
			ctx,
			uint32(index),
			sizes[index],
			byBuildOrder,
		)
		if encodeErr != nil {
			return StorageImage{}, encodeErr
		}
		byBuildOrder[index] = StorageNode{
			id:      StorageNodeID(sha256.Sum256(encoded)),
			encoded: encoded,
		}
	}
	rootID := byBuildOrder[tree.root].id
	nodes := make([]StorageNode, len(byBuildOrder))
	copy(nodes, byBuildOrder)
	scratch := make([]StorageNode, len(nodes))
	if err := sortStorageNodes(ctx, nodes, scratch, 0, len(nodes)); err != nil {
		return StorageImage{}, err
	}

	return StorageImage{root: rootID, nodes: nodes, valid: true}, nil
}

func (tree Tree) validateStorageTree() error {
	if !tree.valid ||
		len(tree.nodes) == 0 ||
		uint64(tree.root) >= uint64(len(tree.nodes)) {
		return errInvalidTree
	}

	return nil
}

func (tree Tree) validateStorageTopology(ctx context.Context) error {
	if tree.root != uint32(len(tree.nodes)-1) ||
		len(tree.edges) != len(tree.nodes)-1 ||
		tree.nodes[tree.root].kind != nodeInternal ||
		tree.nodes[tree.root].depth != 0 {
		return errInvalidTree
	}

	var prefix [31]byte
	nodeCursor := uint64(0)
	edgeCursor := uint64(0)
	entryCursor := uint64(0)
	if err := tree.validateStorageSubtree(
		ctx,
		tree.root,
		0,
		prefix,
		&nodeCursor,
		&edgeCursor,
		&entryCursor,
	); err != nil {
		return err
	}
	if entryCursor != uint64(len(tree.entries)) {
		return errInvalidTree
	}

	return nil
}

func (tree Tree) validateStorageSubtree(
	ctx context.Context,
	index uint32,
	expectedDepth uint8,
	prefix [31]byte,
	nodeCursor *uint64,
	edgeCursor *uint64,
	entryCursor *uint64,
) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if uint64(index) >= uint64(len(tree.nodes)) {
		return errInvalidTree
	}
	current := tree.nodes[index]
	if current.depth != expectedDepth {
		return errInvalidTree
	}

	switch current.kind {
	case nodeInternal:
		if current.depth > 30 ||
			(current.edgeCount == 0 && current.depth != 0) ||
			uint64(current.firstEdge) != *edgeCursor {
			return errInvalidTree
		}
		first := *edgeCursor
		end := first + uint64(current.edgeCount)
		*edgeCursor = end
		if end > uint64(len(tree.edges)) {
			return errInvalidTree
		}
		for edgeIndex := first; edgeIndex < end; edgeIndex++ {
			if err := checkContext(ctx); err != nil {
				return err
			}
			currentEdge := tree.edges[edgeIndex]
			childPrefix := prefix
			childPrefix[current.depth] = currentEdge.index
			if err := tree.validateStorageSubtree(
				ctx,
				currentEdge.child,
				current.depth+1,
				childPrefix,
				nodeCursor,
				edgeCursor,
				entryCursor,
			); err != nil {
				return err
			}
		}
	case nodeStem:
		depth := int(current.depth)
		if !bytes.Equal(current.stem[:depth], prefix[:depth]) ||
			uint64(current.entryStart) != *entryCursor {
			return errInvalidTree
		}
		*entryCursor += uint64(current.entryCount)
		if *entryCursor > uint64(len(tree.entries)) {
			return errInvalidTree
		}
	default:
		return errInvalidTree
	}
	if uint64(index) != *nodeCursor {
		return errInvalidTree
	}
	*nodeCursor++

	return nil
}

func (tree Tree) storageNodeSizes(
	ctx context.Context,
	limits StorageEncodingLimits,
) ([]uint64, uint64, error) {
	sizes := make([]uint64, len(tree.nodes))
	total := uint64(0)
	for index := range tree.nodes {
		if err := checkContext(ctx); err != nil {
			return nil, 0, err
		}
		size, err := tree.storageNodeSize(uint32(index))
		if err != nil {
			return nil, 0, err
		}
		if err := checkStorageEncodingResource(
			StorageEncodingResourceNodeBytes,
			limits.MaxNodeBytes,
			size,
		); err != nil {
			return nil, 0, err
		}
		total += size
		sizes[index] = size
	}
	if err := checkStorageEncodingResource(
		StorageEncodingResourceEncodedBytes,
		limits.MaxEncodedBytes,
		total,
	); err != nil {
		return nil, 0, err
	}

	return sizes, total, nil
}

func (tree Tree) storageNodeSize(index uint32) (uint64, error) {
	if uint64(index) >= uint64(len(tree.nodes)) {
		return 0, errInvalidTree
	}
	current := tree.nodes[index]
	switch current.kind {
	case nodeInternal:
		first := uint64(current.firstEdge)
		end := first + uint64(current.edgeCount)
		if end > uint64(len(tree.edges)) {
			return 0, errInvalidTree
		}
		identity, err := current.commitment.IsIdentity()
		if err != nil || identity != (current.edgeCount == 0) {
			return 0, errInvalidTree
		}
		previous := -1
		for edgeIndex := first; edgeIndex < end; edgeIndex++ {
			currentEdge := tree.edges[edgeIndex]
			if int(currentEdge.index) <= previous ||
				currentEdge.child >= index {
				return 0, errInvalidTree
			}
			previous = int(currentEdge.index)
		}

		return storageNodeHeaderBytes +
			storageNodeCountBytes +
			uint64(current.edgeCount)*storageInternalEdgeBytes, nil
	case nodeStem:
		end := uint64(current.entryStart) + uint64(current.entryCount)
		if current.depth == 0 ||
			current.depth > 31 ||
			end > uint64(len(tree.entries)) ||
			current.entryCount == 0 ||
			current.edgeCount != 0 {
			return 0, errInvalidTree
		}
		identity, err := current.commitment.IsIdentity()
		if err != nil || identity {
			return 0, errInvalidTree
		}
		if _, err := current.c1.IsIdentity(); err != nil {
			return 0, errInvalidTree
		}
		if _, err := current.c2.IsIdentity(); err != nil {
			return 0, errInvalidTree
		}
		previous := -1
		for entryIndex := uint64(current.entryStart); entryIndex < end; entryIndex++ {
			entry := tree.entries[entryIndex]
			if !bytes.Equal(entry.Key[:31], current.stem[:]) ||
				int(entry.Key[31]) <= previous {
				return 0, errInvalidTree
			}
			previous = int(entry.Key[31])
		}

		return storageNodeHeaderBytes +
			storageStemBytes +
			2*storageCommitmentBytes +
			storageNodeCountBytes +
			uint64(current.entryCount)*storageStemEntryBytes, nil
	default:
		return 0, errInvalidTree
	}
}

func (tree Tree) encodeStorageNode(
	ctx context.Context,
	index uint32,
	size uint64,
	encodedNodes []StorageNode,
) ([]byte, error) {
	if err := checkContext(ctx); err != nil {
		return nil, err
	}
	current := tree.nodes[index]
	encoded := make([]byte, int(size))
	copy(encoded, storageNodeMagic[:])
	offset := storageNodeMagicBytes
	profile := internalprofile.ExperimentalBandersnatchIPA256V0()
	encoded[offset] = byte(profile.ID())
	offset += storageNodeProfileIDBytes
	binary.BigEndian.PutUint16(
		encoded[offset:offset+storageNodeVersionBytes],
		profile.Version(),
	)
	offset += storageNodeVersionBytes
	binary.BigEndian.PutUint16(
		encoded[offset:offset+storageNodeEncodingBytes],
		profile.EncodingVersion(),
	)
	offset += storageNodeEncodingBytes
	encoded[offset] = byte(current.kind)
	offset += storageNodeKindBytes
	encoded[offset] = current.depth
	offset += storageNodeDepthBytes
	next, err := encodeStorageCommitment(
		encoded,
		offset,
		current.commitment,
	)
	if err != nil {
		return nil, errInvalidTree
	}
	offset = next

	switch current.kind {
	case nodeInternal:
		binary.BigEndian.PutUint16(
			encoded[offset:offset+storageNodeCountBytes],
			current.edgeCount,
		)
		offset += storageNodeCountBytes
		first := uint64(current.firstEdge)
		end := first + uint64(current.edgeCount)
		for edgeIndex := first; edgeIndex < end; edgeIndex++ {
			if err := checkContext(ctx); err != nil {
				return nil, err
			}
			currentEdge := tree.edges[edgeIndex]
			encoded[offset] = currentEdge.index
			offset++
			child := encodedNodes[currentEdge.child]
			copy(encoded[offset:offset+sha256.Size], child.id[:])
			offset += sha256.Size
		}
	case nodeStem:
		copy(encoded[offset:offset+storageStemBytes], current.stem[:])
		offset += storageStemBytes
		offset, err = encodeStorageCommitment(encoded, offset, current.c1)
		if err != nil {
			return nil, errInvalidTree
		}
		offset, err = encodeStorageCommitment(encoded, offset, current.c2)
		if err != nil {
			return nil, errInvalidTree
		}
		binary.BigEndian.PutUint16(
			encoded[offset:offset+storageNodeCountBytes],
			uint16(current.entryCount),
		)
		offset += storageNodeCountBytes
		end := uint64(current.entryStart) + uint64(current.entryCount)
		for entryIndex := uint64(current.entryStart); entryIndex < end; entryIndex++ {
			if err := checkContext(ctx); err != nil {
				return nil, err
			}
			entry := tree.entries[entryIndex]
			encoded[offset] = entry.Key[31]
			offset++
			copy(encoded[offset:offset+32], entry.Value[:])
			offset += 32
		}
	default:
		return nil, errInvalidTree
	}
	if offset != len(encoded) {
		return nil, errInvalidTree
	}

	return encoded, nil
}

func encodeStorageCommitment(
	target []byte,
	offset int,
	value backend.VectorCommitment,
) (int, error) {
	encoded, err := value.DeduplicationKey()
	if err != nil {
		return 0, err
	}
	if encoded == ([backend.CommitmentSize]byte{}) {
		target[offset] = 0
		return offset + storageCommitmentBytes, nil
	}
	target[offset] = 1
	copy(
		target[offset+storageCommitmentKindBytes:offset+storageCommitmentBytes],
		encoded[:],
	)

	return offset + storageCommitmentBytes, nil
}

func sortStorageNodes(
	ctx context.Context,
	nodes []StorageNode,
	scratch []StorageNode,
	start int,
	end int,
) error {
	if err := checkContext(ctx); err != nil {
		return err
	}
	if end-start < 2 {
		return nil
	}
	middle := start + (end-start)/2
	if err := sortStorageNodes(ctx, nodes, scratch, start, middle); err != nil {
		return err
	}
	if err := sortStorageNodes(ctx, nodes, scratch, middle, end); err != nil {
		return err
	}

	left := start
	right := middle
	for output := start; output < end; output++ {
		if err := checkContext(ctx); err != nil {
			return err
		}
		if right == end ||
			(left < middle &&
				bytes.Compare(nodes[left].id[:], nodes[right].id[:]) <= 0) {
			scratch[output] = nodes[left]
			left++
		} else {
			scratch[output] = nodes[right]
			right++
		}
	}
	for index := start; index < end; index++ {
		if err := checkContext(ctx); err != nil {
			return err
		}
		nodes[index] = scratch[index]
	}

	return nil
}

func checkStorageEncodingResource(
	resource StorageEncodingResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &StorageEncodingResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}
