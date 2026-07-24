package redisdb

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/redis/go-redis/v9"
)

const (
	defaultRedisControlCapacity = 1_024
	redisReplayRegistryCapacity = 1_024
)

var (
	ErrManagementControlDisabled = errors.Join(
		errors.New("redisstream: management control disabled"),
		management.ErrUnsupportedCapability,
	)
)

var _ management.Controller = (*Worker)(nil)

type redisControlEntry struct {
	command management.Command
	result  management.CommandResult
	done    chan struct{}
	once    sync.Once
}

// Execute applies one bounded Redis Streams management command.
func (w *Worker) Execute(
	ctx context.Context,
	command management.Command,
) (management.CommandResult, error) {
	if w.opts.management == nil || w.rdb == nil {
		return management.CommandResult{}, ErrManagementControlDisabled
	}
	if err := command.Validate(); err != nil {
		return management.CommandResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return management.CommandResult{}, err
	}
	entry, existing, immediate := w.redisControlEntry(command)
	if immediate != (management.CommandResult{}) {
		return immediate, nil
	}
	if existing {
		select {
		case <-entry.done:
			return entry.result, nil
		case <-ctx.Done():
			return management.CommandResult{}, ctx.Err()
		}
	}

	w.controlApplyMu.Lock()
	result := w.executeRedisCommand(ctx, command)
	w.controlApplyMu.Unlock()
	entry.once.Do(func() {
		entry.result = result
		close(entry.done)
	})

	return result, nil
}

func (w *Worker) redisControlEntry(
	command management.Command,
) (*redisControlEntry, bool, management.CommandResult) {
	w.controlMu.Lock()
	defer w.controlMu.Unlock()
	if w.controlEntries == nil {
		w.controlEntries = make(map[string]*redisControlEntry)
	}
	if entry, exists := w.controlEntries[command.IdempotencyKey]; exists {
		if !reflect.DeepEqual(entry.command, command) {
			return nil, false, w.redisControlResult(
				command, management.CommandRejected, "idempotency_conflict",
			)
		}

		return entry, true, management.CommandResult{}
	}
	capacity := w.controlCapacity
	if capacity <= 0 {
		capacity = defaultRedisControlCapacity
	}
	if len(w.controlEntries) >= capacity {
		return nil, false, w.redisControlResult(
			command, management.CommandRejected, "idempotency_capacity",
		)
	}
	entry := &redisControlEntry{command: command, done: make(chan struct{})}
	w.controlEntries[command.IdempotencyKey] = entry

	return entry, false, management.CommandResult{}
}

func (w *Worker) executeRedisCommand(
	ctx context.Context,
	command management.Command,
) management.CommandResult {
	if command.Protocol != w.opts.management.Protocol {
		return w.redisControlResult(command, management.CommandUnsupported, "protocol_mismatch")
	}
	if !command.Deadline.After(time.Now()) {
		return w.redisControlResult(command, management.CommandTimedOut, "deadline_exceeded")
	}
	commandContext, cancel := context.WithDeadline(ctx, command.Deadline)
	defer cancel()
	switch command.Action {
	case management.CommandRetry:
		return w.retryRedisRecord(commandContext, command)
	case management.CommandBulkRetry:
		return w.bulkRetryRedisRecords(commandContext, command)
	case management.CommandDelete:
		return w.deleteRedisRecord(commandContext, command)
	case management.CommandPurge:
		if command.Target.Kind == management.TargetQueue {
			return w.redisControlResult(command, management.CommandUnsupported, "unsupported_action")
		}
		return w.purgeRedisRecords(commandContext, command)
	case management.CommandReplay:
		return w.replayRedisRecord(commandContext, command)
	default:
		return w.redisControlResult(command, management.CommandUnsupported, "unsupported_action")
	}
}

func (w *Worker) replayRedisRecord(
	ctx context.Context,
	command management.Command,
) management.CommandResult {
	if len(w.opts.replayDestinations) == 0 {
		return w.redisControlResult(command, management.CommandUnsupported, "unsupported_action")
	}
	destination := command.Replay.Destination
	if _, allowed := w.opts.replayDestinations[destination]; !allowed {
		return w.redisControlResult(command, management.CommandRejected, "destination_not_allowed")
	}
	recordStream := w.opts.failureStream
	if command.Target.Kind == management.TargetDeadLetter {
		recordStream = w.opts.deadLetterStream
	}
	messages, err := w.rdb.XRangeN(
		ctx, recordStream, command.Target.Name, command.Target.Name, 1,
	).Result()
	if err != nil {
		return w.redisControlResult(command, management.CommandUnknown, "")
	}
	if len(messages) != 1 || messages[0].ID != command.Target.Name {
		return w.redisControlResult(command, management.CommandRejected, "record_not_found")
	}
	body, bodyOK := redisRecordString(messages[0].Values[streamBodyField])
	if !bodyOK {
		return w.redisControlResult(command, management.CommandFailed, "record_malformed")
	}
	original, prior, generation, lineageOK := redisNextReplayLineage(messages[0])
	if !lineageOK {
		return w.redisControlResult(command, management.CommandFailed, "record_malformed")
	}
	replayKey := base64.RawURLEncoding.EncodeToString(
		[]byte(recordStream + "\x00" + command.Target.Name),
	)
	registry := destination + ":queue:replay-index"
	priorReplay, registryErr := w.rdb.HGet(ctx, registry, replayKey).Result()
	if registryErr != nil && !errors.Is(registryErr, redis.Nil) {
		return w.redisControlResult(command, management.CommandUnknown, "")
	}
	if priorReplay != "" && command.Replay.IdempotencyPolicy == management.ReplayRejectDuplicate {
		return w.redisControlResult(command, management.CommandRejected, "replay_duplicate")
	}
	if priorReplay == "" {
		registrySize, sizeErr := w.rdb.HLen(ctx, registry).Result()
		if sizeErr != nil {
			return w.redisControlResult(command, management.CommandUnknown, "")
		}
		if registrySize >= redisReplayRegistryCapacity {
			return w.redisControlResult(command, management.CommandRejected, "replay_registry_capacity")
		}
	}
	replayedID, appendErr := w.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: destination, MaxLen: w.opts.maxLength,
		Approx: w.opts.maxLength > 0,
		Values: map[string]any{
			streamBodyField:               body,
			replayOriginalDeadLetterField: original,
			replayPriorDeadLetterField:    prior,
			replayGenerationField:         strconv.FormatUint(uint64(generation), 10),
		},
	}).Result()
	if appendErr != nil {
		return w.redisControlResult(command, management.CommandFailed, "enqueue_failed")
	}
	if command.Replay.IdempotencyPolicy == management.ReplayRejectDuplicate {
		stored, storeErr := w.rdb.HSetNX(ctx, registry, replayKey, replayedID).Result()
		if storeErr != nil {
			return w.redisControlResult(command, management.CommandPartial, "replay_registry_unknown")
		}
		if !stored {
			deleted, deleteErr := w.rdb.XDel(ctx, destination, replayedID).Result()
			if deleteErr != nil || deleted != 1 {
				return w.redisControlResult(command, management.CommandPartial, "replay_duplicate_unknown")
			}
			return w.redisControlResult(command, management.CommandRejected, "replay_duplicate")
		}
		return w.redisControlResult(command, management.CommandAcknowledged, "")
	}
	if err := w.rdb.HSet(ctx, registry, replayKey, replayedID).Err(); err != nil {
		return w.redisControlResult(command, management.CommandPartial, "replay_registry_unknown")
	}
	if priorReplay != "" {
		deleted, deleteErr := w.rdb.XDel(ctx, destination, priorReplay).Result()
		if deleteErr != nil || deleted != 1 {
			return w.redisControlResult(command, management.CommandPartial, "replay_replace_unknown")
		}
	}

	return w.redisControlResult(command, management.CommandAcknowledged, "")
}

func redisNextReplayLineage(message redis.XMessage) (string, string, uint32, bool) {
	lineage, err := redisLineageFromValues(message.Values)
	if err != nil {
		return "", "", 0, false
	}
	if lineage.generation == 0 {
		return message.ID, message.ID, 1, true
	}
	if lineage.generation == ^uint32(0) {
		return "", "", 0, false
	}

	return lineage.original, message.ID, lineage.generation + 1, true
}

func (w *Worker) bulkRetryRedisRecords(
	ctx context.Context,
	command management.Command,
) management.CommandResult {
	recordStream := w.opts.failureStream
	if command.Target.Kind == management.TargetDeadLetter {
		recordStream = w.opts.deadLetterStream
	}
	messages, err := w.rdb.XRangeN(
		ctx, recordStream, "-", "+", int64(command.Selection.Limit),
	).Result()
	if err != nil {
		return w.redisControlResult(command, management.CommandFailed, "records_unavailable")
	}
	for index, message := range messages {
		retry := command
		retry.Action = management.CommandRetry
		retry.Target.Name = message.ID
		retry.Selection = nil
		result := w.retryRedisRecord(ctx, retry)
		if result.Status == management.CommandAcknowledged {
			continue
		}
		if index == 0 {
			return w.redisControlResult(command, result.Status, result.FailureCode)
		}

		return w.redisControlResult(command, management.CommandPartial, "bulk_retry_partial")
	}

	return w.redisControlResult(command, management.CommandAcknowledged, "")
}

func (w *Worker) deleteRedisRecord(
	ctx context.Context,
	command management.Command,
) management.CommandResult {
	recordStream := w.opts.failureStream
	if command.Target.Kind == management.TargetDeadLetter {
		recordStream = w.opts.deadLetterStream
	}
	deleted, err := w.rdb.XDel(ctx, recordStream, command.Target.Name).Result()
	if err != nil {
		return w.redisControlResult(command, management.CommandUnknown, "")
	}
	if deleted != 1 {
		return w.redisControlResult(command, management.CommandRejected, "record_not_found")
	}

	return w.redisControlResult(command, management.CommandAcknowledged, "")
}

func (w *Worker) purgeRedisRecords(
	ctx context.Context,
	command management.Command,
) management.CommandResult {
	recordStream := w.opts.failureStream
	if command.Target.Kind == management.TargetDeadLetter {
		recordStream = w.opts.deadLetterStream
	}
	if err := w.rdb.Del(ctx, recordStream).Err(); err != nil {
		return w.redisControlResult(command, management.CommandUnknown, "")
	}

	return w.redisControlResult(command, management.CommandAcknowledged, "")
}

func (w *Worker) retryRedisRecord(
	ctx context.Context,
	command management.Command,
) management.CommandResult {
	recordStream := w.opts.failureStream
	failure := command.Target.Kind == management.TargetFailure
	if !failure {
		recordStream = w.opts.deadLetterStream
	}
	messages, err := w.rdb.XRangeN(
		ctx, recordStream, command.Target.Name, command.Target.Name, 1,
	).Result()
	if err != nil {
		return w.redisControlResult(command, management.CommandUnknown, "")
	}
	if len(messages) != 1 || messages[0].ID != command.Target.Name {
		return w.redisControlResult(command, management.CommandRejected, "record_not_found")
	}
	body, bodyOK := redisRecordString(messages[0].Values[streamBodyField])
	originalID, originalOK := redisRecordString(messages[0].Values[originalIDField])
	if !bodyOK || !originalOK || originalID == "" {
		return w.redisControlResult(command, management.CommandFailed, "record_malformed")
	}
	if failure {
		pending, pendingErr := w.rdb.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream: w.opts.streamName, Group: w.opts.group,
			Start: originalID, End: originalID, Count: 1,
		}).Result()
		if pendingErr != nil {
			return w.redisControlResult(command, management.CommandUnknown, "")
		}
		if len(pending) != 1 || pending[0].ID != originalID {
			return w.redisControlResult(command, management.CommandRejected, "source_record_stale")
		}
	}
	values := map[string]any{streamBodyField: body}
	if !failure {
		original, prior, generation, ok := redisNextReplayLineage(messages[0])
		if !ok {
			return w.redisControlResult(command, management.CommandFailed, "record_malformed")
		}
		values[replayOriginalDeadLetterField] = original
		values[replayPriorDeadLetterField] = prior
		values[replayGenerationField] = strconv.FormatUint(uint64(generation), 10)
	}
	if err := w.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: w.opts.streamName, MaxLen: w.opts.maxLength,
		Approx: w.opts.maxLength > 0, Values: values,
	}).Err(); err != nil {
		return w.redisControlResult(command, management.CommandFailed, "enqueue_failed")
	}
	if failure {
		acknowledged, ackErr := w.rdb.XAck(
			ctx, w.opts.streamName, w.opts.group, originalID,
		).Result()
		if ackErr != nil || acknowledged != 1 {
			return w.redisControlResult(command, management.CommandPartial, "source_ack_unknown")
		}
	}
	deleted, err := w.rdb.XDel(ctx, recordStream, command.Target.Name).Result()
	if err != nil || deleted != 1 {
		return w.redisControlResult(command, management.CommandPartial, "record_delete_unknown")
	}

	return w.redisControlResult(command, management.CommandAcknowledged, "")
}

func (w *Worker) redisControlResult(
	command management.Command,
	status management.CommandResultStatus,
	failureCode string,
) management.CommandResult {
	return management.CommandResult{
		CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
		WorkerID: w.opts.management.ID, Protocol: w.opts.management.Protocol,
		Status: status, FailureCode: failureCode, CompletedAt: time.Now().UTC(),
	}
}
