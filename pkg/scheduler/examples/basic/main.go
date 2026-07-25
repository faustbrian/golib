// Command basic runs a single-process scheduler with an in-memory lease store.
package main

import (
	"context"
	"errors"
	"log"
	"os/signal"
	"syscall"
	"time"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
)

type executor func(context.Context, scheduler.Context) error

func (execute executor) Execute(ctx context.Context, scheduled scheduler.Context) error {
	return execute(ctx, scheduled)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	schedule, err := scheduler.NewSchedule(
		"heartbeat",
		"service.heartbeat",
		scheduler.EveryMinute(),
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithoutOverlap(scheduler.OverlapSkip, time.Minute),
	)
	if err != nil {
		log.Fatal(err)
	}
	registry, err := scheduler.Compile(schedule)
	if err != nil {
		log.Fatal(err)
	}
	runner, err := scheduler.NewRunner(
		registry,
		memory.New(),
		executor(func(_ context.Context, scheduled scheduler.Context) error {
			log.Printf("running %s for %s", scheduled.Schedule.Name, scheduled.Due)
			return nil
		}),
		scheduler.WithOwner("local-example"),
	)
	if err != nil {
		log.Fatal(err)
	}
	if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
	drainCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := runner.Drain(drainCtx); err != nil {
		log.Fatal(err)
	}
}
