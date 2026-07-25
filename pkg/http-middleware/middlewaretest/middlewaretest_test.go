package middlewaretest_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/middlewaretest"
)

func TestTraceRecordsRequestAndResponseOrderSafely(t *testing.T) {
	t.Parallel()

	trace := middlewaretest.NewTrace()
	handler := trace.Middleware("outer")(trace.Middleware("inner")(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { trace.Record("handler") })))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	want := []string{"outer:request", "inner:request", "handler", "inner:response", "outer:response"}
	if got := trace.Events(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Events() = %v", got)
	}
	got := trace.Events()
	got[0] = "mutated"
	if trace.Events()[0] != "outer:request" {
		t.Fatal("Events returned mutable storage")
	}
}

func TestCapabilitiesReportsOnlyImplementedInterfaces(t *testing.T) {
	t.Parallel()

	capabilities := middlewaretest.CapabilitiesOf(&plainWriter{header: make(http.Header)})
	if capabilities.Flusher || capabilities.Hijacker || capabilities.Pusher || capabilities.ReaderFrom {
		t.Fatalf("capabilities = %#v", capabilities)
	}
}
