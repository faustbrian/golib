package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/audit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeRow struct{ scan func(...any) error }

func (row fakeRow) Scan(destinations ...any) error { return row.scan(destinations...) }

type fakeRows struct {
	pgx.Rows
	values  [][]byte
	index   int
	scanErr error
	scan    func(...any) error
	err     error
	closed  bool
}

func (rows *fakeRows) Next() bool { return rows.index < len(rows.values) }
func (rows *fakeRows) Scan(destinations ...any) error {
	if rows.scan != nil {
		rows.index++
		return rows.scan(destinations...)
	}
	if rows.scanErr != nil {
		return rows.scanErr
	}
	*(destinations[0].(*[]byte)) = append([]byte(nil), rows.values[rows.index]...)
	rows.index++
	return nil
}
func (rows *fakeRows) Err() error { return rows.err }
func (rows *fakeRows) Close()     { rows.closed = true }

type fakeTx struct {
	pgx.Tx
	rows           []pgx.Row
	rowIndex       int
	commitErr      error
	rollbackCalled bool
}

func (tx *fakeTx) QueryRow(context.Context, string, ...any) pgx.Row {
	row := tx.rows[tx.rowIndex]
	tx.rowIndex++
	return row
}
func (tx *fakeTx) Commit(context.Context) error   { return tx.commitErr }
func (tx *fakeTx) Rollback(context.Context) error { tx.rollbackCalled = true; return nil }

type fakeDatabase struct {
	tx        pgx.Tx
	beginErr  error
	rows      pgx.Rows
	queryErr  error
	row       pgx.Row
	querySQL  string
	queryArgs []any
}

func (database *fakeDatabase) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return database.tx, database.beginErr
}
func (database *fakeDatabase) Query(_ context.Context, sql string, arguments ...any) (pgx.Rows, error) {
	database.querySQL = sql
	database.queryArgs = arguments
	return database.rows, database.queryErr
}
func (database *fakeDatabase) QueryRow(context.Context, string, ...any) pgx.Row { return database.row }

func TestConfigAndTransactionWriterValidation(t *testing.T) {
	t.Parallel()

	badLimits := audit.DefaultLimits()
	badLimits.MaxFieldBytes = 0
	if _, err := New(nil, Config{Limits: badLimits}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("New(invalid limits) error = %v", err)
	}
	if _, err := New(nil, Config{MaxBatchRecords: audit.MaxAppendBatchRecords + 1}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("New(invalid batch) error = %v", err)
	}
	if writer, err := NewTx(&fakeTx{}, Config{MaxBatchRecords: audit.MaxAppendBatchRecords}); err != nil || writer.maxBatchRecords != audit.MaxAppendBatchRecords {
		t.Fatalf("NewTx(exact batch limit) = %#v, %v", writer, err)
	}
	if _, err := NewTx(nil, Config{}); !errors.Is(err, ErrTransactionRequired) {
		t.Fatalf("NewTx(nil) error = %v", err)
	}
	if _, err := NewTx(&fakeTx{}, Config{Limits: badLimits}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("NewTx(invalid limits) error = %v", err)
	}

	record := faultRecord(t, "record-1")
	var nilWriter *TxWriter
	if _, err := nilWriter.Stage(context.Background(), []audit.Record{record}); audit.AppendOutcomeOf(err) != audit.AppendRejected {
		t.Fatalf("nil Stage() error = %v", err)
	}
	writer := &TxWriter{tx: &fakeTx{}, limits: audit.DefaultLimits(), maxBatchRecords: 1}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := writer.Stage(nil, []audit.Record{record}); audit.AppendOutcomeOf(err) != audit.AppendRejected { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("nil-context Stage() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := writer.Stage(canceled, []audit.Record{record}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Stage() error = %v", err)
	}
	if _, err := writer.Stage(context.Background(), nil); !errors.Is(err, audit.ErrBatchTooLarge) {
		t.Fatalf("empty Stage() error = %v", err)
	}
	if _, err := writer.Stage(context.Background(), []audit.Record{{}}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("invalid-record Stage() error = %v", err)
	}

	statementFailure := errors.New("statement failed")
	writer.tx = &fakeTx{rows: []pgx.Row{fakeRow{scan: func(...any) error { return statementFailure }}}}
	if _, err := writer.Stage(context.Background(), []audit.Record{record}); !errors.Is(err, statementFailure) || audit.AppendOutcomeOf(err) != audit.AppendRejected {
		t.Fatalf("statement-failed Stage() error = %v", err)
	}
	writer.tx = &fakeTx{rows: []pgx.Row{appendStatusRow(1)}}
	result, err := writer.Stage(context.Background(), []audit.Record{record})
	if err != nil || len(result.Results) != 1 || result.Results[0].Status != audit.AppendAccepted {
		t.Fatalf("Stage() = %#v, %v", result, err)
	}
}

func FuzzConfigBatchBoundary(f *testing.F) {
	for _, value := range []int{-1, 0, 1, audit.MaxAppendBatchRecords, audit.MaxAppendBatchRecords + 1} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, maximum int) {
		writer, err := NewTx(&fakeTx{}, Config{MaxBatchRecords: maximum})
		valid := maximum == 0 || (maximum > 0 && maximum <= audit.MaxAppendBatchRecords)
		if valid && (err != nil || writer == nil) {
			t.Fatalf("valid MaxBatchRecords=%d: writer=%#v error=%v", maximum, writer, err)
		}
		if !valid && !errors.Is(err, audit.ErrInvalidArgument) {
			t.Fatalf("invalid MaxBatchRecords=%d error=%v", maximum, err)
		}
	})
}

func TestAppendFaultClassificationAndDuplicateReconciliation(t *testing.T) {
	t.Parallel()

	record := faultRecord(t, "record-1")
	store := &Store{limits: audit.DefaultLimits(), maxBatchRecords: 1}
	if _, err := store.Append(context.Background(), record); audit.AppendOutcomeOf(err) != audit.AppendRejected {
		t.Fatalf("nil-pool Append() error = %v", err)
	}
	store.pool = &fakeDatabase{}
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if _, err := store.Append(nil, record); audit.AppendOutcomeOf(err) != audit.AppendRejected { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("nil-context Append() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Append(canceled, record); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Append() error = %v", err)
	}
	if _, err := store.AppendBatch(context.Background(), nil); !errors.Is(err, audit.ErrBatchTooLarge) {
		t.Fatalf("empty AppendBatch() error = %v", err)
	}
	if _, err := store.Append(context.Background(), audit.Record{}); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("invalid-record Append() error = %v", err)
	}

	beginFailure := errors.New("begin failed")
	store.pool = &fakeDatabase{beginErr: beginFailure}
	if _, err := store.Append(context.Background(), record); !errors.Is(err, beginFailure) || audit.AppendOutcomeOf(err) != audit.AppendRejected {
		t.Fatalf("begin-failed Append() error = %v", err)
	}
	statementFailure := errors.New("insert failed")
	tx := &fakeTx{rows: []pgx.Row{fakeRow{scan: func(...any) error { return statementFailure }}}}
	store.pool = &fakeDatabase{tx: tx}
	if _, err := store.Append(context.Background(), record); !errors.Is(err, statementFailure) || !tx.rollbackCalled {
		t.Fatalf("insert-failed Append() error/rollback = %v, %t", err, tx.rollbackCalled)
	}
	for _, code := range []string{"40P01", "40001"} {
		transactionFailure := &pgconn.PgError{Code: code, Message: "transaction retry required"}
		tx = &fakeTx{rows: []pgx.Row{fakeRow{scan: func(...any) error { return transactionFailure }}}}
		store.pool = &fakeDatabase{tx: tx}
		if _, err := store.Append(context.Background(), record); !errors.Is(err, transactionFailure) ||
			audit.AppendOutcomeOf(err) != audit.AppendRejected || !tx.rollbackCalled {
			t.Fatalf("SQLSTATE %s Append() error/rollback = %v, %t", code, err, tx.rollbackCalled)
		}
	}
	commitFailure := errors.New("commit ambiguous")
	tx = &fakeTx{rows: []pgx.Row{appendStatusRow(1)}, commitErr: commitFailure}
	store.pool = &fakeDatabase{tx: tx}
	if _, err := store.Append(context.Background(), record); !errors.Is(err, commitFailure) || audit.AppendOutcomeOf(err) != audit.AppendUnknown {
		t.Fatalf("commit-failed Append() error = %v", err)
	}

	prepared, err := store.prepare([]audit.Record{record})
	if err != nil {
		t.Fatal(err)
	}
	tx = &fakeTx{rows: []pgx.Row{appendStatusRow(3)}}
	if _, err := insert(context.Background(), tx, prepared[0]); !errors.Is(err, audit.ErrDuplicateConflict) {
		t.Fatalf("conflicting insert() error = %v", err)
	}
	tx = &fakeTx{rows: []pgx.Row{appendStatusRow(2)}}
	if status, err := insert(context.Background(), tx, prepared[0]); err != nil || status != audit.AppendDuplicate {
		t.Fatalf("duplicate insert() = %v, %v", status, err)
	}
	tx = &fakeTx{rows: []pgx.Row{appendStatusRow(99)}}
	if _, err := insert(context.Background(), tx, prepared[0]); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("invalid append status error = %v", err)
	}
}

func TestQueryAndExportFaultsAreSafeAndBounded(t *testing.T) {
	t.Parallel()

	query, _ := audit.NewQuery(audit.QueryInput{Tenant: audit.AllTenants(), Limit: 1})
	var nilStore *Store
	if err := nilStore.Export(context.Background(), query, func(audit.Record) error { return nil }); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil-store Export() error = %v", err)
	}
	store := &Store{limits: audit.DefaultLimits(), maxBatchRecords: 1}
	if _, err := store.Query(context.Background(), query); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil-pool Query() error = %v", err)
	}
	if err := store.Export(context.Background(), query, func(audit.Record) error { return nil }); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil-pool Export() error = %v", err)
	}
	database := &fakeDatabase{queryErr: errors.New("password=secret")}
	store.pool = database
	//lint:ignore SA1012 Explicit nil-context validation is the contract under test.
	if err := store.Export(nil, query, func(audit.Record) error { return nil }); !errors.Is(err, audit.ErrInvalidArgument) { //nolint:staticcheck // Explicit nil-context validation is under test.
		t.Fatalf("nil-context Export() error = %v", err)
	}
	if err := store.Export(context.Background(), audit.Query{}, func(audit.Record) error { return nil }); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("invalid-query Export() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	database.queryErr = nil
	database.rows = &fakeRows{}
	if _, err := store.Query(canceled, query); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled empty Query() error = %v", err)
	}
	if err := store.Export(canceled, query, func(audit.Record) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled empty Export() error = %v", err)
	}
	database.queryErr = errors.New("password=secret")
	if _, err := store.Query(context.Background(), query); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("query-failed Query() error = %v", err)
	}
	if err := store.Export(context.Background(), query, nil); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("nil-callback Export() error = %v", err)
	}
	if err := store.Export(context.Background(), query, func(audit.Record) error { return nil }); err == nil {
		t.Fatal("query-failed Export() returned nil")
	}

	scanFailure := errors.New("scan failed")
	database.queryErr = nil
	database.rows = &fakeRows{values: [][]byte{{1}}, scanErr: scanFailure}
	if _, err := store.Query(context.Background(), query); !errors.Is(err, scanFailure) {
		t.Fatalf("scan-failed Query() error = %v", err)
	}
	database.rows = &fakeRows{values: [][]byte{[]byte("invalid")}}
	if _, err := store.Query(context.Background(), query); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("invalid-canonical Query() error = %v", err)
	}
	rowsFailure := errors.New("rows failed")
	database.rows = &fakeRows{err: rowsFailure}
	if _, err := store.Query(context.Background(), query); !errors.Is(err, rowsFailure) {
		t.Fatalf("rows-failed Query() error = %v", err)
	}

	record := faultRecord(t, "record-1")
	canonical, _ := audit.CanonicalJSON(record)
	database.rows = &fakeRows{values: [][]byte{canonical}}
	callbackFailure := errors.New("consumer failed")
	if err := store.Export(context.Background(), query, func(audit.Record) error { return callbackFailure }); !errors.Is(err, callbackFailure) {
		t.Fatalf("callback-failed Export() error = %v", err)
	}
	database.rows = &fakeRows{values: [][]byte{canonical}}
	if err := store.Export(context.Background(), query, func(audit.Record) error { return nil }); err != nil {
		t.Fatalf("successful Export() error = %v", err)
	}
	duringExport, cancelDuringExport := context.WithCancel(context.Background())
	database.rows = &fakeRows{values: [][]byte{canonical, canonical}, scan: func(destinations ...any) error {
		*(destinations[0].(*[]byte)) = append([]byte(nil), canonical...)
		cancelDuringExport()
		return nil
	}}
	if err := store.Export(duringExport, query, func(audit.Record) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled during Export() error = %v", err)
	}
	database.rows = &fakeRows{values: [][]byte{{1}}, scanErr: scanFailure}
	if err := store.Export(context.Background(), query, func(audit.Record) error { return nil }); !errors.Is(err, scanFailure) {
		t.Fatalf("scan-failed Export() error = %v", err)
	}
	database.rows = &fakeRows{values: [][]byte{[]byte("invalid")}}
	if err := store.Export(context.Background(), query, func(audit.Record) error { return nil }); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("invalid-canonical Export() error = %v", err)
	}
	database.rows = &fakeRows{err: rowsFailure}
	if err := store.Export(context.Background(), query, func(audit.Record) error { return nil }); !errors.Is(err, rowsFailure) {
		t.Fatalf("rows-failed Export() error = %v", err)
	}
	canceled, cancel = context.WithCancel(context.Background())
	cancel()
	database.rows = &fakeRows{values: [][]byte{canonical}}
	if err := store.Export(canceled, query, func(audit.Record) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Export() error = %v", err)
	}
}

func TestQueryBuilderIncludesEveryStableFilter(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	tenant, _ := audit.Tenant("tenant-1")
	cursor, _ := audit.NewCursor(base, "record-0")
	query, _ := audit.NewQuery(audit.QueryInput{
		Tenant: tenant, From: base, Through: base.Add(time.Hour), ActorID: "actor-1",
		SubjectType: "invoice", SubjectID: "invoice-1", Action: "invoice.viewed",
		CorrelationID: "correlation-1", Outcome: audit.OutcomeSucceeded, Limit: 1, After: cursor,
	})
	database := &fakeDatabase{rows: &fakeRows{}}
	store := &Store{pool: database, limits: audit.DefaultLimits(), maxBatchRecords: 1}
	if _, err := store.queryRows(context.Background(), audit.Query{}, 1); !errors.Is(err, audit.ErrInvalidArgument) {
		t.Fatalf("invalid-scope queryRows() error = %v", err)
	}
	if _, err := store.Query(context.Background(), query); err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"tenant_id", "actor_id", "subject_type", "subject_id", "action", "correlation_id", "outcome", "recorded_at >=", "recorded_at <=", "(recorded_at, record_id) >"} {
		if !strings.Contains(database.querySQL, fragment) {
			t.Fatalf("query missing %q: %s", fragment, database.querySQL)
		}
	}
	absent, _ := audit.NewQuery(audit.QueryInput{Tenant: audit.NoTenant(), Limit: 1})
	if _, err := store.Query(context.Background(), absent); err != nil || !strings.Contains(database.querySQL, "tenant_id IS NULL") {
		t.Fatalf("absent-tenant query = %q, %v", database.querySQL, err)
	}
}

func TestDatabaseErrorsNeverExposeDriverDiagnostics(t *testing.T) {
	t.Parallel()

	cause := errors.New("password=secret host=private")
	failure := &databaseError{operation: "query", cause: cause}
	if !errors.Is(failure, cause) || strings.Contains(failure.Error(), "secret") {
		t.Fatalf("databaseError = %v", failure)
	}
	if retentionKind(audit.RetentionHold) != "hold" || retentionKind(audit.RetentionRelease) != "release" {
		t.Fatal("retention kind mapping changed")
	}
	if Migrations() == nil {
		t.Fatal("Migrations() returned nil")
	}
}

func acceptedRow(id string) pgx.Row {
	return fakeRow{scan: func(destinations ...any) error {
		*(destinations[0].(*string)) = id
		return nil
	}}
}

func appendStatusRow(status int16) pgx.Row {
	return fakeRow{scan: func(destinations ...any) error {
		*(destinations[0].(*int16)) = status
		return nil
	}}
}

func faultRecord(t *testing.T, id string) audit.Record {
	t.Helper()
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	builder, err := audit.NewBuilder(audit.BuilderConfig{Clock: func() time.Time { return now }, IDGenerator: func() (string, error) { return id, nil }})
	if err != nil {
		t.Fatal(err)
	}
	record, err := builder.Build(audit.RecordInput{
		OccurredAt: now, Action: "invoice.viewed", Outcome: audit.OutcomeSucceeded,
		Actor:   audit.ActorInput{Kind: audit.ActorHuman, ID: "actor-1"},
		Subject: audit.SubjectInput{Type: "invoice", ID: "invoice-1"},
		Changes: audit.ChangeSetInput{NoChange: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}
