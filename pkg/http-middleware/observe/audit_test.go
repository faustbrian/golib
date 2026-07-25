package observe_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/observe"
)

func TestObserverSlownessIsSynchronousAndCreatesNoWorker(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	release := make(chan struct{})
	middleware, _ := observe.New(observe.Policy{Observer: func(context.Context, observe.Event) {
		close(entered)
		<-release
	}})
	done := make(chan struct{})
	go func() {
		middleware(http.NotFoundHandler()).ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "/", nil),
		)
		close(done)
	}()
	<-entered
	select {
	case <-done:
		t.Fatal("request returned before synchronous observer")
	default:
	}
	close(release)
	<-done
}

func TestBoundedCallerRecursionDoesNotDuplicateAnEvent(t *testing.T) {
	t.Parallel()
	var handler http.Handler
	var events atomic.Int64
	middleware, _ := observe.New(observe.Policy{Observer: func(context.Context, observe.Event) {
		if events.Add(1) == 1 {
			handler.ServeHTTP(
				httptest.NewRecorder(),
				httptest.NewRequest(http.MethodGet, "/nested", nil),
			)
		}
	}})
	handler = middleware(http.NotFoundHandler())
	handler.ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/outer", nil),
	)
	if events.Load() != 2 {
		t.Fatalf("events = %d", events.Load())
	}
}

func TestEventSchemaAndAllocationBudgets(t *testing.T) {
	typeOfEvent := reflect.TypeOf(observe.Event{})
	want := []string{
		"Method", "Route", "Status", "Bytes", "Duration", "Proto",
		"Outcome", "ClientClass",
	}
	if typeOfEvent.NumField() != len(want) {
		t.Fatalf("event fields = %d, want %d", typeOfEvent.NumField(), len(want))
	}
	for index, name := range want {
		if typeOfEvent.Field(index).Name != name {
			t.Fatalf("event field %d = %s, want %s", index, typeOfEvent.Field(index).Name, name)
		}
	}

	middleware, _ := observe.New(observe.Policy{Observer: func(context.Context, observe.Event) {}})
	handler := middleware(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, "/private?token=secret", nil)
	writer := &discardWriter{header: make(http.Header)}
	allocations := testing.AllocsPerRun(1000, func() {
		handler.ServeHTTP(writer, request)
	})
	if allocations > 18 {
		t.Fatalf("allocations = %.1f, budget = 18", allocations)
	}
}

func TestRouteLocalMiddlewareCanRecordMatchedRoute(t *testing.T) {
	t.Parallel()
	if observe.RecordRoute(nil, "route") || observe.RecordRoute(httptest.NewRequest(http.MethodGet, "/", nil), "route") {
		t.Fatal("route recorded without observation context")
	}
	var event observe.Event
	middleware, _ := observe.New(observe.Policy{Observer: func(_ context.Context, value observe.Event) {
		event = value
	}})
	middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !observe.RecordRoute(r, "users."+string(make([]byte, 256))) {
			t.Fatal("route was not recorded")
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/private", nil))
	if len(event.Route) != 128 || event.Route[:6] != "users." {
		t.Fatalf("route = %q", event.Route)
	}
}

type discardWriter struct{ header http.Header }

func (w *discardWriter) Header() http.Header             { return w.header }
func (*discardWriter) Write(payload []byte) (int, error) { return len(payload), nil }
func (*discardWriter) WriteHeader(int)                   {}
