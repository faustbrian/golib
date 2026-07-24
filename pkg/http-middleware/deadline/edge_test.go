package deadline

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestConfigurationErrorContractAndTimeoutBounds(t *testing.T) {
	t.Parallel()
	_, err := New(Policy{})
	var configuration *ConfigError
	if !errors.As(err, &configuration) || !errors.Is(err, ErrInvalidPolicy) || configuration.Error() == "" {
		t.Fatalf("New() error = %v", err)
	}
	for _, policy := range []TimeoutPolicy{{}, {Timeout: 24*time.Hour + time.Nanosecond, MaxResponseBytes: 1}, {Timeout: time.Second}, {Timeout: time.Second, MaxResponseBytes: 1, MaxConcurrent: -1}, {Timeout: time.Second, MaxResponseBytes: 1, MaxConcurrent: 65_537}, {Timeout: time.Second, MaxResponseBytes: 16<<20 + 1}, {Timeout: time.Second, MaxResponseBytes: 1, Status: 499}, {Timeout: time.Second, MaxResponseBytes: 1, Status: 600}} {
		if _, err := NewTimeout(policy); !errors.Is(err, ErrInvalidPolicy) {
			t.Fatalf("NewTimeout(%+v) error = %v", policy, err)
		}
	}
	if _, err := New(Policy{Timeout: 24*time.Hour + time.Nanosecond}); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("New(oversized timeout) error = %v", err)
	}
}

func TestTimeoutBoundsContextIgnoringHandlers(t *testing.T) {
	t.Parallel()
	middleware, err := NewTimeout(TimeoutPolicy{
		Timeout: time.Millisecond, MaxResponseBytes: 64, MaxConcurrent: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	exited := make(chan struct{})
	entered := make(chan struct{}, 1)
	handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		entered <- struct{}{}
		<-release
		close(exited)
	}))
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/", nil))
	if first.Code != http.StatusServiceUnavailable {
		t.Fatalf("first status = %d", first.Code)
	}
	<-entered
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/", nil))
	if second.Code != http.StatusServiceUnavailable || second.Body.String() != "handler timeout capacity exhausted\n" {
		t.Fatalf("second response = %d %q", second.Code, second.Body.String())
	}
	close(release)
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("retained handler did not exit")
	}
}

func TestTimeoutWriterStateMachine(t *testing.T) {
	t.Parallel()
	w := newTimeoutWriter(2)
	w.Header().Set("X-Test", "yes")
	w.WriteHeader(http.StatusEarlyHints)
	w.WriteHeader(http.StatusCreated)
	w.WriteHeader(http.StatusAccepted)
	if _, err := w.Write([]byte("abc")); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("overflow error = %v", err)
	}
	if _, err := w.Write([]byte("a")); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("repeat overflow error = %v", err)
	}
	header, status, payload, overflow := w.finish()
	if header.Get("X-Test") != "yes" || status != http.StatusCreated || len(payload) != 0 || !overflow {
		t.Fatalf("finish = %v, %d, %q, %v", header, status, payload, overflow)
	}
	if _, err := w.Write([]byte("a")); !errors.Is(err, ErrHandlerTimeout) {
		t.Fatalf("late write error = %v", err)
	}

	empty := newTimeoutWriter(1)
	_, status, _, _ = empty.finish()
	if status != http.StatusOK {
		t.Fatalf("empty status = %d", status)
	}
	implicit := newTimeoutWriter(1)
	if count, err := implicit.Write([]byte("a")); count != 1 || err != nil {
		t.Fatalf("write = %d, %v", count, err)
	}
}

func TestTimeoutWriterRejectsInvalidStatus(t *testing.T) {
	t.Parallel()
	for _, status := range []int{0, 99, 1000} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("WriteHeader did not panic")
				}
			}()
			newTimeoutWriter(1).WriteHeader(status)
		})
	}
}

func TestTimeoutMiddlewareReplaysInformationalAndFinalStatus(t *testing.T) {
	t.Parallel()
	middleware, err := NewTimeout(TimeoutPolicy{Timeout: time.Second, MaxResponseBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	writer := &statusWriter{header: make(http.Header)}
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", "</asset>; rel=preload")
		w.WriteHeader(http.StatusEarlyHints)
		w.Header().Del("Link")
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	if !reflect.DeepEqual(writer.statuses, []int{http.StatusEarlyHints, http.StatusNoContent}) {
		t.Fatalf("statuses = %v", writer.statuses)
	}
}

type statusWriter struct {
	header   http.Header
	statuses []int
}

func (w *statusWriter) Header() http.Header         { return w.header }
func (w *statusWriter) WriteHeader(status int)      { w.statuses = append(w.statuses, status) }
func (w *statusWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestTimeoutPropagatesPanicAndReplacesHeaders(t *testing.T) {
	t.Parallel()
	middleware, err := NewTimeout(TimeoutPolicy{Timeout: time.Second, MaxResponseBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	recorder.Header().Set("X-Old", "remove")
	middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("X-New", "keep") })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Header().Get("X-Old") != "" || recorder.Header().Get("X-New") != "keep" {
		t.Fatalf("headers = %v", recorder.Header())
	}

	defer func() {
		if recover() != "boom" {
			t.Fatal("handler panic not propagated")
		}
	}()
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
