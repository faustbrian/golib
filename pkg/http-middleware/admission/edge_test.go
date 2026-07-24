package admission

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConfigurationErrorsAndRetryHeader(t *testing.T) {
	t.Parallel()
	for _, policy := range []Policy{{}, {MaxInFlight: 1, MaxWaiters: -1}, {MaxInFlight: 1, Wait: -1}, {MaxInFlight: 1, Wait: time.Minute + time.Nanosecond}, {MaxInFlight: 1, RetryAfterSeconds: -1}, {MaxInFlight: 1_000_001}} {
		_, err := New(policy)
		var config *ConfigError
		if !errors.As(err, &config) || !errors.Is(err, ErrInvalidPolicy) || !strings.Contains(err.Error(), "limit") {
			t.Fatalf("New(%#v) error = %v", policy, err)
		}
	}
	middleware, _ := New(Policy{MaxInFlight: 1, RetryAfterSeconds: 3})
	block := make(chan struct{})
	entered := make(chan struct{})
	handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-block }))
	go handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	<-entered
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	close(block)
	if recorder.Header().Get("Retry-After") != "3" {
		t.Fatalf("headers = %v", recorder.Header())
	}
}

func TestCanceledRequestNeverConsumesFreePermit(t *testing.T) {
	t.Parallel()
	middleware, err := New(Policy{MaxInFlight: 1})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(recorder, request)
	if called || recorder.Code != http.StatusRequestTimeout {
		t.Fatalf("called = %v, status = %d", called, recorder.Code)
	}
}

func TestWaitTimeoutAndWaiterBound(t *testing.T) {
	t.Parallel()
	middleware, _ := New(Policy{MaxInFlight: 1, MaxWaiters: 1, Wait: time.Millisecond})
	block := make(chan struct{})
	entered := make(chan struct{})
	handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-block
	}))
	go handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	<-entered
	for range 2 {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if recorder.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d", recorder.Code)
		}
	}
	close(block)
}

func TestWaitingRequestAcquiresReleasedPermit(t *testing.T) {
	t.Parallel()
	shutdown := make(chan struct{})
	middleware, err := New(Policy{MaxInFlight: 1, MaxWaiters: 1, Wait: time.Second, Shutdown: shutdown})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}, 1), make(chan struct{})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	done := make(chan int, 2)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		done <- recorder.Code
	}()
	<-entered
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		done <- recorder.Code
	}()
	time.Sleep(10 * time.Millisecond)
	release <- struct{}{}
	if got := <-done; got != http.StatusNoContent {
		t.Fatalf("first status = %d", got)
	}
	<-entered
	release <- struct{}{}
	if got := <-done; got != http.StatusNoContent {
		t.Fatalf("second status = %d", got)
	}
	if stopped(nil) || stopped(shutdown) {
		t.Fatal("open shutdown reported stopped")
	}
	close(shutdown)
	if !stopped(shutdown) {
		t.Fatal("closed shutdown not reported stopped")
	}
}

func TestShutdownReleasesWaitingRequestAndQueueStaysBounded(t *testing.T) {
	t.Parallel()
	shutdown := make(chan struct{})
	middleware, err := New(Policy{MaxInFlight: 1, MaxWaiters: 1, Wait: time.Second, Shutdown: shutdown})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-release }))
	firstDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		close(firstDone)
	}()
	<-entered
	waitingDone := make(chan int, 1)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		waitingDone <- recorder.Code
	}()
	time.Sleep(10 * time.Millisecond)
	overflow := httptest.NewRecorder()
	handler.ServeHTTP(overflow, httptest.NewRequest(http.MethodGet, "/", nil))
	if overflow.Code != http.StatusServiceUnavailable {
		t.Fatalf("overflow status = %d", overflow.Code)
	}
	close(shutdown)
	if got := <-waitingDone; got != http.StatusServiceUnavailable {
		t.Fatalf("shutdown waiter status = %d", got)
	}
	close(release)
	<-firstDone
}
