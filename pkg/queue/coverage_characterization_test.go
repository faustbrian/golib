package queue

import (
	"bytes"
	"context"
	"errors"
	"log"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type controlledWorker struct {
	queueErr    error
	shutdownErr error
	requests    atomic.Int64
}

func (w *controlledWorker) Run(context.Context, core.TaskMessage) error { return nil }
func (w *controlledWorker) Shutdown() error                             { return w.shutdownErr }
func (w *controlledWorker) Queue(core.TaskMessage) error                { return w.queueErr }
func (w *controlledWorker) Request() (core.TaskMessage, error) {
	w.requests.Add(1)
	return nil, ErrNoTaskInQueue
}

type taskMessage []byte

func (m taskMessage) Bytes() []byte   { return m }
func (m taskMessage) Payload() []byte { return m }

func TestDefaultLoggerWritesUnformattedErrorsAndFatals(t *testing.T) {
	var output bytes.Buffer
	logger := defaultLogger{
		infoLogger:  log.New(&output, "INFO: ", 0),
		errorLogger: log.New(&output, "ERROR: ", 0),
		fatalLogger: log.New(&output, "FATAL: ", 0),
	}

	logger.Infof("info %s", "formatted")
	logger.Errorf("error %s", "formatted")
	logger.Fatalf("fatal %s", "formatted")
	logger.Info("info message")
	logger.Error("error message")
	logger.Fatal("fatal message")

	assert.Contains(t, output.String(), "INFO: info formatted")
	assert.Contains(t, output.String(), "ERROR: error formatted")
	assert.Contains(t, output.String(), "FATAL:")
	assert.Contains(t, output.String(), "ERROR: error message")
	assert.Contains(t, output.String(), "fatal message")
}

func TestEmptyLoggerAcceptsEveryLogShape(t *testing.T) {
	logger := emptyLogger{}
	assert.NotPanics(t, func() {
		logger.Infof("info %s", "formatted")
		logger.Errorf("error %s", "formatted")
		logger.Fatalf("fatal %s", "formatted")
		logger.Info("info")
		logger.Error("error")
		logger.Fatal("fatal")
	})
	assert.IsType(t, emptyLogger{}, NewEmptyLogger())
}

func TestObserverAdaptersForwardAndDiscardEvents(t *testing.T) {
	event := Event{Kind: EventEnqueued}
	var observed Event
	ObserverFunc(func(value Event) { observed = value }).Observe(event)
	emptyObserver{}.Observe(event)

	assert.Equal(t, event, observed)
}

func TestStartWithUpdatedZeroWorkersDoesNotRequestTasks(t *testing.T) {
	worker := &controlledWorker{}
	q, err := NewQueue(WithWorker(worker))
	require.NoError(t, err)
	q.UpdateWorkerCount(0)

	q.Start()
	q.Release()

	assert.Zero(t, worker.requests.Load())
}

func TestRepeatedShutdownIsIdempotentAndLogsWorkerError(t *testing.T) {
	var output bytes.Buffer
	worker := &controlledWorker{shutdownErr: errors.New("shutdown failed")}
	q, err := NewQueue(
		WithWorker(worker),
		WithLogger(defaultLogger{
			infoLogger:  log.New(&output, "", 0),
			errorLogger: log.New(&output, "", 0),
			fatalLogger: log.New(&output, "", 0),
		}),
	)
	require.NoError(t, err)

	q.Shutdown()
	q.Shutdown()

	assert.Contains(t, output.String(), "shutdown failed")
}

func TestQueueReturnsBackendEnqueueFailureWithoutCountingSubmission(t *testing.T) {
	worker := &controlledWorker{queueErr: errors.New("publish failed")}
	q, err := NewQueue(WithWorker(worker))
	require.NoError(t, err)

	err = q.Queue(mockMessage{message: "payload"})

	assert.ErrorContains(t, err, "publish failed")
	assert.Zero(t, q.SubmittedTasks())
	q.Release()
}

func TestQueueRejectsUnsafeJobOptionsBeforeEnqueue(t *testing.T) {
	q, err := NewQueue(WithWorker(&controlledWorker{}))
	require.NoError(t, err)

	err = q.QueueTask(
		func(context.Context) error { return nil },
		job.AllowOption{RetryCount: job.Int64(job.MaxRetryCount + 1)},
	)

	assert.ErrorIs(t, err, job.ErrInvalidMessage)
	assert.Zero(t, q.SubmittedTasks())
	q.Release()
}

func TestSettlementJoinsHandlerAndRejectionFailures(t *testing.T) {
	observer := &recordingObserver{}
	q, err := NewQueue(WithWorker(&controlledWorker{}), WithObserver(observer))
	require.NoError(t, err)
	message := job.NewTask(nil)
	handlerErr := errors.New("handler failed")
	rejectionErr := errors.New("nack password=secret")
	message.SetAcknowledgement(nil, func() error { return rejectionErr })

	err = q.settle(&message, handlerErr)

	assert.ErrorIs(t, err, handlerErr)
	assert.ErrorIs(t, err, rejectionErr)
	assert.Equal(t, management.ClassificationInfrastructure, management.ClassifyFailure(err))
	assert.NotContains(t, err.Error(), "password=secret")
	assert.Contains(t, observer.kinds(), EventRejectFailed)
	q.Release()
}

func TestRunRejectsUnknownTaskMessageType(t *testing.T) {
	q, err := NewQueue(WithWorker(&controlledWorker{}))
	require.NoError(t, err)

	err = q.run(taskMessage("payload"))

	assert.ErrorContains(t, err, "invalid task type")
	q.Release()
}

func TestShutdownDuringHandlerReturnsDeadlineWhenHandlerIgnoresCancellation(t *testing.T) {
	q, err := NewQueue(WithWorker(&controlledWorker{}))
	require.NoError(t, err)
	close(q.quit)
	message := job.NewTask(
		func(context.Context) error {
			time.Sleep(20 * time.Millisecond)
			return nil
		},
		job.AllowOption{Timeout: job.Time(time.Millisecond)},
	)

	assert.ErrorIs(t, q.handle(&message), context.DeadlineExceeded)
}

func TestShutdownPropagatesHandlerPanicAfterCancellation(t *testing.T) {
	q, err := NewQueue(WithWorker(&controlledWorker{}))
	require.NoError(t, err)
	close(q.quit)
	message := job.NewTask(func(ctx context.Context) error {
		<-ctx.Done()
		panic("after cancellation")
	})

	assert.PanicsWithValue(t, "after cancellation", func() { _ = q.handle(&message) })
}

func TestRecoveryHelpersHandleBoundaries(t *testing.T) {
	assert.Equal(t, []byte("???"), source(nil, -1))
	assert.Equal(t, []byte("???"), source(nil, 0))
	assert.Equal(t, []byte("???"), function(0))
	assert.NotEmpty(t, stack(0))

	pc, _, _, ok := runtime.Caller(0)
	require.True(t, ok)
	assert.True(t, strings.Contains(string(function(pc)), "TestRecoveryHelpersHandleBoundaries"))
}

func TestRingRejectsQueueAndSecondShutdownAfterClosing(t *testing.T) {
	ring := NewRing()
	require.NoError(t, ring.Shutdown())
	assert.ErrorIs(t, ring.Shutdown(), ErrQueueShutdown)
	assert.ErrorIs(t, ring.Queue(&job.Message{}), ErrQueueShutdown)
}

func TestNewPoolPanicsWhenQueueConstructionFails(t *testing.T) {
	original := constructQueue
	constructQueue = func(...Option) (*Queue, error) {
		return nil, errors.New("construction failed")
	}
	t.Cleanup(func() { constructQueue = original })

	assert.PanicsWithError(t, "construction failed", func() { NewPool(1) })
}

func TestStackContinuesWhenSourceCannotBeRead(t *testing.T) {
	original := readSourceFile
	readSourceFile = func(string) ([]byte, error) {
		return nil, errors.New("read failed")
	}
	t.Cleanup(func() { readSourceFile = original })

	assert.NotEmpty(t, stack(0))
}
