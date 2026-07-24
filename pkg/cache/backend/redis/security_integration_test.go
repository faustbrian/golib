//go:build integration

package redis_test

import (
	"os"
	"testing"
	"time"

	redisclient "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	cache "github.com/faustbrian/golib/pkg/cache"
	redisbackend "github.com/faustbrian/golib/pkg/cache/backend/redis"
	"github.com/faustbrian/golib/pkg/cache/internal/integrationtest"
)

const redisTestPassword = "integration-only-password"

func TestBackendAuthentication(t *testing.T) {
	container, endpoint := startSecuredRedis(t, nil)
	testcontainers.CleanupContainer(t, container)

	unauthorized := redisclient.NewClient(&redisclient.Options{Addr: endpoint, MaxRetries: -1})
	t.Cleanup(func() { _ = unauthorized.Close() })
	if err := unauthorized.Ping(t.Context()).Err(); err == nil {
		t.Fatal("server accepted unauthenticated client")
	}

	client := redisclient.NewClient(&redisclient.Options{
		Addr: endpoint, Password: redisTestPassword, MaxRetries: -1,
	})
	t.Cleanup(func() { _ = client.Close() })
	assertRedisRoundTrip(t, client)
}

func TestBackendTLSAndAuthentication(t *testing.T) {
	material := integrationtest.NewTLSMaterial(t)
	container, endpoint := startSecuredRedis(t, &material)
	testcontainers.CleanupContainer(t, container)

	client := redisclient.NewClient(&redisclient.Options{
		Addr: endpoint, Password: redisTestPassword, TLSConfig: material.ClientConfig, MaxRetries: -1,
	})
	t.Cleanup(func() { _ = client.Close() })
	assertRedisRoundTrip(t, client)
}

func startSecuredRedis(
	t *testing.T,
	material *integrationtest.TLSMaterial,
) (testcontainers.Container, string) {
	t.Helper()
	image := os.Getenv("CACHE_REDIS_IMAGE")
	if image == "" {
		image = "redis:8.0"
	}
	request := testcontainers.ContainerRequest{
		Image:        image,
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForListeningPort("6379/tcp"),
		Cmd: []string{
			"redis-server", "--requirepass", redisTestPassword,
		},
	}
	if material != nil {
		request.Files = material.Files
		request.Cmd = append(request.Cmd,
			"--port", "0",
			"--tls-port", "6379",
			"--tls-cert-file", "/tls/server.crt",
			"--tls-key-file", "/tls/server.key",
			"--tls-ca-cert-file", "/tls/ca.crt",
			"--tls-auth-clients", "no",
		)
	}
	container, err := testcontainers.GenericContainer(t.Context(), testcontainers.GenericContainerRequest{
		ContainerRequest: request,
		Started:          true,
	})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := container.Endpoint(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	return container, endpoint
}

func assertRedisRoundTrip(t *testing.T, client *redisclient.Client) {
	t.Helper()
	backend, err := redisbackend.New(redisbackend.Config{
		Client: client, Clock: cache.SystemClock{}, MaxRecordSize: 128,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	record := cache.Record{
		Payload: []byte("secured"), ExpiresAt: now.Add(time.Minute), StaleAt: now.Add(2 * time.Minute),
	}
	if written, err := backend.Set(t.Context(), "security:roundtrip", record, cache.Unconditional); err != nil || !written {
		t.Fatalf("secured Set: written=%t err=%v", written, err)
	}
	got, found, err := backend.Get(t.Context(), "security:roundtrip")
	if err != nil || !found || string(got.Payload) != "secured" {
		t.Fatalf("secured Get: record=%#v found=%t err=%v", got, found, err)
	}
}
