package queue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuccessfulHandlerAcknowledgesDelivery(t *testing.T) {
	var acknowledgements atomic.Int64
	var rejections atomic.Int64
	done := make(chan struct{})
	message := job.NewTask(func(context.Context) error { return nil })
	message.SetAcknowledgement(
		func() error { acknowledgements.Add(1); return nil },
		func() error { rejections.Add(1); return nil },
	)
	q, err := NewQueue(WithWorker(NewRing()), WithAfterFn(func() { close(done) }))
	require.NoError(t, err)

	require.NoError(t, q.queue(&message))
	q.Start()
	require.Eventually(t, func() bool { return channelClosed(done) }, time.Second, time.Millisecond)
	q.Release()

	require.EqualValues(t, 1, acknowledgements.Load())
	require.Zero(t, rejections.Load())
}

func TestFailedHandlerRejectsDelivery(t *testing.T) {
	var acknowledgements atomic.Int64
	var rejections atomic.Int64
	done := make(chan struct{})
	message := job.NewTask(func(context.Context) error { return errors.New("handler failed") })
	message.SetAcknowledgement(
		func() error { acknowledgements.Add(1); return nil },
		func() error { rejections.Add(1); return nil },
	)
	q, err := NewQueue(WithWorker(NewRing()), WithAfterFn(func() { close(done) }))
	require.NoError(t, err)

	require.NoError(t, q.queue(&message))
	q.Start()
	require.Eventually(t, func() bool { return channelClosed(done) }, time.Second, time.Millisecond)
	q.Release()

	require.Zero(t, acknowledgements.Load())
	require.EqualValues(t, 1, rejections.Load())
}

func TestAcknowledgementFailureFailsDeliveryAndEmitsEvent(t *testing.T) {
	observer := &recordingObserver{}
	done := make(chan struct{})
	ackErr := errors.New("redis password=operator-secret")
	message := job.NewTask(func(context.Context) error { return nil })
	message.SetAcknowledgement(
		func() error { return ackErr },
		func() error { return nil },
	)
	q, err := NewQueue(
		WithWorker(NewRing()),
		WithObserver(observer),
		WithAfterFn(func() { close(done) }),
	)
	require.NoError(t, err)

	require.NoError(t, q.queue(&message))
	q.Start()
	require.Eventually(t, func() bool { return channelClosed(done) }, time.Second, time.Millisecond)
	q.Release()

	require.EqualValues(t, 1, q.FailureTasks())
	require.Contains(t, observer.kinds(), EventAckFailed)
	var observed Event
	for _, event := range observer.snapshot() {
		if event.Kind == EventAckFailed {
			observed = event
		}
	}
	require.Error(t, observed.Err)
	assert.ErrorIs(t, observed.Err, ackErr)
	assert.Equal(t, management.ClassificationInfrastructure, observed.Classification)
	assert.Equal(t, "acknowledgement_failed", observed.FailureCode)
	var classified *management.Failure
	require.ErrorAs(t, observed.Err, &classified)
	assert.Equal(t, "acknowledgement_failed", classified.Code)
	assert.NotContains(t, observed.Err.Error(), "operator-secret")
}

func TestPanickingHandlerRejectsDelivery(t *testing.T) {
	var rejections atomic.Int64
	done := make(chan struct{})
	message := job.NewTask(func(context.Context) error { panic("boom") })
	message.SetAcknowledgement(
		func() error { return nil },
		func() error { rejections.Add(1); return nil },
	)
	q, err := NewQueue(
		WithWorker(NewRing()),
		WithLogger(NewEmptyLogger()),
		WithAfterFn(func() { close(done) }),
	)
	require.NoError(t, err)

	require.NoError(t, q.queue(&message))
	q.Start()
	require.Eventually(t, func() bool { return channelClosed(done) }, time.Second, time.Millisecond)
	q.Release()

	require.EqualValues(t, 1, rejections.Load())
}

func TestPanickingHandlerUsesPermanentSafeFailure(t *testing.T) {
	const sensitivePanic = "database password=operator-secret"
	observer := &recordingObserver{}
	var settled error
	message := job.NewTask(func(context.Context) error { panic(sensitivePanic) })
	message.SetFailureAcknowledgement(
		func() error { return nil },
		func(failure error) error { settled = failure; return nil },
	)
	q, err := NewQueue(
		WithWorker(NewRing()),
		WithObserver(observer),
		WithLogger(NewEmptyLogger()),
	)
	require.NoError(t, err)
	atomic.StoreInt64(&q.activeWorkers, 1)
	q.metric.IncBusyWorker()

	assert.NotPanics(t, func() { q.work(&message) })
	require.Error(t, settled)
	assert.ErrorIs(t, settled, ErrHandlerPanic)
	assert.Equal(t, management.ClassificationPermanent, management.ClassifyFailure(settled))
	var classified *management.Failure
	require.ErrorAs(t, settled, &classified)
	assert.Equal(t, "handler_panic", classified.Code)
	assert.NotContains(t, settled.Error(), sensitivePanic)
	for _, event := range observer.snapshot() {
		if event.Err != nil {
			assert.NotContains(t, event.Err.Error(), sensitivePanic)
		}
	}
}

func TestHandlerFailureUsesRetryableSafeFailure(t *testing.T) {
	const sensitiveFailure = "postgres://operator-secret@database"
	observer := &recordingObserver{}
	logger := &recordingQueueLogger{}
	var settled error
	message := job.NewTask(func(context.Context) error { return errors.New(sensitiveFailure) })
	message.SetFailureAcknowledgement(
		func() error { return nil },
		func(failure error) error { settled = failure; return nil },
	)
	q, err := NewQueue(
		WithWorker(NewRing()),
		WithObserver(observer),
		WithLogger(logger),
	)
	require.NoError(t, err)
	atomic.StoreInt64(&q.activeWorkers, 1)
	q.metric.IncBusyWorker()

	q.work(&message)
	require.Error(t, settled)
	assert.Equal(t, management.ClassificationRetryable, management.ClassifyFailure(settled))
	var classified *management.Failure
	require.ErrorAs(t, settled, &classified)
	assert.Equal(t, "handler_failed", classified.Code)
	assert.NotContains(t, settled.Error(), sensitiveFailure)
	assert.NotContains(t, logger.output.String(), sensitiveFailure)
	for _, event := range observer.snapshot() {
		if event.Err != nil {
			assert.NotContains(t, event.Err.Error(), sensitiveFailure)
		}
	}
}

func TestNormalizeHandlerFailureUsesStableOriginCodes(t *testing.T) {
	t.Parallel()
	classifiedCause := errors.New("sensitive classified cause")
	tests := map[string]struct {
		input          error
		classification management.Classification
		code           string
	}{
		"nil":             {},
		"canceled":        {context.Canceled, management.ClassificationCanceled, "handler_canceled"},
		"deadline":        {context.DeadlineExceeded, management.ClassificationCanceled, "handler_deadline_exceeded"},
		"classified":      {management.NewFailure(management.ClassificationPermanent, "invalid_order", classifiedCause), management.ClassificationPermanent, "invalid_order"},
		"plain retryable": {errors.New("temporary secret"), management.ClassificationRetryable, "handler_failed"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			resolved := normalizeHandlerFailure(test.input)
			if test.input == nil {
				assert.NoError(t, resolved)
				return
			}
			assert.Equal(t, test.classification, management.ClassifyFailure(resolved))
			var classified *management.Failure
			require.ErrorAs(t, resolved, &classified)
			assert.Equal(t, test.code, classified.Code)
			assert.ErrorIs(t, resolved, test.input)
			assert.NotContains(t, resolved.Error(), "secret")
		})
	}
}

func TestAcknowledgementPanicFailsDeliveryWithoutEscaping(t *testing.T) {
	observer := &recordingObserver{}
	q, err := NewQueue(
		WithWorker(NewRing()),
		WithObserver(observer),
		WithLogger(NewEmptyLogger()),
	)
	require.NoError(t, err)
	message := job.NewTask(func(context.Context) error { return nil })
	message.SetAcknowledgement(
		func() error { panic("ack transport panic") },
		func() error { return nil },
	)
	atomic.StoreInt64(&q.activeWorkers, 1)
	q.metric.IncBusyWorker()

	assert.NotPanics(t, func() { q.work(&message) })
	assert.Equal(t, uint64(1), q.FailureTasks())
	assert.Contains(t, observer.kinds(), EventAckFailed)
}

func TestRejectionPanicJoinsHandlerFailure(t *testing.T) {
	observer := &recordingObserver{}
	q, err := NewQueue(
		WithWorker(NewRing()),
		WithObserver(observer),
		WithLogger(NewEmptyLogger()),
	)
	require.NoError(t, err)
	message := job.NewTask(func(context.Context) error { return nil })
	message.SetAcknowledgement(
		func() error { return nil },
		func() error { panic("nack transport panic") },
	)
	handlerErr := errors.New("handler failed")

	settlementErr := q.settle(&message, handlerErr)
	assert.ErrorIs(t, settlementErr, handlerErr)
	assert.ErrorIs(t, settlementErr, ErrSettlementPanic)
	assert.Equal(t, management.ClassificationInfrastructure, management.ClassifyFailure(settlementErr))
	assert.Contains(t, observer.kinds(), EventRejectFailed)
}

func TestRejectionFailureIsInfrastructureAndRedactsCause(t *testing.T) {
	observer := &recordingObserver{}
	q, err := NewQueue(
		WithWorker(NewRing()),
		WithObserver(observer),
		WithLogger(NewEmptyLogger()),
	)
	require.NoError(t, err)
	message := job.NewTask(func(context.Context) error { return nil })
	transportErr := errors.New("amqp://operator-secret@broker")
	message.SetAcknowledgement(func() error { return nil }, func() error { return transportErr })
	handlerErr := management.NewFailure(
		management.ClassificationPermanent,
		"invalid_order",
		errors.New("customer payload"),
	)

	settlementErr := q.settle(&message, handlerErr)
	assert.ErrorIs(t, settlementErr, handlerErr)
	assert.ErrorIs(t, settlementErr, transportErr)
	assert.Equal(t, management.ClassificationInfrastructure, management.ClassifyFailure(settlementErr))
	assert.NotContains(t, settlementErr.Error(), "operator-secret")
	assert.Contains(t, observer.kinds(), EventRejectFailed)
	for _, event := range observer.snapshot() {
		if event.Kind == EventRejectFailed {
			assert.Equal(t, management.ClassificationInfrastructure, event.Classification)
			assert.Equal(t, "failure_settlement_failed", event.FailureCode)
		}
	}
}

func TestSettlementPassesClassifiedFailureToCapableDelivery(t *testing.T) {
	t.Parallel()

	delivery := &failureAwareDelivery{}
	handlerCause := errors.New("invalid order")
	handlerErr := management.NewFailure(
		management.ClassificationPermanent,
		"invalid_order",
		handlerCause,
	)
	observer := &recordingObserver{}
	q, err := NewQueue(WithWorker(NewRing()), WithObserver(observer))
	require.NoError(t, err)

	settlementErr := q.settle(delivery, handlerErr)
	require.ErrorIs(t, settlementErr, handlerCause)
	assert.Equal(t, handlerErr, delivery.failure)
	assert.Zero(t, delivery.legacyNacks.Load())
	for _, event := range observer.snapshot() {
		if event.Kind == EventRejected {
			assert.Equal(t, management.ClassificationPermanent, event.Classification)
			assert.Equal(t, "invalid_order", event.FailureCode)
		}
	}
}

type failureAwareDelivery struct {
	failure     error
	legacyNacks atomic.Int64
}

func (*failureAwareDelivery) Bytes() []byte                 { return nil }
func (*failureAwareDelivery) Payload() []byte               { return nil }
func (*failureAwareDelivery) AcknowledgementRequired() bool { return true }
func (*failureAwareDelivery) Ack() error                    { return nil }
func (d *failureAwareDelivery) Nack() error {
	d.legacyNacks.Add(1)
	return nil
}
func (d *failureAwareDelivery) NackFailure(err error) error {
	d.failure = err
	return nil
}

func channelClosed(channel <-chan struct{}) bool {
	select {
	case <-channel:
		return true
	default:
		return false
	}
}

type recordingQueueLogger struct {
	output strings.Builder
}

func (l *recordingQueueLogger) Infof(format string, args ...any) {
	_, _ = fmt.Fprintf(&l.output, format, args...)
}

func (l *recordingQueueLogger) Errorf(format string, args ...any) {
	_, _ = fmt.Fprintf(&l.output, format, args...)
}

func (l *recordingQueueLogger) Fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(&l.output, format, args...)
}

func (l *recordingQueueLogger) Info(args ...any)  { _, _ = fmt.Fprint(&l.output, args...) }
func (l *recordingQueueLogger) Error(args ...any) { _, _ = fmt.Fprint(&l.output, args...) }
func (l *recordingQueueLogger) Fatal(args ...any) { _, _ = fmt.Fprint(&l.output, args...) }
