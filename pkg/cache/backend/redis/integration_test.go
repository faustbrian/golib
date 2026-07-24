//go:build integration

package redis_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	dockerclient "github.com/moby/moby/client"
	redisclient "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	cache "github.com/faustbrian/golib/pkg/cache"
	redisbackend "github.com/faustbrian/golib/pkg/cache/backend/redis"
	"github.com/faustbrian/golib/pkg/cache/cachetest"
)

func TestBackendConformance(t *testing.T) {
	image := os.Getenv("CACHE_REDIS_IMAGE")
	if image == "" {
		image = "redis:8.0"
	}
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        image,
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp"),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, container)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := container.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	client := redisclient.NewClient(&redisclient.Options{
		Addr:                  endpoint,
		MaxRetries:            -1,
		DialTimeout:           100 * time.Millisecond,
		ContextTimeoutEnabled: true,
	})
	t.Cleanup(func() { _ = client.Close() })
	backend, err := redisbackend.New(redisbackend.Config{
		Client:        client,
		Clock:         cache.SystemClock{},
		MaxRecordSize: 128,
	})
	if err != nil {
		t.Fatal(err)
	}
	runAdapterEdgeCases(t, ctx, container, client, backend)
	cachetest.RunBackendConformance(t, cachetest.BackendHarness{
		Backend: backend,
		MakeUnavailable: func(t *testing.T) {
			t.Helper()
			if err := testcontainers.TerminateContainer(container); err != nil {
				t.Fatal(err)
			}
		},
	})
}

func runAdapterEdgeCases(
	t *testing.T,
	ctx context.Context,
	container testcontainers.Container,
	client *redisclient.Client,
	backend *redisbackend.Backend,
) {
	t.Helper()
	t.Run("bounded read rejects oversized server value", func(t *testing.T) {
		if err := client.Set(ctx, "edge:oversized", strings.Repeat("x", 129), 0).Err(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := backend.Get(ctx, "edge:oversized"); !errors.Is(err, cache.ErrValueTooLarge) {
			t.Fatalf("oversized read returned %v", err)
		}
	})
	t.Run("malformed and mismatched records remain explicit", func(t *testing.T) {
		if err := client.Set(ctx, "edge:malformed", "x", 0).Err(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := backend.Get(ctx, "edge:malformed"); !errors.Is(err, cache.ErrDecode) {
			t.Fatalf("malformed read returned %v", err)
		}
		mismatch := make([]byte, 21)
		copy(mismatch, "BAD1")
		if err := client.Set(ctx, "edge:mismatch", mismatch, 0).Err(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := backend.Get(ctx, "edge:mismatch"); !errors.Is(err, cache.ErrSchemaMismatch) {
			t.Fatalf("schema mismatch returned %v", err)
		}
	})
	t.Run("invalid writes are rejected before server access", func(t *testing.T) {
		now := time.Now()
		expired := cache.Record{ExpiresAt: now.Add(-2 * time.Second), StaleAt: now.Add(-time.Second)}
		if _, err := backend.Set(ctx, "edge:expired", expired, cache.Unconditional); !errors.Is(err, cache.ErrInvalidTTL) {
			t.Fatalf("expired write returned %v", err)
		}
		oversized := cache.Record{
			Payload:   make([]byte, 108),
			ExpiresAt: now.Add(30 * time.Second),
			StaleAt:   now.Add(time.Minute),
		}
		if _, err := backend.Set(ctx, "edge:oversized-write", oversized, cache.Unconditional); !errors.Is(err, cache.ErrValueTooLarge) {
			t.Fatalf("oversized write returned %v", err)
		}
	})
	t.Run("server removes record at stale deadline", func(t *testing.T) {
		now := time.Now()
		record := cache.Record{
			Payload:   []byte("value"),
			ExpiresAt: now.Add(50 * time.Millisecond),
			StaleAt:   now.Add(100 * time.Millisecond),
		}
		if written, err := backend.Set(ctx, "edge:expiry", record, cache.Unconditional); err != nil || !written {
			t.Fatalf("set expiring record: written=%t err=%v", written, err)
		}
		ttl, err := client.PTTL(ctx, "edge:expiry").Result()
		if err != nil || ttl <= 0 || ttl > 100*time.Millisecond {
			t.Fatalf("server TTL=%v err=%v", ttl, err)
		}
	})
	t.Run("server expiration follows injected clock duration", func(t *testing.T) {
		now := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		skewed, err := redisbackend.New(redisbackend.Config{
			Client: client, Clock: fixedClock{now: now}, MaxRecordSize: 128,
		})
		if err != nil {
			t.Fatal(err)
		}
		record := cache.Record{
			Payload: []byte("value"), ExpiresAt: now.Add(time.Minute), StaleAt: now.Add(2 * time.Minute),
		}
		if written, err := skewed.Set(ctx, "edge:skewed-clock", record, cache.Unconditional); err != nil || !written {
			t.Fatalf("set with skewed clock: written=%t err=%v", written, err)
		}
		ttl, err := client.PTTL(ctx, "edge:skewed-clock").Result()
		if err != nil || ttl < 119*time.Second || ttl > 2*time.Minute {
			t.Fatalf("server TTL=%v err=%v, want injected two-minute duration", ttl, err)
		}
	})
	t.Run("submillisecond expiry uses minimum server TTL", func(t *testing.T) {
		now := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		precise, err := redisbackend.New(redisbackend.Config{
			Client: client, Clock: fixedClock{now: now}, MaxRecordSize: 128,
		})
		if err != nil {
			t.Fatal(err)
		}
		record := cache.Record{
			Payload: []byte("value"), ExpiresAt: now.Add(250 * time.Microsecond), StaleAt: now.Add(500 * time.Microsecond),
		}
		if written, err := precise.Set(ctx, "edge:submillisecond", record, cache.Unconditional); err != nil || !written {
			t.Fatalf("submillisecond Set: written=%t err=%v", written, err)
		}
	})
	t.Run("client recovers after network interruption", func(t *testing.T) {
		docker, err := testcontainers.NewDockerClientWithOpts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer docker.Close()
		if _, err := docker.ContainerPause(ctx, container.GetContainerID(), dockerclient.ContainerPauseOptions{}); err != nil {
			t.Fatal(err)
		}
		paused := true
		defer func() {
			if paused {
				_, _ = docker.ContainerUnpause(ctx, container.GetContainerID(), dockerclient.ContainerUnpauseOptions{})
			}
		}()
		callCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		_, _, outageErr := backend.Get(callCtx, "edge:interruption")
		cancel()
		if outageErr == nil {
			t.Fatal("paused server returned no error")
		}
		if _, err := docker.ContainerUnpause(ctx, container.GetContainerID(), dockerclient.ContainerUnpauseOptions{}); err != nil {
			t.Fatal(err)
		}
		paused = false
		now := time.Now()
		record := cache.Record{
			Payload: []byte("recovered"), ExpiresAt: now.Add(time.Minute), StaleAt: now.Add(2 * time.Minute),
		}
		if written, err := backend.Set(ctx, "edge:interruption", record, cache.Unconditional); err != nil || !written {
			t.Fatalf("Set after interruption: written=%t err=%v", written, err)
		}
		got, found, err := backend.Get(ctx, "edge:interruption")
		if err != nil || !found || string(got.Payload) != "recovered" {
			t.Fatalf("Get after interruption: record=%#v found=%t err=%v", got, found, err)
		}
	})
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }
