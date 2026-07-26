package middlewaretest_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	middleware "github.com/faustbrian/golib/pkg/http-middleware"
	"github.com/faustbrian/golib/pkg/http-middleware/bodylimit"
	compressmw "github.com/faustbrian/golib/pkg/http-middleware/compress"
	"github.com/faustbrian/golib/pkg/http-middleware/cors"
	"github.com/faustbrian/golib/pkg/http-middleware/deadline"
	"github.com/faustbrian/golib/pkg/http-middleware/observe"
	"github.com/faustbrian/golib/pkg/http-middleware/recovery"
	"github.com/faustbrian/golib/pkg/http-middleware/responsepolicy"
	"github.com/faustbrian/golib/pkg/http-middleware/secureheader"
)

func TestTrackingWrappersPreserveExactOptionalInterfaces(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		middleware func(http.Handler) http.Handler
	}{
		{name: "observe", middleware: mustObserve(t)},
		{name: "recovery", middleware: mustRecovery(t)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			underlying := &allWriter{ResponseRecorder: httptest.NewRecorder()}
			called := false
			tc.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				if _, ok := w.(http.Flusher); !ok {
					t.Error("Flusher missing")
				}
				if _, ok := w.(http.Hijacker); !ok {
					t.Error("Hijacker missing")
				}
				if _, ok := w.(http.Pusher); !ok {
					t.Error("Pusher missing")
				}
				if _, ok := w.(io.ReaderFrom); !ok {
					t.Error("ReaderFrom missing")
				}
			})).ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))
			if !called {
				t.Fatal("handler not called")
			}

			plain := &plainWriter{header: make(http.Header)}
			tc.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if _, ok := w.(http.Flusher); ok {
					t.Error("unexpected Flusher")
				}
				if _, ok := w.(http.Hijacker); ok {
					t.Error("unexpected Hijacker")
				}
				if _, ok := w.(http.Pusher); ok {
					t.Error("unexpected Pusher")
				}
				if _, ok := w.(io.ReaderFrom); ok {
					t.Error("unexpected ReaderFrom")
				}
			})).ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/", nil))
		})
	}
}

func TestNestedTransparentWrappersPreserveResponseControllerCapabilities(t *testing.T) {
	t.Parallel()
	observer := mustObserve(t)
	recoverer := mustRecovery(t)
	crossOrigin, _ := cors.New(cors.Policy{})
	headers, _ := secureheader.New(secureheader.APIDefaults())
	limit, _ := bodylimit.New(bodylimit.Policy{MaxBytes: 64})
	chain, err := middleware.New(recoverer, observer, crossOrigin, headers, responsepolicy.NoStore(), limit)
	if err != nil {
		t.Fatal(err)
	}
	underlying := &controllerWriter{allWriter: &allWriter{ResponseRecorder: httptest.NewRecorder()}}
	handler, err := chain.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		controller := http.NewResponseController(w)
		if err := controller.SetReadDeadline(time.Unix(1, 0)); err != nil {
			t.Errorf("SetReadDeadline() error = %v", err)
		}
		if err := controller.SetWriteDeadline(time.Unix(2, 0)); err != nil {
			t.Errorf("SetWriteDeadline() error = %v", err)
		}
		if err := controller.EnableFullDuplex(); err != nil {
			t.Errorf("EnableFullDuplex() error = %v", err)
		}
	}))
	if err != nil {
		t.Fatal(err)
	}
	handler.ServeHTTP(underlying, httptest.NewRequest(http.MethodGet, "/", nil))
	if !underlying.readDeadline || !underlying.writeDeadline || !underlying.fullDuplex {
		t.Fatalf("controller calls = read:%v write:%v duplex:%v", underlying.readDeadline, underlying.writeDeadline, underlying.fullDuplex)
	}
}

func TestBufferedWrappersWithholdUnsupportedCapabilities(t *testing.T) {
	t.Parallel()
	compressor, _ := compressmw.New(compressmw.Policy{MinimumBytes: 1})
	timeout, _ := deadline.NewTimeout(deadline.TimeoutPolicy{
		Timeout: time.Second, MaxResponseBytes: 64,
	})
	for _, tc := range []struct {
		name       string
		middleware func(http.Handler) http.Handler
	}{
		{name: "compression", middleware: compressor},
		{name: "timeout", middleware: timeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			called := false
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Header.Set("Accept-Encoding", "gzip")
			tc.middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				if _, ok := w.(http.Flusher); ok {
					t.Error("unexpected Flusher")
				}
				if _, ok := w.(http.Hijacker); ok {
					t.Error("unexpected Hijacker")
				}
				if _, ok := w.(http.Pusher); ok {
					t.Error("unexpected Pusher")
				}
				if _, ok := w.(io.ReaderFrom); ok {
					t.Error("unexpected ReaderFrom")
				}
				controller := http.NewResponseController(w)
				if err := controller.Flush(); !errors.Is(err, http.ErrNotSupported) {
					t.Errorf("Flush() error = %v", err)
				}
				if err := controller.EnableFullDuplex(); !errors.Is(err, http.ErrNotSupported) {
					t.Errorf("EnableFullDuplex() error = %v", err)
				}
				_, _ = io.WriteString(w, "body")
			})).ServeHTTP(recorder, request)
			if !called {
				t.Fatal("handler not called")
			}
		})
	}
}

type plainWriter struct{ header http.Header }

func (w *plainWriter) Header() http.Header             { return w.header }
func (*plainWriter) Write(payload []byte) (int, error) { return len(payload), nil }
func (*plainWriter) WriteHeader(int)                   {}

type allWriter struct{ *httptest.ResponseRecorder }

func (w *allWriter) Flush() { w.ResponseRecorder.Flush() }
func (*allWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, http.ErrNotSupported
}
func (*allWriter) Push(string, *http.PushOptions) error { return http.ErrNotSupported }
func (w *allWriter) ReadFrom(reader io.Reader) (int64, error) {
	return io.Copy(w.ResponseRecorder, reader)
}

type controllerWriter struct {
	*allWriter
	readDeadline, writeDeadline, fullDuplex bool
}

func (w *controllerWriter) SetReadDeadline(time.Time) error {
	w.readDeadline = true
	return nil
}

func (w *controllerWriter) SetWriteDeadline(time.Time) error {
	w.writeDeadline = true
	return nil
}

func (w *controllerWriter) EnableFullDuplex() error {
	w.fullDuplex = true
	return nil
}

func mustObserve(t *testing.T) func(http.Handler) http.Handler {
	t.Helper()
	result, err := observe.New(observe.Policy{Observer: func(context.Context, observe.Event) {}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func mustRecovery(t *testing.T) func(http.Handler) http.Handler {
	t.Helper()
	result, err := recovery.New(recovery.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
