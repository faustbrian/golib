package audit

import (
	"bytes"
	"context"
	"errors"
	"time"
)

// DeliveryMode is the caller's explicit response to primary sink failure.
type DeliveryMode uint8

const (
	// DeliveryFailClosed returns the primary sink error to the caller.
	DeliveryFailClosed DeliveryMode = iota + 1
	// DeliveryFailOpenWithAlert permits progress only after a successful alert.
	DeliveryFailOpenWithAlert
	// DeliveryDurableBuffer permits progress only after a successful buffer write.
	DeliveryDurableBuffer
)

// MaxRecoveryTimeout is the absolute bound for alerting or durable buffering
// after the caller's primary-operation context has ended.
const MaxRecoveryTimeout = time.Minute

// DeliveryDisposition states what happened after redaction.
type DeliveryDisposition uint8

const (
	// DeliveryPersisted reports primary sink success.
	DeliveryPersisted DeliveryDisposition = iota + 1
	// DeliveryProceededAfterAlert reports explicit fail-open handling.
	DeliveryProceededAfterAlert
	// DeliveryBuffered reports successful fallback persistence.
	DeliveryBuffered
)

// DeliveryAlert contains bounded, identifier-free failure metadata.
type DeliveryAlert struct{ Outcome AppendOutcome }

// Alerter must durably or operationally surface fail-open delivery loss.
type Alerter interface {
	Alert(context.Context, DeliveryAlert) error
}

// AlertFunc adapts a function to Alerter.
type AlertFunc func(context.Context, DeliveryAlert) error

// Alert invokes the adapted function with identifier-free failure metadata.
func (alert AlertFunc) Alert(ctx context.Context, value DeliveryAlert) error {
	if alert == nil || ctx == nil {
		return invalid("alerter", "must be assigned")
	}
	return alert(ctx, value)
}

// BufferLimits declares the finite capacity of a caller-owned durable buffer.
// MaxBytes includes the complete persisted representation and adapter overhead.
type BufferLimits struct {
	MaxRecords      int
	MaxBytes        int
	MaxBatchRecords int
}

func (limits BufferLimits) valid() bool {
	return limits.MaxRecords >= 1 && limits.MaxBytes >= 1 &&
		limits.MaxBatchRecords >= 1 && limits.MaxBatchRecords <= limits.MaxRecords &&
		limits.MaxBatchRecords <= MaxAppendBatchRecords
}

// DurableBuffer is a sink whose implementation contract survives process
// failure and whose finite capacity is observable before recording starts.
// The caller remains responsible for selecting and operating the adapter.
type DurableBuffer interface {
	Sink
	BufferLimits() BufferLimits
}

// RecorderConfig wires an explicit delivery policy. Sink, Redactor, and Mode
// are always required; fail-open requires Alerter and buffering requires Buffer.
type RecorderConfig struct {
	Sink            Sink
	Redactor        Redactor
	Mode            DeliveryMode
	Alerter         Alerter
	Buffer          DurableBuffer
	Observer        Observer
	Clock           func() time.Time
	DelayThreshold  time.Duration
	RecoveryTimeout time.Duration
}

// DeliveryResult reports whether the redacted record persisted, was buffered,
// or was explicitly allowed to proceed after an alert.
type DeliveryResult struct {
	Disposition DeliveryDisposition
	Append      AppendResult
}

// DeliveryBatchResult is the policy result for one bounded atomic batch.
type DeliveryBatchResult struct {
	Disposition DeliveryDisposition
	Append      BatchResult
}

// Recorder applies redaction before any sink or delivery-failure handling. It
// performs no implicit retries and never silently converts a failure to success.
type Recorder struct {
	config RecorderConfig
	clock  func() time.Time
}

type redactionFailure struct{}

func (failure *redactionFailure) Error() string { return "audit: redaction failed" }

func safeRedactionFailure(err error) (result error) {
	defer func() {
		if recover() != nil {
			result = &redactionFailure{}
		}
	}()
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	default:
		return &redactionFailure{}
	}
}

var (
	ErrDeliveryAlertFailed = errors.New("audit: delivery alert failed")
	ErrDurableBufferFailed = errors.New("audit: durable buffer failed")
	errDependencyPanic     = errors.New("audit: dependency panicked")
)

func safeAppendFailure(err error) error {
	return NewAppendError(AppendOutcomeOf(err), err)
}

// NewRecorder validates and constructs an explicit redaction and delivery
// policy. It starts no goroutines and performs no implicit retries.
func NewRecorder(config RecorderConfig) (*Recorder, error) {
	if config.Sink == nil || config.Redactor == nil {
		return nil, invalid("recorder", "requires sink and redactor")
	}
	if config.DelayThreshold < 0 {
		return nil, invalid("delay_threshold", "must not be negative")
	}
	if config.RecoveryTimeout < 0 || config.RecoveryTimeout > MaxRecoveryTimeout {
		return nil, invalid("recovery_timeout", "must be positive and bounded")
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	switch config.Mode {
	case DeliveryFailClosed:
	case DeliveryFailOpenWithAlert:
		if config.Alerter == nil || config.RecoveryTimeout == 0 {
			return nil, invalid("alerter", "is required for fail-open delivery")
		}
	case DeliveryDurableBuffer:
		if config.Buffer == nil || config.RecoveryTimeout == 0 {
			return nil, invalid("buffer", "is required for buffered delivery")
		}
		limits, ok := callBufferLimits(config.Buffer)
		if !ok || !limits.valid() {
			return nil, invalid("buffer", "is required for buffered delivery")
		}
	default:
		return nil, invalid("delivery_mode", "must be explicit")
	}
	return &Recorder{config: config, clock: clock}, nil
}

func callBufferLimits(buffer DurableBuffer) (limits BufferLimits, ok bool) {
	defer func() {
		if recover() != nil {
			limits = BufferLimits{}
			ok = false
		}
	}()
	return buffer.BufferLimits(), true
}

// Submit redacts one record, invokes the configured sink once, and applies the
// selected failure policy. Its result never implies an unreported discard.
func (recorder *Recorder) Submit(ctx context.Context, record Record) (DeliveryResult, error) {
	if recorder == nil || ctx == nil || record.ID() == "" {
		return DeliveryResult{}, invalid("recorder", "must be assigned")
	}
	redacted, err := callRedactor(recorder.config.Redactor, ctx, record)
	if err != nil {
		return DeliveryResult{}, safeRedactionFailure(err)
	}
	if err := validateRedaction(record, redacted); err != nil {
		return DeliveryResult{}, err
	}
	redacted.redactionApplied = true
	if redactionInvalidatesIntegrity(record, redacted) {
		return DeliveryResult{}, ErrIntegrityInvalid
	}
	recorder.observeDelay(ctx, redacted)
	appendResult, appendErr := callAppend(recorder.config.Sink, ctx, redacted)
	if AppendOutcomeOf(appendErr) == AppendCommitted {
		recorder.observeFailure(ctx, appendErr)
		return DeliveryResult{Disposition: DeliveryPersisted, Append: appendResult}, safeAppendFailure(appendErr)
	}
	if appendErr == nil {
		appendErr = validateAppendResult(redacted, appendResult)
		if appendErr == nil {
			recorder.observeAppend(ctx, appendResult)
			return DeliveryResult{Disposition: DeliveryPersisted, Append: appendResult}, nil
		}
	}
	appendErr = safeAppendFailure(appendErr)
	switch recorder.config.Mode {
	case DeliveryFailClosed:
		recorder.observeFailure(ctx, appendErr)
		return DeliveryResult{}, appendErr
	case DeliveryFailOpenWithAlert:
		recorder.observeFailure(ctx, appendErr)
		recoveryCtx, cancel := recorder.recoveryContext(ctx)
		defer cancel()
		if err := callAlert(recorder.config.Alerter, recoveryCtx, DeliveryAlert{Outcome: AppendOutcomeOf(appendErr)}); err != nil {
			return DeliveryResult{}, NewAppendError(AppendOutcomeOf(appendErr), errors.Join(appendErr, ErrDeliveryAlertFailed))
		}
		return DeliveryResult{Disposition: DeliveryProceededAfterAlert}, nil
	case DeliveryDurableBuffer:
		recorder.observeFailure(ctx, appendErr)
		recoveryCtx, cancel := recorder.recoveryContext(ctx)
		defer cancel()
		buffered, err := callAppend(recorder.config.Buffer, recoveryCtx, redacted)
		if err != nil {
			return DeliveryResult{}, NewAppendError(AppendOutcomeOf(err), errors.Join(appendErr, ErrDurableBufferFailed))
		}
		if err := validateAppendResult(redacted, buffered); err != nil {
			return DeliveryResult{}, NewAppendError(AppendUnknown, errors.Join(appendErr, err))
		}
		safeObserve(ctx, recorder.config.Observer, Observation{Kind: ObservationBuffered, Count: 1, Outcome: AppendOutcomeOf(appendErr)})
		return DeliveryResult{Disposition: DeliveryBuffered, Append: buffered}, nil
	default:
		return DeliveryResult{}, appendErr
	}
}

// SubmitBatch redacts every member before invoking the primary sink. Delivery
// policy is applied to the batch as one unit; compatible sinks therefore
// preserve their documented atomic batch semantics.
func (recorder *Recorder) SubmitBatch(ctx context.Context, records []Record) (DeliveryBatchResult, error) {
	if recorder == nil || ctx == nil {
		return DeliveryBatchResult{}, invalid("recorder", "must be assigned")
	}
	if len(records) == 0 || len(records) > MaxAppendBatchRecords {
		return DeliveryBatchResult{}, NewAppendError(AppendRejected, ErrBatchTooLarge)
	}
	redacted := make([]Record, len(records))
	for index, record := range records {
		if record.ID() == "" {
			return DeliveryBatchResult{}, invalid("record", "must be valid")
		}
		value, err := callRedactor(recorder.config.Redactor, ctx, record)
		if err != nil {
			return DeliveryBatchResult{}, safeRedactionFailure(err)
		}
		if err := validateRedaction(record, value); err != nil {
			return DeliveryBatchResult{}, err
		}
		value.redactionApplied = true
		if redactionInvalidatesIntegrity(record, value) {
			return DeliveryBatchResult{}, ErrIntegrityInvalid
		}
		redacted[index] = value
		recorder.observeDelay(ctx, value)
	}
	appendResult, appendErr := callAppendBatch(recorder.config.Sink, ctx, redacted)
	if AppendOutcomeOf(appendErr) == AppendCommitted {
		recorder.observeFailure(ctx, appendErr)
		return DeliveryBatchResult{Disposition: DeliveryPersisted, Append: appendResult}, safeAppendFailure(appendErr)
	}
	if appendErr == nil {
		appendErr = validateBatchResult(redacted, appendResult)
		if appendErr == nil {
			for _, result := range appendResult.Results {
				recorder.observeAppend(ctx, result)
			}
			return DeliveryBatchResult{Disposition: DeliveryPersisted, Append: appendResult}, nil
		}
	}
	appendErr = safeAppendFailure(appendErr)
	switch recorder.config.Mode {
	case DeliveryFailClosed:
		recorder.observeFailure(ctx, appendErr)
		return DeliveryBatchResult{}, appendErr
	case DeliveryFailOpenWithAlert:
		recorder.observeFailure(ctx, appendErr)
		recoveryCtx, cancel := recorder.recoveryContext(ctx)
		defer cancel()
		if err := callAlert(recorder.config.Alerter, recoveryCtx, DeliveryAlert{Outcome: AppendOutcomeOf(appendErr)}); err != nil {
			return DeliveryBatchResult{}, NewAppendError(AppendOutcomeOf(appendErr), errors.Join(appendErr, ErrDeliveryAlertFailed))
		}
		return DeliveryBatchResult{Disposition: DeliveryProceededAfterAlert}, nil
	case DeliveryDurableBuffer:
		recorder.observeFailure(ctx, appendErr)
		recoveryCtx, cancel := recorder.recoveryContext(ctx)
		defer cancel()
		buffered, err := callAppendBatch(recorder.config.Buffer, recoveryCtx, redacted)
		if err != nil {
			return DeliveryBatchResult{}, NewAppendError(AppendOutcomeOf(err), errors.Join(appendErr, ErrDurableBufferFailed))
		}
		if err := validateBatchResult(redacted, buffered); err != nil {
			return DeliveryBatchResult{}, NewAppendError(AppendUnknown, errors.Join(appendErr, err))
		}
		safeObserve(ctx, recorder.config.Observer, Observation{Kind: ObservationBuffered, Count: len(redacted), Outcome: AppendOutcomeOf(appendErr)})
		return DeliveryBatchResult{Disposition: DeliveryBuffered, Append: buffered}, nil
	default:
		return DeliveryBatchResult{}, appendErr
	}
}

func (recorder *Recorder) recoveryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), recorder.config.RecoveryTimeout)
}

func callRedactor(redactor Redactor, ctx context.Context, record Record) (result Record, err error) {
	defer func() {
		if recover() != nil {
			result = Record{}
			err = &redactionFailure{}
		}
	}()
	return redactor.Redact(ctx, record)
}

func callAppend(sink Sink, ctx context.Context, record Record) (result AppendResult, err error) {
	defer func() {
		if recover() != nil {
			result = AppendResult{}
			err = NewAppendError(AppendUnknown, errDependencyPanic)
		}
	}()
	return sink.Append(ctx, record)
}

func callAppendBatch(sink Sink, ctx context.Context, records []Record) (result BatchResult, err error) {
	defer func() {
		if recover() != nil {
			result = BatchResult{}
			err = NewAppendError(AppendUnknown, errDependencyPanic)
		}
	}()
	return sink.AppendBatch(ctx, records)
}

func callAlert(alerter Alerter, ctx context.Context, alert DeliveryAlert) (err error) {
	defer func() {
		if recover() != nil {
			err = errDependencyPanic
		}
	}()
	return alerter.Alert(ctx, alert)
}

func validateAppendResult(record Record, result AppendResult) error {
	if result.RecordID != record.ID() || (result.Status != AppendAccepted && result.Status != AppendDuplicate) {
		return NewAppendError(AppendUnknown, ErrSinkProtocol)
	}
	return nil
}

func validateBatchResult(records []Record, result BatchResult) error {
	if len(result.Results) != len(records) {
		return NewAppendError(AppendUnknown, ErrSinkProtocol)
	}
	for index := range records {
		if err := validateAppendResult(records[index], result.Results[index]); err != nil {
			return err
		}
	}
	return nil
}

func redactionInvalidatesIntegrity(original, redacted Record) bool {
	if !original.integrity.Enabled() {
		return false
	}
	originalCanonical, _ := CanonicalJSON(original)
	redactedCanonical, _ := CanonicalJSON(redacted)
	return !bytes.Equal(originalCanonical, redactedCanonical)
}

func validateRedaction(original, redacted Record) error {
	expected := original
	expected.description = redacted.description
	expected.context.networkOrigin = redacted.context.networkOrigin
	expected.context.userAgent = redacted.context.userAgent
	expected.attributes = redacted.attributes
	expected.changes = redacted.changes
	expectedCanonical, _ := CanonicalJSON(expected)
	redactedCanonical, _ := CanonicalJSON(redacted)
	if !bytes.Equal(expectedCanonical, redactedCanonical) ||
		!mapKeysSubset(redacted.attributes, original.attributes) ||
		!mapKeysSubset(redacted.changes.before, original.changes.before) ||
		!mapKeysSubset(redacted.changes.after, original.changes.after) {
		return invalid("redaction", "may only remove or transform privacy fields")
	}
	hasChanges := len(redacted.changes.before) != 0 || len(redacted.changes.after) != 0
	switch {
	case original.changes.noChange:
		if !redacted.changes.noChange || redacted.changes.redacted || hasChanges {
			return invalid("redaction", "must preserve change semantics")
		}
	case original.changes.redacted:
		if !redacted.changes.redacted || redacted.changes.noChange || hasChanges {
			return invalid("redaction", "must preserve change semantics")
		}
	case hasChanges:
		if redacted.changes.noChange || redacted.changes.redacted {
			return invalid("redaction", "must preserve change semantics")
		}
	default:
		if redacted.changes.noChange || !redacted.changes.redacted {
			return invalid("redaction", "must preserve change semantics")
		}
	}
	return nil
}

func mapKeysSubset(subset, superset map[string]string) bool {
	for key := range subset {
		if _, exists := superset[key]; !exists {
			return false
		}
	}
	return true
}

func (recorder *Recorder) observeAppend(ctx context.Context, result AppendResult) {
	kind := ObservationAccepted
	if result.Status == AppendDuplicate {
		kind = ObservationDuplicated
	}
	safeObserve(ctx, recorder.config.Observer, Observation{Kind: kind, Count: 1, Outcome: AppendCommitted})
}

func (recorder *Recorder) observeFailure(ctx context.Context, err error) {
	outcome := AppendOutcomeOf(err)
	safeObserve(ctx, recorder.config.Observer, Observation{Kind: ObservationFailed, Count: 1, Outcome: outcome})
	if outcome == AppendRejected {
		safeObserve(ctx, recorder.config.Observer, Observation{Kind: ObservationRejected, Count: 1, Outcome: outcome})
	}
}

func (recorder *Recorder) observeDelay(ctx context.Context, record Record) {
	if recorder.config.DelayThreshold == 0 {
		return
	}
	now, err := callClock(recorder.clock)
	if err != nil {
		return
	}
	delay := now.Sub(record.RecordedAt())
	if delay >= recorder.config.DelayThreshold {
		safeObserve(ctx, recorder.config.Observer, Observation{Kind: ObservationDelayed, Count: 1, Duration: delay})
	}
}
