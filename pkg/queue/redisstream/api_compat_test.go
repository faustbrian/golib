package redisdb_test

import (
	"context"
	"testing"
	"time"

	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/internal/testutil/apiguard"
	redisstream "github.com/faustbrian/golib/pkg/queue/redisstream"
	"github.com/stretchr/testify/assert"
)

func TestRedisStreamPublicAPIDoesNotLeakNativeClients(t *testing.T) {
	apiguard.NoPackageReferences(t, ".", map[string]string{
		"github.com/redis/go-redis/v9":   "redis",
		"github.com/valkey-io/valkey-go": "valkey",
	})
}

var (
	_ core.Worker         = (*redisstream.Worker)(nil)
	_ core.WorkerMetadata = (*redisstream.Worker)(nil)

	_ func(...redisstream.Option) *redisstream.Worker                        = redisstream.NewWorker
	_ func(...redisstream.Option) (*redisstream.Worker, error)               = redisstream.NewWorkerE
	_ func(string) redisstream.Option                                        = redisstream.WithAddr
	_ func(time.Duration) redisstream.Option                                 = redisstream.WithBlockTime
	_ func() redisstream.Option                                              = redisstream.WithCluster
	_ func(time.Duration) redisstream.Option                                 = redisstream.WithConnectTimeout
	_ func(string) redisstream.Option                                        = redisstream.WithConnectionString
	_ func(string) redisstream.Option                                        = redisstream.WithConsumer
	_ func(int) redisstream.Option                                           = redisstream.WithDB
	_ func(string) redisstream.Option                                        = redisstream.WithGroup
	_ func(queue.Logger) redisstream.Option                                  = redisstream.WithLogger
	_ func(int64) redisstream.Option                                         = redisstream.WithMaxLength
	_ func(string) redisstream.Option                                        = redisstream.WithPassword
	_ func(time.Duration) redisstream.Option                                 = redisstream.WithRequestTimeout
	_ func(...string) redisstream.Option                                     = redisstream.WithReplayDestinations
	_ func(int64) redisstream.Option                                         = redisstream.WithRecordRetention
	_ func(func(context.Context, core.TaskMessage) error) redisstream.Option = redisstream.WithRunFunc
	_ func() redisstream.Option                                              = redisstream.WithSkipTLSVerify
	_ func(string) redisstream.Option                                        = redisstream.WithStreamName
	_ func() redisstream.Option                                              = redisstream.WithTLS
	_ func(string) redisstream.Option                                        = redisstream.WithUsername
)

func TestRedisStreamStatsRemainSourceCompatible(t *testing.T) {
	stats := redisstream.Stats{
		Depth: 3, Pending: 1, Lag: 2, LagKnown: true,
		OldestJobAge: time.Second,
	}

	assert.Equal(t, int64(3), stats.Depth)
	assert.Equal(t, int64(1), stats.Pending)
	assert.Equal(t, int64(2), stats.Lag)
	assert.True(t, stats.LagKnown)
	assert.Equal(t, time.Second, stats.OldestJobAge)
}
