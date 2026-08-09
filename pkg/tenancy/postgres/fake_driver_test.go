package tenancypostgres_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const fakeDriverName = "tenancy-postgres-test"

var (
	registerFakeDriver sync.Once
	fakeDatabaseNumber atomic.Uint64
	fakeDatabases      sync.Map
)

type fakeDatabaseState struct {
	mutex             sync.Mutex
	data              map[string]map[string]string
	failures          map[string]error
	opened            int
	activeConnections int
	maximumActive     int
}

func newFakeDatabase(t *testing.T) (*sql.DB, *fakeDatabaseState) {
	t.Helper()
	registerFakeDriver.Do(func() { sql.Register(fakeDriverName, fakeDriver{}) })
	name := fmt.Sprintf("database-%d", fakeDatabaseNumber.Add(1))
	state := &fakeDatabaseState{data: make(map[string]map[string]string), failures: make(map[string]error)}
	fakeDatabases.Store(name, state)
	database, err := sql.Open(fakeDriverName, name)
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(32)
	t.Cleanup(func() {
		_ = database.Close()
		fakeDatabases.Delete(name)
	})
	return database, state
}

func (state *fakeDatabaseState) failNext(point string, err error) {
	state.mutex.Lock()
	state.failures[point] = err
	state.mutex.Unlock()
}

func (state *fakeDatabaseState) consumeFailure(point string) (error, bool) {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	err, exists := state.failures[point]
	delete(state.failures, point)
	return err, exists
}

func (state *fakeDatabaseState) openedConnections() int {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return state.opened
}

func (state *fakeDatabaseState) maximumConcurrentConnections() int {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return state.maximumActive
}

type fakeDriver struct{}

func (fakeDriver) Open(name string) (driver.Conn, error) {
	value, ok := fakeDatabases.Load(name)
	if !ok {
		return nil, errors.New("unknown fake database")
	}
	state := value.(*fakeDatabaseState)
	state.mutex.Lock()
	state.opened++
	state.activeConnections++
	state.maximumActive = max(state.maximumActive, state.activeConnections)
	state.mutex.Unlock()
	return &fakeConnection{state: state}, nil
}

type fakeConnection struct {
	state   *fakeDatabaseState
	mutex   sync.Mutex
	closed  bool
	active  bool
	session string
	local   string
	writes  map[string]string
}

func (connection *fakeConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepare unsupported")
}

func (connection *fakeConnection) Close() error {
	connection.mutex.Lock()
	if connection.closed {
		connection.mutex.Unlock()
		return nil
	}
	connection.closed = true
	connection.mutex.Unlock()
	connection.state.mutex.Lock()
	connection.state.activeConnections--
	connection.state.mutex.Unlock()
	return nil
}

func (connection *fakeConnection) Begin() (driver.Tx, error) {
	return connection.BeginTx(context.Background(), driver.TxOptions{})
}

func (connection *fakeConnection) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	if err, failed := connection.state.consumeFailure("begin"); failed {
		return nil, err
	}
	connection.mutex.Lock()
	defer connection.mutex.Unlock()
	if connection.active {
		return nil, errors.New("transaction already active")
	}
	connection.active = true
	connection.local = ""
	connection.writes = make(map[string]string)
	return &fakeTransaction{connection: connection}, nil
}

func (connection *fakeConnection) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if strings.Contains(query, "set_config") {
		if len(args) == 1 {
			if err, failed := connection.state.consumeFailure("session_reset"); failed {
				return nil, err
			}
			connection.mutex.Lock()
			connection.session = ""
			connection.mutex.Unlock()
			return driver.RowsAffected(1), nil
		}
		if err, failed := connection.state.consumeFailure("local_set"); failed {
			return nil, err
		}
		connection.mutex.Lock()
		connection.local = args[1].Value.(string)
		connection.mutex.Unlock()
		return driver.RowsAffected(1), nil
	}
	if query == "test_set_stale" {
		connection.mutex.Lock()
		connection.session = args[0].Value.(string)
		connection.mutex.Unlock()
		return driver.RowsAffected(1), nil
	}
	if query == "test_write" {
		connection.mutex.Lock()
		defer connection.mutex.Unlock()
		connection.writes[args[0].Value.(string)] = args[1].Value.(string)
		return driver.RowsAffected(1), nil
	}
	return nil, errors.New("unexpected exec")
}

func (connection *fakeConnection) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(query, "current_setting") {
		if err, failed := connection.state.consumeFailure("verify_error"); failed {
			return nil, err
		}
		connection.mutex.Lock()
		value := connection.session
		if connection.active {
			value = connection.local
		}
		connection.mutex.Unlock()
		if _, mismatch := connection.state.consumeFailure("verify_mismatch"); mismatch {
			value = "wrong-tenant"
		}
		return &fakeRows{values: [][]driver.Value{{value}}}, nil
	}
	if query == "test_read" {
		key := args[0].Value.(string)
		connection.mutex.Lock()
		tenant := connection.local
		value, pending := connection.writes[key]
		connection.mutex.Unlock()
		if pending {
			return &fakeRows{values: [][]driver.Value{{value}}}, nil
		}
		connection.state.mutex.Lock()
		value, exists := connection.state.data[tenant][key]
		connection.state.mutex.Unlock()
		if !exists {
			return &fakeRows{}, nil
		}
		return &fakeRows{values: [][]driver.Value{{value}}}, nil
	}
	return nil, errors.New("unexpected query")
}

func (connection *fakeConnection) CheckNamedValue(*driver.NamedValue) error { return nil }

type fakeTransaction struct {
	connection *fakeConnection
}

func (transaction *fakeTransaction) Commit() error {
	connection := transaction.connection
	if err, failed := connection.state.consumeFailure("commit"); failed {
		connection.finishTransaction(false)
		return err
	}
	connection.mutex.Lock()
	tenant := connection.local
	writes := connection.writes
	connection.mutex.Unlock()
	connection.state.mutex.Lock()
	if connection.state.data[tenant] == nil {
		connection.state.data[tenant] = make(map[string]string)
	}
	for key, value := range writes {
		connection.state.data[tenant][key] = value
	}
	connection.state.mutex.Unlock()
	connection.finishTransaction(true)
	return nil
}

func (transaction *fakeTransaction) Rollback() error {
	transaction.connection.finishTransaction(false)
	return nil
}

func (connection *fakeConnection) finishTransaction(_ bool) {
	connection.mutex.Lock()
	connection.active = false
	connection.local = ""
	connection.writes = nil
	connection.mutex.Unlock()
}

type fakeRows struct {
	values [][]driver.Value
	index  int
}

func (*fakeRows) Columns() []string { return []string{"value"} }
func (*fakeRows) Close() error      { return nil }

func (rows *fakeRows) Next(destination []driver.Value) error {
	if rows.index >= len(rows.values) {
		return io.EOF
	}
	copy(destination, rows.values[rows.index])
	rows.index++
	return nil
}
