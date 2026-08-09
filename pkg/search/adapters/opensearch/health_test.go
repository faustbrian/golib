package opensearch_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestHealthAndCapacityPreserveOperationalSignals(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/_cluster/health":
			_, _ = io.WriteString(writer, `{"cluster_name":"search","status":"yellow","timed_out":false,"number_of_nodes":3,"number_of_data_nodes":2,"active_primary_shards":4,"active_shards":7,"relocating_shards":0,"initializing_shards":0,"unassigned_shards":1,"number_of_pending_tasks":0,"active_shards_percent_as_number":87.5}`)
		case "/_cluster/stats":
			_, _ = io.WriteString(writer, `{"_nodes":{"total":3,"successful":3,"failed":0},"indices":{"count":4,"shards":{"total":8,"primaries":4},"docs":{"count":120,"deleted":2},"store":{"size_in_bytes":1048576}},"nodes":{"count":{"total":3,"data":2},"jvm":{"mem":{"heap_used_in_bytes":2048,"heap_max_in_bytes":8192}},"fs":{"available_in_bytes":1073741824}}}`)
		case "/_nodes/stats/thread_pool,breaker":
			_, _ = io.WriteString(writer, `{"_nodes":{"total":3,"successful":3,"failed":0},"nodes":{"opaque-a":{"thread_pool":{"search":{"rejected":2},"write":{"rejected":3}},"breakers":{"parent":{"tripped":1}}},"opaque-b":{"thread_pool":{"search":{"rejected":4},"write":{"rejected":0}},"breakers":{"parent":{"tripped":2}}}}}`)
		default:
			http.Error(writer, "unexpected", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client, err := adapter.New(adapter.Config{Endpoints: []string{server.URL}, Transport: server.Client().Transport, TransportOwnership: adapter.TransportBorrowed, RequestTimeout: time.Second, MaximumResponseBytes: 32 << 10})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	health, err := client.Health(t.Context())
	if err != nil || !health.Ready || health.Status != adapter.HealthYellow || health.UnassignedShards != 1 || health.Nodes != 3 {
		t.Fatalf("Health() = %#v/%v", health, err)
	}
	capacity, err := client.Capacity(t.Context())
	if err != nil || capacity.Documents != 120 || capacity.StoreBytes != 1048576 || capacity.DiskAvailableBytes != 1073741824 || capacity.ThreadPoolRejected["search"] != 6 || capacity.BreakerTripped["parent"] != 3 {
		t.Fatalf("Capacity() = %#v/%v", capacity, err)
	}
}

func TestHealthMarksRedOrIncompleteClustersUnready(t *testing.T) {
	t.Parallel()
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, TransportOwnership: adapter.TransportBorrowed,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"cluster_name":"search","status":"red","timed_out":false,"number_of_nodes":1,"number_of_data_nodes":1,"active_primary_shards":0,"active_shards":0,"relocating_shards":0,"initializing_shards":1,"unassigned_shards":1,"number_of_pending_tasks":0,"active_shards_percent_as_number":0}`), nil
		}),
		RequestTimeout: time.Second, MaximumResponseBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	health, err := client.Health(context.Background())
	if err != nil || health.Ready {
		t.Fatalf("Health() = %#v/%v", health, err)
	}
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
