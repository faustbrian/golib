package apihttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/authz"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHandlerUsesBoundedGoTelemetryInstrumentation(t *testing.T) {
	t.Parallel()

	spans := tracetest.NewInMemoryExporter()
	traces := trace.NewTracerProvider(trace.WithSyncer(spans))
	reader := metric.NewManualReader()
	metrics := metric.NewMeterProvider(metric.WithReader(reader))
	handler, err := NewHandler(Config{
		Commands: &commandExecutorStub{},
		Telemetry: &TelemetryConfig{
			TracerProvider: traces,
			MeterProvider:  metrics,
		},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	finished := spans.GetSpans()
	if len(finished) != 1 || finished[0].Name != "queue_control_plane.http.server" {
		t.Fatalf("spans = %+v, want bounded server operation", finished)
	}
	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(data.ScopeMetrics) != 1 || len(data.ScopeMetrics[0].Metrics) != 2 {
		t.Fatalf("metrics = %+v, want request count and duration", data.ScopeMetrics)
	}
}

func TestHandlerExecutesTenantCommandForAuthenticatedActor(t *testing.T) {
	t.Parallel()

	requestedAt := time.Date(2026, time.July, 16, 12, 0, 0, 123456000, time.UTC)
	executor := &commandExecutorStub{result: controlplane.CommandResult{
		IdempotencyKey: "request-123",
		TenantID:       "tenant-1",
		Status:         controlplane.CommandSucceeded,
		CompletedAt:    requestedAt.Add(time.Second),
	}}
	limiter := &workflowLimiterStub{}
	handler, err := NewHandler(Config{
		Commands: executor, MaxRequestBytes: 4096, WorkflowLimiter: limiter,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	body := `{
		"idempotency_key":"request-123",
		"reason":"Deploy maintenance",
		"action":"drain",
		"target":{"kind":"worker_group","name":"payments"},
		"requested_at":"2026-07-16T12:00:00.123456Z"
	}`
	request := authenticatedRequest(t, http.MethodPost, "/v1/tenants/tenant-1/commands", body)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if executor.calls != 1 {
		t.Fatalf("Execute() calls = %d, want 1", executor.calls)
	}
	if !reflect.DeepEqual(limiter.keys, []string{"subject:operator-1|workflow:drain"}) {
		t.Fatalf("workflow keys = %v", limiter.keys)
	}
	want := controlplane.Command{
		IdempotencyKey:       "request-123",
		TenantID:             "tenant-1",
		Actor:                "operator-1",
		AuthenticationMethod: "bearer",
		Reason:               "Deploy maintenance",
		Action:               controlplane.ActionDrain,
		Target:               controlplane.Target{Kind: controlplane.TargetWorkerGroup, Name: "payments"},
		RequestedAt:          requestedAt,
	}
	if !commandsMatch(executor.command, want) {
		t.Fatalf("Execute() command = %+v, want %+v", executor.command, want)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	var result controlplane.CommandResult
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result != executor.result {
		t.Fatalf("response = %+v, want %+v", result, executor.result)
	}
}

func TestHandlerRateLimitsCommandWorkflowIndependently(t *testing.T) {
	t.Parallel()

	executor := &commandExecutorStub{}
	handler, err := NewHandler(Config{
		Commands: executor, WorkflowLimiter: &workflowLimiterStub{deny: true},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(
		t, http.MethodPost, "/v1/tenants/tenant-1/commands", `{
			"idempotency_key":"request-123",
			"reason":"incident response",
			"action":"retry",
			"target":{"kind":"failure","name":"failure-1"},
			"requested_at":"2026-07-16T12:00:00Z"
		}`,
	))
	if response.Code != http.StatusTooManyRequests || executor.calls != 0 ||
		response.Header().Get("Retry-After") != "1" {
		t.Fatalf("response = %d, calls = %d", response.Code, executor.calls)
	}
}

func TestHandlerSupportsDestructiveSafeguards(t *testing.T) {
	t.Parallel()

	executor := &commandExecutorStub{}
	handler, err := NewHandler(Config{Commands: executor})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	body := `{
		"idempotency_key":"request-123",
		"reason":"Recover selected failures",
		"action":"replay",
		"target":{"kind":"dead_letter","name":"dead-1"},
		"requested_at":"2026-07-16T12:00:00Z",
		"confirmed":true,
		"selection":{"limit":25},
		"replay":{"destination":"recovery","idempotency_policy":"reject_duplicate"}
	}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(t, http.MethodPost, "/v1/tenants/tenant-1/commands", body))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if executor.command.Selection == nil || executor.command.Selection.Limit != 25 ||
		executor.command.Replay == nil || executor.command.Replay.Destination != "recovery" {
		t.Fatalf("Execute() command safeguards = %+v, want selection and replay", executor.command)
	}
}

func TestHandlerSupportsScalingOptions(t *testing.T) {
	t.Parallel()

	executor := &commandExecutorStub{}
	handler, err := NewHandler(Config{Commands: executor})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	body := `{
		"idempotency_key":"request-123",
		"reason":"Increase capacity for the event",
		"action":"scale",
		"target":{"kind":"workload","name":"payments"},
		"requested_at":"2026-07-16T12:00:00Z",
		"scale":{"replicas":5}
	}`
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(t, http.MethodPost, "/v1/tenants/tenant-1/commands", body))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if executor.command.Scale == nil || executor.command.Scale.Replicas != 5 {
		t.Fatalf("Execute() scale = %+v, want 5 replicas", executor.command.Scale)
	}
}

func TestHandlerRejectsUnsafeCommandRequests(t *testing.T) {
	t.Parallel()

	largeReason := strings.Repeat("x", 256)
	tests := map[string]struct {
		request func(*testing.T) *http.Request
		status  int
	}{
		"missing principal": {
			request: func(*testing.T) *http.Request {
				return jsonRequest(http.MethodPost, "/v1/tenants/tenant-1/commands", `{}`)
			},
			status: http.StatusUnauthorized,
		},
		"wrong content type": {
			request: func(t *testing.T) *http.Request {
				request := authenticatedRequest(t, http.MethodPost, "/v1/tenants/tenant-1/commands", `{}`)
				request.Header.Set("Content-Type", "text/plain")
				return request
			},
			status: http.StatusUnsupportedMediaType,
		},
		"unknown field": {
			request: func(t *testing.T) *http.Request {
				return authenticatedRequest(t, http.MethodPost, "/v1/tenants/tenant-1/commands", `{"actor":"spoofed"}`)
			},
			status: http.StatusBadRequest,
		},
		"multiple values": {
			request: func(t *testing.T) *http.Request {
				return authenticatedRequest(t, http.MethodPost, "/v1/tenants/tenant-1/commands", `{}`+`{}`)
			},
			status: http.StatusBadRequest,
		},
		"oversized": {
			request: func(t *testing.T) *http.Request {
				return authenticatedRequest(t, http.MethodPost, "/v1/tenants/tenant-1/commands", `{"reason":"`+largeReason+`"}`)
			},
			status: http.StatusRequestEntityTooLarge,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			executor := &commandExecutorStub{}
			handler, err := NewHandler(Config{Commands: executor, MaxRequestBytes: 128})
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, tt.request(t))
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.status, response.Body.String())
			}
			if executor.calls != 0 {
				t.Fatalf("Execute() calls = %d, want 0", executor.calls)
			}
		})
	}
}

func TestHandlerMapsCommandFailuresWithoutLeakingCause(t *testing.T) {
	t.Parallel()

	databaseErr := errors.New("database password=secret endpoint=internal")
	tests := map[string]struct {
		err    error
		status int
		code   string
	}{
		"validation": {
			err:    &controlplane.ValidationError{Field: "target.name"},
			status: http.StatusBadRequest,
			code:   "invalid_request",
		},
		"unauthenticated": {err: authz.ErrUnauthenticated, status: http.StatusUnauthorized, code: "unauthenticated"},
		"denied":          {err: authz.ErrDenied, status: http.StatusForbidden, code: "forbidden"},
		"actor mismatch":  {err: authz.ErrActorMismatch, status: http.StatusForbidden, code: "forbidden"},
		"conflict":        {err: controlpostgres.ErrIdempotencyConflict, status: http.StatusConflict, code: "idempotency_conflict"},
		"unknown outcome": {err: control.ErrOutcomeUnknown, status: http.StatusServiceUnavailable, code: "outcome_unknown"},
		"internal":        {err: databaseErr, status: http.StatusInternalServerError, code: "internal_error"},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			handler, err := NewHandler(Config{Commands: &commandExecutorStub{err: tt.err}})
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			body := `{"idempotency_key":"request-123","reason":"maintenance","action":"pause","target":{"kind":"queue","name":"critical"},"requested_at":"2026-07-16T12:00:00Z"}`
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, authenticatedRequest(t, http.MethodPost, "/v1/tenants/tenant-1/commands", body))
			if response.Code != tt.status {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.status, response.Body.String())
			}
			encoded := append([]byte(nil), response.Body.Bytes()...)
			var problem Problem
			if err := json.Unmarshal(encoded, &problem); err != nil {
				t.Fatalf("decode problem: %v", err)
			}
			if problem.Code != tt.code {
				t.Fatalf("problem code = %q, want %q", problem.Code, tt.code)
			}
			if bytes.Contains(encoded, []byte("password=secret")) || bytes.Contains(encoded, []byte("endpoint=internal")) {
				t.Fatalf("problem leaked internal cause: %s", encoded)
			}
		})
	}
}

func TestNewHandlerRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []Config{
		{},
		{Commands: &commandExecutorStub{}, MaxRequestBytes: -1},
	}
	for _, config := range tests {
		if _, err := NewHandler(config); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("NewHandler() error = %v, want ErrInvalidConfiguration", err)
		}
	}
}

func TestHandlerExposesMachineReadableOperationalEndpoints(t *testing.T) {
	t.Parallel()

	ready := &readinessStub{}
	build := BuildInfo{
		Version: "1.2.3",
		Commit:  "abc123",
		BuiltAt: time.Unix(1, 0),
	}
	capabilities := []string{"commands", "fleet", "audit"}
	handler, err := NewHandler(Config{
		Commands:     &commandExecutorStub{},
		Readiness:    ready,
		Build:        build,
		Capabilities: capabilities,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	tests := map[string]struct {
		path   string
		status int
		body   string
	}{
		"liveness": {
			path:   "/health/live",
			status: http.StatusOK,
			body:   `{"status":"live"}`,
		},
		"readiness": {
			path:   "/health/ready",
			status: http.StatusOK,
			body:   `{"status":"ready"}`,
		},
		"version": {
			path:   "/version",
			status: http.StatusOK,
			body:   `{"version":"1.2.3","commit":"abc123","built_at":"1970-01-01T00:00:01Z"}`,
		},
		"capabilities": {
			path:   "/v1/capabilities",
			status: http.StatusOK,
			body:   `{"capabilities":["commands","fleet","audit"]}`,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if response.Code != tt.status || strings.TrimSpace(response.Body.String()) != tt.body {
				t.Fatalf("response = (%d, %s), want (%d, %s)", response.Code, response.Body.String(), tt.status, tt.body)
			}
		})
	}
	if ready.calls != 1 {
		t.Fatalf("Ready() calls = %d, want 1", ready.calls)
	}

	capabilities[0] = "mutated"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil))
	if strings.Contains(response.Body.String(), "mutated") {
		t.Fatalf("capability response retained caller slice: %s", response.Body.String())
	}
}

func TestHandlerReadinessFailsWithoutLeakingDependencyError(t *testing.T) {
	t.Parallel()

	ready := &readinessStub{err: errors.New("postgres password=secret")}
	handler, err := NewHandler(Config{Commands: &commandExecutorStub{}, Readiness: ready})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if response.Code != http.StatusServiceUnavailable ||
		strings.TrimSpace(response.Body.String()) != `{"status":"not_ready"}` {
		t.Fatalf("response = (%d, %s), want secret-safe 503", response.Code, response.Body.String())
	}
}

func TestHandlerReadinessDefaultsToReady(t *testing.T) {
	t.Parallel()

	handler, err := NewHandler(Config{Commands: &commandExecutorStub{}})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
}

func authenticatedRequest(t *testing.T, method string, target string, body string) *http.Request {
	t.Helper()

	principal, err := authentication.NewPrincipal(authentication.PrincipalSpec{
		Subject: "operator-1",
		Method:  "bearer",
	})
	if err != nil {
		t.Fatalf("NewPrincipal() error = %v", err)
	}
	request := jsonRequest(method, target, body)
	request = request.WithContext(authentication.ContextWithPrincipal(request.Context(), principal))

	return request
}

func jsonRequest(method string, target string, body string) *http.Request {
	request := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")

	return request
}

func commandsMatch(left controlplane.Command, right controlplane.Command) bool {
	return left.IdempotencyKey == right.IdempotencyKey &&
		left.TenantID == right.TenantID &&
		left.Actor == right.Actor &&
		left.AuthenticationMethod == right.AuthenticationMethod &&
		left.Reason == right.Reason &&
		left.Action == right.Action &&
		left.Target == right.Target &&
		left.RequestedAt.Equal(right.RequestedAt)
}

type commandExecutorStub struct {
	result  controlplane.CommandResult
	err     error
	command controlplane.Command
	calls   int
}

type workflowLimiterStub struct {
	deny bool
	keys []string
}

func (s *workflowLimiterStub) Allow(_ context.Context, key string) bool {
	s.keys = append(s.keys, key)

	return !s.deny
}

type readinessStub struct {
	err   error
	calls int
}

func (s *readinessStub) Ready(context.Context) error {
	s.calls++

	return s.err
}

func (s *commandExecutorStub) Execute(
	_ context.Context,
	command controlplane.Command,
) (controlplane.CommandResult, error) {
	s.calls++
	s.command = command

	return s.result, s.err
}
