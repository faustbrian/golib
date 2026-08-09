package kafka

import (
	"context"
	"errors"
	"strconv"
	"time"
)

var (
	// ErrInvalidFailureBatch identifies a batch that cannot be safely retained
	// or whose records do not match its ordered topic-partition coordinates.
	ErrInvalidFailureBatch = errors.New(
		"kafka: consumer failure batch is invalid",
	)
)

const (
	defaultFailureBatchRecords = 100
	maximumFailureBatchRecords = 1_000
	defaultFailureBatchBytes   = 16 << 20
	maximumFailureBatchBytes   = 100 << 20
)

// BatchFailure is the synchronous whole-partition-batch failure-policy input.
// Batch bytes remain borrowed for the callback unless Retain is called. Cause
// is deliberately omitted from formatting and telemetry by the package.
type BatchFailure struct {
	Batch    ConsumedBatch
	Attempt  int
	Category ErrorCategory

	cause error
}

// Cause returns the original batch-handler error for programmatic application
// decisions. Callers must not render it without applying their own redaction.
func (failure BatchFailure) Cause() error {
	return failure.cause
}

// Retain returns a failure whose complete batch is deeply copied. Error
// identity is immutable by convention and is retained without wrapping.
func (failure BatchFailure) Retain() BatchFailure {
	failure.Batch = failure.Batch.Retain()

	return failure
}

// BatchFailureDelegate owns one terminal application-specific decision for a
// complete partition batch. A nil result resolves every record and permits the
// consumer to settle the batch. An error leaves every record unsettled. The
// callback must be synchronous, bounded, cancellation-aware, and concurrency-
// safe when the consumer permits concurrent partition handlers.
type BatchFailureDelegate interface {
	HandleBatchFailure(context.Context, BatchFailure) error
}

// BatchFailureDelegateFunc adapts a function to BatchFailureDelegate.
type BatchFailureDelegateFunc func(context.Context, BatchFailure) error

// HandleBatchFailure invokes delegate.
func (delegate BatchFailureDelegateFunc) HandleBatchFailure(
	ctx context.Context,
	failure BatchFailure,
) error {
	return delegate(ctx, failure)
}

// BatchFailurePublisher is the narrow publication seam used to reroute a
// complete failed partition batch. Producer satisfies this interface. Results
// must remain input ordered and contain exactly one result per record. The
// publisher owns the supplied slice and record bytes and may retain them. It
// must be synchronous, bounded, cancellation-aware, and concurrency-safe when
// the consumer permits concurrent partition handlers.
type BatchFailurePublisher interface {
	PublishBatch(context.Context, []ProducerRecord) ([]DeliveryResult, error)
}

var _ BatchFailurePublisher = (*Producer)(nil)

// BatchFailurePublisherFunc adapts a function to BatchFailurePublisher.
type BatchFailurePublisherFunc func(
	context.Context,
	[]ProducerRecord,
) ([]DeliveryResult, error)

// PublishBatch invokes publisher.
func (publisher BatchFailurePublisherFunc) PublishBatch(
	ctx context.Context,
	records []ProducerRecord,
) ([]DeliveryResult, error) {
	return publisher(ctx, records)
}

// BatchFailureHandlerConfig defines a bounded whole-partition-batch failure
// decorator. Retry re-invokes the complete batch. Retry-topic and dead-letter
// modes publish the complete batch and resolve it only after every target
// delivery has a definite successful result. Configuration values are copied
// during construction; callbacks retain caller-owned lifetime and concurrency
// responsibilities.
type BatchFailureHandlerConfig struct {
	Handler         BatchHandler
	Classifier      FailureClassifier
	Retry           FailureRetryPolicy
	Mode            FailureMode
	Target          FailureTarget
	Publisher       BatchFailurePublisher
	Delegate        BatchFailureDelegate
	Limits          MessageLimits
	MaxBatchRecords int
	MaxBatchBytes   int64
	PublishTimeout  time.Duration
}

// Validate reports whether the batch failure policy is explicit, compatible,
// and bounded without constructing a handler.
func (config BatchFailureHandlerConfig) Validate() error {
	_, err := normalizeBatchFailureHandlerConfig(config)

	return err
}

type batchFailureHandler struct {
	handler         BatchHandler
	classifier      FailureClassifier
	retry           FailureRetryPolicy
	retryCategories map[ErrorCategory]struct{}
	mode            FailureMode
	target          FailureTarget
	publisher       BatchFailurePublisher
	delegate        BatchFailureDelegate
	limits          MessageLimits
	maxBatchRecords int
	maxBatchBytes   int64
	publishTimeout  time.Duration
	wait            failureWait
}

// NewBatchFailureHandler constructs a reusable whole-partition-batch failure
// decorator. Each invocation validates and retains the complete source batch
// before the wrapped handler runs, and each retry receives an isolated copy.
// Construction allocates no durable resources.
func NewBatchFailureHandler(
	config BatchFailureHandlerConfig,
) (BatchHandler, error) {
	handler, err := newBatchFailureHandler(config, waitFailureBackoff)
	if err != nil {
		return nil, err
	}

	return handler, nil
}

func newBatchFailureHandler(
	config BatchFailureHandlerConfig,
	wait failureWait,
) (*batchFailureHandler, error) {
	normalized, err := normalizeBatchFailureHandlerConfig(config)
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

	return &batchFailureHandler{
		handler:         normalized.Handler,
		classifier:      normalized.Classifier,
		retry:           normalized.Retry,
		retryCategories: categories,
		mode:            normalized.Mode,
		target:          normalized.Target,
		publisher:       normalized.Publisher,
		delegate:        normalized.Delegate,
		limits:          normalized.Limits,
		maxBatchRecords: normalized.MaxBatchRecords,
		maxBatchBytes:   normalized.MaxBatchBytes,
		publishTimeout:  normalized.PublishTimeout,
		wait:            wait,
	}, nil
}

func normalizeBatchFailureHandlerConfig(
	config BatchFailureHandlerConfig,
) (BatchFailureHandlerConfig, error) {
	if config.Handler == nil {
		return BatchFailureHandlerConfig{}, ErrBatchHandlerRequired
	}
	if config.Mode > FailureModeDelegate {
		return BatchFailureHandlerConfig{}, ErrInvalidFailurePolicy
	}
	if config.Limits == (MessageLimits{}) {
		config.Limits = DefaultMessageLimits()
	}
	if err := config.Limits.Validate(); err != nil {
		return BatchFailureHandlerConfig{}, err
	}
	var err error
	config.Retry, err = normalizeFailureRetryPolicy(config.Retry)
	if err != nil {
		return BatchFailureHandlerConfig{}, err
	}
	if config.MaxBatchRecords == 0 {
		config.MaxBatchRecords = defaultFailureBatchRecords
	}
	if config.MaxBatchRecords < 1 ||
		config.MaxBatchRecords > maximumFailureBatchRecords {
		return BatchFailureHandlerConfig{}, ErrInvalidFailurePolicy
	}
	if config.MaxBatchBytes == 0 {
		config.MaxBatchBytes = defaultFailureBatchBytes
	}
	if config.MaxBatchBytes < 1 ||
		config.MaxBatchBytes > maximumFailureBatchBytes {
		return BatchFailureHandlerConfig{}, ErrInvalidFailurePolicy
	}

	switch config.Mode {
	case FailureModeStop:
		if config.Target != (FailureTarget{}) {
			return BatchFailureHandlerConfig{}, ErrInvalidFailurePolicy
		}
		if config.Publisher != nil {
			return BatchFailureHandlerConfig{}, ErrInvalidFailurePolicy
		}
		if config.Delegate != nil {
			return BatchFailureHandlerConfig{}, ErrInvalidFailurePolicy
		}
		if config.PublishTimeout != 0 {
			return BatchFailureHandlerConfig{}, ErrInvalidFailurePolicy
		}
	case FailureModeRetryTopic, FailureModeDeadLetter:
		if config.Publisher == nil {
			return BatchFailureHandlerConfig{}, ErrFailurePublisherRequired
		}
		if config.Delegate != nil {
			return BatchFailureHandlerConfig{}, ErrInvalidFailurePolicy
		}
		if config.Target.Version == 0 {
			return BatchFailureHandlerConfig{}, ErrInvalidFailureTarget
		}
		if !validKafkaTopicName(config.Target.Topic, config.Limits.MaxTopicBytes) {
			return BatchFailureHandlerConfig{}, ErrInvalidFailureTarget
		}
		if config.PublishTimeout == 0 {
			config.PublishTimeout = defaultFailurePublishTimeout
		}
		if config.PublishTimeout < 100*time.Millisecond ||
			config.PublishTimeout > 2*time.Minute {
			return BatchFailureHandlerConfig{}, ErrInvalidFailurePolicy
		}
	case FailureModeDelegate:
		if config.Delegate == nil {
			return BatchFailureHandlerConfig{}, ErrFailureDelegateRequired
		}
		if config.Target != (FailureTarget{}) ||
			config.Publisher != nil ||
			config.PublishTimeout != 0 {
			return BatchFailureHandlerConfig{}, ErrInvalidFailurePolicy
		}
	}

	return config, nil
}

// HandleBatch applies the configured policy to one complete partition batch.
// The method is concurrency-safe when all supplied callbacks satisfy their
// documented concurrency contracts.
func (handler *batchFailureHandler) HandleBatch(
	ctx context.Context,
	batch ConsumedBatch,
) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if err := validateFailureBatch(
		batch,
		handler.limits,
		handler.maxBatchRecords,
		handler.maxBatchBytes,
	); err != nil {
		return err
	}

	source := batch.Retain()
	for attempt := 1; ; attempt++ {
		handlerErr := callBatchHandler(ctx, handler.handler, source.Retain())
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

		failure := BatchFailure{
			Batch:    source,
			Attempt:  attempt,
			Category: category,
			cause:    handlerErr,
		}
		if attempt < handler.retry.MaxAttempts {
			if handler.retryable(category) {
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
		}

		return handler.resolve(ctx, failure)
	}
}

func validateFailureBatch(
	batch ConsumedBatch,
	limits MessageLimits,
	maxRecords int,
	maxBytes int64,
) error {
	if len(batch.Records) == 0 {
		return errors.Join(ErrInvalidFailureBatch, ErrRecordsRequired)
	}
	if len(batch.Records) > maxRecords {
		return errors.Join(ErrInvalidFailureBatch, ErrTooManyBatchRecords)
	}
	if batch.Partition < 0 {
		return ErrInvalidFailureBatch
	}
	if !validKafkaTopicName(batch.Topic, limits.MaxTopicBytes) {
		return ErrInvalidFailureBatch
	}
	var total int64
	var priorOffset int64
	for index, record := range batch.Records {
		if record.Topic != batch.Topic || record.Partition != batch.Partition ||
			record.Offset < 0 || (index != 0 && record.Offset <= priorOffset) {
			return ErrInvalidFailureBatch
		}
		if err := validateFailureRecord(record, limits); err != nil {
			return errors.Join(ErrInvalidFailureBatch, err)
		}
		producerRecord := ProducerRecord{
			Topic: record.Topic, Key: record.Key, Value: record.Value,
			Headers: record.Headers,
		}
		size := recordSize(producerRecord)
		if size > maxBytes-total {
			return errors.Join(ErrInvalidFailureBatch, ErrBatchTooLarge)
		}
		total += size
		priorOffset = record.Offset
	}

	return nil
}

func (handler *batchFailureHandler) classify(err error) (
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

func (handler *batchFailureHandler) retryable(category ErrorCategory) bool {
	_, retryable := handler.retryCategories[category]

	return retryable
}

func (handler *batchFailureHandler) resolve(
	ctx context.Context,
	failure BatchFailure,
) error {
	switch handler.mode {
	case FailureModeStop:
		causes := []error{ErrConsumerFailureStopped, failure.cause}
		if failure.Attempt == handler.retry.MaxAttempts &&
			handler.retryable(failure.Category) {
			causes = append(causes, ErrFailureAttemptsExhausted)
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
		delegateErr := callBatchFailureDelegate(ctx, handler.delegate, failure)
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

func (handler *batchFailureHandler) publish(
	ctx context.Context,
	failure BatchFailure,
) error {
	if handler.target.Topic == failure.Batch.Topic {
		return newFailureHandlingError(
			FailureStagePublish,
			failure.Category,
			failure.Attempt,
			ErrFailurePublish,
			ErrInvalidFailureTarget,
			failure.cause,
		)
	}

	records := make([]ProducerRecord, len(failure.Batch.Records))
	var total int64
	for index, source := range failure.Batch.Records {
		records[index] = failureProducerRecord(
			handler.mode,
			handler.target,
			HandlerFailure{
				Record: source, Attempt: failure.Attempt,
				Category: failure.Category, cause: failure.cause,
			},
		)
		records[index].Headers = append(records[index].Headers,
			failureHeader("batch-index", strconv.Itoa(index)),
			failureHeader("batch-count", strconv.Itoa(len(records))),
		)
		if err := records[index].validate(handler.limits); err != nil {
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
		size := recordSize(records[index])
		if size > handler.maxBatchBytes-total {
			return newFailureHandlingError(
				FailureStagePublish,
				failure.Category,
				failure.Attempt,
				ErrFailurePublish,
				ErrFailureRecordInvalid,
				ErrBatchTooLarge,
				failure.cause,
			)
		}
		total += size
	}

	publishCtx, cancel := context.WithTimeout(ctx, handler.publishTimeout)
	results, publishErr, callbackErr := callBatchFailurePublisher(
		publishCtx,
		handler.publisher,
		records,
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

	causes := batchDeliveryCauses(len(records), handler.target.Topic, results)
	if publishErr == nil && len(causes) == 0 {
		return nil
	}
	causes = append(
		[]error{ErrFailurePublish, failure.cause},
		causes...,
	)
	if publishErr != nil {
		causes = append(causes, publishErr)
	}

	return newFailureHandlingErrorWithDeliveries(
		FailureStagePublish,
		failure.Category,
		failure.Attempt,
		results,
		causes...,
	)
}

func batchDeliveryCauses(
	expectedCount int,
	expectedTopic string,
	results []DeliveryResult,
) []error {
	if len(results) < expectedCount {
		return []error{ErrBatchDeliveryFailed, ErrDeliveryResultMissing}
	}
	if len(results) > expectedCount {
		return []error{ErrBatchDeliveryFailed, ErrDeliveryResultInvalid}
	}
	causes := make([]error, 0)
	for _, result := range results {
		if result.Topic != expectedTopic {
			causes = append(causes, ErrDeliveryResultInvalid)
		}
		if result.Err != nil {
			causes = append(causes, result.Err)
		}
	}
	if len(causes) != 0 {
		causes = append([]error{ErrBatchDeliveryFailed}, causes...)
	}

	return causes
}

func callBatchFailureDelegate(
	ctx context.Context,
	delegate BatchFailureDelegate,
	failure BatchFailure,
) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrFailureCallbackPanic
		}
	}()

	return delegate.HandleBatchFailure(ctx, failure)
}

func callBatchFailurePublisher(
	ctx context.Context,
	publisher BatchFailurePublisher,
	records []ProducerRecord,
) (
	results []DeliveryResult,
	publishErr error,
	callbackErr error,
) {
	defer func() {
		if recover() != nil {
			results = nil
			publishErr = nil
			callbackErr = ErrFailureCallbackPanic
		}
	}()

	results, publishErr = publisher.PublishBatch(ctx, records)

	return results, publishErr, nil
}
