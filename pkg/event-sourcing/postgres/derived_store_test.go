package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
)

func TestDerivedStoreConstructorsValidateDependenciesAndConfiguration(
	t *testing.T,
) {
	t.Parallel()

	if store, err := postgres.NewSnapshotStore(
		nil,
		postgres.Config{},
	); store != nil || !errors.Is(err, postgres.ErrPoolRequired) {
		t.Fatalf("NewSnapshotStore(nil) = %#v, %v", store, err)
	}
	if store, err := postgres.NewProjectionStore(
		nil,
		postgres.Config{},
	); store != nil || !errors.Is(err, postgres.ErrPoolRequired) {
		t.Fatalf("NewProjectionStore(nil) = %#v, %v", store, err)
	}
	if writer, err := postgres.NewTxCheckpointWriter(
		nil,
		postgres.Config{},
	); writer != nil || !errors.Is(err, postgres.ErrTransactionRequired) {
		t.Fatalf("NewTxCheckpointWriter(nil) = %#v, %v", writer, err)
	}

	invalid := postgres.Config{Schema: "not-valid"}
	if store, err := postgres.NewSnapshotStore(
		nil,
		invalid,
	); store != nil || !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewSnapshotStore(invalid) = %#v, %v", store, err)
	}
	if store, err := postgres.NewProjectionStore(
		nil,
		invalid,
	); store != nil || !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewProjectionStore(invalid) = %#v, %v", store, err)
	}
	if writer, err := postgres.NewTxCheckpointWriter(
		nil,
		invalid,
	); writer != nil || !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("NewTxCheckpointWriter(invalid) = %#v, %v", writer, err)
	}
}

func TestDerivedStoresImplementPublicContracts(t *testing.T) {
	t.Parallel()

	var _ eventsourcing.SnapshotStore = (*postgres.SnapshotStore)(nil)
	var _ projection.CheckpointStore = (*postgres.ProjectionStore)(nil)
	var _ projection.ControlStore = (*postgres.ProjectionStore)(nil)
}

func TestCommitErrorRedactsAndPreservesCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("driver detail with private data")
	err := &postgres.CommitError{Cause: cause}
	if !errors.Is(err, postgres.ErrCommitOutcomeUnknown) ||
		!errors.Is(err, cause) ||
		strings.Contains(err.Error(), "private data") {
		t.Fatalf("CommitError = %q", err)
	}
	var commitError *postgres.CommitError
	if !errors.As(err, &commitError) || commitError.Cause != cause {
		t.Fatalf("errors.As() = %#v", commitError)
	}
}

func TestDerivedStoresRejectInvalidReceiversAndArguments(t *testing.T) {
	t.Parallel()

	var snapshots *postgres.SnapshotStore
	var checkpoints *postgres.ProjectionStore
	var writer *postgres.TxCheckpointWriter
	var nilContext context.Context

	if _, err := snapshots.Load(
		context.Background(),
		eventsourcing.StreamID{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("SnapshotStore.Load() error = %v", err)
	}
	if err := snapshots.Save(
		context.Background(),
		eventsourcing.Snapshot{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("SnapshotStore.Save() error = %v", err)
	}
	if err := snapshots.Delete(
		context.Background(),
		eventsourcing.StreamID{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("SnapshotStore.Delete() error = %v", err)
	}
	if _, err := checkpoints.Status(
		context.Background(),
		"projection",
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("ProjectionStore.Status() error = %v", err)
	}
	if err := checkpoints.Save(
		context.Background(),
		"projection",
		0,
		1,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("ProjectionStore.Save() error = %v", err)
	}
	if _, err := checkpoints.Pause(
		nilContext,
		"projection",
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("ProjectionStore.Pause() error = %v", err)
	}
	if _, err := checkpoints.Resume(
		context.Background(),
		"",
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("ProjectionStore.Resume() error = %v", err)
	}
	if _, err := checkpoints.ResetCheckpoint(
		context.Background(),
		"projection",
		0,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("ProjectionStore.ResetCheckpoint() error = %v", err)
	}
	if err := writer.Stage(
		context.Background(),
		"projection",
		0,
		1,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("TxCheckpointWriter.Stage() error = %v", err)
	}
}
