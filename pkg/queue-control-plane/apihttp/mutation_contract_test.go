package apihttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	controlkubernetes "github.com/faustbrian/golib/pkg/queue-control-plane/kubernetes"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestAdministrativeEndpointsRejectExplicitAnonymousPrincipal(t *testing.T) {
	t.Parallel()

	h := &handler{}
	tests := map[string]struct {
		method string
		call   func(http.ResponseWriter, *http.Request)
	}{
		"audit":           {method: http.MethodGet, call: h.listAudit},
		"command history": {method: http.MethodGet, call: h.listCommandHistory},
		"command result":  {method: http.MethodGet, call: h.getCommandResult},
		"desired state":   {method: http.MethodGet, call: h.getDesiredState},
		"execute command": {method: http.MethodPost, call: h.executeCommand},
		"queue records":   {method: http.MethodGet, call: h.listFailures},
		"queues":          {method: http.MethodGet, call: h.listQueues},
		"workers":         {method: http.MethodGet, call: h.listWorkers},
		"workloads":       {method: http.MethodGet, call: h.listWorkloads},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(test.method, "/", nil)
			request = request.WithContext(authentication.ContextWithPrincipal(
				request.Context(), authentication.AnonymousPrincipal(),
			))
			response := httptest.NewRecorder()
			test.call(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", response.Code)
			}
		})
	}
}

func TestPublicQueriesAcceptExactBounds(t *testing.T) {
	t.Parallel()

	after, auditLimit, err := parseAuditQuery(url.Values{
		"after": {"1"}, "limit": {"1000"},
	})
	if err != nil || after != 1 || auditLimit != MaxAuditPageSize {
		t.Fatalf("parseAuditQuery() = (%d, %d, %v)", after, auditLimit, err)
	}

	cursor := strings.Repeat("x", MaxCommandCursorBytes)
	gotCursor, commandLimit, err := parseCommandQuery(url.Values{
		"cursor": {cursor}, "limit": {"1000"},
	})
	if err != nil || gotCursor != cursor || commandLimit != MaxCommandPageSize {
		t.Fatalf("parseCommandQuery() = (%q, %d, %v)", gotCursor, commandLimit, err)
	}

	worker, err := parseWorkerQuery(url.Values{"limit": {"1000"}})
	if err != nil || worker.limit != MaxWorkerPageSize {
		t.Fatalf("parseWorkerQuery() = (%+v, %v)", worker, err)
	}

	for _, limit := range []string{"1", "500"} {
		_, err := parseWorkloadQuery(url.Values{"limit": {limit}})
		if err != nil {
			t.Fatalf("parseWorkloadQuery(limit=%s) error = %v", limit, err)
		}
	}
	continuation := strings.Repeat("x", controlkubernetes.MaxContinueTokenBytes)
	workload, err := parseWorkloadQuery(url.Values{"continue": {continuation}})
	if err != nil || workload.continueToken != continuation {
		t.Fatalf("parseWorkloadQuery(continue) = (%+v, %v)", workload, err)
	}

	if !validIdentity(strings.Repeat("x", controlplane.MaxIdentityBytes)) {
		t.Fatal("validIdentity() rejected the exact public identity bound")
	}
}

func TestHandlerUsesExactDefaultWorkerStaleness(t *testing.T) {
	t.Parallel()

	source := &workerSourceStub{}
	handler, err := NewHandler(Config{
		Commands: &commandExecutorStub{}, Workers: source, Viewer: &viewerStub{},
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(
		t, http.MethodGet, "/v1/tenants/tenant-1/workers", "",
	))
	if response.Code != http.StatusOK || source.staleAfter != 30*time.Second {
		t.Fatalf("response = %d, staleAfter = %s", response.Code, source.staleAfter)
	}
}

func TestFixedWindowRateLimiterAcceptsExactKeyBound(t *testing.T) {
	t.Parallel()

	limiter, err := NewFixedWindowRateLimiter(1, time.Minute, 1, time.Now)
	if err != nil {
		t.Fatalf("NewFixedWindowRateLimiter() error = %v", err)
	}
	if !limiter.Allow(context.Background(), strings.Repeat("x", maxRateLimitKeyBytes)) {
		t.Fatal("Allow() rejected the exact key bound")
	}
}

func TestRecordRoutingKeepsFailureAndDeadLetterBackendsDistinct(t *testing.T) {
	t.Parallel()

	source := &recordRoutingSource{}
	h := &handler{records: source}
	for _, test := range []struct {
		kind       queue.RecordKind
		wantTarget controlplane.TargetKind
		wantName   string
	}{
		{kind: queue.RecordFailure, wantTarget: controlplane.TargetFailure, wantName: "failures"},
		{kind: queue.RecordDeadLetter, wantTarget: controlplane.TargetDeadLetter, wantName: "dead_letters"},
	} {
		if got := targetKindForRecord(test.kind); got != test.wantTarget {
			t.Fatalf("targetKindForRecord(%q) = %q, want %q", test.kind, got, test.wantTarget)
		}
		if got := collectionName(test.kind); got != test.wantName {
			t.Fatalf("collectionName(%q) = %q, want %q", test.kind, got, test.wantName)
		}
	}

	request := authenticatedRequest(t, http.MethodGet, "/", "")
	request.SetPathValue("tenant", "tenant-1")
	h.viewer = &viewerStub{}
	for _, kind := range []queue.RecordKind{queue.RecordFailure, queue.RecordDeadLetter} {
		response := httptest.NewRecorder()
		h.listRecords(response, request, kind)
		if response.Code != http.StatusOK {
			t.Fatalf("listRecords(%q) status = %d", kind, response.Code)
		}
	}
	if strings.Join(source.calls, ",") != "failures,dead_letters" {
		t.Fatalf("record source calls = %v", source.calls)
	}
}

func TestRecordModelRevealsOnlyExplicitCompatibleVisibility(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		requested queue.PayloadVisibility
		stored    queue.PayloadVisibility
		want      queue.PayloadVisibility
		wantData  bool
	}{
		"hidden redacted":   {requested: queue.PayloadHidden, stored: queue.PayloadRedacted, want: queue.PayloadHidden},
		"redacted redacted": {requested: queue.PayloadRedacted, stored: queue.PayloadRedacted, want: queue.PayloadRedacted, wantData: true},
		"redacted revealed": {requested: queue.PayloadRedacted, stored: queue.PayloadRevealed, want: queue.PayloadHidden},
		"revealed revealed": {requested: queue.PayloadRevealed, stored: queue.PayloadRevealed, want: queue.PayloadRevealed, wantData: true},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			record := apiJobRecord(queue.RecordFailure)
			record.Payload = queue.Payload{Visibility: test.stored, Size: 1, Data: []byte("x")}
			model := recordModel(record, test.requested, false)
			if model.Payload.Visibility != test.want || (len(model.Payload.Data) > 0) != test.wantData {
				t.Fatalf("payload = %+v, want visibility %q data=%t", model.Payload, test.want, test.wantData)
			}
		})
	}
}

func TestHiddenRecordResponseHasNoAttachmentDisposition(t *testing.T) {
	t.Parallel()

	h := &handler{
		records: &recordSourceStub{record: apiJobRecord(queue.RecordFailure)},
		viewer:  &recordViewerStub{}, now: time.Now, newCommandID: controlplane.NewCommandID,
	}
	request := authenticatedRequest(t, http.MethodGet, "/", "")
	request.SetPathValue("tenant", "tenant-1")
	request.SetPathValue("record", "failure-1")
	response := httptest.NewRecorder()
	h.inspectFailure(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Disposition") != "" {
		t.Fatalf("response = %d, Content-Disposition = %q", response.Code, response.Header().Get("Content-Disposition"))
	}
}

func TestSecurityPredicatesRequireEveryComponent(t *testing.T) {
	t.Parallel()

	for origin, want := range map[string]bool{
		"http://control.example.test":       true,
		"https://control.example.test":      true,
		"ftp://control.example.test":        false,
		"https:":                            false,
		"https://user@control.example.test": false,
		"https://control.example.test/path": false,
		"https://control.example.test?q=1":  false,
		"https://control.example.test#part": false,
	} {
		if got := validOrigin(origin); got != want {
			t.Fatalf("validOrigin(%q) = %t, want %t", origin, got, want)
		}
	}

	options := httptest.NewRequest(http.MethodOptions, "/", nil)
	if isPreflight(options) {
		t.Fatal("OPTIONS without requested method is preflight")
	}
	get := httptest.NewRequest(http.MethodGet, "/", nil)
	get.Header.Set("Access-Control-Request-Method", http.MethodGet)
	if isPreflight(get) {
		t.Fatal("GET with requested method is preflight")
	}

	ctx := authentication.ContextWithPrincipal(
		context.Background(), authentication.AnonymousPrincipal(),
	)
	if subject, ok := authenticationPrincipal(ctx); ok || subject != "" {
		t.Fatalf("authenticationPrincipal(anonymous) = (%q, %t)", subject, ok)
	}
}

func TestWorkerOrderingIsStrict(t *testing.T) {
	t.Parallel()

	left := fleet.WorkerSnapshot{Heartbeat: fleet.Heartbeat{WorkerID: "worker-a"}}
	equal := fleet.WorkerSnapshot{Heartbeat: fleet.Heartbeat{WorkerID: "worker-a"}}
	right := fleet.WorkerSnapshot{Heartbeat: fleet.Heartbeat{WorkerID: "worker-b"}}
	if !workerComesBefore(left, right) || workerComesBefore(right, left) || workerComesBefore(left, equal) {
		t.Fatal("worker ordering is not a strict ascending order")
	}
}

type recordRoutingSource struct {
	calls []string
}

func (s *recordRoutingSource) ListFailures(context.Context, string, queue.PageRequest) (queue.RecordPage, error) {
	s.calls = append(s.calls, "failures")

	return queue.RecordPage{}, nil
}

func (s *recordRoutingSource) ListDeadLetters(context.Context, string, queue.PageRequest) (queue.RecordPage, error) {
	s.calls = append(s.calls, "dead_letters")

	return queue.RecordPage{}, nil
}

func (*recordRoutingSource) Inspect(context.Context, string, queue.InspectRequest) (queue.JobRecord, error) {
	return queue.JobRecord{}, nil
}
