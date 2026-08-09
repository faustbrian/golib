package queueservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func TestCallbackFailuresPreserveCausesWithoutDisclosingTheirText(t *testing.T) {
	callbackErr := errors.New("endpoint credential task-payload")
	assertSafe := func(t *testing.T, err error, operation CallbackOperation) {
		t.Helper()
		if !errors.Is(err, callbackErr) {
			t.Fatalf("error = %v, want callback cause", err)
		}
		var safeErr *CallbackError
		if !errors.As(err, &safeErr) || safeErr.Operation != operation {
			t.Fatalf("error = %v, want CallbackError for %v", err, operation)
		}
		for _, secret := range []string{"endpoint", "credential", "task-payload"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error disclosed %q: %q", secret, err)
			}
		}
	}

	t.Run("startup", func(t *testing.T) {
		producer, err := NewProducer(ProducerOptions[int]{
			Name:        "startup",
			Resource:    1,
			Correlation: mustFactory(t),
			Startup:     func(context.Context, int) error { return callbackErr },
			Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
				return nil
			},
		})
		if err != nil {
			t.Fatalf("NewProducer() error = %v", err)
		}
		assertSafe(t, producer.Component().Start(context.Background()), CallbackStartup)
	})

	t.Run("readiness", func(t *testing.T) {
		producer, err := NewProducer(ProducerOptions[int]{
			Name:        "readiness",
			Resource:    1,
			Correlation: mustFactory(t),
			Readiness:   func(context.Context, int) error { return callbackErr },
			Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
				return nil
			},
		})
		if err != nil {
			t.Fatalf("NewProducer() error = %v", err)
		}
		if err = producer.Component().Start(context.Background()); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		readiness, ok := producer.Readiness()
		if !ok {
			t.Fatal("Readiness() was absent")
		}
		assertSafe(t, readiness.Run(context.Background()), CallbackReadiness)
	})

	t.Run("publish", func(t *testing.T) {
		producer, err := NewProducer(ProducerOptions[int]{
			Name:        "publish",
			Resource:    1,
			Correlation: mustFactory(t),
			Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
				return callbackErr
			},
		})
		if err != nil {
			t.Fatalf("NewProducer() error = %v", err)
		}
		if err = producer.Component().Start(context.Background()); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		_, err = producer.Publish(producerContext(), queuedPayload("secret"))
		assertSafe(t, err, CallbackPublish)
	})

	t.Run("handler", func(t *testing.T) {
		handler, err := NewHandler(HandlerOptions{
			Correlation: mustFactory(t),
			Handler:     func(context.Context, core.TaskMessage) error { return callbackErr },
		})
		if err != nil {
			t.Fatalf("NewHandler() error = %v", err)
		}
		assertSafe(t, handler(context.Background(), plainTask("secret")), CallbackHandler)
	})

	t.Run("run and shutdown", func(t *testing.T) {
		worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
			Name:        "worker",
			Resource:    1,
			Correlation: mustFactory(t),
			Handler:     func(context.Context, core.TaskMessage) error { return nil },
			Run:         func(context.Context, int, Handler) error { return callbackErr },
			Shutdown:    func(context.Context, int) error { return callbackErr },
		})
		if err != nil {
			t.Fatalf("NewLifecycleWorker() error = %v", err)
		}
		plan := worker.Plan()
		if err = plan.Components[0].Start(context.Background()); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		assertSafe(t, plan.Tasks[0].Run(context.Background()), CallbackRun)
		assertSafe(t, stopWithin(plan.Components[0]), CallbackShutdown)
	})

	t.Run("admission", func(t *testing.T) {
		admissionCalls := 0
		shutdownCalls := 0
		worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
			Name:        "worker-admission",
			Resource:    1,
			Correlation: mustFactory(t),
			Handler:     func(context.Context, core.TaskMessage) error { return nil },
			CloseAdmission: func(int) error {
				admissionCalls++

				return callbackErr
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
		assertSafe(t, component.CloseAdmission(), CallbackAdmission)
		assertSafe(t, stopWithin(component), CallbackAdmission)
		if admissionCalls != 1 || shutdownCalls != 1 {
			t.Fatalf(
				"admission calls = %d, shutdown calls = %d, want 1 each",
				admissionCalls,
				shutdownCalls,
			)
		}
	})
}

func TestStartupErrorDiagnosticReportsWhetherCleanupAlsoFailed(t *testing.T) {
	validation := errors.New("validation secret")
	cleanup := errors.New("cleanup secret")
	withoutCleanup := (&StartupError{Validation: validation}).Error()
	withCleanup := (&StartupError{Validation: validation, Cleanup: cleanup}).Error()
	if withoutCleanup != "queue service startup validation failed" {
		t.Fatalf("validation-only diagnostic = %q", withoutCleanup)
	}
	if withCleanup != "queue service startup validation and cleanup failed" {
		t.Fatalf("validation-and-cleanup diagnostic = %q", withCleanup)
	}
}
