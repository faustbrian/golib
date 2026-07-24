// Package bodylimit applies a byte limit before application decoding.
package bodylimit

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/faustbrian/golib/pkg/http-middleware/internal/httpx"
)

// ErrInvalidPolicy identifies invalid request body limit configuration.
var ErrInvalidPolicy = errors.New("bodylimit: invalid policy")

// ConfigError reports an invalid request body limit policy field.
type ConfigError struct{ Field string }

func (e *ConfigError) Error() string { return fmt.Sprintf("bodylimit: invalid %s", e.Field) }
func (e *ConfigError) Unwrap() error { return ErrInvalidPolicy }

// Policy limits encoded transport bytes. It does not decompress or parse a body.
type Policy struct{ MaxBytes int64 }

// New constructs body limiting middleware. Streaming overflow is reported to
// the application as *http.MaxBytesError. The server retains close ownership.
func New(policy Policy) (func(http.Handler) http.Handler, error) {
	if policy.MaxBytes < 1 {
		return nil, &ConfigError{Field: "maximum bytes"}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > policy.MaxBytes {
				r.Close = true
				httpx.SafeError(w, http.StatusRequestEntityTooLarge, "request body too large\n")
				return
			}
			trackedWriter, response := httpx.Track(w)
			var overflow atomic.Bool
			if r.Body != nil {
				limited := http.MaxBytesReader(trackedWriter, r.Body, policy.MaxBytes)
				r.Body = &overflowBody{Reader: limited, close: limited.Close, overflow: &overflow, request: r}
			}
			next.ServeHTTP(trackedWriter, r)
			if overflow.Load() && !response.Committed {
				httpx.SafeError(trackedWriter, http.StatusRequestEntityTooLarge, "request body too large\n")
			}
		})
	}, nil
}

type overflowBody struct {
	io.Reader
	close    func() error
	overflow *atomic.Bool
	request  *http.Request
}

func (b *overflowBody) Read(payload []byte) (int, error) {
	read, err := b.Reader.Read(payload)
	var maximum *http.MaxBytesError
	if errors.As(err, &maximum) {
		b.overflow.Store(true)
		b.request.Close = true
	}
	return read, err
}
func (b *overflowBody) Close() error { return b.close() }
