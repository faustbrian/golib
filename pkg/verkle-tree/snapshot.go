package verkletree

import (
	"context"
	"errors"
	"fmt"

	"github.com/faustbrian/golib/pkg/verkle-tree/internal/authstate"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/backend"
	"github.com/faustbrian/golib/pkg/verkle-tree/internal/committedtree"
)

const maxPublicCount = uint32(2_147_483_647)

// Key is one fixed 32-byte raw key.
type Key [32]byte

// Value is one fixed 32-byte raw value. Its zero value remains present.
type Value [32]byte

// Entry is one present key/value pair.
type Entry struct {
	Key   Key
	Value Value
}

// UpdateKind distinguishes writing a value from deleting a key.
type UpdateKind uint8

const (
	// UpdateSet inserts or replaces a value, including the all-zero value.
	UpdateSet UpdateKind = iota + 1

	// UpdateDelete removes a key and differs from writing an all-zero value.
	UpdateDelete
)

// Update is one immutable caller-owned state change. Its zero value is invalid.
type Update struct {
	kind  UpdateKind
	key   Key
	value Value
}

// Set returns an update that inserts or replaces key with value.
func Set(key Key, value Value) Update {
	return Update{kind: UpdateSet, key: key, value: value}
}

// Delete returns an update that removes key. Deleting an absent key is a
// deterministic no-op.
func Delete(key Key) Update {
	return Update{kind: UpdateDelete, key: key}
}

// StateLimits bounds retained state and atomic batch work.
type StateLimits struct {
	MaxEntries        uint32
	MaxBatchUpdates   uint32
	MaxTemporaryBytes uint64
}

// TreeLimits bounds committed-tree construction.
type TreeLimits struct {
	MaxEntries         uint32
	MaxStems           uint32
	MaxNodes           uint32
	MaxEdges           uint32
	MaxCommitments     uint32
	MaxFieldMappings   uint64
	MaxCommitmentTerms uint64
	MaxTemporaryBytes  uint64
}

// CommitmentLimits bounds fixed-profile generator and commitment work.
type CommitmentLimits struct {
	MaxGeneratorDerivations uint32
	MaxScalarDecodes        uint32
	MaxMSMTerms             uint32
	MaxTemporaryBytes       uint64
}

// SnapshotLimits binds every resource budget required to construct and update
// an immutable snapshot. Every field must be positive.
type SnapshotLimits struct {
	State      StateLimits
	Tree       TreeLimits
	Commitment CommitmentLimits
}

func (limits SnapshotLimits) validate() error {
	if limits.State.MaxEntries == 0 ||
		limits.State.MaxEntries > maxPublicCount ||
		limits.State.MaxBatchUpdates == 0 ||
		limits.State.MaxBatchUpdates > maxPublicCount ||
		limits.State.MaxTemporaryBytes == 0 ||
		limits.Tree.MaxEntries == 0 ||
		limits.Tree.MaxEntries > maxPublicCount ||
		limits.Tree.MaxStems == 0 ||
		limits.Tree.MaxStems > maxPublicCount ||
		limits.Tree.MaxNodes == 0 ||
		limits.Tree.MaxNodes > maxPublicCount ||
		limits.Tree.MaxEdges == 0 ||
		limits.Tree.MaxEdges > maxPublicCount ||
		limits.Tree.MaxCommitments == 0 ||
		limits.Tree.MaxCommitments > maxPublicCount ||
		limits.Tree.MaxFieldMappings == 0 ||
		limits.Tree.MaxCommitmentTerms == 0 ||
		limits.Tree.MaxTemporaryBytes == 0 ||
		limits.Commitment.MaxGeneratorDerivations == 0 ||
		limits.Commitment.MaxScalarDecodes == 0 ||
		limits.Commitment.MaxMSMTerms == 0 ||
		limits.Commitment.MaxTemporaryBytes == 0 {
		return ErrInvalidLimits
	}

	return nil
}

// Snapshot is one immutable ordered state and its exact vector-committed tree.
// Copies are safe for concurrent reads.
type Snapshot struct {
	value authstate.Snapshot
	valid bool
}

// Transition binds one successful atomic update to exact pre- and post-roots.
type Transition struct {
	value authstate.Transition
	valid bool
}

// NewSnapshot validates, defensively owns, orders, and commits entries.
func NewSnapshot(
	ctx context.Context,
	profile Profile,
	entries []Entry,
	limits SnapshotLimits,
) (Snapshot, error) {
	if err := checkPublicContext(ctx); err != nil {
		return Snapshot{}, err
	}
	if err := profile.Validate(); err != nil {
		return Snapshot{}, ErrUnsupportedProfile
	}
	if err := limits.validate(); err != nil {
		return Snapshot{}, err
	}
	owned := make([]authstate.Entry, len(entries))
	for index := range entries {
		if err := checkPublicContext(ctx); err != nil {
			return Snapshot{}, err
		}
		owned[index] = authstate.Entry{
			Key:   authstate.Key(entries[index].Key),
			Value: authstate.Value(entries[index].Value),
		}
	}
	value, err := authstate.NewSnapshot(
		ctx,
		owned,
		authstate.Limits{
			MaxEntries:        limits.State.MaxEntries,
			MaxBatchUpdates:   limits.State.MaxBatchUpdates,
			MaxTemporaryBytes: limits.State.MaxTemporaryBytes,
		},
		committedtree.Limits{
			MaxEntries:         limits.Tree.MaxEntries,
			MaxStems:           limits.Tree.MaxStems,
			MaxNodes:           limits.Tree.MaxNodes,
			MaxEdges:           limits.Tree.MaxEdges,
			MaxCommitments:     limits.Tree.MaxCommitments,
			MaxFieldMappings:   limits.Tree.MaxFieldMappings,
			MaxCommitmentTerms: limits.Tree.MaxCommitmentTerms,
			MaxTemporaryBytes:  limits.Tree.MaxTemporaryBytes,
		},
		backend.CommitmentLimits{
			MaxGeneratorDerivations: limits.Commitment.MaxGeneratorDerivations,
			MaxScalarDecodes:        limits.Commitment.MaxScalarDecodes,
			MaxMSMTerms:             limits.Commitment.MaxMSMTerms,
			MaxTemporaryBytes:       limits.Commitment.MaxTemporaryBytes,
		},
	)
	if err != nil {
		return Snapshot{}, translateSnapshotError("construct snapshot", err)
	}

	return Snapshot{value: value, valid: true}, nil
}

// Get distinguishes an absent key from a present all-zero value.
func (snapshot Snapshot) Get(
	ctx context.Context,
	key Key,
) (Value, bool, error) {
	if !snapshot.valid {
		return Value{}, false, ErrInvalidSnapshot
	}
	if err := checkPublicContext(ctx); err != nil {
		return Value{}, false, err
	}
	value, present, err := snapshot.value.Get(ctx, authstate.Key(key))
	if err != nil {
		return Value{}, false, translateSnapshotError("read snapshot", err)
	}

	return Value(value), present, nil
}

// Root returns the exact immutable profile-bound root.
func (snapshot Snapshot) Root() (Root, error) {
	if !snapshot.valid {
		return Root{}, ErrInvalidSnapshot
	}
	value, err := snapshot.value.RootContainer(context.Background())
	if err != nil {
		return Root{}, translateSnapshotError("read snapshot root", err)
	}

	return Root{value: value}, nil
}

// Apply validates the entire batch, applies it in canonical key order, and
// returns a new immutable snapshot. Failure preserves the receiver.
func (snapshot Snapshot) Apply(
	ctx context.Context,
	updates []Update,
) (Snapshot, Transition, error) {
	if !snapshot.valid {
		return Snapshot{}, Transition{}, ErrInvalidSnapshot
	}
	if err := checkPublicContext(ctx); err != nil {
		return Snapshot{}, Transition{}, err
	}
	owned := make([]authstate.Update, len(updates))
	for index := range updates {
		if err := checkPublicContext(ctx); err != nil {
			return Snapshot{}, Transition{}, err
		}
		switch updates[index].kind {
		case UpdateSet:
			owned[index] = authstate.Set(
				authstate.Key(updates[index].key),
				authstate.Value(updates[index].value),
			)
		case UpdateDelete:
			if updates[index].value != (Value{}) {
				return Snapshot{}, Transition{}, ErrInvalidUpdate
			}
			owned[index] = authstate.Delete(authstate.Key(updates[index].key))
		default:
			return Snapshot{}, Transition{}, ErrInvalidUpdate
		}
	}
	next, transition, err := snapshot.value.Apply(ctx, owned)
	if err != nil {
		return Snapshot{}, Transition{}, translateSnapshotError("apply updates", err)
	}

	return Snapshot{value: next, valid: true},
		Transition{value: transition, valid: true},
		nil
}

// PreRoot returns the exact root before the transition.
func (transition Transition) PreRoot() (Root, error) {
	if !transition.valid {
		return Root{}, ErrInvalidTransition
	}
	value, err := transition.value.PreRootContainer(context.Background())
	if err != nil {
		return Root{}, fmt.Errorf("read transition pre-root: %w", ErrInvalidTransition)
	}

	return Root{value: value}, nil
}

// PostRoot returns the exact root after the transition.
func (transition Transition) PostRoot() (Root, error) {
	if !transition.valid {
		return Root{}, ErrInvalidTransition
	}
	value, err := transition.value.PostRootContainer(context.Background())
	if err != nil {
		return Root{}, fmt.Errorf("read transition post-root: %w", ErrInvalidTransition)
	}

	return Root{value: value}, nil
}

func checkPublicContext(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidContext
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrCancelled, err)
	}

	return nil
}

func translateSnapshotError(operation string, err error) error {
	if resourceErr := translateResourceError(err); resourceErr != nil {
		return resourceErr
	}
	switch {
	case authstate.IsDuplicateKeyError(err):
		return fmt.Errorf("%s: %w", operation, ErrDuplicateKey)
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%s: %w: %w", operation, ErrCancelled, err)
	default:
		return fmt.Errorf("%s: %w", operation, ErrCryptographic)
	}
}

func translateResourceError(err error) error {
	var proofMaterialErr *authstate.ProofMaterialResourceError
	if errors.As(err, &proofMaterialErr) {
		resource := ResourceTemporaryBytes
		switch proofMaterialErr.Resource {
		case authstate.ProofMaterialResourceKeys:
			resource = ResourceKeys
		case authstate.ProofMaterialResourceStemPaths:
			resource = ResourceStemPaths
		case authstate.ProofMaterialResourceNodeReads:
			resource = ResourceNodeReads
		case authstate.ProofMaterialResourcePathCommitments:
			resource = ResourcePathCommitments
		case authstate.ProofMaterialResourcePathBytes:
			resource = ResourcePathBytes
		case authstate.ProofMaterialResourceTemporaryBytes:
		}

		return newPublicResourceError(
			resource,
			proofMaterialErr.Limit,
			proofMaterialErr.Actual,
		)
	}
	var verifierQueryErr *authstate.AggregateVerifierQueryResourceError
	if errors.As(err, &verifierQueryErr) {
		resource := ResourceTemporaryBytes
		switch verifierQueryErr.Resource {
		case authstate.AggregateVerifierQueryResourceQueries:
			resource = ResourceQueries
		case authstate.AggregateVerifierQueryResourceTemporaryBytes:
		}

		return newPublicResourceError(
			resource,
			verifierQueryErr.Limit,
			verifierQueryErr.Actual,
		)
	}
	var treeProofErr *authstate.TreeProofResourceError
	if errors.As(err, &treeProofErr) {
		resource := ResourceTemporaryBytes
		switch treeProofErr.Resource {
		case authstate.TreeProofResourceClaims:
			resource = ResourceClaims
		case authstate.TreeProofResourceStemPaths:
			resource = ResourceStemPaths
		case authstate.TreeProofResourcePathCommitments:
			resource = ResourcePathCommitments
		case authstate.TreeProofResourcePathDerivations:
			resource = ResourcePathDerivations
		case authstate.TreeProofResourcePathBytes:
			resource = ResourcePathBytes
		case authstate.TreeProofResourceTemporaryBytes:
		}

		return newPublicResourceError(
			resource,
			treeProofErr.Limit,
			treeProofErr.Actual,
		)
	}
	var proofEncodingErr *authstate.TreeProofEncodingResourceError
	if errors.As(err, &proofEncodingErr) {
		resource := ResourceTemporaryBytes
		if proofEncodingErr.Resource ==
			authstate.TreeProofEncodingResourceBytes {
			resource = ResourceProofBytes
		}

		return newPublicResourceError(
			resource,
			proofEncodingErr.Limit,
			proofEncodingErr.Actual,
		)
	}
	var proofDecodingErr *authstate.TreeProofDecodingResourceError
	if errors.As(err, &proofDecodingErr) {
		resource := ResourceTemporaryBytes
		switch proofDecodingErr.Resource {
		case authstate.TreeProofDecodingResourceBytes:
			resource = ResourceProofBytes
		case authstate.TreeProofDecodingResourceClaims:
			resource = ResourceClaims
		case authstate.TreeProofDecodingResourceStemPaths:
			resource = ResourceStemPaths
		case authstate.TreeProofDecodingResourcePathCommitments:
			resource = ResourcePathCommitments
		case authstate.TreeProofDecodingResourcePathDerivations:
			resource = ResourcePathDerivations
		case authstate.TreeProofDecodingResourcePathBytes:
			resource = ResourcePathBytes
		case authstate.TreeProofDecodingResourcePointDecodes:
			resource = ResourcePointDecodes
		case authstate.TreeProofDecodingResourceScalarDecodes:
			resource = ResourceScalarDecodes
		case authstate.TreeProofDecodingResourceTemporaryBytes:
		}

		return newPublicResourceError(
			resource,
			proofDecodingErr.Limit,
			proofDecodingErr.Actual,
		)
	}
	var proverQueryErr *committedtree.AggregateProverQueryResourceError
	if errors.As(err, &proverQueryErr) {
		resource := ResourceTemporaryBytes
		switch proverQueryErr.Resource {
		case committedtree.AggregateProverQueryResourceKeys:
			resource = ResourceKeys
		case committedtree.AggregateProverQueryResourceQueries:
			resource = ResourceQueries
		case committedtree.AggregateProverQueryResourceNodeReads:
			resource = ResourceNodeReads
		case committedtree.AggregateProverQueryResourceTemporaryBytes:
		}

		return newPublicResourceError(
			resource,
			proverQueryErr.Limit,
			proverQueryErr.Actual,
		)
	}
	var aggregateOpeningErr *backend.AggregateOpeningResourceError
	if errors.As(err, &aggregateOpeningErr) {
		resource := ResourceTemporaryBytes
		switch aggregateOpeningErr.Resource {
		case backend.AggregateOpeningResourceGeneratorDerivations:
			resource = ResourceGeneratorDerivations
		case backend.AggregateOpeningResourcePrecomputedPoints:
			resource = ResourcePrecomputedPoints
		case backend.AggregateOpeningResourceQueries:
			resource = ResourceQueries
		case backend.AggregateOpeningResourceScalarDecodes:
			resource = ResourceScalarDecodes
		case backend.AggregateOpeningResourceMSMTerms:
			resource = ResourceMSMTerms
		case backend.AggregateOpeningResourceTemporaryBytes:
		case backend.AggregateOpeningResourceWorkers:
			resource = ResourceWorkers
		}

		return newPublicResourceError(
			resource,
			aggregateOpeningErr.Limit,
			aggregateOpeningErr.Actual,
		)
	}
	var openingProofErr *backend.OpeningProofResourceError
	if errors.As(err, &openingProofErr) {
		resource := ResourceProofBytes
		switch openingProofErr.Resource {
		case backend.OpeningProofResourceBytes:
		case backend.OpeningProofResourcePointDecodes:
			resource = ResourcePointDecodes
		case backend.OpeningProofResourceScalarDecodes:
			resource = ResourceScalarDecodes
		}

		return newPublicResourceError(
			resource,
			openingProofErr.Limit,
			openingProofErr.Actual,
		)
	}
	var stateErr *authstate.ResourceError
	if errors.As(err, &stateErr) {
		resource := ResourceTemporaryBytes
		switch stateErr.Resource {
		case authstate.ResourceEntries:
			resource = ResourceEntries
		case authstate.ResourceBatchUpdates:
			resource = ResourceBatchUpdates
		case authstate.ResourceTemporaryBytes:
		}

		return newPublicResourceError(resource, stateErr.Limit, stateErr.Actual)
	}
	var treeErr *committedtree.ResourceError
	if errors.As(err, &treeErr) {
		resource := ResourceTemporaryBytes
		switch treeErr.Resource {
		case committedtree.ResourceEntries:
			resource = ResourceEntries
		case committedtree.ResourceStems:
			resource = ResourceStems
		case committedtree.ResourceNodes:
			resource = ResourceNodes
		case committedtree.ResourceEdges:
			resource = ResourceEdges
		case committedtree.ResourceCommitments:
			resource = ResourceCommitments
		case committedtree.ResourceFieldMappings:
			resource = ResourceFieldMappings
		case committedtree.ResourceCommitmentTerms:
			resource = ResourceCommitmentTerms
		case committedtree.ResourceTemporaryBytes:
		}

		return newPublicResourceError(resource, treeErr.Limit, treeErr.Actual)
	}
	var commitmentErr *backend.CommitmentResourceError
	if errors.As(err, &commitmentErr) {
		resource := ResourceTemporaryBytes
		switch commitmentErr.Resource {
		case backend.CommitmentResourceGeneratorDerivations:
			resource = ResourceGeneratorDerivations
		case backend.CommitmentResourceScalarDecodes:
			resource = ResourceScalarDecodes
		case backend.CommitmentResourceMSMTerms:
			resource = ResourceMSMTerms
		case backend.CommitmentResourceTemporaryBytes:
		}

		return newPublicResourceError(
			resource,
			commitmentErr.Limit,
			commitmentErr.Actual,
		)
	}

	return nil
}

func newPublicResourceError(
	resource Resource,
	limit uint64,
	actual uint64,
) *ResourceError {
	return &ResourceError{Resource: resource, Limit: limit, Actual: actual}
}
