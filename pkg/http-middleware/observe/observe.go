// Package observe emits one bounded transport completion event per request.
package observe

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/faustbrian/golib/pkg/http-middleware/internal/httpx"
)

// Outcome is a bounded completion classification.
type Outcome string

const (
	// Success classifies responses below 400.
	Success Outcome = "success"
	// ClientError classifies responses from 400 through 499.
	ClientError Outcome = "client_error"
	// ServerError classifies responses at or above 500.
	ServerError Outcome = "server_error"
	// Canceled classifies requests whose context was canceled.
	Canceled Outcome = "canceled"
	// Panicked classifies handlers that propagated a panic.
	Panicked Outcome = "panicked"
)

// Event excludes raw paths, queries, headers, payloads, identities, and errors.
// Method and Proto use fixed known values plus OTHER.
type Event struct {
	Method      string
	Route       string
	Status      int
	Bytes       int64
	Duration    time.Duration
	Proto       string
	Outcome     Outcome
	ClientClass string
}

// Policy injects observation without owning a logger or telemetry SDK. Route
// and ClientClass run once at completion; their panics are contained.
type Policy struct {
	Observer        func(context.Context, Event)
	Route           func(*http.Request) string
	ClientClass     func(*http.Request) string
	Now             func() time.Time
	RepanicObserver bool
}

type routeContextKey struct{}

type routeState struct {
	mu    sync.RWMutex
	value string
}

// ErrInvalidPolicy identifies invalid observation policy configuration.
var ErrInvalidPolicy = errors.New("observe: invalid policy")

// ConfigError reports an invalid observation policy field.
type ConfigError struct{ Field string }

func (e *ConfigError) Error() string { return fmt.Sprintf("observe: invalid %s", e.Field) }
func (e *ConfigError) Unwrap() error { return ErrInvalidPolicy }

// New constructs observation middleware.
func New(policy Policy) (func(http.Handler) http.Handler, error) {
	if policy.Observer == nil {
		return nil, &ConfigError{Field: "observer"}
	}
	if policy.Now == nil {
		policy.Now = time.Now
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route := &routeState{}
			r = r.WithContext(context.WithValue(r.Context(), routeContextKey{}, route))
			start := policy.Now()
			trackedWriter, recorder := httpx.Track(w)
			defer func() {
				panicValue := recover()
				status := recorder.Status
				if status == 0 {
					status = http.StatusOK
				}
				result := outcome(r.Context(), status)
				if panicValue != nil {
					result = Panicked
				}
				duration := policy.Now().Sub(start)
				if duration < 0 {
					duration = 0
				}
				routeName := route.load()
				if routeName == "" {
					routeName = metadata(policy.Route, r, 128)
				}
				clientClass := metadata(policy.ClientClass, r, 64)
				event := Event{Method: methodClass(r.Method), Route: routeName, Status: status, Bytes: recorder.Bytes, Duration: duration, Proto: protocolClass(r.Proto), Outcome: result, ClientClass: clientClass}
				notify(r.Context(), policy, event)
				if panicValue != nil {
					panic(panicValue)
				}
			}()
			next.ServeHTTP(trackedWriter, r)
		})
	}, nil
}

// RecordRoute stores a bounded route classification for the outer observation
// layer. Route-local middleware may call it after a router has matched. It
// returns false when request is nil or observation middleware is absent.
func RecordRoute(request *http.Request, route string) bool {
	if request == nil {
		return false
	}
	state, ok := request.Context().Value(routeContextKey{}).(*routeState)
	if !ok {
		return false
	}
	state.mu.Lock()
	state.value = bounded(route, 128)
	state.mu.Unlock()
	return true
}

func (state *routeState) load() string {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.value
}

func outcome(ctx context.Context, status int) Outcome {
	if ctx.Err() != nil {
		return Canceled
	}
	if status >= 500 {
		return ServerError
	}
	if status >= 400 {
		return ClientError
	}
	return Success
}
func notify(ctx context.Context, policy Policy, event Event) {
	if policy.RepanicObserver {
		policy.Observer(ctx, event)
		return
	}
	defer func() { _ = recover() }()
	policy.Observer(ctx, event)
}
func bounded(value string, maximum int) string {
	if len(value) > maximum {
		return value[:maximum]
	}
	return value
}
func metadata(extractor func(*http.Request) string, request *http.Request, maximum int) (value string) {
	if extractor == nil {
		return ""
	}
	defer func() { _ = recover() }()
	return bounded(extractor(request), maximum)
}
func methodClass(method string) string {
	switch method {
	case http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead,
		http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut,
		http.MethodTrace:
		return method
	default:
		return "OTHER"
	}
}
func protocolClass(protocol string) string {
	switch protocol {
	case "HTTP/1.0", "HTTP/1.1", "HTTP/2.0", "HTTP/3.0":
		return protocol
	default:
		return "OTHER"
	}
}
