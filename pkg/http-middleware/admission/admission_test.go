package admission_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/http-middleware/admission"
)

func TestImmediateAdmissionRejectsAboveLimitAndReleasesPermit(t *testing.T) {
	t.Parallel()

	middleware, err := admission.New(admission.Policy{MaxInFlight: 1})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		close(done)
	}()
	<-entered
	overloaded := httptest.NewRecorder()
	handler.ServeHTTP(overloaded, httptest.NewRequest(http.MethodGet, "/", nil))
	if overloaded.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", overloaded.Code)
	}
	close(release)
	<-done
}

func TestBoundedWaitHonorsCancellationWithoutLeakingPermit(t *testing.T) {
	t.Parallel()

	middleware, _ := admission.New(admission.Policy{MaxInFlight: 1, MaxWaiters: 1, Wait: time.Second})
	block := make(chan struct{})
	entered := make(chan struct{})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-block
		w.WriteHeader(http.StatusNoContent)
	}))
	go handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))
	if recorder.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d", recorder.Code)
	}
	close(block)
}

func TestShutdownRejectsNewAndWaitingAdmissions(t *testing.T) {
	t.Parallel()

	shutdown := make(chan struct{})
	close(shutdown)
	middleware, _ := admission.New(admission.Policy{MaxInFlight: 1, MaxWaiters: 1, Wait: time.Second, Shutdown: shutdown})
	recorder := httptest.NewRecorder()
	middleware(http.NotFoundHandler()).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", recorder.Code)
	}
}
