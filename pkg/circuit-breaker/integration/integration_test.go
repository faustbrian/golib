package integration_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	breaker "github.com/faustbrian/golib/pkg/circuit-breaker"
)

var (
	errDatabaseUnavailable = errors.New("database unavailable")
	errDependencyQueueFull = errors.New("dependency queue full")
	errLocalQueueFull      = errors.New("local queue full")
)

func TestHTTPLogicalOperationClassificationAndBodyOwnership(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte("temporarily unavailable"))
	}))
	t.Cleanup(server.Close)

	circuit := newFailureCircuit(t, func(completion breaker.Completion) breaker.Outcome {
		if completion.Err != nil {
			return breaker.OutcomeFailure
		}
		response, ok := completion.Result.(*http.Response)
		if ok && response.StatusCode >= http.StatusInternalServerError {
			return breaker.OutcomeFailure
		}
		return breaker.OutcomeSuccess
	})

	response, err := breaker.Execute(context.Background(), circuit, func(ctx context.Context) (*http.Response, error) {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		if requestErr != nil {
			return nil, requestErr
		}
		return server.Client().Do(request)
	})
	if err != nil {
		t.Fatalf("execute HTTP request: %v", err)
	}
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusServiceUnavailable)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatalf("drain response body: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}

	invoked := false
	_, err = breaker.Execute(context.Background(), circuit, func(context.Context) (*http.Response, error) {
		invoked = true
		return nil, nil
	})
	if !errors.Is(err, breaker.ErrOpen) {
		t.Fatalf("second request error = %v, want ErrOpen", err)
	}
	if invoked {
		t.Fatal("open breaker invoked HTTP operation")
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("HTTP requests = %d, want 1", got)
	}
}

func TestDatabaseCallPreservesDriverError(t *testing.T) {
	t.Parallel()

	circuit := newFailureCircuit(t, nil)
	rows, err := breaker.Execute(context.Background(), circuit, func(context.Context) (int64, error) {
		return 0, errDatabaseUnavailable
	})
	if rows != 0 {
		t.Fatalf("rows = %d, want 0", rows)
	}
	if !errors.Is(err, errDatabaseUnavailable) {
		t.Fatalf("query error = %v, want original database error", err)
	}
	if circuit.Snapshot().State != breaker.StateOpen {
		t.Fatalf("state = %s, want open", circuit.Snapshot().State)
	}
}

func TestQueueClassifierIgnoresLocalBackpressure(t *testing.T) {
	t.Parallel()

	circuit := newFailureCircuit(t, func(completion breaker.Completion) breaker.Outcome {
		switch {
		case errors.Is(completion.Err, errLocalQueueFull):
			return breaker.OutcomeIgnored
		case completion.Err != nil:
			return breaker.OutcomeFailure
		default:
			return breaker.OutcomeSuccess
		}
	})

	_, err := breaker.Execute(context.Background(), circuit, func(context.Context) (string, error) {
		return "", errLocalQueueFull
	})
	if !errors.Is(err, errLocalQueueFull) {
		t.Fatalf("local queue error = %v, want original error", err)
	}
	snapshot := circuit.Snapshot()
	if snapshot.State != breaker.StateClosed || snapshot.WindowClassified != 0 {
		t.Fatalf("ignored outcome snapshot = %+v", snapshot)
	}

	_, err = breaker.Execute(context.Background(), circuit, func(context.Context) (string, error) {
		return "", errDependencyQueueFull
	})
	if !errors.Is(err, errDependencyQueueFull) {
		t.Fatalf("dependency queue error = %v, want original error", err)
	}
	if circuit.Snapshot().State != breaker.StateOpen {
		t.Fatalf("state = %s, want open", circuit.Snapshot().State)
	}
}

func newFailureCircuit(t *testing.T, classifier breaker.Classifier) *breaker.Breaker {
	t.Helper()

	circuit, err := breaker.New(breaker.Config{
		Name:              t.Name(),
		MinimumThroughput: 1,
		Opening: &breaker.OpeningRules{
			FailureCount: 1,
		},
		OpenDuration: breaker.FixedOpenDuration(time.Minute),
		HalfOpen: &breaker.HalfOpenPolicy{
			MaxProbes:         1,
			RequiredSuccesses: 1,
		},
		Classifier: classifier,
	})
	if err != nil {
		t.Fatalf("new breaker: %v", err)
	}
	return circuit
}
