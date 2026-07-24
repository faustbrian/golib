package postgres

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresUnknownCommitCanBeInspectedAfterReconnect(t *testing.T) {
	basePool := integrationPool(t)
	baseStore, err := New(basePool, Options{
		Retention:   time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("fault-owner").Next,
	})
	if err != nil {
		t.Fatalf("New() base store error = %v", err)
	}
	key, fingerprint := storeIdentity(t, t.Name())
	acquired, err := baseStore.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	dropper := &postgresResponseDropper{}
	config := basePool.Config()
	config.ConnConfig.DialFunc = dropper.dial
	faultPool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(faultPool.Close)
	faultStore, err := New(faultPool, Options{
		Retention:   time.Hour,
		OwnerTokens: idempotencytest.NewTokenSource("unused-fault-owner").Next,
	})
	if err != nil {
		t.Fatalf("New() fault store error = %v", err)
	}
	tx, err := faultPool.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if _, err := faultStore.CompleteTx(
		context.Background(), tx, idempotency.CompleteRequest{
			Ownership: acquired.Record.Ownership(), Result: []byte("durable result"),
		},
	); err != nil {
		t.Fatalf("CompleteTx() error = %v", err)
	}
	dropper.DropNextResponse(t)
	if err := tx.Commit(context.Background()); err == nil {
		t.Fatal("Commit() error = nil after response loss")
	}

	record, err := baseStore.Inspect(context.Background(), key)
	if err != nil {
		t.Fatalf("Inspect() after reconnect error = %v", err)
	}
	if record.State != idempotency.StateCompleted ||
		string(record.Result) != "durable result" {
		t.Fatalf("Inspect() after reconnect = %#v", record)
	}
}

type postgresResponseDropper struct {
	mu      sync.Mutex
	current *postgresDropResponseConn
}

func (d *postgresResponseDropper) dial(
	ctx context.Context,
	network string,
	address string,
) (net.Conn, error) {
	connection, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	wrapper := &postgresDropResponseConn{Conn: connection}
	d.mu.Lock()
	d.current = wrapper
	d.mu.Unlock()
	return wrapper, nil
}

func (d *postgresResponseDropper) DropNextResponse(t *testing.T) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil {
		t.Fatal("response dropper has no connection")
	}
	d.current.armed.Store(true)
}

type postgresDropResponseConn struct {
	net.Conn
	armed atomic.Bool
}

func (c *postgresDropResponseConn) Read(buffer []byte) (int, error) {
	read, err := c.Conn.Read(buffer)
	if read > 0 && c.armed.CompareAndSwap(true, false) {
		_ = c.Close()
		return 0, io.ErrUnexpectedEOF
	}
	return read, err
}
