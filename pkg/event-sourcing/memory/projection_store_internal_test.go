package memory

import (
	"context"
	"errors"
	"testing"

	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
)

func TestProjectionStoreChecksCancellationAfterLockAcquisition(t *testing.T) {
	t.Parallel()

	tests := map[string]func(
		*ProjectionStore,
		context.Context,
	) error{
		"load": func(
			store *ProjectionStore,
			ctx context.Context,
		) error {
			_, err := store.Load(ctx, "account-summary")

			return err
		},
		"status": func(
			store *ProjectionStore,
			ctx context.Context,
		) error {
			_, err := store.Status(ctx, "account-summary")

			return err
		},
		"save": func(
			store *ProjectionStore,
			ctx context.Context,
		) error {
			return store.Save(ctx, "account-summary", 0, 1)
		},
		"pause": func(
			store *ProjectionStore,
			ctx context.Context,
		) error {
			_, err := store.Pause(ctx, "account-summary")

			return err
		},
		"resume": func(
			store *ProjectionStore,
			ctx context.Context,
		) error {
			_, err := store.Resume(ctx, "account-summary")

			return err
		},
		"reset": func(
			store *ProjectionStore,
			ctx context.Context,
		) error {
			_, err := store.ResetCheckpoint(
				ctx,
				"account-summary",
				0,
			)

			return err
		},
	}
	for name, operation := range tests {
		operation := operation
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			store := NewProjectionStore()
			ctx := &cancelAfterChecks{allowed: 1}
			if err := operation(store, ctx); !errors.Is(
				err,
				context.Canceled,
			) {
				t.Fatalf("operation() error = %v", err)
			}
			if len(store.checkpoints) != 0 || len(store.states) != 0 {
				t.Fatal("cancelled operation mutated store")
			}
		})
	}
}

func TestProjectionStoreReportsMissingActualCheckpointConflict(
	t *testing.T,
) {
	t.Parallel()

	store := NewProjectionStore()
	err := store.Save(
		context.Background(),
		"account-summary",
		1,
		2,
	)
	var conflict *projection.CheckpointConflictError
	if !errors.As(err, &conflict) ||
		!errors.Is(err, projection.ErrCheckpointConflict) ||
		conflict.Expected != 1 ||
		conflict.Actual != 0 ||
		conflict.ActualExists {
		t.Fatalf("Save() error = %v", err)
	}
}
