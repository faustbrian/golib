package opensearch_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestTelemetryEmitsBoundedOperationOutcomesAndContainsObserverPanics(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	events := make([]adapter.TelemetryEvent, 0, 1)
	observer := adapter.TelemetryObserverFunc(func(_ context.Context, event adapter.TelemetryEvent) error {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
		return nil
	})
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, TransportOwnership: adapter.TransportBorrowed,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"name":"node-a","cluster_name":"search","cluster_uuid":"cluster-a","version":{"number":"3.6.0"}}`), nil
		}),
		RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Telemetry: &adapter.TelemetryConfig{Observer: observer, Clock: time.Now},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.Info(t.Context()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(events) != 1 || events[0].Operation != adapter.OperationInfo || events[0].Category != adapter.TelemetrySuccess || events[0].Status != http.StatusOK || events[0].Duration < 0 {
		t.Fatalf("events = %#v", events)
	}
	mu.Unlock()

	panicClient, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, TransportOwnership: adapter.TransportBorrowed,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{"name":"node-a","cluster_name":"search","cluster_uuid":"cluster-a","version":{"number":"3.6.0"}}`), nil
		}),
		RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Telemetry: &adapter.TelemetryConfig{Observer: adapter.TelemetryObserverFunc(func(context.Context, adapter.TelemetryEvent) error { panic("observer detail") }), Clock: time.Now},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = panicClient.Close() })
	if _, err := panicClient.Info(t.Context()); err != nil {
		t.Fatalf("observer panic changed Info(): %v", err)
	}
}
