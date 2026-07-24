// Package apihttp provides the versioned administrative HTTP API.
package apihttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"reflect"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/authz"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	"github.com/faustbrian/golib/pkg/queue-control-plane/fleet"
	controlkubernetes "github.com/faustbrian/golib/pkg/queue-control-plane/kubernetes"
	controlpostgres "github.com/faustbrian/golib/pkg/queue-control-plane/postgres"
	queue "github.com/faustbrian/golib/pkg/queue/management"
	telemetryhttp "github.com/faustbrian/golib/pkg/telemetry/instrumentation/nethttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const defaultMaxRequestBytes int64 = 1 << 20

var ErrInvalidConfiguration = errors.New("apihttp: invalid configuration")

// CommandExecutor runs an authenticated administrative command.
type CommandExecutor interface {
	Execute(context.Context, controlplane.Command) (controlplane.CommandResult, error)
}

// Readiness checks dependencies without changing process liveness.
type Readiness interface {
	Ready(context.Context) error
}

// WorkerSource provides bounded tenant-scoped worker snapshots.
type WorkerSource interface {
	SnapshotTenant(string, time.Time, time.Duration) fleet.RegistrySnapshot
}

// RemoteWorkerSource provides cancellable tenant-scoped worker snapshots.
type RemoteWorkerSource interface {
	SnapshotTenant(
		context.Context,
		string,
		time.Time,
		time.Duration,
	) (fleet.RegistrySnapshot, error)
}

// WorkloadSource provides bounded tenant-scoped Kubernetes visibility.
type WorkloadSource interface {
	ListTenantWorkloads(context.Context, string, int64, string) (controlkubernetes.Page, error)
}

// Viewer authorizes diagnostic reads against tenant resources.
type Viewer interface {
	Authorize(
		context.Context,
		string,
		string,
		controlplane.Permission,
		controlplane.Target,
	) error
}

// AuditSource reads bounded tenant audit-history pages.
type AuditSource interface {
	ListTenant(context.Context, string, uint64, uint32) (controlpostgres.AuditPage, error)
}

// SensitiveAccessAuditor durably records privileged record reads.
type SensitiveAccessAuditor interface {
	AuditSensitiveAccess(context.Context, controlplane.SensitiveAccess) error
}

// CommandResultSource reads tenant-scoped durable command outcomes.
type CommandResultSource interface {
	Get(context.Context, string, string) (controlplane.CommandResult, error)
}

// RecordSource reads tenant-scoped queue failures and dead letters.
type RecordSource interface {
	ListFailures(context.Context, string, queue.PageRequest) (queue.RecordPage, error)
	ListDeadLetters(context.Context, string, queue.PageRequest) (queue.RecordPage, error)
	Inspect(context.Context, string, queue.InspectRequest) (queue.JobRecord, error)
}

// QueueSource reads tenant-scoped queue status pages.
type QueueSource interface {
	ListQueues(context.Context, string, queue.StatusPageRequest) (queue.QueueStatusPage, error)
}

// DesiredStateSource reads one tenant-scoped durable convergence record.
type DesiredStateSource interface {
	Get(context.Context, string, controlplane.Target) (control.DesiredRecord, error)
}

type commandHistorySource interface {
	ListTenant(context.Context, string, string, uint32) (controlpostgres.CommandPage, error)
}

// BuildInfo is immutable release metadata exposed to automation.
type BuildInfo struct {
	Version string    `json:"version"`
	Commit  string    `json:"commit"`
	BuiltAt time.Time `json:"built_at"`
}

// Config defines bounded dependencies for the administrative API.
type Config struct {
	Commands           CommandExecutor
	MaxRequestBytes    int64
	Readiness          Readiness
	Build              BuildInfo
	Capabilities       []string
	Workers            WorkerSource
	RemoteWorkers      RemoteWorkerSource
	Workloads          WorkloadSource
	Viewer             Viewer
	Now                func() time.Time
	NewCommandID       func() (string, error)
	WorkflowLimiter    RateLimiter
	StaleAfter         time.Duration
	Protocol           fleet.ProtocolRange
	WorkerCapabilities []fleet.Capability
	Telemetry          *TelemetryConfig
	Audit              AuditSource
	SensitiveAudit     SensitiveAccessAuditor
	CommandResults     CommandResultSource
	Records            RecordSource
	Queues             QueueSource
	DesiredState       DesiredStateSource
}

// TelemetryConfig supplies standard OpenTelemetry APIs owned by telemetry.
type TelemetryConfig struct {
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator
	TrustedInbound bool
}

// Problem is the stable secret-safe API error envelope.
type Problem struct {
	Code string `json:"code"`
}

type handler struct {
	commands           CommandExecutor
	maxRequestBytes    int64
	readiness          Readiness
	build              BuildInfo
	capabilities       []string
	workers            WorkerSource
	remoteWorkers      RemoteWorkerSource
	workloads          WorkloadSource
	viewer             Viewer
	now                func() time.Time
	newCommandID       func() (string, error)
	workflowLimiter    RateLimiter
	staleAfter         time.Duration
	protocol           fleet.ProtocolRange
	workerCapabilities []fleet.Capability
	audit              AuditSource
	sensitiveAudit     SensitiveAccessAuditor
	commandResults     CommandResultSource
	commandHistory     commandHistorySource
	records            RecordSource
	queues             QueueSource
	desiredState       DesiredStateSource
}

// CommandRequest is the actor-free JSON mutation accepted by the API.
type CommandRequest struct {
	IdempotencyKey string              `json:"idempotency_key"`
	Reason         string              `json:"reason"`
	Action         controlplane.Action `json:"action"`
	Target         TargetRequest       `json:"target"`
	RequestedAt    time.Time           `json:"requested_at"`
	Confirmed      bool                `json:"confirmed"`
	Selection      *SelectionRequest   `json:"selection,omitempty"`
	Replay         *ReplayRequest      `json:"replay,omitempty"`
	Scale          *ScaleRequest       `json:"scale,omitempty"`
}

// TargetRequest identifies a command target without backend addressing.
type TargetRequest struct {
	Kind controlplane.TargetKind `json:"kind"`
	Name string                  `json:"name"`
}

// SelectionRequest bounds a bulk administrative mutation.
type SelectionRequest struct {
	Limit uint32 `json:"limit"`
}

// ReplayRequest declares explicit replay destination and idempotency policy.
type ReplayRequest struct {
	Destination       string                    `json:"destination"`
	IdempotencyPolicy controlplane.ReplayPolicy `json:"idempotency_policy"`
}

// ScaleRequest declares the desired workload replica count.
type ScaleRequest struct {
	Replicas uint32 `json:"replicas"`
}

// NewHandler creates the versioned administrative HTTP handler.
func NewHandler(config Config) (http.Handler, error) {
	if nilInterface(config.Commands) || config.MaxRequestBytes < 0 || config.StaleAfter < 0 ||
		(config.Workers != nil && nilInterface(config.Workers)) ||
		(config.RemoteWorkers != nil && nilInterface(config.RemoteWorkers)) ||
		(config.Workers != nil && config.RemoteWorkers != nil) ||
		(config.Workloads != nil && nilInterface(config.Workloads)) ||
		(config.Audit != nil && nilInterface(config.Audit)) ||
		(config.SensitiveAudit != nil && nilInterface(config.SensitiveAudit)) ||
		(config.CommandResults != nil && nilInterface(config.CommandResults)) ||
		(config.Records != nil && nilInterface(config.Records)) ||
		(config.Queues != nil && nilInterface(config.Queues)) ||
		(config.DesiredState != nil && nilInterface(config.DesiredState)) ||
		(config.Viewer != nil && nilInterface(config.Viewer)) ||
		(config.WorkflowLimiter != nil && nilInterface(config.WorkflowLimiter)) ||
		(config.Workers == nil && config.RemoteWorkers == nil &&
			config.Workloads == nil && config.Audit == nil &&
			config.CommandResults == nil && config.Records == nil &&
			config.Queues == nil && config.DesiredState == nil) != (config.Viewer == nil) {
		return nil, ErrInvalidConfiguration
	}
	if config.MaxRequestBytes == 0 {
		config.MaxRequestBytes = defaultMaxRequestBytes
	}
	config.Build.BuiltAt = config.Build.BuiltAt.UTC()
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.NewCommandID == nil {
		config.NewCommandID = controlplane.NewCommandID
	}
	if config.StaleAfter == 0 {
		config.StaleAfter = 30 * time.Second
	}

	var commandHistory commandHistorySource
	if source, ok := config.CommandResults.(commandHistorySource); ok && !nilInterface(source) {
		commandHistory = source
	}
	handler := &handler{
		commands:           config.Commands,
		maxRequestBytes:    config.MaxRequestBytes,
		readiness:          config.Readiness,
		build:              config.Build,
		capabilities:       append([]string(nil), config.Capabilities...),
		workers:            config.Workers,
		remoteWorkers:      config.RemoteWorkers,
		workloads:          config.Workloads,
		viewer:             config.Viewer,
		now:                config.Now,
		newCommandID:       config.NewCommandID,
		workflowLimiter:    config.WorkflowLimiter,
		staleAfter:         config.StaleAfter,
		protocol:           config.Protocol,
		workerCapabilities: append([]fleet.Capability(nil), config.WorkerCapabilities...),
		audit:              config.Audit,
		sensitiveAudit:     config.SensitiveAudit,
		commandResults:     config.CommandResults,
		commandHistory:     commandHistory,
		records:            config.Records,
		queues:             config.Queues,
		desiredState:       config.DesiredState,
	}
	router := http.NewServeMux()
	router.HandleFunc("GET /health/live", handler.liveness)
	router.HandleFunc("GET /health/ready", handler.ready)
	router.HandleFunc("GET /version", handler.version)
	router.HandleFunc("GET /v1/capabilities", handler.listCapabilities)
	router.HandleFunc("POST /v1/tenants/{tenant}/commands", handler.executeCommand)
	if handler.workers != nil || handler.remoteWorkers != nil {
		router.HandleFunc("GET /v1/tenants/{tenant}/workers", handler.listWorkers)
	}
	if handler.workloads != nil {
		router.HandleFunc("GET /v1/tenants/{tenant}/workloads", handler.listWorkloads)
	}
	if handler.audit != nil {
		router.HandleFunc("GET /v1/tenants/{tenant}/audit", handler.listAudit)
	}
	if handler.commandHistory != nil {
		router.HandleFunc("GET /v1/tenants/{tenant}/commands", handler.listCommandHistory)
	}
	if handler.commandResults != nil {
		router.HandleFunc("GET /v1/tenants/{tenant}/commands/{key}", handler.getCommandResult)
	}
	if handler.records != nil {
		router.HandleFunc("GET /v1/tenants/{tenant}/failures", handler.listFailures)
		router.HandleFunc("GET /v1/tenants/{tenant}/dead-letters", handler.listDeadLetters)
		router.HandleFunc("GET /v1/tenants/{tenant}/failures/{record}", handler.inspectFailure)
		router.HandleFunc("GET /v1/tenants/{tenant}/dead-letters/{record}", handler.inspectDeadLetter)
	}
	if handler.queues != nil {
		router.HandleFunc("GET /v1/tenants/{tenant}/queues", handler.listQueues)
	}
	if handler.desiredState != nil {
		router.HandleFunc(
			"GET /v1/tenants/{tenant}/desired-state/{kind}/{name}",
			handler.getDesiredState,
		)
	}

	if config.Telemetry == nil {
		return router, nil
	}

	return telemetryhttp.NewHandler(router, telemetryhttp.ServerConfig{
		Operation:      "queue_control_plane.http.server",
		TrustedInbound: config.Telemetry.TrustedInbound,
		TracerProvider: config.Telemetry.TracerProvider,
		MeterProvider:  config.Telemetry.MeterProvider,
		Propagator:     config.Telemetry.Propagator,
	})
}

func (h *handler) liveness(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "live"})
}

func (h *handler) ready(writer http.ResponseWriter, request *http.Request) {
	if h.readiness != nil {
		if err := h.readiness.Ready(request.Context()); err != nil {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
			return
		}
	}

	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

func (h *handler) version(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, h.build)
}

func (h *handler) listCapabilities(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, struct {
		Capabilities []string `json:"capabilities"`
	}{Capabilities: append([]string(nil), h.capabilities...)})
}

func (h *handler) executeCommand(writer http.ResponseWriter, request *http.Request) {
	principal, ok := authentication.PrincipalFromContext(request.Context())
	if !ok || principal.IsAnonymous() {
		writeProblem(writer, http.StatusUnauthorized, "unauthenticated")
		return
	}

	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeProblem(writer, http.StatusUnsupportedMediaType, "unsupported_media_type")
		return
	}

	var input CommandRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, h.maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeDecodeProblem(writer, err)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}

	command := input.command(
		request.PathValue("tenant"), principal.Subject(), principal.Method(),
	)
	if h.workflowLimiter != nil && !h.workflowLimiter.Allow(
		request.Context(), workflowRateLimitKey(principal.Subject(), string(command.Action)),
	) {
		writer.Header().Set("Retry-After", "1")
		writeProblem(writer, http.StatusTooManyRequests, "rate_limited")
		return
	}
	result, err := h.commands.Execute(request.Context(), command)
	if err != nil {
		writeCommandError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, result)
}

func workflowRateLimitKey(subject string, operation string) string {
	return "subject:" + subject + "|workflow:" + operation
}

func (r CommandRequest) command(
	tenant string,
	actor string,
	authenticationMethod string,
) controlplane.Command {
	command := controlplane.Command{
		IdempotencyKey:       r.IdempotencyKey,
		TenantID:             tenant,
		Actor:                actor,
		AuthenticationMethod: authenticationMethod,
		Reason:               r.Reason,
		Action:               r.Action,
		Target: controlplane.Target{
			Kind: r.Target.Kind,
			Name: r.Target.Name,
		},
		RequestedAt: r.RequestedAt,
		Confirmed:   r.Confirmed,
	}
	if r.Selection != nil {
		command.Selection = &controlplane.Selection{Limit: r.Selection.Limit}
	}
	if r.Replay != nil {
		command.Replay = &controlplane.Replay{
			Destination:       r.Replay.Destination,
			IdempotencyPolicy: r.Replay.IdempotencyPolicy,
		}
	}
	if r.Scale != nil {
		command.Scale = &controlplane.Scale{Replicas: r.Scale.Replicas}
	}

	return command
}

func writeDecodeProblem(writer http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		writeProblem(writer, http.StatusRequestEntityTooLarge, "request_too_large")
		return
	}

	writeProblem(writer, http.StatusBadRequest, "invalid_request")
}

func writeCommandError(writer http.ResponseWriter, err error) {
	var validationError *controlplane.ValidationError
	switch {
	case errors.As(err, &validationError):
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, controlpostgres.ErrInvalidCommandRequest):
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, authz.ErrUnauthenticated):
		writeProblem(writer, http.StatusUnauthorized, "unauthenticated")
	case errors.Is(err, authz.ErrDenied), errors.Is(err, authz.ErrActorMismatch):
		writeProblem(writer, http.StatusForbidden, "forbidden")
	case errors.Is(err, controlpostgres.ErrIdempotencyConflict):
		writeProblem(writer, http.StatusConflict, "idempotency_conflict")
	case errors.Is(err, controlpostgres.ErrCommandNotFound):
		writeProblem(writer, http.StatusNotFound, "command_not_found")
	case errors.Is(err, control.ErrOutcomeUnknown):
		writeProblem(writer, http.StatusServiceUnavailable, "outcome_unknown")
	default:
		writeProblem(writer, http.StatusInternalServerError, "internal_error")
	}
}

func writeProblem(writer http.ResponseWriter, status int, code string) {
	writeJSON(writer, status, Problem{Code: code})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	return reflected.Kind() == reflect.Pointer && reflected.IsNil()
}
