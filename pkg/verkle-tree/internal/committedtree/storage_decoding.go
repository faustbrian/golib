package committedtree

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	internalprofile "github.com/faustbrian/golib/pkg/verkle-tree/internal/profile"
)

// The retained-node budget deliberately exceeds three backend commitments,
// slice headers, fixed metadata, and allocator alignment on every supported
// architecture.
const storageDecodedNodeWorkingBytes = uint64(768)

var (
	// ErrInvalidStorageDecodingContext identifies a nil decoding context.
	ErrInvalidStorageDecodingContext = errors.New(
		"invalid committed-tree storage decoding context",
	)
	// ErrInvalidStorageDecodingLimits identifies an incomplete hostile-input
	// budget.
	ErrInvalidStorageDecodingLimits = errors.New(
		"invalid committed-tree storage decoding limits",
	)
	// ErrInvalidStorageNode identifies malformed or non-canonical persisted
	// node bytes.
	ErrInvalidStorageNode = errors.New("invalid committed-tree storage node")
	// ErrStorageNodeProfile identifies a stored-node profile or encoding
	// version that is not the exact package-owned profile.
	ErrStorageNodeProfile = errors.New("unsupported committed-tree storage node profile")
	// ErrStorageDecodingResource identifies an exhausted hostile-input budget.
	ErrStorageDecodingResource = errors.New(
		"committed-tree storage decoding resource limit exceeded",
	)
	// ErrStorageDecodingCancelled identifies cancellation during node decoding.
	ErrStorageDecodingCancelled = errors.New(
		"committed-tree storage decoding cancelled",
	)
)

// StorageDecodingLimits bounds one hostile canonical-node decode. Identity
// commitments do not consume point decodes. Every field except
// MaxPointDecodes must be positive; no field denotes an unbounded resource.
type StorageDecodingLimits struct {
	MaxNodeBytes      uint64
	MaxPointDecodes   uint32
	MaxTemporaryBytes uint64
}

func (limits StorageDecodingLimits) validate() error {
	if limits.MaxNodeBytes == 0 || limits.MaxTemporaryBytes == 0 {
		return ErrInvalidStorageDecodingLimits
	}

	return nil
}

// StorageDecodingResource identifies one bounded decoder resource.
type StorageDecodingResource uint8

const (
	// StorageDecodingResourceNodeBytes counts one untrusted encoded node.
	StorageDecodingResourceNodeBytes StorageDecodingResource = iota + 1
	// StorageDecodingResourcePointDecodes counts strict canonical point
	// decodings after the envelope has been validated.
	StorageDecodingResourcePointDecodes
	// StorageDecodingResourceTemporaryBytes counts the owned encoding and
	// decoded child or entry records.
	StorageDecodingResourceTemporaryBytes
)

// StorageDecodingResourceError reports one rejected node-decoding budget.
type StorageDecodingResourceError struct {
	Resource StorageDecodingResource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *StorageDecodingResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		ErrStorageDecodingResource,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes StorageDecodingResourceError match ErrStorageDecodingResource.
func (err *StorageDecodingResourceError) Unwrap() error {
	return ErrStorageDecodingResource
}

// StorageNodeKind is one fixed pre-v1 profile logical node kind.
type StorageNodeKind uint8

const (
	// StorageNodeKindInternal contains sorted child content addresses.
	StorageNodeKindInternal StorageNodeKind = StorageNodeKind(nodeInternal)
	// StorageNodeKindStem contains one stem and sorted present suffix values.
	StorageNodeKindStem StorageNodeKind = StorageNodeKind(nodeStem)
)

// StorageChild is one owned internal-node edge.
type StorageChild struct {
	Index byte
	ID    StorageNodeID
}

// DecodedStorageNode is one immutable, strictly decoded canonical node. Its
// zero value rejects accessors that can allocate or return retained content.
type DecodedStorageNode struct {
	kind         StorageNodeKind
	depth        uint8
	commitment   backend.VectorCommitment
	c1           backend.VectorCommitment
	c2           backend.VectorCommitment
	children     []StorageChild
	entries      []Entry
	encoded      []byte
	pointDecodes uint32
	valid        bool
}

// Kind returns the fixed logical node kind. Zero means the receiver is
// invalid.
func (node DecodedStorageNode) Kind() StorageNodeKind {
	if !node.valid {
		return 0
	}

	return node.kind
}

// Depth returns the canonical path depth. Zero is also the valid root depth.
func (node DecodedStorageNode) Depth() uint8 {
	if !node.valid {
		return 0
	}

	return node.depth
}

// PointDecodes returns the exact strict point decodes consumed by this node.
func (node DecodedStorageNode) PointDecodes() uint32 {
	if !node.valid {
		return 0
	}

	return node.pointDecodes
}

// RecordCount returns the exact child or entry count without allocating.
func (node DecodedStorageNode) RecordCount() uint16 {
	if !node.valid {
		return 0
	}
	if node.kind == StorageNodeKindInternal {
		return uint16(len(node.children))
	}

	return uint16(len(node.entries))
}

// TemporaryBytes returns the conservative owned decoder storage charged by
// StorageDecodingLimits.
func (node DecodedStorageNode) TemporaryBytes() uint64 {
	if !node.valid {
		return 0
	}

	return storageDecodedTemporaryBytes(
		uint64(len(node.encoded)),
		node.RecordCount(),
		node.kind,
	)
}

// Encoded returns an owned copy of the exact canonical bytes.
func (node DecodedStorageNode) Encoded(ctx context.Context) ([]byte, error) {
	if !node.valid || len(node.encoded) == 0 {
		return nil, ErrInvalidStorageNode
	}
	if err := checkStorageDecodingContext(ctx); err != nil {
		return nil, err
	}

	owned := make([]byte, len(node.encoded))
	copy(owned, node.encoded)

	return owned, nil
}

// Children returns owned sorted child edges for an internal node.
func (node DecodedStorageNode) Children(ctx context.Context) ([]StorageChild, error) {
	if !node.valid || node.kind != StorageNodeKindInternal {
		return nil, ErrInvalidStorageNode
	}
	if err := checkStorageDecodingContext(ctx); err != nil {
		return nil, err
	}

	owned := make([]StorageChild, len(node.children))
	copy(owned, node.children)

	return owned, nil
}

// Entries returns owned sorted key/value entries for a stem node.
func (node DecodedStorageNode) Entries(ctx context.Context) ([]Entry, error) {
	if !node.valid || node.kind != StorageNodeKindStem {
		return nil, ErrInvalidStorageNode
	}
	if err := checkStorageDecodingContext(ctx); err != nil {
		return nil, err
	}

	owned := make([]Entry, len(node.entries))
	copy(owned, node.entries)

	return owned, nil
}

// DecodeStorageNode validates the complete profile envelope and structural
// lengths before any point decoding, rejects alternate encodings and trailing
// bytes, and returns caller-independent immutable content.
func DecodeStorageNode(
	ctx context.Context,
	encoded []byte,
	limits StorageDecodingLimits,
) (DecodedStorageNode, error) {
	if err := checkStorageDecodingContext(ctx); err != nil {
		return DecodedStorageNode{}, err
	}
	if err := limits.validate(); err != nil {
		return DecodedStorageNode{}, err
	}
	if err := checkStorageDecodingResource(
		StorageDecodingResourceNodeBytes,
		limits.MaxNodeBytes,
		uint64(len(encoded)),
	); err != nil {
		return DecodedStorageNode{}, err
	}
	plan, err := inspectStorageNode(encoded)
	if err != nil {
		return DecodedStorageNode{}, err
	}
	if err := checkStorageDecodingResource(
		StorageDecodingResourcePointDecodes,
		uint64(limits.MaxPointDecodes),
		uint64(plan.pointDecodes),
	); err != nil {
		return DecodedStorageNode{}, err
	}
	temporary := storageDecodedTemporaryBytes(
		uint64(len(encoded)),
		plan.count,
		plan.kind,
	)
	if err := checkStorageDecodingResource(
		StorageDecodingResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporary,
	); err != nil {
		return DecodedStorageNode{}, err
	}

	owned := make([]byte, len(encoded))
	copy(owned, encoded)
	node := DecodedStorageNode{
		kind:         plan.kind,
		depth:        plan.depth,
		encoded:      owned,
		pointDecodes: plan.pointDecodes,
		valid:        true,
	}
	offset := storageNodeHeaderBytes - storageCommitmentBytes
	node.commitment, offset, err = decodeStorageCommitment(ctx, owned, offset)
	if err != nil {
		return DecodedStorageNode{}, err
	}

	switch plan.kind {
	case StorageNodeKindInternal:
		offset += storageNodeCountBytes
		node.children = make([]StorageChild, plan.count)
		for index := range node.children {
			if err := checkStorageDecodingContext(ctx); err != nil {
				return DecodedStorageNode{}, err
			}
			node.children[index].Index = owned[offset]
			offset++
			copy(node.children[index].ID[:], owned[offset:offset+len(StorageNodeID{})])
			offset += len(StorageNodeID{})
		}
	case StorageNodeKindStem:
		var stem [storageStemBytes]byte
		copy(stem[:], owned[offset:offset+storageStemBytes])
		offset += storageStemBytes
		node.c1, offset, err = decodeStorageCommitment(ctx, owned, offset)
		if err != nil {
			return DecodedStorageNode{}, err
		}
		node.c2, offset, err = decodeStorageCommitment(ctx, owned, offset)
		if err != nil {
			return DecodedStorageNode{}, err
		}
		offset += storageNodeCountBytes
		node.entries = make([]Entry, plan.count)
		for index := range node.entries {
			if err := checkStorageDecodingContext(ctx); err != nil {
				return DecodedStorageNode{}, err
			}
			copy(node.entries[index].Key[:31], stem[:])
			node.entries[index].Key[31] = owned[offset]
			offset++
			copy(node.entries[index].Value[:], owned[offset:offset+32])
			offset += 32
		}
	}

	return node, nil
}

type storageDecodingPlan struct {
	kind         StorageNodeKind
	depth        uint8
	count        uint16
	pointDecodes uint32
}

func inspectStorageNode(encoded []byte) (storageDecodingPlan, error) {
	if len(encoded) < storageMinimumNodeBytes() ||
		!bytes.Equal(encoded[:storageNodeMagicBytes], storageNodeMagic[:]) {
		return storageDecodingPlan{}, ErrInvalidStorageNode
	}
	offset := storageNodeMagicBytes
	profile := internalprofile.BandersnatchIPA256V0Profile()
	if encoded[offset] != byte(profile.ID()) {
		return storageDecodingPlan{}, errors.Join(ErrInvalidStorageNode, ErrStorageNodeProfile)
	}
	offset += storageNodeProfileIDBytes
	if binary.BigEndian.Uint16(encoded[offset:offset+storageNodeVersionBytes]) != profile.Version() {
		return storageDecodingPlan{}, errors.Join(ErrInvalidStorageNode, ErrStorageNodeProfile)
	}
	offset += storageNodeVersionBytes
	if binary.BigEndian.Uint16(encoded[offset:offset+storageNodeEncodingBytes]) != profile.EncodingVersion() {
		return storageDecodingPlan{}, errors.Join(ErrInvalidStorageNode, ErrStorageNodeProfile)
	}
	offset += storageNodeEncodingBytes
	plan := storageDecodingPlan{kind: StorageNodeKind(encoded[offset])}
	offset += storageNodeKindBytes
	plan.depth = encoded[offset]
	offset += storageNodeDepthBytes
	mainPoint, err := inspectStorageCommitment(encoded, offset)
	if err != nil {
		return storageDecodingPlan{}, err
	}
	plan.pointDecodes = mainPoint
	offset += storageCommitmentBytes

	switch plan.kind {
	case StorageNodeKindInternal:
		if !validStorageInternalDepth(plan.depth) {
			return storageDecodingPlan{}, ErrInvalidStorageNode
		}
		plan.count = binary.BigEndian.Uint16(encoded[offset : offset+storageNodeCountBytes])
		offset += storageNodeCountBytes
		if !storageRecordCountFits(plan.count) {
			return storageDecodingPlan{}, ErrInvalidStorageNode
		}
		want := uint64(offset) + uint64(plan.count)*storageInternalEdgeBytes
		if want != uint64(len(encoded)) || (plan.count == 0) != (mainPoint == 0) ||
			(plan.count == 0 && plan.depth != 0) {
			return storageDecodingPlan{}, ErrInvalidStorageNode
		}
		previous := -1
		for index := uint16(0); index < plan.count; index++ {
			childIndex := int(encoded[offset])
			if childIndex <= previous {
				return storageDecodingPlan{}, ErrInvalidStorageNode
			}
			previous = childIndex
			offset += storageInternalEdgeBytes
		}
	case StorageNodeKindStem:
		if !validStorageStemDepth(plan.depth) || mainPoint == 0 {
			return storageDecodingPlan{}, ErrInvalidStorageNode
		}
		stemFixed := storageStemFixedBytes()
		if !storageDecodingRangeFits(len(encoded), offset, stemFixed) {
			return storageDecodingPlan{}, ErrInvalidStorageNode
		}
		offset += storageStemBytes
		for range 2 {
			point, commitmentErr := inspectStorageCommitment(encoded, offset)
			if commitmentErr != nil {
				return storageDecodingPlan{}, commitmentErr
			}
			plan.pointDecodes += point
			offset += storageCommitmentBytes
		}
		plan.count = binary.BigEndian.Uint16(encoded[offset : offset+storageNodeCountBytes])
		offset += storageNodeCountBytes
		if !storageRecordCountFits(plan.count) {
			return storageDecodingPlan{}, ErrInvalidStorageNode
		}
		want := uint64(offset) + uint64(plan.count)*storageStemEntryBytes
		if plan.count == 0 || want != uint64(len(encoded)) {
			return storageDecodingPlan{}, ErrInvalidStorageNode
		}
		previous := -1
		for index := uint16(0); index < plan.count; index++ {
			suffix := int(encoded[offset])
			if suffix <= previous {
				return storageDecodingPlan{}, ErrInvalidStorageNode
			}
			previous = suffix
			offset += storageStemEntryBytes
		}
	default:
		return storageDecodingPlan{}, ErrInvalidStorageNode
	}

	return plan, nil
}

func inspectStorageCommitment(encoded []byte, offset int) (uint32, error) {
	if !storageDecodingRangeFits(len(encoded), offset, storageCommitmentBytes) {
		return 0, ErrInvalidStorageNode
	}
	payload := encoded[offset+storageCommitmentKindBytes : offset+storageCommitmentBytes]
	switch encoded[offset] {
	case 0:
		if !allZero(payload) {
			return 0, ErrInvalidStorageNode
		}
		return 0, nil
	case 1:
		if allZero(payload) {
			return 0, ErrInvalidStorageNode
		}
		return 1, nil
	default:
		return 0, ErrInvalidStorageNode
	}
}

func decodeStorageCommitment(
	ctx context.Context,
	encoded []byte,
	offset int,
) (backend.VectorCommitment, int, error) {
	point, err := inspectStorageCommitment(encoded, offset)
	if err != nil {
		return backend.VectorCommitment{}, 0, err
	}
	if point == 0 {
		return backend.EmptyVectorCommitment(), offset + storageCommitmentBytes, nil
	}
	value, err := backend.DecodeVectorCommitment(
		ctx,
		encoded[offset+storageCommitmentKindBytes:offset+storageCommitmentBytes],
		backend.VectorCommitmentDecodingLimits{
			MaxCommitmentBytes: backend.CommitmentSize,
			MaxPointDecodes:    1,
		},
	)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return backend.VectorCommitment{}, 0, errors.Join(ErrStorageDecodingCancelled, err)
		}
		return backend.VectorCommitment{}, 0, fmt.Errorf("%w: commitment", ErrInvalidStorageNode)
	}

	return value, offset + storageCommitmentBytes, nil
}

func storageDecodedRecordBytes(kind StorageNodeKind) uint64 {
	if kind == StorageNodeKindStem {
		return entryWorkingBytes
	}

	return uint64(storageInternalEdgeBytes)
}

func storageDecodedTemporaryBytes(
	encodedBytes uint64,
	count uint16,
	kind StorageNodeKind,
) uint64 {
	return encodedBytes + storageDecodedNodeWorkingBytes +
		uint64(count)*storageDecodedRecordBytes(kind)
}

func storageMinimumNodeBytes() int {
	return storageNodeHeaderBytes + storageNodeCountBytes
}

func storageStemFixedBytes() int {
	return storageStemBytes + 2*storageCommitmentBytes + storageNodeCountBytes
}

func storageRecordCountFits(count uint16) bool {
	return uint32(count) <= backend.VectorWidth
}

func validStorageInternalDepth(depth uint8) bool {
	return depth <= 30
}

func validStorageStemDepth(depth uint8) bool {
	return depth >= 1 && depth <= 31
}

func storageDecodingRangeFits(encodedLength int, offset int, size int) bool {
	return offset >= 0 && offset <= encodedLength && size <= encodedLength-offset
}

func allZero(value []byte) bool {
	var combined byte
	for _, current := range value {
		combined |= current
	}

	return combined == 0
}

func checkStorageDecodingContext(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidStorageDecodingContext
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrStorageDecodingCancelled, err)
	}

	return nil
}

func checkStorageDecodingResource(
	resource StorageDecodingResource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return &StorageDecodingResourceError{
		Resource: resource,
		Limit:    limit,
		Actual:   actual,
	}
}
