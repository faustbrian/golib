// Package schedulerservice composes a scheduler.Runner with the service
// lifecycle and correlation schedule semantics.
//
// Callers construct schedules, lease stores, executors, and facility
// components. The adapter constructs and exposes the concrete runner, but it
// does not close facilities itself. Facilities listed in Options start before
// scheduling and stop only after service has canceled the runner task and the
// runner has joined active executions. Facility components therefore transfer
// exactly the ownership expressed by their own lifecycle contracts.
//
// Every occurrence starts an independent correlation workflow by default.
// CorrelationTrustedMetadata must be selected explicitly to continue trusted
// correlation metadata embedded in an application-owned schedule. Retry,
// schedule, lease, readiness, and business-command policy remain caller owned.
package schedulerservice

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/faustbrian/golib/pkg/correlation"
	schedulecorrelation "github.com/faustbrian/golib/pkg/correlation/schedule"
	"github.com/faustbrian/golib/pkg/scheduler"
	"github.com/faustbrian/golib/pkg/scheduler/lease"
	"github.com/faustbrian/golib/pkg/service"
)

// ErrInvalidOptions identifies invalid adapter construction.
var ErrInvalidOptions = errors.New("invalid scheduler service options")

// CorrelationMode selects the existing schedule correlation boundary used for
// each occurrence.
type CorrelationMode uint8

const (
	// CorrelationIndependent starts a new workflow for every occurrence.
	CorrelationIndependent CorrelationMode = iota
	// CorrelationTrustedMetadata continues correlation fields deliberately
	// embedded in application-owned schedule metadata.
	CorrelationTrustedMetadata
)

// Options configure one scheduler lifecycle adapter.
type Options struct {
	// Name is the secret-safe task and lifecycle prefix.
	Name string
	// Registry is the caller-compiled immutable schedule registry.
	Registry *scheduler.Registry
	// Leases is the caller-constructed scheduler lease facility.
	Leases lease.Store
	// Executor performs or dispatches application-owned scheduled work.
	Executor scheduler.Executor
	// Correlation creates per-occurrence correlation identifiers.
	Correlation *correlation.Factory
	// CorrelationOptions configure the existing schedule propagation adapter.
	CorrelationOptions schedulecorrelation.Options
	// CorrelationMode defaults to independent workflows.
	CorrelationMode CorrelationMode
	// RunnerOptions are applied while constructing the concrete runner.
	RunnerOptions []scheduler.RunnerOption
	// Facilities start before scheduling and stop after the runner drains.
	Facilities []service.Component
}

// OptionsError identifies one rejected option.
type OptionsError struct {
	// Field identifies the rejected option.
	Field string
	// Reason describes the safe failure category.
	Reason string
}

// Error returns a secret-safe construction diagnostic.
func (err *OptionsError) Error() string {
	return fmt.Sprintf("%s: %s: %v", err.Field, err.Reason, ErrInvalidOptions)
}

// Unwrap exposes the stable option classification.
func (err *OptionsError) Unwrap() error { return ErrInvalidOptions }

// Adapter retains the concrete runner and immutable service composition.
type Adapter struct {
	name       string
	runner     *scheduler.Runner
	facilities []service.Component
}

// New validates options and constructs a correlation-aware scheduler runner.
func New(options Options) (*Adapter, error) {
	if strings.TrimSpace(options.Name) == "" {
		return nil, &OptionsError{Field: "Name", Reason: "must not be blank"}
	}
	if options.Registry == nil {
		return nil, &OptionsError{Field: "Registry", Reason: "must not be nil"}
	}
	if nilInterface(options.Leases) {
		return nil, &OptionsError{Field: "Leases", Reason: "must not be nil"}
	}
	if nilInterface(options.Executor) {
		return nil, &OptionsError{Field: "Executor", Reason: "must not be nil"}
	}
	if options.Correlation == nil {
		return nil, &OptionsError{
			Field: "Correlation", Reason: "must not be nil",
		}
	}
	if options.CorrelationMode != CorrelationIndependent &&
		options.CorrelationMode != CorrelationTrustedMetadata {
		return nil, &OptionsError{
			Field: "CorrelationMode", Reason: "must be a known mode",
		}
	}

	propagation, err := schedulecorrelation.New(
		options.Correlation,
		options.CorrelationOptions,
	)
	if err != nil {
		return nil, err
	}
	executor := correlatedExecutor{
		executor:    options.Executor,
		propagation: propagation,
		mode:        options.CorrelationMode,
	}
	runnerOptions := append([]scheduler.RunnerOption(nil), options.RunnerOptions...)
	runner, err := scheduler.NewRunner(
		options.Registry,
		options.Leases,
		executor,
		runnerOptions...,
	)
	if err != nil {
		return nil, err
	}

	return &Adapter{
		name:       options.Name,
		runner:     runner,
		facilities: append([]service.Component(nil), options.Facilities...),
	}, nil
}

// Runner returns the concrete scheduler runner constructed by New.
func (adapter *Adapter) Runner() *scheduler.Runner { return adapter.runner }

// Plan returns an isolated long-running service plan. Facilities retain their
// declaration order; the final drain component therefore stops before them.
func (adapter *Adapter) Plan() service.Plan {
	components := append([]service.Component(nil), adapter.facilities...)
	components = append(components, service.Component{
		Name:  adapter.name + "-drain",
		Start: func(context.Context) error { return nil },
		Stop:  adapter.runner.Drain,
	})

	return service.Plan{
		Components: components,
		Tasks: []service.Task{{
			Name: adapter.name,
			Run:  adapter.runner.Run,
		}},
	}
}

type correlatedExecutor struct {
	executor    scheduler.Executor
	propagation *schedulecorrelation.Adapter
	mode        CorrelationMode
}

func (executor correlatedExecutor) Execute(
	ctx context.Context,
	scheduled scheduler.Context,
) error {
	var (
		values correlation.Values
		err    error
	)
	if executor.mode == CorrelationTrustedMetadata {
		values, err = executor.propagation.Run(scheduled.Metadata, true)
	} else {
		values, err = executor.propagation.Start()
	}
	if err != nil {
		return err
	}

	return executor.executor.Execute(correlation.WithValues(ctx, values), scheduled)
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)

	//exhaustive:ignore only nil-capable kinds need special handling
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
