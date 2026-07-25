// Package idempotencyoutbox coordinates a transactional outbox insert with
// PostgreSQL idempotency completion in one caller-owned transaction.
package idempotencyoutbox

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/jackc/pgx/v5"
)

// Writer inserts an envelope through a caller-owned transaction. The
// outbox postgres.Writer satisfies Writer[outbox.Envelope].
type Writer[E any] interface {
	Insert(context.Context, pgx.Tx, E) error
}

// Completer conditionally persists idempotency completion in a caller-owned
// transaction. The idempotency postgres.Store satisfies this contract.
type Completer interface {
	CompleteTx(
		context.Context,
		pgx.Tx,
		idempotency.CompleteRequest,
	) (idempotency.Record, error)
}

// InsertAndComplete inserts envelope and then conditionally completes the
// idempotency record using tx. The caller must roll back on any returned error
// and remains responsible for committing a successful transaction.
func InsertAndComplete[E any](
	ctx context.Context,
	tx pgx.Tx,
	writer Writer[E],
	envelope E,
	completer Completer,
	request idempotency.CompleteRequest,
) (idempotency.Record, error) {
	if err := writer.Insert(ctx, tx, envelope); err != nil {
		return idempotency.Record{}, fmt.Errorf("insert outbox envelope: %w", err)
	}

	record, err := completer.CompleteTx(ctx, tx, request)
	if err != nil {
		return idempotency.Record{}, fmt.Errorf("complete idempotency record: %w", err)
	}

	return record, nil
}
