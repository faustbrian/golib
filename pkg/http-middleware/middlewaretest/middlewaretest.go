// Package middlewaretest provides deterministic fixtures for middleware users.
package middlewaretest

import (
	"io"
	"net/http"
	"sync"
)

// Trace records bounded chain order under concurrent-safe access.
type Trace struct {
	mu      sync.Mutex
	events  []string
	maximum int
}

// NewTrace creates a trace bounded to 1024 events.
func NewTrace() *Trace { return &Trace{maximum: 1024} }

// Record appends an event until the trace's fixed bound is reached.
func (t *Trace) Record(event string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.events) < t.maximum {
		t.events = append(t.events, event)
	}
}

// Middleware records request entry and response unwind.
func (t *Trace) Middleware(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Record(name + ":request")
			next.ServeHTTP(w, r)
			t.Record(name + ":response")
		})
	}
}

// Events returns an independent event snapshot.
func (t *Trace) Events() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.events...)
}

// Capabilities describes standard optional ResponseWriter interfaces.
type Capabilities struct{ Flusher, Hijacker, Pusher, ReaderFrom bool }

// CapabilitiesOf inspects only standard optional interfaces.
func CapabilitiesOf(writer http.ResponseWriter) Capabilities {
	_, flusher := writer.(http.Flusher)
	_, hijacker := writer.(http.Hijacker)
	_, pusher := writer.(http.Pusher)
	_, readerFrom := writer.(io.ReaderFrom)
	return Capabilities{Flusher: flusher, Hijacker: hijacker, Pusher: pusher, ReaderFrom: readerFrom}
}
