package nsq

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic" //nolint:typecheck,nolintlint
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"

	nsq "github.com/nsqio/go-nsq"
)

var _ core.Worker = (*Worker)(nil)
var _ core.WorkerMetadata = (*Worker)(nil)

// BackendName identifies NSQ in lifecycle events.
func (*Worker) BackendName() string { return "nsq" }

// QueueName returns the configured NSQ topic.
func (w *Worker) QueueName() string { return w.opts.topic }

// Worker for NSQ
type Worker struct {
	q               *nsq.Consumer
	p               *nsq.Producer
	cfg             *nsq.Config
	lifecycleMu     sync.Mutex
	stopOnce        sync.Once
	startOnce       sync.Once
	stop            chan struct{}
	stopFlag        int32
	opts            Options
	tasks           chan *nsq.Message
	connectConsumer func(*nsq.Consumer, string) error
	publish         func(string, []byte) error
	stopProducer    func()
}

// NewWorker for struc
func NewWorker(opts ...Option) *Worker {
	w, err := NewWorkerE(opts...)
	if err != nil {
		panic(err)
	}

	return w
}

// NewWorkerE creates a worker and returns configuration errors.
func NewWorkerE(opts ...Option) (*Worker, error) {
	w := &Worker{
		opts:  newOptions(opts...),
		stop:  make(chan struct{}),
		tasks: make(chan *nsq.Message),
	}
	if strings.TrimSpace(w.opts.addr) == "" {
		return nil, errors.New("NSQ address is required")
	}
	if strings.TrimSpace(w.opts.deadLetterTopic) == "" ||
		w.opts.deadLetterTopic == w.opts.topic ||
		strings.ContainsAny(w.opts.deadLetterTopic, "\x00\r\n") ||
		w.opts.maxDeliveryAttempts < 2 || w.opts.maxDeliveryAttempts > 101 {
		return nil, fmt.Errorf("%w: unsafe NSQ dead-letter policy", queue.ErrInvalidConfiguration)
	}

	w.cfg = nsq.NewConfig()
	w.cfg.MaxInFlight = w.opts.maxInFlight
	w.cfg.DialTimeout = w.opts.connectTimeout
	w.connectConsumer = func(consumer *nsq.Consumer, addr string) error {
		return consumer.ConnectToNSQD(addr)
	}

	if err := w.startProducer(); err != nil {
		return nil, err
	}

	w.p.SetLoggerLevel(w.opts.logLevel)
	w.publish = w.p.Publish
	w.stopProducer = w.p.Stop

	return w, nil
}

func (w *Worker) startProducer() error {
	var err error

	w.p, err = nsq.NewProducer(w.opts.addr, w.cfg)

	return err
}

func (w *Worker) startConsumer() (err error) {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()

	if atomic.LoadInt32(&w.stopFlag) == 1 {
		return queue.ErrQueueShutdown
	}

	w.startOnce.Do(func() {
		w.q, err = nsq.NewConsumer(w.opts.topic, w.opts.channel, w.cfg)
		if err != nil {
			return
		}

		w.q.SetLoggerLevel(w.opts.logLevel)
		w.q.AddHandler(nsq.HandlerFunc(w.handleMessage))

		err = w.connectConsumer(w.q, w.opts.addr)
		if err != nil {
			return
		}
	})

	return err
}

func (w *Worker) handleMessage(msg *nsq.Message) error {
	if len(msg.Body) == 0 {
		// Returning nil will automatically send a FIN command to NSQ to mark the message as processed.
		// In this case, a message with an empty body is simply ignored/discarded.
		return nil
	}
	msg.DisableAutoResponse()

	for {
		select {
		case w.tasks <- msg:
			return nil
		case <-w.stop:
			msg.Requeue(-1)
			return nil
		case <-time.After(w.opts.touchInterval):
			msg.Touch()
		}
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

	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()

	w.stopOnce.Do(func() {
		// notify shtdown event to worker and consumer
		close(w.stop)
		// stop producer and consumer
		if w.q != nil {
			w.q.ChangeMaxInFlight(0)
			w.q.Stop()
			<-w.q.StopChan
		}
		w.stopProducer()

		// close task channel
		close(w.tasks)
	})
	return nil
}

// Queue send notification to queue
func (w *Worker) Queue(job core.TaskMessage) error {
	if atomic.LoadInt32(&w.stopFlag) == 1 {
		return queue.ErrQueueShutdown
	}

	return w.publish(w.opts.topic, job.Bytes())
}

// Request fetch new task from queue
func (w *Worker) Request() (core.TaskMessage, error) {
	if err := w.startConsumer(); err != nil {
		return nil, err
	}

	timer := time.NewTimer(w.opts.requestTimeout)
	defer timer.Stop()
	select {
	case task, ok := <-w.tasks:
		if !ok {
			return nil, queue.ErrQueueHasBeenClosed
		}
		data, err := job.DecodeE(task.Body, job.DefaultMaxMessageBytes)
		if err != nil {
			failure := management.NewFailure(
				management.ClassificationMalformed, "malformed_delivery", err,
			)
			if settlementErr := w.settleNSQFailure(task, failure); settlementErr != nil {
				return nil, errors.Join(
					fmt.Errorf("decode NSQ message: %w", err), settlementErr,
				)
			}
			return nil, fmt.Errorf("decode NSQ message: %w", err)
		}
		data.SetFailureAcknowledgement(
			func() error { task.Finish(); return nil },
			func(handlerErr error) error { return w.settleNSQFailure(task, handlerErr) },
		)
		return data, nil
	case <-timer.C:
		return nil, queue.ErrNoTaskInQueue
	}
}

// Stats retrieves the current connection and message statistics for a Consumer
func (w *Worker) Stats() *nsq.ConsumerStats {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()

	if w.q == nil {
		return nil
	}

	return w.q.Stats()
}
