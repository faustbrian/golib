package consumerintegration_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

var databaseDriverSequence atomic.Uint64

func TestDatabaseSQLDependencyFailureAndRowsOwnership(t *testing.T) {
	t.Parallel()

	backend := &consumerSQLDriver{}
	driverName := fmt.Sprintf("breaker-consumer-%d", databaseDriverSequence.Add(1))
	sql.Register(driverName, backend)
	database, err := sql.Open(driverName, "consumer")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	failureCircuit := newConsumerCircuit(t, "database-failure", func(completion breaker.Completion) breaker.Outcome {
		if errors.Is(completion.Err, driver.ErrBadConn) {
			return breaker.OutcomeFailure
		}
		if completion.Err != nil {
			return breaker.OutcomeIgnored
		}

		return breaker.OutcomeSuccess
	})
	rows, err := breaker.Execute(context.Background(), failureCircuit, func(ctx context.Context) (*sql.Rows, error) {
		return database.QueryContext(ctx, "fail")
	})
	if rows != nil || !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("failed query = %#v, %v", rows, err)
	}
	if snapshot := failureCircuit.Snapshot(); snapshot.State != breaker.StateOpen ||
		snapshot.TotalFailures != 1 {
		t.Fatalf("failure snapshot = %+v", snapshot)
	}
	queries := backend.queries.Load()
	_, err = breaker.Execute(context.Background(), failureCircuit, func(ctx context.Context) (*sql.Rows, error) {
		return database.QueryContext(ctx, "fail")
	})
	if !errors.Is(err, breaker.ErrOpen) || backend.queries.Load() != queries {
		t.Fatalf("open rejection = %v, queries = %d", err, backend.queries.Load())
	}

	successCircuit := newConsumerCircuit(t, "database-success", nil)
	rows, err = breaker.Execute(context.Background(), successCircuit, func(ctx context.Context) (*sql.Rows, error) {
		return database.QueryContext(ctx, "select")
	})
	if err != nil {
		t.Fatalf("successful query: %v", err)
	}
	if backend.rowsClosed.Load() {
		t.Fatal("database rows closed before caller ownership")
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close rows: %v", err)
	}
	if !backend.rowsClosed.Load() {
		t.Fatal("caller close did not release database rows")
	}
	if snapshot := successCircuit.Snapshot(); snapshot.TotalSuccesses != 1 {
		t.Fatalf("success snapshot = %+v", snapshot)
	}
}

type consumerSQLDriver struct {
	queries    atomic.Int64
	rowsClosed atomic.Bool
}

func (backend *consumerSQLDriver) Open(string) (driver.Conn, error) {
	return &consumerSQLConn{backend: backend}, nil
}

type consumerSQLConn struct{ backend *consumerSQLDriver }

func (*consumerSQLConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*consumerSQLConn) Close() error                        { return nil }
func (*consumerSQLConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }

func (conn *consumerSQLConn) QueryContext(
	_ context.Context,
	query string,
	_ []driver.NamedValue,
) (driver.Rows, error) {
	conn.backend.queries.Add(1)
	if query == "fail" {
		return nil, driver.ErrBadConn
	}

	return &consumerSQLRows{closed: &conn.backend.rowsClosed}, nil
}

type consumerSQLRows struct{ closed *atomic.Bool }

func (*consumerSQLRows) Columns() []string         { return []string{"value"} }
func (rows *consumerSQLRows) Close() error         { rows.closed.Store(true); return nil }
func (*consumerSQLRows) Next([]driver.Value) error { return io.EOF }

func newConsumerCircuit(
	t *testing.T,
	name string,
	classifier breaker.Classifier,
) *breaker.Breaker {
	t.Helper()

	circuit, err := breaker.New(breaker.Config{
		Name:              name,
		MinimumThroughput: 1,
		Opening:           &breaker.OpeningRules{FailureCount: 1},
		OpenDuration:      breaker.FixedOpenDuration(time.Minute),
		Classifier:        classifier,
	})
	if err != nil {
		t.Fatalf("construct breaker: %v", err)
	}
	t.Cleanup(func() { _ = circuit.Shutdown(context.Background()) })

	return circuit
}
