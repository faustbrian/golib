package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/outbox"
	"github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestWriterInsertsBatchInCallerTransaction(t *testing.T) {
	t.Parallel()

	writer, err := postgres.NewWriter(postgres.WriterConfig{Schema: "tenant", Table: "messages"})
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}

	now := time.Date(2026, time.July, 15, 7, 0, 0, 0, time.UTC)
	envelopes := []outbox.Envelope{
		{
			ID: "evt-1", Topic: "orders.created", Payload: []byte(`{"id":1}`),
			PayloadVersion: 1, Metadata: map[string]string{"b": "2", "a": "1"},
			OrderingKey: "customer-1", IdempotencyKey: "order-1", AvailableAt: now, CreatedAt: now,
		},
		{
			ID: "evt-2", Topic: "orders.created", Payload: []byte(`{"id":2}`),
			PayloadVersion: 1, AvailableAt: now.Add(time.Second), CreatedAt: now,
		},
	}

	var query string
	var arguments []any
	tx := &recordingTx{
		exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
			query = sql
			arguments = append([]any(nil), args...)

			return pgconn.NewCommandTag("INSERT 0 2"), nil
		},
	}

	if err := writer.InsertBatch(context.Background(), tx, envelopes); err != nil {
		t.Fatalf("insert batch: %v", err)
	}
	if !strings.Contains(query, `INSERT INTO "tenant"."messages"`) {
		t.Fatalf("query uses unexpected table: %s", query)
	}
	if strings.Count(query, "(") < 3 || len(arguments) != 20 {
		t.Fatalf("query/argument batch shape is wrong: %q (%d args)", query, len(arguments))
	}
	if arguments[4] != `{"a":"1","b":"2"}` {
		t.Fatalf("metadata JSON = %q, want canonical key order", arguments[4])
	}
}

func TestWriterRejectsEmptyBatchWithoutTouchingTransaction(t *testing.T) {
	t.Parallel()

	writer, err := postgres.NewWriter(postgres.WriterConfig{})
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}

	called := false
	tx := &recordingTx{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		called = true

		return pgconn.CommandTag{}, nil
	}}
	err = writer.InsertBatch(context.Background(), tx, nil)
	if !errors.Is(err, postgres.ErrEmptyBatch) {
		t.Fatalf("error = %v, want %v", err, postgres.ErrEmptyBatch)
	}
	if called {
		t.Fatal("empty batch touched transaction")
	}
}

func TestWriterRejectsOversizedPayloadWithoutTouchingTransaction(t *testing.T) {
	t.Parallel()

	writer, err := postgres.NewWriter(postgres.WriterConfig{})
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}

	called := false
	tx := &recordingTx{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		called = true

		return pgconn.CommandTag{}, nil
	}}
	limits := outbox.DefaultLimits()
	err = writer.Insert(context.Background(), tx, outbox.Envelope{
		ID: "evt-1", Topic: "topic", Payload: make([]byte, limits.MaxPayloadBytes+1),
		PayloadVersion: 1, AvailableAt: time.Now(), CreatedAt: time.Now(),
	})
	if !errors.Is(err, outbox.ErrPayloadTooLarge) {
		t.Fatalf("error = %v, want %v", err, outbox.ErrPayloadTooLarge)
	}
	if called {
		t.Fatal("oversized envelope touched transaction")
	}
}

func TestWriterRejectsSchemaOversizedEncodedMetadataWithoutSQL(t *testing.T) {
	t.Parallel()

	limits := outbox.DefaultLimits()
	limits.MaxMetadataBytes = 32 << 10
	writer, err := postgres.NewWriter(postgres.WriterConfig{Limits: limits})
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}

	called := false
	tx := &recordingTx{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		called = true

		return pgconn.CommandTag{}, nil
	}}
	envelope := outbox.Envelope{
		ID: "evt-1", Topic: "topic", PayloadVersion: 1,
		Metadata:    map[string]string{"key": strings.Repeat("\x01", 22<<10)},
		AvailableAt: time.Now(), CreatedAt: time.Now(),
	}
	if err := writer.Insert(context.Background(), tx, envelope); !errors.Is(err, postgres.ErrValueOutsideBounds) {
		t.Fatalf("error = %v, want %v", err, postgres.ErrValueOutsideBounds)
	}
	if called {
		t.Fatal("oversized encoded metadata touched transaction")
	}
}

func TestWriterRejectsOversizedIDWithoutTouchingTransaction(t *testing.T) {
	t.Parallel()

	writer, err := postgres.NewWriter(postgres.WriterConfig{})
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}

	called := false
	tx := &recordingTx{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		called = true

		return pgconn.CommandTag{}, nil
	}}
	err = writer.Insert(context.Background(), tx, outbox.Envelope{
		ID: strings.Repeat("x", 256), Topic: "topic", PayloadVersion: 1,
		AvailableAt: time.Now(), CreatedAt: time.Now(),
	})
	if !errors.Is(err, outbox.ErrIDTooLarge) {
		t.Fatalf("error = %v, want %v", err, outbox.ErrIDTooLarge)
	}
	if called {
		t.Fatal("oversized envelope touched transaction")
	}
}

func TestWriterRejectsOversizedBatchWithoutTouchingTransaction(t *testing.T) {
	t.Parallel()

	writer, err := postgres.NewWriter(postgres.WriterConfig{})
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}

	called := false
	tx := &recordingTx{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		called = true

		return pgconn.CommandTag{}, nil
	}}
	now := time.Now()
	envelopes := make([]outbox.Envelope, 101)
	for index := range envelopes {
		envelopes[index] = outbox.Envelope{
			ID: "evt-1", Topic: "topic", PayloadVersion: 1,
			AvailableAt: now, CreatedAt: now,
		}
	}
	if err := writer.InsertBatch(context.Background(), tx, envelopes); !errors.Is(err, postgres.ErrBatchTooLarge) {
		t.Fatalf("error = %v, want %v", err, postgres.ErrBatchTooLarge)
	}
	if called {
		t.Fatal("oversized batch touched transaction")
	}
}

func TestWriterPreservesTransactionFailure(t *testing.T) {
	t.Parallel()

	writer, err := postgres.NewWriter(postgres.WriterConfig{})
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	databaseErr := errors.New("transaction aborted")
	tx := &recordingTx{exec: func(context.Context, string, ...any) (pgconn.CommandTag, error) {
		return pgconn.CommandTag{}, databaseErr
	}}

	now := time.Now()
	err = writer.Insert(context.Background(), tx, outbox.Envelope{
		ID: "evt-1", Topic: "topic", PayloadVersion: 1,
		AvailableAt: now, CreatedAt: now,
	})
	if !errors.Is(err, databaseErr) {
		t.Fatalf("error = %v, want wrapped %v", err, databaseErr)
	}
}

func TestWriterRequiresCallerTransaction(t *testing.T) {
	t.Parallel()

	writer, err := postgres.NewWriter(postgres.WriterConfig{})
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}

	err = writer.Insert(context.Background(), nil, outbox.Envelope{ID: "evt-1", Topic: "topic"})
	if !errors.Is(err, postgres.ErrTransactionRequired) {
		t.Fatalf("error = %v, want %v", err, postgres.ErrTransactionRequired)
	}
}

func TestNewWriterRejectsInvalidLimits(t *testing.T) {
	t.Parallel()

	_, err := postgres.NewWriter(postgres.WriterConfig{
		Limits: outbox.Limits{MaxIDBytes: 1},
	})
	if !errors.Is(err, outbox.ErrInvalidLimits) {
		t.Fatalf("error = %v, want %v", err, outbox.ErrInvalidLimits)
	}
}

func TestNewWriterRejectsInvalidBatchLimit(t *testing.T) {
	t.Parallel()

	_, err := postgres.NewWriter(postgres.WriterConfig{MaxBatchSize: -1})
	if !errors.Is(err, postgres.ErrInvalidBatchLimit) {
		t.Fatalf("error = %v, want %v", err, postgres.ErrInvalidBatchLimit)
	}
}

type recordingTx struct {
	pgx.Tx
	exec func(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (tx *recordingTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return tx.exec(ctx, sql, arguments...)
}
