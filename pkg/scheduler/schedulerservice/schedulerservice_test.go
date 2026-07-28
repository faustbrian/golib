package schedulerservice_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/correlation"
	queuecorrelation "github.com/faustbrian/golib/pkg/correlation/queue"
	schedulecorrelation "github.com/faustbrian/golib/pkg/correlation/schedule"
	"github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/memory"
	"github.com/faustbrian/golib/pkg/scheduler/schedulerservice"
	"github.com/faustbrian/golib/pkg/scheduler/schedulertest"
	"github.com/faustbrian/golib/pkg/service"
)

type executorFunc func(context.Context, scheduler.Context) error

func (execute executorFunc) Execute(
	ctx context.Context,
	scheduled scheduler.Context,
) error {
	return execute(ctx, scheduled)
}

type noOpExecutor struct{}

func (noOpExecutor) Execute(context.Context, scheduler.Context) error { return nil }

func TestPlanStopsSchedulingDrainsExecutionsThenClosesFacilities(t *testing.T) {
	start := time.Date(2026, time.January, 1, 0, 0, 30, 0, time.UTC)
	clock := schedulertest.NewFakeClock(start)
	registry := registry(t, nil)
	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		t.Fatalf("correlation.NewFactory() error = %v", err)
	}
	executionStarted := make(chan struct{})
	releaseExecution := make(chan struct{})
	facilityClosed := make(chan struct{})
	adapter, err := schedulerservice.New(schedulerservice.Options{
		Name:        "scheduler",
		Registry:    registry,
		Leases:      memory.New(),
		Correlation: factory,
		Executor: executorFunc(func(ctx context.Context, _ scheduler.Context) error {
			values, ok := correlation.FromContext(ctx)
			if !ok || values.CorrelationID == "" || values.RequestID == "" {
				t.Errorf("correlation values = %#v, %v", values, ok)
			}
			close(executionStarted)
			<-releaseExecution

			return nil
		}),
		RunnerOptions: []scheduler.RunnerOption{
			scheduler.WithOwner("replica-a"),
			scheduler.WithClock(clock),
		},
		Facilities: []service.Component{{
			Name:  "lease-store",
			Start: func(context.Context) error { return nil },
			Stop: func(context.Context) error {
				close(facilityClosed)

				return nil
			},
		}},
	})
	if err != nil {
		t.Fatalf("schedulerservice.New() error = %v", err)
	}

	plan := adapter.Plan()
	runtime, err := service.New(service.Config{Components: plan.Components})
	if err != nil {
		t.Fatalf("service.New() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := runtime.Go(plan.Tasks[0].Name, plan.Tasks[0].Run); err != nil {
		t.Fatalf("Go() error = %v", err)
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if !clock.WaitForTimers(waitCtx, 1) {
		t.Fatal("scheduler did not register its timer")
	}
	clock.Advance(30 * time.Second)
	select {
	case <-executionStarted:
	case <-time.After(time.Second):
		t.Fatal("scheduled execution did not start")
	}

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- runtime.Shutdown(context.Background())
	}()
	select {
	case <-facilityClosed:
		t.Fatal("facility closed before the scheduled execution drained")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseExecution)
	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	select {
	case <-facilityClosed:
	default:
		t.Fatal("facility remained open after scheduler drain")
	}
	if err := adapter.Runner().Tick(
		context.Background(),
		start,
		start.Add(time.Minute),
	); !errors.Is(err, scheduler.ErrDraining) {
		t.Fatalf("Tick() after shutdown error = %v, want ErrDraining", err)
	}
}

func TestTrustedMetadataContinuesAnExplicitCorrelationWorkflow(t *testing.T) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		t.Fatalf("correlation.NewFactory() error = %v", err)
	}
	propagation, err := schedulecorrelation.New(
		factory,
		schedulecorrelation.Options{},
	)
	if err != nil {
		t.Fatalf("schedulecorrelation.New() error = %v", err)
	}
	parent, err := propagation.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	metadata := map[string]string{}
	enqueued, err := propagation.Enqueue(metadata, parent)
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	var received correlation.Values
	adapter, err := schedulerservice.New(schedulerservice.Options{
		Name:            "scheduler",
		Registry:        registry(t, metadata),
		Leases:          memory.New(),
		Correlation:     factory,
		CorrelationMode: schedulerservice.CorrelationTrustedMetadata,
		Executor: executorFunc(func(ctx context.Context, _ scheduler.Context) error {
			received, _ = correlation.FromContext(ctx)

			return nil
		}),
		RunnerOptions: []scheduler.RunnerOption{
			scheduler.WithOwner("replica-a"),
		},
	})
	if err != nil {
		t.Fatalf("schedulerservice.New() error = %v", err)
	}
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	if err := adapter.Runner().Tick(
		context.Background(),
		now.Add(-time.Minute),
		now,
	); err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if received.CorrelationID != enqueued.CorrelationID ||
		received.CausationID.String() != enqueued.RequestID.String() ||
		received.RequestID == enqueued.RequestID {
		t.Fatalf("received correlation = %#v, enqueued = %#v", received, enqueued)
	}
}

func TestIndependentOccurrencesReceiveDistinctWorkflows(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{})
	values := make([]correlation.Values, 0, 2)
	adapter, err := schedulerservice.New(schedulerservice.Options{
		Name: "scheduler", Registry: registry(t, nil), Leases: memory.New(),
		Correlation: factory,
		Executor: executorFunc(func(ctx context.Context, _ scheduler.Context) error {
			current, _ := correlation.FromContext(ctx)
			values = append(values, current)

			return nil
		}),
		RunnerOptions: []scheduler.RunnerOption{
			scheduler.WithOwner("replica-a"),
		},
	})
	if err != nil {
		t.Fatalf("schedulerservice.New() error = %v", err)
	}
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	for range 2 {
		if err := adapter.Runner().Tick(
			context.Background(),
			now.Add(-time.Minute),
			now,
		); err != nil {
			t.Fatalf("Tick() error = %v", err)
		}
		now = now.Add(time.Minute)
	}
	if len(values) != 2 ||
		values[0].CorrelationID == values[1].CorrelationID ||
		values[0].RequestID == values[1].RequestID ||
		values[0].CausationID != "" ||
		values[1].CausationID != "" {
		t.Fatalf("independent values = %#v", values)
	}
}

func TestPlanSnapshotsFacilityComposition(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{})
	facilities := []service.Component{{Name: "leases"}}
	adapter, err := schedulerservice.New(schedulerservice.Options{
		Name: "scheduler", Registry: registry(t, nil), Leases: memory.New(),
		Correlation: factory, Executor: noOpExecutor{},
		RunnerOptions: []scheduler.RunnerOption{
			scheduler.WithOwner("replica-a"),
		},
		Facilities: facilities,
	})
	if err != nil {
		t.Fatalf("schedulerservice.New() error = %v", err)
	}
	facilities[0].Name = "changed"
	first := adapter.Plan()
	first.Components[0].Name = "mutated"
	first.Tasks[0].Name = "mutated"
	second := adapter.Plan()
	if second.Components[0].Name != "leases" ||
		second.Components[1].Name != "scheduler-drain" ||
		second.Tasks[0].Name != "scheduler" {
		t.Fatalf("Plan() snapshot = %#v", second)
	}
}

func TestNewRejectsInvalidOptions(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{})
	validRegistry := registry(t, nil)
	validExecutor := noOpExecutor{}
	tests := []schedulerservice.Options{
		{
			Registry: validRegistry, Leases: memory.New(),
			Correlation: factory, Executor: validExecutor,
		},
		{
			Name: "scheduler", Leases: memory.New(),
			Correlation: factory, Executor: validExecutor,
		},
		{
			Name: "scheduler", Registry: validRegistry,
			Correlation: factory, Executor: validExecutor,
		},
		{
			Name: "scheduler", Registry: validRegistry, Leases: memory.New(),
			Executor: validExecutor,
		},
		{
			Name: "scheduler", Registry: validRegistry, Leases: memory.New(),
			Correlation: factory,
		},
		{
			Name: "scheduler", Registry: validRegistry, Leases: memory.New(),
			Correlation: factory, Executor: validExecutor,
			CorrelationMode: schedulerservice.CorrelationMode(255),
		},
	}
	for _, options := range tests {
		_, err := schedulerservice.New(options)
		if !errors.Is(err, schedulerservice.ErrInvalidOptions) {
			t.Fatalf("New(%+v) error = %v, want ErrInvalidOptions", options, err)
		}
		var optionsError *schedulerservice.OptionsError
		if !errors.As(err, &optionsError) || optionsError.Error() == "" {
			t.Fatalf("New(%+v) error = %v, want OptionsError", options, err)
		}
	}

	var nilExecutor executorFunc
	_, err := schedulerservice.New(schedulerservice.Options{
		Name: "scheduler", Registry: validRegistry, Leases: memory.New(),
		Correlation: factory, Executor: nilExecutor,
		RunnerOptions: []scheduler.RunnerOption{
			scheduler.WithOwner("replica-a"),
		},
	})
	if !errors.Is(err, schedulerservice.ErrInvalidOptions) {
		t.Fatalf("New(typed nil executor) error = %v, want ErrInvalidOptions", err)
	}
}

func TestNewPreservesCorrelationAndRunnerConstructionFailures(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{})
	validRegistry := registry(t, nil)
	validExecutor := executorFunc(func(context.Context, scheduler.Context) error {
		return nil
	})

	_, correlationErr := schedulerservice.New(schedulerservice.Options{
		Name: "scheduler", Registry: validRegistry, Leases: memory.New(),
		Correlation: factory, Executor: validExecutor,
		CorrelationOptions: schedulecorrelation.Options{
			Queue: queuecorrelation.Options{
				Codec: correlation.CodecOptions{
					Policy: correlation.Policy{MaxLength: -1},
				},
			},
		},
	})
	if correlationErr == nil ||
		errors.Is(correlationErr, schedulerservice.ErrInvalidOptions) {
		t.Fatalf("New(invalid correlation) error = %v", correlationErr)
	}

	_, runnerErr := schedulerservice.New(schedulerservice.Options{
		Name: "scheduler", Registry: validRegistry, Leases: memory.New(),
		Correlation: factory, Executor: validExecutor,
	})
	if !errors.Is(runnerErr, scheduler.ErrInvalidRunner) {
		t.Fatalf("New(invalid runner) error = %v, want ErrInvalidRunner", runnerErr)
	}
}

func TestOccurrencePreservesCorrelationGenerationFailure(t *testing.T) {
	generationErr := errors.New("entropy unavailable")
	factory, err := correlation.NewFactory(correlation.FactoryOptions{
		Generator: correlation.GeneratorFunc(func() (string, error) {
			return "", generationErr
		}),
	})
	if err != nil {
		t.Fatalf("correlation.NewFactory() error = %v", err)
	}
	executed := false
	adapter, err := schedulerservice.New(schedulerservice.Options{
		Name: "scheduler", Registry: registry(t, nil), Leases: memory.New(),
		Correlation: factory,
		Executor: executorFunc(func(context.Context, scheduler.Context) error {
			executed = true

			return nil
		}),
		RunnerOptions: []scheduler.RunnerOption{
			scheduler.WithOwner("replica-a"),
		},
	})
	if err != nil {
		t.Fatalf("schedulerservice.New() error = %v", err)
	}
	now := time.Date(2026, time.January, 1, 0, 1, 0, 0, time.UTC)
	if err := adapter.Runner().Tick(
		context.Background(),
		now.Add(-time.Minute),
		now,
	); !errors.Is(err, generationErr) {
		t.Fatalf("Tick() error = %v, want generation failure", err)
	}
	if executed {
		t.Fatal("executor ran without correlation values")
	}
}

func registry(t *testing.T, metadata map[string]string) *scheduler.Registry {
	t.Helper()

	options := []scheduler.Option{
		scheduler.WithMissedRuns(scheduler.MissedRunOnce, 0),
		scheduler.WithRunTimeout(time.Minute),
	}
	if metadata != nil {
		options = append(options, scheduler.WithMetadata(metadata))
	}
	schedule, err := scheduler.NewSchedule(
		"minute",
		"task.minute",
		scheduler.EveryMinute(),
		options...,
	)
	if err != nil {
		t.Fatalf("NewSchedule() error = %v", err)
	}
	registry, err := scheduler.Compile(schedule)
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	return registry
}
