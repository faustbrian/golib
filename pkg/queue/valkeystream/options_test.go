package valkeystream

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionsUseBoundedStandaloneDefaults(t *testing.T) {
	opts, err := newOptions(WithAddress("127.0.0.1:6379"))

	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:6379", opts.address)
	assert.Equal(t, "go-queue", opts.clientName)
	assert.Equal(t, "golang-queue", opts.stream)
	assert.Equal(t, "golang-queue", opts.group)
	assert.NotEmpty(t, opts.consumer)
	assert.Positive(t, opts.dialTimeout)
	assert.Positive(t, opts.commandTimeout)
	assert.Positive(t, opts.requestTimeout)
	assert.Positive(t, opts.blockTime)
	assert.Positive(t, opts.shutdownTimeout)
	assert.Positive(t, opts.blockingPoolSize)
	assert.Positive(t, opts.readBatchSize)
	assert.Positive(t, opts.reclaimBatchSize)
	assert.Positive(t, opts.reclaimMinIdle)
	assert.Positive(t, opts.reclaimInterval)
	assert.Positive(t, opts.maxDeliveryAttempts)
	assert.NotNil(t, opts.logger)
	assert.NoError(t, opts.runFunc(context.Background(), nil))
}

func TestOptionsApplyExplicitConfiguration(t *testing.T) {
	logger := queue.NewEmptyLogger()
	runErr := errors.New("run")
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "valkey.internal"}
	opts, err := newOptions(
		WithAddress("valkey.internal:6380"),
		WithAuthentication("worker", "secret"),
		WithDB(4),
		WithTLSConfig(tlsConfig),
		WithClientName("queue-worker"),
		WithDialTimeout(2*time.Second),
		WithCommandTimeout(3*time.Second),
		WithRequestTimeout(4*time.Second),
		WithBlockTime(500*time.Millisecond),
		WithShutdownTimeout(5*time.Second),
		WithBlockingPool(2, 8, time.Minute),
		WithStreamName("jobs"),
		WithGroup("workers"),
		WithConsumer("worker-1"),
		WithMaxLength(1_000),
		WithRecordRetention(500),
		WithReadBatchSize(16),
		WithReclaim(10*time.Second, time.Second, 32),
		WithFailureStream("jobs-failures"),
		WithDeadLetter("jobs-dead", 7),
		WithReplayDestinations("archive", "quarantine"),
		WithLogger(logger),
		WithRunFunc(func(context.Context, core.TaskMessage) error { return runErr }),
	)

	require.NoError(t, err)
	assert.Equal(t, "worker", opts.username)
	assert.Equal(t, "secret", opts.password)
	assert.Equal(t, 4, opts.db)
	assert.Equal(t, "queue-worker", opts.clientName)
	assert.Equal(t, 2*time.Second, opts.dialTimeout)
	assert.Equal(t, 3*time.Second, opts.commandTimeout)
	assert.Equal(t, 4*time.Second, opts.requestTimeout)
	assert.Equal(t, 500*time.Millisecond, opts.blockTime)
	assert.Equal(t, 5*time.Second, opts.shutdownTimeout)
	assert.Equal(t, 2, opts.blockingPoolMinSize)
	assert.Equal(t, 8, opts.blockingPoolSize)
	assert.Equal(t, time.Minute, opts.blockingPoolCleanup)
	assert.Equal(t, "jobs", opts.stream)
	assert.Equal(t, "workers", opts.group)
	assert.Equal(t, "worker-1", opts.consumer)
	assert.Equal(t, int64(1_000), opts.maxLength)
	assert.Equal(t, int64(500), opts.recordMaxLength)
	assert.Equal(t, 16, opts.readBatchSize)
	assert.Equal(t, 10*time.Second, opts.reclaimMinIdle)
	assert.Equal(t, time.Second, opts.reclaimInterval)
	assert.Equal(t, 32, opts.reclaimBatchSize)
	assert.Equal(t, "jobs-failures", opts.failureStream)
	assert.Equal(t, "jobs-dead", opts.deadLetterStream)
	assert.Equal(t, int64(7), opts.maxDeliveryAttempts)
	assert.Equal(t, map[string]struct{}{
		"archive": {}, "quarantine": {},
	}, opts.replayDestinations)
	assert.Equal(t, logger, opts.logger)
	assert.ErrorIs(t, opts.runFunc(context.Background(), nil), runErr)

	require.NotSame(t, tlsConfig, opts.tlsConfig)
	tlsConfig.ServerName = "mutated.example"
	assert.Equal(t, "valkey.internal", opts.tlsConfig.ServerName)
}

func TestOptionsRaiseTLSMinimum(t *testing.T) {
	opts, err := newOptions(
		WithAddress("127.0.0.1:6379"),
		WithTLSConfig(&tls.Config{MinVersion: tls.VersionTLS11}),
	)

	require.NoError(t, err)
	assert.Equal(t, uint16(tls.VersionTLS12), opts.tlsConfig.MinVersion)
}

func TestOptionsRejectNilAndPreserveOptionErrors(t *testing.T) {
	_, err := newOptions(WithAddress("127.0.0.1:6379"), nil)
	assert.ErrorIs(t, err, ErrInvalidConfiguration)

	optionErr := errors.New("option failed")
	_, err = newOptions(
		WithAddress("127.0.0.1:6379"),
		func(*options) error { return optionErr },
	)
	assert.ErrorIs(t, err, optionErr)
}

func TestOptionsRejectUnsafeConfiguration(t *testing.T) {
	tooManyReplayDestinations := make([]string, maxReplayDestinations+1)
	for index := range tooManyReplayDestinations {
		tooManyReplayDestinations[index] = fmt.Sprintf("archive-%d", index)
	}
	tests := map[string]Option{
		"missing address":          nil,
		"negative database":        WithDB(-1),
		"empty client name":        WithClientName(" "),
		"non-positive dial":        WithDialTimeout(0),
		"non-positive command":     WithCommandTimeout(0),
		"non-positive request":     WithRequestTimeout(0),
		"non-positive block":       WithBlockTime(0),
		"block exceeds request":    WithBlockTime(7 * time.Second),
		"non-positive shutdown":    WithShutdownTimeout(0),
		"invalid pool minimum":     WithBlockingPool(-1, 8, time.Minute),
		"invalid pool maximum":     WithBlockingPool(2, 0, time.Minute),
		"pool minimum exceeds max": WithBlockingPool(9, 8, time.Minute),
		"invalid pool cleanup":     WithBlockingPool(2, 8, -time.Second),
		"empty stream":             WithStreamName(" "),
		"empty group":              WithGroup(" "),
		"empty consumer":           WithConsumer(" "),
		"negative max length":      WithMaxLength(-1),
		"invalid record retention": WithRecordRetention(0),
		"invalid read batch":       WithReadBatchSize(0),
		"oversized read batch":     WithReadBatchSize(maxReadBatchSize + 1),
		"invalid reclaim idle":     WithReclaim(0, time.Second, 1),
		"invalid reclaim interval": WithReclaim(time.Second, 0, 1),
		"invalid reclaim batch":    WithReclaim(time.Second, time.Second, 0),
		"oversized reclaim batch":  WithReclaim(time.Second, time.Second, maxReclaimBatchSize+1),
		"empty failure stream":     WithFailureStream(" "),
		"failure matches source":   WithFailureStream("golang-queue"),
		"failure matches dead":     WithFailureStream("golang-queue-dead"),
		"empty replay destination": WithReplayDestinations(" "),
		"missing replay targets":   WithReplayDestinations(),
		"too many replay targets":  WithReplayDestinations(tooManyReplayDestinations...),
		"duplicate replay target":  WithReplayDestinations("archive", "archive"),
		"replay matches failure":   WithReplayDestinations("golang-queue-failures"),
		"replay matches dead":      WithReplayDestinations("golang-queue-dead"),
		"empty dead letter":        WithDeadLetter(" ", 3),
		"same dead letter stream":  WithDeadLetter("golang-queue", 3),
		"invalid delivery limit":   WithDeadLetter("dead", 1),
		"nil logger":               WithLogger(nil),
		"nil run function":         WithRunFunc(nil),
		"nil TLS config":           WithTLSConfig(nil),
	}

	for name, option := range tests {
		t.Run(name, func(t *testing.T) {
			options := []Option{WithAddress("127.0.0.1:6379")}
			if name == "missing address" {
				options = nil
			} else {
				options = append(options, option)
			}

			_, err := newOptions(options...)

			assert.ErrorIs(t, err, ErrInvalidConfiguration)
			var configErr *ConfigurationError
			assert.ErrorAs(t, err, &configErr)
			assert.NotEmpty(t, configErr.Field)
			assert.NotContains(t, err.Error(), "secret")
		})
	}
}
