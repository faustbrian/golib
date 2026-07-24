package nats

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic" //nolint:typecheck,nolintlint
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/safeerr"
	"github.com/faustbrian/golib/pkg/queue/job"

	nats "github.com/nats-io/nats.go"
)

var _ core.Worker = (*Worker)(nil)
var _ core.WorkerMetadata = (*Worker)(nil)

// BackendName identifies Core NATS in lifecycle events.
func (*Worker) BackendName() string { return "nats" }

// QueueName returns the configured NATS subject.
func (w *Worker) QueueName() string { return w.opts.subj }

// Worker for NSQ
type Worker struct {
	client       *nats.Conn
	stop         chan struct{}
	exit         chan struct{}
	closed       chan struct{}
	stopFlag     int32
	stopOnce     sync.Once
	startOnce    sync.Once
	exitOnce     sync.Once
	opts         options
	subscription *nats.Subscription
	tasks        chan *nats.Msg
}

// NewWorker for struc
func NewWorker(opts ...Option) *Worker {
	w, err := NewWorkerE(opts...)
	if err != nil {
		panic(err)
	}

	return w
}

// NewWorkerE creates a worker and returns connection and subscription errors.
func NewWorkerE(opts ...Option) (*Worker, error) {
	var err error
	w := &Worker{
		opts:   newOptions(opts...),
		stop:   make(chan struct{}),
		exit:   make(chan struct{}),
		closed: make(chan struct{}),
		tasks:  make(chan *nats.Msg),
	}

	w.client, err = nats.Connect(
		w.opts.addr,
		nats.Timeout(w.opts.connectTimeout),
		nats.DrainTimeout(w.opts.connectTimeout),
		nats.ClosedHandler(func(*nats.Conn) { close(w.closed) }),
	)
	if err != nil {
		return nil, safeerr.Wrap("connect to NATS", err)
	}

	if err := w.startConsumer(); err != nil {
		w.client.Close()
		return nil, fmt.Errorf("subscribe to NATS queue: %w", err)
	}

	return w, nil
}

func (w *Worker) startConsumer() (err error) {
	w.startOnce.Do(func() {
		w.subscription, err = w.client.QueueSubscribe(w.opts.subj, w.opts.queue, w.handleMessage)
		if err != nil {
			w.opts.logger.Errorf("error subscribing to queue: %s", err.Error())
			w.exitOnce.Do(func() { close(w.exit) })
		}
	})

	return err
}

func (w *Worker) handleMessage(msg *nats.Msg) {
	select {
	case w.tasks <- msg:
	case <-w.stop:
		if msg != nil {
			// re-queue the task if worker has been shutdown.
			w.opts.logger.Info("re-queue the current task")
			if err := w.client.Publish(w.opts.subj, msg.Data); err != nil {
				w.opts.logger.Errorf("error to re-queue the current task: %s", err.Error())
			}
		}
		w.exitOnce.Do(func() { close(w.exit) })
	}
}

// Run start the worker
func (w *Worker) Run(ctx context.Context, task core.TaskMessage) error {
	return w.opts.runFunc(ctx, task)
}

// Shutdown worker
func (w *Worker) Shutdown() error {
	if !atomic.CompareAndSwapInt32(&w.stopFlag, 0, 1) {
		return queue.ErrQueueShutdown
	}

	w.stopOnce.Do(func() {
		close(w.stop)
		if w.client != nil {
			if !w.client.IsClosed() {
				if err := w.client.Drain(); err != nil {
					w.client.Close()
				}
			}
			if w.closed != nil {
				<-w.closed
			} else {
				w.client.Close()
			}
		}
		close(w.tasks)
	})
	return nil
}

// Queue send notification to queue
func (w *Worker) Queue(job core.TaskMessage) error {
	if atomic.LoadInt32(&w.stopFlag) == 1 {
		return queue.ErrQueueShutdown
	}

	err := w.client.Publish(w.opts.subj, job.Bytes())
	if err != nil {
		return err
	}

	return nil
}

// Request a new task
func (w *Worker) Request() (core.TaskMessage, error) {
	_ = w.startConsumer()
	timer := time.NewTimer(w.opts.requestTimeout)
	defer timer.Stop()
	select {
	case task, ok := <-w.tasks:
		if !ok {
			return nil, queue.ErrQueueHasBeenClosed
		}
		data, err := job.DecodeE(task.Data, job.DefaultMaxMessageBytes)
		if err != nil {
			return nil, fmt.Errorf("decode NATS message: %w", err)
		}
		return data, nil
	case <-timer.C:
		return nil, queue.ErrNoTaskInQueue
	}
}
