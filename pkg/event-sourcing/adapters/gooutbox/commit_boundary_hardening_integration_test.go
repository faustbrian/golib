//go:build integration

package gooutbox_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
	"github.com/faustbrian/golib/pkg/event-sourcing/adapters/gooutbox"
	eventpostgres "github.com/faustbrian/golib/pkg/event-sourcing/postgres"
	outboxpostgres "github.com/faustbrian/golib/pkg/outbox/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDeferredConstraintFailureAtOuterCommitRollsBackBothRows(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	if _, err := pool.Exec(ctx, `
CREATE FUNCTION gooutbox_reject_outer_commit() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'injected outer commit failure' USING ERRCODE = '23514';
END
$$;
CREATE CONSTRAINT TRIGGER gooutbox_reject_outer_commit
AFTER INSERT ON outbox_messages
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW EXECUTE FUNCTION gooutbox_reject_outer_commit()`); err != nil {
		t.Fatal(err)
	}

	stream := commitBoundaryStream(t, "deferred-failure")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stager, _ := newCommitBoundaryStager(t, tx)
	messages, err := stager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{
			atomicityPending(t, stream, "deferred-commit-message"),
		},
	)
	if err != nil || len(messages) != 1 {
		t.Fatalf("Stage() = %#v, %v", messages, err)
	}
	assertCommitBoundaryTransactionCounts(t, ctx, tx, 1, 1)

	commitErr := tx.Commit(ctx)
	var postgresError *pgconn.PgError
	if !errors.As(commitErr, &postgresError) || postgresError.Code != "23514" {
		t.Fatalf("Commit() error = %v, want deferred constraint violation", commitErr)
	}
	assertStoredCounts(t, ctx, pool, 0, 0)
	assertAtomicityStreamAbsent(t, ctx, pool, stream)
}

func TestLostCommitResponseReconcilesDurablePairBeforeExactRetry(t *testing.T) {
	ctx, pool := newIntegrationPool(t)
	proxy := newCommitResponseDropProxy(t, pool)

	connectionConfig := pool.Config().ConnConfig.Copy()
	proxyHost, proxyPortText, err := net.SplitHostPort(proxy.address())
	if err != nil {
		t.Fatal(err)
	}
	proxyPort, err := strconv.ParseUint(proxyPortText, 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	connectionConfig.Host = proxyHost
	connectionConfig.Port = uint16(proxyPort)
	connection, err := pgx.ConnectConfig(ctx, connectionConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = connection.Close(context.Background())
	})

	stream := commitBoundaryStream(t, "lost-response")
	pending := atomicityPending(t, stream, "lost-response-message")
	tx, err := connection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stager, codec := newCommitBoundaryStager(t, tx)
	messages, err := stager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	)
	if err != nil || len(messages) != 1 {
		t.Fatalf("Stage() = %#v, %v", messages, err)
	}
	expectedEnvelope, err := codec.Encode(messages[0])
	if err != nil {
		t.Fatal(err)
	}

	proxy.dropNextCommitResponse()
	if commitErr := tx.Commit(ctx); commitErr == nil {
		t.Fatal("Commit() succeeded despite a withheld PostgreSQL response")
	}
	select {
	case <-proxy.commitDurable():
	case <-ctx.Done():
		t.Fatalf("wait for durable PostgreSQL commit: %v", ctx.Err())
	}

	messageID := messages[0].ID().String()
	assertCommitBoundaryIdentityPair(t, ctx, pool, messageID)
	eventStore, err := eventpostgres.New(pool, eventpostgres.Config{})
	if err != nil {
		t.Fatal(err)
	}
	readOptions, err := eventsourcing.NewReadStreamOptions(
		eventsourcing.ReadStreamOptionsInput{FromVersion: 1, Limit: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	iterator, err := eventStore.ReadStream(ctx, stream, readOptions)
	assertSingleReadMessage(t, ctx, iterator, err, messages[0])
	actualEnvelope := loadRecoveryEnvelope(t, ctx, pool, messageID)
	actualEnvelope.AvailableAt = actualEnvelope.AvailableAt.UTC()
	actualEnvelope.CreatedAt = actualEnvelope.CreatedAt.UTC()
	if !bytes.Equal(
		actualEnvelope.CanonicalJSON(),
		expectedEnvelope.CanonicalJSON(),
	) {
		t.Fatalf(
			"reconciled envelope = %s, want %s",
			actualEnvelope.CanonicalJSON(),
			expectedEnvelope.CanonicalJSON(),
		)
	}

	retryTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	retryStager, _ := newCommitBoundaryStager(t, retryTx)
	if _, retryErr := retryStager.Stage(
		ctx,
		stream,
		eventsourcing.ExpectNewStream(),
		[]eventsourcing.PendingMessage{pending},
	); retryErr == nil ||
		eventsourcing.AppendCommitOutcome(retryErr) !=
			eventsourcing.CommitNotCommitted {
		t.Fatalf("exact retry Stage() error = %v", retryErr)
	}
	if err := retryTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assertStoredCounts(t, ctx, pool, 1, 1)
	assertCommitBoundaryIdentityPair(t, ctx, pool, messageID)
}

func newCommitBoundaryStager(
	t *testing.T,
	tx pgx.Tx,
) (*gooutbox.Stager, *gooutbox.EnvelopeCodec) {
	t.Helper()

	writer, err := outboxpostgres.NewWriter(outboxpostgres.WriterConfig{
		Limits:       gooutbox.DefaultLimits(),
		MaxBatchSize: eventsourcing.MaxAppendMessages,
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := gooutbox.NewEnvelopeCodec(
		gooutbox.FixedTopic("commit-boundary-events"),
		gooutbox.DefaultLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	stager, err := gooutbox.NewStager(
		tx,
		eventpostgres.Config{},
		writer,
		codec,
	)
	if err != nil {
		t.Fatal(err)
	}

	return stager, codec
}

func commitBoundaryStream(
	t *testing.T,
	aggregateID string,
) eventsourcing.StreamID {
	t.Helper()

	stream, err := eventsourcing.NewStreamID("commit-boundary", aggregateID)
	if err != nil {
		t.Fatal(err)
	}

	return stream
}

func assertCommitBoundaryTransactionCounts(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	wantEvents int,
	wantEnvelopes int,
) {
	t.Helper()

	var events, envelopes int
	if err := tx.QueryRow(
		ctx,
		"SELECT count(*) FROM event_sourcing.messages",
	).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(
		ctx,
		"SELECT count(*) FROM outbox_messages",
	).Scan(&envelopes); err != nil {
		t.Fatal(err)
	}
	if events != wantEvents || envelopes != wantEnvelopes {
		t.Fatalf(
			"transactional counts = (%d, %d), want (%d, %d)",
			events,
			envelopes,
			wantEvents,
			wantEnvelopes,
		)
	}
}

func assertCommitBoundaryIdentityPair(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	messageID string,
) {
	t.Helper()

	var pairs int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
  FROM event_sourcing.messages AS events
  JOIN outbox_messages AS envelopes
    ON envelopes.id = events.message_id
   AND envelopes.idempotency_key = events.message_id
 WHERE events.message_id = $1`, messageID).Scan(&pairs); err != nil {
		t.Fatal(err)
	}
	if pairs != 1 {
		t.Fatalf("event/outbox identity pairs = %d, want 1", pairs)
	}
}

type commitResponseDropProxy struct {
	listener net.Listener
	upstream string
	armed    atomic.Bool
	durable  chan struct{}
	done     chan error
}

func newCommitResponseDropProxy(
	t *testing.T,
	pool *pgxpool.Pool,
) *commitResponseDropProxy {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	direct := pool.Config().ConnConfig
	proxy := &commitResponseDropProxy{
		listener: listener,
		upstream: net.JoinHostPort(direct.Host, strconv.Itoa(int(direct.Port))),
		durable:  make(chan struct{}),
		done:     make(chan error, 1),
	}
	go func() {
		proxy.done <- proxy.serve()
	}()
	t.Cleanup(func() {
		_ = proxy.listener.Close()
		select {
		case serveErr := <-proxy.done:
			if serveErr != nil {
				t.Errorf("commit response proxy: %v", serveErr)
			}
		case <-time.After(5 * time.Second):
			t.Error("commit response proxy did not stop")
		}
	})

	return proxy
}

func (proxy *commitResponseDropProxy) address() string {
	return proxy.listener.Addr().String()
}

func (proxy *commitResponseDropProxy) dropNextCommitResponse() {
	proxy.armed.Store(true)
}

func (proxy *commitResponseDropProxy) commitDurable() <-chan struct{} {
	return proxy.durable
}

func (proxy *commitResponseDropProxy) serve() error {
	downstream, err := proxy.listener.Accept()
	if err != nil {
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	}
	defer downstream.Close()
	_ = proxy.listener.Close()

	upstream, err := net.Dial("tcp", proxy.upstream)
	if err != nil {
		return err
	}
	defer upstream.Close()
	frontendDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(upstream, downstream)
		close(frontendDone)
	}()

	reader := bufio.NewReader(upstream)
	for {
		messageType, payload, readErr := readPostgresBackendMessage(reader)
		if readErr != nil {
			if errors.Is(readErr, io.EOF) || errors.Is(readErr, net.ErrClosed) {
				return nil
			}
			return readErr
		}
		if proxy.armed.Load() && messageType == 'C' &&
			bytes.Equal(payload, []byte("COMMIT\x00")) {
			close(proxy.durable)
			_ = downstream.Close()
			_ = upstream.Close()
			<-frontendDone

			return nil
		}
		if err := writePostgresBackendMessage(
			downstream,
			messageType,
			payload,
		); err != nil {
			return err
		}
	}
}

func readPostgresBackendMessage(
	reader *bufio.Reader,
) (byte, []byte, error) {
	messageType, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	var lengthBytes [4]byte
	if _, err := io.ReadFull(reader, lengthBytes[:]); err != nil {
		return 0, nil, err
	}
	length := binary.BigEndian.Uint32(lengthBytes[:])
	if length < 4 || length > 64<<20 {
		return 0, nil, fmt.Errorf("invalid PostgreSQL message length %d", length)
	}
	payload := make([]byte, int(length)-4)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}

	return messageType, payload, nil
}

func writePostgresBackendMessage(
	writer io.Writer,
	messageType byte,
	payload []byte,
) error {
	var lengthBytes [4]byte
	binary.BigEndian.PutUint32(lengthBytes[:], uint32(len(payload)+4))
	if _, err := writer.Write([]byte{messageType}); err != nil {
		return err
	}
	if _, err := writer.Write(lengthBytes[:]); err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}

	return nil
}
