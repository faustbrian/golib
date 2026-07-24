// Package postgres persists complete policy manifests atomically in PostgreSQL.
package postgres

import (
	"context"
	"errors"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/policy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNilPool              = errors.New("authorization postgres pool is nil")
	ErrNotInitialized       = errors.New("authorization policy is not initialized")
	ErrRevisionConflict     = errors.New("authorization policy revision conflict")
	ErrRevisionNotMonotonic = errors.New("authorization policy revision is not monotonic")
)

const (
	loadManifestSQL = "SELECT manifest FROM authorization_policy_manifests " +
		"WHERE singleton = 1"
	updateManifestSQL = "WITH updated AS (" +
		"UPDATE authorization_policy_manifests SET revision = $2, " +
		"manifest = $3, updated_at = clock_timestamp() " +
		"WHERE singleton = 1 AND revision = $1 AND $2 > revision " +
		"RETURNING manifest), inserted AS (" +
		"INSERT INTO authorization_policy_manifests " +
		"(singleton, revision, manifest) SELECT 1, $2, $3 " +
		"WHERE $1 = 0 AND NOT EXISTS (SELECT 1 FROM " +
		"authorization_policy_manifests WHERE singleton = 1) " +
		"ON CONFLICT (singleton) DO NOTHING RETURNING manifest) " +
		"SELECT manifest FROM updated UNION ALL " +
		"SELECT manifest FROM inserted"
)

type row = pgx.Row

type database interface {
	queryRow(context.Context, string, ...any) row
}

type Store struct {
	queryRow func(context.Context, string, ...any) pgx.Row
}

func New(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, ErrNilPool
	}
	return &Store{queryRow: pool.QueryRow}, nil
}

func newStore(database database) *Store {
	return &Store{queryRow: database.queryRow}
}

func (store *Store) Load(ctx context.Context) (policy.Manifest, error) {
	var encoded []byte
	if err := store.queryRow(ctx, loadManifestSQL).Scan(&encoded); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return policy.Manifest{}, ErrNotInitialized
		}
		return policy.Manifest{}, err
	}
	return policy.Decode(encoded)
}

func (store *Store) Update(
	ctx context.Context,
	expected authorization.Revision,
	next policy.Manifest,
) (policy.Manifest, error) {
	encoded, err := policy.Encode(next)
	if err != nil {
		return policy.Manifest{}, err
	}
	if next.Revision <= expected {
		return policy.Manifest{}, ErrRevisionNotMonotonic
	}

	var stored []byte
	err = store.queryRow(
		ctx,
		updateManifestSQL,
		expected,
		next.Revision,
		encoded,
	).Scan(&stored)
	if errors.Is(err, pgx.ErrNoRows) {
		return policy.Manifest{}, ErrRevisionConflict
	}
	if err != nil {
		return policy.Manifest{}, err
	}
	return policy.Decode(stored)
}
