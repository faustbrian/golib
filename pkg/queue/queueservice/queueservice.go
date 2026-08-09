// Package queueservice composes explicit queue producers and workers with the
// service lifecycle and the existing correlation queue semantics.
//
// Producer resources remain concrete and caller-visible. A non-nil Shutdown
// transfers close ownership; shared resources omit it. Service shutdown first
// rejects new publishes, waits for active publishes within the service
// context, and only then closes an owned transport. PublishWithAcceptance
// exposes definite rejection, confirmed acceptance, and ambiguous acceptance;
// the adapter never retries an application publish.
//
// LifecycleWorker adapts typed startup, readiness, blocking run, handler, and
// shutdown callbacks into one service plan. Worker retains the smaller
// *queue.Queue convenience boundary. Both paths stop intake, join admitted work
// within the service context, and release the concrete transport only after
// the drain. NewHandler creates a new request ID for every delivery.
// TrustedMetadata must be enabled only after authenticating the immediate queue
// boundary. Callback failures preserve their causes without formatting their
// text, and callback panics are returned without retaining their values.
package queueservice

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/correlation"
	queuecorrelation "github.com/faustbrian/golib/pkg/correlation/queue"
	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/service"
	"go.opentelemetry.io/otel/propagation"
)

// MaxNameBytes bounds component, task, and readiness identifiers.
const MaxNameBytes = 128

// CallbackOperation identifies one application callback boundary.
type CallbackOperation uint8

const (
	// CallbackStartup identifies resource validation during service start.
	CallbackStartup CallbackOperation = 1
	// CallbackReadiness identifies an opt-in dependency readiness check.
	CallbackReadiness CallbackOperation = 2
	// CallbackPublish identifies concrete producer publication.
	CallbackPublish CallbackOperation = 3
	// CallbackHandler identifies application task handling.
	CallbackHandler CallbackOperation = 4
	// CallbackRun identifies supervised worker intake.
	CallbackRun CallbackOperation = 5
	// CallbackShutdown identifies transferred resource cleanup.
	CallbackShutdown CallbackOperation = 6
	// CallbackAdmission identifies synchronous worker intake closure.
	CallbackAdmission CallbackOperation = 7
)

// PublishAcceptance reports whether a failed publish reached the backend.
type PublishAcceptance uint8

const (
	// PublishNotAccepted means the backend definitively did not accept the task.
	PublishNotAccepted PublishAcceptance = 1
	// PublishAccepted means the backend definitively accepted the task.
	PublishAccepted PublishAcceptance = 2
	// PublishUnknown means the backend may have accepted the task. Applications
	// must reconcile or rely on idempotency instead of retrying blindly.
	PublishUnknown PublishAcceptance = 3
)

var (
	// ErrInvalidOptions identifies invalid adapter construction.
	ErrInvalidOptions = errors.New("invalid queue service options")
	// ErrUnavailable reports an inactive or draining adapter.
	ErrUnavailable = errors.New("queue service adapter unavailable")
	// ErrMissingCorrelation reports a publish without an explicit parent
	// workflow. Callers beginning new work must start it with their factory.
	ErrMissingCorrelation = errors.New("queue service producer correlation missing")
	// ErrCallbackPanic reports a recovered application callback panic.
	ErrCallbackPanic = errors.New("queue service callback panicked")
	// ErrPublishOutcomeUnknown reports a publish that may have reached the
	// backend and therefore must not be retried blindly.
	ErrPublishOutcomeUnknown = errors.New("queue service publish outcome unknown")
	// ErrInvalidPublishAcceptance reports a callback result outside the public
	// acceptance contract.
	ErrInvalidPublishAcceptance = errors.New("queue service publish acceptance invalid")
	// ErrWorkerExited reports a worker run callback that returned successfully
	// before its context was canceled.
	ErrWorkerExited = errors.New("queue service worker exited unexpectedly")
)

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

// CallbackPanicError identifies a recovered callback without retaining or
// formatting the panic value.
type CallbackPanicError struct {
	// Operation identifies the callback boundary.
	Operation CallbackOperation
}

// Error returns a secret-safe callback failure.
func (err *CallbackPanicError) Error() string {
	return fmt.Sprintf(
		"queue service %s callback panicked",
		callbackOperationName(err.Operation),
	)
}

// Unwrap exposes the stable panic classification.
func (err *CallbackPanicError) Unwrap() error { return ErrCallbackPanic }

// CallbackError preserves a callback failure for errors.Is and errors.As
// without formatting potentially sensitive backend or application text.
type CallbackError struct {
	// Operation identifies the callback boundary.
	Operation CallbackOperation
	// Err is the original callback failure.
	Err error
}

// Error returns a secret-safe callback failure.
func (err *CallbackError) Error() string {
	return fmt.Sprintf(
		"queue service %s callback failed",
		callbackOperationName(err.Operation),
	)
}

// Unwrap preserves the callback cause for errors.Is and errors.As.
func (err *CallbackError) Unwrap() error { return err.Err }

// StartupError preserves validation and partial-cleanup failures without
// formatting either potentially sensitive cause.
type StartupError struct {
	// Validation is the startup-check failure.
	Validation error
	// Cleanup is an optional transferred-resource shutdown failure.
	Cleanup error
}

// Error returns a secret-safe startup diagnostic.
func (err *StartupError) Error() string {
	if err.Cleanup != nil {
		return "queue service startup validation and cleanup failed"
	}

	return "queue service startup validation failed"
}

// Unwrap preserves both causes for errors.Is and errors.As.
func (err *StartupError) Unwrap() []error {
	causes := []error{err.Validation}
	if err.Cleanup != nil {
		causes = append(causes, err.Cleanup)
	}

	return causes
}

// PublishError preserves the backend cause and acceptance classification
// without formatting potentially sensitive backend details.
type PublishError struct {
	// Acceptance describes whether the task reached the backend.
	Acceptance PublishAcceptance
	// Err is the original classifiable failure.
	Err error
}

// Error returns a secret-safe publish diagnostic.
func (err *PublishError) Error() string {
	return fmt.Sprintf(
		"queue service publish failed with %s acceptance",
		publishAcceptanceName(err.Acceptance),
	)
}

// Unwrap preserves the backend and stable acceptance causes.
func (err *PublishError) Unwrap() error { return err.Err }

// Startup validates an explicitly constructed resource before admission.
type Startup[R any] func(context.Context, R) error

// Check evaluates whether a resource can accept new work.
type Check[R any] func(context.Context, R) error

// CloseAdmission synchronously and idempotently stops new worker intake. It
// must return promptly and must not wait for admitted handlers to finish.
type CloseAdmission[R any] func(R) error

// Publish appends one correlation-aware message through a concrete resource.
type Publish[R any] func(
	context.Context,
	R,
	core.QueuedMessage,
	...job.AllowOption,
) error

// PublishWithAcceptance appends one task and reports whether a failure reached
// the backend. It must not retry application work internally.
type PublishWithAcceptance[R any] func(
	context.Context,
	R,
	core.QueuedMessage,
	...job.AllowOption,
) (PublishAcceptance, error)

// Shutdown releases an explicitly transferred producer resource.
type Shutdown[R any] func(context.Context, R) error

// ProducerOptions configure one producer lifecycle adapter.
type ProducerOptions[R any] struct {
	// Name is the secret-safe component name.
	Name string
	// Resource is the caller-constructed concrete producer.
	Resource R
	// Correlation creates message-hop identifiers.
	Correlation *correlation.Factory
	// CorrelationOptions configure the existing queue propagation adapter.
	CorrelationOptions queuecorrelation.Options
	// TracePropagator explicitly injects caller-owned telemetry context when
	// configured. Nil disables trace propagation.
	TracePropagator propagation.TextMapPropagator
	// Startup optionally validates Resource before admission begins.
	Startup Startup[R]
	// Readiness optionally checks Resource after successful startup.
	Readiness Check[R]
	// Publish performs one concrete, caller-bounded append. A returned error has
	// unknown backend acceptance. Prefer PublishWithAcceptance when the backend
	// can distinguish a definite rejection from an ambiguous result.
	Publish Publish[R]
	// PublishWithAcceptance performs one concrete append with an explicit
	// backend-acceptance result. Exactly one publish callback is required.
	PublishWithAcceptance PublishWithAcceptance[R]
	// Shutdown transfers transport close ownership when non-nil.
	Shutdown Shutdown[R]
}

// Producer retains a concrete producer and coordinates its in-flight calls.
type Producer[R any] struct {
	name                  string
	resource              R
	propagation           *queuecorrelation.Adapter
	trace                 propagation.TextMapPropagator
	startup               Startup[R]
	readiness             Check[R]
	publish               Publish[R]
	publishWithAcceptance PublishWithAcceptance[R]
	shutdown              Shutdown[R]

	mu              sync.Mutex
	active          bool
	stopping        bool
	inflight        int
	drained         chan struct{}
	startupAttempt  *startupAttempt
	shutdownAttempt *shutdownAttempt
}

type startupAttempt struct {
	done chan struct{}
}

type shutdownAttempt struct {
	done chan struct{}
	err  error
}

// NewProducer validates and constructs an inert producer adapter.
func NewProducer[R any](options ProducerOptions[R]) (*Producer[R], error) {
	if !validName(options.Name) {
		return nil, &OptionsError{
			Field: "Name", Reason: "must be valid UTF-8 within the configured bound",
		}
	}
	if nilValue(options.Resource) {
		return nil, &OptionsError{Field: "Resource", Reason: "must not be nil"}
	}
	if options.Correlation == nil {
		return nil, &OptionsError{Field: "Correlation", Reason: "must not be nil"}
	}
	if options.TracePropagator != nil && nilValue(options.TracePropagator) {
		return nil, &OptionsError{Field: "TracePropagator", Reason: "must not be nil"}
	}
	if (options.Publish == nil) == (options.PublishWithAcceptance == nil) {
		return nil, &OptionsError{
			Field: "Publish", Reason: "requires exactly one publish callback",
		}
	}
	correlationAdapter, err := queuecorrelation.New(
		options.Correlation,
		options.CorrelationOptions,
	)
	if err != nil {
		return nil, err
	}
	return &Producer[R]{
		name: options.Name, resource: options.Resource,
		propagation: correlationAdapter, trace: options.TracePropagator,
		startup: options.Startup, readiness: options.Readiness,
		publish:               options.Publish,
		publishWithAcceptance: options.PublishWithAcceptance,
		shutdown:              options.Shutdown,
	}, nil
}

// Resource returns the exact caller-provided producer.
func (producer *Producer[R]) Resource() R { return producer.resource }

// Component returns the producer's ordered service lifecycle component.
func (producer *Producer[R]) Component() service.Component {
	return service.Component{
		Name: producer.name,
		CloseAdmission: func() error {
			producer.closeAdmission()

			return nil
		},
		Start: producer.start,
		Stop:  producer.stop,
	}
}

// Readiness returns the configured opt-in dependency check.
func (producer *Producer[R]) Readiness() (service.ReadinessCheck, bool) {
	if producer.readiness == nil {
		return service.ReadinessCheck{}, false
	}

	return service.ReadinessCheck{
		Name: producer.name,
		Run: func(ctx context.Context) error {
			if !producer.beginUse() {
				return ErrUnavailable
			}
			defer producer.finishUse()

			return invokeCallback(CallbackReadiness, func() error {
				return producer.readiness(ctx, producer.resource)
			})
		},
	}, true
}

// Publish creates a message hop, attaches its carrier to cloned job metadata,
// and invokes the concrete producer with that child correlation in its
// context. Correlation values are also returned for caller-owned logging and
// telemetry.
func (producer *Producer[R]) Publish(
	ctx context.Context,
	message core.QueuedMessage,
	options ...job.AllowOption,
) (correlation.Values, error) {
	values, _, err := producer.PublishWithAcceptance(ctx, message, options...)

	return values, err
}

// PublishWithAcceptance creates a message hop and reports whether the concrete
// backend accepted the task. The adapter performs exactly one callback call
// and never retries an unknown result.
func (producer *Producer[R]) PublishWithAcceptance(
	ctx context.Context,
	message core.QueuedMessage,
	options ...job.AllowOption,
) (correlation.Values, PublishAcceptance, error) {
	if !producer.beginUse() {
		return correlation.Values{}, PublishNotAccepted, ErrUnavailable
	}
	defer producer.finishUse()
	if ctx == nil {
		return correlation.Values{}, PublishNotAccepted, &OptionsError{
			Field: "ctx", Reason: "must not be nil",
		}
	}
	if cause := context.Cause(ctx); cause != nil {
		return correlation.Values{}, PublishNotAccepted, cause
	}

	parent, ok := correlation.FromContext(ctx)
	if !ok {
		return correlation.Values{}, PublishNotAccepted, ErrMissingCorrelation
	}
	carrier := correlationCarrier(options)
	values, err := producer.propagation.Send(carrier, parent)
	if err != nil {
		return correlation.Values{}, PublishNotAccepted, err
	}
	traceCarrier := propagation.MapCarrier{}
	if producer.trace != nil {
		err = invokeCallback(CallbackPublish, func() error {
			producer.trace.Inject(ctx, traceCarrier)

			return nil
		})
		if err != nil {
			acceptance, publishErr := normalizePublishResult(PublishNotAccepted, err)

			return values, acceptance, publishErr
		}
		removeEmptyFields(traceCarrier)
	}
	transportOptions := withTransportMetadata(options, carrier, traceCarrier)
	if err = transportOptions[0].Metadata.Validate(); err != nil {
		return values, PublishNotAccepted, err
	}

	publishContext := correlation.WithValues(ctx, values)
	acceptance := PublishUnknown
	if producer.publishWithAcceptance != nil {
		acceptance, err = invokePublishCallback(func() (PublishAcceptance, error) {
			return producer.publishWithAcceptance(
				publishContext,
				producer.resource,
				message,
				transportOptions...,
			)
		})
	} else {
		err = invokeCallback(CallbackPublish, func() error {
			return producer.publish(
				publishContext,
				producer.resource,
				message,
				transportOptions...,
			)
		})
		if err == nil {
			acceptance = PublishAccepted
		}
	}

	acceptance, err = normalizePublishResult(acceptance, err)

	return values, acceptance, err
}

func correlationCarrier(options []job.AllowOption) map[string]string {
	carrier := make(map[string]string, 3)
	if len(options) == 0 || options[0].Metadata == nil {
		return carrier
	}
	for key, value := range options[0].Metadata.Correlation {
		carrier[key] = value
	}

	return carrier
}

func (producer *Producer[R]) beginUse() bool {
	producer.mu.Lock()
	defer producer.mu.Unlock()
	if !producer.active || producer.stopping {
		return false
	}
	producer.inflight++

	return true
}

func (producer *Producer[R]) finishUse() {
	producer.mu.Lock()
	defer producer.mu.Unlock()
	producer.inflight--
	if producer.stopping {
		if producer.inflight == 0 {
			if producer.drained != nil {
				close(producer.drained)
				producer.drained = nil
			}
		}
	}
}

func (producer *Producer[R]) closeAdmission() {
	producer.mu.Lock()
	producer.active = false
	producer.stopping = true
	producer.mu.Unlock()
}

func (producer *Producer[R]) start(ctx context.Context) error {
	producer.mu.Lock()
	if producer.stopping {
		producer.mu.Unlock()

		return ErrUnavailable
	}
	if producer.active {
		producer.mu.Unlock()

		return nil
	}
	if producer.startupAttempt != nil {
		producer.mu.Unlock()

		return ErrUnavailable
	}
	var attempt *startupAttempt
	if producer.startup != nil {
		attempt = &startupAttempt{done: make(chan struct{})}
		producer.startupAttempt = attempt
	}
	producer.mu.Unlock()

	if producer.startup != nil {
		callbackErr := invokeCallback(CallbackStartup, func() error {
			return producer.startup(ctx, producer.resource)
		})
		if callbackErr != nil {
			producer.mu.Lock()
			producer.startupAttempt = nil
			if producer.shutdown != nil {
				producer.stopping = true
			}
			close(attempt.done)
			producer.mu.Unlock()
			startupErr := &StartupError{Validation: callbackErr}
			if producer.shutdown != nil {
				startupErr.Cleanup = producer.shutdownResource(ctx)
			}

			return startupErr
		}
	}

	producer.mu.Lock()
	defer producer.mu.Unlock()
	if attempt != nil {
		producer.startupAttempt = nil
		close(attempt.done)
	}
	if producer.stopping {
		return ErrUnavailable
	}
	producer.active = true

	return nil
}

func (producer *Producer[R]) stop(ctx context.Context) error {
	producer.closeAdmission()
	producer.mu.Lock()
	startup := producer.startupAttempt
	if producer.drained == nil {
		producer.drained = make(chan struct{})
		if producer.inflight == 0 {
			close(producer.drained)
		}
	}
	drained := producer.drained
	producer.mu.Unlock()

	if startup != nil {
		select {
		case <-startup.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	select {
	case <-drained:
	case <-ctx.Done():
		return ctx.Err()
	}
	if producer.shutdown == nil {
		return nil
	}

	return producer.shutdownResource(ctx)
}

func (producer *Producer[R]) shutdownResource(ctx context.Context) error {
	producer.mu.Lock()
	if producer.shutdownAttempt != nil {
		attempt := producer.shutdownAttempt
		producer.mu.Unlock()
		select {
		case <-attempt.done:
			return attempt.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	attempt := &shutdownAttempt{done: make(chan struct{})}
	producer.shutdownAttempt = attempt
	producer.mu.Unlock()

	err := invokeCallback(CallbackShutdown, func() error {
		return producer.shutdown(ctx, producer.resource)
	})
	producer.mu.Lock()
	attempt.err = err
	close(attempt.done)
	producer.mu.Unlock()

	return err
}

// Handler is the queue worker handler signature.
type Handler func(context.Context, core.TaskMessage) error

// HandlerOptions configure a correlation-aware delivery boundary.
type HandlerOptions struct {
	// Correlation creates a new request ID for every delivery attempt.
	Correlation *correlation.Factory
	// CorrelationOptions configure the existing queue propagation adapter.
	CorrelationOptions queuecorrelation.Options
	// TrustedMetadata preserves inbound correlation only when explicitly true.
	TrustedMetadata bool
	// TracePropagator explicitly extracts caller-owned telemetry context when
	// configured. Nil disables trace propagation.
	TracePropagator propagation.TextMapPropagator
	// Handler performs application-owned work.
	Handler Handler
}

// NewHandler wraps application work with the existing queue receive boundary.
func NewHandler(options HandlerOptions) (Handler, error) {
	if options.Correlation == nil {
		return nil, &OptionsError{Field: "Correlation", Reason: "must not be nil"}
	}
	if options.TracePropagator != nil && nilValue(options.TracePropagator) {
		return nil, &OptionsError{Field: "TracePropagator", Reason: "must not be nil"}
	}
	if options.Handler == nil {
		return nil, &OptionsError{Field: "Handler", Reason: "must not be nil"}
	}
	correlationAdapter, err := queuecorrelation.New(
		options.Correlation,
		options.CorrelationOptions,
	)
	if err != nil {
		return nil, err
	}

	return func(ctx context.Context, message core.TaskMessage) (err error) {
		defer func() {
			if recover() != nil {
				err = &CallbackPanicError{Operation: CallbackHandler}
			}
		}()
		var metadata map[string]string
		if carrier, ok := message.(interface {
			CorrelationMetadata() map[string]string
		}); ok {
			metadata = carrier.CorrelationMetadata()
		}
		values, receiveErr := correlationAdapter.Receive(
			metadata,
			options.TrustedMetadata,
		)
		if receiveErr != nil {
			return receiveErr
		}
		if options.TracePropagator != nil {
			var traceMetadata map[string]string
			if carrier, ok := message.(interface {
				TraceContextMetadata() map[string]string
			}); ok {
				traceMetadata = carrier.TraceContextMetadata()
			}
			if err := (job.Metadata{TraceContext: traceMetadata}).Validate(); err != nil {
				return err
			}
			ctx = options.TracePropagator.Extract(
				ctx,
				propagation.MapCarrier(traceMetadata),
			)
		}

		return invokeCallback(CallbackHandler, func() error {
			return options.Handler(correlation.WithValues(ctx, values), message)
		})
	}, nil
}

// Run owns worker intake until cancellation or backend failure and must join
// every handler it admits before returning.
type Run[R any] func(context.Context, R, Handler) error

// LifecycleWorkerOptions configure one typed, supervised worker lifecycle.
type LifecycleWorkerOptions[R any] struct {
	// Name is the secret-safe component, task, and readiness name.
	Name string
	// Resource is the caller-constructed concrete worker.
	Resource R
	// Correlation creates a new request ID for every delivery attempt.
	Correlation *correlation.Factory
	// CorrelationOptions configure the existing queue propagation adapter.
	CorrelationOptions queuecorrelation.Options
	// TrustedMetadata preserves inbound correlation only when explicitly true.
	TrustedMetadata bool
	// TracePropagator explicitly extracts caller-owned telemetry context when
	// configured. Nil disables trace propagation.
	TracePropagator propagation.TextMapPropagator
	// Handler performs application-owned work.
	Handler Handler
	// Startup optionally validates Resource before intake can run.
	Startup Startup[R]
	// Readiness optionally checks Resource after successful startup.
	Readiness Check[R]
	// CloseAdmission stops backend intake when service drain begins. The adapter
	// independently rejects handler calls after the callback starts.
	CloseAdmission CloseAdmission[R]
	// Run starts intake and joins admitted handlers before returning.
	Run Run[R]
	// Shutdown stops remaining transport work and closes Resource exactly once.
	Shutdown Shutdown[R]
}

// LifecycleWorker retains a concrete worker and explicit lifecycle callbacks.
type LifecycleWorker[R any] struct {
	name      string
	resource  R
	handler   Handler
	startup   Startup[R]
	readiness Check[R]
	admission CloseAdmission[R]
	run       Run[R]
	shutdown  Shutdown[R]

	mu              sync.Mutex
	active          bool
	stopping        bool
	runStarted      bool
	inflight        int
	drained         chan struct{}
	startupAttempt  *startupAttempt
	shutdownAttempt *shutdownAttempt
	admissionOnce   sync.Once
	admissionErr    error
}

// NewLifecycleWorker validates and constructs an inert typed worker adapter.
func NewLifecycleWorker[R any](
	options LifecycleWorkerOptions[R],
) (*LifecycleWorker[R], error) {
	if !validName(options.Name) {
		return nil, &OptionsError{
			Field: "Name", Reason: "must be valid UTF-8 within the configured bound",
		}
	}
	if nilValue(options.Resource) {
		return nil, &OptionsError{Field: "Resource", Reason: "must not be nil"}
	}
	if options.Run == nil {
		return nil, &OptionsError{Field: "Run", Reason: "must not be nil"}
	}
	if options.Shutdown == nil {
		return nil, &OptionsError{Field: "Shutdown", Reason: "must not be nil"}
	}
	handler, err := NewHandler(HandlerOptions{
		Correlation:        options.Correlation,
		CorrelationOptions: options.CorrelationOptions,
		TrustedMetadata:    options.TrustedMetadata,
		TracePropagator:    options.TracePropagator,
		Handler:            options.Handler,
	})
	if err != nil {
		return nil, err
	}

	worker := &LifecycleWorker[R]{
		name: options.Name, resource: options.Resource, handler: handler,
		startup: options.Startup, readiness: options.Readiness,
		admission: options.CloseAdmission, run: options.Run, shutdown: options.Shutdown,
	}
	worker.handler = func(ctx context.Context, message core.TaskMessage) error {
		if !worker.beginUse() {
			return ErrUnavailable
		}
		defer worker.finishUse()

		return handler(ctx, message)
	}

	return worker, nil
}

// Resource returns the exact caller-provided worker resource.
func (worker *LifecycleWorker[R]) Resource() R { return worker.resource }

// Plan returns the worker component, supervised run task, and optional
// readiness check under one stable identity.
func (worker *LifecycleWorker[R]) Plan() service.Plan {
	plan := service.Plan{
		Components: []service.Component{{
			Name:           worker.name,
			CloseAdmission: worker.closeAdmission,
			Start:          worker.start,
			Stop:           worker.stop,
		}},
		Tasks: []service.Task{{Name: worker.name, Run: worker.runTask}},
	}
	if worker.readiness != nil {
		plan.Readiness = []service.ReadinessCheck{{
			Name: worker.name, Run: worker.checkReadiness,
		}}
	}

	return plan
}

func (worker *LifecycleWorker[R]) start(ctx context.Context) error {
	worker.mu.Lock()
	if worker.stopping {
		worker.mu.Unlock()

		return ErrUnavailable
	}
	if worker.active {
		worker.mu.Unlock()

		return nil
	}
	if worker.startupAttempt != nil {
		worker.mu.Unlock()

		return ErrUnavailable
	}
	var attempt *startupAttempt
	if worker.startup != nil {
		attempt = &startupAttempt{done: make(chan struct{})}
		worker.startupAttempt = attempt
	}
	worker.mu.Unlock()

	if worker.startup != nil {
		callbackErr := invokeCallback(CallbackStartup, func() error {
			return worker.startup(ctx, worker.resource)
		})
		if callbackErr != nil {
			worker.mu.Lock()
			worker.stopping = true
			worker.startupAttempt = nil
			close(attempt.done)
			worker.mu.Unlock()

			return &StartupError{
				Validation: callbackErr,
				Cleanup:    worker.shutdownResource(ctx),
			}
		}
	}

	worker.mu.Lock()
	defer worker.mu.Unlock()
	if attempt != nil {
		worker.startupAttempt = nil
		close(attempt.done)
	}
	if worker.stopping {
		return ErrUnavailable
	}
	worker.active = true

	return nil
}

func (worker *LifecycleWorker[R]) runTask(ctx context.Context) error {
	if !worker.beginRun() {
		return ErrUnavailable
	}
	defer func() {
		worker.mu.Lock()
		worker.active = false
		worker.stopping = true
		worker.mu.Unlock()
		worker.finishUse()
	}()

	err := invokeCallback(CallbackRun, func() error {
		return worker.run(ctx, worker.resource, worker.handler)
	})
	if err == nil && context.Cause(ctx) == nil {
		return ErrWorkerExited
	}

	return err
}

func (worker *LifecycleWorker[R]) beginRun() bool {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if !worker.active || worker.stopping || worker.runStarted {
		return false
	}
	worker.runStarted = true
	worker.inflight++

	return true
}

func (worker *LifecycleWorker[R]) checkReadiness(ctx context.Context) error {
	if !worker.beginUse() {
		return ErrUnavailable
	}
	defer worker.finishUse()

	return invokeCallback(CallbackReadiness, func() error {
		return worker.readiness(ctx, worker.resource)
	})
}

func (worker *LifecycleWorker[R]) beginUse() bool {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if !worker.active || worker.stopping {
		return false
	}
	worker.inflight++

	return true
}

func (worker *LifecycleWorker[R]) finishUse() {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	worker.inflight--
	if worker.stopping {
		if worker.inflight == 0 {
			if worker.drained != nil {
				close(worker.drained)
				worker.drained = nil
			}
		}
	}
}

func (worker *LifecycleWorker[R]) closeAdmission() error {
	worker.mu.Lock()
	worker.active = false
	worker.stopping = true
	worker.mu.Unlock()
	worker.admissionOnce.Do(func() {
		if worker.admission != nil {
			worker.admissionErr = invokeCallback(CallbackAdmission, func() error {
				return worker.admission(worker.resource)
			})
		}
	})

	return worker.admissionErr
}

func (worker *LifecycleWorker[R]) stop(ctx context.Context) error {
	admissionErr := worker.closeAdmission()
	worker.mu.Lock()
	startup := worker.startupAttempt
	if worker.drained == nil {
		worker.drained = make(chan struct{})
		if worker.inflight == 0 {
			close(worker.drained)
		}
	}
	drained := worker.drained
	worker.mu.Unlock()

	if startup != nil {
		select {
		case <-startup.done:
		case <-ctx.Done():
			return errors.Join(admissionErr, ctx.Err())
		}
	}
	select {
	case <-drained:
	case <-ctx.Done():
		return errors.Join(admissionErr, ctx.Err())
	}

	return errors.Join(admissionErr, worker.shutdownResource(ctx))
}

func (worker *LifecycleWorker[R]) shutdownResource(ctx context.Context) error {
	worker.mu.Lock()
	if worker.shutdownAttempt != nil {
		attempt := worker.shutdownAttempt
		worker.mu.Unlock()
		select {
		case <-attempt.done:
			return attempt.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	attempt := &shutdownAttempt{done: make(chan struct{})}
	worker.shutdownAttempt = attempt
	worker.mu.Unlock()

	err := invokeCallback(CallbackShutdown, func() error {
		return worker.shutdown(ctx, worker.resource)
	})
	worker.mu.Lock()
	attempt.err = err
	close(attempt.done)
	worker.mu.Unlock()

	return err
}

// WorkerOptions configure one concrete queue worker lifecycle.
type WorkerOptions struct {
	// Name is the secret-safe component name.
	Name string
	// Queue is the caller-constructed concrete queue.
	Queue *queue.Queue
}

// Worker retains one concrete queue.
type Worker struct {
	name  string
	queue *queue.Queue
}

// NewWorker validates and constructs an inert worker lifecycle adapter.
func NewWorker(options WorkerOptions) (*Worker, error) {
	if !validName(options.Name) {
		return nil, &OptionsError{
			Field: "Name", Reason: "must be valid UTF-8 within the configured bound",
		}
	}
	if options.Queue == nil {
		return nil, &OptionsError{Field: "Queue", Reason: "must not be nil"}
	}

	return &Worker{name: options.Name, queue: options.Queue}, nil
}

// Queue returns the exact caller-provided queue.
func (worker *Worker) Queue() *queue.Queue { return worker.queue }

// Component starts the queue and gracefully releases it during service stop.
func (worker *Worker) Component() service.Component {
	return service.Component{
		Name: worker.name,
		CloseAdmission: func() error {
			return invokeCallback(CallbackAdmission, worker.queue.CloseAdmission)
		},
		Start: func(context.Context) error {
			return invokeCallback(CallbackStartup, func() error {
				worker.queue.Start()

				return nil
			})
		},
		Stop: func(ctx context.Context) error {
			err := invokeCallback(CallbackShutdown, func() error {
				return worker.queue.ReleaseContext(ctx)
			})
			if errors.Is(err, queue.ErrWorkerShutdownPanic) {
				return errors.Join(
					err,
					&CallbackPanicError{Operation: CallbackShutdown},
				)
			}

			return err
		},
	}
}

func withTransportMetadata(
	options []job.AllowOption,
	correlationCarrier map[string]string,
	traceCarrier propagation.MapCarrier,
) []job.AllowOption {
	copied := append([]job.AllowOption(nil), options...)
	if len(copied) == 0 {
		copied = []job.AllowOption{{}}
	}
	metadata := cloneMetadata(copied[0].Metadata)
	if metadata == nil {
		metadata = &job.Metadata{}
	}
	metadata.Correlation = make(map[string]string, len(correlationCarrier))
	for key, value := range correlationCarrier {
		metadata.Correlation[key] = value
	}
	if len(traceCarrier) == 0 {
		metadata.TraceContext = nil
	} else {
		metadata.TraceContext = make(map[string]string, len(traceCarrier))
		for key, value := range traceCarrier {
			metadata.TraceContext[key] = value
		}
	}
	copied[0].Metadata = metadata

	return copied
}

func cloneMetadata(metadata *job.Metadata) *job.Metadata {
	if metadata == nil {
		return nil
	}
	clone := *metadata
	if metadata.EnqueuedAt != nil {
		value := *metadata.EnqueuedAt
		clone.EnqueuedAt = &value
	}
	if metadata.Tags != nil {
		clone.Tags = make(map[string]string, len(metadata.Tags))
		for key, value := range metadata.Tags {
			clone.Tags[key] = value
		}
	}
	if metadata.TraceContext != nil {
		clone.TraceContext = make(map[string]string, len(metadata.TraceContext))
		for key, value := range metadata.TraceContext {
			clone.TraceContext[key] = value
		}
	}
	return &clone
}

func removeEmptyFields(carrier propagation.MapCarrier) {
	for key, value := range carrier {
		if strings.TrimSpace(value) == "" {
			delete(carrier, key)
		}
	}
}

func nilValue(value any) bool {
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

func validName(name string) bool {
	return len(name) <= MaxNameBytes &&
		utf8.ValidString(name) &&
		strings.TrimSpace(name) != ""
}

func callbackOperationName(operation CallbackOperation) string {
	switch operation {
	case CallbackStartup:
		return "startup"
	case CallbackReadiness:
		return "readiness"
	case CallbackPublish:
		return "publish"
	case CallbackHandler:
		return "handler"
	case CallbackRun:
		return "run"
	case CallbackShutdown:
		return "shutdown"
	case CallbackAdmission:
		return "admission"
	default:
		return "unknown"
	}
}

func publishAcceptanceName(acceptance PublishAcceptance) string {
	switch acceptance {
	case PublishNotAccepted:
		return "not-accepted"
	case PublishAccepted:
		return "accepted"
	case PublishUnknown:
		return "unknown"
	default:
		return "invalid"
	}
}

func invokeCallback(
	operation CallbackOperation,
	callback func() error,
) (err error) {
	defer func() {
		if recover() != nil {
			err = &CallbackPanicError{Operation: operation}
		}
	}()

	err = callback()
	if err != nil {
		return &CallbackError{Operation: operation, Err: err}
	}

	return nil
}

func invokePublishCallback(
	callback func() (PublishAcceptance, error),
) (acceptance PublishAcceptance, err error) {
	defer func() {
		if recover() != nil {
			acceptance = PublishUnknown
			err = &CallbackPanicError{Operation: CallbackPublish}
		}
	}()

	acceptance, err = callback()
	if err != nil {
		return acceptance, &CallbackError{Operation: CallbackPublish, Err: err}
	}

	return acceptance, nil
}

func normalizePublishResult(
	acceptance PublishAcceptance,
	err error,
) (PublishAcceptance, error) {
	switch acceptance {
	case PublishAccepted:
		if err == nil {
			return acceptance, nil
		}
		return acceptance, &PublishError{Acceptance: acceptance, Err: err}
	case PublishNotAccepted:
		if err != nil {
			return acceptance, &PublishError{Acceptance: acceptance, Err: err}
		}
	case PublishUnknown:
		return acceptance, &PublishError{
			Acceptance: acceptance,
			Err:        errors.Join(ErrPublishOutcomeUnknown, err),
		}
	default:
		return PublishUnknown, &PublishError{
			Acceptance: PublishUnknown,
			Err:        errors.Join(ErrPublishOutcomeUnknown, ErrInvalidPublishAcceptance, err),
		}
	}

	return acceptance, &PublishError{
		Acceptance: acceptance,
		Err:        errors.Join(ErrInvalidPublishAcceptance, err),
	}
}
