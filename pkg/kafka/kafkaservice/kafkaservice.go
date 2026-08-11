// Package kafkaservice composes explicit Kafka resources with service
// lifecycle and correlation boundaries.
package kafkaservice

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/service"
	"go.opentelemetry.io/otel/propagation"
)

// MaxNameBytes bounds component, task, and readiness identifiers.
const MaxNameBytes = 128

// CallbackOperation identifies one application callback boundary.
type CallbackOperation uint8

const (
	// CallbackStartup identifies resource validation during service start.
	CallbackStartup CallbackOperation = iota + 1
	// CallbackReadiness identifies an opt-in dependency readiness check.
	CallbackReadiness
	// CallbackPublish identifies concrete producer publication.
	CallbackPublish
	// CallbackHandler identifies application record handling.
	CallbackHandler
	// CallbackRun identifies supervised consumer intake.
	CallbackRun
	// CallbackShutdown identifies transferred resource cleanup.
	CallbackShutdown
)

var (
	// ErrInvalidOptions identifies invalid adapter construction.
	ErrInvalidOptions = errors.New("invalid kafka service options")
	// ErrUnavailable reports a resource that is inactive or draining.
	ErrUnavailable = errors.New("kafka service resource unavailable")
	// ErrMissingCorrelation reports an outbound record without a parent
	// workflow.
	ErrMissingCorrelation = errors.New("kafka service producer correlation missing")
	// ErrCallbackPanic reports a recovered application callback panic.
	ErrCallbackPanic = errors.New("kafka service callback panicked")
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
		"kafka service %s callback panicked",
		callbackOperationName(err.Operation),
	)
}

// Unwrap exposes the stable panic classification.
func (err *CallbackPanicError) Unwrap() error { return ErrCallbackPanic }

// CallbackError preserves an application callback failure without formatting
// its potentially sensitive cause.
type CallbackError struct {
	// Operation identifies the callback boundary.
	Operation CallbackOperation
	// Err is the original callback failure.
	Err error
}

// Error returns a secret-safe callback failure diagnostic.
func (err *CallbackError) Error() string {
	return fmt.Sprintf(
		"kafka service %s callback failed",
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
		return "kafka service startup validation and cleanup failed"
	}

	return "kafka service startup validation failed"
}

// Unwrap preserves both causes for errors.Is and errors.As.
func (err *StartupError) Unwrap() []error {
	causes := []error{err.Validation}
	if err.Cleanup != nil {
		causes = append(causes, err.Cleanup)
	}

	return causes
}

// Startup validates an explicitly constructed producer before admission.
type Startup[R any] func(context.Context, R) error

// Check evaluates whether a producer can accept new work.
type Check[R any] func(context.Context, R) error

// Publish sends one concrete Kafka record.
type Publish[R any] func(
	context.Context,
	R,
	kafka.ProducerRecord,
) (kafka.DeliveryResult, error)

// Shutdown flushes and closes an explicitly transferred Kafka resource. It
// must permit another attempt after returning an error.
type Shutdown[R any] func(context.Context, R) error

// Run consumes records until its context is canceled or the resource fails.
type Run[R any] func(context.Context, R, kafka.Handler) error

// ProducerOptions configure one Kafka producer lifecycle adapter.
type ProducerOptions[R any] struct {
	// Name is the secret-safe component and readiness name.
	Name string
	// Resource is the caller-constructed concrete producer.
	Resource R
	// Correlation creates a new request ID for every produced record.
	Correlation *correlation.Factory
	// CorrelationCodec configures transport field names and bounds.
	CorrelationCodec correlation.CodecOptions
	// TracePropagator injects caller-owned trace context when non-nil.
	TracePropagator propagation.TextMapPropagator
	// MessageLimits bounds the record before copying and again after metadata
	// propagation. The zero value selects kafka.DefaultMessageLimits.
	MessageLimits kafka.MessageLimits
	// Startup optionally validates Resource before admission begins.
	Startup Startup[R]
	// Readiness optionally checks Resource after successful startup.
	Readiness Check[R]
	// Publish sends through Resource.
	Publish Publish[R]
	// Shutdown transfers flush and close ownership when non-nil. Failed calls
	// may be retried with a later service stop context.
	Shutdown Shutdown[R]
}

// HandlerOptions configure a correlation-aware Kafka delivery boundary.
type HandlerOptions struct {
	// Correlation creates a new request ID for every delivery attempt.
	Correlation *correlation.Factory
	// CorrelationCodec configures transport field names and bounds.
	CorrelationCodec correlation.CodecOptions
	// TrustedMetadata preserves valid inbound correlation only when true.
	TrustedMetadata bool
	// RejectInvalidMetadata rejects malformed correlation instead of replacing
	// it with a new local workflow.
	RejectInvalidMetadata bool
	// TracePropagator extracts caller-owned trace context when non-nil.
	TracePropagator propagation.TextMapPropagator
	// Handler performs application-owned record processing.
	Handler kafka.Handler
}

// ConsumerOptions configure one Kafka consumer lifecycle adapter.
type ConsumerOptions[R any] struct {
	// Name is the secret-safe component, task, and readiness name.
	Name string
	// Resource is the caller-constructed concrete consumer.
	Resource R
	// Correlation creates a new request ID for every consumed record.
	Correlation *correlation.Factory
	// CorrelationCodec configures transport field names and bounds.
	CorrelationCodec correlation.CodecOptions
	// TrustedMetadata preserves valid inbound correlation only when true.
	TrustedMetadata bool
	// RejectInvalidMetadata rejects malformed correlation instead of replacing
	// it with a new local workflow.
	RejectInvalidMetadata bool
	// TracePropagator extracts caller-owned trace context when non-nil.
	TracePropagator propagation.TextMapPropagator
	// Handler performs application-owned record processing.
	Handler kafka.Handler
	// Startup optionally validates Resource before consumption begins.
	Startup Startup[R]
	// Readiness optionally checks Resource after successful startup.
	Readiness Check[R]
	// Run starts intake and joins admitted handlers before returning.
	Run Run[R]
	// Shutdown stops intake, joins deliveries, and closes Resource. Failed
	// calls may be retried with a later service stop context.
	Shutdown Shutdown[R]
}

// Producer retains an explicit concrete producer and its lifecycle policy.
type Producer[R any] struct {
	name        string
	resource    R
	propagation *correlation.Propagator
	trace       propagation.TextMapPropagator
	limits      kafka.MessageLimits
	startup     Startup[R]
	readiness   Check[R]
	publish     Publish[R]
	shutdown    Shutdown[R]

	mu               sync.Mutex
	active           bool
	stopping         bool
	inflight         int
	drained          chan struct{}
	startupAttempt   *startupAttempt
	shutdownAttempt  *shutdownAttempt
	shutdownComplete bool
}

// Consumer retains an explicit concrete consumer and its lifecycle policy.
type Consumer[R any] struct {
	name      string
	resource  R
	handler   kafka.Handler
	startup   Startup[R]
	readiness Check[R]
	run       Run[R]
	shutdown  Shutdown[R]

	mu               sync.RWMutex
	active           bool
	stopping         bool
	inflight         int
	drained          chan struct{}
	startupAttempt   *startupAttempt
	shutdownAttempt  *shutdownAttempt
	shutdownComplete bool
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
	if options.Publish == nil {
		return nil, &OptionsError{Field: "Publish", Reason: "must not be nil"}
	}
	limits := options.MessageLimits
	if limits == (kafka.MessageLimits{}) {
		limits = kafka.DefaultMessageLimits()
	} else if err := limits.Validate(); err != nil {
		return nil, &OptionsError{
			Field: "MessageLimits", Reason: "must be valid and bounded",
		}
	}
	codec, err := correlation.NewCodec(options.CorrelationCodec)
	if err != nil {
		return nil, err
	}
	propagator, _ := correlation.NewPropagator(options.Correlation, codec)

	return &Producer[R]{
		name: options.Name, resource: options.Resource,
		propagation: propagator, trace: options.TracePropagator,
		limits:  limits,
		startup: options.Startup, readiness: options.Readiness,
		publish: options.Publish, shutdown: options.Shutdown,
	}, nil
}

// NewConsumer validates and constructs an inert consumer adapter.
func NewConsumer[R any](options ConsumerOptions[R]) (*Consumer[R], error) {
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
		Correlation:           options.Correlation,
		CorrelationCodec:      options.CorrelationCodec,
		TrustedMetadata:       options.TrustedMetadata,
		RejectInvalidMetadata: options.RejectInvalidMetadata,
		TracePropagator:       options.TracePropagator,
		Handler:               options.Handler,
	})
	if err != nil {
		return nil, err
	}

	return &Consumer[R]{
		name: options.Name, resource: options.Resource, handler: handler,
		startup: options.Startup, readiness: options.Readiness,
		run: options.Run, shutdown: options.Shutdown,
	}, nil
}

// Resource returns the exact caller-provided producer.
func (producer *Producer[R]) Resource() R { return producer.resource }

// Component returns the producer's ordered service lifecycle component.
func (producer *Producer[R]) Component() service.Component {
	return service.Component{
		Name:           producer.name,
		CloseAdmission: producer.closeAdmission,
		Start:          producer.start,
		Stop:           producer.stop,
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

// Resource returns the exact caller-provided consumer.
func (consumer *Consumer[R]) Resource() R { return consumer.resource }

// Plan returns the consumer component, supervised task, and optional
// readiness check. Service task cancellation stops intake and joins handlers
// before reverse-order component shutdown closes the resource.
func (consumer *Consumer[R]) Plan() service.Plan {
	plan := service.Plan{
		Components: []service.Component{{
			Name:           consumer.name,
			CloseAdmission: consumer.closeAdmission,
			Start:          consumer.start,
			Stop:           consumer.stop,
		}},
		Tasks: []service.Task{{
			Name: consumer.name,
			Run:  consumer.runTask,
		}},
	}
	if consumer.readiness != nil {
		plan.Readiness = []service.ReadinessCheck{{
			Name: consumer.name,
			Run:  consumer.checkReadiness,
		}}
	}

	return plan
}

// Publish creates a child hop, injects correlation and optional trace fields
// into an owned record copy, and sends it through the concrete producer.
func (producer *Producer[R]) Publish(
	ctx context.Context,
	record kafka.ProducerRecord,
) (
	values correlation.Values,
	delivery kafka.DeliveryResult,
	err error,
) {
	if !producer.beginUse() {
		return correlation.Values{}, kafka.DeliveryResult{}, ErrUnavailable
	}
	defer producer.finishUse()
	defer func() {
		if recover() != nil {
			delivery = kafka.DeliveryResult{}
			err = &CallbackPanicError{Operation: CallbackPublish}
		}
	}()

	parent, ok := correlation.FromContext(ctx)
	if !ok {
		return correlation.Values{}, kafka.DeliveryResult{}, ErrMissingCorrelation
	}
	if err = record.Validate(producer.limits); err != nil {
		return correlation.Values{}, kafka.DeliveryResult{}, err
	}
	owned := cloneRecord(record)
	carrier := headerCarrier{headers: &owned.Headers}
	values, err = producer.propagation.Send(&carrier, parent)
	if err != nil {
		return correlation.Values{}, kafka.DeliveryResult{}, err
	}
	if producer.trace != nil {
		carrier.remove(producer.trace.Fields())
		producer.trace.Inject(ctx, &carrier)
	}
	if err = owned.Validate(producer.limits); err != nil {
		return values, kafka.DeliveryResult{}, err
	}
	delivery, err = producer.publish(
		correlation.WithValues(ctx, values),
		producer.resource,
		owned,
	)
	if err != nil {
		err = &CallbackError{Operation: CallbackPublish, Err: err}
	}

	return values, delivery, err
}

// NewHandler wraps application work with a fresh Kafka delivery-attempt
// request ID. Inbound correlation is preserved only when TrustedMetadata is
// explicitly enabled.
func NewHandler(options HandlerOptions) (kafka.Handler, error) {
	if options.Correlation == nil {
		return nil, &OptionsError{Field: "Correlation", Reason: "must not be nil"}
	}
	if options.TracePropagator != nil && nilValue(options.TracePropagator) {
		return nil, &OptionsError{Field: "TracePropagator", Reason: "must not be nil"}
	}
	if nilValue(options.Handler) {
		return nil, &OptionsError{Field: "Handler", Reason: "must not be nil"}
	}
	codec, err := correlation.NewCodec(options.CorrelationCodec)
	if err != nil {
		return nil, err
	}
	propagator, _ := correlation.NewPropagator(options.Correlation, codec)

	return kafka.HandlerFunc(func(
		ctx context.Context,
		record kafka.ConsumedMessage,
	) (err error) {
		defer func() {
			if recover() != nil {
				err = &CallbackPanicError{Operation: CallbackHandler}
			}
		}()

		headers := cloneHeaders(record.Headers)
		carrier := headerCarrier{headers: &headers}
		values, receiveErr := propagator.Receive(
			&carrier,
			correlation.InboundPolicy{
				TrustCorrelation:        options.TrustedMetadata,
				TrustRequestAsCausation: options.TrustedMetadata,
			},
		)
		if receiveErr != nil && !options.RejectInvalidMetadata {
			empty := []kafka.Header{}
			values, receiveErr = propagator.Receive(
				&headerCarrier{headers: &empty},
				correlation.InboundPolicy{},
			)
		}
		if receiveErr != nil {
			return receiveErr
		}
		if options.TracePropagator != nil {
			ctx = options.TracePropagator.Extract(ctx, &carrier)
		}

		err = options.Handler.Handle(correlation.WithValues(ctx, values), record)
		if err != nil {
			return &CallbackError{Operation: CallbackHandler, Err: err}
		}

		return nil
	}), nil
}

func (consumer *Consumer[R]) start(ctx context.Context) error {
	consumer.mu.Lock()
	if consumer.stopping {
		consumer.mu.Unlock()

		return ErrUnavailable
	}
	if consumer.active {
		consumer.mu.Unlock()

		return nil
	}
	if consumer.startupAttempt != nil {
		consumer.mu.Unlock()

		return ErrUnavailable
	}
	var attempt *startupAttempt
	var attemptDone chan struct{}
	if consumer.startup != nil {
		attemptDone = make(chan struct{})
		attempt = &startupAttempt{done: attemptDone}
		consumer.startupAttempt = attempt
	}
	consumer.mu.Unlock()
	if consumer.startup != nil {
		if err := invokeCallback(CallbackStartup, func() error {
			return consumer.startup(ctx, consumer.resource)
		}); err != nil {
			consumer.mu.Lock()
			consumer.stopping = true
			consumer.startupAttempt = nil
			close(attemptDone)
			consumer.mu.Unlock()

			return &StartupError{
				Validation: err,
				Cleanup:    consumer.shutdownResource(ctx),
			}
		}
	}
	consumer.mu.Lock()
	defer consumer.mu.Unlock()
	if attempt != nil {
		consumer.startupAttempt = nil
		close(attempt.done)
	}
	if consumer.stopping {
		return ErrUnavailable
	}
	consumer.active = true

	return nil
}

func (consumer *Consumer[R]) runTask(ctx context.Context) error {
	if !consumer.beginUse() {
		return ErrUnavailable
	}
	defer consumer.finishUse()

	return invokeCallback(CallbackRun, func() error {
		return consumer.run(ctx, consumer.resource, consumer.handler)
	})
}

func (consumer *Consumer[R]) checkReadiness(ctx context.Context) error {
	if !consumer.beginUse() {
		return ErrUnavailable
	}
	defer consumer.finishUse()

	return invokeCallback(CallbackReadiness, func() error {
		return consumer.readiness(ctx, consumer.resource)
	})
}

func (consumer *Consumer[R]) closeAdmission() error {
	consumer.mu.Lock()
	consumer.active = false
	consumer.stopping = true
	consumer.mu.Unlock()

	return nil
}

func (consumer *Consumer[R]) stop(ctx context.Context) error {
	consumer.mu.Lock()
	consumer.active = false
	consumer.stopping = true
	startup := consumer.startupAttempt
	if consumer.drained == nil {
		consumer.drained = make(chan struct{})
		if consumer.inflight == 0 {
			close(consumer.drained)
		}
	}
	drained := consumer.drained
	consumer.mu.Unlock()
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

	return consumer.shutdownResource(ctx)
}

func (consumer *Consumer[R]) shutdownResource(ctx context.Context) error {
	consumer.mu.Lock()
	if consumer.shutdownComplete {
		consumer.mu.Unlock()

		return nil
	}
	if consumer.shutdownAttempt != nil {
		attempt := consumer.shutdownAttempt
		consumer.mu.Unlock()

		select {
		case <-attempt.done:
			return attempt.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	attempt := &shutdownAttempt{done: make(chan struct{})}
	consumer.shutdownAttempt = attempt
	consumer.mu.Unlock()

	err := invokeCallback(CallbackShutdown, func() error {
		return consumer.shutdown(ctx, consumer.resource)
	})
	consumer.mu.Lock()
	attempt.err = err
	consumer.shutdownAttempt = nil
	consumer.shutdownComplete = err == nil
	close(attempt.done)
	consumer.mu.Unlock()

	return err
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
	var attemptDone chan struct{}
	if producer.startup != nil {
		attemptDone = make(chan struct{})
		attempt = &startupAttempt{done: attemptDone}
		producer.startupAttempt = attempt
	}
	producer.mu.Unlock()
	if producer.startup != nil {
		if err := invokeCallback(CallbackStartup, func() error {
			return producer.startup(ctx, producer.resource)
		}); err != nil {
			if producer.shutdown != nil {
				producer.mu.Lock()
				producer.stopping = true
				producer.startupAttempt = nil
				close(attemptDone)
				producer.mu.Unlock()

				return &StartupError{
					Validation: err,
					Cleanup:    producer.shutdownResource(ctx),
				}
			}

			producer.mu.Lock()
			producer.startupAttempt = nil
			close(attemptDone)
			producer.mu.Unlock()

			return &StartupError{Validation: err}
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

func (producer *Producer[R]) beginUse() bool {
	producer.mu.Lock()
	defer producer.mu.Unlock()
	if !producer.active || producer.stopping {
		return false
	}
	producer.inflight++

	return true
}

func (producer *Producer[R]) closeAdmission() error {
	producer.mu.Lock()
	producer.active = false
	producer.stopping = true
	producer.mu.Unlock()

	return nil
}

func (producer *Producer[R]) finishUse() {
	producer.mu.Lock()
	defer producer.mu.Unlock()
	producer.inflight--
	if producer.inflight == 0 && producer.drained != nil {
		close(producer.drained)
		producer.drained = nil
	}
}

func (consumer *Consumer[R]) beginUse() bool {
	consumer.mu.Lock()
	defer consumer.mu.Unlock()
	if !consumer.active || consumer.stopping {
		return false
	}
	consumer.inflight++

	return true
}

func (consumer *Consumer[R]) finishUse() {
	consumer.mu.Lock()
	defer consumer.mu.Unlock()
	consumer.inflight--
	if consumer.inflight == 0 && consumer.drained != nil {
		close(consumer.drained)
		consumer.drained = nil
	}
}

func (producer *Producer[R]) stop(ctx context.Context) error {
	producer.mu.Lock()
	producer.active = false
	producer.stopping = true
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
	if producer.shutdownComplete {
		producer.mu.Unlock()

		return nil
	}
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
	producer.shutdownAttempt = nil
	producer.shutdownComplete = err == nil
	close(attempt.done)
	producer.mu.Unlock()

	return err
}

type headerCarrier struct {
	headers *[]kafka.Header
}

func (carrier *headerCarrier) Get(key string) string {
	values := carrier.Values(key)
	if len(values) == 0 {
		return ""
	}

	return values[len(values)-1]
}

func (carrier *headerCarrier) Set(key, value string) {
	carrier.remove([]string{key})
	*carrier.headers = append(*carrier.headers, kafka.Header{
		Key: key, Value: []byte(value),
	})
}

func (carrier *headerCarrier) Keys() []string {
	keys := make([]string, 0, len(*carrier.headers))
	for _, header := range *carrier.headers {
		keys = append(keys, header.Key)
	}

	return keys
}

func (carrier *headerCarrier) Values(key string) []string {
	var values []string
	for _, header := range *carrier.headers {
		if strings.EqualFold(header.Key, key) {
			values = append(values, string(header.Value))
		}
	}

	return values
}

func (carrier *headerCarrier) remove(fields []string) {
	headers := *carrier.headers
	retained := headers[:0]
	for _, header := range headers {
		remove := slices.ContainsFunc(fields, func(field string) bool {
			return strings.EqualFold(header.Key, field)
		})
		if !remove {
			retained = append(retained, header)
		}
	}
	*carrier.headers = retained
}

func cloneRecord(record kafka.ProducerRecord) kafka.ProducerRecord {
	clone := record
	clone.Key = append([]byte(nil), record.Key...)
	clone.Value = append([]byte(nil), record.Value...)
	clone.Headers = cloneHeaders(record.Headers)

	return clone
}

func cloneHeaders(headers []kafka.Header) []kafka.Header {
	if headers == nil {
		return nil
	}

	clone := make([]kafka.Header, len(headers))
	for index, header := range headers {
		clone[index] = kafka.Header{
			Key: header.Key, Value: append([]byte(nil), header.Value...),
		}
	}

	return clone
}

func nilValue(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
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
	default:
		return "unknown"
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
