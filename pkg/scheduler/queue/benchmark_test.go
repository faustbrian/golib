package queue_test

import (
	"context"
	"testing"

	scheduler "github.com/faustbrian/golib/pkg/scheduler"
	schedulerqueue "github.com/faustbrian/golib/pkg/scheduler/queue"
)

func BenchmarkDispatchEnvelope(b *testing.B) {
	backend := &fakeQueue{}
	dispatcher, _ := schedulerqueue.New(backend)
	schedule, _ := scheduler.NewSchedule(
		"report", "reports.generate", scheduler.Daily(),
		scheduler.WithParameters(map[string]any{"tenant": "acme"}),
	)
	scheduled := scheduler.Context{Schedule: schedule, IdempotencyKey: "key"}
	b.ResetTimer()
	for range b.N {
		if err := dispatcher.Execute(context.Background(), scheduled); err != nil {
			b.Fatal(err)
		}
	}
}
