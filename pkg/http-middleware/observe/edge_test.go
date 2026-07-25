package observe

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConfigurationAndOutcomeTruthTable(t *testing.T) {
	t.Parallel()
	_, err := New(Policy{})
	var configuration *ConfigError
	if !errors.As(err, &configuration) || !errors.Is(err, ErrInvalidPolicy) || configuration.Error() == "" {
		t.Fatalf("New() error = %v", err)
	}
	for _, tc := range []struct {
		status   int
		canceled bool
		want     Outcome
	}{{200, false, Success}, {399, false, Success}, {400, false, ClientError}, {499, false, ClientError}, {500, false, ServerError}, {200, true, Canceled}} {
		ctx := context.Background()
		if tc.canceled {
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(ctx)
			cancel()
		}
		if got := outcome(ctx, tc.status); got != tc.want {
			t.Fatalf("outcome(%d, %v) = %s", tc.status, tc.canceled, got)
		}
	}
}

func TestDefaultClockImplicitStatusAndBoundedMetadata(t *testing.T) {
	t.Parallel()
	var event Event
	middleware, err := New(Policy{Observer: func(_ context.Context, value Event) { event = value }, Route: func(*http.Request) string { return string(make([]byte, 129)) }, ClientClass: func(*http.Request) string { return string(make([]byte, 65)) }})
	if err != nil {
		t.Fatal(err)
	}
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if event.Status != http.StatusOK || event.Duration < 0 || len(event.Route) != 128 || len(event.ClientClass) != 64 {
		t.Fatalf("event = %+v", event)
	}
}

func TestUncommonMethodAndProtocolUseBoundedClasses(t *testing.T) {
	t.Parallel()
	var event Event
	middleware, err := New(Policy{Observer: func(_ context.Context, value Event) { event = value }})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("TENANT-"+strings.Repeat("A", 1024), "/", nil)
	request.Proto = "HTTP/" + strings.Repeat("9", 1024)
	middleware(http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), request)
	if event.Method != "OTHER" || event.Proto != "OTHER" {
		t.Fatalf("event = %+v", event)
	}
}

func TestBackwardInjectedClockClampsDuration(t *testing.T) {
	t.Parallel()
	times := []time.Time{time.Unix(2, 0), time.Unix(1, 0)}
	var event Event
	middleware, err := New(Policy{
		Now: func() time.Time {
			value := times[0]
			times = times[1:]
			return value
		},
		Observer: func(_ context.Context, value Event) { event = value },
	})
	if err != nil {
		t.Fatal(err)
	}
	middleware(http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if event.Duration != 0 {
		t.Fatalf("duration = %s", event.Duration)
	}
}

func TestRouteMetadataIsExtractedAfterRouting(t *testing.T) {
	t.Parallel()
	var event Event
	middleware, err := New(Policy{
		Route:    func(request *http.Request) string { return request.Pattern },
		Observer: func(_ context.Context, value Event) { event = value },
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /users/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	middleware(mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users/42", nil))
	if event.Route != "GET /users/{id}" {
		t.Fatalf("route = %q", event.Route)
	}
}

func TestMetadataExtractorPanicIsContained(t *testing.T) {
	t.Parallel()
	var event Event
	middleware, err := New(Policy{
		Route:       func(*http.Request) string { panic("route") },
		ClientClass: func(*http.Request) string { panic("client") },
		Observer:    func(_ context.Context, value Event) { event = value },
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNoContent || event.Route != "" || event.ClientClass != "" {
		t.Fatalf("response = %d, event = %+v", recorder.Code, event)
	}
}

func TestObserverPanicPolicy(t *testing.T) {
	t.Parallel()
	middleware, err := New(Policy{Observer: func(context.Context, Event) { panic("observer") }, Now: func() time.Time { return time.Unix(0, 0) }, RepanicObserver: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("observer panic was not propagated")
		}
	}()
	middleware(http.NotFoundHandler()).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
