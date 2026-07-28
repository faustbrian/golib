package schedulerservice_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
	"github.com/faustbrian/golib/pkg/scheduler/schedulerservice"
)

func ExampleNew() {
	schedule, _ := scheduler.NewSchedule(
		"nightly-report",
		"reports.generate",
		scheduler.Daily(),
	)
	registry, _ := scheduler.Compile(schedule)
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{})
	adapter, err := schedulerservice.New(schedulerservice.Options{
		Name:        "scheduler",
		Registry:    registry,
		Leases:      memory.New(),
		Executor:    executorFunc(func(context.Context, scheduler.Context) error { return nil }),
		Correlation: factory,
		RunnerOptions: []scheduler.RunnerOption{
			scheduler.WithOwner("replica-a"),
		},
	})
	if err != nil {
		fmt.Println("scheduler setup failed")

		return
	}

	plan := adapter.Plan()
	fmt.Println(plan.Tasks[0].Name, len(plan.Components))
	// Output: scheduler 1
}
