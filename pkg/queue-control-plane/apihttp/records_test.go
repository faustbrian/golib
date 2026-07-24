package apihttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/authz"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestHandlerListsAuthorizedFailuresAndDeadLetters(t *testing.T) {
	t.Parallel()

	for name, kind := range map[string]queue.RecordKind{
		"failures":     queue.RecordFailure,
		"dead-letters": queue.RecordDeadLetter,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := &recordSourceStub{page: queue.RecordPage{
				Items: []queue.JobRecord{apiJobRecord(kind)}, NextCursor: "next",
			}}
			viewer := &recordViewerStub{}
			handler, err := NewHandler(Config{
				Commands: &commandExecutorStub{}, Records: source, Viewer: viewer,
			})
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			target := "/v1/tenants/tenant-1/" + name +
				"?cursor=current&limit=25&search=critical&sort=queue&direction=asc"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, authenticatedRequest(t, http.MethodGet, target, ""))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
			var page RecordPage
			if err := json.NewDecoder(response.Body).Decode(&page); err != nil {
				t.Fatalf("decode page: %v", err)
			}
			if len(page.Records) != 1 || page.Records[0].Kind != kind ||
				page.Records[0].Payload.Visibility != queue.PayloadHidden ||
				page.NextCursor != "next" || strings.Contains(response.Body.String(), `"Data"`) {
				t.Fatalf("page = %+v, body = %s", page, response.Body.String())
			}
			if source.tenant != "tenant-1" || source.request != (queue.PageRequest{
				Cursor: "current", Limit: 25, Search: "critical",
				Sort: queue.SortQueue, Direction: queue.SortAscending,
			}) {
				t.Fatalf("source request = tenant %q %+v", source.tenant, source.request)
			}
			if len(viewer.calls) != 1 || viewer.calls[0].permission != controlplane.PermissionRecordList ||
				viewer.calls[0].target.Kind != targetKindForRecord(kind) {
				t.Fatalf("authorization = %+v", viewer.calls)
			}
		})
	}
}

func TestHandlerInspectsPayloadOnlyAfterExplicitPrivilege(t *testing.T) {
	t.Parallel()

	record := apiJobRecord(queue.RecordFailure)
	record.Payload = queue.Payload{
		Visibility: queue.PayloadRevealed, ContentType: "application/json",
		Size: 2, Data: []byte("{}"),
	}
	record.Diagnostics = queue.Payload{
		Visibility: queue.PayloadRevealed, ContentType: "text/plain",
		Size: 6, Data: []byte("secret"),
	}
	source := &recordSourceStub{record: record}
	viewer := &recordViewerStub{}
	auditor := &sensitiveAuditStub{}
	limiter := &workflowLimiterStub{}
	handler, err := NewHandler(Config{
		Commands: &commandExecutorStub{}, Records: source, Viewer: viewer,
		SensitiveAudit: auditor, WorkflowLimiter: limiter,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(
		t, http.MethodGet, "/v1/tenants/tenant-1/failures/failure-1?payload=revealed", "",
	))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Disposition") !=
		`attachment; filename="queue-record.json"` {
		t.Fatalf("content disposition = %q", response.Header().Get("Content-Disposition"))
	}
	var got Record
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("decode record: %v", err)
	}
	if got.ID != record.ID || string(got.Payload.Data) != "{}" ||
		len(got.Diagnostics.Data) != 0 || got.Diagnostics.Visibility != queue.PayloadHidden ||
		source.inspect.Visibility != queue.PayloadRevealed {
		t.Fatalf("record = %+v, request = %+v", got, source.inspect)
	}
	if len(viewer.calls) != 2 || viewer.calls[0].permission != controlplane.PermissionRecordInspect ||
		viewer.calls[1].permission != controlplane.PermissionPayloadView {
		t.Fatalf("authorization = %+v", viewer.calls)
	}
	if len(auditor.accesses) != 1 ||
		auditor.accesses[0].Permission != controlplane.PermissionPayloadView ||
		auditor.accesses[0].CommandID == "" {
		t.Fatalf("payload audit = %+v", auditor.accesses)
	}
	if !reflect.DeepEqual(limiter.keys, []string{
		"subject:operator-1|workflow:payload_view",
	}) {
		t.Fatalf("payload workflow keys = %v", limiter.keys)
	}

	viewer.calls = nil
	auditor.accesses = nil
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(
		t, http.MethodGet,
		"/v1/tenants/tenant-1/failures/failure-1?diagnostics=revealed", "",
	))
	got = Record{}
	if response.Code != http.StatusOK || json.NewDecoder(response.Body).Decode(&got) != nil ||
		string(got.Diagnostics.Data) != "secret" || got.Payload.Visibility != queue.PayloadHidden {
		t.Fatalf("diagnostics response = %d, record %+v", response.Code, got)
	}
	if len(viewer.calls) != 2 ||
		viewer.calls[1].permission != controlplane.PermissionDiagnosticsView {
		t.Fatalf("diagnostics authorization = %+v", viewer.calls)
	}
	if len(auditor.accesses) != 1 ||
		auditor.accesses[0].Permission != controlplane.PermissionDiagnosticsView {
		t.Fatalf("diagnostics audit = %+v", auditor.accesses)
	}
	if !reflect.DeepEqual(limiter.keys, []string{
		"subject:operator-1|workflow:payload_view",
		"subject:operator-1|workflow:diagnostics_view",
	}) {
		t.Fatalf("sensitive workflow keys = %v", limiter.keys)
	}
	source.record.Payload = queue.Payload{Visibility: queue.PayloadHidden, Size: 2}
	viewer.calls = nil
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(
		t, http.MethodGet, "/v1/tenants/tenant-1/failures/failure-1", "",
	))
	if response.Code != http.StatusOK || source.inspect.Visibility != queue.PayloadHidden ||
		len(viewer.calls) != 1 {
		t.Fatalf("hidden response = %d, inspect %+v, auth %+v", response.Code, source.inspect, viewer.calls)
	}
	for value, visibility := range map[string]queue.PayloadVisibility{
		"hidden":   queue.PayloadHidden,
		"redacted": queue.PayloadRedacted,
	} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, authenticatedRequest(
			t, http.MethodGet, "/v1/tenants/tenant-1/failures/failure-1?payload="+value, "",
		))
		if response.Code != http.StatusOK || source.inspect.Visibility != visibility {
			t.Fatalf("payload %q response = %d, inspect %+v", value, response.Code, source.inspect)
		}
	}
}

func TestHandlerRejectsUnsafeRecordReads(t *testing.T) {
	t.Parallel()

	readErr := errors.New("backend password=secret")
	tests := map[string]struct {
		target        string
		authenticated bool
		viewerErrors  []error
		sourceErr     error
		auditErr      error
		identifierErr error
		workflowDeny  bool
		omitAudit     bool
		status        int
		wantCalls     int
	}{
		"unauthenticated":              {target: "/v1/tenants/tenant-1/failures", status: http.StatusUnauthorized},
		"invalid tenant":               {target: "/v1/tenants/" + strings.Repeat("x", controlplane.MaxIdentityBytes+1) + "/failures", authenticated: true, status: http.StatusBadRequest},
		"view denied":                  {target: "/v1/tenants/tenant-1/failures", authenticated: true, viewerErrors: []error{authz.ErrDenied}, status: http.StatusForbidden},
		"payload denied":               {target: "/v1/tenants/tenant-1/failures/failure-1?payload=revealed", authenticated: true, viewerErrors: []error{nil, authz.ErrDenied}, status: http.StatusForbidden},
		"diagnostics denied":           {target: "/v1/tenants/tenant-1/failures/failure-1?diagnostics=revealed", authenticated: true, viewerErrors: []error{nil, authz.ErrDenied}, status: http.StatusForbidden},
		"audit unavailable":            {target: "/v1/tenants/tenant-1/failures/failure-1?payload=revealed", authenticated: true, auditErr: readErr, status: http.StatusServiceUnavailable},
		"audit identifier unavailable": {target: "/v1/tenants/tenant-1/failures/failure-1?payload=revealed", authenticated: true, identifierErr: readErr, status: http.StatusServiceUnavailable},
		"payload rate limited":         {target: "/v1/tenants/tenant-1/failures/failure-1?payload=revealed", authenticated: true, workflowDeny: true, status: http.StatusTooManyRequests},
		"audit missing":                {target: "/v1/tenants/tenant-1/failures/failure-1?payload=revealed", authenticated: true, omitAudit: true, status: http.StatusServiceUnavailable},
		"source failure":               {target: "/v1/tenants/tenant-1/failures", authenticated: true, sourceErr: readErr, status: http.StatusInternalServerError, wantCalls: 1},
		"inspect failure":              {target: "/v1/tenants/tenant-1/dead-letters/dead-1", authenticated: true, sourceErr: readErr, status: http.StatusInternalServerError, wantCalls: 1},
		"unknown query":                {target: "/v1/tenants/tenant-1/failures?payload=hidden", authenticated: true, status: http.StatusBadRequest},
		"repeated query":               {target: "/v1/tenants/tenant-1/failures?limit=1&limit=2", authenticated: true, status: http.StatusBadRequest},
		"zero limit":                   {target: "/v1/tenants/tenant-1/failures?limit=0", authenticated: true, status: http.StatusBadRequest},
		"large limit":                  {target: "/v1/tenants/tenant-1/failures?limit=201", authenticated: true, status: http.StatusBadRequest},
		"invalid limit":                {target: "/v1/tenants/tenant-1/failures?limit=many", authenticated: true, status: http.StatusBadRequest},
		"empty limit":                  {target: "/v1/tenants/tenant-1/failures?limit=", authenticated: true, status: http.StatusBadRequest},
		"empty cursor":                 {target: "/v1/tenants/tenant-1/failures?cursor=", authenticated: true, status: http.StatusBadRequest},
		"large cursor":                 {target: "/v1/tenants/tenant-1/failures?cursor=" + strings.Repeat("x", queue.MaxCursorBytes+1), authenticated: true, status: http.StatusBadRequest},
		"large search":                 {target: "/v1/tenants/tenant-1/failures?search=" + strings.Repeat("x", queue.MaxSearchBytes+1), authenticated: true, status: http.StatusBadRequest},
		"invalid sort":                 {target: "/v1/tenants/tenant-1/failures?sort=payload", authenticated: true, status: http.StatusBadRequest},
		"empty sort":                   {target: "/v1/tenants/tenant-1/failures?sort=", authenticated: true, status: http.StatusBadRequest},
		"invalid direction":            {target: "/v1/tenants/tenant-1/failures?direction=sideways", authenticated: true, status: http.StatusBadRequest},
		"empty direction":              {target: "/v1/tenants/tenant-1/failures?direction=", authenticated: true, status: http.StatusBadRequest},
		"empty record":                 {target: "/v1/tenants/tenant-1/failures/%20", authenticated: true, status: http.StatusBadRequest},
		"unknown payload":              {target: "/v1/tenants/tenant-1/failures/failure-1?payload=raw", authenticated: true, status: http.StatusBadRequest},
		"repeated payload":             {target: "/v1/tenants/tenant-1/failures/failure-1?payload=hidden&payload=revealed", authenticated: true, status: http.StatusBadRequest},
		"unknown diagnostics":          {target: "/v1/tenants/tenant-1/failures/failure-1?diagnostics=raw", authenticated: true, status: http.StatusBadRequest},
		"repeated diagnostics":         {target: "/v1/tenants/tenant-1/failures/failure-1?diagnostics=hidden&diagnostics=revealed", authenticated: true, status: http.StatusBadRequest},
		"unknown inspect query":        {target: "/v1/tenants/tenant-1/failures/failure-1?sort=queue", authenticated: true, status: http.StatusBadRequest},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := &recordSourceStub{err: tt.sourceErr}
			viewer := &recordViewerStub{errors: tt.viewerErrors}
			config := Config{
				Commands: &commandExecutorStub{}, Records: source, Viewer: viewer,
				SensitiveAudit: &sensitiveAuditStub{err: tt.auditErr},
			}
			if tt.identifierErr != nil {
				config.NewCommandID = func() (string, error) { return "", tt.identifierErr }
			}
			if tt.workflowDeny {
				config.WorkflowLimiter = &workflowLimiterStub{deny: true}
			}
			if tt.omitAudit {
				config.SensitiveAudit = nil
			}
			handler, err := NewHandler(config)
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			request := httptest.NewRequest(http.MethodGet, tt.target, nil)
			if tt.authenticated {
				request = authenticatedRequest(t, http.MethodGet, tt.target, "")
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.status || source.calls != tt.wantCalls ||
				strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("response = %d %s, source calls %d", response.Code, response.Body.String(), source.calls)
			}
		})
	}
}

func TestHandlerDefaultsRecordQueriesAndRequiresViewer(t *testing.T) {
	t.Parallel()

	source := &recordSourceStub{}
	handler, err := NewHandler(Config{
		Commands: &commandExecutorStub{}, Records: source, Viewer: &recordViewerStub{},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(
		t, http.MethodGet, "/v1/tenants/tenant-1/dead-letters", "",
	))
	if response.Code != http.StatusOK || source.request.Limit != defaultRecordPageSize ||
		source.request.Sort != queue.SortOccurredAt || source.request.Direction != queue.SortDescending {
		t.Fatalf("response = %d, request = %+v", response.Code, source.request)
	}

	var typedNil *recordSourceStub
	for _, config := range []Config{
		{Commands: &commandExecutorStub{}, Records: source},
		{Commands: &commandExecutorStub{}, Records: typedNil, Viewer: &recordViewerStub{}},
	} {
		if _, err := NewHandler(config); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("NewHandler() error = %v", err)
		}
	}
}

func TestRecordModelPreservesVersionedGoQueueMetadata(t *testing.T) {
	t.Parallel()

	enqueuedAt := time.Date(2026, time.July, 16, 11, 55, 0, 0, time.UTC)
	firstDeliveryAt := enqueuedAt.Add(time.Minute)
	lastDeliveryAt := firstDeliveryAt.Add(time.Minute)
	deadLetteredAt := lastDeliveryAt.Add(time.Minute)
	retentionDeadline := deadLetteredAt.Add(24 * time.Hour)
	record := apiJobRecord(queue.RecordDeadLetter)
	record.EnvelopeVersion = queue.CurrentEnvelopeVersion
	record.PayloadSchemaVersion = "invoice.v2"
	record.OriginalID = "job-1"
	record.Topic = "billing"
	record.Stream = "critical"
	record.RoutingKey = "invoice.created"
	record.ConsumerGroup = "workers"
	record.SourceRecordID = "1710000000000-0"
	record.EnqueuedAt = &enqueuedAt
	record.FirstDeliveryAt = &firstDeliveryAt
	record.LastDeliveryAt = &lastDeliveryAt
	record.DeadLetteredAt = &deadLetteredAt
	record.RetryPolicy = "exponential"
	record.Classification = queue.ClassificationPermanent
	record.FailureSummary = "invoice validation failed"
	record.Diagnostics = queue.Payload{
		Visibility: queue.PayloadRedacted, ContentType: "text/plain", Size: 32,
	}
	record.HandlerType = "invoice-handler"
	record.JobType = "invoice"
	record.Tags = map[string]string{"region": "eu"}
	record.TraceID = "trace-1"
	record.TenantID = "tenant-1"
	record.ProducerVersion = "v1.2.3"
	record.WorkerVersion = "v1.4.0"
	record.OriginalDeadLetterID = "dead-1"
	record.PriorDeadLetterID = "dead-2"
	record.ReplayGeneration = 2
	record.RetentionDeadline = &retentionDeadline

	got := recordModel(record, queue.PayloadHidden, true)
	if got.EnvelopeVersion != record.EnvelopeVersion ||
		got.PayloadSchemaVersion != record.PayloadSchemaVersion ||
		got.OriginalID != record.OriginalID || got.Topic != record.Topic ||
		got.Stream != record.Stream || got.RoutingKey != record.RoutingKey ||
		got.ConsumerGroup != record.ConsumerGroup ||
		got.SourceRecordID != record.SourceRecordID ||
		got.EnqueuedAt == nil || !got.EnqueuedAt.Equal(enqueuedAt) ||
		got.FirstDeliveryAt == nil || !got.FirstDeliveryAt.Equal(firstDeliveryAt) ||
		got.LastDeliveryAt == nil || !got.LastDeliveryAt.Equal(lastDeliveryAt) ||
		got.DeadLetteredAt == nil || !got.DeadLetteredAt.Equal(deadLetteredAt) ||
		got.RetryPolicy != record.RetryPolicy ||
		got.Classification != record.Classification ||
		got.FailureSummary != record.FailureSummary ||
		got.Diagnostics.Visibility != queue.PayloadRedacted ||
		got.HandlerType != record.HandlerType || got.JobType != record.JobType ||
		got.Tags["region"] != "eu" || got.TraceID != record.TraceID ||
		got.TenantID != record.TenantID ||
		got.ProducerVersion != record.ProducerVersion ||
		got.WorkerVersion != record.WorkerVersion ||
		got.OriginalDeadLetterID != record.OriginalDeadLetterID ||
		got.PriorDeadLetterID != record.PriorDeadLetterID ||
		got.ReplayGeneration != record.ReplayGeneration ||
		got.RetentionDeadline == nil || !got.RetentionDeadline.Equal(retentionDeadline) {
		t.Fatalf("recordModel() lost versioned metadata: %+v", got)
	}
	record.Tags["region"] = "changed"
	if got.Tags["region"] != "eu" {
		t.Fatal("recordModel() aliased backend-owned tags")
	}
}

type recordSourceStub struct {
	page    queue.RecordPage
	record  queue.JobRecord
	err     error
	tenant  string
	request queue.PageRequest
	inspect queue.InspectRequest
	calls   int
}

func (s *recordSourceStub) ListFailures(
	_ context.Context, tenant string, request queue.PageRequest,
) (queue.RecordPage, error) {
	s.calls++
	s.tenant = tenant
	s.request = request

	return s.page, s.err
}

func (s *recordSourceStub) ListDeadLetters(
	_ context.Context, tenant string, request queue.PageRequest,
) (queue.RecordPage, error) {
	s.calls++
	s.tenant = tenant
	s.request = request

	return s.page, s.err
}

func (s *recordSourceStub) Inspect(
	_ context.Context, tenant string, request queue.InspectRequest,
) (queue.JobRecord, error) {
	s.calls++
	s.tenant = tenant
	s.inspect = request

	return s.record, s.err
}

type recordAuthorization struct {
	permission controlplane.Permission
	target     controlplane.Target
}

type sensitiveAuditStub struct {
	accesses []controlplane.SensitiveAccess
	err      error
}

func (s *sensitiveAuditStub) AuditSensitiveAccess(
	_ context.Context,
	access controlplane.SensitiveAccess,
) error {
	s.accesses = append(s.accesses, access)

	return s.err
}

type recordViewerStub struct {
	errors []error
	calls  []recordAuthorization
}

func (s *recordViewerStub) Authorize(
	_ context.Context,
	_ string,
	_ string,
	permission controlplane.Permission,
	target controlplane.Target,
) error {
	s.calls = append(s.calls, recordAuthorization{permission: permission, target: target})
	if len(s.errors) >= len(s.calls) {
		return s.errors[len(s.calls)-1]
	}

	return nil
}

func apiJobRecord(kind queue.RecordKind) queue.JobRecord {
	return queue.JobRecord{
		Kind: kind, ID: "failure-1", Backend: "valkey-streams", Queue: "critical",
		OccurredAt: time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
		Attempts:   3, FailureCode: "handler_failed",
		Payload: queue.Payload{Visibility: queue.PayloadHidden, Size: 128},
	}
}
