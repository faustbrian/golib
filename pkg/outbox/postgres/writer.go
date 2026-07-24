// Package postgres provides pgx-backed transactional persistence for outbox
// envelopes. Writer methods only accept pgx.Tx so callers cannot accidentally
// use a pool or connection and lose application-write atomicity.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/jackc/pgx/v5"
)

var (
	ErrEmptyBatch          = errors.New("outbox/postgres: batch is empty")
	ErrBatchTooLarge       = errors.New("outbox/postgres: batch exceeds configured limit")
	ErrInvalidBatchLimit   = errors.New("outbox/postgres: batch limit is invalid")
	ErrTransactionRequired = errors.New("outbox/postgres: caller transaction is required")
)

const (
	defaultMaxInsertBatch = 100
	maximumInsertBatch    = 6553
)

// WriterConfig selects the application-owned outbox table.
type WriterConfig struct {
	Schema       string
	Table        string
	Limits       outbox.Limits
	MaxBatchSize int
}

// Writer inserts envelopes through caller-owned pgx transactions.
type Writer struct {
	table        string
	limits       outbox.Limits
	maxBatchSize int
}

// NewWriter creates a transactional writer. Empty fields select the default
// public.outbox_messages table.
func NewWriter(config WriterConfig) (*Writer, error) {
	if config.Schema == "" {
		config.Schema = "public"
	}
	if config.Table == "" {
		config.Table = "outbox_messages"
	}
	if config.Limits == (outbox.Limits{}) {
		config.Limits = outbox.DefaultLimits()
	}
	if err := config.Limits.Validate(); err != nil {
		return nil, fmt.Errorf("outbox/postgres: validate writer limits: %w", err)
	}
	if config.MaxBatchSize == 0 {
		config.MaxBatchSize = defaultMaxInsertBatch
	}
	if config.MaxBatchSize < 0 || config.MaxBatchSize > maximumInsertBatch {
		return nil, ErrInvalidBatchLimit
	}

	return &Writer{
		table: sanitizeTable(config.Schema, config.Table), limits: config.Limits,
		maxBatchSize: config.MaxBatchSize,
	}, nil
}

func sanitizeTable(schema, table string) string {
	return pgx.Identifier{schema, table}.Sanitize()
}

// Insert adds one envelope to the caller's transaction.
func (w *Writer) Insert(ctx context.Context, tx pgx.Tx, envelope outbox.Envelope) error {
	return w.InsertBatch(ctx, tx, []outbox.Envelope{envelope})
}

// InsertBatch adds every envelope with one statement in the caller's
// transaction. PostgreSQL therefore accepts all records or none of them.
func (w *Writer) InsertBatch(ctx context.Context, tx pgx.Tx, envelopes []outbox.Envelope) error {
	if tx == nil {
		return ErrTransactionRequired
	}
	if len(envelopes) == 0 {
		return ErrEmptyBatch
	}
	if len(envelopes) > w.maxBatchSize {
		return ErrBatchTooLarge
	}
	for index, envelope := range envelopes {
		if err := envelope.ValidateForInsert(w.limits); err != nil {
			return fmt.Errorf("outbox/postgres: validate envelope %d: %w", index, err)
		}
		if err := validateEnvelopeForSchema(envelope); err != nil {
			return fmt.Errorf("outbox/postgres: validate envelope %d for schema: %w", index, err)
		}
	}

	query, arguments := w.insertQuery(envelopes)
	if _, err := tx.Exec(ctx, query, arguments...); err != nil {
		return fmt.Errorf("outbox/postgres: insert batch: %w", err)
	}

	return nil
}

func validateEnvelopeForSchema(envelope outbox.Envelope) error {
	if len(envelope.ID) > maxIdentifierBytes ||
		len(envelope.Topic) > maxIdentifierBytes ||
		len(envelope.OrderingKey) > maxIdentifierBytes ||
		len(envelope.IdempotencyKey) > maxIdentifierBytes ||
		len(envelope.Payload) > maxPayloadBytes {
		return ErrValueOutsideBounds
	}
	metadata := []byte("{}")
	if envelope.Metadata != nil {
		for key, value := range envelope.Metadata {
			if strings.ContainsRune(key, '\x00') || strings.ContainsRune(value, '\x00') {
				return ErrValueOutsideBounds
			}
		}
		metadata, _ = json.Marshal(envelope.Metadata)
	}
	if len(metadata) > maxEncodedMetadataBytes {
		return ErrValueOutsideBounds
	}

	return nil
}

func (w *Writer) insertQuery(envelopes []outbox.Envelope) (string, []any) {
	const columns = "(id, topic, payload, payload_version, metadata, ordering_key, " +
		"idempotency_key, attempts, available_at, created_at)"

	var query strings.Builder
	query.Grow(len(envelopes)*64 + len(w.table) + len(columns) + 20)
	query.WriteString("INSERT INTO ")
	query.WriteString(w.table)
	query.WriteByte(' ')
	query.WriteString(columns)
	query.WriteString(" VALUES ")

	arguments := make([]any, 0, len(envelopes)*10)
	placeholder := 1
	for index, envelope := range envelopes {
		metadata := []byte("{}")
		if envelope.Metadata != nil {
			metadata, _ = json.Marshal(envelope.Metadata)
		}
		if index > 0 {
			query.WriteByte(',')
		}
		query.WriteByte('(')
		for column := 0; column < 10; column++ {
			if column > 0 {
				query.WriteByte(',')
			}
			query.WriteByte('$')
			query.WriteString(strconv.Itoa(placeholder))
			placeholder++
		}
		query.WriteByte(')')

		arguments = append(arguments,
			envelope.ID,
			envelope.Topic,
			envelope.Payload,
			envelope.PayloadVersion,
			string(metadata),
			envelope.OrderingKey,
			envelope.IdempotencyKey,
			envelope.Attempts,
			envelope.AvailableAt,
			envelope.CreatedAt,
		)
	}

	return query.String(), arguments
}
