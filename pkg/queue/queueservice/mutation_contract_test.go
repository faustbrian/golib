package queueservice

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func TestStartupDiagnosticReportsWhetherRollbackAlsoFailed(t *testing.T) {
	validationErr := errors.New("validation endpoint")
	cleanupErr := errors.New("cleanup credential")
	validationOnly := (&StartupError{Validation: validationErr}).Error()
	validationAndCleanup := (&StartupError{
		Validation: validationErr,
		Cleanup:    cleanupErr,
	}).Error()
	if validationOnly == validationAndCleanup ||
		!strings.Contains(validationAndCleanup, "cleanup") {
		t.Fatalf(
			"startup diagnostics = (%q, %q), want a safe cleanup-failure observation",
			validationOnly,
			validationAndCleanup,
		)
	}
	for _, diagnostic := range []string{validationOnly, validationAndCleanup} {
		if strings.Contains(diagnostic, "endpoint") ||
			strings.Contains(diagnostic, "credential") {
			t.Fatalf("startup diagnostic disclosed callback text: %q", diagnostic)
		}
	}
}

func TestSharedProducerCanRetryStartupBecauseNoResourceWasClosed(t *testing.T) {
	startupErr := errors.New("dependency unavailable")
	startupCalls := 0
	publishCalls := 0
	producer, err := NewProducer(ProducerOptions[int]{
		Name:        "shared-producer",
		Resource:    1,
		Correlation: mustFactory(t),
		Startup: func(context.Context, int) error {
			startupCalls++
			if startupCalls == 1 {
				return startupErr
			}

			return nil
		},
		Publish: func(context.Context, int, core.QueuedMessage, ...job.AllowOption) error {
			publishCalls++

			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewProducer() error = %v", err)
	}
	component := producer.Component()
	if err = component.Start(context.Background()); !errors.Is(err, startupErr) {
		t.Fatalf("first Start() error = %v, want startup cause", err)
	}
	if err = component.Start(context.Background()); err != nil {
		t.Fatalf("retried Start() error = %v", err)
	}
	if _, err = producer.Publish(producerContext(), queuedPayload("work")); err != nil {
		t.Fatalf("Publish() after recovered startup error = %v", err)
	}
	if startupCalls != 2 || publishCalls != 1 {
		t.Fatalf("startup calls = %d, publish calls = %d, want 2 and 1", startupCalls, publishCalls)
	}
}

func TestWorkerReadinessCannotEnterBeforeStartup(t *testing.T) {
	readinessCalls := 0
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name:        "worker",
		Resource:    1,
		Correlation: mustFactory(t),
		Handler:     func(context.Context, core.TaskMessage) error { return nil },
		Readiness: func(context.Context, int) error {
			readinessCalls++

			return nil
		},
		Run:      func(context.Context, int, Handler) error { return nil },
		Shutdown: func(context.Context, int) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	readiness := worker.Plan().Readiness[0]
	if err = readiness.Run(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Readiness() before Start error = %v, want ErrUnavailable", err)
	}
	if readinessCalls != 0 {
		t.Fatalf("readiness calls before Start = %d, want 0", readinessCalls)
	}
}
