//go:build integration

package valkey_test

import (
	"os"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	valkeyclient "github.com/valkey-io/valkey-go"

	cache "github.com/faustbrian/golib/pkg/cache"
	valkeybackend "github.com/faustbrian/golib/pkg/cache/backend/valkey"
	"github.com/faustbrian/golib/pkg/cache/internal/integrationtest"
)

const valkeyTestPassword = "integration-only-password"

func TestBackendAuthentication(t *testing.T) {
	container, endpoint := startSecuredValkey(t, nil)
	testcontainers.CleanupContainer(t, container)

	unauthorized, err := valkeyclient.NewClient(valkeyclient.ClientOption{InitAddress: []string{endpoint}})
	if err == nil {
		t.Cleanup(unauthorized.Close)
		if err := unauthorized.Do(t.Context(), unauthorized.B().Ping().Build()).Error(); err == nil {
			t.Fatal("server accepted unauthenticated client")
		}
	}

	client, err := valkeyclient.NewClient(valkeyclient.ClientOption{
		InitAddress: []string{endpoint}, Password: valkeyTestPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	assertValkeyRoundTrip(t, client)
}

func TestBackendTLSAndAuthentication(t *testing.T) {
	material := integrationtest.NewTLSMaterial(t)
	container, endpoint := startSecuredValkey(t, &material)
	testcontainers.CleanupContainer(t, container)

	client, err := valkeyclient.NewClient(valkeyclient.ClientOption{
		InitAddress: []string{endpoint}, Password: valkeyTestPassword, TLSConfig: material.ClientConfig,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	assertValkeyRoundTrip(t, client)
}

func startSecuredValkey(
	t *testing.T,
	material *integrationtest.TLSMaterial,
) (testcontainers.Container, string) {
	t.Helper()
	image := os.Getenv("CACHE_VALKEY_IMAGE")
	if image == "" {
		image = "valkey/valkey:9.0"
	}
	request := testcontainers.ContainerRequest{
		Image:        image,
		ExposedPorts: []string{"6379/tcp"},
		WaitingFor:   wait.ForListeningPort("6379/tcp"),
		Cmd: []string{
			"valkey-server", "--requirepass", valkeyTestPassword,
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

func assertValkeyRoundTrip(t *testing.T, client valkeyclient.Client) {
	t.Helper()
	backend, err := valkeybackend.New(valkeybackend.Config{
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
