// Package snapshot composes aggregate repositories with optional derived
// aggregate snapshots.
//
// The package starts no goroutines and owns no transactions. Snapshot refresh
// is always explicit.
package snapshot

import (
	"context"
	"errors"
	"fmt"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

var (
	// ErrStateCodecPanic reports a contained snapshot state codec panic.
	ErrStateCodecPanic = errors.New("snapshot state codec panicked")
	// ErrMetadataProviderPanic reports a contained snapshot metadata provider
	// panic.
	ErrMetadataProviderPanic = errors.New("snapshot metadata provider panicked")
)

// AggregateRepository loads aggregate history and restores decoded snapshot
// state before applying later events.
type AggregateRepository[ID, Aggregate any] interface {
	Load(context.Context, ID) (Aggregate, error)
	Restore(
		context.Context,
		ID,
		uint64,
		eventsourcing.AggregateRestorer[Aggregate],
	) (Aggregate, error)
}

// StateCodec encodes and decodes one explicit aggregate snapshot schema.
//
// Encode must return newly owned bytes. Decode must not retain or mutate the
// supplied state or metadata.
type StateCodec[Aggregate any] interface {
	SchemaVersion() eventsourcing.SchemaVersion
	Encode(Aggregate) ([]byte, error)
	Decode([]byte, map[string]string) (Aggregate, error)
}

// FallbackKind identifies a snapshot failure for which full event-history
// restoration may be selected explicitly.
type FallbackKind uint8

const (
	// FallbackMissing permits full-history restoration when no snapshot exists.
	FallbackMissing FallbackKind = iota + 1
	// FallbackCorrupt permits full-history restoration for classified corrupt
	// snapshot state.
	FallbackCorrupt
	// FallbackIncompatible permits full-history restoration for unsupported
	// snapshot schemas.
	FallbackIncompatible
)

// FallbackPolicy is an immutable explicit snapshot failure policy.
//
// Its zero value is invalid so omitted policy cannot silently select behavior.
type FallbackPolicy struct {
	mask       uint8
	configured bool
}

// ThresholdPolicy selects a snapshot refresh after a fixed number of newly
// committed aggregate versions. Its zero value is invalid.
type ThresholdPolicy struct {
	interval uint64
}

// NewThresholdPolicy returns an immutable explicit refresh threshold.
func NewThresholdPolicy(interval uint64) (ThresholdPolicy, error) {
	if interval == 0 {
		return ThresholdPolicy{}, invalid(
			"snapshot refresh threshold must be greater than zero",
		)
	}

	return ThresholdPolicy{interval: interval}, nil
}

// NewFallbackPolicy permits full-history restoration for the selected,
// classified snapshot failures.
func NewFallbackPolicy(kinds ...FallbackKind) (FallbackPolicy, error) {
	if len(kinds) == 0 {
		return FallbackPolicy{}, invalid("fallback policy must select a kind")
	}

	var mask uint8
	for _, kind := range kinds {
		bit, ok := fallbackBit(kind)
		if !ok {
			return FallbackPolicy{}, invalid("fallback policy contains an unknown kind")
		}
		if mask&bit != 0 {
			return FallbackPolicy{}, invalid("fallback policy contains a duplicate kind")
		}
		mask |= bit
	}

	return FallbackPolicy{mask: mask, configured: true}, nil
}

// FailClosed returns an explicit policy that never discards a snapshot
// failure in favor of full-history restoration.
func FailClosed() FallbackPolicy {
	return FallbackPolicy{configured: true}
}

// LoadSource identifies how an aggregate was restored.
type LoadSource uint8

const (
	// LoadUnknown indicates that no aggregate was returned.
	LoadUnknown LoadSource = iota
	// LoadFullHistory indicates that the aggregate was loaded without a
	// snapshot.
	LoadFullHistory
	// LoadSnapshot indicates that restoration started from snapshot state.
	LoadSnapshot
)

// LoadInfo describes a successful aggregate restoration without exposing
// snapshot bytes or codec diagnostics.
type LoadInfo struct {
	source          LoadSource
	snapshotVersion uint64
	fallbackReason  error
}

// Source returns the successful restoration source.
func (info LoadInfo) Source() LoadSource {
	return info.source
}

// SnapshotVersion returns the snapshot version used for restoration, or zero
// when full history was loaded.
func (info LoadInfo) SnapshotVersion() uint64 {
	return info.snapshotVersion
}

// FallbackReason returns a stable snapshot error category when full-history
// fallback was used.
func (info LoadInfo) FallbackReason() error {
	return info.fallbackReason
}

// ManagerConfig supplies every replaceable snapshot composition boundary.
type ManagerConfig[ID, Aggregate any] struct {
	AggregateType string
	EncodeID      eventsourcing.IdentifierEncoder[ID]
	Identify      eventsourcing.AggregateIdentifier[ID, Aggregate]
	Lifecycle     eventsourcing.LifecycleAccessor[Aggregate]
	Repository    AggregateRepository[ID, Aggregate]
	Store         eventsourcing.SnapshotStore
	Codec         StateCodec[Aggregate]
	Clock         eventsourcing.Clock
	Metadata      func(Aggregate) (map[string]string, error)
	Fallback      FallbackPolicy
	Migrations    *MigrationChain
}

// Manager explicitly loads and refreshes replaceable aggregate snapshots.
type Manager[ID, Aggregate any] struct {
	aggregateType string
	encodeID      eventsourcing.IdentifierEncoder[ID]
	identify      eventsourcing.AggregateIdentifier[ID, Aggregate]
	lifecycle     eventsourcing.LifecycleAccessor[Aggregate]
	repository    AggregateRepository[ID, Aggregate]
	store         eventsourcing.SnapshotStore
	codec         StateCodec[Aggregate]
	clock         eventsourcing.Clock
	metadata      func(Aggregate) (map[string]string, error)
	fallback      FallbackPolicy
	migrations    *MigrationChain
	schemaVersion eventsourcing.SchemaVersion
}

// NewManager validates and owns snapshot composition.
func NewManager[ID, Aggregate any](
	config ManagerConfig[ID, Aggregate],
) (*Manager[ID, Aggregate], error) {
	if _, err := eventsourcing.NewStreamID(config.AggregateType, "_"); err != nil {
		return nil, invalid("aggregate type must be a canonical name")
	}
	if config.EncodeID == nil ||
		config.Identify == nil ||
		config.Lifecycle == nil {
		return nil, invalid("aggregate callbacks must be assigned")
	}
	if config.Repository == nil ||
		config.Store == nil ||
		config.Codec == nil ||
		config.Clock == nil {
		return nil, invalid("dependencies must be assigned")
	}
	if !config.Fallback.configured {
		return nil, invalid("fallback policy must be assigned")
	}
	if config.Migrations != nil && !config.Migrations.configured {
		return nil, invalid("migration chain must be constructed")
	}
	schemaVersion := config.Codec.SchemaVersion()
	if schemaVersion == 0 {
		return nil, invalid("snapshot schema version must be greater than zero")
	}

	return &Manager[ID, Aggregate]{
		aggregateType: config.AggregateType,
		encodeID:      config.EncodeID,
		identify:      config.Identify,
		lifecycle:     config.Lifecycle,
		repository:    config.Repository,
		store:         config.Store,
		codec:         config.Codec,
		clock:         config.Clock,
		metadata:      config.Metadata,
		fallback:      config.Fallback,
		migrations:    config.Migrations,
		schemaVersion: schemaVersion,
	}, nil
}

// Load restores one aggregate from a compatible snapshot and later events, or
// from complete history only when the fallback policy permits it.
func (manager *Manager[ID, Aggregate]) Load(
	ctx context.Context,
	id ID,
) (Aggregate, LoadInfo, error) {
	var zero Aggregate
	if ctx == nil || manager == nil {
		return zero, LoadInfo{}, eventsourcing.ErrInvalidArgument
	}
	stream, err := manager.stream(id)
	if err != nil {
		return zero, LoadInfo{}, err
	}
	stored, err := manager.store.Load(ctx, stream)
	if err != nil {
		reason := snapshotReason(err)
		if reason == nil {
			return zero, LoadInfo{}, err
		}

		return manager.fallbackLoad(ctx, id, reason)
	}
	if stored.IsZero() || stored.Stream() != stream {
		return manager.fallbackLoad(
			ctx,
			id,
			eventsourcing.ErrSnapshotCorrupt,
		)
	}
	if stored.SchemaVersion() != manager.schemaVersion {
		if manager.migrations == nil {
			return manager.fallbackLoad(
				ctx,
				id,
				eventsourcing.ErrSnapshotIncompatible,
			)
		}
		stored, err = manager.migrations.Migrate(stored, manager.schemaVersion)
		if err != nil {
			reason := snapshotReason(err)
			if reason != nil {
				return manager.fallbackLoad(ctx, id, reason)
			}

			return zero, LoadInfo{}, err
		}
	}

	aggregate, err := manager.repository.Restore(
		ctx,
		id,
		stored.AggregateVersion(),
		func() (Aggregate, error) {
			return decodeState(
				manager.codec,
				stored.State(),
				stored.Metadata(),
			)
		},
	)
	if err != nil {
		reason := snapshotReason(err)
		if reason != nil {
			return manager.fallbackLoad(ctx, id, reason)
		}

		return zero, LoadInfo{}, err
	}

	return aggregate, LoadInfo{
		source:          LoadSnapshot,
		snapshotVersion: stored.AggregateVersion(),
	}, nil
}

// Refresh encodes and saves derived state for a fully persisted aggregate.
//
// Refresh does not append events and does not make event and snapshot writes
// atomic together. Callers choose transaction and scheduling policy.
func (manager *Manager[ID, Aggregate]) Refresh(
	ctx context.Context,
	id ID,
	aggregate Aggregate,
) (eventsourcing.Snapshot, error) {
	if ctx == nil || manager == nil {
		return eventsourcing.Snapshot{}, eventsourcing.ErrInvalidArgument
	}
	stream, err := manager.stream(id)
	if err != nil {
		return eventsourcing.Snapshot{}, err
	}
	aggregateStream, err := manager.stream(manager.identify(aggregate))
	if err != nil || aggregateStream != stream {
		return eventsourcing.Snapshot{}, invalid(
			"aggregate identifier does not match",
		)
	}
	lifecycle := manager.lifecycle(aggregate)
	if lifecycle == nil {
		return eventsourcing.Snapshot{}, invalid(
			"lifecycle accessor returned nil",
		)
	}
	changes, err := lifecycle.Changes()
	if err != nil {
		return eventsourcing.Snapshot{}, err
	}
	if !changes.Empty() || lifecycle.CommittedVersion() == 0 {
		return eventsourcing.Snapshot{}, eventsourcing.ErrInvalidLifecycleState
	}
	state, err := encodeState(manager.codec, aggregate)
	if err != nil {
		return eventsourcing.Snapshot{}, &CreationError{Cause: err}
	}
	var metadata map[string]string
	if manager.metadata != nil {
		metadata, err = provideMetadata(manager.metadata, aggregate)
		if err != nil {
			return eventsourcing.Snapshot{}, &CreationError{Cause: err}
		}
	}
	created, err := eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
		Stream:           stream,
		AggregateVersion: lifecycle.CommittedVersion(),
		SchemaVersion:    manager.schemaVersion,
		State:            state,
		Metadata:         metadata,
		CreatedAt:        manager.clock.Now(),
	})
	if err != nil {
		return eventsourcing.Snapshot{}, err
	}
	if err := manager.store.Save(ctx, created); err != nil {
		return eventsourcing.Snapshot{}, err
	}

	return created, nil
}

// RefreshIfDue refreshes derived state synchronously once the configured
// number of versions have been committed after previousSnapshotVersion. A zero
// previous version represents an aggregate without a retained snapshot.
//
// The returned boolean reports whether a snapshot was saved. The method starts
// no goroutine and does not make event and snapshot writes atomic together.
func (manager *Manager[ID, Aggregate]) RefreshIfDue(
	ctx context.Context,
	id ID,
	aggregate Aggregate,
	previousSnapshotVersion uint64,
	policy ThresholdPolicy,
) (eventsourcing.Snapshot, bool, error) {
	if ctx == nil || manager == nil || policy.interval == 0 {
		return eventsourcing.Snapshot{}, false,
			eventsourcing.ErrInvalidArgument
	}
	stream, err := manager.stream(id)
	if err != nil {
		return eventsourcing.Snapshot{}, false, err
	}
	aggregateStream, err := manager.stream(manager.identify(aggregate))
	if err != nil || aggregateStream != stream {
		return eventsourcing.Snapshot{}, false, invalid(
			"aggregate identifier does not match",
		)
	}
	lifecycle := manager.lifecycle(aggregate)
	if lifecycle == nil {
		return eventsourcing.Snapshot{}, false, invalid(
			"lifecycle accessor returned nil",
		)
	}
	changes, err := lifecycle.Changes()
	if err != nil {
		return eventsourcing.Snapshot{}, false, err
	}
	committedVersion := lifecycle.CommittedVersion()
	if !changes.Empty() || committedVersion == 0 ||
		previousSnapshotVersion > committedVersion {
		return eventsourcing.Snapshot{}, false,
			eventsourcing.ErrInvalidLifecycleState
	}
	if committedVersion-previousSnapshotVersion < policy.interval {
		return eventsourcing.Snapshot{}, false, nil
	}

	created, err := manager.Refresh(ctx, id, aggregate)
	if err != nil {
		return eventsourcing.Snapshot{}, false, err
	}

	return created, true, nil
}

// CreationError redacts application snapshot encoder and metadata-provider
// diagnostics while preserving their cause for errors.Is and errors.As.
type CreationError struct {
	Cause error
}

// Error implements error without disclosing aggregate state or codec details.
func (*CreationError) Error() string {
	return "aggregate snapshot creation failed"
}

// Unwrap preserves the application cause for errors.Is and errors.As.
func (err *CreationError) Unwrap() error {
	return err.Cause
}

func (manager *Manager[ID, Aggregate]) stream(
	id ID,
) (eventsourcing.StreamID, error) {
	encoded, err := manager.encodeID(id)
	if err != nil {
		return eventsourcing.StreamID{}, fmt.Errorf(
			"encode aggregate identifier: %w",
			err,
		)
	}

	return eventsourcing.NewStreamID(manager.aggregateType, encoded)
}

func (manager *Manager[ID, Aggregate]) fallbackLoad(
	ctx context.Context,
	id ID,
	reason error,
) (Aggregate, LoadInfo, error) {
	var zero Aggregate
	if !manager.fallback.allows(reason) {
		return zero, LoadInfo{}, reason
	}
	aggregate, err := manager.repository.Load(ctx, id)
	if err != nil {
		return zero, LoadInfo{}, err
	}

	return aggregate, LoadInfo{
		source:         LoadFullHistory,
		fallbackReason: reason,
	}, nil
}

func (policy FallbackPolicy) allows(reason error) bool {
	var kind FallbackKind
	switch {
	case errors.Is(reason, eventsourcing.ErrSnapshotNotFound):
		kind = FallbackMissing
	case errors.Is(reason, eventsourcing.ErrSnapshotCorrupt):
		kind = FallbackCorrupt
	case errors.Is(reason, eventsourcing.ErrSnapshotIncompatible):
		kind = FallbackIncompatible
	default:
		return false
	}
	bit, _ := fallbackBit(kind)

	return policy.mask&bit != 0
}

func fallbackBit(kind FallbackKind) (uint8, bool) {
	switch kind {
	case FallbackMissing:
		return 1, true
	case FallbackCorrupt:
		return 2, true
	case FallbackIncompatible:
		return 4, true
	default:
		return 0, false
	}
}

func snapshotReason(err error) error {
	switch {
	case errors.Is(err, eventsourcing.ErrSnapshotNotFound):
		return eventsourcing.ErrSnapshotNotFound
	case errors.Is(err, eventsourcing.ErrSnapshotCorrupt):
		return eventsourcing.ErrSnapshotCorrupt
	case errors.Is(err, eventsourcing.ErrSnapshotIncompatible):
		return eventsourcing.ErrSnapshotIncompatible
	default:
		return nil
	}
}

func decodeState[Aggregate any](
	codec StateCodec[Aggregate],
	state []byte,
	metadata map[string]string,
) (aggregate Aggregate, err error) {
	defer func() {
		if recover() != nil {
			var zero Aggregate
			aggregate = zero
			err = ErrStateCodecPanic
		}
	}()

	return codec.Decode(state, metadata)
}

func encodeState[Aggregate any](
	codec StateCodec[Aggregate],
	aggregate Aggregate,
) (state []byte, err error) {
	defer func() {
		if recover() != nil {
			state = nil
			err = ErrStateCodecPanic
		}
	}()

	return codec.Encode(aggregate)
}

func provideMetadata[Aggregate any](
	provider func(Aggregate) (map[string]string, error),
	aggregate Aggregate,
) (metadata map[string]string, err error) {
	defer func() {
		if recover() != nil {
			metadata = nil
			err = ErrMetadataProviderPanic
		}
	}()

	return provider(aggregate)
}

func invalid(reason string) error {
	return fmt.Errorf("%w: %s", eventsourcing.ErrInvalidArgument, reason)
}

var _ error = (*CreationError)(nil)
