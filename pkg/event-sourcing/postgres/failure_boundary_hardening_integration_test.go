//go:build integration

package postgres_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type appendFailureResult struct {
	messages []eventsourcing.Message
	err      error
}

func TestPostgreSQLAppendBackendDeathDuringStatementIsNotCommitted(
	t *testing.T,
) {
	ctx, directPool := newDerivedIntegrationPool(t)
	const applicationName = "event-sourcing-kill-during-statement"
	faultConfig, err := pgxpool.ParseConfig(directPool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	faultConfig.ConnConfig.RuntimeParams["application_name"] = applicationName
	faultConfig.MaxConns = 1
	faultConfig.MinConns = 0
	faultConfig.MinIdleConns = 0
	faultPool, err := pgxpool.NewWithConfig(ctx, faultConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(faultPool.Close)
	faultStore, err := eventpostgres.New(faultPool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}

	blocker, err := directPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		defer cancel()
		_ = blocker.Rollback(cleanupCtx)
	}()
	var lastPosition int64
	if err := blocker.QueryRow(
		ctx,
		`SELECT last_position
		 FROM event_sourcing.positions
		 WHERE singleton = true
		 FOR UPDATE`,
	).Scan(&lastPosition); err != nil {
		t.Fatal(err)
	}
	if lastPosition != 0 {
		t.Fatalf("initial global position = %d", lastPosition)
	}

	stream := mustStream(t, "account", "kill-during-statement")
	pending := []eventsourcing.PendingMessage{
		mustPending(t, stream, "kill-during-statement-message", 1),
	}
	expected := eventsourcing.ExpectNewStream()
	result := make(chan appendFailureResult, 1)
	go func() {
		messages, appendErr := faultStore.Append(
			ctx,
			stream,
			expected,
			pending,
		)
		result <- appendFailureResult{messages: messages, err: appendErr}
	}()

	backendPID := waitForPostgreSQLApplicationLock(
		t,
		ctx,
		directPool,
		applicationName,
	)
	var terminated bool
	if err := directPool.QueryRow(
		ctx,
		"SELECT pg_terminate_backend($1)",
		backendPID,
	).Scan(&terminated); err != nil {
		t.Fatal(err)
	}
	if !terminated {
		t.Fatalf("PostgreSQL backend %d was not terminated", backendPID)
	}

	select {
	case failed := <-result:
		if failed.messages != nil || failed.err == nil ||
			eventsourcing.AppendCommitOutcome(failed.err) !=
				eventsourcing.CommitNotCommitted {
			t.Fatalf(
				"append killed during statement = %#v, %v",
				failed.messages,
				failed.err,
			)
		}
	case <-ctx.Done():
		t.Fatalf("append did not observe backend death: %v", ctx.Err())
	}
	if err := blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	messages, outcome, err := faultStore.ReconcileAppend(
		ctx,
		stream,
		expected,
		pending,
	)
	if messages != nil || outcome != eventsourcing.CommitNotCommitted || err != nil {
		t.Fatalf("reconciliation after backend death = %#v, %d, %v", messages, outcome, err)
	}
	retried, err := faultStore.Append(ctx, stream, expected, pending)
	if err != nil || len(retried) != 1 {
		t.Fatalf("retry after backend death = %#v, %v", retried, err)
	}
	position, exists := retried[0].GlobalPosition()
	if retried[0].StreamVersion() != 1 || !exists || position != 1 {
		t.Fatalf("retried message = %#v", retried[0])
	}
	var count int
	if err := directPool.QueryRow(
		ctx,
		`SELECT count(*)
		 FROM event_sourcing.messages
		 WHERE message_id = $1`,
		pending[0].ID().String(),
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("durable message count after retry = %d", count)
	}
}

func waitForPostgreSQLApplicationLock(
	t testing.TB,
	ctx context.Context,
	pool *pgxpool.Pool,
	applicationName string,
) int32 {
	t.Helper()

	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var lastErr error
	for {
		var backendPID int32
		lastErr = pool.QueryRow(
			deadline,
			`SELECT pid
			 FROM pg_stat_activity
			 WHERE application_name = $1
				AND state = 'active'
				AND wait_event_type = 'Lock'`,
			applicationName,
		).Scan(&backendPID)
		if lastErr == nil {
			return backendPID
		}
		select {
		case <-deadline.Done():
			t.Fatalf(
				"wait for blocked application %q: %v: %v",
				applicationName,
				deadline.Err(),
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

func TestPostgreSQLAppendReconcilesDroppedCommitResponseWithoutDuplicates(
	t *testing.T,
) {
	ctx, directPool := newDerivedIntegrationPool(t)
	directConfig := directPool.Config()
	upstream := net.JoinHostPort(
		directConfig.ConnConfig.Host,
		strconv.FormatUint(uint64(directConfig.ConnConfig.Port), 10),
	)
	proxy := newCommitResponseDropProxy(t, upstream)

	proxyConfig, err := pgxpool.ParseConfig(directPool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	proxyHost, proxyPortText, err := net.SplitHostPort(proxy.Address())
	if err != nil {
		t.Fatal(err)
	}
	proxyPort, err := strconv.ParseUint(proxyPortText, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	proxyConfig.ConnConfig.Host = proxyHost
	proxyConfig.ConnConfig.Port = uint16(proxyPort)
	proxyConfig.MaxConns = 1
	proxyConfig.MinConns = 0
	proxyConfig.MinIdleConns = 0
	proxyPool, err := pgxpool.NewWithConfig(ctx, proxyConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proxyPool.Close)
	proxyStore, err := eventpostgres.New(proxyPool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}

	stream := mustStream(t, "account", "dropped-commit-response")
	pending := []eventsourcing.PendingMessage{
		mustPending(t, stream, "dropped-commit-response-message", 1),
	}
	expected := eventsourcing.ExpectNewStream()
	messages, err := proxyStore.Append(ctx, stream, expected, pending)
	if messages != nil || err == nil ||
		eventsourcing.AppendCommitOutcome(err) != eventsourcing.CommitUnknown {
		t.Fatalf("append with dropped commit response = %#v, %v", messages, err)
	}
	if !proxy.DroppedCommitResponse() {
		t.Fatal("proxy did not drop a PostgreSQL commit response")
	}

	directStore, err := eventpostgres.New(directPool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for attempt := range 2 {
		persisted, outcome, reconcileErr := directStore.ReconcileAppend(
			ctx,
			stream,
			expected,
			pending,
		)
		if reconcileErr != nil || outcome != eventsourcing.CommitCommitted ||
			len(persisted) != 1 {
			t.Fatalf(
				"reconciliation %d = %#v, %d, %v",
				attempt,
				persisted,
				outcome,
				reconcileErr,
			)
		}
		if persisted[0].ID() != pending[0].ID() ||
			persisted[0].StreamVersion() != 1 {
			t.Fatalf("reconciled message %d = %#v", attempt, persisted[0])
		}
	}

	var messageCount, streamVersion, lastPosition int64
	if err := directPool.QueryRow(
		ctx,
		`SELECT count(*), max(stream_version), max(global_position)
		 FROM event_sourcing.messages
		 WHERE aggregate_type = $1 AND aggregate_id = $2`,
		stream.AggregateType(),
		stream.AggregateID(),
	).Scan(&messageCount, &streamVersion, &lastPosition); err != nil {
		t.Fatal(err)
	}
	if messageCount != 1 || streamVersion != 1 || lastPosition != 1 {
		t.Fatalf(
			"durable append state = %d/%d/%d",
			messageCount,
			streamVersion,
			lastPosition,
		)
	}
}

type commitResponseDropProxy struct {
	listener net.Listener
	upstream string

	armed   atomic.Bool
	dropped atomic.Bool
	closed  atomic.Bool

	connectionsMu sync.Mutex
	connections   map[net.Conn]struct{}
	wait          sync.WaitGroup
}

func newCommitResponseDropProxy(t testing.TB, upstream string) *commitResponseDropProxy {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxy := &commitResponseDropProxy{
		listener:    listener,
		upstream:    upstream,
		connections: make(map[net.Conn]struct{}),
	}
	proxy.wait.Add(1)
	go proxy.accept()
	t.Cleanup(proxy.Close)

	return proxy
}

func (proxy *commitResponseDropProxy) Address() string {
	return proxy.listener.Addr().String()
}

func (proxy *commitResponseDropProxy) DroppedCommitResponse() bool {
	return proxy.dropped.Load()
}

func (proxy *commitResponseDropProxy) Close() {
	if !proxy.closed.CompareAndSwap(false, true) {
		return
	}
	_ = proxy.listener.Close()
	proxy.connectionsMu.Lock()
	for connection := range proxy.connections {
		_ = connection.Close()
	}
	proxy.connectionsMu.Unlock()
	proxy.wait.Wait()
}

func (proxy *commitResponseDropProxy) accept() {
	defer proxy.wait.Done()
	for {
		client, err := proxy.listener.Accept()
		if err != nil {
			return
		}
		server, err := net.DialTimeout("tcp", proxy.upstream, 5*time.Second)
		if err != nil {
			_ = client.Close()
			continue
		}
		proxy.track(client, server)
		proxy.wait.Add(1)
		go proxy.forwardConnection(client, server)
	}
}

func (proxy *commitResponseDropProxy) forwardConnection(client, server net.Conn) {
	defer proxy.wait.Done()
	defer proxy.untrack(client, server)
	defer client.Close()
	defer server.Close()

	clientFinished := make(chan struct{})
	proxy.wait.Add(1)
	go func() {
		defer proxy.wait.Done()
		proxy.forwardClient(server, client)
		close(clientFinished)
	}()

	buffer := make([]byte, 32*1024)
	for {
		read, err := server.Read(buffer)
		if read > 0 {
			if proxy.armed.Load() {
				proxy.dropped.Store(true)
				return
			}
			if _, writeErr := client.Write(buffer[:read]); writeErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
		select {
		case <-clientFinished:
			return
		default:
		}
	}
}

func (proxy *commitResponseDropProxy) forwardClient(server, client net.Conn) {
	buffer := make([]byte, 32*1024)
	detector := postgreSQLCommitDetector{}
	for {
		read, err := client.Read(buffer)
		if read > 0 {
			if detector.Observe(buffer[:read]) {
				proxy.armed.Store(true)
			}
			if _, writeErr := server.Write(buffer[:read]); writeErr != nil {
				return
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				_ = server.SetDeadline(time.Now())
			}
			return
		}
	}
}

type postgreSQLCommitDetector struct {
	startupObserved bool
	buffer          []byte
}

func (detector *postgreSQLCommitDetector) Observe(data []byte) bool {
	const maximumFrameSize = 1024 * 1024
	if len(detector.buffer)+len(data) > maximumFrameSize {
		detector.buffer = detector.buffer[:0]
		return false
	}
	detector.buffer = append(detector.buffer, data...)
	if !detector.startupObserved {
		if len(detector.buffer) < 4 {
			return false
		}
		startupLength := int(binary.BigEndian.Uint32(detector.buffer[:4]))
		if startupLength < 8 || startupLength > maximumFrameSize ||
			len(detector.buffer) < startupLength {
			return false
		}
		detector.buffer = detector.buffer[startupLength:]
		detector.startupObserved = true
	}

	for len(detector.buffer) >= 5 {
		frameLength := int(binary.BigEndian.Uint32(detector.buffer[1:5]))
		if frameLength < 4 || frameLength > maximumFrameSize {
			detector.buffer = detector.buffer[:0]
			return false
		}
		frameEnd := 1 + frameLength
		if len(detector.buffer) < frameEnd {
			return false
		}
		messageType := detector.buffer[0]
		payload := detector.buffer[5:frameEnd]
		detector.buffer = detector.buffer[frameEnd:]
		if postgreSQLFrameCommits(messageType, payload) {
			return true
		}
	}

	return false
}

func postgreSQLFrameCommits(messageType byte, payload []byte) bool {
	var query []byte
	switch messageType {
	case 'Q':
		query = payload
	case 'P':
		statementEnd := bytesIndexByte(payload, 0)
		if statementEnd < 0 {
			return false
		}
		query = payload[statementEnd+1:]
	default:
		return false
	}
	queryEnd := bytesIndexByte(query, 0)
	if queryEnd >= 0 {
		query = query[:queryEnd]
	}

	return strings.EqualFold(strings.TrimSpace(string(query)), "commit")
}

func bytesIndexByte(value []byte, target byte) int {
	for index, current := range value {
		if current == target {
			return index
		}
	}

	return -1
}

func (proxy *commitResponseDropProxy) track(connections ...net.Conn) {
	proxy.connectionsMu.Lock()
	defer proxy.connectionsMu.Unlock()
	for _, connection := range connections {
		proxy.connections[connection] = struct{}{}
	}
}

func (proxy *commitResponseDropProxy) untrack(connections ...net.Conn) {
	proxy.connectionsMu.Lock()
	defer proxy.connectionsMu.Unlock()
	for _, connection := range connections {
		delete(proxy.connections, connection)
	}
}
