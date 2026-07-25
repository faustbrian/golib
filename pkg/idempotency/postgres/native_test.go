package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/jackc/pgx/v5"
)

func TestNativeExecutorPropagatesEveryTransactionBoundary(t *testing.T) {
	backendErr := errors.New("postgres fault")
	now := time.Unix(1_700_000_000, 0).UTC()
	valid := codecRecord(t)
	encoded, err := encodeRecord(valid)
	if err != nil {
		t.Fatalf("encodeRecord() error = %v", err)
	}
	noWrite := func(time.Time, *idempotency.Record) (idempotency.Record, time.Time, bool, error) {
		return idempotency.Record{}, time.Time{}, false, nil
	}
	validWrite := func(time.Time, *idempotency.Record) (idempotency.Record, time.Time, bool, error) {
		return valid, now.Add(time.Hour), true, nil
	}
	tests := map[string]struct {
		database *fakeDatabase
		mutate   recordMutation
	}{
		"begin": {
			database: &fakeDatabase{beginErr: backendErr}, mutate: noWrite,
		},
		"lock and time": {
			database: databaseWithRows(errorRow(backendErr)), mutate: noWrite,
		},
		"select": {
			database: databaseWithRows(lockRow(now), errorRow(backendErr)), mutate: noWrite,
		},
		"expired delete": {
			database: databaseWithTransaction(&fakeTransaction{
				rows:       []pgx.Row{lockRow(now), persistedRow(encoded, now.Add(-time.Second))},
				execErrors: []error{backendErr},
			}),
			mutate: noWrite,
		},
		"mutation": {
			database: databaseWithRows(lockRow(now), errorRow(pgx.ErrNoRows)),
			mutate: func(time.Time, *idempotency.Record) (
				idempotency.Record, time.Time, bool, error,
			) {
				return idempotency.Record{}, time.Time{}, false, backendErr
			},
		},
		"encode": {
			database: databaseWithRows(lockRow(now), errorRow(pgx.ErrNoRows)),
			mutate: func(time.Time, *idempotency.Record) (
				idempotency.Record, time.Time, bool, error,
			) {
				return idempotency.Record{}, now.Add(time.Hour), true, nil
			},
		},
		"upsert": {
			database: databaseWithTransaction(&fakeTransaction{
				rows:       []pgx.Row{lockRow(now), errorRow(pgx.ErrNoRows)},
				execErrors: []error{backendErr},
			}),
			mutate: validWrite,
		},
		"commit": {
			database: databaseWithTransaction(&fakeTransaction{
				rows:      []pgx.Row{lockRow(now), errorRow(pgx.ErrNoRows)},
				commitErr: backendErr,
			}),
			mutate: noWrite,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			executor := &nativeExecutor{database: test.database}
			err := executor.withRecord(context.Background(), make([]byte, 32), test.mutate)
			if !errors.Is(err, backendErr) && name != "encode" {
				t.Fatalf("withRecord() error = %v", err)
			}
			if name == "encode" && err == nil {
				t.Fatal("withRecord() encode error = nil")
			}
			if test.database.transaction != nil && !test.database.transaction.rolledBack {
				t.Fatal("transaction was not rolled back")
			}
		})
	}
}

func TestNativeExecutorDeletesExpiredRecordBeforeFreshMutation(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	record := codecRecord(t)
	encoded, err := encodeRecord(record)
	if err != nil {
		t.Fatalf("encodeRecord() error = %v", err)
	}
	transaction := &fakeTransaction{
		rows: []pgx.Row{lockRow(now), persistedRow(encoded, now.Add(-time.Second))},
	}
	database := databaseWithTransaction(transaction)
	sawMissing := false
	err = (&nativeExecutor{database: database}).withRecord(
		context.Background(), make([]byte, 32),
		func(_ time.Time, current *idempotency.Record) (
			idempotency.Record, time.Time, bool, error,
		) {
			sawMissing = current == nil
			return idempotency.Record{}, time.Time{}, false, nil
		},
	)
	if err != nil || !sawMissing || len(transaction.execQueries) != 1 ||
		transaction.execQueries[0] != deleteRecordSQL || !transaction.committed {
		t.Fatalf(
			"withRecord() error = %v, missing = %t, exec = %#v, committed = %t",
			err, sawMissing, transaction.execQueries, transaction.committed,
		)
	}
}

func TestNativeExecutorUsesLockedBackendClock(t *testing.T) {
	backendNow := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	transaction := &fakeTransaction{
		rows: []pgx.Row{lockRow(backendNow), errorRow(pgx.ErrNoRows)},
	}
	var mutationNow time.Time
	err := (&nativeExecutor{database: databaseWithTransaction(transaction)}).withRecord(
		context.Background(), make([]byte, 32),
		func(now time.Time, _ *idempotency.Record) (
			idempotency.Record, time.Time, bool, error,
		) {
			mutationNow = now
			return idempotency.Record{}, time.Time{}, false, nil
		},
	)
	if err != nil {
		t.Fatalf("withRecord() error = %v", err)
	}
	if !mutationNow.Equal(backendNow) {
		t.Fatalf("mutation clock = %v, want backend clock %v", mutationNow, backendNow)
	}
	if len(transaction.queryQueries) == 0 ||
		transaction.queryQueries[0] != lockAndTimeSQL {
		t.Fatalf("first query = %#v, want lock-and-time query", transaction.queryQueries)
	}
}

func TestLoadRecordRejectsMalformedAndReturnsPersistedRecord(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	if _, _, err := loadRecord(
		context.Background(),
		&fakeTransaction{rows: []pgx.Row{persistedRow([]byte("bad"), now)}},
		make([]byte, 32),
	); err == nil {
		t.Fatal("loadRecord() malformed error = nil")
	}
	record := codecRecord(t)
	encoded, err := encodeRecord(record)
	if err != nil {
		t.Fatalf("encodeRecord() error = %v", err)
	}
	actual, purgeAt, err := loadRecord(
		context.Background(),
		&fakeTransaction{rows: []pgx.Row{persistedRow(encoded, now)}},
		make([]byte, 32),
	)
	if err != nil || actual == nil || actual.Key != record.Key || !purgeAt.Equal(now) {
		t.Fatalf("loadRecord() = %#v, %v, %v", actual, purgeAt, err)
	}
}

func TestNativeCleanupReturnsCountAndError(t *testing.T) {
	database := &fakeDatabase{row: scanRow(func(destinations ...any) error {
		*destinations[0].(*int64) = 7
		return nil
	})}
	count, err := (&nativeExecutor{database: database}).cleanup(context.Background(), 10)
	if err != nil || count != 7 {
		t.Fatalf("cleanup() = %d, %v", count, err)
	}
	backendErr := errors.New("cleanup failed")
	database.row = errorRow(backendErr)
	if _, err := (&nativeExecutor{database: database}).cleanup(
		context.Background(), 10,
	); !errors.Is(err, backendErr) {
		t.Fatalf("cleanup() error = %v", err)
	}
}

type fakeDatabase struct {
	transaction *fakeTransaction
	beginErr    error
	row         pgx.Row
}

func databaseWithRows(rows ...pgx.Row) *fakeDatabase {
	return databaseWithTransaction(&fakeTransaction{rows: rows})
}

func databaseWithTransaction(transaction *fakeTransaction) *fakeDatabase {
	return &fakeDatabase{transaction: transaction}
}

func (d *fakeDatabase) begin(context.Context) (nativeTransaction, error) {
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	return d.transaction, nil
}

func (d *fakeDatabase) queryRow(context.Context, string, ...any) pgx.Row {
	return d.row
}

type fakeTransaction struct {
	rows         []pgx.Row
	execErrors   []error
	execQueries  []string
	queryQueries []string
	commitErr    error
	committed    bool
	rolledBack   bool
}

func (t *fakeTransaction) queryRow(_ context.Context, query string, _ ...any) pgx.Row {
	t.queryQueries = append(t.queryQueries, query)
	row := t.rows[0]
	t.rows = t.rows[1:]
	return row
}

func (t *fakeTransaction) exec(_ context.Context, query string, _ ...any) error {
	t.execQueries = append(t.execQueries, query)
	if len(t.execErrors) == 0 {
		return nil
	}
	err := t.execErrors[0]
	t.execErrors = t.execErrors[1:]
	return err
}

func (t *fakeTransaction) commit(context.Context) error {
	t.committed = true
	return t.commitErr
}

func (t *fakeTransaction) rollback(context.Context) error {
	t.rolledBack = true
	return nil
}

type scanRow func(...any) error

func (r scanRow) Scan(destinations ...any) error {
	return r(destinations...)
}

func errorRow(err error) pgx.Row {
	return scanRow(func(...any) error { return err })
}

func lockRow(now time.Time) pgx.Row {
	return scanRow(func(destinations ...any) error {
		*destinations[0].(*any) = nil
		*destinations[1].(*time.Time) = now
		return nil
	})
}

func persistedRow(encoded []byte, purgeAt time.Time) pgx.Row {
	return scanRow(func(destinations ...any) error {
		*destinations[0].(*[]byte) = append([]byte(nil), encoded...)
		*destinations[1].(*time.Time) = purgeAt
		return nil
	})
}
