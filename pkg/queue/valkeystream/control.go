package valkeystream

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
)

const defaultControlCapacity = 1_024

var ErrManagementControlDisabled = errors.New("valkeystream: management control disabled")

var _ management.Controller = (*Worker)(nil)

type nativeMutationTransport interface {
	nativeRecordTransport
	RetryRecord(context.Context, string, string, string, string, string, bool) (nativeRetryOutcome, error)
	ReplayRecord(context.Context, string, string, string, management.ReplayPolicy) (nativeReplayOutcome, error)
	DeleteRecord(context.Context, string, string) (bool, error)
	PurgeRecords(context.Context, string) error
}

type nativeReplayOutcome string

const (
	nativeReplayOK        nativeReplayOutcome = "ok"
	nativeReplayNotFound  nativeReplayOutcome = "not_found"
	nativeReplayMalformed nativeReplayOutcome = "malformed"
	nativeReplayDuplicate nativeReplayOutcome = "duplicate"
)

func (o nativeReplayOutcome) valid() bool {
	switch o {
	case nativeReplayOK, nativeReplayNotFound, nativeReplayMalformed, nativeReplayDuplicate:
		return true
	default:
		return false
	}
}

type nativeRetryOutcome string

const (
	nativeRetryOK        nativeRetryOutcome = "ok"
	nativeRetryNotFound  nativeRetryOutcome = "not_found"
	nativeRetryStale     nativeRetryOutcome = "stale"
	nativeRetryMalformed nativeRetryOutcome = "malformed"
)

func (o nativeRetryOutcome) valid() bool {
	switch o {
	case nativeRetryOK, nativeRetryNotFound, nativeRetryStale, nativeRetryMalformed:
		return true
	default:
		return false
	}
}

type nativeControlEntry struct {
	command management.Command
	result  management.CommandResult
	done    chan struct{}
	once    sync.Once
}

// Execute applies one bounded native Valkey record mutation. Replay and queue
// purge remain unsupported until their durable backend semantics are proven.
func (w *Worker) Execute(
	ctx context.Context, command management.Command,
) (management.CommandResult, error) {
	if w.opts.management == nil {
		return management.CommandResult{}, ErrManagementControlDisabled
	}
	if err := command.Validate(); err != nil {
		return management.CommandResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return management.CommandResult{}, err
	}
	if _, ok := w.transport.(nativeMutationTransport); !ok {
		return management.CommandResult{}, ErrManagementControlDisabled
	}
	entry, existing, immediate := w.controlEntry(command)
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
	result := w.executeNativeCommand(ctx, command)
	w.controlApplyMu.Unlock()
	entry.once.Do(func() {
		entry.result = result
		close(entry.done)
	})
	return result, nil
}

func (w *Worker) controlEntry(
	command management.Command,
) (*nativeControlEntry, bool, management.CommandResult) {
	w.controlMu.Lock()
	defer w.controlMu.Unlock()
	if w.controlEntries == nil {
		w.controlEntries = make(map[string]*nativeControlEntry)
	}
	if entry, exists := w.controlEntries[command.IdempotencyKey]; exists {
		if !reflect.DeepEqual(entry.command, command) {
			return nil, false, w.controlResult(command, management.CommandRejected, "idempotency_conflict")
		}
		return entry, true, management.CommandResult{}
	}
	capacity := w.controlCapacity
	if capacity <= 0 {
		capacity = defaultControlCapacity
	}
	if len(w.controlEntries) >= capacity {
		return nil, false, w.controlResult(command, management.CommandRejected, "idempotency_capacity")
	}
	entry := &nativeControlEntry{command: command, done: make(chan struct{})}
	w.controlEntries[command.IdempotencyKey] = entry
	return entry, false, management.CommandResult{}
}

func (w *Worker) executeNativeCommand(
	ctx context.Context, command management.Command,
) management.CommandResult {
	if command.Protocol != w.opts.management.Protocol {
		return w.controlResult(command, management.CommandUnsupported, "protocol_mismatch")
	}
	now := w.controlNow()
	if !command.Deadline.After(now) {
		return w.controlResult(command, management.CommandTimedOut, "deadline_exceeded")
	}
	commandContext, cancel := context.WithDeadline(ctx, command.Deadline)
	defer cancel()
	transport := w.transport.(nativeMutationTransport)

	switch command.Action {
	case management.CommandRetry:
		return w.retryNativeRecord(commandContext, transport, command)
	case management.CommandBulkRetry:
		return w.bulkRetryNativeRecords(commandContext, transport, command)
	case management.CommandDelete:
		return w.deleteNativeRecord(commandContext, transport, command)
	case management.CommandPurge:
		if command.Target.Kind == management.TargetQueue {
			return w.controlResult(command, management.CommandUnsupported, "unsupported_action")
		}
		if err := transport.PurgeRecords(commandContext, w.recordStream(command.Target.Kind)); err != nil {
			return w.controlResult(command, management.CommandUnknown, "")
		}
		return w.controlResult(command, management.CommandAcknowledged, "")
	case management.CommandReplay:
		return w.replayNativeRecord(commandContext, transport, command)
	default:
		return w.controlResult(command, management.CommandUnsupported, "unsupported_action")
	}
}

func (w *Worker) replayNativeRecord(
	ctx context.Context, transport nativeMutationTransport, command management.Command,
) management.CommandResult {
	if len(w.opts.replayDestinations) == 0 {
		return w.controlResult(command, management.CommandUnsupported, "unsupported_action")
	}
	destination := command.Replay.Destination
	if _, allowed := w.opts.replayDestinations[destination]; !allowed {
		return w.controlResult(command, management.CommandRejected, "destination_not_allowed")
	}
	outcome, err := transport.ReplayRecord(
		ctx, w.recordStream(command.Target.Kind), command.Target.Name,
		destination, command.Replay.IdempotencyPolicy,
	)
	if err != nil {
		return w.controlResult(command, management.CommandUnknown, "")
	}
	switch outcome {
	case nativeReplayOK:
		return w.controlResult(command, management.CommandAcknowledged, "")
	case nativeReplayNotFound:
		return w.controlResult(command, management.CommandRejected, "record_not_found")
	case nativeReplayMalformed:
		return w.controlResult(command, management.CommandFailed, "record_malformed")
	case nativeReplayDuplicate:
		return w.controlResult(command, management.CommandRejected, "replay_duplicate")
	default:
		return w.controlResult(command, management.CommandUnknown, "")
	}
}

func (w *Worker) retryNativeRecord(
	ctx context.Context, transport nativeMutationTransport, command management.Command,
) management.CommandResult {
	outcome, err := transport.RetryRecord(
		ctx, w.recordStream(command.Target.Kind), command.Target.Name,
		w.opts.stream, w.opts.stream, w.opts.group,
		command.Target.Kind == management.TargetFailure,
	)
	if err != nil {
		return w.controlResult(command, management.CommandUnknown, "")
	}
	switch outcome {
	case nativeRetryOK:
		return w.controlResult(command, management.CommandAcknowledged, "")
	case nativeRetryNotFound:
		return w.controlResult(command, management.CommandRejected, "record_not_found")
	case nativeRetryStale:
		return w.controlResult(command, management.CommandRejected, "source_record_stale")
	case nativeRetryMalformed:
		return w.controlResult(command, management.CommandFailed, "record_malformed")
	default:
		return w.controlResult(command, management.CommandUnknown, "")
	}
}

func (w *Worker) bulkRetryNativeRecords(
	ctx context.Context, transport nativeMutationTransport, command management.Command,
) management.CommandResult {
	records, err := transport.ReadRecords(ctx, w.recordStream(command.Target.Kind))
	if err != nil {
		return w.controlResult(command, management.CommandFailed, "records_unavailable")
	}
	limit := min(len(records), int(command.Selection.Limit))
	for index := range limit {
		outcome, retryErr := transport.RetryRecord(
			ctx, w.recordStream(command.Target.Kind), records[index].ID,
			w.opts.stream, w.opts.stream, w.opts.group,
			command.Target.Kind == management.TargetFailure,
		)
		if retryErr != nil {
			if index == 0 {
				return w.controlResult(command, management.CommandUnknown, "")
			}
			return w.controlResult(command, management.CommandPartial, "bulk_enqueue_unknown")
		}
		if outcome != nativeRetryOK {
			return w.controlResult(command, management.CommandPartial, "bulk_record_unavailable")
		}
	}
	return w.controlResult(command, management.CommandAcknowledged, "")
}

func (w *Worker) deleteNativeRecord(
	ctx context.Context, transport nativeMutationTransport, command management.Command,
) management.CommandResult {
	deleted, err := transport.DeleteRecord(
		ctx, w.recordStream(command.Target.Kind), command.Target.Name,
	)
	if err != nil {
		return w.controlResult(command, management.CommandUnknown, "")
	}
	if !deleted {
		return w.controlResult(command, management.CommandRejected, "record_not_found")
	}
	return w.controlResult(command, management.CommandAcknowledged, "")
}

func (w *Worker) recordStream(kind management.TargetKind) string {
	if kind == management.TargetDeadLetter {
		return w.opts.deadLetterStream
	}
	return w.opts.failureStream
}

func (w *Worker) controlResult(
	command management.Command, status management.CommandResultStatus, failureCode string,
) management.CommandResult {
	return management.CommandResult{
		CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
		WorkerID: w.opts.management.ID, Protocol: w.opts.management.Protocol,
		Status: status, FailureCode: failureCode, CompletedAt: w.controlNow(),
	}
}

func (w *Worker) controlNow() time.Time {
	if w.now == nil {
		return time.Now().UTC()
	}
	return w.now().UTC()
}
