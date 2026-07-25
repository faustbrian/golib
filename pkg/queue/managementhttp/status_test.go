package managementhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
)

func TestClientReadsAuthenticatedBoundedStatus(t *testing.T) {
	t.Parallel()

	source := &statusReaderStub{
		workers: management.WorkerStatusPage{
			Items: []management.WorkerStatus{validWorkerStatus()}, NextCursor: "workers-next",
		},
		queues: management.QueueStatusPage{
			Items: []management.QueueStatus{validQueueStatus()}, NextCursor: "queues-next",
		},
	}
	handler, err := NewHandler(HandlerConfig{Token: "transport-secret", Status: source})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := NewClient(ClientConfig{
		BaseURL: server.URL, Token: "transport-secret", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	request := management.StatusPageRequest{Cursor: "current", Limit: 25}
	workers, err := client.ListWorkers(context.Background(), request)
	if err != nil || !reflect.DeepEqual(workers, source.workers) {
		t.Fatalf("ListWorkers() = (%+v, %v)", workers, err)
	}
	queues, err := client.ListQueues(context.Background(), request)
	if err != nil || !reflect.DeepEqual(queues, source.queues) {
		t.Fatalf("ListQueues() = (%+v, %v)", queues, err)
	}
	if source.workerRequest != request || source.queueRequest != request {
		t.Fatalf("source requests = (%+v, %+v)", source.workerRequest, source.queueRequest)
	}
}

func TestHandlerRejectsUnauthorizedAndUnsafeStatusRequests(t *testing.T) {
	t.Parallel()

	sourceErr := errors.New("redis password=secret")
	tests := map[string]struct {
		token      string
		target     string
		source     *statusReaderStub
		wantStatus int
		wantCalls  int
	}{
		"missing token":  {target: "/v1/status/workers?limit=1", source: &statusReaderStub{}, wantStatus: http.StatusUnauthorized},
		"wrong token":    {token: "wrong", target: "/v1/status/queues?limit=1", source: &statusReaderStub{}, wantStatus: http.StatusUnauthorized},
		"invalid query":  {token: "transport-secret", target: "/v1/status/workers?limit=0", source: &statusReaderStub{}, wantStatus: http.StatusBadRequest},
		"unknown query":  {token: "transport-secret", target: "/v1/status/queues?limit=1&search=x", source: &statusReaderStub{}, wantStatus: http.StatusBadRequest},
		"source error":   {token: "transport-secret", target: "/v1/status/workers?limit=1", source: &statusReaderStub{err: sourceErr}, wantStatus: http.StatusInternalServerError, wantCalls: 1},
		"invalid output": {token: "transport-secret", target: "/v1/status/queues?limit=1", source: &statusReaderStub{queues: management.QueueStatusPage{Items: []management.QueueStatus{{Queue: "invalid"}}}}, wantStatus: http.StatusInternalServerError, wantCalls: 1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler, err := NewHandler(HandlerConfig{Token: "transport-secret", Status: tt.source})
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			request := httptest.NewRequest(http.MethodGet, tt.target, nil)
			if tt.token != "" {
				request.Header.Set("Authorization", "Bearer "+tt.token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.wantStatus || tt.source.calls != tt.wantCalls ||
				strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("response = %d %s, calls %d", response.Code, response.Body.String(), tt.source.calls)
			}
		})
	}
}

func TestHandlerUsesStableStatusWireNames(t *testing.T) {
	t.Parallel()

	handler, err := NewHandler(HandlerConfig{
		Token: "transport-secret",
		Status: &statusReaderStub{queues: management.QueueStatusPage{
			Items: []management.QueueStatus{validQueueStatus()}, NextCursor: "next",
		}},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/status/queues?limit=1", nil)
	request.Header.Set("Authorization", "Bearer transport-secret")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK || !strings.Contains(body, `"next_cursor":"next"`) ||
		!strings.Contains(body, `"oldest_age":{"value":60000000000,"supported":true}`) ||
		strings.Contains(body, "NextCursor") {
		t.Fatalf("response = %d %s", response.Code, body)
	}
}

func TestClientRejectsInvalidStatusWithoutNetwork(t *testing.T) {
	t.Parallel()

	calls := 0
	client, err := NewClient(ClientConfig{
		BaseURL: "https://worker.example", Token: "transport-secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("unexpected network")
		})},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.ListWorkers(context.Background(), management.StatusPageRequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListWorkers() error = %v", err)
	}
	if _, err := client.ListQueues(context.Background(), management.StatusPageRequest{Limit: management.MaxStatusPageSize + 1}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListQueues() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("network calls = %d, want 0", calls)
	}
}

func TestClientFailsClosedOnInvalidAndOversizedResponses(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body        string
		status      int
		maxResponse int64
		wantErr     error
	}{
		"API error":      {body: `{"code":"internal_error"}`, status: http.StatusInternalServerError, wantErr: ErrRemoteFailure},
		"invalid JSON":   {body: `{`, status: http.StatusOK, wantErr: ErrInvalidResponse},
		"empty page":     {body: `{}`, status: http.StatusOK, wantErr: nil},
		"unknown field":  {body: `{"unexpected":true}`, status: http.StatusOK, wantErr: ErrInvalidResponse},
		"invalid page":   {body: `{"Items":[{"Queue":"invalid"}]}`, status: http.StatusOK, wantErr: ErrInvalidResponse},
		"trailing JSON":  {body: `{} {}`, status: http.StatusOK, wantErr: ErrInvalidResponse},
		"response bound": {body: strings.Repeat("x", 32), status: http.StatusOK, maxResponse: 8, wantErr: ErrResponseTooLarge},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tt.status)
				_, _ = writer.Write([]byte(tt.body))
			}))
			defer server.Close()
			client, err := NewClient(ClientConfig{
				BaseURL: server.URL, Token: "transport-secret", HTTPClient: server.Client(),
				MaxResponseBytes: tt.maxResponse,
			})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			_, err = client.ListQueues(context.Background(), management.StatusPageRequest{Limit: 1})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ListQueues() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestClientRejectsInvalidWorkerPage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"items":[{"id":"invalid"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{
		BaseURL: server.URL, Token: "transport-secret", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.ListWorkers(context.Background(), management.StatusPageRequest{Limit: 1}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("ListWorkers() error = %v", err)
	}
}

func TestClientHandlesTransportContextAndReadFailures(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("dial includes secret")
	client, err := NewClient(ClientConfig{
		BaseURL: "https://worker.example", Token: "transport-secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportErr
		})},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	request := management.StatusPageRequest{Limit: 1}
	if _, err := client.ListQueues(context.Background(), request); !errors.Is(err, ErrRemoteFailure) ||
		errors.Is(err, transportErr) {
		t.Fatalf("transport error = %v", err)
	}
	//lint:ignore SA1012 Public boundary must reject a nil context safely.
	//nolint:staticcheck // Public boundary must reject a nil context safely.
	if _, err := client.ListWorkers(nil, request); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil context error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ListQueues(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}

	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       &errorReadCloser{err: errors.New("read failed")},
			Header:     make(http.Header),
		}, nil
	})}
	if _, err := client.ListQueues(context.Background(), request); !errors.Is(err, ErrRemoteFailure) {
		t.Fatalf("read error = %v", err)
	}
}

func TestHandlerRejectsEveryMalformedStatusQueryShape(t *testing.T) {
	t.Parallel()

	handler, err := NewHandler(HandlerConfig{Token: "transport-secret", Status: &statusReaderStub{}})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	for _, target := range []string{
		"/v1/status/queues",
		"/v1/status/queues?limit=",
		"/v1/status/queues?limit=many",
		"/v1/status/queues?limit=1&cursor=",
		"/v1/status/queues?limit=1&limit=2",
	} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.Header.Set("Authorization", "Bearer transport-secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || decodeProblem(t, response) != "invalid_request" {
			t.Fatalf("%s response = %d %s", target, response.Code, response.Body.String())
		}
	}
}

func TestTransportUsesSafeDefaultsAndValueReader(t *testing.T) {
	t.Parallel()

	client, err := NewClient(ClientConfig{BaseURL: "https://worker.example", Token: "token"})
	if err != nil || client.httpClient.Timeout != 30*time.Second {
		t.Fatalf("NewClient() = (%+v, %v)", client, err)
	}
	handler, err := NewHandler(HandlerConfig{Token: "token", Status: valueStatusReader{}})
	if err != nil || handler == nil {
		t.Fatalf("NewHandler() = (%v, %v)", handler, err)
	}
}

func TestTransportRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	var typedStatus *statusReaderStub
	for _, config := range []HandlerConfig{
		{},
		{Token: "token"},
		{Token: "token", Status: typedStatus},
	} {
		if handler, err := NewHandler(config); handler != nil || !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("NewHandler() = (%v, %v)", handler, err)
		}
	}
	for _, config := range []ClientConfig{
		{},
		{BaseURL: "://invalid", Token: "token"},
		{BaseURL: "ftp://worker.example", Token: "token"},
		{BaseURL: "https://worker.example/path", Token: "token"},
		{BaseURL: "https://worker.example", Token: ""},
		{BaseURL: "https://worker.example", Token: "token", MaxResponseBytes: -1},
	} {
		if client, err := NewClient(config); client != nil || !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("NewClient() = (%v, %v)", client, err)
		}
	}
}

func validWorkerStatus() management.WorkerStatus {
	return management.WorkerStatus{
		ID: "worker-1", Version: "v1.0.0", StartedAt: time.Unix(1, 0).UTC(),
		HeartbeatAt: time.Unix(2, 0).UTC(), Queues: []string{"critical"},
		Concurrency: 1, State: management.WorkerRunning,
		DrainStatus: management.DrainNotRequested, Backend: "valkey-streams",
		Protocol:     management.ProtocolVersion{Major: 1},
		Capabilities: []management.Capability{management.CapabilityQueueStatus},
	}
}

func validQueueStatus() management.QueueStatus {
	return management.QueueStatus{
		Backend: "valkey-streams", Queue: "critical", ObservedAt: time.Unix(2, 0).UTC(),
		Metrics: management.QueueMetrics{
			Depth:            management.Measurement[int64]{Value: 2, Supported: true},
			Lag:              management.Measurement[int64]{Value: 1, Supported: true},
			Pending:          management.Measurement[int64]{Value: 1, Supported: true},
			OldestAge:        management.Measurement[time.Duration]{Value: time.Minute, Supported: true},
			Throughput:       management.Measurement[float64]{Value: 2.5, Supported: true},
			Runtime:          management.Measurement[time.Duration]{Value: time.Second, Supported: true},
			Succeeded:        management.Measurement[uint64]{Value: 10, Supported: true},
			Failed:           management.Measurement[uint64]{Value: 1, Supported: true},
			Retried:          management.Measurement[uint64]{Value: 2, Supported: true},
			Reclaimed:        management.Measurement[uint64]{Value: 3, Supported: true},
			DeadLettered:     management.Measurement[uint64]{Value: 4, Supported: true},
			SettlementErrors: management.Measurement[uint64]{Value: 5, Supported: true},
		},
	}
}

type statusReaderStub struct {
	workers       management.WorkerStatusPage
	queues        management.QueueStatusPage
	err           error
	workerRequest management.StatusPageRequest
	queueRequest  management.StatusPageRequest
	calls         int
}

func (s *statusReaderStub) ListWorkers(
	_ context.Context,
	request management.StatusPageRequest,
) (management.WorkerStatusPage, error) {
	s.calls++
	s.workerRequest = request
	return s.workers, s.err
}

func (s *statusReaderStub) ListQueues(
	_ context.Context,
	request management.StatusPageRequest,
) (management.QueueStatusPage, error) {
	s.calls++
	s.queueRequest = request
	return s.queues, s.err
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type errorReadCloser struct{ err error }

var _ io.ReadCloser = (*errorReadCloser)(nil)

func (r *errorReadCloser) Read([]byte) (int, error) { return 0, r.err }
func (*errorReadCloser) Close() error               { return nil }

type valueStatusReader struct{}

func (valueStatusReader) ListWorkers(
	context.Context,
	management.StatusPageRequest,
) (management.WorkerStatusPage, error) {
	return management.WorkerStatusPage{}, nil
}

func (valueStatusReader) ListQueues(
	context.Context,
	management.StatusPageRequest,
) (management.QueueStatusPage, error) {
	return management.QueueStatusPage{}, nil
}

func decodeProblem(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return problem.Code
}
