package opensearch_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/search"
	adapter "github.com/faustbrian/golib/pkg/search/adapters/opensearch"
)

func TestOperationalArtifactsAndIncidentDrills(t *testing.T) {
	dashboard := readOperationalJSON[operationalDashboard](t, "operations/dashboard.json")
	alerts := readOperationalJSON[operationalAlerts](t, "operations/alerts.json")
	drills := readOperationalJSON[operationalDrills](t, "operations/incident-drills.json")
	runbookBytes, err := os.ReadFile("operations/runbook.md")
	if err != nil {
		t.Fatal(err)
	}
	runbook := string(runbookBytes)
	validateOperationalArtifacts(t, dashboard, alerts, drills, runbook)

	t.Run("overload", drillOverload)
	t.Run("cluster-loss", drillClusterLoss)
	t.Run("unknown-write-outcome", drillUnknownWriteOutcome)
	t.Run("pit-expiry", drillPITExpiry)
	t.Run("migration-rollback", drillMigrationRollback)
	t.Run("migration-unknown-outcome", drillMigrationUnknownOutcome)
	t.Run("generation-cleanup", drillGenerationCleanup)
	t.Run("drift-repair", drillDriftRepair)
	t.Run("full-rebuild", drillFullRebuild)
}

func drillOverload(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var requests atomic.Int32
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			close(entered)
			<-release
			return validInfoResponse("overload-node"), nil
		}),
		TransportOwnership: adapter.TransportBorrowed,
		Resilience:         adapter.ResilienceConfig{MaximumInFlight: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	first := make(chan error, 1)
	go func() {
		_, infoErr := client.Info(t.Context())
		first <- infoErr
	}()
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	if _, infoErr := client.Info(t.Context()); !errors.Is(infoErr, adapter.ErrBackpressure) {
		t.Fatalf("overload Info() error = %v", infoErr)
	}
	close(release)
	if infoErr := <-first; infoErr != nil {
		t.Fatal(infoErr)
	}
	if requests.Load() != 1 {
		t.Fatalf("overload downstream requests = %d, want 1", requests.Load())
	}
}

func drillClusterLoss(t *testing.T) {
	var requests atomic.Int32
	hosts := make(chan string, 2)
	client, err := adapter.New(adapter.Config{
		Endpoints:      []string{"https://node-a.example.test", "https://node-b.example.test"},
		RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests.Add(1)
			hosts <- request.URL.Host
			return nil, errors.New("cluster unavailable")
		}),
		TransportOwnership: adapter.TransportBorrowed,
		Resilience: adapter.ResilienceConfig{
			MaximumInFlight: 1, CircuitFailureThreshold: 2, CircuitOpenDuration: time.Minute,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for attempt := 0; attempt < 2; attempt++ {
		if _, infoErr := client.Info(t.Context()); !errors.Is(infoErr, adapter.ErrTransport) {
			t.Fatalf("cluster-loss attempt %d error = %v", attempt, infoErr)
		}
	}
	if _, infoErr := client.Info(t.Context()); !errors.Is(infoErr, adapter.ErrCircuitOpen) {
		t.Fatalf("cluster-loss open-circuit error = %v", infoErr)
	}
	close(hosts)
	seen := map[string]bool{}
	for host := range hosts {
		seen[host] = true
	}
	if requests.Load() != 2 || len(seen) != 2 {
		t.Fatalf("cluster-loss requests/hosts = %d/%v", requests.Load(), seen)
	}
}

func drillUnknownWriteOutcome(t *testing.T) {
	client := newWriteClient(t, "https://search.example.test", &observedTransport{err: errors.New("connection reset after dispatch")})
	document, err := search.NewDocument("tenant-a", "events", "drill-write", 7, json.RawMessage(`{"value":"drill"}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	outcome, writeErr := client.Write(t.Context(), search.IndexDocument(document), search.RefreshNone)
	var failure *adapter.Failure
	if !errors.As(writeErr, &failure) || failure.OutcomeKnown || outcome.State != search.OutcomeUnknown {
		t.Fatalf("unknown-write outcome/error = %#v/%v", outcome, writeErr)
	}
}

func drillPITExpiry(t *testing.T) {
	limits := search.DefaultLimits()
	codec := mustCursorCodec(t)
	request := search.Request{
		Tenant: "tenant-a", Index: "events", Query: search.MatchAllQuery{},
		Sort: []search.Sort{{Field: search.DocumentIDSortField, Direction: search.Ascending}},
		Page: search.CursorPage{Size: 1, KeepAlive: time.Minute},
	}
	fingerprint, err := search.RequestFingerprint(request, limits)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := codec.Encode(search.CursorBinding{
		Tenant: request.Tenant, Index: request.Index,
		QueryFingerprint: fingerprint, IndexFingerprint: "mapping-v2-fingerprint",
	}, search.CursorState{
		PointInTime: "pit-drill", SortValues: []json.RawMessage{json.RawMessage(`"a"`)}, ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	deleted := 0
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method == http.MethodDelete && request.URL.Path == "/_search/point_in_time" {
				deleted++
				return cursorResponse(http.StatusOK, `{"pits":[{"pit_id":"pit-drill","successful":true}]}`), nil
			}
			return cursorResponse(http.StatusNotFound, `{"error":{"type":"resource_not_found_exception"},"status":404}`), nil
		}),
		TransportOwnership: adapter.TransportBorrowed,
		Search: &adapter.SearchConfig{
			Limits: limits, CursorCodec: codec, Clock: time.Now,
			Authorizer: adapter.SearchAuthorizerFunc(func(context.Context, adapter.SearchAuthorization) error { return nil }),
			Resolver: adapter.IndexResolverFunc(func(context.Context, string, string, adapter.IndexAccess) (adapter.IndexTarget, error) {
				return adapter.IndexTarget{Name: "tenant-a-events-v2", PhysicalName: "tenant-a-events-v2", Fingerprint: "mapping-v2-fingerprint"}, nil
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	request.Page = search.CursorPage{Size: 1, Cursor: cursor, KeepAlive: time.Minute}
	if _, searchErr := client.Search(t.Context(), request); !errors.Is(searchErr, adapter.ErrPITExpired) {
		t.Fatalf("PIT-expiry Search() error = %v", searchErr)
	}
	if deleted != 1 {
		t.Fatalf("PIT-expiry cleanup requests = %d, want 1", deleted)
	}
}

func drillMigrationRollback(t *testing.T) {
	var guarded, swaps atomic.Int32
	client := newCutoverClient(t,
		adapter.LifecycleCutoverGuardFunc(func(_ context.Context, _ adapter.LifecycleCutoverRequest, operation func() error) error {
			guarded.Add(1)
			defer guarded.Add(-1)
			return operation()
		}),
		adapter.LifecycleVerifierFunc(func(_ context.Context, request adapter.LifecycleVerificationRequest) (adapter.LifecycleVerificationResult, error) {
			if guarded.Load() != 1 {
				t.Fatal("migration verification ran without the write fence")
			}
			return adapter.LifecycleVerificationResult{TargetFingerprint: request.ExpectedTargetFingerprint}, nil
		}),
		roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if guarded.Load() != 1 {
				t.Fatal("migration request ran without the write fence")
			}
			if request.URL.Path == "/_aliases" {
				swaps.Add(1)
				return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
			}
			return cursorResponse(http.StatusOK, `{"count":1,"_shards":{"total":1,"successful":1,"failed":0}}`), nil
		}),
	)
	forward, err := client.CutoverAlias(t.Context(), "tenant-a", "events-read", "events-v1", "events-v2", "definition-v2")
	if err != nil || !forward.Verified {
		t.Fatalf("forward cutover = %#v/%v", forward, err)
	}
	rollback, err := client.CutoverAlias(t.Context(), "tenant-a", "events-read", "events-v2", "events-v1", "definition-v1")
	if err != nil || !rollback.Verified || swaps.Load() != 2 || guarded.Load() != 0 {
		t.Fatalf("rollback cutover/swaps/guard = %#v/%v/%d/%d", rollback, err, swaps.Load(), guarded.Load())
	}
}

func drillMigrationUnknownOutcome(t *testing.T) {
	client, err := adapter.New(adapter.Config{
		Endpoints: []string{"https://search.example.test"}, RequestTimeout: time.Second, MaximumResponseBytes: 4096,
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return cursorResponse(http.StatusOK, `{"acknowledged":false,"shards_acknowledged":false}`), nil
		}),
		TransportOwnership: adapter.TransportBorrowed,
		Lifecycle: &adapter.LifecycleConfig{
			Authorizer: adapter.LifecycleAuthorizerFunc(func(context.Context, string, []string) error { return nil }),
			MutationGuard: adapter.LifecycleMutationGuardFunc(func(_ context.Context, _ adapter.LifecycleMutationRequest, operation func() error) error {
				return operation()
			}),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	definition, err := search.NewIndexDefinition("events-v2", json.RawMessage(`{}`), json.RawMessage(`{}`), search.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	err = client.CreateIndex(t.Context(), "tenant-a", definition)
	var failure *adapter.Failure
	if !errors.As(err, &failure) || failure.OutcomeKnown || failure.Operation != adapter.OperationCreateIndex || failure.Category != adapter.FailureMalformed {
		t.Fatalf("ambiguous migration create = %#v/%v", failure, err)
	}
}

func drillGenerationCleanup(t *testing.T) {
	request := search.LifecycleCleanupRequest{
		MigrationID: "migration-1", Tenant: "tenant-a", Alias: "events-read",
		ActiveIndex: "events-v2", ActiveFingerprint: "definition-v2",
		InactiveIndex: "events-v1", InactiveFingerprint: "definition-v1",
	}
	eligible := false
	deleted := 0
	client := newCleanupClient(t, adapter.LifecycleCleanupGuardFunc(func(_ context.Context, got search.LifecycleCleanupRequest, operation func() error) error {
		if got != request || !eligible {
			return errors.New("retention, readers, or aliases still block cleanup")
		}
		return operation()
	}), roundTripFunc(func(httpRequest *http.Request) (*http.Response, error) {
		if httpRequest.Method != http.MethodDelete || httpRequest.URL.Path != "/events-v1" {
			t.Fatalf("cleanup request = %s %s", httpRequest.Method, httpRequest.URL.Path)
		}
		deleted++
		return cursorResponse(http.StatusOK, `{"acknowledged":true}`), nil
	}))
	if err := client.CleanupIndex(t.Context(), request); !errors.Is(err, adapter.ErrLifecycleCleanupGuardRejected) || deleted != 0 {
		t.Fatalf("ineligible cleanup = %v/deletes=%d", err, deleted)
	}
	eligible = true
	if err := client.CleanupIndex(t.Context(), request); err != nil || deleted != 1 {
		t.Fatalf("eligible cleanup = %v/deletes=%d", err, deleted)
	}
}

func drillDriftRepair(t *testing.T) {
	limits := search.DefaultLimits()
	document, err := search.NewDocument("tenant-a", "events", "event-a", 2, json.RawMessage(`{"value":"current"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	source := operationalReconciliationReader{tenant: document.Tenant, index: document.Index, records: []search.ReconciliationRecord{search.SourceRecord(document)}}
	stale := operationalReconciliationReader{tenant: document.Tenant, index: document.Index, records: []search.ReconciliationRecord{search.IndexRecord(document.ID, 1, search.SourceDigest(json.RawMessage(`{"value":"stale"}`)))}}
	repair := &operationalRepairIndexer{}
	reconciler, err := search.NewReconciler(source, stale, repair, limits)
	if err != nil {
		t.Fatal(err)
	}
	report, err := reconciler.Run(t.Context(), search.ReconciliationRequest{Tenant: document.Tenant, Index: document.Index, PageSize: 1, MaxRecords: 2, Repair: true})
	if err != nil || !report.Complete || report.Repaired != 1 || len(report.Drift) != 1 || report.Drift[0].Kind != search.DriftStale {
		t.Fatalf("drift repair = %#v/%v", report, err)
	}
	current := operationalReconciliationReader{tenant: document.Tenant, index: document.Index, records: []search.ReconciliationRecord{search.IndexRecord(document.ID, document.Version, search.SourceDigest(document.Source))}}
	reconciler, err = search.NewReconciler(source, current, repair, limits)
	if err != nil {
		t.Fatal(err)
	}
	report, err = reconciler.Run(t.Context(), search.ReconciliationRequest{Tenant: document.Tenant, Index: document.Index, PageSize: 1, MaxRecords: 2})
	if err != nil || !report.Complete || report.Repaired != 0 || len(report.Drift) != 0 {
		t.Fatalf("second zero-drift pass = %#v/%v", report, err)
	}
}

func drillFullRebuild(t *testing.T) {
	limits := search.DefaultLimits()
	a, _ := search.NewDocument("tenant-a", "events", "a", 1, json.RawMessage(`{"value":"a"}`), limits)
	b, _ := search.NewDocument("tenant-a", "events", "b", 1, json.RawMessage(`{"value":"b"}`), limits)
	c, _ := search.NewDocument("tenant-a", "events", "c", 2, json.RawMessage(`{"value":"outbox"}`), limits)
	snapshot := operationalReconciliationReader{tenant: a.Tenant, index: a.Index, records: []search.ReconciliationRecord{search.SourceRecord(a), search.SourceRecord(b)}}
	empty := operationalReconciliationReader{tenant: a.Tenant, index: a.Index}
	repair := &operationalRepairIndexer{}
	reconciler, err := search.NewReconciler(snapshot, empty, repair, limits)
	if err != nil {
		t.Fatal(err)
	}
	report, err := reconciler.Run(t.Context(), search.ReconciliationRequest{Tenant: a.Tenant, Index: a.Index, PageSize: 1, MaxRecords: 4, Repair: true})
	if err != nil || !report.Complete || report.Repaired != 2 {
		t.Fatalf("snapshot rebuild = %#v/%v", report, err)
	}
	if _, err := repair.Write(t.Context(), search.IndexDocument(c), search.RefreshWaitFor); err != nil {
		t.Fatal(err)
	}
	if _, err := repair.Write(t.Context(), search.DeleteDocument(a.Tenant, a.Index, b.ID, 2), search.RefreshWaitFor); err != nil {
		t.Fatal(err)
	}
	finalSource := operationalReconciliationReader{tenant: a.Tenant, index: a.Index, records: []search.ReconciliationRecord{search.SourceRecord(a), search.SourceRecord(c)}}
	finalIndex := operationalReconciliationReader{tenant: a.Tenant, index: a.Index, records: []search.ReconciliationRecord{
		search.IndexRecord(a.ID, a.Version, search.SourceDigest(a.Source)), search.IndexRecord(c.ID, c.Version, search.SourceDigest(c.Source)),
	}}
	for pass := 1; pass <= 2; pass++ {
		reconciler, err = search.NewReconciler(finalSource, finalIndex, repair, limits)
		if err != nil {
			t.Fatal(err)
		}
		report, err = reconciler.Run(t.Context(), search.ReconciliationRequest{Tenant: a.Tenant, Index: a.Index, PageSize: 1, MaxRecords: 4})
		if err != nil || !report.Complete || report.Repaired != 0 || len(report.Drift) != 0 {
			t.Fatalf("full rebuild zero-drift pass %d = %#v/%v", pass, report, err)
		}
	}
}

type operationalReconciliationReader struct {
	tenant, index string
	records       []search.ReconciliationRecord
}

func (reader operationalReconciliationReader) Read(_ context.Context, tenant, index, cursor string, pageSize int) (search.ReconciliationPage, error) {
	if tenant != reader.tenant || index != reader.index || pageSize <= 0 {
		return search.ReconciliationPage{}, errors.New("operational reconciliation scope rejected")
	}
	start := 0
	if cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 || parsed >= len(reader.records) {
			return search.ReconciliationPage{}, errors.New("operational reconciliation cursor rejected")
		}
		start = parsed
	}
	end := min(start+pageSize, len(reader.records))
	done := end == len(reader.records)
	next := ""
	if !done {
		next = strconv.Itoa(end)
	}
	return search.ReconciliationPage{Records: append([]search.ReconciliationRecord(nil), reader.records[start:end]...), Cursor: next, Done: done}, nil
}

type operationalRepairIndexer struct{ operations []search.WriteOperation }

func (repair *operationalRepairIndexer) Write(_ context.Context, operation search.WriteOperation, _ search.RefreshPolicy) (search.ItemOutcome, error) {
	repair.operations = append(repair.operations, operation)
	return search.ItemOutcome{ID: operation.ID, Action: operation.Action, State: search.OutcomeApplied, Version: operation.Version}, nil
}

func (repair *operationalRepairIndexer) Bulk(_ context.Context, request search.BulkRequest) (search.BulkResult, error) {
	repair.operations = append(repair.operations, request.Operations...)
	items := make([]search.ItemOutcome, len(request.Operations))
	for position, operation := range request.Operations {
		items[position] = search.ItemOutcome{Position: position, ID: operation.ID, Action: operation.Action, State: search.OutcomeApplied, Version: operation.Version}
	}
	return search.NewBulkResult(items)
}

type operationalDashboard struct {
	Version int    `json:"version"`
	Title   string `json:"title"`
	Refresh string `json:"refresh"`
	Panels  []struct {
		ID      string   `json:"id"`
		Source  string   `json:"source"`
		Signals []string `json:"signals"`
		GroupBy []string `json:"group_by"`
	} `json:"panels"`
	ForbiddenLabels []string `json:"forbidden_labels"`
}

type operationalAlerts struct {
	Version int `json:"version"`
	Alerts  []struct {
		Name, Severity, Condition, For, Runbook string
	} `json:"alerts"`
}

type operationalDrills struct {
	Version int `json:"version"`
	Drills  []struct {
		Name, Objective, Evidence string
	} `json:"drills"`
}

func readOperationalJSON[T any](t *testing.T, path string) T {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var value T
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("%s has trailing JSON", path)
	}
	return value
}

func validateOperationalArtifacts(t *testing.T, dashboard operationalDashboard, alerts operationalAlerts, drills operationalDrills, runbook string) {
	t.Helper()
	if dashboard.Version != 1 || dashboard.Title == "" {
		t.Fatalf("dashboard identity = %#v", dashboard)
	}
	if _, err := time.ParseDuration(dashboard.Refresh); err != nil {
		t.Fatalf("dashboard refresh = %q", dashboard.Refresh)
	}
	requiredSignals := map[string]bool{
		"request_rate": false, "request_latency": false, "backpressure_rejections": false,
		"circuit_open": false, "health_ready": false, "disk_available_bytes": false,
		"unknown_write_outcomes": false, "pit_cleanup_failures": false, "reconciliation_drift": false,
	}
	forbidden := map[string]bool{}
	for _, label := range dashboard.ForbiddenLabels {
		forbidden[label] = true
	}
	for _, panel := range dashboard.Panels {
		if panel.ID == "" || panel.Source == "" || len(panel.Signals) == 0 {
			t.Fatalf("invalid dashboard panel: %#v", panel)
		}
		telemetrySignals := map[string]bool{
			"request_rate": true, "request_latency": true, "response_category": true, "http_status": true,
			"in_flight": true, "queued": true, "backpressure_rejections": true, "circuit_open": true,
			string(adapter.TelemetryUnknownWriteOutcome): true,
			string(adapter.TelemetryPartialSearch):       true,
			string(adapter.TelemetryPITCleanupFailure):   true,
			string(adapter.TelemetryCutoverFailure):      true,
		}
		for _, signal := range panel.Signals {
			if _, required := requiredSignals[signal]; required {
				requiredSignals[signal] = true
			}
			if panel.Source == "telemetry" && !telemetrySignals[signal] {
				t.Fatalf("telemetry panel %q requires unsupported signal %q", panel.ID, signal)
			}
		}
		if panel.Source != "telemetry" && panel.Source != "health-report" && panel.Source != "capacity-report" && panel.Source != "reconciliation-report" {
			t.Fatalf("dashboard panel %q has unknown source %q", panel.ID, panel.Source)
		}
		for _, label := range panel.GroupBy {
			if forbidden[label] {
				t.Fatalf("dashboard groups by forbidden label %q", label)
			}
		}
	}
	for signal, present := range requiredSignals {
		if !present {
			t.Fatalf("dashboard is missing %q", signal)
		}
	}
	requiredAlerts := map[string]bool{"cluster-not-ready": false, "unknown-write-outcome": false, "sustained-overload": false, "low-disk-headroom": false, "migration-drift": false}
	if alerts.Version != 1 {
		t.Fatalf("alerts version = %d", alerts.Version)
	}
	for _, alert := range alerts.Alerts {
		if alert.Name == "" || alert.Condition == "" || (alert.Severity != "warning" && alert.Severity != "critical") {
			t.Fatalf("invalid alert: %#v", alert)
		}
		if _, err := time.ParseDuration(alert.For); err != nil {
			t.Fatalf("alert %q duration = %q", alert.Name, alert.For)
		}
		if !strings.Contains(runbook, "## "+alert.Runbook+"\n") {
			t.Fatalf("alert %q has no runbook section %q", alert.Name, alert.Runbook)
		}
		if _, required := requiredAlerts[alert.Name]; required {
			requiredAlerts[alert.Name] = true
		}
	}
	for name, present := range requiredAlerts {
		if !present {
			t.Fatalf("alerts are missing %q", name)
		}
	}
	requiredDrills := map[string]bool{
		"overload": false, "cluster-loss": false, "unknown-write-outcome": false, "pit-expiry": false,
		"migration-rollback": false, "migration-unknown-outcome": false, "generation-cleanup": false,
		"drift-repair": false, "full-rebuild": false,
	}
	if drills.Version != 1 {
		t.Fatalf("drills version = %d", drills.Version)
	}
	for _, drill := range drills.Drills {
		if drill.Objective == "" || drill.Evidence == "" {
			t.Fatalf("invalid drill: %#v", drill)
		}
		if _, required := requiredDrills[drill.Name]; required {
			requiredDrills[drill.Name] = true
		}
	}
	for name, present := range requiredDrills {
		if !present || !strings.Contains(runbook, "## "+name+"\n") {
			t.Fatalf("drill/runbook is missing %q", name)
		}
	}
}
