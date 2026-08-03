package verkletree

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	snapshotMagicBytes          = 4
	snapshotProfileIDBytes      = 1
	snapshotProfileVersionBytes = 2
	snapshotEncodingBytes       = 2
	snapshotCountBytes          = 4
	snapshotEntryBytes          = 32 + 32
	snapshotContainerVersion    = uint16(1)

	snapshotHeaderBytes = snapshotMagicBytes +
		snapshotProfileIDBytes +
		snapshotProfileVersionBytes +
		snapshotEncodingBytes +
		int(RootSize) +
		snapshotCountBytes
	// These are the largest 64-byte entry count and resulting encoded length
	// whose 55-byte header fit in a signed 32-bit allocation length.
	maxSnapshotEntries      = uint32(33_554_431)
	maxSnapshotEncodedBytes = uint64(2_147_483_639)
)

var snapshotMagic = [snapshotMagicBytes]byte{'V', 'K', 'S', 'S'}

// SnapshotEncodingLimits bounds canonical whole-snapshot serialization.
type SnapshotEncodingLimits struct {
	MaxSnapshotBytes  uint64
	MaxEntries        uint32
	MaxTemporaryBytes uint64
}

func (limits SnapshotEncodingLimits) validate() error {
	if limits.MaxSnapshotBytes == 0 ||
		limits.MaxSnapshotBytes > maxSnapshotEncodedBytes ||
		limits.MaxEntries == 0 ||
		limits.MaxEntries > maxSnapshotEntries ||
		limits.MaxTemporaryBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

// SnapshotDecodingLimits bounds hostile whole-snapshot decoding and the
// independently rebuilt authenticated state. MaxPointDecodes may be zero for
// an empty snapshot and must not exceed the one encoded root commitment.
type SnapshotDecodingLimits struct {
	MaxSnapshotBytes  uint64
	MaxEntries        uint32
	MaxPointDecodes   uint32
	MaxTemporaryBytes uint64
	Snapshot          SnapshotLimits
}

func (limits SnapshotDecodingLimits) validate() error {
	if limits.MaxSnapshotBytes == 0 ||
		limits.MaxSnapshotBytes > maxSnapshotEncodedBytes ||
		limits.MaxEntries == 0 ||
		limits.MaxEntries > maxSnapshotEntries ||
		limits.MaxPointDecodes > 1 ||
		limits.MaxTemporaryBytes == 0 ||
		limits.Snapshot.validate() != nil {
		return ErrInvalidLimits
	}

	return nil
}

// Bytes returns one caller-owned canonical snapshot encoding. The encoding
// binds the exact profile, root, and ascending present key/value entries.
func (snapshot Snapshot) Bytes(
	ctx context.Context,
	limits SnapshotEncodingLimits,
) ([]byte, error) {
	if !snapshot.valid {
		return nil, ErrInvalidSnapshot
	}
	if err := checkPublicContext(ctx); err != nil {
		return nil, err
	}
	if err := limits.validate(); err != nil {
		return nil, err
	}
	rootBytes, rootErr := snapshotCanonicalRoot(snapshot)
	if rootErr != nil {
		return nil, ErrInvalidSnapshot
	}
	// A canonical root can only be returned after the same immutable internal
	// snapshot has validated successfully, so EntryCount cannot fail here.
	count, _ := snapshot.value.EntryCount()
	if err := checkSnapshotEncodingResource(
		ResourceEntries,
		uint64(limits.MaxEntries),
		uint64(count),
	); err != nil {
		return nil, err
	}
	encodedBytes := uint64(snapshotHeaderBytes) +
		uint64(count)*snapshotEntryBytes
	if err := checkSnapshotEncodingResource(
		ResourceSnapshotBytes,
		limits.MaxSnapshotBytes,
		encodedBytes,
	); err != nil {
		return nil, err
	}
	temporaryBytes := encodedBytes + uint64(count)*snapshotEntryBytes
	if err := checkSnapshotEncodingResource(
		ResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		temporaryBytes,
	); err != nil {
		return nil, err
	}
	entries, err := snapshot.value.CopyEntries(
		ctx,
		limits.MaxEntries,
		limits.MaxTemporaryBytes,
	)
	if err != nil {
		return nil, translateSnapshotEncodingError("copy entries", err)
	}
	profile := ExperimentalBandersnatchIPA256V0()

	encoded := make([]byte, int(encodedBytes))
	copy(encoded, snapshotMagic[:])
	offset := snapshotMagicBytes
	encoded[offset] = byte(profile.ID())
	offset += snapshotProfileIDBytes
	binary.BigEndian.PutUint16(encoded[offset:], profile.Version())
	offset += snapshotProfileVersionBytes
	binary.BigEndian.PutUint16(encoded[offset:], snapshotContainerVersion)
	offset += snapshotEncodingBytes
	copy(encoded[offset:], rootBytes[:])
	offset += int(RootSize)
	binary.BigEndian.PutUint32(encoded[offset:], count)
	offset += snapshotCountBytes
	for index := range entries {
		if err := checkPublicContext(ctx); err != nil {
			return nil, err
		}
		copy(encoded[offset:], entries[index].Key[:])
		offset += len(entries[index].Key)
		copy(encoded[offset:], entries[index].Value[:])
		offset += len(entries[index].Value)
	}

	return encoded, nil
}

// DecodeSnapshot validates one exact canonical snapshot encoding, rebuilds
// its authenticated tree, and requires the derived root to match the encoded
// root before returning an immutable snapshot.
func DecodeSnapshot(
	ctx context.Context,
	encoded []byte,
	limits SnapshotDecodingLimits,
) (Snapshot, error) {
	if err := checkPublicContext(ctx); err != nil {
		return Snapshot{}, err
	}
	if err := limits.validate(); err != nil {
		return Snapshot{}, err
	}
	if err := checkSnapshotEncodingResource(
		ResourceSnapshotBytes,
		limits.MaxSnapshotBytes,
		uint64(len(encoded)),
	); err != nil {
		return Snapshot{}, err
	}
	if len(encoded) < snapshotHeaderBytes ||
		!bytes.Equal(encoded[:snapshotMagicBytes], snapshotMagic[:]) {
		return Snapshot{}, ErrInvalidSnapshot
	}
	profile := ExperimentalBandersnatchIPA256V0()
	if encoded[snapshotMagicBytes] != byte(profile.ID()) ||
		binary.BigEndian.Uint16(encoded[5:7]) != profile.Version() ||
		binary.BigEndian.Uint16(encoded[7:9]) != snapshotContainerVersion {
		return Snapshot{}, ErrUnsupportedProfile
	}
	countOffset := snapshotHeaderBytes - snapshotCountBytes
	count := binary.BigEndian.Uint32(encoded[countOffset:snapshotHeaderBytes])
	if err := checkSnapshotEncodingResource(
		ResourceEntries,
		uint64(limits.MaxEntries),
		uint64(count),
	); err != nil {
		return Snapshot{}, err
	}
	expectedBytes := uint64(snapshotHeaderBytes) +
		uint64(count)*snapshotEntryBytes
	if expectedBytes != uint64(len(encoded)) {
		return Snapshot{}, ErrInvalidSnapshot
	}
	if err := checkSnapshotEncodingResource(
		ResourceTemporaryBytes,
		limits.MaxTemporaryBytes,
		uint64(count)*3*snapshotEntryBytes,
	); err != nil {
		return Snapshot{}, err
	}
	root, err := DecodeRoot(
		ctx,
		encoded[9:countOffset],
		RootDecodingLimits{
			MaxRootBytes:    RootSize,
			MaxPointDecodes: limits.MaxPointDecodes,
		},
	)
	if err != nil {
		return Snapshot{}, translateSnapshotDecodingError("decode root", err)
	}

	entries := make([]Entry, int(count))
	offset := snapshotHeaderBytes
	for index := range entries {
		if err := checkPublicContext(ctx); err != nil {
			return Snapshot{}, err
		}
		copy(entries[index].Key[:], encoded[offset:offset+32])
		offset += 32
		copy(entries[index].Value[:], encoded[offset:offset+32])
		offset += 32
		if index == 0 {
			continue
		}
		if bytes.Compare(
			entries[index-1].Key[:], entries[index].Key[:],
		) != -1 {
			return Snapshot{}, ErrInvalidSnapshot
		}
	}

	snapshot, err := NewSnapshot(ctx, profile, entries, limits.Snapshot)
	if err != nil {
		return Snapshot{}, translateSnapshotDecodingError("rebuild snapshot", err)
	}
	encodedRoot, _ := root.Bytes()
	derivedRoot, derivedErr := snapshotCanonicalRoot(snapshot)
	if derivedErr != nil || encodedRoot != derivedRoot {
		return Snapshot{}, ErrInvalidSnapshot
	}

	return snapshot, nil
}

func snapshotCanonicalRoot(snapshot Snapshot) ([RootSize]byte, error) {
	root, err := snapshot.Root()
	if err != nil {
		return [RootSize]byte{}, err
	}

	return root.Bytes()
}

func checkSnapshotEncodingResource(
	resource Resource,
	limit uint64,
	actual uint64,
) error {
	if actual <= limit {
		return nil
	}

	return newPublicResourceError(resource, limit, actual)
}

func translateSnapshotEncodingError(operation string, err error) error {
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w: %w", operation, ErrCancelled, err)
	}

	return fmt.Errorf("%s: %w", operation, ErrInvalidSnapshot)
}

func translateSnapshotDecodingError(operation string, err error) error {
	var resourceErr *ResourceError
	if errors.As(err, &resourceErr) {
		return resourceErr
	}
	if errors.Is(err, ErrUnsupportedProfile) {
		return fmt.Errorf("%s: %w", operation, ErrUnsupportedProfile)
	}
	if errors.Is(err, ErrCancelled) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w: %w", operation, ErrCancelled, err)
	}

	return fmt.Errorf("%s: %w", operation, ErrInvalidSnapshot)
}
