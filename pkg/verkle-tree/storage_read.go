package verkletree

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"reflect"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
)

const (
	storageReadSeenWorkingBytes    = uint64(128)
	storageReadPendingWorkingBytes = uint64(80)
	storageReadEntryWorkingBytes   = uint64(64)
	storageReadRecordCopyBytes     = uint64(64)
)

// StorageReadLimits bounds hostile store access, canonical node decoding, and
// reconstruction. Snapshot separately bounds deterministic cryptographic tree
// reconstruction. MaxPointDecodes may be zero to permit only identity-only
// nodes; every other field must be positive.
type StorageReadLimits struct {
	// MaxEntries bounds present key/value entries reconstructed from storage.
	MaxEntries uint32
	// MaxNodes bounds distinct reachable logical nodes.
	MaxNodes uint32
	// MaxEdges bounds reachable internal-node edges.
	MaxEdges uint32
	// MaxNodeReads bounds adapter node-read calls.
	MaxNodeReads uint32
	// MaxNodeBytes bounds each adapter-returned canonical node.
	MaxNodeBytes uint64
	// MaxEncodedBytes covers loaded bytes plus canonical re-encoding used to
	// verify the root-node address.
	MaxEncodedBytes uint64
	// MaxHashes covers loaded content-address checks plus canonical
	// re-encoding hashes.
	MaxHashes uint64
	// MaxPointDecodes bounds strict commitment decoding across loaded nodes.
	MaxPointDecodes uint64
	// MaxTemporaryBytes bounds conservatively accounted load scratch.
	MaxTemporaryBytes uint64
	// Snapshot independently bounds authenticated tree reconstruction.
	Snapshot SnapshotLimits
}

func (limits StorageReadLimits) validate() error {
	if limits.MaxEntries == 0 ||
		limits.MaxEntries > maxPublicCount ||
		limits.MaxNodes == 0 ||
		limits.MaxNodes > maxPublicCount ||
		limits.MaxEdges == 0 ||
		limits.MaxEdges > maxPublicCount ||
		limits.MaxNodeReads == 0 ||
		limits.MaxNodeReads > maxPublicCount ||
		limits.MaxNodeBytes == 0 ||
		limits.MaxEncodedBytes == 0 ||
		limits.MaxHashes == 0 ||
		limits.MaxTemporaryBytes == 0 ||
		limits.Snapshot.validate() != nil {
		return ErrInvalidLimits
	}

	return nil
}

type pendingStorageNode struct {
	id     NodeID
	prefix [31]byte
	depth  uint8
}

type storageReadAccounting struct {
	nodes        uint64
	edges        uint64
	entries      uint64
	reads        uint64
	encodedBytes uint64
	hashes       uint64
	pointDecodes uint64
}

type storageNodeSource interface {
	ReadNode(ctx context.Context, id NodeID, maxBytes uint64) ([]byte, error)
}

// LoadSnapshot opens one caller-owned isolated read view, verifies every
// reachable content address and canonical node, reconstructs the immutable
// state, and independently recomputes both the mathematical root and canonical
// root-node address. It closes the view exactly once and returns no snapshot on
// any read, close, resource, topology, or cryptographic mismatch.
func LoadSnapshot(
	ctx context.Context,
	profile Profile,
	reader NodeReader,
	limits StorageReadLimits,
) (result Snapshot, resultErr error) {
	if err := checkPublicContext(ctx); err != nil {
		return Snapshot{}, err
	}
	if err := profile.Validate(); err != nil {
		return Snapshot{}, ErrUnsupportedProfile
	}
	if !validStorageValue(reader) {
		return Snapshot{}, ErrInvalidStore
	}
	if err := limits.validate(); err != nil {
		return Snapshot{}, err
	}
	available := reader.Capabilities()
	if !available.Supports(RequiredReadStoreCapabilities) {
		return Snapshot{}, &StoreCapabilityError{
			Required:  RequiredReadStoreCapabilities,
			Available: available,
			Missing:   RequiredReadStoreCapabilities &^ available,
		}
	}
	view, err := reader.OpenSnapshot(ctx)
	if err != nil {
		return Snapshot{}, wrapStorageReadError("open snapshot", err)
	}
	if !validStorageValue(view) {
		return Snapshot{}, fmt.Errorf("open snapshot: %w", ErrStorageRead)
	}
	defer func() {
		if closeErr := view.Close(ctx); closeErr != nil {
			result = Snapshot{}
			wrapped := wrapStorageReadError("close snapshot", closeErr)
			if resultErr == nil {
				resultErr = wrapped
			} else {
				resultErr = errors.Join(resultErr, wrapped)
			}
		}
	}()

	publication, err := view.Publication(ctx)
	if err != nil {
		return Snapshot{}, wrapStorageReadError("read publication", err)
	}
	root, rootNode, err := publication.values()
	if err != nil {
		return Snapshot{}, fmt.Errorf("read publication: %w", ErrStorageRead)
	}

	result, err = loadStoredSnapshot(ctx, view, profile, root, rootNode, limits)
	if err != nil {
		return Snapshot{}, err
	}

	return result, nil
}

func loadStoredSnapshot(
	ctx context.Context,
	view storageNodeSource,
	profile Profile,
	publishedRoot Root,
	rootNode NodeID,
	limits StorageReadLimits,
) (Snapshot, error) {
	return loadStoredSnapshotObserved(
		ctx, view, profile, publishedRoot, rootNode, limits, nil,
	)
}

func loadStoredSnapshotObserved(
	ctx context.Context,
	view storageNodeSource,
	profile Profile,
	publishedRoot Root,
	rootNode NodeID,
	limits StorageReadLimits,
	observe func(NodeID) error,
) (Snapshot, error) {
	accounting := storageReadAccounting{nodes: 1}
	if err := checkStorageReadTemporary(limits, accounting.nodes, 0, 0); err != nil {
		return Snapshot{}, err
	}
	pending := []pendingStorageNode{{id: rootNode}}
	seen := map[NodeID]struct{}{rootNode: {}}
	if observe != nil {
		if err := observe(rootNode); err != nil {
			return Snapshot{}, err
		}
	}
	entries := make([]Entry, 0)

	for cursor := 0; cursor < len(pending); cursor++ {
		if err := checkPublicContext(ctx); err != nil {
			return Snapshot{}, err
		}
		current := pending[cursor]
		if err := addStorageReadResource(
			ResourceNodeReads,
			uint64(limits.MaxNodeReads),
			&accounting.reads,
			1,
		); err != nil {
			return Snapshot{}, err
		}
		retained := storageReadRetainedBytes(accounting.nodes, accounting.entries)
		if err := checkStorageReadTemporary(
			limits,
			accounting.nodes,
			accounting.entries,
			1,
		); err != nil {
			return Snapshot{}, err
		}
		if accounting.encodedBytes >= limits.MaxEncodedBytes {
			return Snapshot{}, &ResourceError{
				Resource: ResourceEncodedNodeBytes,
				Limit:    limits.MaxEncodedBytes,
				Actual:   storageReadNextActual(accounting.encodedBytes),
			}
		}
		if err := addStorageReadResource(
			ResourceNodeHashes,
			limits.MaxHashes,
			&accounting.hashes,
			1,
		); err != nil {
			return Snapshot{}, err
		}
		maxReadBytes := min(
			limits.MaxNodeBytes,
			storageReadRemaining(limits.MaxEncodedBytes, accounting.encodedBytes),
			storageReadRemaining(limits.MaxTemporaryBytes, retained),
		)
		encoded, err := view.ReadNode(ctx, current.id, maxReadBytes)
		if err != nil {
			return Snapshot{}, wrapStorageReadError("read node", err)
		}
		if err := checkStorageReadResource(
			ResourceNodeBytes,
			limits.MaxNodeBytes,
			uint64(len(encoded)),
		); err != nil {
			return Snapshot{}, err
		}
		if err := addStorageReadResource(
			ResourceEncodedNodeBytes,
			limits.MaxEncodedBytes,
			&accounting.encodedBytes,
			uint64(len(encoded)),
		); err != nil {
			return Snapshot{}, err
		}
		if NodeID(sha256.Sum256(encoded)) != current.id {
			return Snapshot{}, fmt.Errorf("read node: %w", ErrStorageNodeCorrupt)
		}

		rawBytes := uint64(len(encoded))
		if err := checkStorageReadTemporary(
			limits,
			accounting.nodes,
			accounting.entries,
			storageReadSum(rawBytes, 1),
		); err != nil {
			return Snapshot{}, err
		}
		remainingPoints := storageReadRemaining(
			limits.MaxPointDecodes,
			min(limits.MaxPointDecodes, accounting.pointDecodes),
		)
		decoded, err := committedtree.DecodeStorageNode(
			ctx,
			encoded,
			committedtree.StorageDecodingLimits{
				MaxNodeBytes: limits.MaxNodeBytes,
				MaxPointDecodes: uint32(min(
					remainingPoints,
					uint64(math.MaxUint32),
				)),
				MaxTemporaryBytes: storageReadRemaining(
					storageReadRemaining(limits.MaxTemporaryBytes, retained),
					rawBytes,
				),
			},
		)
		if err != nil {
			return Snapshot{}, translateStorageDecodingError(err)
		}
		accounting.pointDecodes += uint64(decoded.PointDecodes())
		if decoded.Depth() != current.depth ||
			(cursor == 0 && decoded.Kind() != committedtree.StorageNodeKindInternal) {
			return Snapshot{}, fmt.Errorf("read node: %w", ErrStorageNodeCorrupt)
		}

		recordBytes := storageReadRecordBytes(decoded.RecordCount())
		peak := storageReadSum(
			storageReadRetainedBytes(accounting.nodes, accounting.entries),
			rawBytes,
			decoded.TemporaryBytes(),
			recordBytes,
		)
		if err := checkStorageReadResource(
			ResourceTemporaryBytes,
			limits.MaxTemporaryBytes,
			peak,
		); err != nil {
			return Snapshot{}, err
		}

		switch decoded.Kind() {
		case committedtree.StorageNodeKindInternal:
			if err := addStorageReadResource(
				ResourceEdges,
				uint64(limits.MaxEdges),
				&accounting.edges,
				uint64(decoded.RecordCount()),
			); err != nil {
				return Snapshot{}, err
			}
			children, childErr := decoded.Children(ctx)
			if childErr != nil {
				return Snapshot{}, translateStorageDecodingError(childErr)
			}
			for _, child := range children {
				if err := checkPublicContext(ctx); err != nil {
					return Snapshot{}, err
				}
				id := NodeID(child.ID)
				if _, duplicate := seen[id]; duplicate {
					return Snapshot{}, fmt.Errorf("read node: %w", ErrStorageNodeCorrupt)
				}
				if err := addStorageReadResource(
					ResourceNodes,
					uint64(limits.MaxNodes),
					&accounting.nodes,
					1,
				); err != nil {
					return Snapshot{}, err
				}
				if err := checkStorageReadTemporary(
					limits,
					accounting.nodes,
					accounting.entries,
					storageReadSum(rawBytes, decoded.TemporaryBytes(), recordBytes),
				); err != nil {
					return Snapshot{}, err
				}
				prefix := current.prefix
				prefix[current.depth] = child.Index
				seen[id] = struct{}{}
				if observe != nil {
					if err := observe(id); err != nil {
						return Snapshot{}, err
					}
				}
				pending = append(pending, pendingStorageNode{
					id:     id,
					prefix: prefix,
					depth:  current.depth + 1,
				})
			}
		case committedtree.StorageNodeKindStem:
			if err := addStorageReadResource(
				ResourceEntries,
				uint64(limits.MaxEntries),
				&accounting.entries,
				uint64(decoded.RecordCount()),
			); err != nil {
				return Snapshot{}, err
			}
			if err := checkStorageReadTemporary(
				limits,
				accounting.nodes,
				accounting.entries,
				storageReadSum(rawBytes, decoded.TemporaryBytes(), recordBytes),
			); err != nil {
				return Snapshot{}, err
			}
			storedEntries, entryErr := decoded.Entries(ctx)
			if entryErr != nil {
				return Snapshot{}, translateStorageDecodingError(entryErr)
			}
			for _, entry := range storedEntries {
				if !bytes.Equal(entry.Key[:current.depth], current.prefix[:current.depth]) {
					return Snapshot{}, fmt.Errorf("read node: %w", ErrStorageNodeCorrupt)
				}
				entries = append(entries, Entry{
					Key:   Key(entry.Key),
					Value: Value(entry.Value),
				})
			}
		}
	}
	snapshot, err := NewSnapshot(ctx, profile, entries, limits.Snapshot)
	if err != nil {
		return Snapshot{}, translateStorageReconstructionError(err)
	}
	// NewSnapshot returning nil error establishes root availability.
	computedRoot, _ := snapshot.Root()
	wantRoot, _ := publishedRoot.Bytes()
	gotRoot, _ := computedRoot.Bytes()
	if gotRoot != wantRoot {
		return Snapshot{}, fmt.Errorf("reconstruct snapshot: %w", ErrStorageNodeCorrupt)
	}

	if err := checkStorageReadResource(
		ResourceEncodedNodeBytes,
		limits.MaxEncodedBytes,
		2*accounting.encodedBytes,
	); err != nil {
		return Snapshot{}, err
	}
	if err := checkStorageReadResource(
		ResourceNodeHashes,
		limits.MaxHashes,
		accounting.hashes+accounting.nodes,
	); err != nil {
		return Snapshot{}, err
	}
	remainingEncoded := storageReadRemaining(limits.MaxEncodedBytes, accounting.encodedBytes)
	remainingHashes := storageReadRemaining(limits.MaxHashes, accounting.hashes)
	image, err := snapshot.value.StorageImage(
		ctx,
		committedtree.StorageEncodingLimits{
			MaxNodes:        limits.MaxNodes,
			MaxNodeBytes:    limits.MaxNodeBytes,
			MaxEncodedBytes: remainingEncoded,
			MaxHashes: uint32(min(
				remainingHashes,
				uint64(math.MaxUint32),
			)),
			MaxTemporaryBytes: limits.MaxTemporaryBytes,
		},
	)
	if err != nil {
		return Snapshot{}, translateStorageReadEncodingError(err)
	}
	// StorageImage returning nil error establishes root-node availability.
	computedRootNode, _ := image.RootID()
	if NodeID(computedRootNode) != rootNode {
		return Snapshot{}, fmt.Errorf("reconstruct snapshot: %w", ErrStorageNodeCorrupt)
	}

	return snapshot, nil
}

func checkStorageReadTemporary(
	limits StorageReadLimits,
	nodes uint64,
	entries uint64,
	extra uint64,
) error {
	retained := storageReadRetainedBytes(nodes, entries)

	return checkStorageReadResource(
		ResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		retained+extra,
	)
}

func storageReadRetainedBytes(nodes uint64, entries uint64) uint64 {
	return nodes*(storageReadSeenWorkingBytes+2*storageReadPendingWorkingBytes) +
		2*entries*storageReadEntryWorkingBytes
}

func storageReadNextActual(current uint64) uint64 {
	if current == math.MaxUint64 {
		return current
	}

	return current + 1
}

func storageReadRemaining(limit uint64, used uint64) uint64 {
	return limit - used
}

func storageReadSum(values ...uint64) uint64 {
	total := uint64(0)
	for _, value := range values {
		total += value
	}

	return total
}

func storageReadRecordBytes(count uint16) uint64 {
	return uint64(count) * storageReadRecordCopyBytes
}

func addStorageReadResource(
	resource Resource,
	limit uint64,
	current *uint64,
	delta uint64,
) error {
	if *current > math.MaxUint64-delta {
		return &ResourceError{
			Resource: resource,
			Limit:    limit,
			Actual:   math.MaxUint64,
		}
	}
	next := *current + delta
	if err := checkStorageReadResource(resource, limit, next); err != nil {
		return err
	}
	*current = next

	return nil
}

func checkStorageReadResource(resource Resource, limit uint64, actual uint64) error {
	if actual <= limit {
		return nil
	}

	return &ResourceError{Resource: resource, Limit: limit, Actual: actual}
}

func translateStorageDecodingError(err error) error {
	if errors.Is(err, committedtree.ErrStorageNodeProfile) {
		return ErrUnsupportedProfile
	}
	var resourceErr *committedtree.StorageDecodingResourceError
	if errors.As(err, &resourceErr) {
		resource := ResourceTemporaryBytes
		switch resourceErr.Resource {
		case committedtree.StorageDecodingResourceNodeBytes:
			resource = ResourceNodeBytes
		case committedtree.StorageDecodingResourcePointDecodes:
			resource = ResourcePointDecodes
		case committedtree.StorageDecodingResourceTemporaryBytes:
		}

		return &ResourceError{
			Resource: resource,
			Limit:    resourceErr.Limit,
			Actual:   resourceErr.Actual,
		}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("decode stored node: %w: %w", ErrCancelled, err)
	}

	return fmt.Errorf("decode stored node: %w", ErrStorageNodeCorrupt)
}

func translateStorageReadEncodingError(err error) error {
	translated := translateStorageEncodingError(err)
	if errors.Is(translated, ErrResourceExhausted) || errors.Is(translated, ErrCancelled) {
		return translated
	}

	return fmt.Errorf("reconstruct snapshot: %w", ErrStorageNodeCorrupt)
}

func translateStorageReconstructionError(err error) error {
	if errors.Is(err, ErrResourceExhausted) || errors.Is(err, ErrCancelled) {
		return err
	}

	return fmt.Errorf("reconstruct snapshot: %w", ErrStorageNodeCorrupt)
}

func wrapStorageReadError(operation string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w: %w: %w", operation, ErrStorageRead, ErrCancelled, err)
	}

	return fmt.Errorf("%s: %w: %w", operation, ErrStorageRead, err)
}

func validStorageValue(value any) bool {
	if value == nil {
		return false
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return !reflected.IsNil()
	default:
		return true
	}
}
