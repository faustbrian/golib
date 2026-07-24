package deadline_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/http-middleware/deadline"
)

func TestDeadlineNeverExtendsParent(t *testing.T) {
	t.Parallel()

	parentDeadline := time.Now().Add(20 * time.Millisecond)
	ctx, cancel := context.WithDeadline(context.Background(), parentDeadline)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	middleware, err := deadline.New(deadline.Policy{Timeout: time.Hour})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok := r.Context().Deadline()
		if !ok || !got.Equal(parentDeadline) {
			t.Fatalf("deadline = %v, %v; want %v", got, ok, parentDeadline)
		}
	})).ServeHTTP(httptest.NewRecorder(), req)
}

func TestBufferedTimeoutReturnsSafeBoundedResponse(t *testing.T) {
	t.Parallel()

	middleware, err := deadline.NewTimeout(deadline.TimeoutPolicy{Timeout: time.Millisecond, MaxResponseBytes: 1024})
	if err != nil {
		t.Fatalf("NewTimeout() error = %v", err)
	}
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusServiceUnavailable || recorder.Body.String() != "handler timeout\n" {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestBufferedTimeoutCommitsCompletedResponseAndBoundsOutput(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, payload string
		status        int
	}{
		{name: "success", payload: "ok", status: http.StatusCreated},
		{name: "overflow", payload: "toolarge", status: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			middleware, _ := deadline.NewTimeout(deadline.TimeoutPolicy{Timeout: time.Second, MaxResponseBytes: 4})
			recorder := httptest.NewRecorder()
			middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Test", "yes")
				w.WriteHeader(http.StatusCreated)
				_, err := io.WriteString(w, tc.payload)
				if tc.name == "overflow" && !errors.Is(err, deadline.ErrResponseTooLarge) {
					t.Errorf("write error = %v", err)
				}
			})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.status)
			}
			if tc.name == "success" && (recorder.Body.String() != "ok" || recorder.Header().Get("X-Test") != "yes") {
				t.Fatalf("response = %q %v", recorder.Body.String(), recorder.Header())
			}
			if tc.name == "overflow" && recorder.Header().Get("X-Test") != "" {
				t.Fatalf("overflow leaked headers: %v", recorder.Header())
			}
		})
	}
}

func TestDeadlineCancelsContextIgnoringNoCodeInterruptionPromise(t *testing.T) {
	t.Parallel()

	middleware, _ := deadline.New(deadline.Policy{Timeout: time.Millisecond})
	done := make(chan struct{})
	middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(done)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("deadline did not cancel request context")
	}
}
