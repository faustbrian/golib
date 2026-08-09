package queueservice

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func TestProducerDrainAndShutdownShareOneTerminationBudget(t *testing.T) {
	type budgetKey struct{}
	const budgetValue = "termination-budget"
	publishEntered := make(chan struct{})
	releasePublish := make(chan struct{})
	producer, err := NewProducer(ProducerOptions[int]{
		Name: "producer", Resource: 1, Correlation: mustFactory(t),
		Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
			close(publishEntered)
			<-releasePublish

			return nil
		},
		Shutdown: func(ctx context.Context, _ int) error {
			if ctx.Value(budgetKey{}) != budgetValue {
				return errors.New("shutdown received a different context")
			}
			if _, ok := ctx.Deadline(); !ok {
				return errors.New("shutdown lost the termination deadline")
			}

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	component := producer.Component()
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	publishResult := make(chan error, 1)
	go func() {
		_, publishErr := producer.Publish(producerContext(), queuedPayload("work"))
		publishResult <- publishErr
	}()
	awaitValue(t, publishEntered)

	budget, cancelBudget := context.WithTimeout(
		context.WithValue(context.Background(), budgetKey{}, budgetValue),
		time.Second,
	)
	defer cancelBudget()
	stopResult := make(chan error, 1)
	go func() { stopResult <- component.Stop(budget) }()
	awaitProducerStopping(t, producer)
	close(releasePublish)
	if err = awaitValue(t, publishResult); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err = awaitValue(t, stopResult); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestLifecycleWorkerRacesDrainReadinessCancellationAndDuplicateSignals(t *testing.T) {
	const iterations = 64
	const readinessCalls = 8
	for iteration := 0; iteration < iterations; iteration++ {
		runEntered := make(chan struct{})
		readinessEntered := make(chan struct{}, readinessCalls)
		releaseReadiness := make(chan struct{})
		admissionEntered := make(chan struct{})
		var admissionCalls atomic.Int64
		var shutdownCalls atomic.Int64
		worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
			Name: "worker", Resource: iteration + 1, Correlation: mustFactory(t),
			Handler: func(context.Context, core.TaskMessage) error { return nil },
			Readiness: func(context.Context, int) error {
				readinessEntered <- struct{}{}
				<-releaseReadiness

				return nil
			},
			CloseAdmission: func(int) error {
				admissionCalls.Add(1)
				close(admissionEntered)

				return nil
			},
			Run: func(ctx context.Context, _ int, _ Handler) error {
				close(runEntered)
				<-ctx.Done()

				return nil
			},
			Shutdown: func(context.Context, int) error {
				shutdownCalls.Add(1)

				return nil
			},
		})
		if err != nil {
			t.Fatalf("iteration %d: NewLifecycleWorker() error = %v", iteration, err)
		}
		plan := worker.Plan()
		component := plan.Components[0]
		if err = component.Start(context.Background()); err != nil {
			t.Fatalf("iteration %d: Start() error = %v", iteration, err)
		}
		runContext, cancelRun := context.WithCancel(context.Background())
		runResult := make(chan error, 1)
		go func() { runResult <- plan.Tasks[0].Run(runContext) }()
		awaitValue(t, runEntered)

		readinessResults := make(chan error, readinessCalls)
		for call := 0; call < readinessCalls; call++ {
			go func() { readinessResults <- plan.Readiness[0].Run(context.Background()) }()
		}
		for call := 0; call < readinessCalls; call++ {
			awaitValue(t, readinessEntered)
		}

		stopContext, cancelStop := context.WithTimeout(context.Background(), time.Second)
		stopResult := make(chan error, 1)
		go func() { stopResult <- component.Stop(stopContext) }()
		awaitValue(t, admissionEntered)

		var duplicateSignals sync.WaitGroup
		for signal := 0; signal < 8; signal++ {
			duplicateSignals.Add(1)
			go func() {
				defer duplicateSignals.Done()
				if closeErr := component.CloseAdmission(); closeErr != nil {
					t.Errorf("iteration %d: duplicate CloseAdmission() error = %v", iteration, closeErr)
				}
			}()
		}
		duplicateSignals.Wait()
		if err = plan.Tasks[0].Run(context.Background()); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("iteration %d: duplicate Run() error = %v, want ErrUnavailable", iteration, err)
		}

		cancelRun()
		close(releaseReadiness)
		for call := 0; call < readinessCalls; call++ {
			if err = awaitValue(t, readinessResults); err != nil {
				t.Fatalf("iteration %d: admitted readiness error = %v", iteration, err)
			}
		}
		if err = awaitValue(t, runResult); err != nil {
			t.Fatalf("iteration %d: Run() cancellation error = %v", iteration, err)
		}
		if err = awaitValue(t, stopResult); err != nil {
			t.Fatalf("iteration %d: Stop() error = %v", iteration, err)
		}
		cancelStop()
		if admissionCalls.Load() != 1 || shutdownCalls.Load() != 1 {
			t.Fatalf(
				"iteration %d: admission/shutdown calls = %d/%d, want 1/1",
				iteration,
				admissionCalls.Load(),
				shutdownCalls.Load(),
			)
		}
		if err = plan.Readiness[0].Run(context.Background()); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("iteration %d: stale readiness error = %v", iteration, err)
		}
	}
}

func TestAdmissionPanicIsCachedAndDoesNotPreventShutdown(t *testing.T) {
	shutdownCalls := 0
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name: "worker", Resource: 1, Correlation: mustFactory(t),
		Handler: func(context.Context, core.TaskMessage) error { return nil },
		CloseAdmission: func(int) error {
			panic("sensitive admission value")
		},
		Run: func(context.Context, int, Handler) error { return nil },
		Shutdown: func(context.Context, int) error {
			shutdownCalls++

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	component := worker.Plan().Components[0]
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	for call := 0; call < 2; call++ {
		err = component.CloseAdmission()
		var panicErr *CallbackPanicError
		if !errors.As(err, &panicErr) || panicErr.Operation != CallbackAdmission {
			t.Fatalf("CloseAdmission(%d) error = %v, want admission panic", call, err)
		}
		if strings.Contains(err.Error(), "sensitive") {
			t.Fatal("admission panic diagnostic disclosed its value")
		}
	}
	if err = stopWithin(component); !errors.Is(err, ErrCallbackPanic) {
		t.Fatalf("Stop() error = %v, want cached admission panic", err)
	}
	if shutdownCalls != 1 {
		t.Fatalf("shutdown calls = %d, want 1", shutdownCalls)
	}
}

func TestStartupRollbackShutdownPanicPreservesBothCauses(t *testing.T) {
	startupErr := errors.New("startup failed")
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name: "worker", Resource: 1, Correlation: mustFactory(t),
		Handler: func(context.Context, core.TaskMessage) error { return nil },
		Startup: func(context.Context, int) error { return startupErr },
		Run:     func(context.Context, int, Handler) error { return nil },
		Shutdown: func(context.Context, int) error {
			panic("sensitive rollback value")
		},
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	err = worker.Plan().Components[0].Start(context.Background())
	if !errors.Is(err, startupErr) || !errors.Is(err, ErrCallbackPanic) {
		t.Fatalf("Start() error = %v, want startup and rollback panic causes", err)
	}
	if strings.Contains(err.Error(), "sensitive") {
		t.Fatal("rollback panic diagnostic disclosed its value")
	}
}
