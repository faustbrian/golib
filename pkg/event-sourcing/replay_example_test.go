package eventsourcing_test

import (
	"context"
	"fmt"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/memory"
	"github.com/faustbrian/golib/pkg/event-sourcing/projection"
)

func Example_replayProjection() {
	ctx := context.Background()
	store := memory.NewStore()
	repository, err := quickstartRepository(store)
	if err != nil {
		panic(err)
	}
	account := &quickstartAccount{id: "account-42"}
	if err := account.open("Ada"); err != nil {
		panic(err)
	}
	if _, err := repository.Save(ctx, account); err != nil {
		panic(err)
	}

	var seen string
	runner, err := projection.NewRunner(projection.RunnerConfig{
		Name:        "account-summary",
		Reader:      store,
		Checkpoints: memory.NewProjectionStore(),
		BatchSize:   10,
		Handler: func(_ context.Context, delivery eventsourcing.Delivery) error {
			seen = delivery.Mode().String() + ":" +
				delivery.Message().EventName().String()

			return nil
		},
	})
	if err != nil {
		panic(err)
	}
	batch, err := runner.RunBatch(ctx)
	if err != nil {
		panic(err)
	}
	next, err := runner.RunBatch(ctx)
	if err != nil {
		panic(err)
	}

	fmt.Printf(
		"%s handled=%d checkpoint=%d next=%d\n",
		seen,
		batch.Handled(),
		batch.Checkpoint(),
		next.Scanned(),
	)
	// Output: replay:account.opened handled=1 checkpoint=1 next=0
}
