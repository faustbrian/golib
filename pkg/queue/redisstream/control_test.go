package redisdb

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControllerRetriesDeadLetterIdempotentlyAfterDurableEnqueue(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	protocol := management.ProtocolVersion{Major: 1}
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithConsumer("worker-1"), WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second), WithDeadLetter("jobs-dead", 5),
		WithFailureStream("jobs-failures"), WithManagementStatus(management.StatusMetadata{
			ID: "worker-1", Version: "v1", Concurrency: 1, Protocol: protocol,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	queued := job.NewMessage(rawMessage("permanent"))
	require.NoError(t, worker.Queue(&queued))
	delivery, err := worker.Request()
	require.NoError(t, err)
	require.NoError(t, delivery.(*job.Message).NackFailure(management.NewFailure(
		management.ClassificationPermanent, "invalid_order", errors.New("invalid order"),
	)))
	page, err := worker.ListDeadLetters(context.Background(), management.PageRequest{
		Limit: 1, Sort: management.SortOccurredAt, Direction: management.SortAscending,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	now := time.Now().UTC()
	command := management.Command{
		ID: "command-1", IdempotencyKey: "retry-1", Actor: "operator",
		Reason: "retry corrected order", Protocol: protocol,
		Action:      management.CommandRetry,
		Target:      management.Target{Kind: management.TargetDeadLetter, Name: page.Items[0].ID},
		RequestedAt: now, Deadline: now.Add(time.Minute),
	}
	result, err := worker.Execute(context.Background(), command)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	_, err = worker.Inspect(context.Background(), management.InspectRequest{
		Kind: management.RecordDeadLetter, ID: page.Items[0].ID,
		Visibility: management.PayloadHidden,
	})
	assert.ErrorIs(t, err, management.ErrRecordNotFound)
	length, err := worker.rdb.XLen(context.Background(), "jobs").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(2), length)
	retried, err := worker.rdb.XRevRangeN(context.Background(), "jobs", "+", "-", 1).Result()
	require.NoError(t, err)
	require.Len(t, retried, 1)
	assert.Equal(t, page.Items[0].ID, retried[0].Values[replayOriginalDeadLetterField])
	assert.Equal(t, page.Items[0].ID, retried[0].Values[replayPriorDeadLetterField])
	assert.Equal(t, "1", retried[0].Values[replayGenerationField])

	repeated, err := worker.Execute(context.Background(), command)
	require.NoError(t, err)
	assert.Equal(t, result, repeated)
	length, err = worker.rdb.XLen(context.Background(), "jobs").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(2), length)
}

func TestControllerBulkRetryDeleteAndPurgeStayBounded(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	protocol := management.ProtocolVersion{Major: 1}
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithFailureStream("failures"), WithDeadLetter("dead", 5),
		WithManagementStatus(management.StatusMetadata{
			ID: "worker-1", Version: "v1", Concurrency: 1, Protocol: protocol,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	payload := job.NewMessage(rawMessage("retry"))
	appendDeadLetter := func(originalID string) string {
		t.Helper()
		require.NoError(t, worker.appendRecord(
			context.Background(), "dead", originalID, payload.Bytes(), 1,
			streamqueue.FailureMetadata{
				Classification: management.ClassificationPermanent, Code: "invalid_order",
			},
		))
		records, rangeErr := worker.rdb.XRevRangeN(context.Background(), "dead", "+", "-", 1).Result()
		require.NoError(t, rangeErr)
		require.Len(t, records, 1)
		return records[0].ID
	}
	_ = appendDeadLetter("1000-0")
	_ = appendDeadLetter("2000-0")
	now := time.Now().UTC()
	bulk := management.Command{
		ID: "bulk-1", IdempotencyKey: "bulk-key", Actor: "operator",
		Reason: "retry corrected records", Protocol: protocol,
		Action:      management.CommandBulkRetry,
		Target:      management.Target{Kind: management.TargetDeadLetter, Name: "dead"},
		RequestedAt: now, Deadline: now.Add(time.Minute), Confirmed: true,
		Selection: &management.Selection{Limit: 2},
	}
	result, err := worker.Execute(context.Background(), bulk)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	length, err := worker.rdb.XLen(context.Background(), "jobs").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(2), length)
	deadLength, err := worker.rdb.XLen(context.Background(), "dead").Result()
	require.NoError(t, err)
	assert.Zero(t, deadLength)

	deleteID := appendDeadLetter("3000-0")
	deleteCommand := management.Command{
		ID: "delete-1", IdempotencyKey: "delete-key", Actor: "operator",
		Reason: "delete invalid record", Protocol: protocol,
		Action:      management.CommandDelete,
		Target:      management.Target{Kind: management.TargetDeadLetter, Name: deleteID},
		RequestedAt: now, Deadline: now.Add(time.Minute),
	}
	result, err = worker.Execute(context.Background(), deleteCommand)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)

	_ = appendDeadLetter("4000-0")
	_ = appendDeadLetter("5000-0")
	purge := management.Command{
		ID: "purge-1", IdempotencyKey: "purge-key", Actor: "operator",
		Reason: "purge confirmed records", Protocol: protocol,
		Action:      management.CommandPurge,
		Target:      management.Target{Kind: management.TargetDeadLetter, Name: "dead"},
		RequestedAt: now, Deadline: now.Add(time.Minute), Confirmed: true,
	}
	result, err = worker.Execute(context.Background(), purge)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	deadLength, err = worker.rdb.XLen(context.Background(), "dead").Result()
	require.NoError(t, err)
	assert.Zero(t, deadLength)
}

func TestControllerRejectsStaleFailureAndUnsafeCommands(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	protocol := management.ProtocolVersion{Major: 1}
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithFailureStream("failures"), WithDeadLetter("dead", 5),
		WithManagementStatus(management.StatusMetadata{
			ID: "worker-1", Version: "v1", Concurrency: 1, Protocol: protocol,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	payload := job.NewMessage(rawMessage("retry"))
	require.NoError(t, worker.appendRecord(
		context.Background(), "failures", "1000-0", payload.Bytes(), 1,
		streamqueue.FailureMetadata{Classification: management.ClassificationRetryable},
	))
	require.NoError(t, worker.rdb.XGroupCreateMkStream(
		context.Background(), "jobs", "workers", "0",
	).Err())
	records, err := worker.rdb.XRangeN(context.Background(), "failures", "-", "+", 1).Result()
	require.NoError(t, err)
	require.Len(t, records, 1)
	now := time.Now().UTC()
	command := management.Command{
		ID: "retry-stale", IdempotencyKey: "retry-stale-key", Actor: "operator",
		Reason: "retry stale failure", Protocol: protocol, Action: management.CommandRetry,
		Target:      management.Target{Kind: management.TargetFailure, Name: records[0].ID},
		RequestedAt: now, Deadline: now.Add(time.Minute),
	}
	result, err := worker.Execute(context.Background(), command)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "source_record_stale", result.FailureCode)

	queuePurge := command
	queuePurge.ID = "queue-purge"
	queuePurge.IdempotencyKey = "queue-purge-key"
	queuePurge.Action = management.CommandPurge
	queuePurge.Target = management.Target{Kind: management.TargetQueue, Name: "jobs"}
	queuePurge.Confirmed = true
	result, err = worker.Execute(context.Background(), queuePurge)
	require.NoError(t, err)
	assert.Equal(t, management.CommandUnsupported, result.Status)

	wrongProtocol := command
	wrongProtocol.ID = "wrong-protocol"
	wrongProtocol.IdempotencyKey = "wrong-protocol-key"
	wrongProtocol.Protocol = management.ProtocolVersion{Major: 2}
	result, err = worker.Execute(context.Background(), wrongProtocol)
	require.NoError(t, err)
	assert.Equal(t, management.CommandUnsupported, result.Status)
	assert.Equal(t, "protocol_mismatch", result.FailureCode)

	expired := command
	expired.ID = "expired"
	expired.IdempotencyKey = "expired-key"
	expired.RequestedAt = now.Add(-2 * time.Minute)
	expired.Deadline = now.Add(-time.Minute)
	result, err = worker.Execute(context.Background(), expired)
	require.NoError(t, err)
	assert.Equal(t, management.CommandTimedOut, result.Status)
}

func TestControllerSerializesConcurrentIdempotentRetry(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	protocol := management.ProtocolVersion{Major: 1}
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithFailureStream("failures"), WithDeadLetter("dead", 5),
		WithManagementStatus(management.StatusMetadata{
			ID: "worker-1", Version: "v1", Concurrency: 1, Protocol: protocol,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	payload := job.NewMessage(rawMessage("retry"))
	require.NoError(t, worker.appendRecord(
		context.Background(), "dead", "1000-0", payload.Bytes(), 1,
		streamqueue.FailureMetadata{Classification: management.ClassificationPermanent},
	))
	records, err := worker.rdb.XRangeN(context.Background(), "dead", "-", "+", 1).Result()
	require.NoError(t, err)
	require.Len(t, records, 1)
	now := time.Now().UTC()
	command := management.Command{
		ID: "retry-once", IdempotencyKey: "retry-once-key", Actor: "operator",
		Reason: "retry once", Protocol: protocol, Action: management.CommandRetry,
		Target:      management.Target{Kind: management.TargetDeadLetter, Name: records[0].ID},
		RequestedAt: now, Deadline: now.Add(time.Minute),
	}

	const callers = 16
	results := make(chan management.CommandResult, callers)
	errorsFound := make(chan error, callers)
	var start sync.WaitGroup
	start.Add(1)
	var callersDone sync.WaitGroup
	callersDone.Add(callers)
	for range callers {
		go func() {
			defer callersDone.Done()
			start.Wait()
			result, executeErr := worker.Execute(context.Background(), command)
			results <- result
			errorsFound <- executeErr
		}()
	}
	start.Done()
	callersDone.Wait()
	close(results)
	close(errorsFound)
	for executeErr := range errorsFound {
		require.NoError(t, executeErr)
	}
	for result := range results {
		assert.Equal(t, management.CommandAcknowledged, result.Status)
	}
	length, err := worker.rdb.XLen(context.Background(), "jobs").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), length)
}

func TestControllerFailsClosedOnIdempotencyConflictAndCapacity(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	protocol := management.ProtocolVersion{Major: 1}
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithFailureStream("failures"), WithDeadLetter("dead", 5),
		WithManagementStatus(management.StatusMetadata{
			ID: "worker-1", Version: "v1", Concurrency: 1, Protocol: protocol,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	worker.controlCapacity = 1
	now := time.Now().UTC()
	command := management.Command{
		ID: "missing", IdempotencyKey: "shared-key", Actor: "operator",
		Reason: "delete missing", Protocol: protocol, Action: management.CommandDelete,
		Target:      management.Target{Kind: management.TargetDeadLetter, Name: "1-0"},
		RequestedAt: now, Deadline: now.Add(time.Minute),
	}
	result, err := worker.Execute(context.Background(), command)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "record_not_found", result.FailureCode)

	conflict := command
	conflict.ID = "conflict"
	result, err = worker.Execute(context.Background(), conflict)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "idempotency_conflict", result.FailureCode)

	capacity := command
	capacity.ID = "capacity"
	capacity.IdempotencyKey = "second-key"
	result, err = worker.Execute(context.Background(), capacity)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "idempotency_capacity", result.FailureCode)
}

func TestControllerReplaysWithDurableDuplicatePoliciesAndLineage(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	protocol := management.ProtocolVersion{Major: 1}
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("jobs"), WithGroup("workers"),
		WithFailureStream("failures"), WithDeadLetter("dead", 5),
		WithReplayDestinations("archive"),
		WithManagementStatus(management.StatusMetadata{
			ID: "worker-1", Version: "v1", Concurrency: 1, Protocol: protocol,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	payload := job.NewMessage(rawMessage("replay"))
	require.NoError(t, worker.appendRecord(
		context.Background(), "dead", "1000-0", payload.Bytes(), 2,
		streamqueue.FailureMetadata{Classification: management.ClassificationPermanent},
	))
	records, err := worker.rdb.XRangeN(context.Background(), "dead", "-", "+", 1).Result()
	require.NoError(t, err)
	require.Len(t, records, 1)
	now := time.Now().UTC()
	replay := management.Command{
		ID: "replay-1", IdempotencyKey: "replay-key-1", Actor: "operator",
		Reason: "replay corrected record", Protocol: protocol,
		Action:      management.CommandReplay,
		Target:      management.Target{Kind: management.TargetDeadLetter, Name: records[0].ID},
		RequestedAt: now, Deadline: now.Add(time.Minute), Confirmed: true,
		Replay: &management.ReplayOptions{
			Destination: "archive", IdempotencyPolicy: management.ReplayRejectDuplicate,
		},
	}
	result, err := worker.Execute(context.Background(), replay)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	archived, err := worker.rdb.XRangeN(context.Background(), "archive", "-", "+", 10).Result()
	require.NoError(t, err)
	require.Len(t, archived, 1)
	assert.Equal(t, records[0].ID, archived[0].Values[replayPriorDeadLetterField])
	assert.Equal(t, records[0].ID, archived[0].Values[replayOriginalDeadLetterField])
	assert.Equal(t, "1", archived[0].Values[replayGenerationField])
	deadLength, err := worker.rdb.XLen(context.Background(), "dead").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), deadLength)

	duplicate := replay
	duplicate.ID = "replay-2"
	duplicate.IdempotencyKey = "replay-key-2"
	result, err = worker.Execute(context.Background(), duplicate)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "replay_duplicate", result.FailureCode)
	archived, err = worker.rdb.XRangeN(context.Background(), "archive", "-", "+", 10).Result()
	require.NoError(t, err)
	require.Len(t, archived, 1)
	firstReplayID := archived[0].ID

	replace := replay
	replace.ID = "replay-3"
	replace.IdempotencyKey = "replay-key-3"
	replace.Replay.IdempotencyPolicy = management.ReplayReplaceDuplicate
	result, err = worker.Execute(context.Background(), replace)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	archived, err = worker.rdb.XRangeN(context.Background(), "archive", "-", "+", 10).Result()
	require.NoError(t, err)
	require.Len(t, archived, 1)
	assert.NotEqual(t, firstReplayID, archived[0].ID)

	replayWorker, err := NewWorkerE(
		WithAddr(server.Addr()), WithStreamName("archive"), WithGroup("archive-workers"),
		WithConsumer("archive-worker-1"), WithBlockTime(time.Millisecond),
		WithRequestTimeout(time.Second), WithFailureStream("archive-failures"),
		WithDeadLetter("archive-dead", 5), WithManagementStatus(management.StatusMetadata{
			ID: "archive-worker-1", Version: "v1", Concurrency: 1, Protocol: protocol,
		}),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = replayWorker.Shutdown() })
	replayedDelivery, err := replayWorker.Request()
	require.NoError(t, err)
	require.NoError(t, replayedDelivery.(*job.Message).NackFailure(management.NewFailure(
		management.ClassificationPermanent, "still_invalid", errors.New("still invalid"),
	)))
	replayedDead, err := replayWorker.ListDeadLetters(context.Background(), management.PageRequest{
		Limit: 1, Sort: management.SortOccurredAt, Direction: management.SortAscending,
	})
	require.NoError(t, err)
	require.Len(t, replayedDead.Items, 1)
	assert.Equal(t, records[0].ID, replayedDead.Items[0].OriginalDeadLetterID)
	assert.Equal(t, records[0].ID, replayedDead.Items[0].PriorDeadLetterID)
	assert.Equal(t, uint32(1), replayedDead.Items[0].ReplayGeneration)

	unapproved := replay
	unapproved.ID = "replay-4"
	unapproved.IdempotencyKey = "replay-key-4"
	unapproved.Replay.Destination = "other"
	result, err = worker.Execute(context.Background(), unapproved)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "destination_not_allowed", result.FailureCode)
}
