package valkeystream

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeControllerRetriesDeletesBulkRetriesAndPurges(t *testing.T) {
	transport := &mutationTransportStub{recordTransportStub: &recordTransportStub{records: []nativeRecord{
		{ID: "1-0", OriginalID: "source-1", Body: []byte("one"), Attempts: 1, OccurredAt: time.Unix(1, 0)},
		{ID: "2-0", OriginalID: "source-2", Body: []byte("two"), Attempts: 2, OccurredAt: time.Unix(2, 0)},
	}}, deleteFound: true}
	worker := controlledWorker(transport)

	retry := nativeCommand("retry-1", management.CommandRetry, management.TargetFailure, "1-0")
	result, err := worker.Execute(t.Context(), retry)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	assert.Equal(t, []string{"critical"}, transport.destinations)
	assert.Empty(t, transport.deletions)

	deleteCommand := nativeCommand("delete-1", management.CommandDelete, management.TargetDeadLetter, "2-0")
	result, err = worker.Execute(t.Context(), deleteCommand)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	assert.Equal(t, "dead:2-0", transport.deletions[0])

	bulk := nativeCommand("bulk-1", management.CommandBulkRetry, management.TargetFailure, "failed")
	bulk.Confirmed = true
	bulk.Selection = &management.Selection{Limit: 1}
	result, err = worker.Execute(t.Context(), bulk)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	assert.Len(t, transport.destinations, 2)

	purge := nativeCommand("purge-1", management.CommandPurge, management.TargetDeadLetter, "dead")
	purge.Confirmed = true
	result, err = worker.Execute(t.Context(), purge)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	assert.Equal(t, []string{"dead"}, transport.purges)
}

func TestNativeControllerFailsClosedAndReportsMutationUncertainty(t *testing.T) {
	worker := &Worker{}
	_, err := worker.Execute(context.Background(), management.Command{})
	assert.ErrorIs(t, err, ErrManagementControlDisabled)
	metadata := &management.StatusMetadata{
		ID: "worker-1", Version: "v1", Concurrency: 1,
		Protocol: management.ProtocolVersion{Major: 1},
	}
	worker = &Worker{opts: options{management: metadata}, transport: &recordTransportStub{}}
	_, err = worker.Execute(context.Background(), nativeCommand(
		"disabled", management.CommandRetry, management.TargetFailure, "1-0",
	))
	assert.ErrorIs(t, err, ErrManagementControlDisabled)

	transport := &mutationTransportStub{recordTransportStub: &recordTransportStub{records: []nativeRecord{{
		ID: "1-0", OriginalID: "source", Body: []byte("one"), Attempts: 1,
		OccurredAt: time.Unix(1, 0),
	}}}, deleteFound: true}
	worker = controlledWorker(transport)
	_, err = worker.Execute(context.Background(), management.Command{})
	assert.Error(t, err)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = worker.Execute(cancelled, nativeCommand(
		"cancelled", management.CommandRetry, management.TargetFailure, "1-0",
	))
	assert.ErrorIs(t, err, context.Canceled)

	protocol := nativeCommand("protocol", management.CommandRetry, management.TargetFailure, "1-0")
	protocol.Protocol.Minor = 1
	result, err := worker.Execute(context.Background(), protocol)
	require.NoError(t, err)
	assert.Equal(t, management.CommandUnsupported, result.Status)
	assert.Equal(t, "protocol_mismatch", result.FailureCode)
	pause := nativeCommand("pause", management.CommandPause, management.TargetQueue, "critical")
	result, err = worker.Execute(context.Background(), pause)
	require.NoError(t, err)
	assert.Equal(t, management.CommandUnsupported, result.Status)

	expired := nativeCommand("expired", management.CommandRetry, management.TargetFailure, "1-0")
	expired.RequestedAt = time.Now().Add(-2 * time.Second)
	expired.Deadline = time.Now().Add(-time.Second)
	result, err = worker.Execute(context.Background(), expired)
	require.NoError(t, err)
	assert.Equal(t, management.CommandTimedOut, result.Status)

	notFound := nativeCommand("missing", management.CommandRetry, management.TargetFailure, "missing")
	result, err = worker.Execute(context.Background(), notFound)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "record_not_found", result.FailureCode)

	replay := nativeCommand("replay", management.CommandReplay, management.TargetFailure, "1-0")
	replay.Confirmed = true
	replay.Replay = &management.ReplayOptions{
		Destination: "other", IdempotencyPolicy: management.ReplayRejectDuplicate,
	}
	result, err = worker.Execute(context.Background(), replay)
	require.NoError(t, err)
	assert.Equal(t, management.CommandUnsupported, result.Status)

	queuePurge := nativeCommand("queue-purge", management.CommandPurge, management.TargetQueue, "critical")
	queuePurge.Confirmed = true
	result, err = worker.Execute(context.Background(), queuePurge)
	require.NoError(t, err)
	assert.Equal(t, management.CommandUnsupported, result.Status)

	transport.purgeErr = errors.New("purge unknown")
	purge := nativeCommand("purge-failure", management.CommandPurge, management.TargetFailure, "failed")
	purge.Confirmed = true
	result, err = worker.Execute(context.Background(), purge)
	require.NoError(t, err)
	assert.Equal(t, management.CommandUnknown, result.Status)
	transport.purgeErr = nil

	transport.err = errors.New("read failed")
	readFailure := nativeCommand("read-failure", management.CommandRetry, management.TargetFailure, "1-0")
	result, err = worker.Execute(context.Background(), readFailure)
	require.NoError(t, err)
	assert.Equal(t, management.CommandUnknown, result.Status)
	transport.err = nil

	transport.retryErr = errors.New("append unknown")
	appendFailure := nativeCommand("append-failure", management.CommandRetry, management.TargetFailure, "1-0")
	result, err = worker.Execute(context.Background(), appendFailure)
	require.NoError(t, err)
	assert.Equal(t, management.CommandUnknown, result.Status)
	transport.retryErr = nil
	transport.deleteFound = false
	staleDelete := nativeCommand("stale-delete", management.CommandDelete, management.TargetFailure, "1-0")
	result, err = worker.Execute(context.Background(), staleDelete)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	transport.deleteFound = true

	transport.deleteErr = errors.New("delete unknown")
	deleteFailure := nativeCommand("delete-failure", management.CommandDelete, management.TargetFailure, "1-0")
	result, err = worker.Execute(context.Background(), deleteFailure)
	require.NoError(t, err)
	assert.Equal(t, management.CommandUnknown, result.Status)
	transport.deleteErr = nil
	transport.retryOutcome = nativeRetryStale
	staleRetry := nativeCommand("stale-retry", management.CommandRetry, management.TargetFailure, "1-0")
	result, err = worker.Execute(context.Background(), staleRetry)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	transport.retryOutcome = nativeRetryMalformed
	malformed := nativeCommand("malformed", management.CommandRetry, management.TargetFailure, "1-0")
	result, err = worker.Execute(context.Background(), malformed)
	require.NoError(t, err)
	assert.Equal(t, management.CommandFailed, result.Status)
	transport.retryOutcome = nativeRetryOutcome("unexpected")
	unexpected := nativeCommand("unexpected", management.CommandRetry, management.TargetFailure, "1-0")
	result, err = worker.Execute(context.Background(), unexpected)
	require.NoError(t, err)
	assert.Equal(t, management.CommandUnknown, result.Status)
}

func TestNativeControllerBulkPartialOutcomesAndExistingCancellation(t *testing.T) {
	records := []nativeRecord{
		{ID: "1-0", Body: []byte("one"), Attempts: 1, OccurredAt: time.Unix(1, 0)},
		{ID: "2-0", Body: []byte("two"), Attempts: 1, OccurredAt: time.Unix(2, 0)},
	}
	transport := &mutationTransportStub{
		recordTransportStub: &recordTransportStub{records: records}, deleteFound: true,
	}
	worker := controlledWorker(transport)
	bulk := func(id string) management.Command {
		command := nativeCommand(id, management.CommandBulkRetry, management.TargetFailure, "failed")
		command.Confirmed = true
		command.Selection = &management.Selection{Limit: 2}
		return command
	}

	transport.retryAt = 1
	result, err := worker.Execute(context.Background(), bulk("enqueue-partial"))
	require.NoError(t, err)
	assert.Equal(t, management.CommandPartial, result.Status)
	transport.retryAt = 0
	transport.retryErr = errors.New("first enqueue unknown")
	result, err = worker.Execute(context.Background(), bulk("enqueue-unknown"))
	require.NoError(t, err)
	assert.Equal(t, management.CommandUnknown, result.Status)
	transport.retryErr = nil
	transport.retryOutcome = nativeRetryStale
	result, err = worker.Execute(context.Background(), bulk("delete-partial"))
	require.NoError(t, err)
	assert.Equal(t, management.CommandPartial, result.Status)
	transport.records = nil
	transport.retryOutcome = ""
	result, err = worker.Execute(context.Background(), bulk("empty"))
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	transport.err = errors.New("bulk read failed")
	result, err = worker.Execute(context.Background(), bulk("read-failed"))
	require.NoError(t, err)
	assert.Equal(t, management.CommandFailed, result.Status)
	transport.err = nil

	waiting := bulk("waiting")
	worker.controlEntries[waiting.IdempotencyKey] = &nativeControlEntry{
		command: waiting, done: make(chan struct{}),
	}
	waitContext := newWaitingContext()
	_, err = worker.Execute(waitContext, waiting)
	assert.ErrorIs(t, err, context.Canceled)

	defaultCapacity := controlledWorker(transport)
	defaultCapacity.controlCapacity = 0
	result, err = defaultCapacity.Execute(context.Background(), bulk("default-capacity"))
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	worker.now = nil
	assert.False(t, worker.controlNow().IsZero())
}

func TestNativeControllerIdempotencyAndCapacity(t *testing.T) {
	transport := &mutationTransportStub{recordTransportStub: &recordTransportStub{records: []nativeRecord{{
		ID: "1-0", OriginalID: "source", Body: []byte("one"), Attempts: 1,
		OccurredAt: time.Unix(1, 0),
	}}}, deleteFound: true}
	worker := controlledWorker(transport)
	worker.controlCapacity = 1
	command := nativeCommand("retry-1", management.CommandRetry, management.TargetFailure, "1-0")
	first, err := worker.Execute(context.Background(), command)
	require.NoError(t, err)
	second, err := worker.Execute(context.Background(), command)
	require.NoError(t, err)
	assert.Equal(t, first, second)
	assert.Len(t, transport.destinations, 1)

	conflict := command
	conflict.ID = "different"
	result, err := worker.Execute(context.Background(), conflict)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "idempotency_conflict", result.FailureCode)

	capacity := nativeCommand("capacity", management.CommandDelete, management.TargetFailure, "1-0")
	result, err = worker.Execute(context.Background(), capacity)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "idempotency_capacity", result.FailureCode)
}

func TestNativeControllerReplaysOnlyToConfiguredDestinations(t *testing.T) {
	transport := &mutationTransportStub{recordTransportStub: &recordTransportStub{records: []nativeRecord{{
		ID: "1-0", OriginalID: "source", Body: []byte("one"), Attempts: 1,
		OccurredAt: time.Unix(1, 0),
	}}}}
	worker := controlledWorker(transport)
	worker.opts.replayDestinations = map[string]struct{}{"archive": {}}

	replay := nativeCommand("replay-1", management.CommandReplay, management.TargetFailure, "1-0")
	replay.Confirmed = true
	replay.Replay = &management.ReplayOptions{
		Destination: "archive", IdempotencyPolicy: management.ReplayRejectDuplicate,
	}
	result, err := worker.Execute(context.Background(), replay)
	require.NoError(t, err)
	assert.Equal(t, management.CommandAcknowledged, result.Status)
	assert.Equal(t, []string{"archive"}, transport.replayDestinations)

	replay.ID = "replay-2"
	replay.IdempotencyKey = "replay-2"
	replay.Replay.Destination = "unapproved"
	result, err = worker.Execute(context.Background(), replay)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "destination_not_allowed", result.FailureCode)

	replay.ID = "replay-3"
	replay.IdempotencyKey = "replay-3"
	replay.Replay.Destination = "archive"
	transport.replayOutcome = nativeReplayDuplicate
	result, err = worker.Execute(context.Background(), replay)
	require.NoError(t, err)
	assert.Equal(t, management.CommandRejected, result.Status)
	assert.Equal(t, "replay_duplicate", result.FailureCode)

	for outcome, expected := range map[nativeReplayOutcome]management.CommandResultStatus{
		nativeReplayNotFound:              management.CommandRejected,
		nativeReplayMalformed:             management.CommandFailed,
		nativeReplayOutcome("unexpected"): management.CommandUnknown,
	} {
		replay.ID = "replay-" + string(outcome)
		replay.IdempotencyKey = replay.ID
		transport.replayOutcome = outcome
		result, err = worker.Execute(context.Background(), replay)
		require.NoError(t, err)
		assert.Equal(t, expected, result.Status)
	}
	transport.replayOutcome = ""
	transport.replayErr = errors.New("replay unknown")
	replay.ID = "replay-error"
	replay.IdempotencyKey = replay.ID
	result, err = worker.Execute(context.Background(), replay)
	require.NoError(t, err)
	assert.Equal(t, management.CommandUnknown, result.Status)
}

func controlledWorker(transport *mutationTransportStub) *Worker {
	return &Worker{opts: options{
		stream: "critical", failureStream: "failures", deadLetterStream: "dead",
		maxLength: 100, management: &management.StatusMetadata{
			ID: "worker-1", Version: "v1", Concurrency: 1,
			Protocol: management.ProtocolVersion{Major: 1},
		},
	}, transport: transport, controlCapacity: 16, now: time.Now}
}

func nativeCommand(
	id string, action management.CommandAction, kind management.TargetKind, name string,
) management.Command {
	now := time.Now().UTC()
	return management.Command{
		ID: id, IdempotencyKey: id, Actor: "operator", Reason: "incident",
		Protocol: management.ProtocolVersion{Major: 1}, Action: action,
		Target: management.Target{Kind: kind, Name: name}, RequestedAt: now,
		Deadline: now.Add(time.Minute),
	}
}

type mutationTransportStub struct {
	*recordTransportStub
	destinations       []string
	deletions          []string
	purges             []string
	deleteErr          error
	deleteFound        bool
	purgeErr           error
	retryErr           error
	retryOutcome       nativeRetryOutcome
	retryAt            int
	retryCalls         int
	replayDestinations []string
	replayOutcome      nativeReplayOutcome
	replayErr          error
}

func (s *mutationTransportStub) ReplayRecord(
	_ context.Context, _, _ string, destination string,
	_ management.ReplayPolicy,
) (nativeReplayOutcome, error) {
	s.replayDestinations = append(s.replayDestinations, destination)
	if s.replayErr != nil {
		return "", s.replayErr
	}
	if s.replayOutcome != "" {
		return s.replayOutcome, nil
	}
	return nativeReplayOK, nil
}

func (s *mutationTransportStub) RetryRecord(
	_ context.Context, _ string, id, destination, _, _ string, _ bool,
) (nativeRetryOutcome, error) {
	s.destinations = append(s.destinations, destination)
	s.retryCalls++
	if s.err != nil {
		return "", s.err
	}
	if s.retryAt > 0 && s.retryCalls > s.retryAt {
		return "", errors.New("later enqueue unknown")
	}
	if s.retryErr != nil {
		return "", s.retryErr
	}
	if s.retryOutcome != "" {
		return s.retryOutcome, nil
	}
	for _, record := range s.records {
		if record.ID == id {
			return nativeRetryOK, nil
		}
	}
	return nativeRetryNotFound, nil
}

func (s *mutationTransportStub) DeleteRecord(
	_ context.Context, stream, id string,
) (bool, error) {
	s.deletions = append(s.deletions, stream+":"+id)
	return s.deleteFound, s.deleteErr
}

func (s *mutationTransportStub) PurgeRecords(
	_ context.Context, stream string,
) error {
	s.purges = append(s.purges, stream)
	return s.purgeErr
}

type waitingContext struct {
	context.Context
	done  chan struct{}
	calls int
}

func newWaitingContext() *waitingContext {
	done := make(chan struct{})
	close(done)
	return &waitingContext{Context: context.Background(), done: done}
}

func (c *waitingContext) Done() <-chan struct{} { return c.done }

func (c *waitingContext) Err() error {
	c.calls++
	if c.calls == 1 {
		return nil
	}
	return context.Canceled
}
