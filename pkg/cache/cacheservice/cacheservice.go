// Package cacheservice adapts explicit cache resources to the service
// lifecycle without hiding their concrete types.
//
// A non-nil Shutdown transfers close ownership to the adapter; omitting it
// keeps the resource shared and guarantees the adapter never closes it.
// Startup validation and readiness are opt-in and use service-provided
// contexts. The adapter performs no retries, closes transferred resources once,
// and preserves validation and cleanup causes without formatting them.
package cacheservice

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/faustbrian/golib/pkg/service"
)

var (
	// ErrInvalidOptions identifies invalid adapter construction.
	ErrInvalidOptions = errors.New("invalid cache service options")
	// ErrUnavailable identifies a resource that has not started or is stopping.
	ErrUnavailable = errors.New("cache service resource unavailable")
)

// Startup performs optional resource validation before startup succeeds.
type Startup[R any] func(context.Context, R) error

// Check evaluates whether a resource can accept new work.
type Check[R any] func(context.Context, R) error

// Shutdown releases an explicitly transferred resource.
type Shutdown[R any] func(context.Context, R) error

// Options configure one cache lifecycle adapter.
type Options[R any] struct {
	// Name is the secret-safe component name.
	Name string
	// Resource is the caller-constructed cache or Valkey resource.
	Resource R
	// Startup optionally validates Resource.
	Startup Startup[R]
	// Readiness optionally checks whether Resource can accept new work.
	Readiness Check[R]
	// Shutdown transfers close ownership when non-nil.
	Shutdown Shutdown[R]
}

// OptionsError identifies one rejected option.
type OptionsError struct {
	Field  string
	Reason string
}

// Error returns a secret-safe construction diagnostic.
func (err *OptionsError) Error() string {
	return fmt.Sprintf("%s: %s: %v", err.Field, err.Reason, ErrInvalidOptions)
}

// Unwrap exposes the stable option classification.
func (err *OptionsError) Unwrap() error { return ErrInvalidOptions }

// StartupError preserves validation and cleanup failures without formatting
// either potentially sensitive cause.
type StartupError struct {
	// Validation is the startup-check failure.
	Validation error
	// Cleanup is an optional transferred-resource shutdown failure.
	Cleanup error
}

// Error returns a secret-safe startup diagnostic.
func (err *StartupError) Error() string {
	if err.Cleanup != nil {
		return "cache service startup validation and cleanup failed"
	}

	return "cache service startup validation failed"
}

// Unwrap preserves both causes for errors.Is and errors.As.
func (err *StartupError) Unwrap() []error {
	causes := []error{err.Validation}
	if err.Cleanup != nil {
		causes = append(causes, err.Cleanup)
	}

	return causes
}

// Adapter retains one explicit concrete resource and its lifecycle policy.
type Adapter[R any] struct {
	name      string
	resource  R
	startup   Startup[R]
	readiness Check[R]
	shutdown  Shutdown[R]

	mu     sync.RWMutex
	active bool

	stopOnce sync.Once
	stopErr  error
}

// New validates and constructs an inert adapter.
func New[R any](options Options[R]) (*Adapter[R], error) {
	if strings.TrimSpace(options.Name) == "" {
		return nil, &OptionsError{Field: "Name", Reason: "must not be blank"}
	}
	if nilResource(options.Resource) {
		return nil, &OptionsError{Field: "Resource", Reason: "must not be nil"}
	}

	return &Adapter[R]{
		name: options.Name, resource: options.Resource,
		startup: options.Startup, readiness: options.Readiness,
		shutdown: options.Shutdown,
	}, nil
}

// Resource returns the exact caller-provided concrete resource.
func (adapter *Adapter[R]) Resource() R { return adapter.resource }

// Readiness returns the configured opt-in readiness check.
func (adapter *Adapter[R]) Readiness() (service.ReadinessCheck, bool) {
	if adapter.readiness == nil {
		return service.ReadinessCheck{}, false
	}

	return service.ReadinessCheck{
		Name: adapter.name,
		Run: func(ctx context.Context) error {
			adapter.mu.RLock()
			active := adapter.active
			adapter.mu.RUnlock()
			if !active {
				return ErrUnavailable
			}

			return adapter.readiness(ctx, adapter.resource)
		},
	}, true
}

// Component returns the ordered service lifecycle component.
func (adapter *Adapter[R]) Component() service.Component {
	return service.Component{
		Name: adapter.name,
		Start: func(ctx context.Context) error {
			if adapter.startup != nil {
				if err := adapter.startup(ctx, adapter.resource); err != nil {
					cleanup := adapter.stop(ctx)

					return &StartupError{Validation: err, Cleanup: cleanup}
				}
			}

			adapter.mu.Lock()
			adapter.active = true
			adapter.mu.Unlock()

			return nil
		},
		Stop: adapter.stop,
	}
}

func (adapter *Adapter[R]) stop(ctx context.Context) error {
	adapter.stopOnce.Do(func() {
		adapter.mu.Lock()
		adapter.active = false
		adapter.mu.Unlock()
		if adapter.shutdown != nil {
			adapter.stopErr = adapter.shutdown(ctx, adapter.resource)
		}
	})

	return adapter.stopErr
}

func nilResource[R any](resource R) bool {
	value := reflect.ValueOf(resource)
	if !value.IsValid() {
		return true
	}
	kind := value.Kind()
	nilable := kind >= reflect.Chan && kind <= reflect.Slice

	return nilable && value.IsNil()
}
