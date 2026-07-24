package redisdb

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionsConfigureRedisStreams(t *testing.T) {
	logger := queue.NewEmptyLogger()
	runErr := errors.New("run")
	opts := newOptions(
		WithAddr("redis:6379"),
		WithDB(2),
		WithCluster(),
		WithTLS(),
		WithSkipTLSVerify(),
		WithMaxLength(42),
		WithBlockTime(25*time.Millisecond),
		WithStreamName("jobs"),
		WithGroup("workers"),
		WithConsumer("worker-1"),
		WithUsername("user"),
		WithPassword("secret"),
		WithConnectionString("redis://redis:6379/2"),
		WithRunFunc(func(context.Context, core.TaskMessage) error { return runErr }),
		WithLogger(logger),
		WithRequestTimeout(30*time.Millisecond),
		WithConnectTimeout(35*time.Millisecond),
		WithReclaim(time.Minute, 2*time.Second, 16),
		WithFailureStream("jobs-failures"),
		WithDeadLetter("jobs-dead", 7),
		WithReplayDestinations("archive", "quarantine"),
		WithRecordRetention(500),
		WithCommandTimeout(3*time.Second),
	)

	assert.Equal(t, "redis:6379", opts.addr)
	assert.Equal(t, 2, opts.db)
	assert.True(t, opts.cluster)
	assert.Equal(t, int64(42), opts.maxLength)
	assert.Equal(t, 25*time.Millisecond, opts.blockTime)
	assert.Equal(t, "jobs", opts.streamName)
	assert.Equal(t, "workers", opts.group)
	assert.Equal(t, "worker-1", opts.consumer)
	assert.Equal(t, "user", opts.username)
	assert.Equal(t, "secret", opts.password)
	assert.Equal(t, "redis://redis:6379/2", opts.connectionString)
	assert.Equal(t, logger, opts.logger)
	assert.Equal(t, 30*time.Millisecond, opts.requestTimeout)
	assert.Equal(t, 35*time.Millisecond, opts.connectTimeout)
	assert.Equal(t, time.Minute, opts.reclaimMinIdle)
	assert.Equal(t, 2*time.Second, opts.reclaimInterval)
	assert.Equal(t, int64(16), opts.reclaimBatchSize)
	assert.Equal(t, "jobs-failures", opts.failureStream)
	assert.Equal(t, "jobs-dead", opts.deadLetterStream)
	assert.Equal(t, int64(7), opts.maxDeliveryAttempts)
	assert.Equal(t, map[string]struct{}{
		"archive": {}, "quarantine": {},
	}, opts.replayDestinations)
	assert.Equal(t, int64(500), opts.recordMaxLength)
	assert.Equal(t, 3*time.Second, opts.commandTimeout)
	assert.Equal(t, uint16(tls.VersionTLS12), opts.tls.MinVersion)
	assert.True(t, opts.tls.InsecureSkipVerify)
	assert.ErrorIs(t, opts.runFunc(context.Background(), nil), runErr)
	worker := &Worker{opts: opts}
	assert.Equal(t, "redis-streams", worker.BackendName())
	assert.Equal(t, "jobs", worker.QueueName())
}

func TestWorkerRejectsUnsafeDeadLetterConfigurationBeforeConnecting(t *testing.T) {
	t.Parallel()

	tests := map[string]Option{
		"reclaim idle":     WithReclaim(0, time.Second, 1),
		"reclaim interval": WithReclaim(time.Second, 0, 1),
		"reclaim batch":    WithReclaim(time.Second, time.Second, 0),
		"large batch":      WithReclaim(time.Second, time.Second, streamqueue.MaxBatchSize+1),
		"failure collision": func(options *options) {
			WithStreamName("jobs")(options)
			WithFailureStream("jobs")(options)
		},
		"dead collision": func(options *options) {
			WithStreamName("jobs")(options)
			WithDeadLetter("jobs", 5)(options)
		},
		"record collision": WithFailureStream("golang-queue-dead"),
		"attempt limit":    WithDeadLetter("dead", 1),
		"command timeout":  WithCommandTimeout(0),
		"empty replay":     WithReplayDestinations(),
		"blank replay":     WithReplayDestinations(" "),
		"duplicate replay": WithReplayDestinations("archive", "archive"),
		"failure replay":   WithReplayDestinations("golang-queue-failures"),
		"dead replay":      WithReplayDestinations("golang-queue-dead"),
		"record retention": WithRecordRetention(0),
	}
	for name, option := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			worker, err := NewWorkerE(WithAddr("127.0.0.1:1"), option)
			assert.Nil(t, worker)
			assert.ErrorIs(t, err, queue.ErrInvalidConfiguration)
		})
	}
}

func TestSkipTLSVerifyCreatesConfig(t *testing.T) {
	assert.True(t, newOptions(WithSkipTLSVerify()).tls.InsecureSkipVerify)
}

func TestDefaultRunFunctionSucceeds(t *testing.T) {
	assert.NoError(t, newOptions().runFunc(context.Background(), nil))
}

func TestWorkerQueuesRequestsAcknowledgesRunsAndShutsDown(t *testing.T) {
	server := miniredis.RunT(t)
	var handled []byte
	worker, err := NewWorkerE(
		WithAddr(server.Addr()),
		WithStreamName("jobs"),
		WithGroup("workers"),
		WithConsumer("worker-1"),
		WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second),
		WithRunFunc(func(_ context.Context, task core.TaskMessage) error {
			handled = append([]byte(nil), task.Payload()...)
			return nil
		}),
	)
	require.NoError(t, err)
	worker.startConsumer()
	message := job.NewMessage(rawMessage("payload"))

	require.NoError(t, worker.Queue(&message))
	received, err := worker.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("payload"), received.Payload())
	require.NoError(t, worker.Run(context.Background(), received))
	assert.Equal(t, []byte("payload"), handled)
	require.NoError(t, received.(*job.Message).Nack())
	require.NoError(t, received.(*job.Message).Ack())
	require.NoError(t, worker.Shutdown())
	assert.ErrorIs(t, worker.Shutdown(), queue.ErrQueueShutdown)
	assert.ErrorIs(t, worker.Queue(&message), queue.ErrQueueShutdown)
}

func TestWorkerConsumesMessagesQueuedBeforeConsumerGroupStarts(t *testing.T) {
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()),
		WithStreamName("jobs"),
		WithGroup("workers"),
		WithConsumer("worker-1"),
		WithBlockTime(time.Millisecond),
		WithRequestTimeout(20*time.Millisecond),
	)
	require.NoError(t, err)
	message := job.NewMessage(rawMessage("queued-before-start"))
	require.NoError(t, worker.Queue(&message))

	received, err := worker.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("queued-before-start"), received.Payload())
	require.NoError(t, received.(*job.Message).Ack())
	require.NoError(t, worker.Shutdown())
}

func TestWorkerConnectsWithConnectionString(t *testing.T) {
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(WithConnectionString("redis://" + server.Addr() + "/0"))
	require.NoError(t, err)
	require.NoError(t, worker.Shutdown())
}

func TestLegacyConstructorReturnsConnectedWorker(t *testing.T) {
	server := miniredis.RunT(t)
	worker := NewWorker(WithAddr(server.Addr()))

	require.NoError(t, worker.Shutdown())
}

func TestWorkerConstructorsReturnConnectionErrors(t *testing.T) {
	started := time.Now()
	worker, err := NewWorkerE(
		WithAddr("127.0.0.1:1"),
		WithConnectTimeout(20*time.Millisecond),
	)
	assert.Nil(t, worker)
	assert.ErrorContains(t, err, "connect to Redis")
	assert.Less(t, time.Since(started), 250*time.Millisecond)

	assert.Panics(t, func() {
		NewWorker(WithAddr("127.0.0.1:1"), WithConnectTimeout(20*time.Millisecond))
	})
}

func TestClusterConstructorReturnsBoundedConnectionError(t *testing.T) {
	started := time.Now()
	worker, err := NewWorkerE(
		WithAddr("127.0.0.1:1"),
		WithCluster(),
		WithConnectTimeout(20*time.Millisecond),
	)
	assert.Nil(t, worker)
	assert.ErrorContains(t, err, "connect to Redis")
	assert.Less(t, time.Since(started), 250*time.Millisecond)
}

func TestRequestReturnsPayloadAndChannelErrors(t *testing.T) {
	t.Run("malformed body", func(t *testing.T) {
		worker := workerWithTask(redis.XMessage{
			ID: "1-0", Values: map[string]any{"body": "not-json"},
		})

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.Error(t, err)
	})

	t.Run("oversized body", func(t *testing.T) {
		worker := workerWithTask(redis.XMessage{
			ID: "1-0",
			Values: map[string]any{
				"body": strings.Repeat("x", job.DefaultMaxMessageBytes+1),
			},
		})

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorIs(t, err, job.ErrMessageTooLarge)
	})

	t.Run("missing body", func(t *testing.T) {
		worker := workerWithTask(redis.XMessage{ID: "1-0", Values: map[string]any{}})

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.Error(t, err)
	})

	t.Run("closed", func(t *testing.T) {
		tasks := make(chan redis.XMessage)
		close(tasks)
		worker := &Worker{tasks: tasks, opts: newOptions()}
		worker.startOnce.Do(func() {})

		message, err := worker.Request()
		assert.Nil(t, message)
		assert.ErrorIs(t, err, queue.ErrQueueHasBeenClosed)
	})
}

func TestRequestDeadLettersMalformedDeliveryBeforeAcknowledgingSource(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithConsumer("worker-1"), WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second), WithDeadLetter("jobs-dead", 5),
		WithFailureStream("jobs-failures"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	_, err = worker.rdb.XAdd(context.Background(), &redis.XAddArgs{
		Stream: "jobs", Values: map[string]any{"body": "not-json"},
	}).Result()
	require.NoError(t, err)

	message, err := worker.Request()
	assert.Nil(t, message)
	assert.Error(t, err)
	pending, err := worker.rdb.XPending(context.Background(), "jobs", "workers").Result()
	require.NoError(t, err)
	assert.Zero(t, pending.Count)
	records, err := worker.rdb.XRange(context.Background(), "jobs-dead", "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, string(management.ClassificationMalformed), records[0].Values[classificationField])
	assert.Equal(t, "malformed_delivery", records[0].Values[failureCodeField])
	assert.Equal(t, "1", records[0].Values[envelopeVersionField])
}

func TestRequestDeadLettersPermanentHandlerFailureAndRecordsAttempt(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithConsumer("worker-1"), WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second), WithDeadLetter("jobs-dead", 5),
		WithFailureStream("jobs-failures"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	queued := job.NewMessage(rawMessage("permanent"))
	require.NoError(t, worker.Queue(&queued))
	delivery, err := worker.Request()
	require.NoError(t, err)
	handlerErr := management.NewFailure(
		management.ClassificationPermanent,
		"invalid_order",
		errors.New("invalid order"),
	)
	require.NoError(t, delivery.(*job.Message).NackFailure(handlerErr))

	pending, err := worker.rdb.XPending(context.Background(), "jobs", "workers").Result()
	require.NoError(t, err)
	assert.Zero(t, pending.Count)
	for _, stream := range []string{"jobs-failures", "jobs-dead"} {
		records, rangeErr := worker.rdb.XRange(context.Background(), stream, "-", "+").Result()
		require.NoError(t, rangeErr)
		require.Len(t, records, 1)
		assert.Equal(t, string(management.ClassificationPermanent), records[0].Values[classificationField])
		assert.Equal(t, "invalid_order", records[0].Values[failureCodeField])
	}
}

func TestWorkerReclaimsRetryableDeliveryAndDeadLettersAtExactLimit(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	server.SetTime(time.Unix(100, 0))
	first, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithConsumer("worker-1"), WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second), WithReclaim(time.Second, time.Millisecond, 1),
		WithDeadLetter("jobs-dead", 2), WithFailureStream("jobs-failures"),
		WithLogger(queue.NewEmptyLogger()),
	)
	require.NoError(t, err)
	queued := job.NewMessage(rawMessage("retryable"))
	require.NoError(t, first.Queue(&queued))
	delivery, err := first.Request()
	require.NoError(t, err)
	require.NoError(t, delivery.(*job.Message).NackFailure(errors.New("retry")))
	require.NoError(t, first.Shutdown())

	server.SetTime(time.Unix(102, 0))
	second, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithConsumer("worker-2"), WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second), WithReclaim(time.Second, time.Millisecond, 1),
		WithDeadLetter("jobs-dead", 2), WithFailureStream("jobs-failures"),
		WithLogger(queue.NewEmptyLogger()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Shutdown() })
	reclaimed, err := second.Request()
	require.NoError(t, err)
	assert.Equal(t, []byte("retryable"), reclaimed.Payload())
	require.NoError(t, reclaimed.(*job.Message).NackFailure(errors.New("retry again")))

	pending, err := second.rdb.XPending(context.Background(), "jobs", "workers").Result()
	require.NoError(t, err)
	assert.Zero(t, pending.Count)
	records, err := second.rdb.XRange(context.Background(), "jobs-dead", "-", "+").Result()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "2", records[0].Values[deliveryAttemptsField])
	assert.Equal(t, "attempts_exhausted", records[0].Values[failureCodeField])
}

func TestRequestUsesConfiguredTimeout(t *testing.T) {
	worker := &Worker{
		tasks: make(chan redis.XMessage),
		opts:  newOptions(WithRequestTimeout(time.Millisecond)),
	}
	worker.startOnce.Do(func() {})

	started := time.Now()
	message, err := worker.Request()

	assert.Nil(t, message)
	assert.ErrorIs(t, err, queue.ErrNoTaskInQueue)
	assert.Less(t, time.Since(started), 100*time.Millisecond)
}

func TestClusterWorkerShutdownClosesResources(t *testing.T) {
	client := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}})
	worker := &Worker{
		rdb:   client,
		tasks: make(chan redis.XMessage),
		stop:  make(chan struct{}),
		exit:  make(chan struct{}),
		opts:  newOptions(),
	}

	require.NoError(t, worker.Shutdown())
}

func TestStartConsumerLogsExistingGroupError(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	require.NoError(t, client.XGroupCreateMkStream(
		context.Background(), "jobs", "workers", "$",
	).Err())
	worker, err := NewWorkerE(
		WithAddr(server.Addr()),
		WithStreamName("jobs"),
		WithGroup("workers"),
		WithBlockTime(time.Millisecond),
		WithLogger(queue.NewEmptyLogger()),
	)
	require.NoError(t, err)

	worker.startConsumer()
	require.NoError(t, worker.Shutdown())
	require.NoError(t, client.Close())
}

func TestFetchTaskHandlesReadErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "empty stream", err: redis.Nil},
		{name: "backend failure", err: errors.New("read failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			stop := make(chan struct{})
			worker := &Worker{
				stop: stop,
				opts: newOptions(WithLogger(queue.NewEmptyLogger())),
				readGroup: func(context.Context, *redis.XReadGroupArgs) ([]redis.XStream, error) {
					close(stop)
					return nil, test.err
				},
			}

			worker.fetchTask()
		})
	}
}

func TestFetchTaskDoesNotDuplicatePendingDeliveryDuringShutdown(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	stop := make(chan struct{})
	exit := make(chan struct{})
	worker := &Worker{
		rdb:   client,
		tasks: make(chan redis.XMessage),
		stop:  stop,
		exit:  exit,
		opts: newOptions(
			WithStreamName("jobs"),
			WithLogger(queue.NewEmptyLogger()),
		),
		readGroup: func(context.Context, *redis.XReadGroupArgs) ([]redis.XStream, error) {
			close(stop)
			return []redis.XStream{{Messages: []redis.XMessage{{
				ID: "1-0", Values: map[string]any{"body": "payload"},
			}}}}, nil
		},
	}

	worker.fetchTask()
	select {
	case <-exit:
	default:
		t.Fatal("fetchTask did not signal shutdown completion")
	}
	length, err := client.XLen(context.Background(), "jobs").Result()
	require.NoError(t, err)
	assert.Zero(t, length)
	require.NoError(t, client.Close())
}

func TestShutdownWaitsForFetchTaskExitSignal(t *testing.T) {
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()),
		WithStreamName("shutdown"),
		WithLogger(queue.NewEmptyLogger()),
	)
	require.NoError(t, err)
	worker.readGroup = func(ctx context.Context, _ *redis.XReadGroupArgs) ([]redis.XStream, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	worker.startConsumer()
	require.NoError(t, worker.Shutdown())
	select {
	case <-worker.exit:
	default:
		t.Fatal("fetchTask did not signal shutdown completion")
	}
}

func TestStartConsumerAfterShutdownDoesNothing(t *testing.T) {
	worker := &Worker{stopFlag: 1}

	worker.startConsumer()

	assert.Zero(t, atomic.LoadInt32(&worker.started))
}

func TestStatsReportsOutstandingDepthAndOldestJobAge(t *testing.T) {
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()),
		WithStreamName("jobs"),
		WithGroup("workers"),
		WithConsumer("worker-1"),
		WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second),
	)
	require.NoError(t, err)
	worker.startConsumer()
	message := job.NewMessage(rawMessage("payload"))
	require.NoError(t, worker.Queue(&message))

	if !assert.Eventually(t, func() bool {
		stats, statsErr := worker.Stats(context.Background())
		return statsErr == nil && stats.Depth >= 1 && stats.OldestJobAge >= 0
	}, time.Second, time.Millisecond) {
		stats, statsErr := worker.Stats(context.Background())
		t.Fatalf("unexpected stats: %+v, error: %v", stats, statsErr)
	}

	received, err := worker.Request()
	require.NoError(t, err)
	stats, err := worker.Stats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Pending)
	assert.Equal(t, stats.Pending+stats.Lag, stats.Depth)
	assert.GreaterOrEqual(t, stats.OldestJobAge, time.Duration(0))
	require.NoError(t, received.(*job.Message).Ack())

	stats, err = worker.Stats(context.Background())
	require.NoError(t, err)
	assert.Zero(t, stats.Pending)
	assert.Equal(t, stats.Lag, stats.Depth)
	require.NoError(t, worker.Shutdown())
}

func TestStatsReturnsBackendAndGroupErrors(t *testing.T) {
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(WithAddr(server.Addr()))
	require.NoError(t, err)

	stats, err := worker.Stats(context.Background())
	assert.Zero(t, stats)
	assert.Error(t, err)

	require.NoError(t, worker.rdb.(*redis.Client).Close())
	stats, err = worker.Stats(context.Background())
	assert.Zero(t, stats)
	assert.Error(t, err)
}

func TestStatsCoversGroupAndCommandBranches(t *testing.T) {
	expected := errors.New("backend")
	for _, test := range []struct {
		name         string
		groups       []redis.XInfoGroup
		pending      []redis.XPendingExt
		messages     []redis.XMessage
		pendingErr   error
		rangeErr     error
		wantErr      bool
		wantDepth    int64
		wantLagKnown bool
	}{
		{name: "group missing", groups: []redis.XInfoGroup{{Name: "other"}}, wantErr: true},
		{name: "lag unknown", groups: []redis.XInfoGroup{{Name: "workers", Lag: -1}}, wantDepth: -1},
		{name: "empty", groups: []redis.XInfoGroup{{Name: "workers"}}, wantLagKnown: true},
		{name: "pending error", groups: []redis.XInfoGroup{{Name: "workers", Pending: 1}}, pendingErr: expected, wantErr: true},
		{name: "pending empty", groups: []redis.XInfoGroup{{Name: "workers", Pending: 1}}, wantDepth: 1, wantLagKnown: true},
		{name: "range error", groups: []redis.XInfoGroup{{Name: "workers", Lag: 1}}, rangeErr: expected, wantErr: true},
		{name: "range empty", groups: []redis.XInfoGroup{{Name: "workers", Lag: 1}}, wantDepth: 1, wantLagKnown: true},
		{name: "invalid pending ID", groups: []redis.XInfoGroup{{Name: "workers", Pending: 1}}, pending: []redis.XPendingExt{{ID: "invalid"}}, wantErr: true},
		{name: "invalid queued ID", groups: []redis.XInfoGroup{{Name: "workers", Lag: 1}}, messages: []redis.XMessage{{ID: "invalid"}}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			worker := &Worker{
				opts: newOptions(WithGroup("workers")),
				readGroups: func(context.Context, string) ([]redis.XInfoGroup, error) {
					return test.groups, nil
				},
				readPending: func(context.Context, *redis.XPendingExtArgs) ([]redis.XPendingExt, error) {
					return test.pending, test.pendingErr
				},
				readRange: func(context.Context, string, string, string, int64) ([]redis.XMessage, error) {
					return test.messages, test.rangeErr
				},
			}

			stats, err := worker.Stats(context.Background())
			if test.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.wantDepth, stats.Depth)
			assert.Equal(t, test.wantLagKnown, stats.LagKnown)
		})
	}
}

func TestStreamMessageAgeValidatesRedisIDs(t *testing.T) {
	now := time.UnixMilli(1_000)
	age, err := streamMessageAge("500-0", now)
	require.NoError(t, err)
	assert.Equal(t, 500*time.Millisecond, age)

	age, err = streamMessageAge("2000-0", now)
	require.NoError(t, err)
	assert.Zero(t, age)

	for _, id := range []string{"invalid", "nope-0"} {
		age, err = streamMessageAge(id, now)
		assert.Zero(t, age)
		assert.Error(t, err)
	}
}

func workerWithTask(task redis.XMessage) *Worker {
	tasks := make(chan redis.XMessage, 1)
	tasks <- task
	worker := &Worker{
		tasks: tasks,
		ack:   func(string) error { return nil },
		opts:  newOptions(),
	}
	worker.startOnce.Do(func() {})
	return worker
}

type rawMessage string

func (m rawMessage) Bytes() []byte { return []byte(m) }
