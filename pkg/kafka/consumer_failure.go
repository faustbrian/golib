package kafka

import (
	"context"
	"errors"
	"strconv"
	"time"
)

var (
	// ErrInvalidFailurePolicy identifies an incompatible or unbounded consumer
	// failure-handling configuration.
	ErrInvalidFailurePolicy = errors.New(
		"kafka: consumer failure policy is invalid",
	)
	// ErrInvalidFailureTarget identifies an invalid, unversioned, or
	// source-equal retry or dead-letter topic.
	ErrInvalidFailureTarget = errors.New(
		"kafka: consumer failure target is invalid",
	)
	// ErrFailurePublisherRequired identifies a publish strategy without its
	// narrow record publisher.
	ErrFailurePublisherRequired = errors.New(
		"kafka: consumer failure publisher is required",
	)
	// ErrFailureDelegateRequired identifies a delegated strategy without its
	// application callback.
	ErrFailureDelegateRequired = errors.New(
		"kafka: consumer failure delegate is required",
	)
	// ErrConsumerFailureStopped identifies a handler failure deliberately left
	// unsettled for Kafka redelivery.
	ErrConsumerFailureStopped = errors.New(
		"kafka: consumer failure stopped without settlement",
	)
	// ErrFailureAttemptsExhausted identifies a bounded in-process retry policy
	// that reached its final handler attempt.
	ErrFailureAttemptsExhausted = errors.New(
		"kafka: consumer failure attempts exhausted",
	)
	// ErrFailureBackoff identifies cancellation or failure while waiting for a
	// bounded in-process retry.
	ErrFailureBackoff = errors.New(
		"kafka: consumer failure retry backoff interrupted",
	)
	// ErrFailurePublish identifies a retry-topic or dead-letter publication
	// that did not receive a definite successful producer result.
	ErrFailurePublish = errors.New(
		"kafka: consumer failure publication failed",
	)
	// ErrFailureDelegate identifies an application failure delegate that did
	// not resolve the source record.
	ErrFailureDelegate = errors.New(
		"kafka: consumer failure delegate failed",
	)
	// ErrFailureCallbackPanic identifies a contained classifier, publisher, or
	// delegate panic.
	ErrFailureCallbackPanic = errors.New(
		"kafka: consumer failure callback panicked",
	)
	// ErrInvalidFailureClassification identifies a classifier result outside
	// the stable ErrorCategory set.
	ErrInvalidFailureClassification = errors.New(
		"kafka: consumer failure classification is invalid",
	)
	// ErrFailureRecordInvalid identifies a source, retry, or dead-letter record
	// whose Kafka metadata or material violates the configured bounded policy.
	ErrFailureRecordInvalid = errors.New(
		"kafka: consumer failure record is invalid",
	)
)

// FailureMode selects the terminal action after the original handler and any
// bounded in-process attempts fail.
type FailureMode uint8

const (
	// FailureModeStop returns a redacted error and leaves the source offset
	// unsettled. This is the zero value and preserves at-least-once redelivery.
	FailureModeStop FailureMode = iota
	// FailureModeRetryTopic publishes an owned copy to one explicit versioned retry
	// topic. A successful publish resolves the handler so the caller may settle
	// the source offset as a separate Kafka effect.
	FailureModeRetryTopic
	// FailureModeDeadLetter publishes an owned copy to one explicit versioned
	// dead-letter topic. Source settlement remains a separate Kafka effect.
	FailureModeDeadLetter
	// FailureModeDelegate transfers the terminal decision to one synchronous
	// application callback. A nil delegate result explicitly resolves the
	// source handler; an error leaves it unsettled.
	FailureModeDelegate
)

// FailureRetryPolicy bounds optional in-process handler retries. MaxAttempts
// includes the initial handler call. The zero value performs one attempt.
type FailureRetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Categories     []ErrorCategory
}

// FailureTarget identifies one explicit versioned retry or dead-letter topic.
// Version describes the application's topic/envelope contract version and is
// propagated in package-owned failure metadata.
type FailureTarget struct {
	Topic   string
	Version uint16
}

// HandlerFailure is the synchronous failure-policy input. Record bytes remain
// borrowed for the callback unless Retain is called. Cause is deliberately
// omitted from formatting and telemetry by the package.
type HandlerFailure struct {
	Record   ConsumedRecord
	Attempt  int
	Category ErrorCategory

	cause error
}

// Cause returns the original handler error for programmatic application
// decisions. Callers must not render it without applying their own redaction.
func (failure HandlerFailure) Cause() error {
	return failure.cause
}

// Retain returns a failure whose record bytes are deeply copied. Error identity
// is immutable by convention and is retained without wrapping.
func (failure HandlerFailure) Retain() HandlerFailure {
	failure.Record = failure.Record.Retain()

	return failure
}

// FailureClassifier maps an application handler error to one stable,
// low-cardinality operational category. Implementations must be synchronous,
// bounded, concurrency-safe, and must not render record data or credentials.
type FailureClassifier interface {
	ClassifyFailure(error) ErrorCategory
}

// FailureClassifierFunc adapts a function to FailureClassifier.
type FailureClassifierFunc func(error) ErrorCategory

// ClassifyFailure invokes classifier.
func (classifier FailureClassifierFunc) ClassifyFailure(err error) ErrorCategory {
	return classifier(err)
}

// FailureDelegate owns a terminal application-specific failure decision. A
// nil result declares the source record resolved and permits normal consumer
// settlement. An error leaves the source record unsettled.
type FailureDelegate interface {
	HandleFailure(context.Context, HandlerFailure) error
}

// FailureDelegateFunc adapts a function to FailureDelegate.
type FailureDelegateFunc func(context.Context, HandlerFailure) error

// HandleFailure invokes delegate.
func (delegate FailureDelegateFunc) HandleFailure(
	ctx context.Context,
	failure HandlerFailure,
) error {
	return delegate(ctx, failure)
}

// FailurePublisher is the narrow publication seam used for non-transactional
// retry and dead-letter topics. Producer satisfies this interface. A custom
// implementation owns the supplied record and must return one definite
// delivery result before returning.
type FailurePublisher interface {
	PublishRecord(context.Context, ProducerRecord) DeliveryResult
}

// FailureHandlerConfig defines a bounded failure-policy handler decorator.
// Configuration is validated and copied before construction. Callback
// implementations retain their own lifetime and concurrency ownership.
type FailureHandlerConfig struct {
	Handler        Handler
	Classifier     FailureClassifier
	Retry          FailureRetryPolicy
	Mode           FailureMode
	Target         FailureTarget
	Publisher      FailurePublisher
	Delegate       FailureDelegate
	Limits         MessageLimits
	PublishTimeout time.Duration
}

// Validate reports whether the failure policy is explicit, compatible, and
// bounded without constructing a handler.
func (config FailureHandlerConfig) Validate() error {
	_, err := normalizeFailureHandlerConfig(config)

	return err
}

// FailureStage identifies the bounded phase that failed. It is stable and
// low-cardinality.
type FailureStage uint8

const (
	FailureStageStop FailureStage = iota + 1
	FailureStageClassify
	FailureStageBackoff
	FailureStagePublish
	FailureStageDelegate
)

// String returns the stable failure-stage name.
func (stage FailureStage) String() string {
	switch stage {
	case FailureStageStop:
		return "stop"
	case FailureStageClassify:
		return "classify"
	case FailureStageBackoff:
		return "backoff"
	case FailureStagePublish:
		return "publish"
	case FailureStageDelegate:
		return "delegate"
	default:
		return "unknown"
	}
}

// FailureHandlingError reports a redacted failure-policy outcome while
// preserving sentinel, handler, and operation error identity through Unwrap.
type FailureHandlingError struct {
	stage    FailureStage
	category ErrorCategory
	attempt  int
	causes   []error
	delivery []DeliveryResult
}

// Error returns a stable diagnostic that excludes topics, keys, payloads,
// headers, credentials, and callback error text.
func (err *FailureHandlingError) Error() string {
	if err == nil {
		return "kafka: consumer failure handling failed"
	}

	return "kafka: consumer failure " + err.stage.String() + " " +
		err.category.String() + " failure"
}

// Unwrap preserves programmatic error identity without rendering causes.
func (err *FailureHandlingError) Unwrap() []error {
	if err == nil {
		return nil
	}

	return append([]error(nil), err.causes...)
}

// Stage returns the bounded phase that failed.
func (err *FailureHandlingError) Stage() FailureStage {
	if err == nil {
		return 0
	}

	return err.stage
}

// Category returns the original handler failure classification.
func (err *FailureHandlingError) Category() ErrorCategory {
	if err == nil {
		return 0
	}

	return err.category
}

// Attempt returns the last handler attempt involved in this outcome.
func (err *FailureHandlingError) Attempt() int {
	if err == nil {
		return 0
	}

	return err.attempt
}

// DeliveryResults returns owned input-ordered retry-topic or dead-letter
// delivery outcomes when a whole-batch publication failed. Other failure
// outcomes return nil.
func (err *FailureHandlingError) DeliveryResults() []DeliveryResult {
	if err == nil || err.delivery == nil {
		return nil
	}

	return append([]DeliveryResult(nil), err.delivery...)
}

type failureWait func(context.Context, time.Duration) error

type failureHandler struct {
	handler         Handler
	classifier      FailureClassifier
	retry           FailureRetryPolicy
	retryCategories map[ErrorCategory]struct{}
	mode            FailureMode
	target          FailureTarget
	publisher       FailurePublisher
	delegate        FailureDelegate
	limits          MessageLimits
	publishTimeout  time.Duration
	wait            failureWait
}

const (
	defaultFailurePublishTimeout = 10 * time.Second
	maximumFailureAttempts       = 32
	maximumFailureBackoff        = 5 * time.Minute
)

// NewFailureHandler constructs a reusable handler decorator implementing
// explicit stop, bounded retry, retry-topic, dead-letter, or delegated failure
// policy. Each invocation validates the source metadata and record limits
// before retaining bytes or calling the wrapped handler, then gives every
// attempt an isolated copy so handler mutation cannot alter later attempts or
// failure publication. Construction allocates no durable resources.
func NewFailureHandler(config FailureHandlerConfig) (Handler, error) {
	handler, err := newFailureHandler(config, waitFailureBackoff)
	if err != nil {
		return nil, err
	}

	return handler, nil
}

func newFailureHandler(
	config FailureHandlerConfig,
	wait failureWait,
) (*failureHandler, error) {
	normalized, err := normalizeFailureHandlerConfig(config)
	if err != nil {
		return nil, err
	}

	categories := make(
		map[ErrorCategory]struct{},
		len(normalized.Retry.Categories),
	)
	for _, category := range normalized.Retry.Categories {
		categories[category] = struct{}{}
	}

	return &failureHandler{
		handler:         normalized.Handler,
		classifier:      normalized.Classifier,
		retry:           normalized.Retry,
		retryCategories: categories,
		mode:            normalized.Mode,
		target:          normalized.Target,
		publisher:       normalized.Publisher,
		delegate:        normalized.Delegate,
		limits:          normalized.Limits,
		publishTimeout:  normalized.PublishTimeout,
		wait:            wait,
	}, nil
}

func normalizeFailureHandlerConfig(
	config FailureHandlerConfig,
) (FailureHandlerConfig, error) {
	if config.Handler == nil {
		return FailureHandlerConfig{}, ErrHandlerRequired
	}
	if config.Mode > FailureModeDelegate {
		return FailureHandlerConfig{}, ErrInvalidFailurePolicy
	}
	if config.Limits == (MessageLimits{}) {
		config.Limits = DefaultMessageLimits()
	}
	if err := config.Limits.Validate(); err != nil {
		return FailureHandlerConfig{}, err
	}
	var err error
	config.Retry, err = normalizeFailureRetryPolicy(config.Retry)
	if err != nil {
		return FailureHandlerConfig{}, err
	}

	switch config.Mode {
	case FailureModeStop:
		if config.Target != (FailureTarget{}) ||
			config.Publisher != nil ||
			config.Delegate != nil ||
			config.PublishTimeout != 0 {
			return FailureHandlerConfig{}, ErrInvalidFailurePolicy
		}
	case FailureModeRetryTopic, FailureModeDeadLetter:
		if config.Publisher == nil {
			return FailureHandlerConfig{}, ErrFailurePublisherRequired
		}
		if config.Delegate != nil {
			return FailureHandlerConfig{}, ErrInvalidFailurePolicy
		}
		if config.Target.Version == 0 ||
			!validKafkaTopicName(
				config.Target.Topic,
				config.Limits.MaxTopicBytes,
			) {
			return FailureHandlerConfig{}, ErrInvalidFailureTarget
		}
		if config.PublishTimeout == 0 {
			config.PublishTimeout = defaultFailurePublishTimeout
		}
		if config.PublishTimeout < 100*time.Millisecond ||
			config.PublishTimeout > 2*time.Minute {
			return FailureHandlerConfig{}, ErrInvalidFailurePolicy
		}
	case FailureModeDelegate:
		if config.Delegate == nil {
			return FailureHandlerConfig{}, ErrFailureDelegateRequired
		}
		if config.Target != (FailureTarget{}) {
			return FailureHandlerConfig{}, ErrInvalidFailurePolicy
		}
		if config.Publisher != nil {
			return FailureHandlerConfig{}, ErrInvalidFailurePolicy
		}
		if config.PublishTimeout != 0 {
			return FailureHandlerConfig{}, ErrInvalidFailurePolicy
		}
	}

	return config, nil
}

func normalizeFailureRetryPolicy(
	policy FailureRetryPolicy,
) (FailureRetryPolicy, error) {
	if policy.MaxAttempts == 0 {
		policy.MaxAttempts = 1
	}
	if policy.MaxAttempts < 1 || policy.MaxAttempts > maximumFailureAttempts {
		return FailureRetryPolicy{}, ErrInvalidFailurePolicy
	}
	if policy.MaxAttempts == 1 {
		if policy.InitialBackoff != 0 {
			return FailureRetryPolicy{}, ErrInvalidFailurePolicy
		}
		if policy.MaxBackoff != 0 {
			return FailureRetryPolicy{}, ErrInvalidFailurePolicy
		}
		if len(policy.Categories) != 0 {
			return FailureRetryPolicy{}, ErrInvalidFailurePolicy
		}

		return policy, nil
	}
	if policy.InitialBackoff < time.Millisecond ||
		policy.MaxBackoff < policy.InitialBackoff ||
		policy.MaxBackoff > maximumFailureBackoff {
		return FailureRetryPolicy{}, ErrInvalidFailurePolicy
	}
	if len(policy.Categories) == 0 {
		policy.Categories = []ErrorCategory{ErrorRetryable}
	}
	seen := make(map[ErrorCategory]struct{}, len(policy.Categories))
	for _, category := range policy.Categories {
		if !validErrorCategory(category) {
			return FailureRetryPolicy{}, ErrInvalidFailurePolicy
		}
		if _, duplicate := seen[category]; duplicate {
			return FailureRetryPolicy{}, ErrInvalidFailurePolicy
		}
		seen[category] = struct{}{}
	}
	policy.Categories = append([]ErrorCategory(nil), policy.Categories...)

	return policy, nil
}

func validErrorCategory(category ErrorCategory) bool {
	return category >= ErrorPermanent && category <= ErrorFatal
}

// Handle applies the configured failure policy. The decorator is safe for
// concurrent calls when its supplied handler, classifier, publisher, and
// delegate implementations satisfy their documented concurrency contracts.
func (handler *failureHandler) Handle(
	ctx context.Context,
	record ConsumedMessage,
) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if err := validateFailureRecord(record, handler.limits); err != nil {
		return err
	}

	source := record.Retain()
	for attempt := 1; ; attempt++ {
		handlerErr := callHandler(ctx, handler.handler, source.Retain())
		if handlerErr == nil {
			return nil
		}

		category, classificationErr := handler.classify(handlerErr)
		if classificationErr != nil {
			return newFailureHandlingError(
				FailureStageClassify,
				ErrorPermanent,
				attempt,
				ErrFailureCallbackPanic,
				handlerErr,
				classificationErr,
			)
		}
		if !validErrorCategory(category) {
			return newFailureHandlingError(
				FailureStageClassify,
				ErrorPermanent,
				attempt,
				ErrInvalidFailureClassification,
				handlerErr,
			)
		}
		if cause := context.Cause(ctx); cause != nil {
			return newFailureHandlingError(
				FailureStageStop,
				category,
				attempt,
				ErrConsumerFailureStopped,
				handlerErr,
				cause,
			)
		}

		failure := HandlerFailure{
			Record:   source,
			Attempt:  attempt,
			Category: category,
			cause:    handlerErr,
		}
		if attempt < handler.retry.MaxAttempts &&
			handler.retryable(category) {
			delay := failureBackoff(handler.retry, attempt)
			if err := handler.wait(ctx, delay); err != nil {
				return newFailureHandlingError(
					FailureStageBackoff,
					category,
					attempt,
					ErrFailureBackoff,
					handlerErr,
					err,
				)
			}

			continue
		}

		return handler.resolve(ctx, failure)
	}
}

func validateFailureRecord(record ConsumedMessage, limits MessageLimits) error {
	if record.Partition < 0 || record.Offset < 0 || record.LeaderEpoch < -1 {
		return ErrFailureRecordInvalid
	}
	switch record.TimestampType {
	case TimestampUnknown, TimestampCreateTime, TimestampLogAppendTime:
	default:
		return ErrFailureRecordInvalid
	}

	err := (ProducerRecord{
		Topic:   record.Topic,
		Key:     record.Key,
		Value:   record.Value,
		Headers: record.Headers,
	}).validate(limits)
	if err != nil {
		return errors.Join(ErrFailureRecordInvalid, err)
	}

	return nil
}

func (handler *failureHandler) classify(err error) (
	category ErrorCategory,
	classificationErr error,
) {
	if handler.classifier == nil {
		return classifyError(err), nil
	}
	defer func() {
		if recover() != nil {
			classificationErr = ErrFailureCallbackPanic
		}
	}()

	return handler.classifier.ClassifyFailure(err), nil
}

func (handler *failureHandler) retryable(category ErrorCategory) bool {
	_, retryable := handler.retryCategories[category]

	return retryable
}

func failureBackoff(policy FailureRetryPolicy, attempt int) time.Duration {
	delay := policy.InitialBackoff
	for current := 1; current < attempt && delay != policy.MaxBackoff; current++ {
		delay = min(delay*2, policy.MaxBackoff)
	}
	return delay
}

func waitFailureBackoff(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

func (handler *failureHandler) resolve(
	ctx context.Context,
	failure HandlerFailure,
) error {
	switch handler.mode {
	case FailureModeStop:
		causes := []error{ErrConsumerFailureStopped, failure.cause}
		if failure.Attempt == handler.retry.MaxAttempts {
			if handler.retryable(failure.Category) {
				causes = append(causes, ErrFailureAttemptsExhausted)
			}
		}

		return newFailureHandlingError(
			FailureStageStop,
			failure.Category,
			failure.Attempt,
			causes...,
		)
	case FailureModeRetryTopic, FailureModeDeadLetter:
		return handler.publish(ctx, failure)
	case FailureModeDelegate:
		delegateErr := callFailureDelegate(ctx, handler.delegate, failure)
		if delegateErr == nil {
			return nil
		}

		return newFailureHandlingError(
			FailureStageDelegate,
			failure.Category,
			failure.Attempt,
			ErrFailureDelegate,
			failure.cause,
			delegateErr,
		)
	default:
		return newFailureHandlingError(
			FailureStageStop,
			failure.Category,
			failure.Attempt,
			ErrInvalidFailurePolicy,
			failure.cause,
		)
	}
}

func (handler *failureHandler) publish(
	ctx context.Context,
	failure HandlerFailure,
) error {
	if handler.target.Topic == failure.Record.Topic {
		return newFailureHandlingError(
			FailureStagePublish,
			failure.Category,
			failure.Attempt,
			ErrFailurePublish,
			ErrInvalidFailureTarget,
			failure.cause,
		)
	}

	record := handler.failureRecord(failure)
	if err := record.validate(handler.limits); err != nil {
		return newFailureHandlingError(
			FailureStagePublish,
			failure.Category,
			failure.Attempt,
			ErrFailurePublish,
			ErrFailureRecordInvalid,
			failure.cause,
			err,
		)
	}

	publishCtx, cancel := context.WithTimeout(ctx, handler.publishTimeout)
	result, callbackErr := callFailurePublisher(
		publishCtx,
		handler.publisher,
		record,
	)
	cancel()
	if callbackErr != nil {
		return newFailureHandlingError(
			FailureStagePublish,
			failure.Category,
			failure.Attempt,
			ErrFailurePublish,
			ErrFailureCallbackPanic,
			failure.cause,
			callbackErr,
		)
	}
	if result.Err != nil {
		return newFailureHandlingError(
			FailureStagePublish,
			failure.Category,
			failure.Attempt,
			ErrFailurePublish,
			failure.cause,
			result.Err,
		)
	}

	return nil
}

func (handler *failureHandler) failureRecord(
	failure HandlerFailure,
) ProducerRecord {
	return failureProducerRecord(handler.mode, handler.target, failure)
}

func failureProducerRecord(
	mode FailureMode,
	target FailureTarget,
	failure HandlerFailure,
) ProducerRecord {
	kind := "retry"
	if mode == FailureModeDeadLetter {
		kind = "dead-letter"
	}
	source := failure.Record
	headers := cloneHeaders(source.Headers)
	headers = append(headers,
		failureHeader("schema-version", "1"),
		failureHeader("kind", kind),
		failureHeader("target-version", strconv.FormatUint(
			uint64(target.Version),
			10,
		)),
		failureHeader("source-topic", source.Topic),
		failureHeader("source-partition", strconv.FormatInt(
			int64(source.Partition),
			10,
		)),
		failureHeader("source-offset", strconv.FormatInt(source.Offset, 10)),
		failureHeader(
			"source-timestamp",
			source.Timestamp.UTC().Format(time.RFC3339Nano),
		),
		failureHeader(
			"source-timestamp-type",
			failureTimestampType(source.TimestampType),
		),
		failureHeader("source-leader-epoch", strconv.FormatInt(
			int64(source.LeaderEpoch),
			10,
		)),
		failureHeader("attempt", strconv.Itoa(failure.Attempt)),
		failureHeader("error-category", failure.Category.String()),
	)

	return ProducerRecord{
		Topic:     target.Topic,
		Key:       cloneBytes(source.Key),
		Value:     cloneBytes(source.Value),
		Headers:   headers,
		Timestamp: source.Timestamp,
	}
}

func failureHeader(name string, value string) Header {
	return Header{
		Key:   "golib.kafka.failure." + name,
		Value: []byte(value),
	}
}

func failureTimestampType(timestampType TimestampType) string {
	switch timestampType {
	case TimestampCreateTime:
		return "create-time"
	case TimestampLogAppendTime:
		return "log-append-time"
	default:
		return "unknown"
	}
}

func callFailureDelegate(
	ctx context.Context,
	delegate FailureDelegate,
	failure HandlerFailure,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrFailureCallbackPanic
		}
	}()

	return delegate.HandleFailure(ctx, failure)
}

func callFailurePublisher(
	ctx context.Context,
	publisher FailurePublisher,
	record ProducerRecord,
) (result DeliveryResult, err error) {
	defer func() {
		if recover() != nil {
			err = ErrFailureCallbackPanic
		}
	}()

	return publisher.PublishRecord(ctx, record), nil
}

func newFailureHandlingError(
	stage FailureStage,
	category ErrorCategory,
	attempt int,
	causes ...error,
) *FailureHandlingError {
	filtered := make([]error, 0, len(causes))
	for _, cause := range causes {
		if cause != nil {
			filtered = append(filtered, cause)
		}
	}

	return &FailureHandlingError{
		stage:    stage,
		category: category,
		attempt:  attempt,
		causes:   filtered,
	}
}

func newFailureHandlingErrorWithDeliveries(
	stage FailureStage,
	category ErrorCategory,
	attempt int,
	deliveries []DeliveryResult,
	causes ...error,
) *FailureHandlingError {
	err := newFailureHandlingError(stage, category, attempt, causes...)
	err.delivery = append([]DeliveryResult(nil), deliveries...)

	return err
}
