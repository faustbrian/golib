package valkeystream_test

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/testutil/apiguard"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/faustbrian/golib/pkg/queue/valkeystream"
	"github.com/stretchr/testify/assert"
)

func TestValkeyStreamPublicAPIDoesNotLeakNativeClients(t *testing.T) {
	apiguard.NoPackageReferences(t, ".", map[string]string{
		"github.com/redis/go-redis/v9":   "redis",
		"github.com/valkey-io/valkey-go": "valkey",
	})
}

var (
	_ core.Worker             = (*valkeystream.Worker)(nil)
	_ core.WorkerMetadata     = (*valkeystream.Worker)(nil)
	_ management.RecordReader = (*valkeystream.Worker)(nil)
	_ management.Controller   = (*valkeystream.Worker)(nil)

	_ func(...valkeystream.Option) *valkeystream.Worker                       = valkeystream.NewWorker
	_ func(...valkeystream.Option) (*valkeystream.Worker, error)              = valkeystream.NewWorkerE
	_ func(string) valkeystream.Option                                        = valkeystream.WithAddress
	_ func(string, string) valkeystream.Option                                = valkeystream.WithAuthentication
	_ func(time.Duration) valkeystream.Option                                 = valkeystream.WithBlockTime
	_ func(int, int, time.Duration) valkeystream.Option                       = valkeystream.WithBlockingPool
	_ func(string) valkeystream.Option                                        = valkeystream.WithClientName
	_ func(time.Duration) valkeystream.Option                                 = valkeystream.WithCommandTimeout
	_ func(string) valkeystream.Option                                        = valkeystream.WithConsumer
	_ func(int) valkeystream.Option                                           = valkeystream.WithDB
	_ func(string, int64) valkeystream.Option                                 = valkeystream.WithDeadLetter
	_ func(time.Duration) valkeystream.Option                                 = valkeystream.WithDialTimeout
	_ func(string) valkeystream.Option                                        = valkeystream.WithFailureStream
	_ func(...string) valkeystream.Option                                     = valkeystream.WithReplayDestinations
	_ func(string) valkeystream.Option                                        = valkeystream.WithGroup
	_ func(queue.Logger) valkeystream.Option                                  = valkeystream.WithLogger
	_ func(int64) valkeystream.Option                                         = valkeystream.WithMaxLength
	_ func(int64) valkeystream.Option                                         = valkeystream.WithRecordRetention
	_ func(int) valkeystream.Option                                           = valkeystream.WithReadBatchSize
	_ func(time.Duration, time.Duration, int) valkeystream.Option             = valkeystream.WithReclaim
	_ func(time.Duration) valkeystream.Option                                 = valkeystream.WithRequestTimeout
	_ func(func(context.Context, core.TaskMessage) error) valkeystream.Option = valkeystream.WithRunFunc
	_ func(time.Duration) valkeystream.Option                                 = valkeystream.WithShutdownTimeout
	_ func(string) valkeystream.Option                                        = valkeystream.WithStreamName
	_ func(*tls.Config) valkeystream.Option                                   = valkeystream.WithTLSConfig
	_ error                                                                   = valkeystream.ErrInvalidConfiguration
	_ error                                                                   = valkeystream.ErrManagementRecordNotFound
	_ error                                                                   = valkeystream.ErrManagementRecordsDisabled
	_ error                                                                   = valkeystream.ErrManagementControlDisabled
)

func TestValkeyStreamStatsRemainPackageOwned(t *testing.T) {
	stats := valkeystream.Stats{
		Depth: 3, Pending: 1, Lag: 2, LagKnown: true,
		OldestPendingAge: time.Second,
		Enqueued:         4, Delivered: 3, Reclaimed: 2, Retries: 2,
		Acknowledged: 1, DeadLettered: 1, SettlementFailures: 1,
	}

	assert.Equal(t, int64(3), stats.Depth)
	assert.Equal(t, int64(1), stats.Pending)
	assert.Equal(t, int64(2), stats.Lag)
	assert.True(t, stats.LagKnown)
	assert.Equal(t, time.Second, stats.OldestPendingAge)
	assert.Equal(t, uint64(4), stats.Enqueued)
	assert.Equal(t, uint64(3), stats.Delivered)
	assert.Equal(t, uint64(2), stats.Reclaimed)
	assert.Equal(t, uint64(2), stats.Retries)
	assert.Equal(t, uint64(1), stats.Acknowledged)
	assert.Equal(t, uint64(1), stats.DeadLettered)
	assert.Equal(t, uint64(1), stats.SettlementFailures)

	configurationErr := &valkeystream.ConfigurationError{
		Field: "address", Cause: errors.New("cause"),
	}
	assert.ErrorIs(t, configurationErr, valkeystream.ErrInvalidConfiguration)
}
