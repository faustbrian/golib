package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	gopostgres "github.com/faustbrian/golib/pkg/postgres"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	"github.com/faustbrian/golib/pkg/queue-control-plane/history"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	// ErrNilBeginner reports a missing PostgreSQL transaction source.
	ErrNilBeginner = errors.New("postgres: transaction beginner is nil")
	// ErrIdempotencyConflict reports reuse of a key for another command.
	ErrIdempotencyConflict = errors.New("postgres: idempotency key conflicts with stored command")
	// ErrCompletionConflict reports an attempt to replace a terminal result.
	ErrCompletionConflict = errors.New("postgres: terminal command result conflicts with stored result")
	// ErrCommandNotFound reports completion of an unknown tenant-scoped key.
	ErrCommandNotFound = errors.New("postgres: command not found")
	// ErrAuditSequenceExhausted reports an audit chain beyond bigint capacity.
	ErrAuditSequenceExhausted = errors.New("postgres: audit sequence exhausted")
	// ErrAuditHashInvalid reports malformed persisted audit-chain state.
	ErrAuditHashInvalid = errors.New("postgres: invalid audit hash")
	// ErrDesiredStateConflict reports a concurrent desired-state replacement.
	ErrDesiredStateConflict = errors.New("postgres: desired state update conflict")
)

// NewJournal creates a journal backed by postgres transaction semantics.
func NewJournal(beginner gopostgres.Beginner) (*Journal, error) {
	if beginner == nil {
		return nil, ErrNilBeginner
	}

	return newJournal(newPostgresTransactionRunner(beginner)), nil
}

type postgresTransactionRunner struct {
	beginner gopostgres.Beginner
}

func newPostgresTransactionRunner(beginner gopostgres.Beginner) *postgresTransactionRunner {
	return &postgresTransactionRunner{beginner: beginner}
}

func (r *postgresTransactionRunner) WithinTransaction(
	ctx context.Context,
	fn func(context.Context, journalTransaction) error,
) error {
	return gopostgres.RunTransaction(
		ctx,
		r.beginner,
		gopostgres.TransactionOptions{},
		func(ctx context.Context, tx pgx.Tx) error {
			return fn(ctx, &sqlJournalTransaction{tx: tx})
		},
	)
}

type sqlExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type sqlJournalTransaction struct {
	tx sqlExecutor
}

func (t *sqlJournalTransaction) Accept(
	ctx context.Context,
	command controlplane.Command,
) (controlplane.CommandResult, bool, error) {
	if command.AuthenticationMethod == "" {
		command.AuthenticationMethod = "internal"
	}
	if command.Capability == "" {
		command.Capability = string(command.Action)
	}
	command.RequestedAt = postgresTimestamp(command.RequestedAt)
	if command.Deadline.IsZero() {
		command.Deadline = command.RequestedAt.Add(controlplane.DefaultCommandLifetime)
	}
	command.Deadline = postgresTimestamp(command.Deadline)
	selectionLimit, replayDestination, replayPolicy, scaleReplicas := commandOptions(command)
	tag, err := t.tx.Exec(
		ctx,
		insertCommandSQL,
		command.TenantID,
		command.IdempotencyKey,
		command.CommandID,
		command.Actor,
		command.AuthenticationMethod,
		command.Reason,
		command.Action,
		command.Capability,
		command.Target.Kind,
		command.Target.Name,
		command.RequestedAt,
		command.Deadline,
		command.Confirmed,
		selectionLimit,
		replayDestination,
		replayPolicy,
		scaleReplicas,
		controlplane.CommandPending,
	)
	if err != nil {
		return controlplane.CommandResult{}, false, fmt.Errorf("postgres: insert command: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return controlplane.CommandResult{
			CommandID:      command.CommandID,
			IdempotencyKey: command.IdempotencyKey,
			TenantID:       command.TenantID,
			Status:         controlplane.CommandPending,
		}, true, nil
	}

	storedCommand, storedResult, err := t.loadCommand(
		ctx,
		loadCommandSQL,
		command.TenantID,
		command.IdempotencyKey,
	)
	if err != nil {
		return controlplane.CommandResult{}, false, err
	}
	if !commandsEqual(storedCommand, command) {
		return controlplane.CommandResult{}, false, ErrIdempotencyConflict
	}

	return storedResult, false, nil
}

func (t *sqlJournalTransaction) Complete(
	ctx context.Context,
	result controlplane.CommandResult,
) (controlplane.Command, bool, error) {
	command, stored, err := t.loadCommand(
		ctx,
		loadCommandForUpdateSQL,
		result.TenantID,
		result.IdempotencyKey,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return controlplane.Command{}, false, ErrCommandNotFound
	}
	if err != nil {
		return controlplane.Command{}, false, err
	}
	if resultsEqual(stored, result) {
		return command, false, nil
	}
	expected := transitionSources(result.Status)
	if !containsStatus(expected, stored.Status) {
		return controlplane.Command{}, false, ErrCompletionConflict
	}

	failure := nullableString(result.Failure)
	result.DispatchedAt = postgresTimestamp(result.DispatchedAt)
	result.AcknowledgedAt = postgresTimestamp(result.AcknowledgedAt)
	result.CompletedAt = postgresTimestamp(result.CompletedAt)
	tag, err := t.tx.Exec(
		ctx,
		completeCommandSQL,
		result.TenantID,
		result.IdempotencyKey,
		result.Status,
		failure,
		nullableTime(result.DispatchedAt),
		nullableTime(result.AcknowledgedAt),
		nullableTime(result.CompletedAt),
		nullableBool(result.CapabilityAvailable),
		nullableString(result.WorkerID),
		protocolPart(result.Protocol, true),
		protocolPart(result.Protocol, false),
		expected,
	)
	if err != nil {
		return controlplane.Command{}, false, fmt.Errorf("postgres: complete command: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return controlplane.Command{}, false, ErrCompletionConflict
	}

	return command, true, nil
}

func (t *sqlJournalTransaction) ApplyDesired(
	ctx context.Context,
	command controlplane.Command,
) error {
	next, err := control.NextDesiredState(nil, command)
	if errors.Is(err, control.ErrNotDesiredStateAction) {
		return nil
	}
	if err != nil {
		return err
	}

	current, err := t.loadDesired(ctx, command.TenantID, command.Target)
	if err == nil {
		next, err = control.NextDesiredState(&current, command)
	} else if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	} else {
		return err
	}
	if err != nil {
		return err
	}
	if next.Revision > math.MaxInt64 {
		return control.ErrDesiredRevisionExhausted
	}

	next.ChangedAt = postgresTimestamp(next.ChangedAt)
	tag, err := t.tx.Exec(
		ctx,
		upsertDesiredStateSQL,
		next.TenantID,
		next.Target.Kind,
		next.Target.Name,
		next.State,
		int64(next.Revision),
		next.CommandKey,
		next.ChangedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: store desired state: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return ErrDesiredStateConflict
	}

	return nil
}

func (t *sqlJournalTransaction) AppendAudit(
	ctx context.Context,
	tenant string,
	event history.Event,
) error {
	zero := history.Hash{}
	if _, err := t.tx.Exec(ctx, ensureAuditAnchorSQL, tenant, zero[:], time.Unix(0, 0).UTC()); err != nil {
		return fmt.Errorf("postgres: ensure audit anchor: %w", err)
	}

	var sequence int64
	var encodedHash []byte
	if err := t.tx.QueryRow(ctx, loadAuditHeadSQL, tenant).Scan(&sequence, &encodedHash); err != nil {
		return fmt.Errorf("postgres: load audit head: %w", err)
	}
	if sequence < 0 {
		return ErrInvalidAuditState
	}
	if sequence == math.MaxInt64 {
		return ErrAuditSequenceExhausted
	}
	previous, err := decodeHash(encodedHash)
	if err != nil {
		return err
	}

	sequence++
	event.Sequence, _ = uint64FromInt64(sequence)
	event.HashVersion = 2
	event.OccurredAt = postgresTimestamp(event.OccurredAt)
	entry := history.Seal(previous, event)
	if _, err := t.tx.Exec(
		ctx,
		insertAuditEventSQL,
		tenant,
		sequence,
		int16(2),
		event.CommandID,
		nullableString(event.IdempotencyKey),
		event.OccurredAt,
		event.Actor,
		event.Action,
		event.Target,
		event.Result,
		entry.PreviousHash[:],
		entry.Hash[:],
	); err != nil {
		return fmt.Errorf("postgres: insert audit event: %w", err)
	}

	return nil
}

func (t *sqlJournalTransaction) loadCommand(
	ctx context.Context,
	query string,
	tenant string,
	key string,
) (controlplane.Command, controlplane.CommandResult, error) {
	return scanStoredCommand(t.tx.QueryRow(ctx, query, tenant, key))
}

func scanStoredCommand(row interface{ Scan(...any) error }) (controlplane.Command, controlplane.CommandResult, error) {
	var command controlplane.Command
	var result controlplane.CommandResult
	var action string
	var capability string
	var targetKind string
	var selection sql.NullInt64
	var replayDestination sql.NullString
	var replayPolicy sql.NullString
	var scaleReplicas sql.NullInt64
	var status string
	var failure sql.NullString
	var capabilityAvailable sql.NullBool
	var workerID sql.NullString
	var protocolMajor sql.NullInt64
	var protocolMinor sql.NullInt64
	var dispatched sql.NullTime
	var acknowledged sql.NullTime
	var completed sql.NullTime
	err := row.Scan(
		&command.TenantID,
		&command.IdempotencyKey,
		&command.CommandID,
		&command.Actor,
		&command.AuthenticationMethod,
		&command.Reason,
		&action,
		&capability,
		&targetKind,
		&command.Target.Name,
		&command.RequestedAt,
		&command.Deadline,
		&command.Confirmed,
		&selection,
		&replayDestination,
		&replayPolicy,
		&scaleReplicas,
		&status,
		&failure,
		&capabilityAvailable,
		&workerID,
		&protocolMajor,
		&protocolMinor,
		&dispatched,
		&acknowledged,
		&completed,
	)
	if err != nil {
		return controlplane.Command{}, controlplane.CommandResult{}, fmt.Errorf("postgres: load command: %w", err)
	}
	command.RequestedAt = command.RequestedAt.UTC()
	command.Deadline = command.Deadline.UTC()

	command.Action = controlplane.Action(action)
	command.Capability = capability
	command.Target.Kind = controlplane.TargetKind(targetKind)
	if selection.Valid {
		limit, ok := uint32FromInt64(selection.Int64)
		if !ok {
			return controlplane.Command{}, controlplane.CommandResult{}, ErrInvalidCommandState
		}
		command.Selection = &controlplane.Selection{Limit: limit}
	}
	if replayDestination.Valid || replayPolicy.Valid {
		command.Replay = &controlplane.Replay{
			Destination:       replayDestination.String,
			IdempotencyPolicy: controlplane.ReplayPolicy(replayPolicy.String),
		}
	}
	if scaleReplicas.Valid {
		replicas, ok := uint32FromInt64(scaleReplicas.Int64)
		if !ok {
			return controlplane.Command{}, controlplane.CommandResult{}, ErrInvalidCommandState
		}
		command.Scale = &controlplane.Scale{Replicas: replicas}
	}
	result = controlplane.CommandResult{
		CommandID:      command.CommandID,
		IdempotencyKey: command.IdempotencyKey,
		TenantID:       command.TenantID,
		Status:         controlplane.CommandStatus(status),
		Failure:        failure.String,
		WorkerID:       workerID.String,
	}
	if protocolMajor.Valid && protocolMinor.Valid {
		major, majorOK := uint16FromInt64(protocolMajor.Int64)
		minor, minorOK := uint16FromInt64(protocolMinor.Int64)
		if !majorOK || !minorOK {
			return controlplane.Command{}, controlplane.CommandResult{}, ErrInvalidCommandState
		}
		result.Protocol = &controlplane.ProtocolVersion{
			Major: major, Minor: minor,
		}
	}
	if capabilityAvailable.Valid {
		value := capabilityAvailable.Bool
		result.CapabilityAvailable = &value
	}
	if dispatched.Valid {
		result.DispatchedAt = dispatched.Time.UTC()
	}
	if acknowledged.Valid {
		result.AcknowledgedAt = acknowledged.Time.UTC()
	}
	if completed.Valid {
		result.CompletedAt = completed.Time.UTC()
	}

	return command, result, nil
}

func (t *sqlJournalTransaction) loadDesired(
	ctx context.Context,
	tenant string,
	target controlplane.Target,
) (control.DesiredRecord, error) {
	record, err := scanDesired(t.tx.QueryRow(ctx, loadDesiredStateForUpdateSQL, tenant, target.Kind, target.Name))
	if err != nil {
		return control.DesiredRecord{}, fmt.Errorf("postgres: load desired state: %w", err)
	}

	return record, nil
}

func scanDesired(row pgx.Row) (control.DesiredRecord, error) {
	var record control.DesiredRecord
	var targetKind string
	var state string
	var revision int64
	err := row.Scan(
		&record.TenantID,
		&targetKind,
		&record.Target.Name,
		&state,
		&revision,
		&record.CommandKey,
		&record.ChangedAt,
	)
	if err != nil {
		return control.DesiredRecord{}, err
	}
	record.Target.Kind = controlplane.TargetKind(targetKind)
	record.State = control.DesiredState(state)
	var ok bool
	record.Revision, ok = uint64FromInt64(revision)
	if !ok {
		return control.DesiredRecord{}, control.ErrInvalidDesiredTransition
	}
	record.ChangedAt = record.ChangedAt.UTC()

	return record, nil
}

func commandOptions(command controlplane.Command) (any, any, any, any) {
	var selectionLimit any
	if command.Selection != nil {
		selectionLimit = int64(command.Selection.Limit)
	}
	var replayDestination any
	var replayPolicy any
	if command.Replay != nil {
		replayDestination = command.Replay.Destination
		replayPolicy = command.Replay.IdempotencyPolicy
	}
	var scaleReplicas any
	if command.Scale != nil {
		scaleReplicas = int64(command.Scale.Replicas)
	}

	return selectionLimit, replayDestination, replayPolicy, scaleReplicas
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}

	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}

	return value
}

func nullableBool(value *bool) any {
	if value == nil {
		return nil
	}

	return *value
}

func protocolPart(value *controlplane.ProtocolVersion, major bool) any {
	if value == nil {
		return nil
	}
	if major {
		return int64(value.Major)
	}

	return int64(value.Minor)
}

func transitionSources(status controlplane.CommandStatus) []string {
	switch status {
	case controlplane.CommandDispatched:
		return []string{string(controlplane.CommandPending), string(controlplane.CommandAccepted)}
	case controlplane.CommandAcknowledged:
		return []string{string(controlplane.CommandDispatched)}
	case controlplane.CommandSucceeded:
		return []string{string(controlplane.CommandAcknowledged), string(controlplane.CommandAccepted)}
	case controlplane.CommandCanceled:
		return []string{string(controlplane.CommandPending), string(controlplane.CommandAccepted)}
	default:
		return []string{
			string(controlplane.CommandDispatched),
			string(controlplane.CommandAcknowledged),
			string(controlplane.CommandAccepted),
		}
	}
}

func containsStatus(statuses []string, status controlplane.CommandStatus) bool {
	for _, candidate := range statuses {
		if candidate == string(status) {
			return true
		}
	}

	return false
}

func commandsEqual(left controlplane.Command, right controlplane.Command) bool {
	return left.IdempotencyKey == right.IdempotencyKey &&
		left.TenantID == right.TenantID &&
		left.Actor == right.Actor &&
		commandAuthenticationMethod(left) == commandAuthenticationMethod(right) &&
		left.Reason == right.Reason &&
		left.Action == right.Action &&
		commandCapability(left) == commandCapability(right) &&
		left.Target == right.Target &&
		postgresTimestamp(left.RequestedAt).Equal(postgresTimestamp(right.RequestedAt)) &&
		commandDeadline(left).Equal(commandDeadline(right)) &&
		left.Confirmed == right.Confirmed &&
		selectionsEqual(left.Selection, right.Selection) &&
		replaysEqual(left.Replay, right.Replay) &&
		scalesEqual(left.Scale, right.Scale)
}

func commandCapability(command controlplane.Command) string {
	if command.Capability == "" {
		return string(command.Action)
	}

	return command.Capability
}

func commandAuthenticationMethod(command controlplane.Command) string {
	if command.AuthenticationMethod == "" {
		return "internal"
	}

	return command.AuthenticationMethod
}

func commandDeadline(command controlplane.Command) time.Time {
	deadline := command.Deadline
	if deadline.IsZero() {
		deadline = command.RequestedAt.Add(controlplane.DefaultCommandLifetime)
	}

	return postgresTimestamp(deadline)
}

func selectionsEqual(left *controlplane.Selection, right *controlplane.Selection) bool {
	if left == nil || right == nil {
		return left == right
	}

	return *left == *right
}

func replaysEqual(left *controlplane.Replay, right *controlplane.Replay) bool {
	if left == nil || right == nil {
		return left == right
	}

	return *left == *right
}

func scalesEqual(left *controlplane.Scale, right *controlplane.Scale) bool {
	if left == nil || right == nil {
		return left == right
	}

	return *left == *right
}

func resultsEqual(left controlplane.CommandResult, right controlplane.CommandResult) bool {
	return left.CommandID == right.CommandID &&
		left.IdempotencyKey == right.IdempotencyKey &&
		left.TenantID == right.TenantID &&
		left.Status == right.Status &&
		left.Failure == right.Failure &&
		left.WorkerID == right.WorkerID &&
		protocolsEqual(left.Protocol, right.Protocol) &&
		optionalBoolsEqual(left.CapabilityAvailable, right.CapabilityAvailable) &&
		postgresTimestamp(left.DispatchedAt).Equal(postgresTimestamp(right.DispatchedAt)) &&
		postgresTimestamp(left.AcknowledgedAt).Equal(postgresTimestamp(right.AcknowledgedAt)) &&
		postgresTimestamp(left.CompletedAt).Equal(postgresTimestamp(right.CompletedAt))
}

func protocolsEqual(left *controlplane.ProtocolVersion, right *controlplane.ProtocolVersion) bool {
	if left == nil || right == nil {
		return left == right
	}

	return *left == *right
}

func optionalBoolsEqual(left *bool, right *bool) bool {
	if left == nil || right == nil {
		return left == right
	}

	return *left == *right
}

func postgresTimestamp(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func decodeHash(encoded []byte) (history.Hash, error) {
	var decoded history.Hash
	if len(encoded) != len(decoded) {
		return decoded, ErrAuditHashInvalid
	}
	copy(decoded[:], encoded)

	return decoded, nil
}

const insertCommandSQL = `
INSERT INTO queue_control_commands (
    tenant_id, idempotency_key, command_id, actor, authentication_method,
    reason, action, required_capability, target_kind,
    target_name, requested_at, deadline, confirmed, selection_limit,
    replay_destination, replay_policy, scale_replicas, status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
    $17, $18
)
ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
`

const commandColumnsSQL = `
tenant_id, idempotency_key, command_id, actor, authentication_method, reason,
action, required_capability, target_kind,
target_name, requested_at, deadline, confirmed, selection_limit,
replay_destination, replay_policy, scale_replicas, status, failure_code,
capability_available, acknowledged_by, acknowledgement_protocol_major,
acknowledgement_protocol_minor,
dispatched_at, acknowledged_at, completed_at
`

const loadCommandSQL = `
SELECT ` + commandColumnsSQL + `
FROM queue_control_commands
WHERE tenant_id = $1 AND idempotency_key = $2
`

const loadCommandForUpdateSQL = loadCommandSQL + ` FOR UPDATE`

const completeCommandSQL = `
UPDATE queue_control_commands
SET status = $3, failure_code = $4,
    dispatched_at = $5, acknowledged_at = $6, completed_at = $7,
    capability_available = $8, acknowledged_by = $9,
    acknowledgement_protocol_major = $10,
    acknowledgement_protocol_minor = $11,
    updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $1 AND idempotency_key = $2 AND status = ANY($12)
`

const loadDesiredStateForUpdateSQL = `
SELECT tenant_id, target_kind, target_name, state, revision, command_id,
       changed_at
FROM queue_control_desired_states
WHERE tenant_id = $1 AND target_kind = $2 AND target_name = $3
FOR UPDATE
`

const upsertDesiredStateSQL = `
INSERT INTO queue_control_desired_states (
    tenant_id, target_kind, target_name, state, revision, command_id,
    changed_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (tenant_id, target_kind, target_name) DO UPDATE
SET state = EXCLUDED.state,
    revision = EXCLUDED.revision,
    command_id = EXCLUDED.command_id,
    changed_at = EXCLUDED.changed_at
WHERE queue_control_desired_states.revision < EXCLUDED.revision
`

const ensureAuditAnchorSQL = `
INSERT INTO queue_control_audit_anchors (
    tenant_id, sequence, hash, retained_through
) VALUES ($1, 0, $2, $3)
ON CONFLICT (tenant_id) DO NOTHING
`

const loadAuditHeadSQL = `
WITH locked_anchor AS MATERIALIZED (
    SELECT sequence, hash
    FROM queue_control_audit_anchors
    WHERE tenant_id = $1
    FOR UPDATE
), audit_head AS (
    SELECT sequence, hash
    FROM queue_control_audit_events
    WHERE tenant_id = $1
    UNION ALL
    SELECT sequence, hash FROM locked_anchor
)
SELECT sequence, hash
FROM audit_head
ORDER BY sequence DESC
LIMIT 1
`

const insertAuditEventSQL = `
INSERT INTO queue_control_audit_events (
    tenant_id, sequence, hash_version, command_id, idempotency_key,
    occurred_at, actor, action,
    target, result, previous_hash, hash
) OVERRIDING SYSTEM VALUE
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
`
