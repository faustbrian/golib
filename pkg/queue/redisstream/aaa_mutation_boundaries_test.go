package redisdb

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	queue "github.com/faustbrian/golib/pkg/queue"
	"github.com/faustbrian/golib/pkg/queue/internal/streamqueue"
	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplayDestinationsRejectsAnOversizedAllowlist(t *testing.T) {
	destinations := make([]string, maxReplayDestinations+1)
	for index := range destinations {
		destinations[index] = fmt.Sprintf("archive-%d", index)
	}
	worker, err := NewWorkerE(
		WithAddr("127.0.0.1:1"), WithReplayDestinations(destinations...),
	)
	assert.Nil(t, worker)
	assert.ErrorIs(t, err, queue.ErrInvalidConfiguration)
}

func TestRedisOptionsPreserveInclusiveLimitsAndLaterDestinations(t *testing.T) {
	destinations := make([]string, maxReplayDestinations)
	for index := range destinations {
		destinations[index] = fmt.Sprintf("archive-%d", index)
	}
	opts := newOptions(
		WithReclaim(time.Second, time.Second, streamqueue.MaxBatchSize),
		WithReplayDestinations(destinations...),
	)
	require.NoError(t, opts.validateDeadLetter())
	assert.Equal(t, 60*time.Second, newOptions().blockTime)

	exactIdentity := strings.Repeat("x", management.MaxIdentityBytes)
	opts = newOptions(WithReplayDestinations(" ", "archive", "archive", "quarantine", exactIdentity))
	assert.True(t, opts.replayInvalid)
	assert.Contains(t, opts.replayDestinations, "archive")
	assert.Contains(t, opts.replayDestinations, "quarantine")
	assert.Contains(t, opts.replayDestinations, exactIdentity)
}

func TestRedisRecordCursorRejectsEveryMalformedBoundary(t *testing.T) {
	tests := map[string]string{
		"invalid base64":       "!",
		"non-canonical base64": "YR",
		"oversized identity": base64.RawURLEncoding.EncodeToString(
			[]byte(strings.Repeat("1", management.MaxIdentityBytes+1)),
		),
	}
	for name, cursor := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := decodeRedisRecordCursor(cursor)
			assert.ErrorIs(t, err, management.ErrMalformedCursor)
		})
	}
}

func TestRedisFetchClampsEachInvalidBlockTime(t *testing.T) {
	for _, configured := range []time.Duration{0, 2 * time.Second} {
		t.Run(configured.String(), func(t *testing.T) {
			stop := make(chan struct{})
			var observed time.Duration
			worker := &Worker{
				stop: stop,
				opts: newOptions(
					WithBlockTime(configured), WithLogger(queue.NewEmptyLogger()),
				),
				readGroup: func(_ context.Context, arguments *redis.XReadGroupArgs) ([]redis.XStream, error) {
					observed = arguments.Block
					close(stop)
					return nil, redis.Nil
				},
			}
			worker.fetchTask()
			assert.Equal(t, time.Second, observed)
		})
	}
}

func TestRedisReplayLineageRejectsEachMalformedField(t *testing.T) {
	valid := map[string]any{
		replayOriginalDeadLetterField: "original",
		replayPriorDeadLetterField:    "prior",
		replayGenerationField:         "1",
	}
	tests := map[string]func(map[string]any){
		"missing original":   func(values map[string]any) { delete(values, replayOriginalDeadLetterField) },
		"missing prior":      func(values map[string]any) { delete(values, replayPriorDeadLetterField) },
		"missing generation": func(values map[string]any) { delete(values, replayGenerationField) },
		"blank original":     func(values map[string]any) { values[replayOriginalDeadLetterField] = " " },
		"blank prior":        func(values map[string]any) { values[replayPriorDeadLetterField] = " " },
		"oversized original": func(values map[string]any) {
			values[replayOriginalDeadLetterField] = strings.Repeat("x", management.MaxIdentityBytes+1)
		},
		"oversized prior": func(values map[string]any) {
			values[replayPriorDeadLetterField] = strings.Repeat("x", management.MaxIdentityBytes+1)
		},
		"invalid generation": func(values map[string]any) { values[replayGenerationField] = "invalid" },
		"zero generation":    func(values map[string]any) { values[replayGenerationField] = "0" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			values := make(map[string]any, len(valid))
			for key, value := range valid {
				values[key] = value
			}
			mutate(values)
			_, err := redisLineageFromValues(values)
			assert.Error(t, err)
		})
	}
	lineage, err := redisLineageFromValues(valid)
	require.NoError(t, err)
	assert.Equal(t, redisReplayLineage{original: "original", prior: "prior", generation: 1}, lineage)
	exact := strings.Repeat("x", management.MaxIdentityBytes)
	lineage, err = redisLineageFromValues(map[string]any{
		replayOriginalDeadLetterField: exact,
		replayPriorDeadLetterField:    exact,
		replayGenerationField:         "1",
	})
	require.NoError(t, err)
	assert.Equal(t, exact, lineage.original)
	assert.Equal(t, exact, lineage.prior)
}

func TestRedisRecordSearchScansBeyondPageAndNeverOverfills(t *testing.T) {
	server := miniredis.RunT(t)
	worker, err := NewWorkerE(
		WithAddr(server.Addr()), WithFailureStream("failures"), WithDeadLetter("dead", 5),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = worker.Shutdown() })
	for index, code := range []string{"skip-a", "skip-b", "skip-c", "match", "match"} {
		require.NoError(t, worker.appendRecord(
			t.Context(), "failures", fmt.Sprintf("%d-0", index+1), []byte(code), 1,
			streamqueue.FailureMetadata{Classification: management.ClassificationRetryable, Code: code},
		))
	}
	page, err := worker.ListFailures(t.Context(), management.PageRequest{
		Limit: 1, Search: "match", Sort: management.SortOccurredAt,
		Direction: management.SortAscending,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "match", page.Items[0].FailureCode)
	page, err = worker.ListFailures(t.Context(), management.PageRequest{
		Limit: 1, Search: "skip", Sort: management.SortOccurredAt,
		Direction: management.SortAscending,
	})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
}

func TestRedisConfigurationAndStatsPreserveExactBoundaries(t *testing.T) {
	redisOptions := &redis.Options{}
	configureRedisOptions(redisOptions, time.Second)
	assert.Equal(t, -1, redisOptions.DialerRetries)
	assert.Equal(t, -1, redisOptions.MaxRetries)
	clusterOptions := redisClusterOptions(options{
		addr: "redis-a:6379,redis-b:6379", username: "user", password: "password",
		connectTimeout: time.Second,
	})
	assert.Equal(t, []string{"redis-a:6379", "redis-b:6379"}, clusterOptions.Addrs)
	assert.Equal(t, -1, clusterOptions.DialerRetries)
	assert.Equal(t, -1, clusterOptions.MaxRedirects)
	assert.True(t, clusterOptions.ContextTimeoutEnabled)
	assert.False(t, redisGroupLagCapability("redis_version:7.0.0", errors.New("unavailable")))
	assert.True(t, redisGroupLagCapability("redis_version:7.0.0", nil))
	assert.False(t, redisGroupLagCapability("redis_version:6.2.0", nil))

	worker := &Worker{
		opts: options{streamName: "jobs", group: "workers"},
		readGroups: func(context.Context, string) ([]redis.XInfoGroup, error) {
			return []redis.XInfoGroup{
				{Name: "workers", Pending: 1, Lag: 1, LastDeliveredID: "1-0"},
				{Name: "workers", Pending: 9, Lag: 9, LastDeliveredID: "9-0"},
			}, nil
		},
		readPending: func(context.Context, *redis.XPendingExtArgs) ([]redis.XPendingExt, error) {
			return []redis.XPendingExt{{ID: "1-0"}}, nil
		},
		readRange: func(context.Context, string, string, string, int64) ([]redis.XMessage, error) {
			return []redis.XMessage{{ID: "1-0"}}, nil
		},
	}
	stats, err := worker.Stats(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats.Depth)
	assert.Equal(t, int64(1), stats.Pending)

	pendingCalls := 0
	rangeCalls := 0
	worker.readGroups = func(context.Context, string) ([]redis.XInfoGroup, error) {
		return []redis.XInfoGroup{{Name: "workers", Pending: 0, Lag: 1, LastDeliveredID: "1-0"}}, nil
	}
	worker.readPending = func(context.Context, *redis.XPendingExtArgs) ([]redis.XPendingExt, error) {
		pendingCalls++
		return nil, nil
	}
	worker.readRange = func(context.Context, string, string, string, int64) ([]redis.XMessage, error) {
		rangeCalls++
		return []redis.XMessage{{ID: "1-0"}}, nil
	}
	_, err = worker.Stats(t.Context())
	require.NoError(t, err)
	assert.Zero(t, pendingCalls)
	assert.Equal(t, 1, rangeCalls)

	worker.readGroups = func(context.Context, string) ([]redis.XInfoGroup, error) {
		return []redis.XInfoGroup{{Name: "workers", Pending: 1, Lag: 0}}, nil
	}
	worker.readPending = func(context.Context, *redis.XPendingExtArgs) ([]redis.XPendingExt, error) {
		return []redis.XPendingExt{{ID: "1-0"}}, nil
	}
	worker.readRange = func(context.Context, string, string, string, int64) ([]redis.XMessage, error) {
		rangeCalls++
		return nil, nil
	}
	_, err = worker.Stats(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, rangeCalls)
}

func TestRedisWorkerStatusRequiresMetadataAndStartTimeSeparately(t *testing.T) {
	for _, worker := range []*Worker{
		{startedAt: time.Now()},
		{opts: options{management: &management.StatusMetadata{ID: "worker"}}},
	} {
		_, err := worker.ObserveWorker(context.Background())
		assert.ErrorIs(t, err, ErrManagementStatusDisabled)
	}
}

func TestRedisQueueDepthRequiresCapabilityAndKnownLag(t *testing.T) {
	for _, test := range []struct {
		name              string
		groupLagSupported bool
		lag               int64
	}{
		{name: "capability unavailable", lag: 3},
		{name: "lag unknown", groupLagSupported: true, lag: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			worker := &Worker{
				opts:              options{streamName: "jobs", group: "workers", management: &management.StatusMetadata{ID: "worker"}},
				groupLagSupported: test.groupLagSupported,
				readGroups: func(context.Context, string) ([]redis.XInfoGroup, error) {
					return []redis.XInfoGroup{{Name: "workers", Lag: test.lag}}, nil
				},
				readPending: func(context.Context, *redis.XPendingExtArgs) ([]redis.XPendingExt, error) { return nil, nil },
				readRange:   func(context.Context, string, string, string, int64) ([]redis.XMessage, error) { return nil, nil },
			}
			status, err := worker.ObserveQueue(context.Background())
			require.NoError(t, err)
			assert.False(t, status.Metrics.Depth.Supported)
			assert.False(t, status.Metrics.Lag.Supported)
		})
	}
}

func TestRedisPermanentFailureBelowAttemptLimitKeepsItsCode(t *testing.T) {
	worker, message := newPendingRedisMessage(t)
	err := worker.settleHandlerFailure(
		message, []byte("payload"), management.NewFailure(
			management.ClassificationPermanent, "invalid_order", errors.New("invalid"),
		),
	)
	require.NoError(t, err)
	records, err := worker.rdb.XRevRangeN(t.Context(), worker.opts.deadLetterStream, "+", "-", 1).Result()
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "invalid_order", records[0].Values[failureCodeField])
}
