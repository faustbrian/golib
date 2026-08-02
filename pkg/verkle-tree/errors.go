package verkletree

import (
	"errors"
	"fmt"

	internalprofile "github.com/faustbrian/golib/pkg/verkle-tree/internal/profile"
)

var (
	// ErrUnsupportedProfile identifies an unknown, zero, or internally
	// inconsistent Verkle profile.
	ErrUnsupportedProfile = internalprofile.ErrUnsupported

	// ErrInvalidContext identifies a nil context.
	ErrInvalidContext = errors.New("invalid Verkle operation context")

	// ErrCancelled identifies an operation stopped by context cancellation.
	ErrCancelled = errors.New("verkle operation cancelled")

	// ErrInvalidLimits identifies a zero, overflowing, or unsupported resource
	// declaration.
	ErrInvalidLimits = errors.New("invalid Verkle resource limits")

	// ErrInvalidSnapshot identifies an unusable immutable snapshot.
	ErrInvalidSnapshot = errors.New("invalid Verkle snapshot")

	// ErrInvalidUpdate identifies a zero or internally inconsistent update.
	ErrInvalidUpdate = errors.New("invalid Verkle update")

	// ErrDuplicateKey identifies duplicate keys in initial state or one batch.
	ErrDuplicateKey = errors.New("duplicate Verkle key")

	// ErrInvalidTransition identifies an unusable transition result.
	ErrInvalidTransition = errors.New("invalid Verkle transition")

	// ErrInvalidRoot identifies malformed or unusable root state.
	ErrInvalidRoot = errors.New("invalid Verkle root")

	// ErrInvalidProofEngine identifies an unusable proof engine.
	ErrInvalidProofEngine = errors.New("invalid Verkle proof engine")

	// ErrInvalidProof identifies a malformed, incomplete, or unusable proof.
	ErrInvalidProof = errors.New("invalid Verkle proof")

	// ErrInvalidStore identifies a nil or unusable caller-owned storage
	// boundary.
	ErrInvalidStore = errors.New("invalid Verkle node store")

	// ErrStoreCapability identifies a store missing a required read or
	// publication guarantee.
	ErrStoreCapability = errors.New("missing Verkle store capability")

	// ErrStorageCommit identifies an atomic node/root publication failure.
	ErrStorageCommit = errors.New("verkle storage commit failed")

	// ErrStorageRead identifies a caller-owned store read or read-snapshot
	// lifecycle failure.
	ErrStorageRead = errors.New("verkle storage read failed")

	// ErrStorageAudit identifies a caller-owned audit adapter or audit-view
	// lifecycle failure.
	ErrStorageAudit = errors.New("verkle storage audit failed")

	// ErrStorageMaintenance identifies an unusable opaque maintenance request
	// or a caller-owned maintenance adapter failure.
	ErrStorageMaintenance = errors.New("verkle storage maintenance failed")

	// ErrInvalidRetention identifies a requested retained-publication set that
	// is malformed, duplicated, current, or absent from the audited store view.
	ErrInvalidRetention = errors.New("invalid Verkle retained publication set")

	// ErrStorageInventory identifies an incomplete, duplicated, reordered, or
	// otherwise inconsistent immutable node inventory.
	ErrStorageInventory = errors.New("invalid Verkle storage inventory")

	// ErrStorageSnapshotMissing identifies a store with no published snapshot.
	ErrStorageSnapshotMissing = errors.New("verkle storage snapshot missing")

	// ErrStorageNodeMissing identifies a referenced content-addressed node that
	// the store could not return.
	ErrStorageNodeMissing = errors.New("verkle storage node missing")

	// ErrStorageNodeCorrupt identifies a node, topology, root, or content
	// address that is inconsistent with the canonical persisted snapshot.
	ErrStorageNodeCorrupt = errors.New("verkle storage node corrupt")

	// ErrStaleRoot identifies a compare-and-swap publication conflict.
	ErrStaleRoot = errors.New("stale Verkle root")

	// ErrVerification identifies a well-formed proof that did not authenticate
	// its complete bound claim set.
	ErrVerification = errors.New("verkle proof verification failed")

	// ErrResourceExhausted identifies a declared resource budget rejection.
	ErrResourceExhausted = errors.New("verkle resource limit exceeded")

	// ErrCryptographic identifies a commitment or proof backend failure that is
	// not an ordinary resource, input, or cancellation error.
	ErrCryptographic = errors.New("verkle cryptographic operation failed")
)

// Resource identifies one caller-visible bounded resource.
type Resource uint8

const (
	// ResourceEntries counts retained key/value entries.
	ResourceEntries Resource = iota + 1

	// ResourceBatchUpdates counts updates in one atomic operation.
	ResourceBatchUpdates

	// ResourceKeys counts requested proof keys.
	ResourceKeys

	// ResourceStems counts distinct 31-byte stems.
	ResourceStems

	// ResourceStemPaths counts retained terminal stem paths.
	ResourceStemPaths

	// ResourceNodes counts retained logical nodes.
	ResourceNodes

	// ResourceEdges counts retained internal-node edges.
	ResourceEdges

	// ResourceCommitments counts vector commitments.
	ResourceCommitments

	// ResourcePathCommitments counts retained proof-path commitments.
	ResourcePathCommitments

	// ResourcePathDerivations counts bounded topology derivations.
	ResourcePathDerivations

	// ResourcePathBytes counts retained canonical path bytes.
	ResourcePathBytes

	// ResourceQueries counts aggregate opening queries.
	ResourceQueries

	// ResourceNodeReads counts immutable committed-node reads.
	ResourceNodeReads

	// ResourceNodeBytes counts one canonical persisted node.
	ResourceNodeBytes

	// ResourceEncodedNodeBytes counts aggregate canonical persisted-node bytes
	// encoded or decoded by one operation.
	ResourceEncodedNodeBytes

	// ResourceNodeHashes counts content-address calculations.
	ResourceNodeHashes

	// ResourceClaims counts retained membership and absence claims.
	ResourceClaims

	// ResourceFieldMappings counts commitment-to-field operations.
	ResourceFieldMappings

	// ResourceCommitmentTerms counts bounded commitment terms.
	ResourceCommitmentTerms

	// ResourceGeneratorDerivations counts fixed-profile generator derivations.
	ResourceGeneratorDerivations

	// ResourcePrecomputedPoints counts fixed-profile precomputed points.
	ResourcePrecomputedPoints

	// ResourceScalarDecodes counts canonical scalar decodings.
	ResourceScalarDecodes

	// ResourceMSMTerms counts multi-scalar-multiplication terms.
	ResourceMSMTerms

	// ResourceTemporaryBytes counts conservatively owned scratch memory.
	ResourceTemporaryBytes

	// ResourceRootBytes counts untrusted root-container bytes.
	ResourceRootBytes

	// ResourcePointDecodes counts strict group-point decodings.
	ResourcePointDecodes

	// ResourceProofBytes counts canonical proof bytes.
	ResourceProofBytes

	// ResourceWorkers counts dependency-owned proof workers.
	ResourceWorkers

	// ResourcePublications counts current and retained roots audited together.
	ResourcePublications

	// ResourceInventoryPages counts bounded node-inventory calls.
	ResourceInventoryPages

	// ResourceInventoryNodes counts content addresses returned by inventory.
	ResourceInventoryNodes

	// ResourceUnreachableNodes counts stored nodes outside every audited root.
	ResourceUnreachableNodes
)

// ResourceError reports an exact rejected budget without disclosing keys,
// values, commitments, roots, or proofs.
type ResourceError struct {
	Resource Resource
	Limit    uint64
	Actual   uint64
}

// Error implements error.
func (err *ResourceError) Error() string {
	return fmt.Sprintf(
		"%v: resource %d has value %d, limit %d",
		ErrResourceExhausted,
		err.Resource,
		err.Actual,
		err.Limit,
	)
}

// Unwrap makes ResourceError match ErrResourceExhausted.
func (err *ResourceError) Unwrap() error {
	return ErrResourceExhausted
}
