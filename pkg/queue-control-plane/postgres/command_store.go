package postgres

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	gopostgres "github.com/faustbrian/golib/pkg/postgres"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/jackc/pgx/v5"
)

const (
	// MaxCommandPageSize bounds one command-history database response.
	MaxCommandPageSize uint32 = 1_000
	// MaxCommandCursorBytes bounds opaque command-history pagination state.
	MaxCommandCursorBytes = 1_024
	// MaxCommandRetentionBatch bounds one terminal-command cleanup transaction.
	MaxCommandRetentionBatch uint32 = 1_000
)

var (
	// ErrInvalidCommandRequest reports an unbounded command lookup scope.
	ErrInvalidCommandRequest = errors.New("postgres: invalid command request")
	// ErrInvalidCommandState reports a malformed persisted command result.
	ErrInvalidCommandState = errors.New("postgres: invalid command state")
	// ErrInvalidCommandRetentionRequest reports an unsafe cleanup scope.
	ErrInvalidCommandRetentionRequest = errors.New("postgres: invalid command retention request")
	// ErrInvalidCommandRetentionState reports an impossible deletion count.
	ErrInvalidCommandRetentionState = errors.New("postgres: invalid command retention state")
)

// CommandRecord joins one immutable command envelope to its durable outcome.
type CommandRecord struct {
	Command controlplane.Command
	Result  controlplane.CommandResult
}

// CommandPage is one bounded newest-first tenant command-history page.
type CommandPage struct {
	Records    []CommandRecord
	NextCursor string
}

// CommandRetentionResult reports one bounded terminal-command cleanup batch.
type CommandRetentionResult struct {
	Deleted uint32
}

// CommandStore reads tenant-scoped durable command outcomes.
type CommandStore struct {
	beginner gopostgres.Beginner
}

// NewCommandStore creates a transaction-backed command result reader.
func NewCommandStore(beginner gopostgres.Beginner) (*CommandStore, error) {
	if beginner == nil {
		return nil, ErrNilBeginner
	}

	return &CommandStore{beginner: beginner}, nil
}

// Get returns one durable result by tenant and idempotency key.
func (s *CommandStore) Get(
	ctx context.Context,
	tenant string,
	key string,
) (controlplane.CommandResult, error) {
	if invalidCommandIdentity(tenant) || invalidCommandIdentity(key) {
		return controlplane.CommandResult{}, ErrInvalidCommandRequest
	}

	var result controlplane.CommandResult
	err := gopostgres.RunTransaction(
		ctx,
		s.beginner,
		gopostgres.TransactionOptions{TxOptions: pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		}},
		func(ctx context.Context, tx pgx.Tx) error {
			_, stored, err := (&sqlJournalTransaction{tx: tx}).loadCommand(
				ctx, loadCommandSQL, tenant, key,
			)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrCommandNotFound
			}
			if err != nil {
				return err
			}
			if err := stored.Validate(); err != nil {
				return fmt.Errorf("%w: %w", ErrInvalidCommandState, err)
			}
			result = stored

			return nil
		},
	)
	if err != nil {
		return controlplane.CommandResult{}, err
	}

	return result, nil
}

// ListTenant returns one newest-first tenant command-history page.
func (s *CommandStore) ListTenant(
	ctx context.Context,
	tenant string,
	cursor string,
	limit uint32,
) (CommandPage, error) {
	before, key, err := decodeCommandCursor(cursor)
	if invalidCommandIdentity(tenant) || err != nil || limit == 0 || limit > MaxCommandPageSize {
		return CommandPage{}, ErrInvalidCommandRequest
	}

	var page CommandPage
	err = gopostgres.RunTransaction(
		ctx,
		s.beginner,
		gopostgres.TransactionOptions{TxOptions: pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		}},
		func(ctx context.Context, tx pgx.Tx) error {
			records, err := loadCommandPage(ctx, tx, tenant, before, key, limit+1)
			if err != nil {
				return err
			}
			if len(records) > int(limit) {
				records = records[:limit]
				last := records[len(records)-1].Command
				page.NextCursor = encodeCommandCursor(last.RequestedAt, last.IdempotencyKey)
			}
			page.Records = records

			return nil
		},
	)
	if err != nil {
		return CommandPage{}, err
	}

	return page, nil
}

// RetainCommandsBefore deletes old terminal commands that no retained audit
// event or current desired state references.
func (s *CommandStore) RetainCommandsBefore(
	ctx context.Context,
	tenant string,
	cutoff time.Time,
	batchSize uint32,
) (CommandRetentionResult, error) {
	if invalidCommandIdentity(tenant) || cutoff.IsZero() ||
		batchSize == 0 || batchSize > MaxCommandRetentionBatch {
		return CommandRetentionResult{}, ErrInvalidCommandRetentionRequest
	}

	var result CommandRetentionResult
	err := gopostgres.RunTransaction(
		ctx,
		s.beginner,
		gopostgres.TransactionOptions{},
		func(ctx context.Context, tx pgx.Tx) error {
			tag, err := tx.Exec(
				ctx,
				retainCommandsSQL,
				tenant,
				postgresTimestamp(cutoff),
				int64(batchSize),
			)
			if err != nil {
				return fmt.Errorf("postgres: retain commands: %w", err)
			}
			deleted := tag.RowsAffected()
			if deleted < 0 || deleted > int64(batchSize) {
				return ErrInvalidCommandRetentionState
			}
			result.Deleted = uint32(deleted) //nolint:gosec // Checked against the bounded batch above.

			return nil
		},
	)
	if err != nil {
		return CommandRetentionResult{}, err
	}

	return result, nil
}

func loadCommandPage(
	ctx context.Context,
	queryer interface {
		Query(context.Context, string, ...any) (pgx.Rows, error)
	},
	tenant string,
	before any,
	key string,
	limit uint32,
) ([]CommandRecord, error) {
	rows, err := queryer.Query(ctx, listCommandsSQL, tenant, before, key, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("postgres: query command page: %w", err)
	}
	defer rows.Close()

	records := make([]CommandRecord, 0, limit)
	for rows.Next() {
		command, result, err := scanStoredCommand(rows)
		if err != nil {
			return nil, err
		}
		if err := command.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidCommandState, err)
		}
		if err := result.Validate(); err != nil ||
			result.TenantID != command.TenantID ||
			result.IdempotencyKey != command.IdempotencyKey {
			return nil, fmt.Errorf("%w: result does not match command", ErrInvalidCommandState)
		}
		records = append(records, CommandRecord{Command: command, Result: result})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: read command page: %w", err)
	}

	return records, nil
}

func decodeCommandCursor(cursor string) (any, string, error) {
	if cursor == "" {
		return nil, "", nil
	}
	if len(cursor) > MaxCommandCursorBytes {
		return nil, "", ErrInvalidCommandRequest
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, "", ErrInvalidCommandRequest
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 || invalidCommandIdentity(parts[1]) {
		return nil, "", ErrInvalidCommandRequest
	}
	microseconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, "", ErrInvalidCommandRequest
	}

	return time.UnixMicro(microseconds).UTC(), parts[1], nil
}

func encodeCommandCursor(requestedAt time.Time, key string) string {
	plain := strconv.FormatInt(requestedAt.UnixMicro(), 10) + ":" + key

	return base64.RawURLEncoding.EncodeToString([]byte(plain))
}

func invalidCommandIdentity(value string) bool {
	return strings.TrimSpace(value) == "" || len(value) > controlplane.MaxIdentityBytes
}

const listCommandsSQL = `
SELECT ` + commandColumnsSQL + `
FROM queue_control_commands
WHERE tenant_id = $1
  AND ($2::timestamptz IS NULL OR (requested_at, idempotency_key) < ($2, $3))
ORDER BY requested_at DESC, idempotency_key DESC
LIMIT $4
`

const retainCommandsSQL = `
WITH eligible AS MATERIALIZED (
    SELECT commands.tenant_id, commands.idempotency_key
    FROM queue_control_commands AS commands
    WHERE commands.tenant_id = $1
      AND commands.status <> 'accepted'
      AND commands.completed_at < $2
      AND NOT EXISTS (
          SELECT 1
          FROM queue_control_audit_events AS audit
          WHERE audit.tenant_id = commands.tenant_id
            AND audit.idempotency_key = commands.idempotency_key
      )
      AND NOT EXISTS (
          SELECT 1
          FROM queue_control_desired_states AS desired
          WHERE desired.tenant_id = commands.tenant_id
            AND desired.command_id = commands.command_id
      )
    ORDER BY completed_at, idempotency_key
    LIMIT $3
    FOR UPDATE OF commands
)
DELETE FROM queue_control_commands AS commands
USING eligible
WHERE commands.tenant_id = eligible.tenant_id
  AND commands.idempotency_key = eligible.idempotency_key
`
