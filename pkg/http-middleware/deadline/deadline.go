// Package deadline gives request contexts a bounded handler deadline. It does
// not replace server read, write, idle, upstream, shutdown, or process limits.
package deadline

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrInvalidPolicy identifies invalid deadline policy configuration.
var ErrInvalidPolicy = errors.New("deadline: invalid policy")

// ConfigError reports an invalid deadline policy field.
type ConfigError struct{ Field string }

func (e *ConfigError) Error() string { return fmt.Sprintf("deadline: invalid %s", e.Field) }
func (e *ConfigError) Unwrap() error { return ErrInvalidPolicy }

// Policy configures a handler context timeout of at most 24 hours. Code that
// ignores its context is not interrupted.
type Policy struct{ Timeout time.Duration }

const maximumTimeout = 24 * time.Hour

// New constructs deadline middleware. A shorter parent deadline is preserved.
func New(policy Policy) (func(http.Handler) http.Handler, error) {
	if policy.Timeout <= 0 || policy.Timeout > maximumTimeout {
		return nil, &ConfigError{Field: "timeout"}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), policy.Timeout)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}, nil
}
