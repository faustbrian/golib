package redisdb

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type redisCommandFault struct {
	mu      sync.Mutex
	command string
	failAt  int
	calls   int
	err     error
	stop    bool
}

type stagedCanceledContext struct {
	context.Context
	done  chan struct{}
	calls atomic.Uint32
}

func (c *stagedCanceledContext) Done() <-chan struct{} { return c.done }

func (c *stagedCanceledContext) Err() error {
	if c.calls.Add(1) == 1 {
		return nil
	}
	return context.Canceled
}

func (h *redisCommandFault) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *redisCommandFault) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		h.mu.Lock()
		if cmd.Name() == h.command {
			h.calls++
			if h.calls == h.failAt {
				h.mu.Unlock()
				if h.stop {
					return nil
				}
				return h.err
			}
		}
		h.mu.Unlock()
		return next(ctx, cmd)
	}
}

func (h *redisCommandFault) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func TestRedisControllerExecutionBoundaries(t *testing.T) {
	t.Parallel()

	worker, _ := newFaultControlWorker(t)
	command := faultControlCommand(management.CommandDelete, management.TargetDeadLetter, "1-0")

	_, err := (&Worker{}).Execute(t.Context(), command)
	assert.ErrorIs(t, err, management.ErrUnsupportedCapability)
	_, err = worker.Execute(t.Context(), management.Command{})
	assert.Error(t, err)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = worker.Execute(canceled, command)
	assert.ErrorIs(t, err, context.Canceled)

	waiting := &redisControlEntry{command: command, done: make(chan struct{})}
	worker.controlEntries = map[string]*redisControlEntry{command.IdempotencyKey: waiting}
	closed := make(chan struct{})
	close(closed)
	waitContext := &stagedCanceledContext{Context: t.Context(), done: closed}
	_, err = worker.Execute(waitContext, command)
	assert.ErrorIs(t, err, context.Canceled)
	waiting.result = management.CommandResult{Status: management.CommandRejected}
	close(waiting.done)
	result, err := worker.Execute(t.Context(), command)
	require.NoError(t, err)
	assert.Equal(t, waiting.result, result)

	unsupported := command
	unsupported.Action = management.CommandAction("unknown")
	result = worker.executeRedisCommand(t.Context(), unsupported)
	assert.Equal(t, management.CommandUnsupported, result.Status)
	replay := faultControlCommand(
		management.CommandReplay, management.TargetDeadLetter, "1-0",
	)
	replay.Replay = &management.ReplayOptions{
		Destination: "archive", IdempotencyPolicy: management.ReplayRejectDuplicate,
	}
	worker.opts.replayDestinations = nil
	result = worker.replayRedisRecord(t.Context(), replay)
	assert.Equal(t, management.CommandUnsupported, result.Status)
}

func TestRedisReplayRejectsMissingMalformedAndCapacityRecords(t *testing.T) {
	t.Parallel()

	worker, _ := newFaultControlWorker(t)
	command := faultControlCommand(
		management.CommandReplay, management.TargetDeadLetter, "9999999999999-0",
	)
	command.Replay = &management.ReplayOptions{
		Destination: "archive", IdempotencyPolicy: management.ReplayRejectDuplicate,
	}
	result := worker.replayRedisRecord(t.Context(), command)
	assert.Equal(t, management.CommandRejected, result.Status)

	malformedID, err := worker.rdb.XAdd(t.Context(), &redis.XAddArgs{
		Stream: "dead", Values: map[string]any{"unexpected": "value"},
	}).Result()
	require.NoError(t, err)
	command.Target.Name = malformedID
	result = worker.replayRedisRecord(t.Context(), command)
	assert.Equal(t, management.CommandFailed, result.Status)

	lineageID, err := worker.rdb.XAdd(t.Context(), &redis.XAddArgs{
		Stream: "dead", Values: map[string]any{
			streamBodyField: "body", replayGenerationField: "invalid",
		},
	}).Result()
	require.NoError(t, err)
	command.Target.Name = lineageID
	result = worker.replayRedisRecord(t.Context(), command)
	assert.Equal(t, management.CommandFailed, result.Status)

	worker, recordID := newFaultControlWorker(t)
	values := make(map[string]any, redisReplayRegistryCapacity)
	for index := range redisReplayRegistryCapacity {
		values[time.Unix(int64(index), 0).String()] = "replay"
	}
	require.NoError(t, worker.rdb.HSet(
		t.Context(), "archive:queue:replay-index", values,
	).Err())
	command = faultControlCommand(management.CommandReplay, management.TargetDeadLetter, recordID)
	command.Replay = &management.ReplayOptions{
		Destination: "archive", IdempotencyPolicy: management.ReplayRejectDuplicate,
	}
	result = worker.replayRedisRecord(t.Context(), command)
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "replay_registry_capacity", result.FailureCode)
}

func TestRedisReplayReconcilesConcurrentDuplicateRegistration(t *testing.T) {
	t.Parallel()

	for name, failDelete := range map[string]bool{
		"delete appended duplicate": false,
		"unknown duplicate delete":  true,
	} {
		t.Run(name, func(t *testing.T) {
			worker, recordID := newFaultControlWorker(t)
			addRedisStop(t, worker, "hsetnx", 1)
			if failDelete {
				addRedisFault(t, worker, "xdel", 1)
			}
			command := faultControlCommand(
				management.CommandReplay, management.TargetDeadLetter, recordID,
			)
			command.Replay = &management.ReplayOptions{
				Destination: "archive", IdempotencyPolicy: management.ReplayRejectDuplicate,
			}
			result := worker.replayRedisRecord(t.Context(), command)
			if failDelete {
				assert.Equal(t, management.CommandPartial, result.Status)
				assert.Equal(t, "replay_duplicate_unknown", result.FailureCode)
			} else {
				assert.Equal(t, management.CommandRejected, result.Status)
				assert.Equal(t, "replay_duplicate", result.FailureCode)
			}
		})
	}
}

func TestRedisReplayReportsEveryDurableBoundary(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		prepare func(*testing.T, *Worker, string)
		command string
		failAt  int
		want    management.CommandResultStatus
		code    string
	}{
		"record read":        {command: "xrange", failAt: 1, want: management.CommandUnknown},
		"registry read":      {command: "hget", failAt: 1, want: management.CommandUnknown},
		"registry size":      {command: "hlen", failAt: 1, want: management.CommandUnknown},
		"destination append": {command: "xadd", failAt: 1, want: management.CommandFailed, code: "enqueue_failed"},
		"reject registry":    {command: "hsetnx", failAt: 1, want: management.CommandPartial, code: "replay_registry_unknown"},
		"replace registry": {
			prepare: preparePriorReplay, command: "hset", failAt: 1,
			want: management.CommandPartial, code: "replay_registry_unknown",
		},
		"replace delete": {
			prepare: preparePriorReplay, command: "xdel", failAt: 1,
			want: management.CommandPartial, code: "replay_replace_unknown",
		},
	} {
		t.Run(name, func(t *testing.T) {
			worker, recordID := newFaultControlWorker(t)
			command := faultControlCommand(
				management.CommandReplay, management.TargetDeadLetter, recordID,
			)
			command.Replay = &management.ReplayOptions{
				Destination: "archive", IdempotencyPolicy: management.ReplayRejectDuplicate,
			}
			if test.prepare != nil {
				test.prepare(t, worker, recordID)
				command.Replay.IdempotencyPolicy = management.ReplayReplaceDuplicate
			}
			addRedisFault(t, worker, test.command, test.failAt)
			result := worker.replayRedisRecord(t.Context(), command)
			assert.Equal(t, test.want, result.Status)
			assert.Equal(t, test.code, result.FailureCode)
		})
	}
}

func TestRedisRetryReportsEveryDurableBoundary(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		command string
		failAt  int
		want    management.CommandResultStatus
		code    string
	}{
		"record read":        {command: "xrange", failAt: 1, want: management.CommandUnknown},
		"destination append": {command: "xadd", failAt: 1, want: management.CommandFailed, code: "enqueue_failed"},
		"record delete":      {command: "xdel", failAt: 1, want: management.CommandPartial, code: "record_delete_unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			worker, recordID := newFaultControlWorker(t)
			addRedisFault(t, worker, test.command, test.failAt)
			result := worker.retryRedisRecord(t.Context(), faultControlCommand(
				management.CommandRetry, management.TargetDeadLetter, recordID,
			))
			assert.Equal(t, test.want, result.Status)
			assert.Equal(t, test.code, result.FailureCode)
		})
	}

	worker, _ := newFaultControlWorker(t)
	malformedID, err := worker.rdb.XAdd(t.Context(), &redis.XAddArgs{
		Stream: "dead", Values: map[string]any{"unexpected": "value"},
	}).Result()
	require.NoError(t, err)
	result := worker.retryRedisRecord(t.Context(), faultControlCommand(
		management.CommandRetry, management.TargetDeadLetter, malformedID,
	))
	assert.Equal(t, management.CommandFailed, result.Status)
	assert.Equal(t, "record_malformed", result.FailureCode)
	result = worker.retryRedisRecord(t.Context(), faultControlCommand(
		management.CommandRetry, management.TargetDeadLetter, "9999999999999-0",
	))
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "record_not_found", result.FailureCode)
}

func TestRedisDeletePurgeAndBulkFailuresRemainExplicit(t *testing.T) {
	t.Parallel()

	worker, recordID := newFaultControlWorker(t)
	addRedisFault(t, worker, "xdel", 1)
	result := worker.deleteRedisRecord(t.Context(), faultControlCommand(
		management.CommandDelete, management.TargetDeadLetter, recordID,
	))
	assert.Equal(t, management.CommandUnknown, result.Status)

	worker, _ = newFaultControlWorker(t)
	addRedisFault(t, worker, "del", 1)
	result = worker.purgeRedisRecords(t.Context(), faultControlCommand(
		management.CommandPurge, management.TargetDeadLetter, "dead",
	))
	assert.Equal(t, management.CommandUnknown, result.Status)

	worker, _ = newFaultControlWorker(t)
	addRedisFault(t, worker, "xrange", 1)
	bulk := faultControlCommand(management.CommandBulkRetry, management.TargetDeadLetter, "dead")
	bulk.Selection = &management.Selection{Limit: 1}
	result = worker.bulkRetryRedisRecords(t.Context(), bulk)
	assert.Equal(t, management.CommandFailed, result.Status)
	assert.Equal(t, "records_unavailable", result.FailureCode)

	worker, _ = newFaultControlWorker(t)
	require.NoError(t, worker.rdb.Del(t.Context(), "dead").Err())
	_, err := worker.rdb.XAdd(t.Context(), &redis.XAddArgs{
		Stream: "dead", Values: map[string]any{"unexpected": "value"},
	}).Result()
	require.NoError(t, err)
	bulk.Selection = &management.Selection{Limit: 1}
	result = worker.bulkRetryRedisRecords(t.Context(), bulk)
	assert.Equal(t, management.CommandFailed, result.Status)
	assert.Equal(t, "record_malformed", result.FailureCode)

	worker, _ = newFaultControlWorker(t)
	_, err = worker.rdb.XAdd(t.Context(), &redis.XAddArgs{
		Stream: "dead", Values: map[string]any{"unexpected": "value"},
	}).Result()
	require.NoError(t, err)
	bulk.Selection = &management.Selection{Limit: 2}
	result = worker.bulkRetryRedisRecords(t.Context(), bulk)
	assert.Equal(t, management.CommandPartial, result.Status)
	assert.Equal(t, "bulk_retry_partial", result.FailureCode)
}

func TestRedisActiveFailureRetryReportsPendingAndAckUncertainty(t *testing.T) {
	t.Parallel()

	for name, command := range map[string]string{
		"pending read": "xpending",
		"source ack":   "xack",
	} {
		t.Run(name, func(t *testing.T) {
			worker, recordID := newActiveFailureControlWorker(t)
			if command == "xack" {
				addRedisStop(t, worker, command, 1)
			} else {
				addRedisFault(t, worker, command, 1)
			}
			result := worker.retryRedisRecord(t.Context(), faultControlCommand(
				management.CommandRetry, management.TargetFailure, recordID,
			))
			if command == "xpending" {
				assert.Equal(t, management.CommandUnknown, result.Status)
			} else {
				assert.Equal(t, management.CommandPartial, result.Status)
				assert.Equal(t, "source_ack_unknown", result.FailureCode)
			}
		})
	}
}

func TestRedisReplayLineageRejectsMalformedAndExhaustedRecords(t *testing.T) {
	t.Parallel()

	message := redis.XMessage{ID: "1-0", Values: map[string]any{
		replayGenerationField: "invalid",
	}}
	_, _, _, ok := redisNextReplayLineage(message)
	assert.False(t, ok)
	message.Values = map[string]any{
		replayOriginalDeadLetterField: "original", replayPriorDeadLetterField: "prior",
		replayGenerationField: "4294967295",
	}
	_, _, _, ok = redisNextReplayLineage(message)
	assert.False(t, ok)
	message.Values[replayGenerationField] = "1"
	original, prior, generation, ok := redisNextReplayLineage(message)
	assert.True(t, ok)
	assert.Equal(t, "original", original)
	assert.Equal(t, message.ID, prior)
	assert.Equal(t, uint32(2), generation)

	worker, _ := newFaultControlWorker(t)
	payload := job.NewMessage(rawMessage("payload"))
	malformedID, err := worker.rdb.XAdd(t.Context(), &redis.XAddArgs{
		Stream: "dead", Values: map[string]any{
			streamBodyField: payload.Bytes(), originalIDField: "1000-0",
			deliveryAttemptsField: "1", envelopeVersionField: "1",
			classificationField: string(management.ClassificationPermanent),
			failureCodeField:    "invalid_order", sourceStreamField: "jobs",
			consumerGroupField: "workers", replayGenerationField: "invalid",
		},
	}).Result()
	require.NoError(t, err)
	command := faultControlCommand(
		management.CommandRetry, management.TargetDeadLetter, malformedID,
	)
	result := worker.retryRedisRecord(t.Context(), command)
	assert.Equal(t, management.CommandFailed, result.Status)
	assert.Equal(t, "record_malformed", result.FailureCode)
}

func newFaultControlWorker(t *testing.T) (*Worker, string) {
	t.Helper()
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithBlockTime(time.Millisecond), WithRequestTimeout(time.Second),
		WithFailureStream("failures"), WithDeadLetter("dead", 5),
		WithReplayDestinations("archive"), WithManagementStatus(management.StatusMetadata{
			ID: "worker-1", Version: "v1", Concurrency: 1,
			Protocol: management.ProtocolVersion{Major: 1},
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	payload := job.NewMessage(rawMessage("payload"))
	require.NoError(t, worker.appendRecord(
		t.Context(), "dead", "1000-0", payload.Bytes(), 1,
		streamqueue.FailureMetadata{
			Classification: management.ClassificationPermanent, Code: "invalid_order",
		},
	))
	records, err := worker.rdb.XRangeN(t.Context(), "dead", "-", "+", 1).Result()
	require.NoError(t, err)
	require.Len(t, records, 1)
	return worker, records[0].ID
}

func faultControlCommand(
	action management.CommandAction, kind management.TargetKind, name string,
) management.Command {
	now := time.Now().UTC()
	return management.Command{
		ID: "command", IdempotencyKey: "key", Actor: "operator", Reason: "test fault",
		Protocol: management.ProtocolVersion{Major: 1}, Action: action,
		Target:      management.Target{Kind: kind, Name: name},
		RequestedAt: now, Deadline: now.Add(time.Minute), Confirmed: action == management.CommandReplay,
	}
}

func addRedisFault(t *testing.T, worker *Worker, command string, failAt int) {
	t.Helper()
	client, ok := worker.rdb.(*redis.Client)
	require.True(t, ok)
	client.AddHook(&redisCommandFault{
		command: command, failAt: failAt, err: errors.New("injected Redis fault"),
	})
}

func addRedisStop(t *testing.T, worker *Worker, command string, failAt int) {
	t.Helper()
	client, ok := worker.rdb.(*redis.Client)
	require.True(t, ok)
	client.AddHook(&redisCommandFault{command: command, failAt: failAt, stop: true})
}

func newActiveFailureControlWorker(t *testing.T) (*Worker, string) {
	t.Helper()
	worker, _ := newFaultControlWorker(t)
	queued := job.NewMessage(rawMessage("active"))
	require.NoError(t, worker.Queue(&queued))
	require.NoError(t, worker.rdb.XGroupCreate(
		t.Context(), "jobs", "workers", "0",
	).Err())
	streams, err := worker.rdb.XReadGroup(t.Context(), &redis.XReadGroupArgs{
		Group: "workers", Consumer: "worker-1", Streams: []string{"jobs", ">"}, Count: 1,
	}).Result()
	require.NoError(t, err)
	require.Len(t, streams, 1)
	require.Len(t, streams[0].Messages, 1)
	source := streams[0].Messages[0]
	require.NoError(t, worker.appendRecord(
		t.Context(), "failures", source.ID, queued.Bytes(), 1,
		streamqueue.FailureMetadata{
			Classification: management.ClassificationRetryable, Code: "handler_failed",
		},
	))
	records, err := worker.rdb.XRevRangeN(t.Context(), "failures", "+", "-", 1).Result()
	require.NoError(t, err)
	return worker, records[0].ID
}

func preparePriorReplay(t *testing.T, worker *Worker, recordID string) {
	t.Helper()
	command := faultControlCommand(management.CommandReplay, management.TargetDeadLetter, recordID)
	command.Replay = &management.ReplayOptions{
		Destination: "archive", IdempotencyPolicy: management.ReplayRejectDuplicate,
	}
	result := worker.replayRedisRecord(t.Context(), command)
	require.Equal(t, management.CommandAcknowledged, result.Status)
}

var _ redis.Hook = (*redisCommandFault)(nil)
