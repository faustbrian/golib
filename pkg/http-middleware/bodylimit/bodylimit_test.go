package bodylimit_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faustbrian/golib/pkg/http-middleware/bodylimit"
)

func TestKnownOversizedBodyIsRejectedBeforeHandler(t *testing.T) {
	t.Parallel()

	middleware, err := bodylimit.New(bodylimit.Policy{MaxBytes: 4})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	called := false
	handler := middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345")))

	if called || recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("called = %v, status = %d", called, recorder.Code)
	}
}

func TestStreamingOverflowUsesStandardMaxBytesError(t *testing.T) {
	t.Parallel()

	middleware, _ := bodylimit.New(bodylimit.Policy{MaxBytes: 4})
	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		var limit *http.MaxBytesError
		if !errors.As(err, &limit) || limit.Limit != 4 {
			t.Fatalf("read error = %v", err)
		}
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345"))
	req.ContentLength = -1
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestIgnoredStreamingOverflowGetsSafeResponseBeforeCommit(t *testing.T) {
	t.Parallel()

	middleware, _ := bodylimit.New(bodylimit.Policy{MaxBytes: 4})
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345"))
	req.ContentLength = -1
	recorder := httptest.NewRecorder()
	middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { _, _ = io.ReadAll(r.Body) })).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusRequestEntityTooLarge || !req.Close {
		t.Fatalf("status = %d, close = %v", recorder.Code, req.Close)
	}
}

func TestBodyCloseRemainsOwnedByServer(t *testing.T) {
	t.Parallel()

	body := &trackedBody{Reader: strings.NewReader("123")}
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Body = body
	req.ContentLength = 3
	middleware, _ := bodylimit.New(bodylimit.Policy{MaxBytes: 4})
	middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(httptest.NewRecorder(), req)
	if body.closed {
		t.Fatal("middleware closed request body")
	}
}

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }
