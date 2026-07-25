package valkey

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/idempotency"
	"github.com/faustbrian/golib/pkg/idempotency/idempotencytest"
	valkeygo "github.com/valkey-io/valkey-go"
)

func TestResponseDropperWaitsForOperationWrite(t *testing.T) {
	connection := &dropResponseConn{Conn: &scriptedConn{
		reads: bytes.NewBufferString("+PONG\r\n+RESULT\r\n"),
	}}
	connection.dropNextOperationResponse()

	ping := []byte("*1\r\n$4\r\nPING\r\n")
	if _, err := connection.Write(ping); err != nil {
		t.Fatalf("Write(PING) error = %v", err)
	}
	eval := []byte("*2\r\n$4\r\nEVAL\r\n$8\r\nreturn 1\r\n")
	if _, err := connection.Write(eval); err != nil {
		t.Fatalf("Write(EVAL) error = %v", err)
	}

	buffer := make([]byte, len("+PONG\r\n"))
	if _, err := connection.Read(buffer); err != nil || string(buffer) != "+PONG\r\n" {
		t.Fatalf("Read(PING) = %q, %v", buffer, err)
	}
	buffer = make([]byte, len("+RESULT\r\n"))
	if _, err := connection.Read(buffer); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Read(EVAL) error = %v, want unexpected EOF", err)
	}
	if !connection.dropped.Load() {
		t.Fatal("operation response was not dropped")
	}
}

func TestValkeyUnknownResultsCanBeInspectedAfterReconnect(t *testing.T) {
	address := integrationAddress(t)
	dropper := &responseDropper{}
	client, err := valkeygo.NewClient(valkeygo.ClientOption{
		InitAddress:       []string{address},
		ForceSingleClient: true,
		PipelineMultiplex: -1,
		DisableRetry:      true,
		DialCtxFn:         dropper.dial,
	})
	if err != nil {
		t.Fatalf("valkey.NewClient() error = %v", err)
	}
	t.Cleanup(client.Close)
	store, err := Open(context.Background(), client, Options{
		Prefix: "idempotency-fault", Retention: time.Minute,
		OwnerTokens: idempotencytest.NewTokenSource("fault-owner").Next,
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	key, err := idempotency.NewKey("fault", "tenant", "acquire", "caller", t.Name())
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("unknown result"))
	if err != nil {
		t.Fatalf("NewFingerprint() error = %v", err)
	}
	storageKey := recordKey("idempotency-fault", key)
	t.Cleanup(func() {
		_ = client.Do(context.Background(), client.B().Del().Key(storageKey).Build()).Error()
	})
	warmFaultScripts(t, store, client)

	dropper.DropNextResponse(t)
	_, acquireErr := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if acquireErr == nil {
		t.Fatal("Acquire() error = nil after response loss")
	}
	dropper.RequireDropped(t, "Acquire")

	record, err := store.Inspect(context.Background(), key)
	if err != nil {
		t.Fatalf(
			"Inspect() after reconnect error = %v; Acquire() transport error = %v",
			err, acquireErr,
		)
	}
	if record.State != idempotency.StateAcquired || record.Attempt != 1 {
		t.Fatalf("Inspect() after reconnect = %#v", record)
	}
	if record.Fingerprint != fingerprint {
		t.Fatalf("Inspect() fingerprint = %#v, want %#v", record.Fingerprint, fingerprint)
	}

	dropper.DropNextResponse(t)
	_, completeErr := store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: record.Ownership(), Result: []byte("durable result"),
	})
	if completeErr == nil {
		t.Fatal("Complete() error = nil after response loss")
	}
	dropper.RequireDropped(t, "Complete")

	record, err = store.Inspect(context.Background(), key)
	if err != nil {
		t.Fatalf(
			"Inspect() completed record after reconnect error = %v; "+
				"Complete() transport error = %v",
			err, completeErr,
		)
	}
	if record.State != idempotency.StateCompleted || string(record.Result) != "durable result" {
		t.Fatalf("Inspect() completed record after reconnect = %#v", record)
	}
}

func warmFaultScripts(t *testing.T, store *Store, client valkeygo.Client) {
	t.Helper()
	key, err := idempotency.NewKey("fault", "tenant", "warm", "caller", t.Name())
	if err != nil {
		t.Fatalf("warm NewKey() error = %v", err)
	}
	fingerprint, err := idempotency.NewFingerprint("v1", []byte("warm scripts"))
	if err != nil {
		t.Fatalf("warm NewFingerprint() error = %v", err)
	}
	acquired, err := store.Acquire(context.Background(), idempotency.AcquireRequest{
		Key: key, Fingerprint: fingerprint, Lease: time.Minute,
	})
	if err != nil {
		t.Fatalf("warm Acquire() error = %v", err)
	}
	if _, err := store.Complete(context.Background(), idempotency.CompleteRequest{
		Ownership: acquired.Record.Ownership(), Result: []byte("warm"),
	}); err != nil {
		t.Fatalf("warm Complete() error = %v", err)
	}
	storageKey := recordKey("idempotency-fault", key)
	if err := client.Do(context.Background(), client.B().Del().Key(storageKey).Build()).Error(); err != nil {
		t.Fatalf("warm DEL error = %v", err)
	}
}

type responseDropper struct {
	mu      sync.Mutex
	current *dropResponseConn
}

func (d *responseDropper) dial(
	ctx context.Context,
	address string,
	dialer *net.Dialer,
	_ *tls.Config,
) (net.Conn, error) {
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	wrapper := &dropResponseConn{Conn: connection}
	d.mu.Lock()
	d.current = wrapper
	d.mu.Unlock()
	return wrapper, nil
}

func (d *responseDropper) DropNextResponse(t *testing.T) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil {
		t.Fatal("response dropper has no connection")
	}
	d.current.dropNextOperationResponse()
}

func (d *responseDropper) RequireDropped(t *testing.T, operation string) {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil ||
		!d.current.operationWritten.Load() ||
		!d.current.dropped.Load() {
		t.Fatalf("%s failed before the injected response-loss boundary", operation)
	}
}

type dropResponseConn struct {
	net.Conn
	armed                atomic.Bool
	dropped              atomic.Bool
	pending              atomic.Bool
	operationWritten     atomic.Bool
	pendingPingResponses atomic.Int64
}

func (c *dropResponseConn) dropNextOperationResponse() {
	c.armed.Store(false)
	c.dropped.Store(false)
	c.operationWritten.Store(false)
	c.pending.Store(true)
}

func (c *dropResponseConn) Write(buffer []byte) (int, error) {
	isPing := bytes.Equal(buffer, []byte("*1\r\n$4\r\nPING\r\n"))
	if isPing {
		c.pendingPingResponses.Add(1)
	}
	written, err := c.Conn.Write(buffer)
	if isPing && (err != nil || written != len(buffer)) {
		c.pendingPingResponses.Add(-1)
	}
	if written > 0 && isScriptOperation(buffer) && c.pending.CompareAndSwap(true, false) {
		c.operationWritten.Store(true)
		c.armed.Store(true)
	}

	return written, err
}

func isScriptOperation(buffer []byte) bool {
	return bytes.Contains(buffer, []byte("$4\r\nEVAL\r\n")) ||
		bytes.Contains(buffer, []byte("$7\r\nEVALSHA\r\n"))
}

func (c *dropResponseConn) Read(buffer []byte) (int, error) {
	read, err := c.Conn.Read(buffer)
	if read > 0 && c.consumePingResponse(buffer[:read]) {
		return read, err
	}
	if read > 0 && c.armed.CompareAndSwap(true, false) {
		c.dropped.Store(true)
		_ = c.Close()
		return 0, io.ErrUnexpectedEOF
	}
	return read, err
}

func (c *dropResponseConn) consumePingResponse(buffer []byte) bool {
	if !bytes.Equal(buffer, []byte("+PONG\r\n")) {
		return false
	}
	for {
		pending := c.pendingPingResponses.Load()
		if pending == 0 {
			return false
		}
		if c.pendingPingResponses.CompareAndSwap(pending, pending-1) {
			return true
		}
	}
}

type scriptedConn struct {
	reads *bytes.Buffer
}

func (c *scriptedConn) Read(buffer []byte) (int, error) {
	return c.reads.Read(buffer)
}

func (*scriptedConn) Write(buffer []byte) (int, error) {
	return len(buffer), nil
}

func (*scriptedConn) Close() error                     { return nil }
func (*scriptedConn) LocalAddr() net.Addr              { return nil }
func (*scriptedConn) RemoteAddr() net.Addr             { return nil }
func (*scriptedConn) SetDeadline(time.Time) error      { return nil }
func (*scriptedConn) SetReadDeadline(time.Time) error  { return nil }
func (*scriptedConn) SetWriteDeadline(time.Time) error { return nil }
