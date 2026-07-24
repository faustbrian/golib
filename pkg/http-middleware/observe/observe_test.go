package observe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/http-middleware/observe"
)

func TestObserverReceivesOneBoundedCompletionEvent(t *testing.T) {
	t.Parallel()

	start := time.Unix(100, 0)
	times := []time.Time{start, start.Add(25 * time.Millisecond)}
	var event observe.Event
	middleware, err := observe.New(observe.Policy{
		Now:      func() time.Time { got := times[0]; times = times[1:]; return got },
		Route:    func(*http.Request) string { return "orders.show" },
		Observer: func(_ context.Context, got observe.Event) { event = got },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/secret/123?token=secret", nil)
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	})).ServeHTTP(recorder, req)

	if event.Method != http.MethodPost || event.Route != "orders.show" || event.Status != http.StatusCreated || event.Bytes != 2 {
		t.Fatalf("event = %#v", event)
	}
	if event.Duration != 25*time.Millisecond || event.Outcome != observe.Success || event.Proto != "HTTP/1.1" {
		t.Fatalf("event = %#v", event)
	}
}

func TestObserverPanicIsContainedByDefault(t *testing.T) {
	t.Parallel()

	middleware, _ := observe.New(observe.Policy{Observer: func(context.Context, observe.Event) { panic("observer") }})
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestObserverRunsWhenHandlerPanicsAndPanicContinues(t *testing.T) {
	t.Parallel()

	called := false
	middleware, _ := observe.New(observe.Policy{Observer: func(_ context.Context, event observe.Event) { called = event.Outcome == observe.Panicked }})
	defer func() {
		if recovered := recover(); recovered != "handler" || !called {
			t.Fatalf("panic = %v, called = %v", recovered, called)
		}
	}()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("handler") })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestObserverIncludesOnlyBoundedInjectedClientClass(t *testing.T) {
	t.Parallel()

	var event observe.Event
	middleware, _ := observe.New(observe.Policy{Observer: func(_ context.Context, got observe.Event) { event = got }, ClientClass: func(*http.Request) string { return strings.Repeat("trusted-proxy", 20) }})
	middleware(http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/raw/private", nil))
	if len(event.ClientClass) != 64 {
		t.Fatalf("client class length = %d", len(event.ClientClass))
	}
}
