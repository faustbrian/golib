package valkeystream

import (
	"context"
	"crypto/tls"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkey "github.com/valkey-io/valkey-go"
)

func TestNativeConnectionBufferBoundIsThirtyTwoKiB(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 32*1024, nativeConnectionBufferSize())
}

func TestNativeClientOptionsAreBoundedAndStandalone(t *testing.T) {
	opts, err := newOptions(
		WithAddress("valkey.internal:6380"),
		WithAuthentication("worker", "secret"),
		WithDB(4),
		WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS13}),
		WithClientName("queue-worker"),
		WithDialTimeout(2*time.Second),
		WithCommandTimeout(3*time.Second),
		WithBlockingPool(2, 8, time.Minute),
	)
	require.NoError(t, err)

	clientOpts := nativeClientOptions(opts)

	assert.Equal(t, []string{"valkey.internal:6380"}, clientOpts.InitAddress)
	assert.True(t, clientOpts.ForceSingleClient)
	assert.True(t, clientOpts.DisableCache)
	assert.True(t, clientOpts.DisableRetry)
	assert.True(t, clientOpts.AlwaysPipelining)
	assert.Equal(t, "worker", clientOpts.Username)
	assert.Equal(t, "secret", clientOpts.Password)
	assert.Equal(t, "queue-worker", clientOpts.ClientName)
	assert.Equal(t, 4, clientOpts.SelectDB)
	assert.Equal(t, 2*time.Second, clientOpts.Dialer.Timeout)
	assert.Equal(t, 3*time.Second, clientOpts.ConnWriteTimeout)
	assert.Equal(t, 2, clientOpts.BlockingPoolMinSize)
	assert.Equal(t, 8, clientOpts.BlockingPoolSize)
	assert.Equal(t, time.Minute, clientOpts.BlockingPoolCleanup)
	assert.Equal(t, 32*1024, clientOpts.ReadBufferEachConn)
	assert.Equal(t, 32*1024, clientOpts.WriteBufferEachConn)
	assert.Equal(t, 8, clientOpts.RingScaleEachConn)
	require.NotSame(t, opts.tlsConfig, clientOpts.TLSConfig)
}

func TestNativeTransportRunsStreamLifecycle(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Now().UTC()
	server.SetTime(now)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{server.Addr()},
		ForceSingleClient: true,
		DisableCache:      true,
		DisableRetry:      true,
		AlwaysPipelining:  true,
	})
	require.NoError(t, err)
	transport := newNativeTransport(client, 100, 1024)
	t.Cleanup(func() { require.NoError(t, transport.Close()) })
	ctx := context.Background()

	require.NoError(t, transport.EnsureGroup(ctx, "jobs", "workers"))
	require.NoError(t, transport.EnsureGroup(ctx, "jobs", "workers"))

	id, err := transport.Add(ctx, streamqueue.AddRequest{
		Stream: "jobs", MaxLength: 100, Body: []byte("payload"),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	deliveries, err := transport.Read(ctx, streamqueue.ReadRequest{
		Stream: "jobs", Group: "workers", Consumer: "worker-1",
		Count: 1, Block: 10 * time.Millisecond,
	})
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	assert.Equal(t, id, deliveries[0].ID)
	assert.Equal(t, []byte("payload"), deliveries[0].Body)
	assert.Equal(t, int64(1), deliveries[0].Attempts)
	assert.False(t, deliveries[0].Reclaimed)

	state, err := transport.GroupState(ctx, "jobs", "workers")
	require.NoError(t, err)
	// miniredis reports total entries as lag; real Valkey semantics are covered
	// by the Valkey 9 integration matrix.
	assert.Equal(t, streamqueue.GroupState{Pending: 1, Lag: 1, OldestPendingID: id}, state)

	server.SetTime(now.Add(2 * time.Second))
	claimed, err := transport.Claim(ctx, streamqueue.ClaimRequest{
		Stream: "jobs", Group: "workers", Consumer: "worker-2",
		MinIdle: time.Second, Start: "0-0", Count: 1,
	})
	require.NoError(t, err)
	require.Len(t, claimed.Deliveries, 1)
	assert.Equal(t, id, claimed.Deliveries[0].ID)
	assert.Equal(t, int64(2), claimed.Deliveries[0].Attempts)
	assert.True(t, claimed.Deliveries[0].Reclaimed)
	assert.NotEmpty(t, claimed.Next)
	server.SetTime(now.Add(4 * time.Second))
	claimed, err = transport.Claim(ctx, streamqueue.ClaimRequest{
		Stream: "jobs", Group: "workers", Consumer: "worker-3",
		MinIdle: time.Second, Start: "0-0", Count: 1,
	})
	require.NoError(t, err)
	require.Len(t, claimed.Deliveries, 1)
	assert.Equal(t, int64(3), claimed.Deliveries[0].Attempts)

	require.NoError(t, transport.Ack(ctx, streamqueue.AckRequest{
		Stream: "jobs", Group: "workers", ID: id,
	}))
	state, err = transport.GroupState(ctx, "jobs", "workers")
	require.NoError(t, err)
	assert.Zero(t, state.Pending)
}

func TestNativeTransportDeadLettersBeforeAcknowledging(t *testing.T) {
	server := miniredis.RunT(t)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{server.Addr()}, ForceSingleClient: true,
		DisableCache: true, DisableRetry: true, AlwaysPipelining: true,
	})
	require.NoError(t, err)
	transport := newNativeTransport(client, 100, 1024)
	t.Cleanup(func() { require.NoError(t, transport.Close()) })
	ctx := context.Background()

	require.NoError(t, transport.EnsureGroup(ctx, "jobs", "workers"))
	id, err := transport.Add(ctx, streamqueue.AddRequest{
		Stream: "jobs", MaxLength: 100, Body: []byte("poison"),
	})
	require.NoError(t, err)
	_, err = transport.Read(ctx, streamqueue.ReadRequest{
		Stream: "jobs", Group: "workers", Consumer: "worker",
		Count: 1, Block: time.Millisecond,
	})
	require.NoError(t, err)

	require.NoError(t, transport.DeadLetter(ctx, streamqueue.DeadLetterRequest{
		Source: "jobs", Destination: "jobs-dead", Group: "workers",
		Delivery: streamqueue.Delivery{ID: id, Body: []byte("poison"), Attempts: 5},
		Failure:  testFailureMetadata(),
	}))
	state, err := transport.GroupState(ctx, "jobs", "workers")
	require.NoError(t, err)
	assert.Zero(t, state.Pending)

	entries, err := client.Do(ctx, client.B().Xrange().Key("jobs-dead").Start("-").End("+").Build()).AsXRange()
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "poison", entries[0].FieldValues[streamBodyField])
	assert.Equal(t, id, entries[0].FieldValues[originalIDField])
	assert.Equal(t, "5", entries[0].FieldValues[deliveryAttemptsField])
}

func TestNativeTransportRecordRetentionIsIndependentAndExplicit(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{server.Addr()}, ForceSingleClient: true,
		DisableCache: true, DisableRetry: true, AlwaysPipelining: true,
	})
	require.NoError(t, err)
	transport := newNativeTransport(client, 1, 1024)
	t.Cleanup(func() { require.NoError(t, transport.Close()) })
	ctx := context.Background()
	appendRecords := func(stream string) {
		t.Helper()
		for index := range 3 {
			require.NoError(t, transport.RecordFailure(
				ctx, stream, "jobs", "workers", streamqueue.Delivery{
					ID: fmt.Sprintf("%d-0", index+1), Body: []byte("payload"), Attempts: 1,
				}, testFailureMetadata(),
			))
		}
	}
	appendRecords("unbounded-records")
	entries, err := client.Do(ctx, client.B().Xrange().Key("unbounded-records").
		Start("-").End("+").Build()).AsXRange()
	require.NoError(t, err)
	require.Len(t, entries, 3)

	transport.recordMaxLength = 2
	appendRecords("bounded-records")
	entries, err = client.Do(ctx, client.B().Xrange().Key("bounded-records").
		Start("-").End("+").Build()).AsXRange()
	require.NoError(t, err)
	require.Len(t, entries, 2)
}
