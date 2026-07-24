package main

import (
	"errors"
	"net/http"
	"reflect"
	"time"

	"github.com/faustbrian/golib/pkg/queue-control-plane/apihttp"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	"github.com/faustbrian/golib/pkg/queue-control-plane/server"
	controlui "github.com/faustbrian/golib/pkg/queue-control-plane/ui"
	"go.opentelemetry.io/otel/metric"
)

// ErrInvalidApplicationDependencies reports an incomplete process graph.
var ErrInvalidApplicationDependencies = errors.New("queue-control-plane: invalid application dependencies")

type applicationDependencies struct {
	Access         *server.StaticAccess
	Journal        control.Journal
	Dispatcher     control.Dispatcher
	RateLimiter    apihttp.RateLimiter
	Readiness      apihttp.Readiness
	Workers        apihttp.WorkerSource
	RemoteWorkers  apihttp.RemoteWorkerSource
	Workloads      apihttp.WorkloadSource
	Audit          apihttp.AuditSource
	CommandResults apihttp.CommandResultSource
	Records        apihttp.RecordSource
	Queues         apihttp.QueueSource
	DesiredState   apihttp.DesiredStateSource
	Build          apihttp.BuildInfo
	Now            func() time.Time
	Telemetry      *apihttp.TelemetryConfig
	Meter          metric.Meter
}

func buildApplication(config Config, dependencies applicationDependencies) (http.Handler, error) {
	if dependencies.Access == nil || missingDependency(dependencies.Journal) ||
		missingDependency(dependencies.Dispatcher) ||
		missingDependency(dependencies.RateLimiter) {
		return nil, ErrInvalidApplicationDependencies
	}
	now := dependencies.Now
	if now == nil {
		now = time.Now
	}

	service := control.NewService(dependencies.Access.Authorizer, dependencies.Journal, dependencies.Dispatcher, now)
	if dependencies.Telemetry != nil {
		var err error
		service, err = control.NewInstrumentedService(
			dependencies.Access.Authorizer,
			dependencies.Journal,
			dependencies.Dispatcher,
			now,
			dependencies.Meter,
		)
		if err != nil {
			return nil, err
		}
	}
	capabilities := applicationCapabilities(dependencies)
	var viewer apihttp.Viewer
	if len(capabilities) > 1 {
		viewer = dependencies.Access.Authorizer
	}
	var sensitiveAudit apihttp.SensitiveAccessAuditor
	if candidate, ok := dependencies.Audit.(apihttp.SensitiveAccessAuditor); ok &&
		!missingDependency(candidate) {
		sensitiveAudit = candidate
	}
	api, err := apihttp.NewHandler(apihttp.Config{
		Commands:           service,
		Readiness:          dependencies.Readiness,
		Build:              dependencies.Build,
		Capabilities:       capabilities,
		Workers:            dependencies.Workers,
		RemoteWorkers:      dependencies.RemoteWorkers,
		Workloads:          dependencies.Workloads,
		Viewer:             viewer,
		Audit:              dependencies.Audit,
		SensitiveAudit:     sensitiveAudit,
		CommandResults:     dependencies.CommandResults,
		Records:            dependencies.Records,
		Queues:             dependencies.Queues,
		DesiredState:       dependencies.DesiredState,
		Now:                now,
		WorkflowLimiter:    dependencies.RateLimiter,
		Protocol:           applicationProtocolRange(),
		WorkerCapabilities: applicationWorkerCapabilities(),
		Telemetry:          dependencies.Telemetry,
	})
	if err != nil {
		return nil, err
	}
	surface := api
	if config.UIEnabled {
		router := http.NewServeMux()
		router.HandleFunc("GET /ui", func(writer http.ResponseWriter, request *http.Request) {
			http.Redirect(writer, request, "/ui/", http.StatusTemporaryRedirect)
		})
		router.Handle("/ui/", http.StripPrefix("/ui/", controlui.NewHandler()))
		router.Handle("/", api)
		surface = router
	}

	return server.NewAdministrativeHandler(
		surface,
		dependencies.Access.Extractor,
		dependencies.Access.Authenticator,
		apihttp.SecurityConfig{
			AllowedOrigins: config.AllowedOrigins,
			RateLimiter:    dependencies.RateLimiter,
		},
	)
}

func applicationProtocolRange() fleet.ProtocolRange {
	version := fleet.ProtocolVersion{Major: 1}

	return fleet.ProtocolRange{Minimum: version, Maximum: version}
}

func applicationWorkerCapabilities() []fleet.Capability {
	return []fleet.Capability{
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
}

func applicationCapabilities(dependencies applicationDependencies) []string {
	capabilities := []string{"commands"}
	if !missingDependency(dependencies.Workers) || !missingDependency(dependencies.RemoteWorkers) {
		capabilities = append(capabilities, "workers")
	}
	if !missingDependency(dependencies.Workloads) {
		capabilities = append(capabilities, "workloads")
	}
	if !missingDependency(dependencies.Audit) {
		capabilities = append(capabilities, "audit")
	}
	if !missingDependency(dependencies.CommandResults) {
		capabilities = append(capabilities, "command_results")
	}
	if !missingDependency(dependencies.Records) {
		capabilities = append(capabilities, "records")
	}
	if !missingDependency(dependencies.Queues) {
		capabilities = append(capabilities, "queues")
	}
	if !missingDependency(dependencies.DesiredState) {
		capabilities = append(capabilities, "desired_state")
	}

	return capabilities
}

func missingDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
