package opensearch_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestInfoClassifiesCancellationTransportAndOverload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		ctx       func() context.Context
		response  *http.Response
		transport error
		category  adapter.FailureCategory
		status    int
		retryable bool
		cause     error
	}{
		{
			name: "cancelled", ctx: cancelledContext,
			category: adapter.FailureCancelled, cause: context.Canceled,
		},
		{
			name: "transport", ctx: context.Background,
			transport: errors.New("synthetic dial detail"),
			category:  adapter.FailureTransport, retryable: true,
		},
		{
			name: "429 overload", ctx: context.Background,
			response: errorResponse(http.StatusTooManyRequests, "rejected_execution_exception"),
			category: adapter.FailureOverloaded, status: http.StatusTooManyRequests,
			retryable: true,
		},
		{
			name: "503 overload", ctx: context.Background,
			response: errorResponse(http.StatusServiceUnavailable, "cluster_manager_not_discovered_exception"),
			category: adapter.FailureOverloaded, status: http.StatusServiceUnavailable,
			retryable: true,
		},
		{
			name: "cluster block", ctx: context.Background,
			response: errorResponse(http.StatusForbidden, "cluster_block_exception"),
			category: adapter.FailureClusterBlocked, status: http.StatusForbidden,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			transport := &observedTransport{response: test.response, err: test.transport}
			client, err := adapter.New(adapter.Config{
				Endpoints: []string{"https://search.example.test"},
				Transport: transport, TransportOwnership: adapter.TransportBorrowed,
				RequestTimeout: time.Second, MaximumResponseBytes: 4 << 10,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })

			_, infoErr := client.Info(test.ctx())
			var failure *adapter.Failure
			if !errors.As(infoErr, &failure) {
				t.Fatalf("Info() error = %T %v, want *Failure", infoErr, infoErr)
			}
			if failure.Operation != adapter.OperationInfo ||
				failure.Category != test.category || failure.Status != test.status ||
				failure.Retryable != test.retryable || !failure.OutcomeKnown {
				t.Fatalf("Info() failure = %#v", failure)
			}
			if test.cause != nil && !errors.Is(infoErr, test.cause) {
				t.Fatalf("Info() error = %v, want cause %v", infoErr, test.cause)
			}
			if strings.Contains(infoErr.Error(), "synthetic") ||
				strings.Contains(infoErr.Error(), "search.example.test") {
				t.Fatalf("Info() error leaked transport data: %v", infoErr)
			}
		})
	}
}

func TestInfoClassifiesMalformedErrorBodiesWithoutEchoingThem(t *testing.T) {
	t.Parallel()

	response := &http.Response{
		StatusCode: http.StatusBadRequest,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			`{"error":{"type":"not safe detail\nsecret","reason":"query value"}}`,
		)),
	}
	client, err := adapter.New(adapter.Config{
		Endpoints:          []string{"https://search.example.test"},
		Transport:          &observedTransport{response: response},
		TransportOwnership: adapter.TransportBorrowed,
		RequestTimeout:     time.Second, MaximumResponseBytes: 4 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	_, infoErr := client.Info(t.Context())
	var failure *adapter.Failure
	if !errors.As(infoErr, &failure) ||
		failure.Category != adapter.FailureRejected ||
		failure.Code != "unknown" || failure.Status != http.StatusBadRequest {
		t.Fatalf("Info() failure = %#v / %v", failure, infoErr)
	}
	if strings.Contains(infoErr.Error(), "query") || strings.Contains(infoErr.Error(), "secret") {
		t.Fatalf("Info() error leaked response: %v", infoErr)
	}
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	return ctx
}

func errorResponse(status int, errorType string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(
			`{"error":{"type":"` + errorType + `","reason":"sensitive detail"}}`,
		)),
	}
}
