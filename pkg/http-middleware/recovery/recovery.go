// Package recovery contains application panics below an explicit boundary.
package recovery

import (
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"runtime/debug"

	"github.com/faustbrian/golib/pkg/http-middleware/internal/httpx"
)

// Class is deliberately bounded and never includes the panic value.
type Class string

const (
	// ApplicationPanic classifies non-runtime application panics.
	ApplicationPanic Class = "application_panic"
	// RuntimePanic classifies values implementing runtime.Error.
	RuntimePanic Class = "runtime_panic"
)

// Event reports safe panic classification and response ownership state.
type Event struct {
	Class     Class
	Committed bool
	Stack     []byte
}

// Policy configures recovery observation. Observer panics are contained.
type Policy struct {
	Observer      func(Event)
	CaptureStack  bool
	MaxStackBytes int
}

// ErrInvalidPolicy identifies invalid recovery policy configuration.
var ErrInvalidPolicy = errors.New("recovery: invalid policy")

// ConfigError reports an invalid recovery policy field.
type ConfigError struct{ Field string }

func (e *ConfigError) Error() string { return fmt.Sprintf("recovery: invalid %s", e.Field) }
func (e *ConfigError) Unwrap() error { return ErrInvalidPolicy }

// New constructs recovery middleware. http.ErrAbortHandler is re-panicked so
// normal connection abort behavior remains owned by net/http.
func New(policy Policy) (func(http.Handler) http.Handler, error) {
	if policy.CaptureStack && policy.MaxStackBytes == 0 {
		policy.MaxStackBytes = 64 << 10
	}
	if policy.MaxStackBytes < 0 || policy.MaxStackBytes > 1<<20 || (!policy.CaptureStack && policy.MaxStackBytes != 0) {
		return nil, &ConfigError{Field: "stack limit"}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			trackedWriter, recorder := httpx.Track(w)
			defer func() {
				panicValue := recover()
				if panicValue == nil {
					return
				}
				if panicErr, ok := panicValue.(error); ok && errors.Is(panicErr, http.ErrAbortHandler) {
					panic(panicValue)
				}
				class := ApplicationPanic
				if _, ok := panicValue.(runtime.Error); ok {
					class = RuntimePanic
				}
				event := Event{Class: class, Committed: recorder.Committed}
				if policy.CaptureStack {
					stack := debug.Stack()
					if len(stack) > policy.MaxStackBytes {
						stack = stack[:policy.MaxStackBytes]
					}
					event.Stack = append([]byte(nil), stack...)
				}
				observe(policy.Observer, event)
				if !recorder.Committed {
					for name := range w.Header() {
						w.Header().Del(name)
					}
					httpx.SafeError(trackedWriter, http.StatusInternalServerError, "internal server error\n")
				}
			}()
			next.ServeHTTP(trackedWriter, r)
		})
	}, nil
}

func observe(observer func(Event), event Event) {
	if observer == nil {
		return
	}
	defer func() { _ = recover() }()
	observer(event)
}
