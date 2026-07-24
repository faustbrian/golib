package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/safeerr"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"

	amqp "github.com/rabbitmq/amqp091-go"
)

var _ core.Worker = (*Worker)(nil)
var _ core.WorkerMetadata = (*Worker)(nil)

const (
	deliveryAttemptHeader  = "x-queue-delivery-attempt"
	classificationHeader   = "x-queue-classification"
	failureCodeHeader      = "x-queue-failure-code"
	envelopeVersionHeader  = "x-queue-envelope-version"
	sourceQueueHeader      = "x-queue-source-queue"
	sourceExchangeHeader   = "x-queue-source-exchange"
	sourceRoutingKeyHeader = "x-queue-source-routing-key"
)

// BackendName identifies RabbitMQ in lifecycle events.
func (*Worker) BackendName() string { return "rabbitmq" }

// QueueName returns the configured RabbitMQ queue.
func (w *Worker) QueueName() string { return w.opts.queue }

// ReconnectConfig defines the retry policy for RabbitMQ connection.
type ReconnectConfig struct {
	MaxRetries   int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

type amqpConnection interface {
	Close() error
	Channel() (*amqp.Channel, error)
}

type amqpChannel interface {
	ExchangeDeclare(string, string, bool, bool, bool, bool, amqp.Table) error
	QueueDeclare(string, bool, bool, bool, bool, amqp.Table) (amqp.Queue, error)
	QueueBind(string, string, string, bool, amqp.Table) error
	Consume(string, string, bool, bool, bool, bool, amqp.Table) (<-chan amqp.Delivery, error)
	Cancel(string, bool) error
	Close() error
	Confirm(bool) error
	NotifyPublish(chan amqp.Confirmation) chan amqp.Confirmation
	PublishWithContext(context.Context, string, string, bool, bool, amqp.Publishing) error
}

var dialAMQP = func(addr string) (amqpConnection, error) {
	return amqp.Dial(addr)
}

var connectRabbitMQ = openRabbitMQ

// dialWithRetry tries to connect to RabbitMQ with retry and backoff.
func dialWithRetry(addr string, cfg ReconnectConfig) (amqpConnection, error) {
	if cfg.MaxRetries < 1 {
		return nil, errors.New("RabbitMQ max retries must be at least one")
	}
	var conn amqpConnection
	var err error
	delay := cfg.InitialDelay
	for i := 0; i < cfg.MaxRetries; i++ {
		conn, err = dialAMQP(addr)
		if err == nil {
			return conn, nil
		}
		if i+1 < cfg.MaxRetries {
			time.Sleep(delay)
		}
		// Exponential backoff with cap
		delay = time.Duration(math.Min(float64(cfg.MaxDelay), float64(delay)*2))
	}
	return nil, safeerr.Wrap("failed to connect to RabbitMQ after retries", err)
}

func openRabbitMQ(
	addr string,
	cfg ReconnectConfig,
) (amqpConnection, amqpChannel, error) {
	connection, err := dialWithRetry(addr, cfg)
	if err != nil {
		return nil, nil, err
	}
	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()
		return nil, nil, errors.New("set up RabbitMQ channel: " + err.Error())
	}
	return connection, channel, nil
}

/*
Worker struct implements the core.Worker interface for RabbitMQ.
It manages the AMQP connection, channel, and task consumption.
Fields:
- conn: AMQP connection to RabbitMQ server.
- channel: AMQP channel for communication.
- stop: Channel to signal worker shutdown.
- stopFlag: Atomic flag to indicate if the worker is stopped.
- stopOnce: Ensures shutdown logic runs only once.
- startOnce: Ensures consumer initialization runs only once.
- opts: Configuration options for the worker.
- tasks: Channel for receiving AMQP deliveries (tasks).
*/
type Worker struct {
	conn          amqpConnection
	channel       amqpChannel
	stop          chan struct{}
	stopFlag      int32
	stopOnce      sync.Once
	startOnce     sync.Once
	channelMu     sync.Mutex
	startErr      error
	opts          options
	tasks         <-chan amqp.Delivery
	confirmations <-chan amqp.Confirmation
}

/*
NewWorker creates and initializes a new Worker instance with the provided options.
It establishes a connection to RabbitMQ, sets up the channel, and declares the exchange.
If any step fails, it logs a fatal error and terminates the process.

Parameters:
- opts: Variadic list of Option functions to configure the worker.

Returns:
- Pointer to the initialized Worker.
*/
func NewWorker(opts ...Option) *Worker {
	w, err := NewWorkerE(opts...)
	if err != nil {
		panic(err)
	}

	return w
}

// NewWorkerE creates a worker and returns connection and setup errors.
func NewWorkerE(opts ...Option) (*Worker, error) {
	var err error
	w := &Worker{
		opts:  newOptions(opts...),
		stop:  make(chan struct{}),
		tasks: make(chan amqp.Delivery),
	}

	if !isVaildExchange(w.opts.exchangeType) {
		return nil, errors.New("invalid RabbitMQ exchange type: " + w.opts.exchangeType)
	}
	if w.opts.publishTimeout <= 0 {
		return nil, errors.New("RabbitMQ publish timeout must be positive")
	}
	if err := w.opts.validateDeadLetter(); err != nil {
		return nil, fmt.Errorf("%w: unsafe RabbitMQ dead-letter policy", err)
	}
	w.conn, w.channel, err = connectRabbitMQ(w.opts.addr, w.opts.reconnect)
	if err != nil {
		return nil, err
	}

	if err := w.channel.ExchangeDeclare(
		w.opts.exchangeName, // name
		w.opts.exchangeType, // type
		true,                // durable
		false,               // auto-deleted
		false,               // internal
		false,               // noWait
		nil,                 // arguments
	); err != nil {
		_ = w.channel.Close()
		_ = w.conn.Close()
		return nil, errors.New("declare RabbitMQ exchange: " + err.Error())
	}
	if err := w.channel.Confirm(false); err != nil {
		_ = w.channel.Close()
		_ = w.conn.Close()
		return nil, errors.New("enable RabbitMQ publisher confirms: " + err.Error())
	}
	w.confirmations = w.channel.NotifyPublish(make(chan amqp.Confirmation, 1))

	return w, nil
}

/*
startConsumer initializes the consumer for the worker.
It declares the queue, binds it to the exchange, and starts consuming messages.
This method is safe to call multiple times but will only execute once due to sync.Once.

Returns:
- error: Any error encountered during initialization, or nil on success.
*/
func (w *Worker) startConsumer() error {
	w.channelMu.Lock()
	defer w.channelMu.Unlock()

	return w.startConsumerLocked()
}

func (w *Worker) startConsumerLocked() error {
	if atomic.LoadInt32(&w.stopFlag) == 1 {
		return queue.ErrQueueShutdown
	}

	w.startOnce.Do(func() {
		if err := w.channel.ExchangeDeclare(
			w.opts.deadLetter.Exchange, ExchangeDirect, true, false, false, false, nil,
		); err != nil {
			w.startErr = fmt.Errorf("declare RabbitMQ dead-letter exchange: %w", err)
			w.opts.logger.Error("Dead-letter ExchangeDeclare failed: ", err)
			return
		}
		deadQueue, err := w.channel.QueueDeclare(
			w.opts.deadLetter.Queue, true, false, false, false, nil,
		)
		if err != nil {
			w.startErr = fmt.Errorf("declare RabbitMQ dead-letter queue: %w", err)
			w.opts.logger.Error("Dead-letter QueueDeclare failed: ", err)
			return
		}
		if err := w.channel.QueueBind(
			deadQueue.Name, w.opts.deadLetter.RoutingKey,
			w.opts.deadLetter.Exchange, false, nil,
		); err != nil {
			w.startErr = fmt.Errorf("bind RabbitMQ dead-letter queue: %w", err)
			w.opts.logger.Error("Dead-letter QueueBind failed: ", err)
			return
		}
		q, err := w.channel.QueueDeclare(
			w.opts.queue, // name
			true,         // durable
			false,        // delete when unused
			false,        // exclusive
			false,        // no-wait
			amqp.Table{
				"x-dead-letter-exchange":    w.opts.deadLetter.Exchange,
				"x-dead-letter-routing-key": w.opts.deadLetter.RoutingKey,
			}, // arguments
		)
		if err != nil {
			w.startErr = err
			w.opts.logger.Error("QueueDeclare failed: ", err)
			return
		}

		if err := w.channel.QueueBind(q.Name, w.opts.routingKey, w.opts.exchangeName, false, nil); err != nil {
			w.startErr = err
			w.opts.logger.Error("QueueBind failed: ", err)
			return
		}

		w.tasks, err = w.channel.Consume(
			q.Name,         // queue
			w.opts.tag,     // consumer
			w.opts.autoAck, // auto-ack
			false,          // exclusive
			false,          // no-local
			false,          // no-wait
			nil,            // args
		)
		if err != nil {
			w.startErr = err
			w.opts.logger.Error("Consume failed: ", err)
			return
		}
	})

	return w.startErr
}

/*
Run executes the worker's task processing function.
It delegates the actual task handling to the configured runFunc.

Parameters:
- ctx: Context for cancellation and timeout.
- task: The task message to process.

Returns:
- error: Any error returned by the runFunc.
*/
func (w *Worker) Run(ctx context.Context, task core.TaskMessage) error {
	return w.opts.runFunc(ctx, task)
}

/*
Shutdown gracefully stops the worker.
It ensures shutdown logic runs only once, cancels the consumer, and closes the AMQP connection.
If the worker is already stopped, it returns queue.ErrQueueShutdown.

Returns:
- error: Any error encountered during shutdown, or nil on success.
*/
func (w *Worker) Shutdown() (err error) {
	if !atomic.CompareAndSwapInt32(&w.stopFlag, 0, 1) {
		return queue.ErrQueueShutdown
	}

	w.channelMu.Lock()
	defer w.channelMu.Unlock()

	w.stopOnce.Do(func() {
		close(w.stop)
		// Cancel consumer first
		if w.channel != nil {
			if cerr := w.channel.Cancel(w.opts.tag, true); cerr != nil {
				w.opts.logger.Error("consumer cancel failed: ", cerr)
				if err == nil {
					err = cerr
				}
			}
			// Try to close channel
			if cerr := w.channel.Close(); cerr != nil {
				w.opts.logger.Error("AMQP channel close error: ", cerr)
				if err == nil {
					err = cerr
				}
			}
		}
		// Then close connection
		if w.conn != nil {
			if cerr := w.conn.Close(); cerr != nil {
				w.opts.logger.Error("AMQP connection close error: ", cerr)
				if err == nil {
					err = cerr
				}
			}
		}
	})

	return err
}

/*
Queue publishes a new task message to the RabbitMQ exchange.
If the worker is stopped, it returns queue.ErrQueueShutdown.

Parameters:
- job: The task message to be published.

Returns:
- error: Any error encountered during publishing, or nil on success.
*/
func (w *Worker) Queue(job core.TaskMessage) error {
	w.channelMu.Lock()
	defer w.channelMu.Unlock()
	if atomic.LoadInt32(&w.stopFlag) == 1 {
		return queue.ErrQueueShutdown
	}
	if err := w.startConsumerLocked(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), w.opts.publishTimeout)
	defer cancel()
	err := w.channel.PublishWithContext(
		ctx,
		w.opts.exchangeName, // exchange
		w.opts.routingKey,   // routing key
		false,               // mandatory
		false,               // immediate
		amqp.Publishing{
			Headers: amqp.Table{
				deliveryAttemptHeader: int64(1),
			},
			ContentType:     "text/plain",
			ContentEncoding: "",
			Body:            job.Bytes(),
			DeliveryMode:    amqp.Persistent,
			Priority:        0, // 0-9
		})

	if err != nil {
		return err
	}

	select {
	case confirmation, ok := <-w.confirmations:
		if !ok {
			return errors.New("RabbitMQ publisher confirmation channel closed")
		}
		if !confirmation.Ack {
			return errors.New("RabbitMQ publish was not acknowledged")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

/*
Request retrieves a new task message from the queue.
It starts the consumer if not already started, waits for a message, and unmarshals it into a job.Message.
If no message is received within a timeout, it returns queue.ErrNoTaskInQueue.

Returns:
- core.TaskMessage: The received task message, or nil if none.
- error: Any error encountered, or queue.ErrNoTaskInQueue if no task is available.
*/
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
			decodeErr := fmt.Errorf("decode RabbitMQ delivery: %w", err)
			if w.opts.autoAck {
				return nil, decodeErr
			}
			failure := management.NewFailure(
				management.ClassificationMalformed, "malformed_delivery", decodeErr,
			)
			if settleErr := w.settleRabbitFailure(task, failure); settleErr != nil {
				return nil, errors.Join(
					decodeErr,
					fmt.Errorf("dead-letter malformed RabbitMQ delivery: %w", settleErr),
				)
			}
			return nil, decodeErr
		}
		if !w.opts.autoAck {
			data.SetFailureAcknowledgement(
				func() error { return task.Ack(false) },
				func(handlerErr error) error { return w.settleRabbitFailure(task, handlerErr) },
			)
		}
		return data, nil
	case <-timer.C:
		return nil, queue.ErrNoTaskInQueue
	}
}

func (w *Worker) settleRabbitFailure(task amqp.Delivery, handlerErr error) error {
	if handlerErr == nil {
		return task.Nack(false, true)
	}
	resolution := management.ResolveFailure(handlerErr)
	classification := resolution.Classification
	code := resolution.Code
	if code == "" {
		code = "handler_failed"
	}
	if classification == management.ClassificationCanceled ||
		classification == management.ClassificationInfrastructure {
		return task.Nack(false, true)
	}
	attempt, validAttempt := rabbitDeliveryAttempt(task.Headers)
	if !validAttempt {
		classification = management.ClassificationMalformed
		code = "malformed_delivery_attempt"
		attempt = 1
	}
	terminal := classification == management.ClassificationPermanent ||
		classification == management.ClassificationMalformed ||
		attempt >= int64(w.opts.deadLetter.MaxDeliveryAttempts)
	exchange := w.opts.exchangeName
	routingKey := w.opts.routingKey
	nextAttempt := attempt + 1
	if terminal {
		exchange = w.opts.deadLetter.Exchange
		routingKey = w.opts.deadLetter.RoutingKey
		nextAttempt = attempt
		if attempt >= int64(w.opts.deadLetter.MaxDeliveryAttempts) &&
			classification == management.ClassificationRetryable {
			code = "attempts_exhausted"
		}
	}
	if w.channel == nil || w.confirmations == nil {
		return task.Nack(false, !terminal)
	}
	if err := w.publishRabbitSettlement(
		task, exchange, routingKey, nextAttempt, classification, code, terminal,
	); err != nil {
		requeueErr := task.Nack(false, true)
		if terminal {
			err = management.NewFailure(
				management.ClassificationInfrastructure,
				management.FailureCodeDeadLetterDestinationUnavailable,
				err,
			)
		}
		return errors.Join(err, requeueErr)
	}
	if err := task.Ack(false); err != nil {
		return fmt.Errorf("acknowledge RabbitMQ settlement source: %w", err)
	}

	return nil
}

func (w *Worker) publishRabbitSettlement(
	task amqp.Delivery,
	exchange string,
	routingKey string,
	attempt int64,
	classification management.Classification,
	failureCode string,
	terminal bool,
) error {
	headers := amqp.Table{deliveryAttemptHeader: attempt}
	if terminal {
		headers[classificationHeader] = string(classification)
		headers[failureCodeHeader] = failureCode
		headers[envelopeVersionHeader] = int64(management.CurrentEnvelopeVersion)
		headers[sourceQueueHeader] = w.opts.queue
		headers[sourceExchangeHeader] = w.opts.exchangeName
		headers[sourceRoutingKeyHeader] = w.opts.routingKey
	}
	w.channelMu.Lock()
	defer w.channelMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), w.opts.publishTimeout)
	defer cancel()
	if err := w.channel.PublishWithContext(
		ctx, exchange, routingKey, false, false, amqp.Publishing{
			Headers: headers, ContentType: task.ContentType,
			ContentEncoding: task.ContentEncoding, Body: task.Body,
			DeliveryMode: amqp.Persistent, Priority: task.Priority,
			CorrelationId: task.CorrelationId, MessageId: task.MessageId,
			Timestamp: task.Timestamp, Type: task.Type, AppId: task.AppId,
		},
	); err != nil {
		return fmt.Errorf("publish RabbitMQ settlement destination: %w", err)
	}
	select {
	case confirmation, ok := <-w.confirmations:
		if !ok {
			return errors.New("rabbitMQ settlement confirmation channel closed")
		}
		if !confirmation.Ack {
			return errors.New("rabbitMQ settlement publish was not acknowledged")
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func rabbitDeliveryAttempt(headers amqp.Table) (int64, bool) {
	if headers == nil {
		return 1, true
	}
	value, exists := headers[deliveryAttemptHeader]
	if !exists {
		return 1, true
	}
	var attempt int64
	switch typed := value.(type) {
	case int64:
		attempt = typed
	case int32:
		attempt = int64(typed)
	case int:
		attempt = int64(typed)
	default:
		return 0, false
	}

	if attempt < 1 || attempt > 101 {
		return 0, false
	}

	return attempt, true
}
