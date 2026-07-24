package recovery_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/recovery"
)

func TestRecoveryWritesSafeResponseAndObservesBoundedClass(t *testing.T) {
	t.Parallel()

	var event recovery.Event
	middleware, err := recovery.New(recovery.Policy{Observer: func(got recovery.Event) { event = got }})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("secret panic value")
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusInternalServerError || recorder.Body.String() != "internal server error\n" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
	if event.Class != recovery.ApplicationPanic || event.Committed {
		t.Fatalf("event = %#v", event)
	}
}

func TestRecoveryClearsPreparedHeadersBeforeSafeResponse(t *testing.T) {
	t.Parallel()

	middleware, _ := recovery.New(recovery.Policy{})
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("X-Secret", "prepared"); panic("boom") })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Header().Get("X-Secret") != "" {
		t.Fatalf("headers = %v", recorder.Header())
	}
}

func TestRecoveryDoesNotRewriteCommittedResponse(t *testing.T) {
	t.Parallel()

	var event recovery.Event
	middleware, _ := recovery.New(recovery.Policy{Observer: func(got recovery.Event) { event = got }})
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("partial"))
		panic(123)
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusAccepted || recorder.Body.String() != "partial" || !event.Committed {
		t.Fatalf("response = %d %q, event = %#v", recorder.Code, recorder.Body.String(), event)
	}
}

func TestRecoveryRepanicsAbortHandler(t *testing.T) {
	t.Parallel()

	middleware, _ := recovery.New(recovery.Policy{})
	defer func() {
		if recovered := recover(); !errors.Is(recovered.(error), http.ErrAbortHandler) {
			t.Fatalf("panic = %v", recovered)
		}
	}()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestRecoveryCapturesBoundedCallerOnlyStack(t *testing.T) {
	t.Parallel()

	var event recovery.Event
	middleware, err := recovery.New(recovery.Policy{Observer: func(got recovery.Event) { event = got }, CaptureStack: true, MaxStackBytes: 64})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("secret panic value") })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if len(event.Stack) == 0 || len(event.Stack) > 64 {
		t.Fatalf("stack length = %d", len(event.Stack))
	}
	if strings.Contains(string(event.Stack), "secret panic value") {
		t.Fatal("stack exposed panic value")
	}
}
