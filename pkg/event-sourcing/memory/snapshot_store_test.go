package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
)

func TestSnapshotStoreSavesLoadsAndDeletesDerivedState(t *testing.T) {
	t.Parallel()

	store := memory.NewSnapshotStore()
	snapshot := memorySnapshot(t, 7, 2, `{"owner":"Ada"}`)
	if err := store.Save(context.Background(), snapshot); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(context.Background(), snapshot.Stream())
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Equal(snapshot) {
		t.Fatalf("loaded snapshot = %#v", loaded)
	}
	state := loaded.State()
	state[0] = '!'
	if string(mustLoadSnapshot(t, store, snapshot.Stream()).State()) !=
		`{"owner":"Ada"}` {
		t.Fatal("loaded snapshot aliases store state")
	}

	if err := store.Save(context.Background(), snapshot); err != nil {
		t.Fatalf("idempotent Save() error = %v", err)
	}
	if err := store.Delete(context.Background(), snapshot.Stream()); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(context.Background(), snapshot.Stream()); err != nil {
		t.Fatalf("idempotent Delete() error = %v", err)
	}
	if _, err := store.Load(
		context.Background(),
		snapshot.Stream(),
	); !errors.Is(err, eventsourcing.ErrSnapshotNotFound) {
		t.Fatalf("Load(deleted) error = %v", err)
	}
}

func TestSnapshotStoreRejectsStaleAndConflictingUpdates(t *testing.T) {
	t.Parallel()

	store := memory.NewSnapshotStore()
	current := memorySnapshot(t, 7, 2, `{"owner":"Ada"}`)
	if err := store.Save(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	for name, snapshot := range map[string]eventsourcing.Snapshot{
		"older aggregate": memorySnapshot(t, 6, 2, `{"owner":"Ada"}`),
		"older schema":    memorySnapshot(t, 8, 1, `{"owner":"Ada"}`),
	} {
		snapshot := snapshot
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := store.Save(context.Background(), snapshot)
			if !errors.Is(err, eventsourcing.ErrSnapshotStale) {
				t.Fatalf("Save() error = %v", err)
			}
			var versionError *eventsourcing.SnapshotVersionError
			if !errors.As(err, &versionError) ||
				versionError.StoredAggregateVersion != 7 ||
				versionError.StoredSchemaVersion != 2 {
				t.Fatalf("SnapshotVersionError = %#v", versionError)
			}
		})
	}

	conflict := memorySnapshot(t, 7, 2, `{"owner":"Grace"}`)
	err := store.Save(context.Background(), conflict)
	if !errors.Is(err, eventsourcing.ErrSnapshotConflict) {
		t.Fatalf("Save(conflict) error = %v", err)
	}
	var conflictError *eventsourcing.SnapshotConflictError
	if !errors.As(err, &conflictError) ||
		conflictError.AggregateVersion != 7 ||
		conflictError.SchemaVersion != 2 {
		t.Fatalf("SnapshotConflictError = %#v", conflictError)
	}
	if !mustLoadSnapshot(t, store, current.Stream()).Equal(current) {
		t.Fatal("rejected update changed stored snapshot")
	}
}

func TestSnapshotStoreAcceptsNewerStateAndSchema(t *testing.T) {
	t.Parallel()

	store := memory.NewSnapshotStore()
	for _, snapshot := range []eventsourcing.Snapshot{
		memorySnapshot(t, 7, 1, `{"owner":"Ada"}`),
		memorySnapshot(t, 7, 2, `{"owner":"Ada","closed":false}`),
		memorySnapshot(t, 8, 2, `{"owner":"Ada","closed":true}`),
	} {
		if err := store.Save(context.Background(), snapshot); err != nil {
			t.Fatal(err)
		}
	}
	loaded := mustLoadSnapshot(
		t,
		store,
		memorySnapshot(t, 8, 2, `{"owner":"Ada","closed":true}`).Stream(),
	)
	if loaded.AggregateVersion() != 8 ||
		loaded.SchemaVersion() != 2 ||
		string(loaded.State()) != `{"owner":"Ada","closed":true}` {
		t.Fatalf("loaded snapshot = %#v", loaded)
	}
}

func TestSnapshotStoreValidatesContextAndValues(t *testing.T) {
	t.Parallel()

	store := memory.NewSnapshotStore()
	stream := mustMemoryStream(t)
	var nilContext context.Context
	if _, err := store.Load(
		nilContext,
		stream,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Load(nil) error = %v", err)
	}
	if err := store.Save(
		nilContext,
		memorySnapshot(t, 1, 1, `{}`),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Save(nil) error = %v", err)
	}
	if err := store.Delete(
		nilContext,
		stream,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Delete(nil) error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Load(
		ctx,
		stream,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load(cancelled) error = %v", err)
	}
	if err := store.Save(
		ctx,
		memorySnapshot(t, 1, 1, `{}`),
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save(cancelled) error = %v", err)
	}
	if err := store.Delete(
		ctx,
		stream,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("Delete(cancelled) error = %v", err)
	}

	if _, err := store.Load(
		context.Background(),
		eventsourcing.StreamID{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Load(zero stream) error = %v", err)
	}
	if err := store.Save(
		context.Background(),
		eventsourcing.Snapshot{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Save(zero snapshot) error = %v", err)
	}
	if err := store.Delete(
		context.Background(),
		eventsourcing.StreamID{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Delete(zero stream) error = %v", err)
	}

	var nilStore *memory.SnapshotStore
	if _, err := nilStore.Load(
		context.Background(),
		stream,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil Load() error = %v", err)
	}
	if err := nilStore.Save(
		context.Background(),
		memorySnapshot(t, 1, 1, `{}`),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil Save() error = %v", err)
	}
	if err := nilStore.Delete(
		context.Background(),
		stream,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil Delete() error = %v", err)
	}

	var zeroStore memory.SnapshotStore
	if _, err := zeroStore.Load(
		context.Background(),
		stream,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("zero Load() error = %v", err)
	}
	if err := zeroStore.Save(
		context.Background(),
		memorySnapshot(t, 1, 1, `{}`),
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("zero Save() error = %v", err)
	}
	if err := zeroStore.Delete(
		context.Background(),
		stream,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("zero Delete() error = %v", err)
	}
}

func memorySnapshot(
	t *testing.T,
	aggregateVersion uint64,
	schemaVersion eventsourcing.SchemaVersion,
	state string,
) eventsourcing.Snapshot {
	t.Helper()

	snapshot, err := eventsourcing.NewSnapshot(eventsourcing.SnapshotInput{
		Stream:           mustMemoryStream(t),
		AggregateVersion: aggregateVersion,
		SchemaVersion:    schemaVersion,
		State:            []byte(state),
		Metadata:         map[string]string{"codec": "json"},
		CreatedAt: time.Date(
			2026,
			time.July,
			25,
			0,
			0,
			int(aggregateVersion),
			0,
			time.UTC,
		),
	})
	if err != nil {
		t.Fatal(err)
	}

	return snapshot
}

func mustMemoryStream(t *testing.T) eventsourcing.StreamID {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("account", "account-42")
	if err != nil {
		t.Fatal(err)
	}

	return stream
}

func mustLoadSnapshot(
	t *testing.T,
	store *memory.SnapshotStore,
	stream eventsourcing.StreamID,
) eventsourcing.Snapshot {
	t.Helper()

	snapshot, err := store.Load(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}

	return snapshot
}
