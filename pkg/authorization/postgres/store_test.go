package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	authorization "github.com/faustbrian/golib/pkg/authorization"
	"github.com/faustbrian/golib/pkg/authorization/policy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fakeDatabase struct {
	rows  []*fakeRow
	query string
	args  []any
}

func (database *fakeDatabase) queryRow(
	_ context.Context,
	query string,
	args ...any,
) row {
	database.query = query
	database.args = append([]any(nil), args...)
	result := database.rows[0]
	database.rows = database.rows[1:]
	return result
}

type fakeRow struct {
	encoded []byte
	err     error
}

func (result *fakeRow) Scan(destinations ...any) error {
	if result.err != nil {
		return result.err
	}
	*(destinations[0].(*[]byte)) = append([]byte(nil), result.encoded...)
	return nil
}

func TestStoreLoadAndUpdateManifest(t *testing.T) {
	t.Parallel()

	current := manifest(1)
	candidate := manifest(2)
	currentJSON, err := policy.Encode(current)
	if err != nil {
		t.Fatalf("policy.Encode() error = %v", err)
	}
	candidateJSON, err := policy.Encode(candidate)
	if err != nil {
		t.Fatalf("policy.Encode() error = %v", err)
	}
	database := &fakeDatabase{rows: []*fakeRow{
		{encoded: currentJSON},
		{encoded: candidateJSON},
	}}
	store := newStore(database)

	loaded, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Store.Load() error = %v", err)
	}
	if loaded.Revision != 1 {
		t.Errorf("Store.Load().Revision = %d, want 1", loaded.Revision)
	}
	if !strings.Contains(database.query, "authorization_policy_manifests") {
		t.Errorf("Store.Load() query = %q", database.query)
	}

	updated, err := store.Update(context.Background(), 1, candidate)
	if err != nil {
		t.Fatalf("Store.Update() error = %v", err)
	}
	if updated.Revision != 2 {
		t.Errorf("Store.Update().Revision = %d, want 2", updated.Revision)
	}
	if len(database.args) != 3 || database.args[0] != authorization.Revision(1) ||
		database.args[1] != authorization.Revision(2) || !json.Valid(database.args[2].([]byte)) {
		t.Errorf("Store.Update() args = %#v", database.args)
	}
	if !strings.Contains(database.query, "UPDATE authorization_policy_manifests") ||
		!strings.Contains(database.query, "WHERE $1 = 0") {
		t.Errorf("Store.Update() query does not guard initialization: %q", database.query)
	}
}

func TestStoreErrorsAreFailClosed(t *testing.T) {
	t.Parallel()

	databaseError := errors.New("database unavailable")
	tests := map[string]struct {
		row  *fakeRow
		want error
	}{
		"not initialized": {row: &fakeRow{err: pgx.ErrNoRows}, want: ErrNotInitialized},
		"database":        {row: &fakeRow{err: databaseError}, want: databaseError},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := newStore(&fakeDatabase{rows: []*fakeRow{tt.row}})
			_, err := store.Load(context.Background())
			if !errors.Is(err, tt.want) {
				t.Errorf("Store.Load() error = %v, want %v", err, tt.want)
			}
		})
	}

	conflict := newStore(&fakeDatabase{rows: []*fakeRow{{err: pgx.ErrNoRows}}})
	if _, err := conflict.Update(context.Background(), 1, manifest(2)); !errors.Is(err, ErrRevisionConflict) {
		t.Errorf("conflicting Store.Update() error = %v, want ErrRevisionConflict", err)
	}

	notMonotonic := newStore(&fakeDatabase{})
	if _, err := notMonotonic.Update(context.Background(), 2, manifest(2)); !errors.Is(err, ErrRevisionNotMonotonic) {
		t.Errorf("non-monotonic Store.Update() error = %v, want ErrRevisionNotMonotonic", err)
	}

	invalid := manifest(3)
	invalid.Format = "invalid"
	if _, err := notMonotonic.Update(context.Background(), 2, invalid); !errors.Is(err, policy.ErrInvalidManifest) {
		t.Errorf("invalid Store.Update() error = %v, want ErrInvalidManifest", err)
	}

	failed := newStore(&fakeDatabase{rows: []*fakeRow{{err: databaseError}}})
	if _, err := failed.Update(context.Background(), 1, manifest(2)); !errors.Is(err, databaseError) {
		t.Errorf("failed Store.Update() error = %v, want database error", err)
	}
}

func TestNewValidatesPool(t *testing.T) {
	t.Parallel()

	if _, err := New(nil); !errors.Is(err, ErrNilPool) {
		t.Errorf("New(nil) error = %v, want ErrNilPool", err)
	}
	if store, err := New(&pgxpool.Pool{}); err != nil || store == nil {
		t.Errorf("New(pool) = (%v, %v), want store", store, err)
	}
}

func TestSchemaMigrationAndGoMigration(t *testing.T) {
	t.Parallel()

	migration := SchemaMigration()
	if migration.Version != 1 || migration.Name == "" ||
		!strings.Contains(migration.Up, "authorization_policy_manifests") ||
		!strings.Contains(migration.Down, "DROP TABLE") {
		t.Errorf("SchemaMigration() = %+v", migration)
	}
	if _, err := GoMigration(); err != nil {
		t.Fatalf("GoMigration() error = %v", err)
	}
	corpus, err := os.ReadFile("testdata/schema-v1-up.sql")
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}
	if got := strings.TrimSuffix(string(corpus), "\n"); got != migration.Up {
		t.Errorf("schema compatibility corpus does not match SchemaMigration().Up")
	}
}

func manifest(revision authorization.Revision) policy.Manifest {
	return policy.Manifest{
		Format:    policy.FormatV1,
		Revision:  revision,
		Algorithm: policy.AlgorithmDenyOverrides,
		Policies:  []policy.Record{},
	}
}
