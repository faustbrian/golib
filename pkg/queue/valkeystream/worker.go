package valkeystream

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/safeerr"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	valkey "github.com/valkey-io/valkey-go"
)

var _ core.Worker = (*Worker)(nil)
var _ core.WorkerMetadata = (*Worker)(nil)

var newValkeyClient = valkey.NewClient

// Worker is a standalone Valkey Streams queue worker.
type Worker struct {
	opts            options
	transport       streamqueue.Transport
	tasks           chan streamqueue.Delivery
	ctx             context.Context
	cancel          context.CancelFunc
	done            chan struct{}
	stopped         atomic.Bool
	wait            sync.WaitGroup
	metrics         workerMetrics
	startedAt       time.Time
	now             func() time.Time
	currentJobs     atomic.Uint32
	controlMu       sync.Mutex
	controlApplyMu  sync.Mutex
	controlEntries  map[string]*nativeControlEntry
	controlCapacity int
}

// Stats describes server-reported outstanding work and monotonic lifecycle
// counters observed by this worker. Depth is -1 when Valkey cannot report lag.
type Stats struct {
	// Depth is pending plus lag, or -1 when lag is unknown.
	Depth int64
	// Pending is the server-reported consumer-group pending count.
	Pending int64
	// Lag is the server-reported count not yet delivered to the group.
	Lag int64
	// LagKnown reports whether Depth and Lag are determinate.
	LagKnown bool
	// OldestPendingAge is derived from the oldest pending stream identifier.
	OldestPendingAge time.Duration
	// Enqueued counts successful appends observed by this worker process.
	Enqueued uint64
	// Delivered counts messages returned by Request in this worker process.
	Delivered uint64
	// Reclaimed counts delivered messages recovered through XAUTOCLAIM.
	Reclaimed uint64
	// Retries counts reclaimed retry deliveries observed by this worker.
	Retries uint64
	// Acknowledged counts successful source acknowledgements.
	Acknowledged uint64
	// DeadLettered counts successful terminal dead-letter transfers.
	DeadLettered uint64
	// SettlementFailures counts failed ack and dead-letter operations.
	SettlementFailures uint64
}

type workerMetrics struct {
	enqueued           atomic.Uint64
	delivered          atomic.Uint64
	reclaimed          atomic.Uint64
	retries            atomic.Uint64
	acknowledged       atomic.Uint64
	deadLettered       atomic.Uint64
	settlementFailures atomic.Uint64
}

// NewWorker constructs a Valkey Streams worker and panics when configuration
// or initial connectivity is invalid.
func NewWorker(option ...Option) *Worker {
	worker, err := NewWorkerE(option...)
	if err != nil {
		panic(err)
	}
	return worker
}

// NewWorkerE constructs a Valkey Streams worker with a native valkey-go
// client and validates connectivity and consumer-group ownership.
func NewWorkerE(option ...Option) (*Worker, error) {
	opts, err := newOptions(option...)
	if err != nil {
		return nil, err
	}
	client, err := newValkeyClient(nativeClientOptions(opts))
	if err != nil {
		return nil, safeerr.Wrap("valkeystream: initialize native client", err)
	}
	transport := newNativeTransport(client, opts.maxLength, job.DefaultMaxMessageBytes)
	transport.recordMaxLength = opts.recordMaxLength
	ctx, cancel := context.WithTimeout(context.Background(), opts.commandTimeout)
	defer cancel()
	if err = client.Do(ctx, client.B().Ping().Build()).Error(); err != nil {
		_ = transport.Close()
		return nil, safeerr.Wrap("valkeystream: connect to server", err)
	}
	if err = transport.EnsureGroup(ctx, opts.stream, opts.group); err != nil {
		_ = transport.Close()
		return nil, err
	}
	return newWorkerForTransport(opts, transport), nil
}

func newWorkerForTransport(opts options, transport streamqueue.Transport) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	worker := &Worker{
		opts: opts, transport: transport,
		tasks:           make(chan streamqueue.Delivery, opts.readBatchSize*2),
		ctx:             ctx,
		cancel:          cancel,
		done:            make(chan struct{}),
		startedAt:       time.Now().UTC(),
		now:             time.Now,
		controlEntries:  make(map[string]*nativeControlEntry, defaultControlCapacity),
		controlCapacity: defaultControlCapacity,
	}
	worker.wait.Add(2)
	go worker.readLoop()
	go worker.reclaimLoop()
	go func() {
		worker.wait.Wait()
		close(worker.tasks)
		close(worker.done)
	}()
	return worker
}

// BackendName identifies this adapter in lifecycle events.
func (*Worker) BackendName() string { return "valkey-streams" }

// QueueName returns the configured stream name.
func (w *Worker) QueueName() string { return w.opts.stream }

// Queue appends one bounded encoded task to the configured stream.
func (w *Worker) Queue(task core.TaskMessage) error {
	if w.stopped.Load() {
		return queue.ErrQueueShutdown
	}
	if task == nil {
		return errors.New("valkeystream: task is required")
	}
	request := streamqueue.AddRequest{
		Stream: w.opts.stream, MaxLength: w.opts.maxLength, Body: task.Bytes(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), w.opts.commandTimeout)
	defer cancel()
	_, err := w.transport.Add(ctx, request)
	if err == nil {
		w.metrics.enqueued.Add(1)
	}
	return err
}

// Stats returns an honest consumer-group snapshot and this worker's monotonic
// lifecycle counters.
func (w *Worker) Stats(ctx context.Context) (Stats, error) {
	state, err := w.transport.GroupState(ctx, w.opts.stream, w.opts.group)
	if err != nil {
		return Stats{}, err
	}
	shared := state.Stats()
	stats := Stats{
		Depth: shared.Depth, Pending: shared.Pending, Lag: shared.Lag,
		LagKnown: shared.LagKnown,
		Enqueued: w.metrics.enqueued.Load(), Delivered: w.metrics.delivered.Load(),
		Reclaimed: w.metrics.reclaimed.Load(), Retries: w.metrics.retries.Load(),
		Acknowledged:       w.metrics.acknowledged.Load(),
		DeadLettered:       w.metrics.deadLettered.Load(),
		SettlementFailures: w.metrics.settlementFailures.Load(),
	}
	if state.OldestPendingID != "" {
		stats.OldestPendingAge, err = streamqueue.MessageAge(state.OldestPendingID, time.Now())
		if err != nil {
			return Stats{}, fmt.Errorf("valkeystream: inspect oldest pending delivery: %w", err)
		}
	}
	return stats, nil
}

// Request waits for one new or reclaimed delivery.
func (w *Worker) Request() (core.TaskMessage, error) {
	timer := time.NewTimer(w.opts.requestTimeout)
	defer timer.Stop()
	select {
	case delivery, ok := <-w.tasks:
		if !ok {
			return nil, queue.ErrQueueHasBeenClosed
		}
		w.metrics.delivered.Add(1)
		if delivery.Reclaimed {
			w.metrics.reclaimed.Add(1)
			w.metrics.retries.Add(1)
		}
		return w.decode(delivery)
	case <-timer.C:
		return nil, queue.ErrNoTaskInQueue
	case <-w.ctx.Done():
		return nil, queue.ErrQueueHasBeenClosed
	}
}

// Run invokes the configured task handler.
func (w *Worker) Run(ctx context.Context, task core.TaskMessage) error {
	w.currentJobs.Add(1)
	defer w.currentJobs.Add(^uint32(0))
	return w.opts.runFunc(ctx, task)
}

// Shutdown cancels blocking reads and reclaim scans, waits within the
// configured bound, and closes every native connection owned by the worker.
func (w *Worker) Shutdown() error {
	if !w.stopped.CompareAndSwap(false, true) {
		return queue.ErrQueueShutdown
	}
	w.cancel()
	timer := time.NewTimer(w.opts.shutdownTimeout)
	defer timer.Stop()
	select {
	case <-w.done:
		return w.transport.Close()
	case <-timer.C:
		_ = w.transport.Close()
		return fmt.Errorf("valkeystream: shutdown: %w", context.DeadlineExceeded)
	}
}

func (w *Worker) readLoop() {
	defer w.wait.Done()
	request := streamqueue.ReadRequest{
		Stream: w.opts.stream, Group: w.opts.group, Consumer: w.opts.consumer,
		Count: int64(w.opts.readBatchSize), Block: w.opts.blockTime,
	}
	for w.ctx.Err() == nil {
		deliveries, err := w.transport.Read(w.ctx, request)
		if err != nil {
			if w.ctx.Err() != nil {
				return
			}
			w.opts.logger.Error("valkeystream: read failed")
			if !waitContext(w.ctx, w.opts.reclaimInterval) {
				return
			}
			continue
		}
		for _, delivery := range deliveries {
			if !w.deliver(delivery) {
				return
			}
		}
	}
}

func (w *Worker) reclaimLoop() {
	defer w.wait.Done()
	ticker := time.NewTicker(w.opts.reclaimInterval)
	defer ticker.Stop()
	cursor := "0-0"
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			result, err := w.transport.Claim(w.ctx, streamqueue.ClaimRequest{
				Stream: w.opts.stream, Group: w.opts.group, Consumer: w.opts.consumer,
				MinIdle: w.opts.reclaimMinIdle, Start: cursor,
				Count: int64(w.opts.reclaimBatchSize),
			})
			if err != nil {
				if w.ctx.Err() == nil {
					w.opts.logger.Error("valkeystream: reclaim failed")
				}
				continue
			}
			if result.Next == "" || result.Next == "0-0" {
				cursor = "0-0"
			} else {
				cursor = result.Next
			}
			for _, delivery := range result.Deliveries {
				if !w.deliver(delivery) {
					return
				}
			}
		}
	}
}

func (w *Worker) deliver(delivery streamqueue.Delivery) bool {
	select {
	case w.tasks <- delivery:
		return true
	case <-w.ctx.Done():
		return false
	}
}

func (w *Worker) decode(delivery streamqueue.Delivery) (core.TaskMessage, error) {
	message, err := job.DecodeE(delivery.Body, job.DefaultMaxMessageBytes)
	if err != nil {
		deadLetter := delivery
		if errors.Is(err, job.ErrMessageTooLarge) {
			deadLetter.Body = nil
		}
		failure := streamqueue.FailureMetadata{
			Classification: management.ClassificationMalformed,
			Code:           "malformed_delivery",
		}
		if errors.Is(err, job.ErrMessageTooLarge) {
			failure.Code = "message_too_large"
		}
		if deadLetterErr := w.deadLetter(deadLetter, failure); deadLetterErr != nil {
			w.metrics.settlementFailures.Add(1)
			return nil, errors.Join(err, deadLetterErr)
		}
		w.metrics.deadLettered.Add(1)
		return nil, err
	}
	var once sync.Once
	var settlementErr error
	settle := func(action func() error) error {
		once.Do(func() { settlementErr = action() })
		return settlementErr
	}
	message.SetFailureAcknowledgement(
		func() error {
			return settle(func() error {
				ctx, cancel := context.WithTimeout(context.Background(), w.opts.commandTimeout)
				defer cancel()
				err := w.transport.Ack(ctx, streamqueue.AckRequest{
					Stream: w.opts.stream, Group: w.opts.group, ID: delivery.ID,
				})
				if err != nil {
					w.metrics.settlementFailures.Add(1)
					return err
				}
				w.metrics.acknowledged.Add(1)
				return nil
			})
		},
		func(handlerErr error) error {
			return settle(func() error {
				if records, ok := w.transport.(nativeRecordTransport); ok {
					ctx, cancel := context.WithTimeout(context.Background(), w.opts.commandTimeout)
					defer cancel()
					if err := records.RecordFailure(
						ctx, w.opts.failureStream, w.opts.stream, w.opts.group,
						delivery, failureMetadata(handlerErr, "handler_failed"),
					); err != nil {
						return err
					}
				}
				if !terminalFailure(handlerErr, delivery.Attempts, w.opts.maxDeliveryAttempts) {
					return nil
				}
				err := w.deadLetter(
					delivery,
					failureMetadata(handlerErr, "attempts_exhausted"),
				)
				if err != nil {
					w.metrics.settlementFailures.Add(1)
					return err
				}
				w.metrics.deadLettered.Add(1)
				return nil
			})
		},
	)
	return message, nil
}

func terminalFailure(handlerErr error, attempts, maximumAttempts int64) bool {
	switch management.ClassifyFailure(handlerErr) {
	case management.ClassificationPermanent, management.ClassificationMalformed:
		return true
	case management.ClassificationRetryable:
		return attempts >= maximumAttempts
	default:
		return false
	}
}

func failureMetadata(err error, fallbackCode string) streamqueue.FailureMetadata {
	resolution := management.ResolveFailure(err)
	metadata := streamqueue.FailureMetadata{
		Classification: resolution.Classification,
		Code:           fallbackCode,
	}
	if resolution.Code != "" {
		metadata.Code = resolution.Code
	}

	return metadata
}

func (w *Worker) deadLetter(
	delivery streamqueue.Delivery,
	failure streamqueue.FailureMetadata,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), w.opts.commandTimeout)
	defer cancel()
	return w.transport.DeadLetter(ctx, streamqueue.DeadLetterRequest{
		Source: w.opts.stream, Destination: w.opts.deadLetterStream,
		Group: w.opts.group, Delivery: delivery, Failure: failure,
	})
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
