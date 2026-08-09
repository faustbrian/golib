package queueservice

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/service"
	"github.com/faustbrian/golib/pkg/service/healthhttp"
)

func TestHealthProbesRaceWorkerDrainAndCancellation(t *testing.T) {
	readinessEntered := make(chan struct{}, 1)
	releaseReadiness := make(chan struct{})
	runEntered := make(chan struct{})
	runCanceled := make(chan struct{})
	var blockReadiness atomic.Bool
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name: "worker", Resource: 1, Correlation: mustFactory(t),
		Handler: func(context.Context, core.TaskMessage) error { return nil },
		Readiness: func(context.Context, int) error {
			if blockReadiness.Load() {
				readinessEntered <- struct{}{}
				<-releaseReadiness
			}

			return nil
		},
		Run: func(ctx context.Context, _ int, _ Handler) error {
			close(runEntered)
			<-ctx.Done()
			close(runCanceled)

			return ctx.Err()
		},
		Shutdown: func(context.Context, int) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	runtime, probes := startHealthRuntime(t, worker)
	awaitValue(t, runEntered)
	blockReadiness.Store(true)
	admitted := make(chan int, 1)
	go func() { admitted <- probeStatus(probes.Readiness()) }()
	awaitValue(t, readinessEntered)

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	firstStop := make(chan error, 1)
	secondStop := make(chan error, 1)
	go func() { firstStop <- runtime.Shutdown(shutdownContext) }()
	go func() { secondStop <- runtime.Shutdown(shutdownContext) }()
	awaitValue(t, runCanceled)
	assertProbeStatus(t, probes.Liveness(), http.StatusOK)
	assertProbeStatus(t, probes.Readiness(), http.StatusServiceUnavailable)
	close(releaseReadiness)
	if status := awaitValue(t, admitted); status != http.StatusOK {
		t.Fatalf("admitted readiness status = %d, want 200", status)
	}
	if err = awaitValue(t, firstStop); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err = awaitValue(t, secondStop); err != nil {
		t.Fatalf("repeated Shutdown() error = %v", err)
	}
}

func TestHealthProbesRaceWorkerBackendFailure(t *testing.T) {
	backendErr := errors.New("backend unavailable")
	readinessEntered := make(chan struct{}, 1)
	releaseReadiness := make(chan struct{})
	runEntered := make(chan struct{})
	failBackend := make(chan struct{})
	var blockReadiness atomic.Bool
	worker, err := NewLifecycleWorker(LifecycleWorkerOptions[int]{
		Name: "worker", Resource: 1, Correlation: mustFactory(t),
		Handler: func(context.Context, core.TaskMessage) error { return nil },
		Readiness: func(context.Context, int) error {
			if blockReadiness.Load() {
				readinessEntered <- struct{}{}
				<-releaseReadiness
			}

			return nil
		},
		Run: func(context.Context, int, Handler) error {
			close(runEntered)
			<-failBackend

			return backendErr
		},
		Shutdown: func(context.Context, int) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewLifecycleWorker() error = %v", err)
	}
	runtime, probes := startHealthRuntime(t, worker)
	awaitValue(t, runEntered)
	blockReadiness.Store(true)
	admitted := make(chan int, 1)
	go func() { admitted <- probeStatus(probes.Readiness()) }()
	awaitValue(t, readinessEntered)
	close(failBackend)
	select {
	case <-runtime.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("backend failure did not cancel the service")
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), time.Second)
	defer cancelShutdown()
	stopped := make(chan error, 1)
	go func() { stopped <- runtime.Shutdown(shutdownContext) }()
	assertProbeStatus(t, probes.Liveness(), http.StatusOK)
	assertProbeStatus(t, probes.Readiness(), http.StatusServiceUnavailable)
	close(releaseReadiness)
	if status := awaitValue(t, admitted); status != http.StatusOK {
		t.Fatalf("admitted readiness status = %d, want 200", status)
	}
	if err = awaitValue(t, stopped); !errors.Is(err, backendErr) {
		t.Fatalf("Shutdown() error = %v, want backend failure", err)
	}
}

func startHealthRuntime(
	t *testing.T,
	worker *LifecycleWorker[int],
) (*service.Service, *healthhttp.Probes) {
	t.Helper()
	plan := worker.Plan()
	runtime, err := service.New(service.Config{Components: plan.Components})
	if err != nil {
		t.Fatalf("service.New() error = %v", err)
	}
	if err = runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err = runtime.Go(plan.Tasks[0].Name, plan.Tasks[0].Run); err != nil {
		t.Fatalf("Go() error = %v", err)
	}
	probes, err := healthhttp.New(healthhttp.Config{
		Lifecycle: runtime,
		Checks: []healthhttp.Check{{
			Name: "worker",
			Run:  plan.Readiness[0].Run,
		}},
	})
	if err != nil {
		t.Fatalf("healthhttp.New() error = %v", err)
	}
	assertProbeStatus(t, probes.Liveness(), http.StatusOK)
	assertProbeStatus(t, probes.Readiness(), http.StatusOK)

	return runtime, probes
}

func assertProbeStatus(t *testing.T, handler http.Handler, wanted int) {
	t.Helper()
	if status := probeStatus(handler); status != wanted {
		t.Fatalf("probe status = %d, want %d", status, wanted)
	}
}

func probeStatus(handler http.Handler) int {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))

	return response.Code
}
