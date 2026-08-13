package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	"github.com/faustbrian/golib/pkg/queue-control-plane/dataplane"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
	"github.com/faustbrian/golib/pkg/queue-control-plane/server"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestBuildInfoAcceptsExactIdentityBounds(t *testing.T) {
	t.Parallel()

	identity := strings.Repeat("x", controlplane.MaxIdentityBytes)
	info, err := parseBuildInfo(identity, identity, "")
	if err != nil || info.Version != identity || info.Commit != identity {
		t.Fatalf("parseBuildInfo(exact bounds) = (%+v, %v)", info, err)
	}
}

func TestConfigAcceptsEveryExactDocumentBound(t *testing.T) {
	t.Parallel()

	tests := map[string]map[string]string{
		"access minimum": {"QUEUE_CONTROL_ACCESS_MAX_BYTES": "1"},
		"access maximum": {"QUEUE_CONTROL_ACCESS_MAX_BYTES": strconv.FormatInt(defaultAccessDocumentSize, 10)},
		"retention minimum": {
			"QUEUE_CONTROL_RETENTION_ONLY": "true", "QUEUE_CONTROL_RETENTION_FILE": "retention.json",
			"QUEUE_CONTROL_RETENTION_MAX_BYTES": "1",
		},
		"retention maximum": {
			"QUEUE_CONTROL_RETENTION_ONLY": "true", "QUEUE_CONTROL_RETENTION_FILE": "retention.json",
			"QUEUE_CONTROL_RETENTION_MAX_BYTES": strconv.FormatInt(defaultAccessDocumentSize, 10),
		},
		"kubernetes minimum": {
			"QUEUE_CONTROL_KUBERNETES_TENANTS_FILE":      "tenants.json",
			"QUEUE_CONTROL_KUBERNETES_TENANTS_MAX_BYTES": "1",
		},
		"kubernetes maximum": {
			"QUEUE_CONTROL_KUBERNETES_TENANTS_FILE":      "tenants.json",
			"QUEUE_CONTROL_KUBERNETES_TENANTS_MAX_BYTES": strconv.FormatInt(defaultAccessDocumentSize, 10),
		},
		"management minimum": {
			"QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE":      "management.json",
			"QUEUE_CONTROL_MANAGEMENT_TENANTS_MAX_BYTES": "1",
		},
		"management maximum": {
			"QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE":      "management.json",
			"QUEUE_CONTROL_MANAGEMENT_TENANTS_MAX_BYTES": strconv.FormatInt(defaultAccessDocumentSize, 10),
		},
	}
	for name, overrides := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			environment := map[string]string{
				"DATABASE_URL": "postgres://database/control", "QUEUE_CONTROL_ACCESS_FILE": "access.json",
			}
			for key, value := range overrides {
				environment[key] = value
			}
			if _, err := LoadConfig(mapEnvironment(environment)); err != nil {
				t.Fatalf("LoadConfig(exact bound) error = %v", err)
			}
		})
	}
}

func TestDocumentSizeParsingSeparatesSyntaxAndBounds(t *testing.T) {
	t.Parallel()

	for encoded, want := range map[string]bool{
		"1": true, strconv.FormatInt(defaultAccessDocumentSize, 10): true,
		"0": false, "-1": false,
		strconv.FormatInt(defaultAccessDocumentSize+1, 10): false,
		"invalid": false, "999999999999999999999999999999": false,
	} {
		_, ok := parseDocumentSize(encoded)
		if ok != want {
			t.Fatalf("parseDocumentSize(%q) valid = %t, want %t", encoded, ok, want)
		}
	}
}

func TestConfigRejectsUIInEachOneShotMode(t *testing.T) {
	t.Parallel()

	for _, mode := range []map[string]string{
		{"QUEUE_CONTROL_MIGRATE_ONLY": "true"},
		{"QUEUE_CONTROL_RETENTION_ONLY": "true", "QUEUE_CONTROL_RETENTION_FILE": "retention.json"},
	} {
		environment := map[string]string{
			"DATABASE_URL": "postgres://database/control", "QUEUE_CONTROL_UI_ENABLED": "true",
		}
		for key, value := range mode {
			environment[key] = value
		}
		if _, err := LoadConfig(mapEnvironment(environment)); !errors.Is(err, ErrInvalidRuntimeConfiguration) {
			t.Fatalf("LoadConfig(UI one-shot) error = %v", err)
		}
	}
}

func TestInvalidProcessDependenciesRejectsEveryMissingDependency(t *testing.T) {
	t.Parallel()

	valid := validProcessDependencies(t)
	mutations := []func(*processDependencies){
		func(value *processDependencies) { value.buildInfo = nil },
		func(value *processDependencies) { value.buildTelemetry = nil },
		func(value *processDependencies) { value.loadAccess = nil },
		func(value *processDependencies) { value.migrate = nil },
		func(value *processDependencies) { value.retain = nil },
		func(value *processDependencies) { value.loadWorkloads = nil },
		func(value *processDependencies) { value.routeDispatchers = nil },
		func(value *processDependencies) { value.loadManagement = nil },
		func(value *processDependencies) { value.openPool = nil },
		func(value *processDependencies) { value.buildPersistence = nil },
		func(value *processDependencies) { value.buildRateLimiter = nil },
		func(value *processDependencies) { value.listen = nil },
		func(value *processDependencies) { value.buildServer = nil },
		func(value *processDependencies) { value.dispatcher = nil },
	}
	if invalidProcessDependencies(valid) {
		t.Fatal("valid dependencies reported invalid")
	}
	for index, mutate := range mutations {
		candidate := valid
		mutate(&candidate)
		if !invalidProcessDependencies(candidate) {
			t.Fatalf("missing dependency %d accepted", index)
		}
	}
}

func TestRunProcessRejectsEachIncompleteOptionalRuntime(t *testing.T) {
	t.Parallel()

	managementFields := []func(*managementRuntime){
		func(value *managementRuntime) { value.Workers = nil },
		func(value *managementRuntime) { value.Queues = nil },
		func(value *managementRuntime) { value.Records = nil },
		func(value *managementRuntime) { value.Dispatcher = nil },
	}
	for index, clear := range managementFields {
		dependencies := validProcessDependencies(t)
		dependencies.loadManagement = func(string, int64) (managementRuntime, error) {
			value := managementRuntime{
				Workers: applicationRemoteWorkerSource{}, Queues: applicationQueueSource{},
				Records: applicationRecordSource{}, Dispatcher: managementProcessDispatcher{},
			}
			clear(&value)

			return value, nil
		}
		err := runProcess(context.Background(), mapEnvironment(map[string]string{
			"DATABASE_URL": "postgres://database/control", "QUEUE_CONTROL_ACCESS_FILE": "access.json",
			"QUEUE_CONTROL_MANAGEMENT_TENANTS_FILE": "management.json",
		}), dependencies)
		if !errors.Is(err, ErrInvalidManagementRuntime) {
			t.Fatalf("management field %d error = %v", index, err)
		}
	}

	workloadFields := []func(*workloadRuntime){
		func(value *workloadRuntime) { value.Source = nil },
		func(value *workloadRuntime) { value.Dispatcher = nil },
	}
	for index, clear := range workloadFields {
		dependencies := validProcessDependencies(t)
		dependencies.loadWorkloads = func(string, int64) (workloadRuntime, error) {
			value := workloadRuntime{Source: applicationWorkloadSource{}, Dispatcher: applicationDispatcher{}}
			clear(&value)

			return value, nil
		}
		err := runProcess(context.Background(), mapEnvironment(map[string]string{
			"DATABASE_URL": "postgres://database/control", "QUEUE_CONTROL_ACCESS_FILE": "access.json",
			"QUEUE_CONTROL_KUBERNETES_TENANTS_FILE": "tenants.json",
		}), dependencies)
		if !errors.Is(err, ErrInvalidWorkloadRuntime) {
			t.Fatalf("workload field %d error = %v", index, err)
		}
	}
}

func TestManagementBoundariesAreExact(t *testing.T) {
	t.Parallel()

	valid := managementTenantEntry{
		ID: "tenant", BaseURL: "https://worker.example", TokenFile: "token",
	}
	validParser := func(string) (*url.URL, error) {
		return &url.URL{Scheme: "https", Host: "worker.example"}, nil
	}
	if invalidManagementTenantWithParser(valid, validParser) {
		t.Fatal("valid management tenant rejected")
	}
	for name, tenant := range map[string]managementTenantEntry{
		"empty id":        {ID: "", BaseURL: valid.BaseURL, TokenFile: valid.TokenFile},
		"oversized id":    {ID: strings.Repeat("x", controlplane.MaxIdentityBytes+1), BaseURL: valid.BaseURL, TokenFile: valid.TokenFile},
		"empty URL":       {ID: valid.ID, BaseURL: "", TokenFile: valid.TokenFile},
		"oversized URL":   {ID: valid.ID, BaseURL: strings.Repeat("x", maxManagementURLBytes+1), TokenFile: valid.TokenFile},
		"empty token":     {ID: valid.ID, BaseURL: valid.BaseURL, TokenFile: ""},
		"oversized token": {ID: valid.ID, BaseURL: valid.BaseURL, TokenFile: strings.Repeat("x", maxManagementPathBytes+1)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !invalidManagementTenantWithParser(tenant, validParser) {
				t.Fatal("invalid management tenant accepted")
			}
		})
	}
	exact := valid
	exact.ID = strings.Repeat("i", controlplane.MaxIdentityBytes)
	exact.BaseURL = strings.Repeat("u", maxManagementURLBytes)
	exact.TokenFile = strings.Repeat("t", maxManagementPathBytes)
	if invalidManagementTenantWithParser(exact, validParser) {
		t.Fatal("exact management tenant bounds rejected")
	}
}

func TestManagementURLRestrictionsAreIndependent(t *testing.T) {
	t.Parallel()

	tenant := managementTenantEntry{ID: "tenant", BaseURL: "https://worker.example", TokenFile: "token"}
	invalidURLs := []*url.URL{
		{Scheme: "http", Host: "worker.example"},
		{Scheme: "https"},
		{Scheme: "https", Host: "worker.example", User: url.User("user")},
		{Scheme: "https", Host: "worker.example", Path: "/api"},
		{Scheme: "https", Host: "worker.example", RawQuery: "x=1"},
		{Scheme: "https", Host: "worker.example", Fragment: "fragment"},
	}
	for index, endpoint := range invalidURLs {
		if !invalidManagementTenantWithParser(tenant, func(string) (*url.URL, error) { return endpoint, nil }) {
			t.Fatalf("invalid URL %d accepted", index)
		}
	}
}

func TestReadBoundedManagementAcceptsExactLimit(t *testing.T) {
	t.Parallel()

	data, err := readBoundedManagement(strings.NewReader("abcd"), 4)
	if err != nil || string(data) != "abcd" {
		t.Fatalf("readBoundedManagement(exact) = (%q, %v)", data, err)
	}
}

func TestManagementRuntimeAcceptsMaximumTenantCount(t *testing.T) {
	t.Parallel()

	var document strings.Builder
	document.WriteString(`{"tenants":[`)
	for index := range maxManagementTenants {
		if index > 0 {
			document.WriteByte(',')
		}
		document.WriteString(`{"id":"tenant-` + strconv.Itoa(index) + `","base_url":"https://worker.example","token_file":"token"}`)
	}
	document.WriteString(`]}`)
	open := func(path string) (io.ReadCloser, error) {
		if path == "management.json" {
			return io.NopCloser(strings.NewReader(document.String())), nil
		}
		return io.NopCloser(strings.NewReader("secret")), nil
	}
	runtime, err := loadManagementRuntime("management.json", int64(document.Len()), open, func(string, string) (queue.StatusReader, error) {
		return &managementStatusStub{}, nil
	})
	if err != nil || missingDependency(runtime.Workers) || missingDependency(runtime.Queues) ||
		missingDependency(runtime.Records) || missingDependency(runtime.Dispatcher) {
		t.Fatalf("loadManagementRuntime(max tenants) = (%+v, %v)", runtime, err)
	}
}

func TestManagementFactoriesRejectErrorsAndMissingResultsIndependently(t *testing.T) {
	t.Parallel()

	stageErr := errors.New("factory failed")
	validDocument := `{"tenants":[{"id":"tenant","base_url":"https://worker.example","token_file":"token"}]}`
	open := func(path string) (io.ReadCloser, error) {
		if path == "management.json" {
			return io.NopCloser(strings.NewReader(validDocument)), nil
		}
		return io.NopCloser(strings.NewReader("secret")), nil
	}
	status := func(string, string) (queue.StatusReader, error) { return &managementStatusStub{}, nil }
	validSource := dataplane.NewStatusSource
	validRecords := dataplane.NewRecordSource
	validFleet := dataplane.NewFleetSource
	validDispatcher := newProductionManagementDispatcher
	tests := []func() (managementRuntime, error){
		func() (managementRuntime, error) {
			return loadManagementRuntimeWithSource("management.json", 1024, open, func(string, string) (queue.StatusReader, error) { return &managementStatusStub{}, stageErr }, validSource, validRecords, validFleet, validDispatcher)
		},
		func() (managementRuntime, error) {
			return loadManagementRuntimeWithSource("management.json", 1024, open, status, func(dataplane.StatusReaderResolver) (*dataplane.StatusSource, error) {
				return &dataplane.StatusSource{}, stageErr
			}, validRecords, validFleet, validDispatcher)
		},
		func() (managementRuntime, error) {
			return loadManagementRuntimeWithSource("management.json", 1024, open, status, validSource, func(dataplane.RecordReaderResolver) (*dataplane.RecordSource, error) {
				return &dataplane.RecordSource{}, stageErr
			}, validFleet, validDispatcher)
		},
		func() (managementRuntime, error) {
			return loadManagementRuntimeWithSource("management.json", 1024, open, status, validSource, validRecords, func(dataplane.WorkerStatusSource) (*dataplane.FleetSource, error) {
				return &dataplane.FleetSource{}, stageErr
			}, validDispatcher)
		},
		func() (managementRuntime, error) {
			return loadManagementRuntimeWithSource("management.json", 1024, open, status, validSource, validRecords, validFleet, func(dataplane.ControllerResolver) (control.Dispatcher, error) {
				return applicationDispatcher{}, stageErr
			})
		},
	}
	for index, run := range tests {
		runtime, err := run()
		if runtime != (managementRuntime{}) || !errors.Is(err, ErrInvalidManagementRuntime) {
			t.Fatalf("factory %d = (%+v, %v)", index, runtime, err)
		}
	}
}

func TestManagementRuntimeRejectsEachMissingFactory(t *testing.T) {
	t.Parallel()

	document := `{"tenants":[{"id":"tenant","base_url":"https://worker.example","token_file":"token"}]}`
	open := func(path string) (io.ReadCloser, error) {
		if path == "management.json" {
			return io.NopCloser(strings.NewReader(document)), nil
		}
		return io.NopCloser(strings.NewReader("secret")), nil
	}
	status := func(string, string) (queue.StatusReader, error) { return &managementStatusStub{}, nil }
	tests := []func() (managementRuntime, error){
		func() (managementRuntime, error) {
			return loadManagementRuntimeWithSource("management.json", 1024, nil, status, dataplane.NewStatusSource, dataplane.NewRecordSource, dataplane.NewFleetSource, newProductionManagementDispatcher)
		},
		func() (managementRuntime, error) {
			return loadManagementRuntimeWithSource("management.json", 1024, open, nil, dataplane.NewStatusSource, dataplane.NewRecordSource, dataplane.NewFleetSource, newProductionManagementDispatcher)
		},
		func() (managementRuntime, error) {
			return loadManagementRuntimeWithSource("management.json", 1024, open, status, nil, dataplane.NewRecordSource, dataplane.NewFleetSource, newProductionManagementDispatcher)
		},
		func() (managementRuntime, error) {
			return loadManagementRuntimeWithSource("management.json", 1024, open, status, dataplane.NewStatusSource, nil, dataplane.NewFleetSource, newProductionManagementDispatcher)
		},
		func() (managementRuntime, error) {
			return loadManagementRuntimeWithSource("management.json", 1024, open, status, dataplane.NewStatusSource, dataplane.NewRecordSource, nil, newProductionManagementDispatcher)
		},
		func() (managementRuntime, error) {
			return loadManagementRuntimeWithSource("management.json", 1024, open, status, dataplane.NewStatusSource, dataplane.NewRecordSource, dataplane.NewFleetSource, nil)
		},
	}
	for index, run := range tests {
		runtime, err := run()
		if runtime != (managementRuntime{}) || !errors.Is(err, ErrInvalidManagementRuntime) {
			t.Fatalf("missing factory %d = (%+v, %v)", index, runtime, err)
		}
	}

	if runtime, err := loadManagementRuntime("management.json", defaultAccessDocumentSize, open, status); err != nil || missingDependency(runtime.Queues) {
		t.Fatalf("exact maximum document bound = (%+v, %v)", runtime, err)
	}
	for _, maxBytes := range []int64{0, defaultAccessDocumentSize + 1} {
		runtime, err := loadManagementRuntime("management.json", maxBytes, open, status)
		if runtime != (managementRuntime{}) || !errors.Is(err, ErrInvalidManagementRuntime) {
			t.Fatalf("invalid document bound %d = (%+v, %v)", maxBytes, runtime, err)
		}
	}
	for maxBytes, want := range map[int64]bool{
		0: false, 1: true, defaultAccessDocumentSize: true,
		defaultAccessDocumentSize + 1: false,
	} {
		if got := validManagementDocumentSize(maxBytes); got != want {
			t.Fatalf("validManagementDocumentSize(%d) = %t, want %t", maxBytes, got, want)
		}
	}
}

func TestRetentionPolicyAcceptsEveryExactBoundary(t *testing.T) {
	t.Parallel()

	if maximumRetention != 10*365*24*time.Hour {
		t.Fatalf("maximumRetention = %v", maximumRetention)
	}
	base := retentionPolicy{TenantID: "tenant", RetainFor: minimumRetention, BatchSize: 1, MaxBatches: 1}
	for _, policy := range []retentionPolicy{
		base,
		{TenantID: strings.Repeat("x", controlplane.MaxIdentityBytes), RetainFor: maximumRetention, BatchSize: controlpostgres.MaxAuditBatch, MaxBatches: maximumRetentionBatches},
	} {
		if !validRetentionPolicy(policy) {
			t.Fatalf("exact-boundary policy rejected: %+v", policy)
		}
	}
}

func TestRetentionDocumentAcceptsExactByteAndPlanBounds(t *testing.T) {
	t.Parallel()

	document := `{"tenants":[` + retentionPolicyJSON("tenant") + `]}`
	policies, err := loadRetentionPolicies(strings.NewReader(document), int64(len(document)))
	if err != nil || len(policies) != 1 {
		t.Fatalf("exact byte bound = (%+v, %v)", policies, err)
	}

	var many strings.Builder
	many.WriteString(`{"tenants":[`)
	for index := range maximumRetentionPlans {
		if index > 0 {
			many.WriteByte(',')
		}
		many.WriteString(retentionPolicyJSON("tenant-" + strconv.Itoa(index)))
	}
	many.WriteString(`]}`)
	policies, err = loadRetentionPolicies(strings.NewReader(many.String()), int64(many.Len()))
	if err != nil || len(policies) != maximumRetentionPlans {
		t.Fatalf("exact plan bound = (%d, %v)", len(policies), err)
	}
}

func TestRetentionDocumentSizeBoundaryIsExact(t *testing.T) {
	t.Parallel()

	for size, want := range map[int64]bool{-1: false, 0: false, 1: true} {
		if got := validRetentionDocumentSize(size); got != want {
			t.Fatalf("validRetentionDocumentSize(%d) = %t, want %t", size, got, want)
		}
	}
}

func TestRetentionDocumentRejectsReadFailure(t *testing.T) {
	t.Parallel()

	policies, err := loadRetentionPolicies(retentionFailingReader{}, 1)
	if policies != nil || !errors.Is(err, ErrInvalidRetentionDocument) {
		t.Fatalf("loadRetentionPolicies(read failure) = (%+v, %v)", policies, err)
	}
}

type retentionFailingReader struct{}

func (retentionFailingReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestApplyRetentionPreservesControlFlowAcrossPolicies(t *testing.T) {
	t.Parallel()

	now := time.Unix(100_000, 0).UTC()
	stageErr := errors.New("retain failed")
	policy := func(tenant string) retentionPolicy {
		return retentionPolicy{TenantID: tenant, RetainFor: time.Hour, BatchSize: 2, MaxBatches: 2}
	}
	tests := []struct {
		name     string
		policies []retentionPolicy
		audit    *retentionAuditStub
		wantErr  error
		verified []string
		retains  int
		commands int
	}{
		{name: "invalid continues", policies: []retentionPolicy{{}, policy("next")}, audit: &retentionAuditStub{}, wantErr: ErrInvalidRetentionPlan, verified: []string{"next", "next"}, retains: 1, commands: 1},
		{name: "legal hold continues", policies: []retentionPolicy{{TenantID: "held", RetainFor: time.Hour, BatchSize: 1, MaxBatches: 1, LegalHold: true}, policy("next")}, audit: &retentionAuditStub{}, verified: []string{"next", "next"}, retains: 1, commands: 1},
		{name: "pre verify continues", policies: []retentionPolicy{policy("first"), policy("next")}, audit: &retentionAuditStub{verifyErrors: []error{stageErr, nil, nil}}, wantErr: stageErr, verified: []string{"first", "next", "next"}, retains: 1, commands: 1},
		{name: "retain error breaks and continues", policies: []retentionPolicy{policy("first"), policy("next")}, audit: &retentionAuditStub{retainErr: stageErr}, wantErr: stageErr, verified: []string{"first", "first", "next", "next"}, retains: 2, commands: 0},
		{name: "post verify continues", policies: []retentionPolicy{policy("first"), policy("next")}, audit: &retentionAuditStub{verifyErrors: []error{nil, stageErr, nil, nil}}, wantErr: stageErr, verified: []string{"first", "first", "next", "next"}, retains: 2, commands: 1},
		{name: "partial batches break", policies: []retentionPolicy{policy("only")}, audit: &retentionAuditStub{}, verified: []string{"only", "only"}, retains: 1, commands: 1},
		{name: "command error breaks", policies: []retentionPolicy{policy("only")}, audit: &retentionAuditStub{commandErr: stageErr}, wantErr: stageErr, verified: []string{"only", "only"}, retains: 1, commands: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := applyRetention(context.Background(), test.audit, test.policies, func() time.Time { return now })
			if test.wantErr == nil && err != nil || test.wantErr != nil && !errors.Is(err, test.wantErr) {
				t.Fatalf("applyRetention() error = %v, want %v", err, test.wantErr)
			}
			if !reflect.DeepEqual(test.audit.verified, test.verified) || len(test.audit.cutoffs) != test.retains || len(test.audit.commandCutoffs) != test.commands {
				t.Fatalf("calls = verified %v, retention %d, commands %d", test.audit.verified, len(test.audit.cutoffs), len(test.audit.commandCutoffs))
			}
		})
	}
}

func TestPresentRetentionAuditRejectsNilAndTypedNil(t *testing.T) {
	t.Parallel()

	if audit, ok := presentRetentionAudit(nil); ok || audit != nil {
		t.Fatalf("presentRetentionAudit(nil) = (%v, %t)", audit, ok)
	}
	var typedNil *retentionAuditStub
	if audit, ok := presentRetentionAudit(typedNil); ok || audit != nil {
		t.Fatalf("presentRetentionAudit(typed nil) = (%v, %t)", audit, ok)
	}
	want := &retentionAuditStub{}
	if audit, ok := presentRetentionAudit(want); !ok || audit != want {
		t.Fatalf("presentRetentionAudit(valid) = (%v, %t)", audit, ok)
	}
}

func TestApplyRetentionRejectsImpossibleMissingCheckedAudit(t *testing.T) {
	t.Parallel()

	var typedNil *retentionAuditStub
	err := applyRetention(context.Background(), typedNil, []retentionPolicy{{
		TenantID: "tenant", RetainFor: time.Hour, BatchSize: 1, MaxBatches: 1,
	}}, time.Now)
	if !errors.Is(err, ErrInvalidRetentionPlan) {
		t.Fatalf("applyRetention(typed nil) error = %v", err)
	}
}

func TestRequireRetentionAuditRejectsImpossibleNil(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("requireRetentionAudit(nil) did not panic")
		}
	}()
	requireRetentionAudit(nil)
}

func TestBuildApplicationUsesSuppliedClock(t *testing.T) {
	t.Parallel()

	now := time.Unix(1234, 0).UTC()
	journal := &clockJournal{}
	access, err := server.LoadStaticAccess(strings.NewReader(`{
		"keys":[{"id":"key-1","key":"secret-1","subject":"operator-1"}],
		"acl":[{"id":"drain-workers","subject":"operator-1","tenant":"tenant-1",
			"action":"drain","resource_type":"worker_group","resource_id":"payments","effect":"allow"}]
	}`), 1024)
	if err != nil {
		t.Fatalf("LoadStaticAccess() error = %v", err)
	}
	handler, err := buildApplication(Config{}, applicationDependencies{
		Access: access, Journal: journal, Dispatcher: applicationDispatcher{},
		RateLimiter: applicationRateLimiter{}, Now: func() time.Time { return now },
	})
	if err != nil || handler == nil {
		t.Fatalf("buildApplication() = (%v, %v)", handler, err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/tenants/tenant-1/commands", strings.NewReader(`{
		"idempotency_key":"request-1","reason":"maintenance","action":"drain",
		"target":{"kind":"worker_group","name":"payments"},"requested_at":"1970-01-01T00:01:00Z"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(server.APIKeyIDHeader, "key-1")
	request.Header.Set(server.APIKeySecretHeader, "secret-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("command status = %d, body = %s", response.Code, response.Body.String())
	}
	if !journal.dispatched.DispatchedAt.Equal(now) || !journal.completed.CompletedAt.Equal(now) {
		t.Fatalf("journal timestamps = dispatched %v, completed %v, want %v",
			journal.dispatched.DispatchedAt, journal.completed.CompletedAt, now)
	}
}

func TestSensitiveAccessAuditorRejectsTypedNilAndUnsupportedSources(t *testing.T) {
	t.Parallel()

	var typedNil *sensitiveAuditStub
	if sensitiveAccessAuditor(typedNil) != nil {
		t.Fatal("typed-nil sensitive auditor accepted")
	}
	if sensitiveAccessAuditor(applicationAuditSource{}) == nil {
		t.Fatal("valid sensitive auditor rejected")
	}
	if sensitiveAccessAuditor(auditOnlyStub{}) != nil {
		t.Fatal("non-sensitive audit source accepted")
	}
}

type sensitiveAuditStub struct{}

func (*sensitiveAuditStub) ListTenant(context.Context, string, uint64, uint32) (controlpostgres.AuditPage, error) {
	return controlpostgres.AuditPage{}, nil
}

func (*sensitiveAuditStub) AuditSensitiveAccess(context.Context, controlplane.SensitiveAccess) error {
	return nil
}

type auditOnlyStub struct{}

func (auditOnlyStub) ListTenant(context.Context, string, uint64, uint32) (controlpostgres.AuditPage, error) {
	return controlpostgres.AuditPage{}, nil
}

var _ apihttp.AuditSource = auditOnlyStub{}

type clockJournal struct {
	dispatched controlplane.CommandResult
	completed  controlplane.CommandResult
}

func (*clockJournal) Accept(context.Context, controlplane.Command) (controlplane.CommandResult, bool, error) {
	return controlplane.CommandResult{}, true, nil
}

func (journal *clockJournal) Complete(_ context.Context, result controlplane.CommandResult) error {
	journal.completed = result

	return nil
}

func (journal *clockJournal) MarkDispatched(_ context.Context, result controlplane.CommandResult) error {
	journal.dispatched = result

	return nil
}

func (*clockJournal) MarkAcknowledged(context.Context, controlplane.CommandResult) error { return nil }
