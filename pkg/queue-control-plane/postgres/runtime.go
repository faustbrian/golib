package postgres

import (
	"context"
	"errors"

	gopostgres "github.com/faustbrian/golib/pkg/postgres"
)

// ErrInvalidRuntimePool reports a missing or uninitialized PostgreSQL pool.
var ErrInvalidRuntimePool = errors.New("postgres: invalid runtime pool")

// Runtime is the complete PostgreSQL-backed control-plane service bundle.
// The caller retains ownership of the underlying pool.
type Runtime struct {
	Journal   *Journal
	Audit     *AuditStore
	Commands  *CommandStore
	Desired   *DesiredStore
	Readiness *PoolReadiness
}

// PoolReadiness adapts postgres bounded health checks to the API contract.
type PoolReadiness struct {
	pool *gopostgres.Pool
}

// Ready performs a bounded PostgreSQL ping through postgres.
func (readiness *PoolReadiness) Ready(ctx context.Context) error {
	return readiness.pool.Ping(ctx)
}

// NewRuntime wires all control-plane persistence services to one pool.
func NewRuntime(pool *gopostgres.Pool) (*Runtime, error) {
	if pool == nil || pool.Raw() == nil {
		return nil, ErrInvalidRuntimePool
	}
	raw := pool.Raw()

	return &Runtime{
		Journal:   newJournal(newPostgresTransactionRunner(raw)),
		Audit:     &AuditStore{beginner: raw},
		Commands:  &CommandStore{beginner: raw},
		Desired:   &DesiredStore{queryer: raw},
		Readiness: &PoolReadiness{pool: pool},
	}, nil
}
