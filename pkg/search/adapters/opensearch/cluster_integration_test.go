//go:build integration

package opensearch_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func integrationEndpoints(t *testing.T) []string {
	t.Helper()
	value := os.Getenv("OPENSEARCH_URLS")
	if value == "" {
		t.Skip("OPENSEARCH_URLS is not configured")
	}
	endpoints := strings.Split(value, ",")
	if len(endpoints) < 2 || endpoints[0] == "" || endpoints[1] == "" {
		t.Fatal("OPENSEARCH_URLS must contain at least two comma-separated endpoints")
	}
	return endpoints
}

func integrationClient(t *testing.T, endpoints []string) *adapter.Client {
	t.Helper()
	client, err := adapter.New(adapter.Config{
		Endpoints: endpoints, AllowInsecureHTTP: true,
		RequestTimeout: 5 * time.Second, MaximumResponseBytes: 8 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestRealOpenSearchMultiNodeRotation(t *testing.T) {
	endpoints := integrationEndpoints(t)
	expected := os.Getenv("OPENSEARCH_EXPECTED_VERSION")
	if expected == "" {
		t.Fatal("OPENSEARCH_EXPECTED_VERSION is required")
	}
	client := integrationClient(t, endpoints)
	clusters := make(map[string]struct{}, len(endpoints))
	for range endpoints {
		info, err := client.Info(t.Context())
		if err != nil || info.Version != expected {
			t.Fatal(info, err)
		}
		clusters[info.ClusterUUID] = struct{}{}
	}
	if len(clusters) != 1 {
		t.Fatalf("endpoints did not identify one cluster: %#v", clusters)
	}
}

func TestRealOpenSearchEndpointFailoverBudget(t *testing.T) {
	endpoints := integrationEndpoints(t)
	client := integrationClient(t, endpoints[:2])
	if _, err := client.Info(t.Context()); err == nil || !errors.Is(err, adapter.ErrTransport) {
		t.Fatalf("dead first endpoint did not produce one transport failure: %v", err)
	}
	if _, err := client.Info(context.Background()); err != nil {
		t.Fatalf("next operation did not advance to the live endpoint: %v", err)
	}
}
