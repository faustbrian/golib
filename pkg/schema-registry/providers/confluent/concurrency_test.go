package confluent

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderConcurrencyBudgetCancelsQueuedRequest(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	provider := internalProvider(t, roundTripperFunction(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-release
		return response(http.StatusOK, `{}`), nil
	}))

	first := make(chan error, 1)
	go func() {
		first <- provider.doJSON(context.Background(), http.MethodGet, "/schemas", nil, nil)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first request did not acquire provider budget")
	}

	waiterContext, cancel := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() {
		second <- provider.doJSON(waiterContext, http.MethodGet, "/schemas", nil, nil)
	}()
	cancel()
	select {
	case err := <-second:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("queued request error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued request ignored cancellation")
	}
	if calls.Load() != 1 {
		t.Fatalf("transport calls while budget occupied = %d", calls.Load())
	}

	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first request error = %v", err)
	}
}
