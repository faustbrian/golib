//go:build integration

package valkey_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	dockerclient "github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	valkeyclient "github.com/valkey-io/valkey-go"

	cache "github.com/faustbrian/golib/pkg/cache"
	valkeybackend "github.com/faustbrian/golib/pkg/cache/backend/valkey"
	"github.com/faustbrian/golib/pkg/cache/cachetest"
)

func TestBackendConformance(t *testing.T) {
	image := os.Getenv("CACHE_VALKEY_IMAGE")
	if image == "" {
		image = "valkey/valkey:9.0"
	}
	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        image,
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor: wait.ForAll(
				wait.ForListeningPort("6379/tcp"),
				wait.ForLog("Ready to accept connections"),
			),
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
	client, err := valkeyclient.NewClient(valkeyclient.ClientOption{InitAddress: []string{endpoint}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	backend, err := valkeybackend.New(valkeybackend.Config{
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
	client valkeyclient.Client,
	backend *valkeybackend.Backend,
) {
	t.Helper()
	t.Run("bounded read rejects oversized server value", func(t *testing.T) {
		command := client.B().Set().Key("edge:oversized").Value(strings.Repeat("x", 129)).Build()
		if err := client.Do(ctx, command).Error(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := backend.Get(ctx, "edge:oversized"); !errors.Is(err, cache.ErrValueTooLarge) {
			t.Fatalf("oversized read returned %v", err)
		}
	})
	t.Run("malformed and mismatched records remain explicit", func(t *testing.T) {
		command := client.B().Set().Key("edge:malformed").Value("x").Build()
		if err := client.Do(ctx, command).Error(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := backend.Get(ctx, "edge:malformed"); !errors.Is(err, cache.ErrDecode) {
			t.Fatalf("malformed read returned %v", err)
		}
		mismatch := make([]byte, 21)
		copy(mismatch, "BAD1")
		command = client.B().Set().Key("edge:mismatch").Value(valkeyclient.BinaryString(mismatch)).Build()
		if err := client.Do(ctx, command).Error(); err != nil {
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
		ttlMillis, err := client.Do(ctx, client.B().Pttl().Key("edge:expiry").Build()).ToInt64()
		ttl := time.Duration(ttlMillis) * time.Millisecond
		if err != nil || ttl <= 0 || ttl > 100*time.Millisecond {
			t.Fatalf("server TTL=%v err=%v", ttl, err)
		}
	})
	t.Run("server expiration follows injected clock duration", func(t *testing.T) {
		now := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		skewed, err := valkeybackend.New(valkeybackend.Config{
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
		ttlMillis, err := client.Do(ctx, client.B().Pttl().Key("edge:skewed-clock").Build()).ToInt64()
		ttl := time.Duration(ttlMillis) * time.Millisecond
		if err != nil || ttl < 119*time.Second || ttl > 2*time.Minute {
			t.Fatalf("server TTL=%v err=%v, want injected two-minute duration", ttl, err)
		}
	})
	t.Run("submillisecond expiry uses minimum server TTL", func(t *testing.T) {
		now := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		precise, err := valkeybackend.New(valkeybackend.Config{
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
	t.Run("protected publish rejects stale and missing ownership", func(t *testing.T) {
		leaseKey := "lease:{catalog}:lease"
		cacheKey := "cache:catalog"
		now := time.Now()
		record := cache.Record{
			Payload: []byte("successor"), ExpiresAt: now.Add(time.Minute), StaleAt: now.Add(2 * time.Minute),
		}
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		if err := backend.SetIfOwned(
			canceled,
			cacheKey,
			record,
			testOwnershipGuard{key: leaseKey, owner: "successor", token: "2"},
		); !errors.Is(err, context.Canceled) {
			t.Fatalf("SetIfOwned(canceled) error = %v", err)
		}
		if err := backend.SetIfOwned(ctx, cacheKey, record, nil); !errors.Is(err, cache.ErrInvalidPolicy) {
			t.Fatalf("SetIfOwned(invalid guard) error = %v", err)
		}
		if err := backend.SetIfOwned(
			ctx,
			cacheKey,
			cache.Record{},
			testOwnershipGuard{key: leaseKey, owner: "successor", token: "2"},
		); !errors.Is(err, cache.ErrInvalidRecord) {
			t.Fatalf("SetIfOwned(invalid record) error = %v", err)
		}
		setLease := func(owner, token string) {
			t.Helper()
			command := client.B().Hset().Key(leaseKey).
				FieldValue().FieldValue("owner", owner).
				FieldValue("token", token).
				Build()
			if err := client.Do(ctx, command).Error(); err != nil {
				t.Fatal(err)
			}
		}
		setLease("successor", "2")
		stale := testOwnershipGuard{key: leaseKey, owner: "predecessor", token: "1"}
		if err := backend.SetIfOwned(ctx, cacheKey, record, stale); !errors.Is(err, cache.ErrOwnershipLost) {
			t.Fatalf("SetIfOwned(stale) error = %v", err)
		}
		if exists, err := client.Do(ctx, client.B().Exists().Key(cacheKey).Build()).ToInt64(); err != nil || exists != 0 {
			t.Fatalf("stale publish cache existence = %d, %v", exists, err)
		}

		current := testOwnershipGuard{key: leaseKey, owner: "successor", token: "2"}
		if err := backend.SetIfOwned(ctx, cacheKey, record, current); err != nil {
			t.Fatalf("SetIfOwned(current) error = %v", err)
		}
		if err := client.Do(ctx, client.B().Del().Key(cacheKey).Key(leaseKey).Build()).Error(); err != nil {
			t.Fatal(err)
		}
		if err := backend.SetIfOwned(ctx, cacheKey, record, current); !errors.Is(err, cache.ErrOwnershipLost) {
			t.Fatalf("SetIfOwned(missing) error = %v", err)
		}
		if exists, err := client.Do(ctx, client.B().Exists().Key(cacheKey).Build()).ToInt64(); err != nil || exists != 0 {
			t.Fatalf("missing-owner publish cache existence = %d, %v", exists, err)
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
		callCtx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
		now := time.Now()
		outageErr = backend.SetIfOwned(
			callCtx,
			"edge:interruption-protected",
			cache.Record{
				Payload:   []byte("value"),
				ExpiresAt: now.Add(time.Minute),
				StaleAt:   now.Add(2 * time.Minute),
			},
			testOwnershipGuard{key: "lease:interruption", owner: "worker", token: "1"},
		)
		cancel()
		if outageErr == nil {
			t.Fatal("paused server accepted protected write")
		}
		if _, err := docker.ContainerUnpause(ctx, container.GetContainerID(), dockerclient.ContainerUnpauseOptions{}); err != nil {
			t.Fatal(err)
		}
		paused = false
		now = time.Now()
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

type testOwnershipGuard struct {
	key   string
	owner string
	token string
}

func (guard testOwnershipGuard) StorageKey() string { return guard.key }

func (guard testOwnershipGuard) Owner() string { return guard.owner }

func (guard testOwnershipGuard) Token() string { return guard.token }
