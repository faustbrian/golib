package redisdb

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/safeerr"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"

	"github.com/appleboy/com/bytesconv"
	"github.com/redis/go-redis/v9"
)

var _ core.Worker = (*Worker)(nil)
var _ core.WorkerMetadata = (*Worker)(nil)

const (
	streamBodyField               = "body"
	originalIDField               = "original_id"
	deliveryAttemptsField         = "delivery_attempts"
	envelopeVersionField          = "envelope_version"
	classificationField           = "classification"
	failureCodeField              = "failure_code"
	sourceStreamField             = "source_stream"
	consumerGroupField            = "consumer_group"
	replayOriginalDeadLetterField = "replay_original_dead_letter_id"
	replayPriorDeadLetterField    = "replay_prior_dead_letter_id"
	replayGenerationField         = "replay_generation"
)

// BackendName identifies Redis Streams in lifecycle events.
func (*Worker) BackendName() string { return "redis-streams" }

// QueueName returns the configured Redis stream.
func (w *Worker) QueueName() string { return w.opts.streamName }

// Stats describes outstanding work for this worker's Redis consumer group.
// Depth is Pending plus Lag and is -1 when Redis cannot determine group lag.
type Stats struct {
	Depth        int64
	Pending      int64
	Lag          int64
	LagKnown     bool
	OldestJobAge time.Duration
}

// Worker for Redis
type Worker struct {
	// redis config
	rdb               redis.Cmdable
	readGroup         func(context.Context, *redis.XReadGroupArgs) ([]redis.XStream, error)
	readGroups        func(context.Context, string) ([]redis.XInfoGroup, error)
	readPending       func(context.Context, *redis.XPendingExtArgs) ([]redis.XPendingExt, error)
	readRange         func(context.Context, string, string, string, int64) ([]redis.XMessage, error)
	autoClaim         func(context.Context, *redis.XAutoClaimArgs) ([]redis.XMessage, string, error)
	readContext       context.Context
	cancelRead        context.CancelFunc
	tasks             chan redis.XMessage
	ack               func(string) error
	stopFlag          int32
	started           int32
	lifecycleMu       sync.Mutex
	stopOnce          sync.Once
	startOnce         sync.Once
	stop              chan struct{}
	exit              chan struct{}
	opts              options
	startedAt         time.Time
	now               func() time.Time
	currentJobs       atomic.Uint32
	groupLagSupported bool
	controlMu         sync.Mutex
	controlApplyMu    sync.Mutex
	controlEntries    map[string]*redisControlEntry
	controlCapacity   int
}

// NewWorker for struc
func NewWorker(opts ...Option) *Worker {
	w, err := NewWorkerE(opts...)
	if err != nil {
		panic(err)
	}

	return w
}

// NewWorkerE creates a worker and returns connection and configuration errors.
func NewWorkerE(opts ...Option) (*Worker, error) {
	var err error
	readContext, cancelRead := context.WithCancel(context.Background())
	w := &Worker{
		opts:        newOptions(opts...),
		readContext: readContext,
		cancelRead:  cancelRead,
		stop:        make(chan struct{}),
		exit:        make(chan struct{}),
		tasks:       make(chan redis.XMessage),
		startedAt:   time.Now().UTC(),
		now:         time.Now,
	}
	if err := w.opts.validateDeadLetter(); err != nil {
		cancelRead()
		return nil, err
	}
	if w.opts.management != nil && w.opts.management.Validate() != nil {
		return nil, ErrInvalidManagementStatus
	}

	if w.opts.connectionString != "" {
		options, err := redis.ParseURL(w.opts.connectionString)
		if err != nil {
			return nil, safeerr.Wrap("parse Redis connection string", err)
		}
		configureRedisOptions(options, w.opts.connectTimeout)
		w.rdb = redis.NewClient(options)
	} else if w.opts.addr != "" {
		if w.opts.cluster {
			w.rdb = redis.NewClusterClient(&redis.ClusterOptions{
				Addrs:                 strings.Split(w.opts.addr, ","),
				Username:              w.opts.username,
				Password:              w.opts.password,
				TLSConfig:             w.opts.tls,
				DialTimeout:           w.opts.connectTimeout,
				DialerRetries:         -1,
				MaxRedirects:          -1,
				ContextTimeoutEnabled: true,
			})
		} else {
			options := &redis.Options{
				Addr:      w.opts.addr,
				Username:  w.opts.username,
				Password:  w.opts.password,
				DB:        w.opts.db,
				TLSConfig: w.opts.tls,
			}
			configureRedisOptions(options, w.opts.connectTimeout)
			w.rdb = redis.NewClient(options)
		}
	}
	if w.rdb == nil {
		return nil, errors.New("redis address or connection string is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), w.opts.connectTimeout)
	defer cancel()
	_, err = w.rdb.Ping(ctx).Result()
	if err != nil {
		closeRedisClient(w.rdb)
		return nil, fmt.Errorf("connect to Redis: %w", err)
	}
	if w.opts.management != nil {
		info, infoErr := w.rdb.Info(ctx, "server").Result()
		w.groupLagSupported = infoErr == nil && redisGroupLagSupported(info)
	}
	w.ack = func(id string) error {
		return w.rdb.XAck(context.Background(), w.opts.streamName, w.opts.group, id).Err()
	}
	w.readGroup = func(ctx context.Context, args *redis.XReadGroupArgs) ([]redis.XStream, error) {
		return w.rdb.XReadGroup(ctx, args).Result()
	}
	w.readGroups = func(ctx context.Context, stream string) ([]redis.XInfoGroup, error) {
		return w.rdb.XInfoGroups(ctx, stream).Result()
	}
	w.readPending = func(ctx context.Context, args *redis.XPendingExtArgs) ([]redis.XPendingExt, error) {
		return w.rdb.XPendingExt(ctx, args).Result()
	}
	w.readRange = func(
		ctx context.Context, stream string, start string, stop string, count int64,
	) ([]redis.XMessage, error) {
		return w.rdb.XRangeN(ctx, stream, start, stop, count).Result()
	}
	w.autoClaim = func(
		ctx context.Context, args *redis.XAutoClaimArgs,
	) ([]redis.XMessage, string, error) {
		return w.rdb.XAutoClaim(ctx, args).Result()
	}

	return w, nil
}

func configureRedisOptions(options *redis.Options, timeout time.Duration) {
	options.DialTimeout = timeout
	options.DialerRetries = -1
	options.MaxRetries = -1
	options.ContextTimeoutEnabled = true
}

func closeRedisClient(client redis.Cmdable) {
	switch value := client.(type) {
	case *redis.Client:
		_ = value.Close()
	case *redis.ClusterClient:
		_ = value.Close()
	}
}

func (w *Worker) startConsumer() {
	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()
	if atomic.LoadInt32(&w.stopFlag) == 1 {
		return
	}

	w.startOnce.Do(func() {
		if err := w.rdb.XGroupCreateMkStream(
			context.Background(),
			w.opts.streamName,
			w.opts.group,
			"0",
		).Err(); err != nil {
			w.opts.logger.Error(err)
		}

		atomic.StoreInt32(&w.started, 1)
		go w.fetchTask()
	})
}

func (w *Worker) fetchTask() {
	if w.exit != nil {
		defer close(w.exit)
	}

	reclaimCursor := "0-0"
	nextReclaim := time.Time{}
	for {
		select {
		case <-w.stop:
			return
		default:
		}

		ctx := w.readContext
		if ctx == nil {
			ctx = context.Background()
		}
		if w.autoClaim != nil && !time.Now().Before(nextReclaim) {
			claimed, next, reclaimErr := w.autoClaim(ctx, &redis.XAutoClaimArgs{
				Stream: w.opts.streamName, Group: w.opts.group,
				Consumer: w.opts.consumer, MinIdle: w.opts.reclaimMinIdle,
				Start: reclaimCursor, Count: w.opts.reclaimBatchSize,
			})
			nextReclaim = time.Now().Add(w.opts.reclaimInterval)
			if reclaimErr != nil && !errors.Is(reclaimErr, redis.Nil) {
				if ctx.Err() == nil {
					w.opts.logger.Errorf("error while reclaiming Redis stream entries: %v", reclaimErr)
				}
			} else {
				if next == "" || next == "0-0" {
					reclaimCursor = "0-0"
				} else {
					reclaimCursor = next
				}
				for _, message := range claimed {
					select {
					case w.tasks <- message:
					case <-w.stop:
						return
					}
				}
			}
		}
		blockTime := w.opts.blockTime
		if blockTime <= 0 || blockTime > time.Second {
			blockTime = time.Second
		}
		data, err := w.readGroup(ctx, &redis.XReadGroupArgs{
			Group:    w.opts.group,
			Consumer: w.opts.consumer,
			Streams:  []string{w.opts.streamName, ">"},
			// count is number of entries we want to read from redis
			Count: 1,
			// we use the block command to make sure if no entry is found we wait
			// until an entry is found
			Block: blockTime,
		})
		if err != nil {
			if errors.Is(err, redis.Nil) {
				w.opts.logger.Infof("no messages available in Redis stream [%s]", w.opts.streamName)
				continue
			}
			w.opts.logger.Errorf("error while reading from redis %v", err)
			continue
		}
		// we have received the data we should loop it and queue the messages
		// so that our tasks can start processing
		for _, result := range data {
			for _, message := range result.Messages {
				select {
				case w.tasks <- message:
				case <-w.stop:
					w.opts.logger.Info("leave pending task for recovery: ", message.ID)
					return
				}
			}
		}
	}
}

// Shutdown worker
func (w *Worker) Shutdown() error {
	if !atomic.CompareAndSwapInt32(&w.stopFlag, 0, 1) {
		return queue.ErrQueueShutdown
	}

	w.lifecycleMu.Lock()
	defer w.lifecycleMu.Unlock()

	w.stopOnce.Do(func() {
		close(w.stop)
		if w.cancelRead != nil {
			w.cancelRead()
		}

		if atomic.LoadInt32(&w.started) == 1 {
			<-w.exit
		}

		closeRedisClient(w.rdb)
		close(w.tasks)
	})
	return nil
}

func (w *Worker) queue(data interface{}) error {
	ctx := context.Background()

	// Publish a message.
	err := w.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: w.opts.streamName,
		MaxLen: w.opts.maxLength,
		Values: data,
	}).Err()

	return err
}

// Queue send notification to queue
func (w *Worker) Queue(task core.TaskMessage) error {
	if atomic.LoadInt32(&w.stopFlag) == 1 {
		return queue.ErrQueueShutdown
	}

	return w.queue(map[string]interface{}{"body": bytesconv.BytesToStr(task.Bytes())})
}

// Run start the worker
func (w *Worker) Run(ctx context.Context, task core.TaskMessage) error {
	w.currentJobs.Add(1)
	defer w.currentJobs.Add(^uint32(0))
	return w.opts.runFunc(ctx, task)
}

// Request a new task
func (w *Worker) Request() (core.TaskMessage, error) {
	w.startConsumer()
	timer := time.NewTimer(w.opts.requestTimeout)
	defer timer.Stop()
	select {
	case task, ok := <-w.tasks:
		if !ok {
			return nil, queue.ErrQueueHasBeenClosed
		}
		body, ok := task.Values["body"].(string)
		if !ok {
			decodeErr := errors.New("redis stream message body must be a string")
			if w.rdb != nil {
				if deadLetterErr := w.deadLetterMalformed(task, nil, "malformed_delivery"); deadLetterErr != nil {
					return nil, errors.Join(decodeErr, deadLetterErr)
				}
			}
			return nil, decodeErr
		}
		data, err := job.DecodeE(bytesconv.StrToBytes(body), job.DefaultMaxMessageBytes)
		if err != nil {
			deadLetterBody := []byte(body)
			failureCode := "malformed_delivery"
			if errors.Is(err, job.ErrMessageTooLarge) {
				deadLetterBody = nil
				failureCode = "message_too_large"
			}
			if w.rdb != nil {
				if deadLetterErr := w.deadLetterMalformed(
					task, deadLetterBody, failureCode,
				); deadLetterErr != nil {
					return nil, errors.Join(fmt.Errorf("decode Redis stream message: %w", err), deadLetterErr)
				}
			}
			return nil, fmt.Errorf("decode Redis stream message: %w", err)
		}
		var once sync.Once
		var settlementErr error
		settle := func(action func() error) error {
			once.Do(func() { settlementErr = action() })
			return settlementErr
		}
		data.SetFailureAcknowledgement(
			func() error { return settle(func() error { return w.ack(task.ID) }) },
			func(handlerErr error) error {
				return settle(func() error {
					return w.settleHandlerFailure(task, []byte(body), handlerErr)
				})
			},
		)
		return data, nil
	case <-timer.C:
		return nil, queue.ErrNoTaskInQueue
	}
}

func (w *Worker) deadLetterMalformed(
	message redis.XMessage,
	body []byte,
	failureCode string,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), w.opts.commandTimeout)
	defer cancel()
	attempts, err := w.pendingAttempts(ctx, message.ID)
	if err != nil {
		return fmt.Errorf("inspect Redis stream delivery attempts: %w", err)
	}
	failure := streamqueue.FailureMetadata{
		Classification: management.ClassificationMalformed,
		Code:           failureCode,
	}
	lineage, err := redisLineageFromValues(message.Values)
	if err != nil {
		return fmt.Errorf("inspect Redis stream replay lineage: %w", err)
	}
	if err := w.appendRecordWithLineage(
		ctx, w.opts.deadLetterStream, message.ID, body, attempts, failure,
		lineage,
	); err != nil {
		return management.NewFailure(
			management.ClassificationInfrastructure,
			management.FailureCodeDeadLetterDestinationUnavailable,
			err,
		)
	}
	return w.acknowledgeRecord(ctx, message.ID)
}

func (w *Worker) settleHandlerFailure(
	message redis.XMessage,
	body []byte,
	handlerErr error,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), w.opts.commandTimeout)
	defer cancel()
	attempts, err := w.pendingAttempts(ctx, message.ID)
	if err != nil {
		return fmt.Errorf("inspect Redis stream delivery attempts: %w", err)
	}
	failure := redisFailureMetadata(handlerErr, "handler_failed")
	lineage, err := redisLineageFromValues(message.Values)
	if err != nil {
		return fmt.Errorf("inspect Redis stream replay lineage: %w", err)
	}
	if err := w.appendRecordWithLineage(
		ctx, w.opts.failureStream, message.ID, body, attempts, failure,
		lineage,
	); err != nil {
		return fmt.Errorf("append Redis stream failure: %w", err)
	}
	if !redisTerminalFailure(handlerErr, attempts, w.opts.maxDeliveryAttempts) {
		return nil
	}
	if attempts >= w.opts.maxDeliveryAttempts && failure.Code == "handler_failed" {
		failure.Code = "attempts_exhausted"
	}
	if err := w.appendRecordWithLineage(
		ctx, w.opts.deadLetterStream, message.ID, body, attempts, failure,
		lineage,
	); err != nil {
		return management.NewFailure(
			management.ClassificationInfrastructure,
			management.FailureCodeDeadLetterDestinationUnavailable,
			err,
		)
	}

	return w.acknowledgeRecord(ctx, message.ID)
}

func redisFailureMetadata(err error, fallbackCode string) streamqueue.FailureMetadata {
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

func redisTerminalFailure(handlerErr error, attempts, maximumAttempts int64) bool {
	switch management.ClassifyFailure(handlerErr) {
	case management.ClassificationPermanent, management.ClassificationMalformed:
		return true
	case management.ClassificationRetryable:
		return attempts >= maximumAttempts
	default:
		return false
	}
}

func (w *Worker) appendRecord(
	ctx context.Context,
	stream string,
	originalID string,
	body []byte,
	attempts int64,
	failure streamqueue.FailureMetadata,
) error {
	return w.appendRecordWithLineage(
		ctx, stream, originalID, body, attempts, failure, redisReplayLineage{},
	)
}

type redisReplayLineage struct {
	original   string
	prior      string
	generation uint32
}

func (w *Worker) appendRecordWithLineage(
	ctx context.Context,
	stream string,
	originalID string,
	body []byte,
	attempts int64,
	failure streamqueue.FailureMetadata,
	lineage redisReplayLineage,
) error {
	values := map[string]any{
		streamBodyField:       body,
		originalIDField:       originalID,
		deliveryAttemptsField: attempts,
		envelopeVersionField:  management.CurrentEnvelopeVersion,
		classificationField:   string(failure.Classification),
		failureCodeField:      failure.Code,
		sourceStreamField:     w.opts.streamName,
		consumerGroupField:    w.opts.group,
	}
	if lineage.generation > 0 {
		values[replayOriginalDeadLetterField] = lineage.original
		values[replayPriorDeadLetterField] = lineage.prior
		values[replayGenerationField] = strconv.FormatUint(uint64(lineage.generation), 10)
	}
	if err := w.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: stream,
		MaxLen: w.opts.recordMaxLength,
		Approx: false,
		Values: values,
	}).Err(); err != nil {
		return err
	}

	return nil
}

func redisLineageFromValues(values map[string]any) (redisReplayLineage, error) {
	original, originalOK := redisRecordString(values[replayOriginalDeadLetterField])
	prior, priorOK := redisRecordString(values[replayPriorDeadLetterField])
	generationText, generationOK := redisRecordString(values[replayGenerationField])
	if !originalOK && !priorOK && !generationOK {
		return redisReplayLineage{}, nil
	}
	if !originalOK || !priorOK || !generationOK || strings.TrimSpace(original) == "" ||
		strings.TrimSpace(prior) == "" || len(original) > management.MaxIdentityBytes ||
		len(prior) > management.MaxIdentityBytes {
		return redisReplayLineage{}, errors.New("malformed replay lineage")
	}
	generation, err := strconv.ParseUint(generationText, 10, 32)
	if err != nil || generation == 0 {
		return redisReplayLineage{}, errors.New("malformed replay generation")
	}

	return redisReplayLineage{
		original: original, prior: prior, generation: uint32(generation),
	}, nil
}

func (w *Worker) acknowledgeRecord(ctx context.Context, id string) error {
	acknowledged, err := w.rdb.XAck(ctx, w.opts.streamName, w.opts.group, id).Result()
	if err != nil {
		return fmt.Errorf("settle Redis stream dead letter source: %w", err)
	}
	if acknowledged != 1 {
		return management.NewFailure(
			management.ClassificationInfrastructure,
			management.FailureCodeLeaseLost,
			errors.New("redis stream delivery is no longer pending"),
		)
	}

	return nil
}

func (w *Worker) pendingAttempts(ctx context.Context, id string) (int64, error) {
	pending, err := w.rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: w.opts.streamName, Group: w.opts.group,
		Start: id, End: id, Count: 1,
	}).Result()
	if err != nil {
		return 0, err
	}
	if len(pending) != 1 || pending[0].ID != id || pending[0].RetryCount < 1 {
		return 0, errors.New("redis stream pending delivery is unavailable")
	}

	return pending[0].RetryCount, nil
}

// Stats returns consumer-group depth and the age of its oldest outstanding job.
func (w *Worker) Stats(ctx context.Context) (Stats, error) {
	groups, err := w.readGroups(ctx, w.opts.streamName)
	if err != nil {
		return Stats{}, fmt.Errorf("read Redis stream groups: %w", err)
	}
	var group *redis.XInfoGroup
	for index := range groups {
		if groups[index].Name == w.opts.group {
			group = &groups[index]
			break
		}
	}
	if group == nil {
		return Stats{}, fmt.Errorf("redis stream group %q does not exist", w.opts.group)
	}

	sharedStats := (streamqueue.GroupState{Pending: group.Pending, Lag: group.Lag}).Stats()
	stats := Stats{
		Depth: sharedStats.Depth, Pending: sharedStats.Pending,
		Lag: sharedStats.Lag, LagKnown: sharedStats.LagKnown,
	}
	if group.Pending == 0 && group.Lag == 0 {
		return stats, nil
	}

	var oldestIDs []string
	if group.Pending > 0 {
		pending, pendingErr := w.readPending(ctx, &redis.XPendingExtArgs{
			Stream: w.opts.streamName,
			Group:  w.opts.group,
			Start:  "-",
			End:    "+",
			Count:  1,
		})
		if pendingErr != nil {
			return Stats{}, fmt.Errorf("read Redis pending jobs: %w", pendingErr)
		}
		if len(pending) > 0 {
			oldestIDs = append(oldestIDs, pending[0].ID)
		}
	}
	if group.Lag > 0 {
		start := "(" + group.LastDeliveredID
		messages, rangeErr := w.readRange(ctx, w.opts.streamName, start, "+", 1)
		if rangeErr != nil {
			return Stats{}, fmt.Errorf("read Redis queued jobs: %w", rangeErr)
		}
		if len(messages) > 0 {
			oldestIDs = append(oldestIDs, messages[0].ID)
		}
	}

	now := time.Now()
	for _, id := range oldestIDs {
		age, ageErr := streamMessageAge(id, now)
		if ageErr != nil {
			return Stats{}, ageErr
		}
		if age > stats.OldestJobAge {
			stats.OldestJobAge = age
		}
	}
	return stats, nil
}

func streamMessageAge(id string, now time.Time) (time.Duration, error) {
	age, err := streamqueue.MessageAge(id, now)
	if err != nil {
		return 0, fmt.Errorf("invalid Redis stream message ID: %w", err)
	}
	return age, nil
}
