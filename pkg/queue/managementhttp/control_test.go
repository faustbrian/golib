package managementhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
)

func TestClientExecutesAuthenticatedBoundedCommand(t *testing.T) {
	t.Parallel()

	command := validCommand()
	want := validCommandResult()
	controller := &controllerStub{result: want}
	handler, err := NewHandler(HandlerConfig{
		Token: "transport-secret", Controller: controller,
	})
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

	got, err := client.Execute(context.Background(), command)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Execute() = (%+v, %v), want (%+v, nil)", got, err, want)
	}
	if !reflect.DeepEqual(controller.command, command) {
		t.Fatalf("controller command = %+v, want %+v", controller.command, command)
	}
}

func TestClientRejectsInvalidCommandWithoutNetwork(t *testing.T) {
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
	//lint:ignore SA1012 Public boundary must reject a nil context safely.
	//nolint:staticcheck // Public boundary must reject a nil context safely.
	if _, err := client.Execute(nil, validCommand()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Execute(nil) error = %v", err)
	}
	if _, err := client.Execute(context.Background(), management.Command{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Execute(invalid) error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("network calls = %d, want 0", calls)
	}
}

func TestHandlerRejectsUnauthorizedAndUnsafeCommands(t *testing.T) {
	t.Parallel()

	validBody := commandJSON(validCommand())
	tests := map[string]struct {
		token       string
		method      string
		contentType string
		body        string
		controller  *controllerStub
		wantStatus  int
		wantCalls   int
	}{
		"missing token":      {method: http.MethodPost, contentType: "application/json", body: validBody, controller: &controllerStub{}, wantStatus: http.StatusUnauthorized},
		"wrong token":        {token: "wrong", method: http.MethodPost, contentType: "application/json", body: validBody, controller: &controllerStub{}, wantStatus: http.StatusUnauthorized},
		"wrong content type": {token: "transport-secret", method: http.MethodPost, contentType: "text/plain", body: validBody, controller: &controllerStub{}, wantStatus: http.StatusUnsupportedMediaType},
		"empty body":         {token: "transport-secret", method: http.MethodPost, contentType: "application/json", controller: &controllerStub{}, wantStatus: http.StatusBadRequest},
		"invalid command":    {token: "transport-secret", method: http.MethodPost, contentType: "application/json", body: `{}`, controller: &controllerStub{}, wantStatus: http.StatusBadRequest},
		"unknown field":      {token: "transport-secret", method: http.MethodPost, contentType: "application/json", body: `{"unknown":true}`, controller: &controllerStub{}, wantStatus: http.StatusBadRequest},
		"trailing JSON":      {token: "transport-secret", method: http.MethodPost, contentType: "application/json", body: validBody + `{}`, controller: &controllerStub{}, wantStatus: http.StatusBadRequest},
		"oversized body":     {token: "transport-secret", method: http.MethodPost, contentType: "application/json", body: strings.Repeat("x", int(maxCommandRequestBytes+1)), controller: &controllerStub{}, wantStatus: http.StatusRequestEntityTooLarge},
		"controller error":   {token: "transport-secret", method: http.MethodPost, contentType: "application/json", body: validBody, controller: &controllerStub{err: errors.New("redis password=secret")}, wantStatus: http.StatusInternalServerError, wantCalls: 1},
		"invalid result":     {token: "transport-secret", method: http.MethodPost, contentType: "application/json", body: validBody, controller: &controllerStub{result: management.CommandResult{}}, wantStatus: http.StatusInternalServerError, wantCalls: 1},
		"mismatched result":  {token: "transport-secret", method: http.MethodPost, contentType: "application/json", body: validBody, controller: &controllerStub{result: mismatchedCommandResult()}, wantStatus: http.StatusInternalServerError, wantCalls: 1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler, err := NewHandler(HandlerConfig{
				Token: "transport-secret", Controller: tt.controller,
			})
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			request := httptest.NewRequest(tt.method, "/v1/commands", strings.NewReader(tt.body))
			if tt.token != "" {
				request.Header.Set("Authorization", "Bearer "+tt.token)
			}
			if tt.contentType != "" {
				request.Header.Set("Content-Type", tt.contentType)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.wantStatus || tt.controller.calls != tt.wantCalls ||
				strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("response = %d %s, calls %d", response.Code, response.Body.String(), tt.controller.calls)
			}
		})
	}
}

func TestHandlerBoundsChunkedCommandBody(t *testing.T) {
	t.Parallel()

	handler, err := NewHandler(HandlerConfig{
		Token: "transport-secret", Controller: &controllerStub{},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPost, "/v1/commands",
		strings.NewReader(strings.Repeat(" ", int(maxCommandRequestBytes+1))+`{}`),
	)
	request.ContentLength = -1
	request.Header.Set("Authorization", "Bearer transport-secret")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestClientRejectsUnsafeCommandResponses(t *testing.T) {
	t.Parallel()

	valid := resultJSON(validCommandResult())
	tests := map[string]struct {
		body        string
		status      int
		maxResponse int64
		wantErr     error
	}{
		"API error":         {body: `{"code":"internal_error"}`, status: http.StatusInternalServerError, wantErr: ErrRemoteFailure},
		"invalid JSON":      {body: `{`, status: http.StatusOK, wantErr: ErrInvalidResponse},
		"unknown field":     {body: `{"unknown":true}`, status: http.StatusOK, wantErr: ErrInvalidResponse},
		"trailing JSON":     {body: valid + `{}`, status: http.StatusOK, wantErr: ErrInvalidResponse},
		"invalid result":    {body: `{}`, status: http.StatusOK, wantErr: ErrInvalidResponse},
		"mismatched result": {body: resultJSON(mismatchedCommandResult()), status: http.StatusOK, wantErr: ErrInvalidResponse},
		"response bound":    {body: strings.Repeat("x", 32), status: http.StatusOK, maxResponse: 8, wantErr: ErrResponseTooLarge},
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
			_, err = client.Execute(context.Background(), validCommand())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Execute() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestCommandTransportPropagatesCancellation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	controller := &controllerStub{execute: func(ctx context.Context, _ management.Command) (management.CommandResult, error) {
		close(started)
		<-ctx.Done()
		return management.CommandResult{}, ctx.Err()
	}}
	handler, err := NewHandler(HandlerConfig{Token: "transport-secret", Controller: controller})
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
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, executeErr := client.Execute(ctx, validCommand())
		done <- executeErr
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute() cancellation error = %v", err)
	}
}

func TestClientHandlesCommandConstructionTransportAndReadFailures(t *testing.T) {
	t.Parallel()

	client, err := NewClient(ClientConfig{
		BaseURL: "https://worker.example", Token: "transport-secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial includes secret")
		})},
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if _, err := client.Execute(context.Background(), validCommand()); !errors.Is(err, ErrRemoteFailure) {
		t.Fatalf("transport error = %v", err)
	}

	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       &errorReadCloser{err: errors.New("read failed")},
			Header:     make(http.Header),
		}, nil
	})}
	if _, err := client.Execute(context.Background(), validCommand()); !errors.Is(err, ErrRemoteFailure) {
		t.Fatalf("read error = %v", err)
	}

	client.baseURL = &url.URL{Scheme: "http", Host: "invalid\nhost"}
	if _, err := client.Execute(context.Background(), validCommand()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("request construction error = %v", err)
	}

	command := validCommand()
	command.RequestedAt = time.Date(10_000, 1, 1, 0, 0, 0, 0, time.UTC)
	command.Deadline = command.RequestedAt.Add(time.Minute)
	if _, err := client.Execute(context.Background(), command); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("marshal error = %v", err)
	}
}

func TestCommandWirePreservesOptionalSafeguards(t *testing.T) {
	t.Parallel()

	bulk := validCommand()
	bulk.Action = management.CommandBulkRetry
	bulk.Target = management.Target{Kind: management.TargetFailure, Name: "failed"}
	bulk.Confirmed = true
	bulk.Selection = &management.Selection{Limit: 25}
	replay := validCommand()
	replay.Action = management.CommandReplay
	replay.Target = management.Target{Kind: management.TargetDeadLetter, Name: "dead-1"}
	replay.Confirmed = true
	replay.Replay = &management.ReplayOptions{
		Destination: "recovery", IdempotencyPolicy: management.ReplayRejectDuplicate,
	}
	for _, command := range []management.Command{bulk, replay} {
		if got := managementCommand(transportCommand(command)); !reflect.DeepEqual(got, command) {
			t.Fatalf("wire round trip = %+v, want %+v", got, command)
		}
	}
}

func TestHandlerConfigurationAcceptsIndependentServices(t *testing.T) {
	t.Parallel()

	valid := []HandlerConfig{
		{Token: "secret", Status: &statusReaderStub{}},
		{Token: "secret", Controller: &controllerStub{}},
		{Token: "secret", Status: &statusReaderStub{}, Controller: &controllerStub{}},
	}
	for _, config := range valid {
		if handler, err := NewHandler(config); handler == nil || err != nil {
			t.Fatalf("NewHandler(%+v) = (%v, %v)", config, handler, err)
		}
	}
	for _, config := range []HandlerConfig{{}, {Token: "secret"}} {
		if handler, err := NewHandler(config); handler != nil || !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("NewHandler(%+v) = (%v, %v)", config, handler, err)
		}
	}
}

type controllerStub struct {
	command management.Command
	result  management.CommandResult
	err     error
	calls   int
	execute func(context.Context, management.Command) (management.CommandResult, error)
}

func (s *controllerStub) Execute(ctx context.Context, command management.Command) (management.CommandResult, error) {
	s.calls++
	s.command = command
	if s.execute != nil {
		return s.execute(ctx, command)
	}
	return s.result, s.err
}

func validCommand() management.Command {
	requestedAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	return management.Command{
		ID: "command-1", IdempotencyKey: "request-1", Actor: "operator-1",
		Reason: "drain for deployment", Protocol: management.ProtocolVersion{Major: 1},
		Action:      management.CommandDrain,
		Target:      management.Target{Kind: management.TargetWorker, Name: "worker-1"},
		RequestedAt: requestedAt, Deadline: requestedAt.Add(time.Minute),
	}
}

func validCommandResult() management.CommandResult {
	return management.CommandResult{
		CommandID: "command-1", IdempotencyKey: "request-1", WorkerID: "worker-1",
		Protocol: management.ProtocolVersion{Major: 1}, Status: management.CommandAcknowledged,
		CompletedAt: time.Date(2026, 7, 16, 10, 0, 1, 0, time.UTC),
	}
}

func mismatchedCommandResult() management.CommandResult {
	result := validCommandResult()
	result.CommandID = "another-command"
	return result
}

func commandJSON(command management.Command) string {
	return `{"id":"` + command.ID + `","idempotency_key":"` + command.IdempotencyKey +
		`","actor":"` + command.Actor + `","reason":"` + command.Reason +
		`","protocol":{"major":1,"minor":0},"action":"drain",` +
		`"target":{"kind":"worker","name":"worker-1"},` +
		`"requested_at":"2026-07-16T10:00:00Z","deadline":"2026-07-16T10:01:00Z",` +
		`"confirmed":false}`
}

func resultJSON(result management.CommandResult) string {
	return `{"command_id":"` + result.CommandID + `","idempotency_key":"` + result.IdempotencyKey +
		`","worker_id":"` + result.WorkerID + `","protocol":{"major":1,"minor":0},` +
		`"status":"acknowledged","failure_code":"","completed_at":"2026-07-16T10:00:01Z"}`
}
