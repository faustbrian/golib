package router_test

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	router "github.com/faustbrian/golib/pkg/router"
)

func TestRouterPreservesResponseWriterOptionalInterfaces(t *testing.T) {
	t.Parallel()

	writer := &optionalWriter{header: make(http.Header)}
	builder := router.New(router.WithMiddleware(router.NamedMiddleware{
		Name: "transport",
		Middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(actual http.ResponseWriter, request *http.Request) {
				actual.(http.Flusher).Flush()
				_, _, _ = actual.(http.Hijacker).Hijack()
				_ = actual.(http.Pusher).Push("/asset", nil)
				next.ServeHTTP(actual, request)
			})
		},
	}))
	mustRegister(t, builder, router.Route{
		Methods: []string{"GET"}, Path: "/",
		Handler: http.HandlerFunc(func(actual http.ResponseWriter, _ *http.Request) {
			if actual != writer {
				t.Error("router replaced the response writer")
			}
			if _, ok := actual.(http.Flusher); !ok {
				t.Error("Flusher was lost")
			}
			if _, ok := actual.(http.Hijacker); !ok {
				t.Error("Hijacker was lost")
			}
			if _, ok := actual.(http.Pusher); !ok {
				t.Error("Pusher was lost")
			}
			actual.WriteHeader(http.StatusNoContent)
		}),
	})
	mustCompile(t, builder).ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
	if writer.status != http.StatusNoContent {
		t.Fatalf("status: got %d", writer.status)
	}
	if writer.flushes != 1 || writer.hijacks != 1 || writer.pushes != 1 {
		t.Fatalf("optional calls: flush=%d hijack=%d push=%d", writer.flushes, writer.hijacks, writer.pushes)
	}
}

func TestMiddlewareMayShortCircuitPanicCancelAndReenter(t *testing.T) {
	t.Parallel()

	t.Run("short circuit", func(t *testing.T) {
		called := false
		builder := router.New(router.WithMiddleware(router.NamedMiddleware{
			Name: "stop", Middleware: func(http.Handler) http.Handler {
				return http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
					writer.WriteHeader(http.StatusUnauthorized)
				})
			},
		}))
		mustRegister(t, builder, router.Route{
			Methods: []string{"GET"}, Path: "/",
			Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }),
		})
		response := httptest.NewRecorder()
		mustCompile(t, builder).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
		if response.Code != http.StatusUnauthorized || called {
			t.Fatalf("short circuit: status=%d called=%v", response.Code, called)
		}
	})

	t.Run("panic propagates", func(t *testing.T) {
		builder := router.New()
		mustRegister(t, builder, router.Route{
			Methods: []string{"GET"}, Path: "/",
			Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("application") }),
		})
		compiled := mustCompile(t, builder)
		defer func() {
			if recover() == nil {
				t.Error("router recovered an application panic")
			}
		}()
		compiled.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	})

	t.Run("middleware panic propagates", func(t *testing.T) {
		called := false
		builder := router.New(router.WithMiddleware(router.NamedMiddleware{
			Name: "panic", Middleware: func(http.Handler) http.Handler {
				return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					panic("middleware")
				})
			},
		}))
		mustRegister(t, builder, router.Route{
			Methods: []string{http.MethodGet}, Path: "/",
			Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }),
		})
		compiled := mustCompile(t, builder)
		defer func() {
			if recovered := recover(); recovered != "middleware" || called {
				t.Errorf("middleware panic: recovered=%v handler-called=%t", recovered, called)
			}
		}()
		compiled.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	})

	t.Run("cancellation", func(t *testing.T) {
		middlewareObserved := false
		builder := router.New(router.WithMiddleware(router.NamedMiddleware{
			Name: "cancellation", Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					middlewareObserved = errors.Is(request.Context().Err(), context.Canceled)
					next.ServeHTTP(writer, request)
				})
			},
		}))
		mustRegister(t, builder, router.Route{
			Methods: []string{"GET"}, Path: "/",
			Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if !errors.Is(request.Context().Err(), context.Canceled) {
					t.Errorf("context error: %v", request.Context().Err())
				}
				writer.WriteHeader(http.StatusNoContent)
			}),
		})
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		ctx, cancel := context.WithCancel(request.Context())
		cancel()
		mustCompile(t, builder).ServeHTTP(httptest.NewRecorder(), request.WithContext(ctx))
		if !middlewareObserved {
			t.Fatal("middleware did not observe cancellation")
		}
	})

	t.Run("reentry", func(t *testing.T) {
		builder := router.New()
		var compiled *router.Router
		mustRegister(t, builder, router.Route{
			Methods: []string{"GET"}, Path: "/inner",
			Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(http.StatusNoContent)
			}),
		})
		mustRegister(t, builder, router.Route{
			Methods: []string{"GET"}, Path: "/outer",
			Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				inner := httptest.NewRecorder()
				compiled.ServeHTTP(inner, httptest.NewRequest(http.MethodGet, "/inner", nil))
				writer.WriteHeader(inner.Code)
			}),
		})
		compiled = mustCompile(t, builder)
		response := httptest.NewRecorder()
		compiled.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/outer", nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("reentrant status: %d", response.Code)
		}
	})
}

func TestMiddlewareConstructorPanicPropagatesAfterValidation(t *testing.T) {
	t.Parallel()

	builder := router.New(router.WithMiddleware(router.NamedMiddleware{
		Name: "panic",
		Middleware: func(http.Handler) http.Handler {
			panic("middleware construction")
		},
	}))
	mustRegister(t, builder, router.Route{
		Methods: []string{http.MethodGet}, Path: "/", Handler: http.NotFoundHandler(),
	})
	defer func() {
		if recovered := recover(); recovered != "middleware construction" {
			t.Errorf("middleware panic: got %v", recovered)
		}
	}()
	_, _ = builder.Compile()
}

type optionalWriter struct {
	header  http.Header
	status  int
	flushes int
	hijacks int
	pushes  int
}

func (writer *optionalWriter) Header() http.Header { return writer.header }
func (writer *optionalWriter) Write(value []byte) (int, error) {
	return len(value), nil
}
func (writer *optionalWriter) WriteHeader(status int) { writer.status = status }
func (writer *optionalWriter) Flush()                 { writer.flushes++ }
func (writer *optionalWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	writer.hijacks++
	return nil, nil, errors.New("not connected")
}
func (writer *optionalWriter) Push(string, *http.PushOptions) error {
	writer.pushes++
	return http.ErrNotSupported
}
