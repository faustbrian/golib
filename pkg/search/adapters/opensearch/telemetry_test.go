package opensearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
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

func TestTelemetryEmitsUnknownWriteOutcomeSignal(t *testing.T) {
	t.Parallel()

	events := make(chan adapter.TelemetryEvent, 4)
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, TransportOwnership: adapter.TransportBorrowed,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection reset after dispatch")
		}),
		RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Telemetry: &adapter.TelemetryConfig{Observer: adapter.TelemetryObserverFunc(func(_ context.Context, event adapter.TelemetryEvent) error {
			events <- event
			return nil
		}), Clock: time.Now},
		Search: &adapter.SearchConfig{
			Limits: search.DefaultLimits(), CursorCodec: mustCursorCodec(t), Clock: time.Now,
			WriteGuard: allowWriteAuthorization(),
			Resolver: adapter.IndexResolverFunc(func(context.Context, string, string, adapter.IndexAccess) (adapter.IndexTarget, error) {
				return adapter.IndexTarget{Name: "tenant-a-events-v2", PhysicalName: "tenant-a-events-v2", Fingerprint: "mapping-v2-fingerprint"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	document, err := search.NewDocument("tenant-a", "events", "event-1", 7, json.RawMessage(`{"value":"safe"}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if outcome, writeErr := client.Write(t.Context(), search.IndexDocument(document), search.RefreshNone); writeErr == nil || outcome.State != search.OutcomeUnknown {
		t.Fatalf("Write() = %#v/%v", outcome, writeErr)
	}

	close(events)
	found := false
	for event := range events {
		if event.Signal == adapter.TelemetryUnknownWriteOutcome {
			found = event.Operation == adapter.OperationWrite && event.Status == 0
		}
	}
	if !found {
		t.Fatal("unknown write outcome telemetry signal was not emitted")
	}
}

func TestTelemetryEmitsCutoverFailureSignal(t *testing.T) {
	t.Parallel()

	events := make(chan adapter.TelemetryEvent, 2)
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("unexpected transport")
		}),
		TransportOwnership: adapter.TransportBorrowed,
		Telemetry: &adapter.TelemetryConfig{Observer: adapter.TelemetryObserverFunc(func(_ context.Context, event adapter.TelemetryEvent) error {
			events <- event
			return nil
		}), Clock: time.Now},
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }),
			CutoverGuard: adapter.LifecycleCutoverGuardFunc(func(context.Context, adapter.LifecycleCutoverRequest, func() error) error {
				return errors.New("guard did not run operation")
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, cutoverErr := client.CutoverAlias(t.Context(), "tenant-a", "events-read", "events-v1", "events-v2", "definition-v2"); cutoverErr == nil {
		t.Fatal("CutoverAlias() error = nil")
	}
	close(events)
	if !containsTelemetrySignal(events, adapter.TelemetryCutoverFailure) {
		t.Fatal("cutover failure telemetry signal was not emitted")
	}
}
