package managementhttp

import (
	"context"
	"encoding/json"
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

func TestClientReadsAuthenticatedBoundedRecords(t *testing.T) {
	t.Parallel()

	request := validRecordPageRequest()
	failure := validManagementRecord(management.RecordFailure, management.PayloadHidden)
	deadLetter := validManagementRecord(management.RecordDeadLetter, management.PayloadHidden)
	revealed := validManagementRecord(management.RecordDeadLetter, management.PayloadRevealed)
	reader := &recordReaderHTTPStub{
		failures:    management.RecordPage{Items: []management.JobRecord{failure}, NextCursor: "failure-next"},
		deadLetters: management.RecordPage{Items: []management.JobRecord{deadLetter}, NextCursor: "dead-next"},
		record:      revealed,
	}
	handler, err := NewHandler(HandlerConfig{Token: "transport-secret", Records: reader})
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

	failures, err := client.ListFailures(context.Background(), request)
	if err != nil || !reflect.DeepEqual(failures, reader.failures) {
		t.Fatalf("ListFailures() = (%+v, %v)", failures, err)
	}
	deadLetters, err := client.ListDeadLetters(context.Background(), request)
	if err != nil || !reflect.DeepEqual(deadLetters, reader.deadLetters) {
		t.Fatalf("ListDeadLetters() = (%+v, %v)", deadLetters, err)
	}
	inspect := management.InspectRequest{
		Kind: management.RecordDeadLetter, ID: "record-1",
		Visibility: management.PayloadRevealed,
	}
	record, err := client.Inspect(context.Background(), inspect)
	if err != nil || !reflect.DeepEqual(record, revealed) {
		t.Fatalf("Inspect() = (%+v, %v)", record, err)
	}
	if reader.failureRequest != request || reader.deadLetterRequest != request ||
		reader.inspectRequest != inspect {
		t.Fatalf("reader requests = %+v %+v %+v", reader.failureRequest, reader.deadLetterRequest, reader.inspectRequest)
	}
}

func TestLegacyRecordWireOmitsV1EnvelopeFields(t *testing.T) {
	t.Parallel()

	record := management.JobRecord{
		Kind: management.RecordFailure, ID: "record-1", Backend: "valkey-streams",
		Queue: "critical", OccurredAt: time.Unix(2, 0).UTC(), Attempts: 1,
		FailureCode: "handler_failed",
	}
	encoded, err := json.Marshal(transportRecord(record))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, field := range []string{"envelope_version", "classification", "diagnostics"} {
		if strings.Contains(string(encoded), `"`+field+`"`) {
			t.Fatalf("legacy record JSON contains v1 field %q: %s", field, encoded)
		}
	}
}

func TestRecordTransportRejectsUnsafeRequestsAndOutput(t *testing.T) {
	t.Parallel()

	sourceErr := errors.New("valkey password=secret")
	tests := map[string]struct {
		target     string
		token      string
		reader     *recordReaderHTTPStub
		wantStatus int
		wantCalls  int
	}{
		"unauthorized":       {target: "/v1/records/failures?limit=1&sort=occurred_at&direction=desc", reader: &recordReaderHTTPStub{}, wantStatus: http.StatusUnauthorized},
		"invalid list":       {target: "/v1/records/failures?limit=0&sort=occurred_at&direction=desc", token: "transport-secret", reader: &recordReaderHTTPStub{}, wantStatus: http.StatusBadRequest},
		"invalid limit":      {target: "/v1/records/failures?limit=many&sort=occurred_at&direction=desc", token: "transport-secret", reader: &recordReaderHTTPStub{}, wantStatus: http.StatusBadRequest},
		"unknown query":      {target: "/v1/records/dead-letters?limit=1&sort=occurred_at&direction=desc&raw=true", token: "transport-secret", reader: &recordReaderHTTPStub{}, wantStatus: http.StatusBadRequest},
		"missing visibility": {target: "/v1/records/failures/record-1", token: "transport-secret", reader: &recordReaderHTTPStub{}, wantStatus: http.StatusBadRequest},
		"invalid visibility": {target: "/v1/records/failures/record-1?visibility=raw", token: "transport-secret", reader: &recordReaderHTTPStub{}, wantStatus: http.StatusBadRequest},
		"empty ID":           {target: "/v1/records/failures/%20?visibility=hidden", token: "transport-secret", reader: &recordReaderHTTPStub{}, wantStatus: http.StatusBadRequest},
		"source error":       {target: "/v1/records/failures?limit=1&sort=occurred_at&direction=desc", token: "transport-secret", reader: &recordReaderHTTPStub{err: sourceErr}, wantStatus: http.StatusInternalServerError, wantCalls: 1},
		"invalid page":       {target: "/v1/records/dead-letters?limit=1&sort=occurred_at&direction=desc", token: "transport-secret", reader: &recordReaderHTTPStub{deadLetters: management.RecordPage{Items: []management.JobRecord{{}}}}, wantStatus: http.StatusInternalServerError, wantCalls: 1},
		"invalid record":     {target: "/v1/records/dead-letters/record-1?visibility=hidden", token: "transport-secret", reader: &recordReaderHTTPStub{}, wantStatus: http.StatusInternalServerError, wantCalls: 1},
		"wrong record kind":  {target: "/v1/records/failures/record-1?visibility=hidden", token: "transport-secret", reader: &recordReaderHTTPStub{record: validManagementRecord(management.RecordDeadLetter, management.PayloadHidden)}, wantStatus: http.StatusInternalServerError, wantCalls: 1},
		"overexposed record": {target: "/v1/records/failures/record-1?visibility=hidden", token: "transport-secret", reader: &recordReaderHTTPStub{record: validManagementRecord(management.RecordFailure, management.PayloadRedacted)}, wantStatus: http.StatusInternalServerError, wantCalls: 1},
		"overexposed page":   {target: "/v1/records/failures?limit=1&sort=occurred_at&direction=desc", token: "transport-secret", reader: &recordReaderHTTPStub{failures: management.RecordPage{Items: []management.JobRecord{validManagementRecord(management.RecordFailure, management.PayloadRedacted)}}}, wantStatus: http.StatusInternalServerError, wantCalls: 1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			handler, err := NewHandler(HandlerConfig{Token: "transport-secret", Records: tt.reader})
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			request := httptest.NewRequest(http.MethodGet, tt.target, nil)
			if tt.token != "" {
				request.Header.Set("Authorization", "Bearer "+tt.token)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.wantStatus || tt.reader.calls != tt.wantCalls ||
				strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("response = %d %s, calls = %d", response.Code, response.Body.String(), tt.reader.calls)
			}
		})
	}
}

func TestClientRejectsInvalidRecordRequestsWithoutNetwork(t *testing.T) {
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
	if _, err := client.ListFailures(context.Background(), management.PageRequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListFailures() error = %v", err)
	}
	if _, err := client.ListDeadLetters(context.Background(), management.PageRequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ListDeadLetters() error = %v", err)
	}
	if _, err := client.Inspect(context.Background(), management.InspectRequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Inspect() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("network calls = %d", calls)
	}
}

func TestRecordTransportPreservesStableManagementErrors(t *testing.T) {
	t.Parallel()

	stable := []error{
		management.ErrRecordNotFound,
		management.ErrUnsupportedCapability,
		management.ErrManagementUnavailable,
		management.ErrMalformedCursor,
		management.ErrInvalidFilter,
		management.ErrStaleRecord,
		management.ErrMutationConflict,
		management.ErrPartialMutation,
		management.ErrUnknownMutation,
	}
	for _, target := range stable {
		t.Run(target.Error(), func(t *testing.T) {
			t.Parallel()

			reader := &recordReaderHTTPStub{err: target}
			handler, err := NewHandler(HandlerConfig{Token: "transport-secret", Records: reader})
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

			_, err = client.Inspect(context.Background(), management.InspectRequest{
				Kind: management.RecordFailure, ID: "record-1",
				Visibility: management.PayloadHidden,
			})
			if !errors.Is(err, target) {
				t.Fatalf("Inspect() error = %v, want %v", err, target)
			}
		})
	}
}

func TestClientFailsClosedOnUnsafeRecordResponses(t *testing.T) {
	t.Parallel()

	validPage, err := json.Marshal(transportRecordPage(management.RecordPage{
		Items: []management.JobRecord{validManagementRecord(management.RecordFailure, management.PayloadHidden)},
	}))
	if err != nil {
		t.Fatalf("marshal valid page: %v", err)
	}
	overexposedPage, err := json.Marshal(transportRecordPage(management.RecordPage{
		Items: []management.JobRecord{validManagementRecord(management.RecordFailure, management.PayloadRedacted)},
	}))
	if err != nil {
		t.Fatalf("marshal overexposed page: %v", err)
	}
	tests := map[string]struct {
		body        string
		status      int
		maxResponse int64
		wantErr     error
	}{
		"remote failure":    {body: `{}`, status: http.StatusInternalServerError, wantErr: ErrRemoteFailure},
		"malformed problem": {body: `{`, status: http.StatusInternalServerError, wantErr: ErrRemoteFailure},
		"invalid JSON":      {body: `{`, status: http.StatusOK, wantErr: ErrInvalidResponse},
		"unknown field":     {body: `{"unknown":true}`, status: http.StatusOK, wantErr: ErrInvalidResponse},
		"trailing JSON":     {body: string(validPage) + `{}`, status: http.StatusOK, wantErr: ErrInvalidResponse},
		"response bound":    {body: strings.Repeat("x", 32), status: http.StatusOK, maxResponse: 8, wantErr: ErrResponseTooLarge},
		"overexposed page":  {body: string(overexposedPage), status: http.StatusOK, wantErr: ErrInvalidResponse},
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
			_, err = client.ListFailures(context.Background(), validRecordPageRequest())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ListFailures() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRecordClientHandlesConstructionTransportContextAndReadFailures(t *testing.T) {
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
	request := validRecordPageRequest()
	if _, err := client.ListFailures(context.Background(), request); !errors.Is(err, ErrRemoteFailure) {
		t.Fatalf("transport error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.ListFailures(ctx, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled error = %v", err)
	}
	//lint:ignore SA1012 Public boundary must reject a nil context safely.
	//nolint:staticcheck // Public boundary must reject a nil context safely.
	if _, err := client.Inspect(nil, management.InspectRequest{
		Kind: management.RecordFailure, ID: "record-1",
		Visibility: management.PayloadHidden,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("nil context error = %v", err)
	}
	client.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       &errorReadCloser{err: errors.New("read failed")},
			Header:     make(http.Header),
		}, nil
	})}
	if _, err := client.ListFailures(context.Background(), request); !errors.Is(err, ErrRemoteFailure) {
		t.Fatalf("read error = %v", err)
	}
	client.baseURL = &url.URL{Scheme: "http", Host: "invalid\nhost"}
	if _, err := client.Inspect(context.Background(), management.InspectRequest{
		Kind: management.RecordFailure, ID: "record-1",
		Visibility: management.PayloadHidden,
	}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("construction error = %v", err)
	}
}

func TestRecordVisibilityHelpersPreserveLeastPrivilege(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		requested management.PayloadVisibility
		actual    management.PayloadVisibility
		allowed   bool
	}{
		{management.PayloadHidden, management.PayloadHidden, true},
		{management.PayloadHidden, management.PayloadRedacted, false},
		{management.PayloadRedacted, management.PayloadHidden, true},
		{management.PayloadRedacted, management.PayloadRedacted, true},
		{management.PayloadRedacted, management.PayloadRevealed, false},
		{management.PayloadRevealed, management.PayloadHidden, true},
		{management.PayloadVisibility("raw"), management.PayloadHidden, false},
	} {
		if got := allowedVisibility(test.requested, test.actual); got != test.allowed {
			t.Fatalf("allowedVisibility(%q, %q) = %v", test.requested, test.actual, got)
		}
	}
	for input, want := range map[string]management.PayloadVisibility{
		"hidden":   management.PayloadHidden,
		"redacted": management.PayloadRedacted,
		"revealed": management.PayloadRevealed,
	} {
		visibility, ok := inspectVisibility(url.Values{"visibility": []string{input}})
		if !ok || visibility != want {
			t.Fatalf("inspectVisibility(%q) = (%q, %v)", input, visibility, ok)
		}
	}
}

func TestClientRejectsOverexposedInspectedRecord(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(writer).Encode(transportRecord(
			validManagementRecord(management.RecordFailure, management.PayloadRedacted),
		))
	}))
	defer server.Close()
	client, err := NewClient(ClientConfig{
		BaseURL: server.URL, Token: "transport-secret", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	_, err = client.Inspect(context.Background(), management.InspectRequest{
		Kind: management.RecordFailure, ID: "record-1",
		Visibility: management.PayloadHidden,
	})
	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Inspect() error = %v", err)
	}
}

func validRecordPageRequest() management.PageRequest {
	return management.PageRequest{
		Cursor: "current", Limit: 25, Search: "timeout",
		Sort: management.SortOccurredAt, Direction: management.SortDescending,
	}
}

func validManagementRecord(
	kind management.RecordKind,
	visibility management.PayloadVisibility,
) management.JobRecord {
	payload := management.Payload{Visibility: visibility, ContentType: "application/json", Size: 8}
	if visibility == management.PayloadRevealed {
		payload.Data = []byte(`{"id":1}`)
	}
	deadLetteredAt := time.Unix(2, 0).UTC()
	record := management.JobRecord{
		Kind: kind, ID: "record-1", Backend: "valkey-streams", Queue: "critical",
		OccurredAt: deadLetteredAt, Attempts: 3,
		FailureCode: "handler_failed", Payload: payload,
		EnvelopeVersion:      management.CurrentEnvelopeVersion,
		PayloadSchemaVersion: "order.v1", OriginalID: "job-1", Stream: "critical",
		ConsumerGroup: "workers", SourceRecordID: "1-0", DeadLetteredAt: &deadLetteredAt,
		RetryPolicy: "default-v1", Classification: management.ClassificationPermanent,
		FailureSummary: "handler rejected job", HandlerType: "CreateOrder",
		Tags: map[string]string{"region": "eu"}, TraceID: "trace-1", TenantID: "tenant-1",
		ProducerVersion: "1.2.0", WorkerVersion: "1.3.0",
	}
	if kind == management.RecordFailure {
		record.DeadLetteredAt = nil
	}

	return record
}

type recordReaderHTTPStub struct {
	failures          management.RecordPage
	deadLetters       management.RecordPage
	record            management.JobRecord
	err               error
	failureRequest    management.PageRequest
	deadLetterRequest management.PageRequest
	inspectRequest    management.InspectRequest
	calls             int
}

func (s *recordReaderHTTPStub) ListFailures(
	_ context.Context,
	request management.PageRequest,
) (management.RecordPage, error) {
	s.calls++
	s.failureRequest = request
	return s.failures, s.err
}

func (s *recordReaderHTTPStub) ListDeadLetters(
	_ context.Context,
	request management.PageRequest,
) (management.RecordPage, error) {
	s.calls++
	s.deadLetterRequest = request
	return s.deadLetters, s.err
}

func (s *recordReaderHTTPStub) Inspect(
	_ context.Context,
	request management.InspectRequest,
) (management.JobRecord, error) {
	s.calls++
	s.inspectRequest = request
	return s.record, s.err
}
