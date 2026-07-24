package recovery

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPolicyValidationAndRuntimeClassification(t *testing.T) {
	t.Parallel()
	for _, policy := range []Policy{{MaxStackBytes: 1}, {CaptureStack: true, MaxStackBytes: -1}, {CaptureStack: true, MaxStackBytes: 1<<20 + 1}} {
		_, err := New(policy)
		var configuration *ConfigError
		if !errors.As(err, &configuration) || !errors.Is(err, ErrInvalidPolicy) || configuration.Error() == "" {
			t.Fatalf("New(%+v) error = %v", policy, err)
		}
	}
	var event Event
	middleware, err := New(Policy{Observer: func(value Event) { event = value }})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(fakeRuntimeError{}) })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if event.Class != RuntimePanic || recorder.Code != http.StatusInternalServerError {
		t.Fatalf("event = %+v, status = %d", event, recorder.Code)
	}
}

type fakeRuntimeError struct{}

func (fakeRuntimeError) Error() string { return "bounded runtime panic" }
func (fakeRuntimeError) RuntimeError() {}

func TestNilAndPanickingObserversCannotCorruptRecovery(t *testing.T) {
	t.Parallel()
	for _, observer := range []func(Event){nil, func(Event) { panic("observer") }} {
		middleware, err := New(Policy{Observer: observer})
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("application") })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d", recorder.Code)
		}
	}
}

func TestNormalCompletionAndDefaultStackLimit(t *testing.T) {
	t.Parallel()
	middleware, err := New(Policy{CaptureStack: true})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d", recorder.Code)
	}
}
