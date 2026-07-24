// Package responsepolicy contains declarative cache and transport admission
// helpers. It does not implement caching or own health handlers.
package responsepolicy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/faustbrian/golib/pkg/http-middleware/internal/httpx"
)

// NoStore applies RFC 9111 no-store response policy before downstream handling.
func NoStore() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apply := func(header http.Header) { header.Set("Cache-Control", "no-store") }
			apply(w.Header())
			next.ServeHTTP(httpx.WithPolicy(w, apply), r)
		})
	}
}

// State is a bounded transport admission state.
type State uint8

const (
	// Ready permits requests to reach the application handler.
	Ready State = iota
	// NotReady rejects requests while the service is not ready.
	NotReady
	// Maintenance rejects requests during maintenance.
	Maintenance
)

// AdmissionPolicy configures transport-only readiness admission.
type AdmissionPolicy struct {
	State      func(context.Context) State
	Status     int
	RetryAfter string
}

// ErrInvalidPolicy identifies invalid response policy configuration.
var ErrInvalidPolicy = errors.New("responsepolicy: invalid policy")

// ConfigError reports an invalid response policy field.
type ConfigError struct{ Field string }

func (e *ConfigError) Error() string { return fmt.Sprintf("responsepolicy: invalid %s", e.Field) }
func (e *ConfigError) Unwrap() error { return ErrInvalidPolicy }

// Admission short-circuits through a caller-owned concurrency-safe state source.
func Admission(policy AdmissionPolicy) (func(http.Handler) http.Handler, error) {
	if policy.State == nil {
		return nil, &ConfigError{Field: "state source"}
	}
	if policy.Status == 0 {
		policy.Status = http.StatusServiceUnavailable
	}
	if policy.Status < 400 || policy.Status > 599 {
		return nil, &ConfigError{Field: "status"}
	}
	if policy.RetryAfter != "" && !validRetryAfter(policy.RetryAfter) {
		return nil, &ConfigError{Field: "retry after"}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if policy.State(r.Context()) != Ready {
				if policy.RetryAfter != "" {
					w.Header().Set("Retry-After", policy.RetryAfter)
				}
				httpx.SafeError(w, policy.Status, "service unavailable\n")
				return
			}
			next.ServeHTTP(w, r)
		})
	}, nil
}

func validRetryAfter(value string) bool {
	if !httpx.ValidFieldValue(value, 128) {
		return false
	}
	if seconds, err := strconv.ParseUint(value, 10, 31); err == nil {
		return seconds <= 86400
	}
	_, err := http.ParseTime(value)
	return err == nil
}
