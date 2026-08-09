package opensearch_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestAdmissionRejectsExcessWorkWithoutReachingTransport(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		close(entered)
		<-release
		return jsonResponse(http.StatusOK, `{"name":"node-a","cluster_name":"search","cluster_uuid":"cluster-a","version":{"number":"3.6.0"}}`), nil
	})
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, Transport: transport, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Resilience: adapter.ResilienceConfig{MaximumInFlight: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	first := make(chan error, 1)
	go func() { _, callErr := client.Info(t.Context()); first <- callErr }()
	<-entered
	_, secondErr := client.Info(t.Context())
	var failure *adapter.Failure
	if !errors.As(secondErr, &failure) || failure.Category != adapter.FailureBackpressure || failure.OutcomeKnown {
		t.Fatalf("second Info() error = %#v", secondErr)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first Info() error = %v", err)
	}
	snapshot := client.ResilienceSnapshot()
	if snapshot.MaximumInFlight != 1 || snapshot.Rejections != 1 || snapshot.InFlight != 0 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestCircuitBreakerStopsOverloadAmplificationUntilCooldown(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"type":"rejected_execution_exception"}}`))}, nil
	})
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, Transport: transport, TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Resilience: adapter.ResilienceConfig{MaximumInFlight: 2, CircuitFailureThreshold: 2, CircuitOpenDuration: time.Minute, Clock: func() time.Time { return now }},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for range 2 {
		_, _ = client.Info(t.Context())
	}
	_, err = client.Info(t.Context())
	var failure *adapter.Failure
	if !errors.As(err, &failure) || failure.Category != adapter.FailureCircuitOpen || failure.OutcomeKnown || calls.Load() != 2 {
		t.Fatalf("open circuit result = %#v, calls = %d", err, calls.Load())
	}
}
