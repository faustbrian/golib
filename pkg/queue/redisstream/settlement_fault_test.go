package redisdb

import (
	"context"
	"errors"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisMalformedSettlementKeepsSourceRecoverableAtEveryBoundary(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		command string
		stop    bool
		lineage bool
	}{
		"pending inspection": {command: "xpending"},
		"lineage decoding":   {lineage: true},
		"destination append": {command: "xadd"},
		"source ack error":   {command: "xack"},
		"source ack unknown": {command: "xack", stop: true},
	} {
		t.Run(name, func(t *testing.T) {
			worker, message := newPendingRedisMessage(t)
			if test.lineage {
				message.Values[replayGenerationField] = "invalid"
			} else if test.stop {
				addRedisStop(t, worker, test.command, 1)
			} else {
				addRedisFault(t, worker, test.command, 1)
			}
			err := worker.deadLetterMalformed(message, []byte("invalid"), "malformed_delivery")
			require.Error(t, err)
			resolution := management.ResolveFailure(err)
			if test.command == "xadd" {
				assert.Equal(t, management.FailureCodeDeadLetterDestinationUnavailable, resolution.Code)
			}
			if test.command == "xack" && test.stop {
				assert.Equal(t, management.FailureCodeLeaseLost, resolution.Code)
			}
			pending, pendingErr := worker.rdb.XPending(t.Context(), "jobs", "workers").Result()
			require.NoError(t, pendingErr)
			assert.Equal(t, int64(1), pending.Count)
		})
	}
}

func TestRedisHandlerSettlementReportsFailureAndDeadLetterBoundaries(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		command string
		failAt  int
		lineage bool
	}{
		"pending inspection": {command: "xpending", failAt: 1},
		"lineage decoding":   {lineage: true},
		"failure append":     {command: "xadd", failAt: 1},
		"dead-letter append": {command: "xadd", failAt: 2},
	} {
		t.Run(name, func(t *testing.T) {
			worker, message := newPendingRedisMessage(t)
			if test.lineage {
				message.Values[replayGenerationField] = "invalid"
			} else {
				addRedisFault(t, worker, test.command, test.failAt)
			}
			err := worker.settleHandlerFailure(
				message, []byte("payload"), management.NewFailure(
					management.ClassificationPermanent, "invalid", errors.New("invalid"),
				),
			)
			require.Error(t, err)
		})
	}
}

func TestRedisSettlementHelpersRejectMalformedState(t *testing.T) {
	t.Parallel()

	worker, message := newPendingRedisMessage(t)
	addRedisFault(t, worker, "xadd", 1)
	err := worker.appendRecordWithLineage(
		t.Context(), "dead", message.ID, []byte("payload"), 1,
		redisFailureMetadata(errors.New("temporary"), "handler_failed"),
		redisReplayLineage{original: "original", prior: "prior", generation: 1},
	)
	assert.Error(t, err)

	for _, values := range []map[string]any{
		{replayOriginalDeadLetterField: "original"},
		{
			replayOriginalDeadLetterField: "original", replayPriorDeadLetterField: "prior",
			replayGenerationField: "invalid",
		},
	} {
		_, err = redisLineageFromValues(values)
		assert.Error(t, err)
	}

	worker, _ = newFaultControlWorker(t)
	_, err = worker.pendingAttempts(t.Context(), "9999999999999-0")
	assert.Error(t, err)
	addRedisFault(t, worker, "xpending", 1)
	_, err = worker.pendingAttempts(t.Context(), "9999999999999-0")
	assert.Error(t, err)
}

func newPendingRedisMessage(t *testing.T) (*Worker, redis.XMessage) {
	t.Helper()
	worker, _ := newFaultControlWorker(t)
	queued := job.NewMessage(rawMessage("payload"))
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
	return worker, streams[0].Messages[0]
}

func TestRedisFailureMetadataUsesSafeClassifiedCode(t *testing.T) {
	t.Parallel()
	metadata := redisFailureMetadata(management.NewFailure(
		management.ClassificationPermanent, "invalid_order", errors.New("secret"),
	), "fallback")
	assert.Equal(t, "invalid_order", metadata.Code)
	assert.False(t, redisTerminalFailure(context.Canceled, 10, 1))
}

func TestRedisRequestReportsMalformedDeadLetterFailure(t *testing.T) {
	t.Parallel()

	for name, body := range map[string]any{
		"non-string body":  1,
		"invalid envelope": "not-json",
	} {
		t.Run(name, func(t *testing.T) {
			worker, message := newPendingRedisMessage(t)
			addRedisFault(t, worker, "xpending", 1)
			message.Values[streamBodyField] = body
			requestWorker := &Worker{
				rdb: worker.rdb, tasks: make(chan redis.XMessage, 1), opts: worker.opts,
			}
			requestWorker.startOnce.Do(func() {})
			requestWorker.tasks <- message
			task, err := requestWorker.Request()
			assert.Nil(t, task)
			assert.Error(t, err)
		})
	}
}

func TestRedisReclaimLoopReportsErrorsAndPreservesClaimedWorkOnStop(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		next    string
		claimed []redis.XMessage
		err     error
	}{
		"reclaim error": {err: errors.New("reclaim unavailable")},
		"stop with claimed work": {
			next: "2-0", claimed: []redis.XMessage{{ID: "1-0"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			stop := make(chan struct{})
			worker := &Worker{
				stop: stop, tasks: make(chan redis.XMessage),
				opts: newOptions(
					WithLogger(queue.NewEmptyLogger()),
					WithReclaim(time.Millisecond, time.Millisecond, 1),
				),
				autoClaim: func(
					context.Context, *redis.XAutoClaimArgs,
				) ([]redis.XMessage, string, error) {
					if len(test.claimed) > 0 {
						close(stop)
					}
					return test.claimed, test.next, test.err
				},
				readGroup: func(
					context.Context, *redis.XReadGroupArgs,
				) ([]redis.XStream, error) {
					close(stop)
					return nil, redis.Nil
				},
			}
			worker.fetchTask()
		})
	}
}

func TestRedisPendingAttemptsRejectsAbsentDelivery(t *testing.T) {
	t.Parallel()
	worker, _ := newFaultControlWorker(t)
	addRedisStop(t, worker, "xpending", 1)
	_, err := worker.pendingAttempts(t.Context(), "1-0")
	assert.Error(t, err)
}
