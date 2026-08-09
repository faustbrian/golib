package audit

import (
	"context"
	"errors"
	"fmt"
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
	Sink           Sink
	Redactor       Redactor
	Mode           DeliveryMode
	Alerter        Alerter
	Buffer         DurableBuffer
	Observer       Observer
	Clock          func() time.Time
	DelayThreshold time.Duration
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

func safeRedactionFailure(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	default:
		return &redactionFailure{}
	}
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
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	switch config.Mode {
	case DeliveryFailClosed:
	case DeliveryFailOpenWithAlert:
		if config.Alerter == nil {
			return nil, invalid("alerter", "is required for fail-open delivery")
		}
	case DeliveryDurableBuffer:
		if config.Buffer == nil || !config.Buffer.BufferLimits().valid() {
			return nil, invalid("buffer", "is required for buffered delivery")
		}
	default:
		return nil, invalid("delivery_mode", "must be explicit")
	}
	return &Recorder{config: config, clock: clock}, nil
}

// Submit redacts one record, invokes the configured sink once, and applies the
// selected failure policy. Its result never implies an unreported discard.
func (recorder *Recorder) Submit(ctx context.Context, record Record) (DeliveryResult, error) {
	if recorder == nil || ctx == nil {
		return DeliveryResult{}, invalid("recorder", "must be assigned")
	}
	redacted, err := recorder.config.Redactor.Redact(ctx, record)
	if err != nil {
		return DeliveryResult{}, safeRedactionFailure(err)
	}
	if redacted.ID() != record.ID() {
		return DeliveryResult{}, invalid("redaction", "must preserve record identity")
	}
	recorder.observeDelay(ctx, redacted)
	appendResult, appendErr := recorder.config.Sink.Append(ctx, redacted)
	if appendErr == nil {
		recorder.observeAppend(ctx, appendResult)
		return DeliveryResult{Disposition: DeliveryPersisted, Append: appendResult}, nil
	}
	switch recorder.config.Mode {
	case DeliveryFailClosed:
		recorder.observeFailure(ctx, appendErr)
		return DeliveryResult{}, appendErr
	case DeliveryFailOpenWithAlert:
		recorder.observeFailure(ctx, appendErr)
		if err := recorder.config.Alerter.Alert(ctx, DeliveryAlert{Outcome: AppendOutcomeOf(appendErr)}); err != nil {
			return DeliveryResult{}, errors.Join(appendErr, fmt.Errorf("audit: delivery alert failed: %w", err))
		}
		return DeliveryResult{Disposition: DeliveryProceededAfterAlert}, nil
	case DeliveryDurableBuffer:
		recorder.observeFailure(ctx, appendErr)
		buffered, err := recorder.config.Buffer.Append(ctx, redacted)
		if err != nil {
			return DeliveryResult{}, errors.Join(appendErr, fmt.Errorf("audit: durable buffer failed: %w", err))
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
		value, err := recorder.config.Redactor.Redact(ctx, record)
		if err != nil {
			return DeliveryBatchResult{}, safeRedactionFailure(err)
		}
		if value.ID() != record.ID() {
			return DeliveryBatchResult{}, invalid("redaction", "must preserve record identity")
		}
		redacted[index] = value
		recorder.observeDelay(ctx, value)
	}
	appendResult, appendErr := recorder.config.Sink.AppendBatch(ctx, redacted)
	if appendErr == nil {
		for _, result := range appendResult.Results {
			recorder.observeAppend(ctx, result)
		}
		return DeliveryBatchResult{Disposition: DeliveryPersisted, Append: appendResult}, nil
	}
	switch recorder.config.Mode {
	case DeliveryFailClosed:
		recorder.observeFailure(ctx, appendErr)
		return DeliveryBatchResult{}, appendErr
	case DeliveryFailOpenWithAlert:
		recorder.observeFailure(ctx, appendErr)
		if err := recorder.config.Alerter.Alert(ctx, DeliveryAlert{Outcome: AppendOutcomeOf(appendErr)}); err != nil {
			return DeliveryBatchResult{}, errors.Join(appendErr, fmt.Errorf("audit: delivery alert failed: %w", err))
		}
		return DeliveryBatchResult{Disposition: DeliveryProceededAfterAlert}, nil
	case DeliveryDurableBuffer:
		recorder.observeFailure(ctx, appendErr)
		buffered, err := recorder.config.Buffer.AppendBatch(ctx, redacted)
		if err != nil {
			return DeliveryBatchResult{}, errors.Join(appendErr, fmt.Errorf("audit: durable buffer failed: %w", err))
		}
		safeObserve(ctx, recorder.config.Observer, Observation{Kind: ObservationBuffered, Count: len(redacted), Outcome: AppendOutcomeOf(appendErr)})
		return DeliveryBatchResult{Disposition: DeliveryBuffered, Append: buffered}, nil
	default:
		return DeliveryBatchResult{}, appendErr
	}
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
	delay := recorder.clock().Sub(record.RecordedAt())
	if delay >= recorder.config.DelayThreshold {
		safeObserve(ctx, recorder.config.Observer, Observation{Kind: ObservationDelayed, Count: 1, Duration: delay})
	}
}
