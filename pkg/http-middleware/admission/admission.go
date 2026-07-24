// Package admission bounds local in-flight requests without implementing a
// distributed rate algorithm.
package admission

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/faustbrian/golib/pkg/http-middleware/internal/httpx"
)

// Policy configures bounded local request admission. Wait is capped at one
// minute.
type Policy struct {
	MaxInFlight, MaxWaiters int
	Wait                    time.Duration
	RetryAfterSeconds       int
	Shutdown                <-chan struct{}
}

// ErrInvalidPolicy identifies invalid admission policy configuration.
var ErrInvalidPolicy = errors.New("admission: invalid policy")

// ConfigError reports an invalid admission policy field.
type ConfigError struct{ Field string }

func (e *ConfigError) Error() string { return fmt.Sprintf("admission: invalid %s", e.Field) }
func (e *ConfigError) Unwrap() error { return ErrInvalidPolicy }

const maximumWait = time.Minute

// New constructs semaphore admission. Channel wait order is not a contractual
// fairness guarantee; queues are strictly bounded and allocate no waiter goroutine.
func New(policy Policy) (func(http.Handler) http.Handler, error) {
	if policy.MaxInFlight < 1 || policy.MaxInFlight > 1_000_000 || policy.MaxWaiters < 0 || policy.MaxWaiters > 1_000_000 || policy.Wait < 0 || policy.Wait > maximumWait || policy.RetryAfterSeconds < 0 {
		return nil, &ConfigError{Field: "limit"}
	}
	permits := make(chan struct{}, policy.MaxInFlight)
	var waiters atomic.Int64
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if stopped(policy.Shutdown) {
				reject(w, policy)
				return
			}
			acquired := false
			select {
			case permits <- struct{}{}:
				acquired = true
			default:
			}
			if !acquired && policy.Wait > 0 && policy.MaxWaiters > 0 {
				if waiters.Add(1) <= int64(policy.MaxWaiters) {
					timer := time.NewTimer(policy.Wait)
					select {
					case permits <- struct{}{}:
						acquired = true
					case <-r.Context().Done():
						timer.Stop()
						waiters.Add(-1)
						httpx.SafeError(w, http.StatusRequestTimeout, "request canceled\n")
						return
					case <-policy.Shutdown:
						timer.Stop()
						waiters.Add(-1)
						reject(w, policy)
						return
					case <-timer.C:
					}
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					waiters.Add(-1)
				} else {
					waiters.Add(-1)
				}
			}
			if !acquired {
				reject(w, policy)
				return
			}
			if r.Context().Err() != nil {
				<-permits
				httpx.SafeError(w, http.StatusRequestTimeout, "request canceled\n")
				return
			}
			defer func() { <-permits }()
			next.ServeHTTP(w, r)
		})
	}, nil
}

func stopped(shutdown <-chan struct{}) bool {
	if shutdown == nil {
		return false
	}
	select {
	case <-shutdown:
		return true
	default:
		return false
	}
}
func reject(w http.ResponseWriter, policy Policy) {
	if policy.RetryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(policy.RetryAfterSeconds))
	}
	httpx.SafeError(w, http.StatusServiceUnavailable, "server busy\n")
}
