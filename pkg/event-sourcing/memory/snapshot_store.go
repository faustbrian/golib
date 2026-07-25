package memory

import (
	"context"
	"sync"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

// SnapshotStore is a concurrency-safe in-memory derived snapshot store.
type SnapshotStore struct {
	mutex     sync.RWMutex
	snapshots map[eventsourcing.StreamID]eventsourcing.Snapshot
}

// NewSnapshotStore creates an empty independent snapshot store.
func NewSnapshotStore() *SnapshotStore {
	return &SnapshotStore{
		snapshots: make(map[eventsourcing.StreamID]eventsourcing.Snapshot),
	}
}

// Load returns one immutable snapshot or ErrSnapshotNotFound.
func (store *SnapshotStore) Load(
	ctx context.Context,
	stream eventsourcing.StreamID,
) (eventsourcing.Snapshot, error) {
	if store == nil || store.snapshots == nil || ctx == nil || stream.IsZero() {
		return eventsourcing.Snapshot{}, eventsourcing.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return eventsourcing.Snapshot{}, err
	}

	store.mutex.RLock()
	defer store.mutex.RUnlock()
	snapshot, exists := store.snapshots[stream]
	if !exists {
		return eventsourcing.Snapshot{}, eventsourcing.ErrSnapshotNotFound
	}

	return snapshot, nil
}

// Save atomically retains non-regressing derived snapshot state.
func (store *SnapshotStore) Save(
	ctx context.Context,
	snapshot eventsourcing.Snapshot,
) error {
	if store == nil || store.snapshots == nil || ctx == nil || snapshot.IsZero() {
		return eventsourcing.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()
	current, exists := store.snapshots[snapshot.Stream()]
	if !exists {
		store.snapshots[snapshot.Stream()] = snapshot

		return nil
	}
	if snapshot.AggregateVersion() < current.AggregateVersion() ||
		snapshot.SchemaVersion() < current.SchemaVersion() {
		return &eventsourcing.SnapshotVersionError{
			Stream:                   snapshot.Stream(),
			StoredAggregateVersion:   current.AggregateVersion(),
			IncomingAggregateVersion: snapshot.AggregateVersion(),
			StoredSchemaVersion:      current.SchemaVersion(),
			IncomingSchemaVersion:    snapshot.SchemaVersion(),
		}
	}
	if snapshot.AggregateVersion() == current.AggregateVersion() &&
		snapshot.SchemaVersion() == current.SchemaVersion() {
		if snapshot.Equal(current) {
			return nil
		}

		return &eventsourcing.SnapshotConflictError{
			Stream:           snapshot.Stream(),
			AggregateVersion: snapshot.AggregateVersion(),
			SchemaVersion:    snapshot.SchemaVersion(),
		}
	}

	store.snapshots[snapshot.Stream()] = snapshot

	return nil
}

// Delete removes derived state idempotently.
func (store *SnapshotStore) Delete(
	ctx context.Context,
	stream eventsourcing.StreamID,
) error {
	if store == nil || store.snapshots == nil || ctx == nil || stream.IsZero() {
		return eventsourcing.ErrInvalidArgument
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	store.mutex.Lock()
	defer store.mutex.Unlock()
	delete(store.snapshots, stream)

	return nil
}

var _ eventsourcing.SnapshotStore = (*SnapshotStore)(nil)
