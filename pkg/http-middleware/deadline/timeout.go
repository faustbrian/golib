package deadline

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/faustbrian/golib/pkg/http-middleware/internal/httpx"
)

var (
	// ErrHandlerTimeout is returned to late writes after the response timeout.
	ErrHandlerTimeout = errors.New("deadline: handler timeout")
	// ErrResponseTooLarge identifies output above the configured buffer bound.
	ErrResponseTooLarge = errors.New("deadline: response too large")
)

// TimeoutPolicy configures a bounded non-streaming handler timeout of at most
// 24 hours. MaxConcurrent defaults to 1024 and is capped at 65,536. A handler
// that ignores cancellation may continue executing and retain one concurrency
// slot, but cannot write through the closed buffer or create unbounded
// middleware-owned goroutine growth.
type TimeoutPolicy struct {
	Timeout          time.Duration
	MaxResponseBytes int
	MaxConcurrent    int
	Status           int
}

// NewTimeout constructs bounded buffered timeout middleware. The wrapped
// writer intentionally exposes no streaming, hijacking, push, or full-duplex
// capability because those operations are incompatible with response replay.
func NewTimeout(policy TimeoutPolicy) (func(http.Handler) http.Handler, error) {
	if policy.Status == 0 {
		policy.Status = http.StatusServiceUnavailable
	}
	if policy.MaxConcurrent == 0 {
		policy.MaxConcurrent = 1024
	}
	if policy.Timeout <= 0 || policy.Timeout > maximumTimeout || policy.MaxResponseBytes < 1 || policy.MaxResponseBytes > 16<<20 || policy.MaxConcurrent < 1 || policy.MaxConcurrent > 65_536 || policy.Status < 500 || policy.Status > 599 {
		return nil, &ConfigError{Field: "timeout response policy"}
	}
	workers := make(chan struct{}, policy.MaxConcurrent)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case workers <- struct{}{}:
			default:
				httpx.SafeError(w, policy.Status, "handler timeout capacity exhausted\n")
				return
			}
			var responseMu sync.Mutex
			ctx, cancel := context.WithTimeout(r.Context(), policy.Timeout)
			defer cancel()
			buffer := newTimeoutWriter(policy.MaxResponseBytes, func(status int, header http.Header) {
				responseMu.Lock()
				defer responseMu.Unlock()
				copyHeaders(w.Header(), header)
				w.WriteHeader(status)
			})
			completed := make(chan any, 1)
			go func() {
				var panicValue any
				defer func() {
					panicValue = recover()
					<-workers
					completed <- panicValue
				}()
				next.ServeHTTP(buffer, r.WithContext(ctx))
			}()
			select {
			case panicValue := <-completed:
				header, status, payload, overflow := buffer.finish()
				if panicValue != nil {
					panic(panicValue)
				}
				if overflow {
					responseMu.Lock()
					defer responseMu.Unlock()
					httpx.SafeError(w, http.StatusInternalServerError, "internal server error\n")
					return
				}
				responseMu.Lock()
				defer responseMu.Unlock()
				copyHeaders(w.Header(), header)
				w.WriteHeader(status)
				_, _ = w.Write(payload)
			case <-ctx.Done():
				buffer.timeout()
				responseMu.Lock()
				defer responseMu.Unlock()
				httpx.SafeError(w, policy.Status, "handler timeout\n")
			}
		})
	}, nil
}

type timeoutWriter struct {
	mu            sync.Mutex
	header        http.Header
	status        int
	payload       bytes.Buffer
	maximum       int
	closed        bool
	overflow      bool
	informational func(int, http.Header)
}

func newTimeoutWriter(maximum int, informational ...func(int, http.Header)) *timeoutWriter {
	writer := &timeoutWriter{header: make(http.Header), maximum: maximum}
	if len(informational) > 0 {
		writer.informational = informational[0]
	}
	return writer
}
func (w *timeoutWriter) Header() http.Header { return w.header }
func (w *timeoutWriter) WriteHeader(status int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	httpx.CheckWriteHeaderCode(status)
	if w.closed || w.status != 0 {
		return
	}
	if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
		if w.informational != nil {
			w.informational(status, w.header.Clone())
		}
		return
	}
	if w.status == 0 {
		w.status = status
	}
}
func (w *timeoutWriter) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, ErrHandlerTimeout
	}
	if w.overflow {
		return 0, ErrResponseTooLarge
	}
	if w.payload.Len()+len(payload) > w.maximum {
		w.overflow = true
		return 0, ErrResponseTooLarge
	}
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.payload.Write(payload)
}
func (w *timeoutWriter) timeout() { w.mu.Lock(); w.closed = true; w.mu.Unlock() }
func (w *timeoutWriter) finish() (http.Header, int, []byte, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	return w.header.Clone(), status, append([]byte(nil), w.payload.Bytes()...), w.overflow
}
func copyHeaders(destination, source http.Header) {
	for name := range destination {
		destination.Del(name)
	}
	for name, values := range source {
		destination[name] = append([]string(nil), values...)
	}
}
