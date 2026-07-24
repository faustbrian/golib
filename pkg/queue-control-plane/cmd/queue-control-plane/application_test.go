package main

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
	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	controlkubernetes "github.com/faustbrian/golib/pkg/queue-control-plane/kubernetes"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
	"github.com/faustbrian/golib/pkg/queue-control-plane/server"
	queue "github.com/faustbrian/golib/pkg/queue/management"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestBuildApplicationRejectsIncompleteDependencies(t *testing.T) {
	t.Parallel()

	access := applicationAccess(t)
	valid := applicationDependencies{
		Access:      access,
		Journal:     &applicationJournal{},
		Dispatcher:  applicationDispatcher{},
		RateLimiter: applicationRateLimiter{},
	}
	for name, mutate := range map[string]func(*applicationDependencies){
		"access":       func(deps *applicationDependencies) { deps.Access = nil },
		"journal":      func(deps *applicationDependencies) { deps.Journal = nil },
		"dispatcher":   func(deps *applicationDependencies) { deps.Dispatcher = nil },
		"rate limiter": func(deps *applicationDependencies) { deps.RateLimiter = nil },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			deps := valid
			mutate(&deps)
			handler, err := buildApplication(Config{}, deps)
			if handler != nil || !errors.Is(err, ErrInvalidApplicationDependencies) {
				t.Fatalf("buildApplication() = (%v, %v), want nil and stable error", handler, err)
			}
		})
	}
}

func TestBuildApplicationComposesPublicHealthAndBoundedCapabilities(t *testing.T) {
	t.Parallel()

	handler, err := buildApplication(Config{}, applicationDependencies{
		Access:         applicationAccess(t),
		Journal:        &applicationJournal{},
		Dispatcher:     applicationDispatcher{},
		RateLimiter:    applicationRateLimiter{},
		Workers:        fleet.NewRegistry(1),
		Workloads:      applicationWorkloadSource{},
		Audit:          applicationAuditSource{},
		CommandResults: applicationCommandSource{},
		Records:        applicationRecordSource{},
		Queues:         applicationQueueSource{},
		Build: apihttp.BuildInfo{
			Version: "v1.0.0",
			Commit:  "abcdef",
			BuiltAt: time.Unix(1, 0),
		},
		Telemetry: &apihttp.TelemetryConfig{
			TracerProvider: tracenoop.NewTracerProvider(),
			MeterProvider:  metricnoop.NewMeterProvider(),
			Propagator:     propagation.TraceContext{},
		},
		Meter: metricnoop.NewMeterProvider().Meter("test"),
	})
	if err != nil {
		t.Fatalf("buildApplication() error = %v", err)
	}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}

	capabilities := httptest.NewRecorder()
	handler.ServeHTTP(capabilities, httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil))
	var response struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(capabilities.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	want := []string{"commands", "workers", "workloads", "audit", "command_results", "records", "queues"}
	if capabilities.Code != http.StatusOK || !reflect.DeepEqual(response.Capabilities, want) {
		t.Fatalf("capabilities = (%d, %v), want (200, %v)", capabilities.Code, response.Capabilities, want)
	}
}

func TestApplicationNegotiatesEveryCurrentGoQueueCapability(t *testing.T) {
	t.Parallel()

	workerCapabilities := []fleet.Capability{
		fleet.CapabilityBulkRetry,
		fleet.CapabilityDeadLetters,
		fleet.CapabilityDelete,
		fleet.CapabilityDrain,
		fleet.CapabilityFailures,
		fleet.CapabilityPause,
		fleet.CapabilityPurge,
		fleet.CapabilityQueueStatus,
		fleet.CapabilityReplay,
		fleet.CapabilityResume,
		fleet.CapabilityRetentionBytes,
		fleet.CapabilityRetentionCount,
		fleet.CapabilityRetentionTime,
		fleet.CapabilityRetry,
		fleet.CapabilityTerminate,
		fleet.CapabilityWorkerStatus,
	}
	compatibility := fleet.Negotiate(
		applicationProtocolRange(),
		fleet.ProtocolVersion{Major: 1},
		workerCapabilities,
		applicationWorkerCapabilities(),
	)
	if compatibility.State != fleet.CompatibilityCompatible ||
		!reflect.DeepEqual(compatibility.Enabled, workerCapabilities) {
		t.Fatalf("compatibility = %+v, want every current capability enabled", compatibility)
	}
}

func TestBuildApplicationRejectsTelemetryWithoutMeter(t *testing.T) {
	t.Parallel()

	handler, err := buildApplication(Config{}, applicationDependencies{
		Access:      applicationAccess(t),
		Journal:     &applicationJournal{},
		Dispatcher:  applicationDispatcher{},
		RateLimiter: applicationRateLimiter{},
		Telemetry:   &apihttp.TelemetryConfig{},
	})
	if handler != nil || err == nil {
		t.Fatalf("buildApplication() = (%v, %v), want telemetry error", handler, err)
	}
}

func TestBuildApplicationPropagatesInvalidReadSource(t *testing.T) {
	t.Parallel()

	var workers *fleet.Registry
	handler, err := buildApplication(Config{}, applicationDependencies{
		Access:      applicationAccess(t),
		Journal:     &applicationJournal{},
		Dispatcher:  applicationDispatcher{},
		RateLimiter: applicationRateLimiter{},
		Workers:     workers,
	})
	if handler != nil || !errors.Is(err, apihttp.ErrInvalidConfiguration) {
		t.Fatalf("buildApplication() = (%v, %v), want nil and API configuration error", handler, err)
	}
}

func TestBuildApplicationOptionallyServesEmbeddedConsole(t *testing.T) {
	t.Parallel()

	handler, err := buildApplication(Config{UIEnabled: true}, applicationDependencies{
		Access:      applicationAccess(t),
		Journal:     &applicationJournal{},
		Dispatcher:  applicationDispatcher{},
		RateLimiter: applicationRateLimiter{},
	})
	if err != nil {
		t.Fatalf("buildApplication() error = %v", err)
	}
	redirect := httptest.NewRecorder()
	handler.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "/ui", nil))
	if redirect.Code != http.StatusTemporaryRedirect || redirect.Header().Get("Location") != "/ui/" {
		t.Fatalf("GET /ui = (%d, %q)", redirect.Code, redirect.Header().Get("Location"))
	}

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Queue control plane") ||
		!strings.Contains(page.Header().Get("Content-Security-Policy"), "connect-src 'self'") {
		t.Fatalf("GET /ui/ = status %d headers %v body %q", page.Code, page.Header(), page.Body.String())
	}
}

func applicationAccess(t *testing.T) *server.StaticAccess {
	t.Helper()

	access, err := server.LoadStaticAccess(
		strings.NewReader(`{"keys":[{"id":"key-1","key":"secret-1","subject":"operator-1"}],"acl":[]}`),
		1024,
	)
	if err != nil {
		t.Fatalf("LoadStaticAccess() error = %v", err)
	}

	return access
}

type applicationJournal struct{}

func (*applicationJournal) Accept(
	context.Context,
	controlplane.Command,
) (controlplane.CommandResult, bool, error) {
	return controlplane.CommandResult{}, false, nil
}

func (*applicationJournal) Complete(context.Context, controlplane.CommandResult) error { return nil }

func (*applicationJournal) MarkDispatched(context.Context, controlplane.CommandResult) error {
	return nil
}

func (*applicationJournal) MarkAcknowledged(context.Context, controlplane.CommandResult) error {
	return nil
}

type applicationDispatcher struct{}

func (applicationDispatcher) Dispatch(context.Context, controlplane.Command) error { return nil }

type applicationRateLimiter struct{}

func (applicationRateLimiter) Allow(context.Context, string) bool { return true }

type applicationWorkloadSource struct{}

func (applicationWorkloadSource) ListTenantWorkloads(
	context.Context,
	string,
	int64,
	string,
) (controlkubernetes.Page, error) {
	return controlkubernetes.Page{}, nil
}

type applicationAuditSource struct{}

func (applicationAuditSource) AuditSensitiveAccess(
	context.Context,
	controlplane.SensitiveAccess,
) error {
	return nil
}

func (applicationAuditSource) ListTenant(
	context.Context,
	string,
	uint64,
	uint32,
) (controlpostgres.AuditPage, error) {
	return controlpostgres.AuditPage{}, nil
}

type applicationCommandSource struct{}

func (applicationCommandSource) Get(
	context.Context,
	string,
	string,
) (controlplane.CommandResult, error) {
	return controlplane.CommandResult{}, nil
}

type applicationRecordSource struct{}

type applicationQueueSource struct{}

func (applicationQueueSource) ListQueues(
	context.Context,
	string,
	queue.StatusPageRequest,
) (queue.QueueStatusPage, error) {
	return queue.QueueStatusPage{}, nil
}

func (applicationRecordSource) ListFailures(
	context.Context,
	string,
	queue.PageRequest,
) (queue.RecordPage, error) {
	return queue.RecordPage{}, nil
}

func (applicationRecordSource) ListDeadLetters(
	context.Context,
	string,
	queue.PageRequest,
) (queue.RecordPage, error) {
	return queue.RecordPage{}, nil
}

func (applicationRecordSource) Inspect(
	context.Context,
	string,
	queue.InspectRequest,
) (queue.JobRecord, error) {
	return queue.JobRecord{}, nil
}

func (applicationCommandSource) ListTenant(
	context.Context,
	string,
	string,
	uint32,
) (controlpostgres.CommandPage, error) {
	return controlpostgres.CommandPage{}, nil
}
