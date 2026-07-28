// Package postgresservice adapts PostgreSQL pools to the service lifecycle.
//
// Constructor results are owned by the adapter. Existing pools remain shared
// unless ownership is explicitly transferred. Startup validation, readiness,
// and shutdown use caller and service contexts plus the pool's own bounds. The
// adapter performs no retries, closes owned pools exactly once, and never
// closes shared pools.
package postgresservice

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
	ErrInvalidOptions = errors.New("invalid postgres service options")
	// ErrUnavailable identifies a pool that has not started or is stopping.
	ErrUnavailable = errors.New("postgres service pool unavailable")
)

// Resource is the lifecycle surface required from a PostgreSQL pool.
type Resource interface {
	Ping(context.Context) error
	Close(context.Context) error
}

// Constructor acquires a pool whose ownership transfers to the adapter only
// after a successful return.
type Constructor func(context.Context) (Resource, error)

// Options configure one PostgreSQL lifecycle adapter.
type Options struct {
	// Name is the secret-safe component and readiness-check name.
	Name string
	// Construct acquires an adapter-owned pool during component startup.
	Construct Constructor
	// Pool supplies an existing pool. The caller retains ownership unless
	// TransferOwnership is true.
	Pool Resource
	// TransferOwnership closes Pool during shutdown and failed startup.
	TransferOwnership bool
	// StartupPing validates the acquired pool before startup succeeds.
	StartupPing bool
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

// StartupError preserves validation and partial-cleanup failures without
// formatting either potentially sensitive cause.
type StartupError struct {
	Validation error
	Cleanup    error
}

// Error returns a secret-safe startup diagnostic.
func (err *StartupError) Error() string {
	if err.Cleanup != nil {
		return "postgres service startup validation and cleanup failed"
	}

	return "postgres service startup validation failed"
}

// Unwrap preserves both failures for errors.Is and errors.As.
func (err *StartupError) Unwrap() []error {
	causes := []error{err.Validation}
	if err.Cleanup != nil {
		causes = append(causes, err.Cleanup)
	}

	return causes
}

// Adapter owns the lifecycle state for one PostgreSQL pool.
type Adapter struct {
	name      string
	construct Constructor
	existing  Resource
	owned     bool
	ping      bool

	mu       sync.RWMutex
	resource Resource
	active   bool
	stopOnce sync.Once
	stopErr  error
}

// New validates and constructs an inert adapter.
func New(options Options) (*Adapter, error) {
	if strings.TrimSpace(options.Name) == "" {
		return nil, &OptionsError{Field: "Name", Reason: "must not be blank"}
	}
	hasPool := !nilResource(options.Pool)
	hasConstructor := options.Construct != nil
	if hasConstructor == hasPool {
		return nil, &OptionsError{
			Field: "Pool", Reason: "configure exactly one pool or constructor",
		}
	}

	return &Adapter{
		name: options.Name, construct: options.Construct, existing: options.Pool,
		owned: options.Construct != nil || options.TransferOwnership,
		ping:  options.StartupPing,
	}, nil
}

// Component returns the ordered service lifecycle component.
func (adapter *Adapter) Component() service.Component {
	return service.Component{
		Name:  adapter.name,
		Start: adapter.start,
		Stop:  adapter.stop,
	}
}

// Pool returns the current pool after successful component startup.
func (adapter *Adapter) Pool() (Resource, bool) {
	adapter.mu.RLock()
	defer adapter.mu.RUnlock()

	return adapter.resource, adapter.active
}

// Readiness returns an opt-in dependency check for the active pool. Callers
// decide whether to include it in service.Plan.Readiness.
func (adapter *Adapter) Readiness() service.ReadinessCheck {
	return service.ReadinessCheck{
		Name: adapter.name,
		Run: func(ctx context.Context) error {
			resource, ok := adapter.Pool()
			if !ok {
				return ErrUnavailable
			}

			return resource.Ping(ctx)
		},
	}
}

func (adapter *Adapter) start(ctx context.Context) error {
	resource := adapter.existing
	if adapter.construct != nil {
		var err error
		resource, err = adapter.construct(ctx)
		if err != nil {
			return err
		}
	}
	if nilResource(resource) {
		return ErrUnavailable
	}
	if adapter.ping {
		if err := resource.Ping(ctx); err != nil {
			var cleanup error
			if adapter.owned {
				cleanup = resource.Close(ctx)
			}

			return &StartupError{Validation: err, Cleanup: cleanup}
		}
	}

	adapter.mu.Lock()
	adapter.resource = resource
	adapter.active = true
	adapter.mu.Unlock()

	return nil
}

func (adapter *Adapter) stop(ctx context.Context) error {
	adapter.stopOnce.Do(func() {
		adapter.mu.Lock()
		resource := adapter.resource
		active := adapter.active
		adapter.active = false
		adapter.mu.Unlock()
		if active && adapter.owned {
			adapter.stopErr = resource.Close(ctx)
		}
	})

	return adapter.stopErr
}

func nilResource(resource Resource) bool {
	if resource == nil {
		return true
	}
	value := reflect.ValueOf(resource)

	return value.Kind() == reflect.Pointer && value.IsNil()
}
