package apihttp

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	authentication "github.com/faustbrian/golib/pkg/authentication"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

const (
	defaultRecordPageSize       uint32 = 100
	sensitiveContentDisposition        = `attachment; filename="queue-record.json"`
)

// RecordPage is one stable bounded administrative record page.
type RecordPage struct {
	Records    []Record `json:"records"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// Record is the stable HTTP representation of a failed or dead-lettered job.
type Record struct {
	Kind                 queue.RecordKind     `json:"kind"`
	ID                   string               `json:"id"`
	Backend              string               `json:"backend"`
	Queue                string               `json:"queue"`
	OccurredAt           time.Time            `json:"occurred_at"`
	Attempts             uint32               `json:"attempts"`
	FailureCode          string               `json:"failure_code"`
	Payload              RecordPayload        `json:"payload"`
	EnvelopeVersion      uint16               `json:"envelope_version,omitempty"`
	PayloadSchemaVersion string               `json:"payload_schema_version,omitempty"`
	OriginalID           string               `json:"original_id,omitempty"`
	Topic                string               `json:"topic,omitempty"`
	Stream               string               `json:"stream,omitempty"`
	RoutingKey           string               `json:"routing_key,omitempty"`
	ConsumerGroup        string               `json:"consumer_group,omitempty"`
	SourceRecordID       string               `json:"source_record_id,omitempty"`
	EnqueuedAt           *time.Time           `json:"enqueued_at,omitempty"`
	FirstDeliveryAt      *time.Time           `json:"first_delivery_at,omitempty"`
	LastDeliveryAt       *time.Time           `json:"last_delivery_at,omitempty"`
	DeadLetteredAt       *time.Time           `json:"dead_lettered_at,omitempty"`
	RetryPolicy          string               `json:"retry_policy,omitempty"`
	Classification       queue.Classification `json:"classification,omitempty"`
	FailureSummary       string               `json:"failure_summary,omitempty"`
	Diagnostics          RecordPayload        `json:"diagnostics"`
	HandlerType          string               `json:"handler_type,omitempty"`
	JobType              string               `json:"job_type,omitempty"`
	Tags                 map[string]string    `json:"tags,omitempty"`
	TraceID              string               `json:"trace_id,omitempty"`
	TenantID             string               `json:"tenant_id,omitempty"`
	ProducerVersion      string               `json:"producer_version,omitempty"`
	WorkerVersion        string               `json:"worker_version,omitempty"`
	OriginalDeadLetterID string               `json:"original_dead_letter_id,omitempty"`
	PriorDeadLetterID    string               `json:"prior_dead_letter_id,omitempty"`
	ReplayGeneration     uint32               `json:"replay_generation,omitempty"`
	RetentionDeadline    *time.Time           `json:"retention_deadline,omitempty"`
}

// RecordPayload is hidden by default and contains bytes only after explicit
// privileged inspection.
type RecordPayload struct {
	Visibility  queue.PayloadVisibility `json:"visibility"`
	ContentType string                  `json:"content_type,omitempty"`
	Size        int64                   `json:"size"`
	Data        []byte                  `json:"data,omitempty"`
}

func (h *handler) listFailures(writer http.ResponseWriter, request *http.Request) {
	h.listRecords(writer, request, queue.RecordFailure)
}

func (h *handler) listDeadLetters(writer http.ResponseWriter, request *http.Request) {
	h.listRecords(writer, request, queue.RecordDeadLetter)
}

func (h *handler) listRecords(
	writer http.ResponseWriter,
	request *http.Request,
	kind queue.RecordKind,
) {
	principal, tenant, ok := h.authorizeRecordRead(
		writer, request, kind, collectionName(kind), controlplane.PermissionRecordList,
	)
	if !ok {
		return
	}
	query, err := parseRecordQuery(request.URL.Query())
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}

	var page queue.RecordPage
	if kind == queue.RecordFailure {
		page, err = h.records.ListFailures(request.Context(), tenant, query)
	} else {
		page, err = h.records.ListDeadLetters(request.Context(), tenant, query)
	}
	_ = principal
	if err != nil {
		writeProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(writer, http.StatusOK, recordPage(page))
}

func (h *handler) inspectFailure(writer http.ResponseWriter, request *http.Request) {
	h.inspectRecord(writer, request, queue.RecordFailure)
}

func (h *handler) inspectDeadLetter(writer http.ResponseWriter, request *http.Request) {
	h.inspectRecord(writer, request, queue.RecordDeadLetter)
}

func (h *handler) inspectRecord(
	writer http.ResponseWriter,
	request *http.Request,
	kind queue.RecordKind,
) {
	id := request.PathValue("record")
	principal, tenant, ok := h.authorizeRecordRead(
		writer, request, kind, id, controlplane.PermissionRecordInspect,
	)
	if !ok {
		return
	}
	visibility, revealDiagnostics, err := parseRecordVisibility(request.URL.Query())
	if err != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	permissions := sensitiveRecordPermissions(visibility, revealDiagnostics)
	for _, permission := range permissions {
		if h.workflowLimiter != nil && !h.workflowLimiter.Allow(
			request.Context(), workflowRateLimitKey(principal.Subject(), string(permission)),
		) {
			writer.Header().Set("Retry-After", "1")
			writeProblem(writer, http.StatusTooManyRequests, "rate_limited")
			return
		}
	}
	if visibility == queue.PayloadRevealed {
		if err := h.viewer.Authorize(
			request.Context(), tenant, principal.Subject(),
			controlplane.PermissionPayloadView,
			controlplane.Target{Kind: targetKindForRecord(kind), Name: id},
		); err != nil {
			writeCommandError(writer, err)
			return
		}
	}
	if revealDiagnostics {
		if err := h.viewer.Authorize(
			request.Context(), tenant, principal.Subject(),
			controlplane.PermissionDiagnosticsView,
			controlplane.Target{Kind: targetKindForRecord(kind), Name: id},
		); err != nil {
			writeCommandError(writer, err)
			return
		}
	}
	for _, permission := range permissions {
		if h.sensitiveAudit == nil {
			writeProblem(writer, http.StatusServiceUnavailable, "audit_unavailable")
			return
		}
		identifier, err := h.newCommandID()
		if err != nil {
			writeProblem(writer, http.StatusServiceUnavailable, "audit_unavailable")
			return
		}
		if err := h.sensitiveAudit.AuditSensitiveAccess(
			request.Context(),
			controlplane.SensitiveAccess{
				CommandID: identifier, TenantID: tenant, Actor: principal.Subject(),
				Permission: permission,
				Target:     controlplane.Target{Kind: targetKindForRecord(kind), Name: id},
				OccurredAt: h.now().UTC(),
			},
		); err != nil {
			writeProblem(writer, http.StatusServiceUnavailable, "audit_unavailable")
			return
		}
	}
	record, err := h.records.Inspect(request.Context(), tenant, queue.InspectRequest{
		Kind: kind, ID: id, Visibility: visibility,
	})
	if err != nil {
		writeProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	if len(permissions) > 0 {
		writer.Header().Set("Content-Disposition", sensitiveContentDisposition)
	}
	writeJSON(writer, http.StatusOK, recordModel(record, visibility, revealDiagnostics))
}

func sensitiveRecordPermissions(
	visibility queue.PayloadVisibility,
	revealDiagnostics bool,
) []controlplane.Permission {
	permissions := make([]controlplane.Permission, 0, 2)
	if visibility == queue.PayloadRevealed {
		permissions = append(permissions, controlplane.PermissionPayloadView)
	}
	if revealDiagnostics {
		permissions = append(permissions, controlplane.PermissionDiagnosticsView)
	}

	return permissions
}

func (h *handler) authorizeRecordRead(
	writer http.ResponseWriter,
	request *http.Request,
	kind queue.RecordKind,
	name string,
	permission controlplane.Permission,
) (authentication.Principal, string, bool) {
	principal, ok := authentication.PrincipalFromContext(request.Context())
	if !ok || principal.IsAnonymous() {
		writeProblem(writer, http.StatusUnauthorized, "unauthenticated")
		return authentication.Principal{}, "", false
	}
	tenant := request.PathValue("tenant")
	if !validIdentity(tenant) || !validIdentity(name) {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return authentication.Principal{}, "", false
	}
	if err := h.viewer.Authorize(
		request.Context(), tenant, principal.Subject(), permission,
		controlplane.Target{Kind: targetKindForRecord(kind), Name: name},
	); err != nil {
		writeCommandError(writer, err)
		return authentication.Principal{}, "", false
	}

	return principal, tenant, true
}

func parseRecordQuery(values url.Values) (queue.PageRequest, error) {
	request := queue.PageRequest{
		Limit: defaultRecordPageSize, Sort: queue.SortOccurredAt, Direction: queue.SortDescending,
	}
	allowed := map[string]bool{
		"cursor": true, "limit": true, "search": true, "sort": true, "direction": true,
	}
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return queue.PageRequest{}, ErrInvalidConfiguration
		}
	}
	if raw, exists := values["cursor"]; exists {
		if raw[0] == "" {
			return queue.PageRequest{}, ErrInvalidConfiguration
		}
		request.Cursor = raw[0]
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return queue.PageRequest{}, ErrInvalidConfiguration
		}
		request.Limit = uint32(limit)
	} else if _, exists := values["limit"]; exists {
		return queue.PageRequest{}, ErrInvalidConfiguration
	}
	request.Search = values.Get("search")
	if raw := values.Get("sort"); raw != "" {
		request.Sort = queue.SortField(raw)
	} else if _, exists := values["sort"]; exists {
		return queue.PageRequest{}, ErrInvalidConfiguration
	}
	if raw := values.Get("direction"); raw != "" {
		request.Direction = queue.SortDirection(raw)
	} else if _, exists := values["direction"]; exists {
		return queue.PageRequest{}, ErrInvalidConfiguration
	}
	if err := request.Validate(); err != nil {
		return queue.PageRequest{}, ErrInvalidConfiguration
	}

	return request, nil
}

func parseRecordVisibility(values url.Values) (queue.PayloadVisibility, bool, error) {
	for key, entries := range values {
		if (key != "payload" && key != "diagnostics") || len(entries) != 1 {
			return queue.PayloadHidden, false, ErrInvalidConfiguration
		}
	}
	visibility := queue.PayloadHidden
	if entries, exists := values["payload"]; exists {
		switch entries[0] {
		case "hidden":
		case string(queue.PayloadRedacted):
			visibility = queue.PayloadRedacted
		case string(queue.PayloadRevealed):
			visibility = queue.PayloadRevealed
		default:
			return queue.PayloadHidden, false, ErrInvalidConfiguration
		}
	}
	revealDiagnostics := false
	if entries, exists := values["diagnostics"]; exists {
		switch entries[0] {
		case "hidden":
		case "revealed":
			revealDiagnostics = true
		default:
			return queue.PayloadHidden, false, ErrInvalidConfiguration
		}
	}

	return visibility, revealDiagnostics, nil
}

func targetKindForRecord(kind queue.RecordKind) controlplane.TargetKind {
	if kind == queue.RecordFailure {
		return controlplane.TargetFailure
	}

	return controlplane.TargetDeadLetter
}

func collectionName(kind queue.RecordKind) string {
	if kind == queue.RecordFailure {
		return "failures"
	}

	return "dead_letters"
}

func recordPage(page queue.RecordPage) RecordPage {
	result := RecordPage{Records: make([]Record, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, item := range page.Items {
		result.Records = append(
			result.Records,
			recordModel(item, queue.PayloadHidden, false),
		)
	}

	return result
}

func recordModel(
	record queue.JobRecord,
	visibility queue.PayloadVisibility,
	revealDiagnostics bool,
) Record {
	model := Record{
		Kind: record.Kind, ID: record.ID, Backend: record.Backend, Queue: record.Queue,
		OccurredAt: record.OccurredAt.UTC(),
		Attempts:   record.Attempts, FailureCode: record.FailureCode,
		Payload:              hiddenRecordPayloadModel(record.Payload),
		EnvelopeVersion:      record.EnvelopeVersion,
		PayloadSchemaVersion: record.PayloadSchemaVersion,
		OriginalID:           record.OriginalID,
		Topic:                record.Topic,
		Stream:               record.Stream,
		RoutingKey:           record.RoutingKey,
		ConsumerGroup:        record.ConsumerGroup,
		SourceRecordID:       record.SourceRecordID,
		EnqueuedAt:           recordTimeModel(record.EnqueuedAt),
		FirstDeliveryAt:      recordTimeModel(record.FirstDeliveryAt),
		LastDeliveryAt:       recordTimeModel(record.LastDeliveryAt),
		DeadLetteredAt:       recordTimeModel(record.DeadLetteredAt),
		RetryPolicy:          record.RetryPolicy,
		Classification:       record.Classification,
		FailureSummary:       record.FailureSummary,
		Diagnostics:          hiddenRecordPayloadModel(record.Diagnostics),
		HandlerType:          record.HandlerType,
		JobType:              record.JobType,
		Tags:                 cloneRecordTags(record.Tags),
		TraceID:              record.TraceID,
		TenantID:             record.TenantID,
		ProducerVersion:      record.ProducerVersion,
		WorkerVersion:        record.WorkerVersion,
		OriginalDeadLetterID: record.OriginalDeadLetterID,
		PriorDeadLetterID:    record.PriorDeadLetterID,
		ReplayGeneration:     record.ReplayGeneration,
		RetentionDeadline:    recordTimeModel(record.RetentionDeadline),
	}
	if visibility == queue.PayloadRevealed ||
		(visibility == queue.PayloadRedacted && record.Payload.Visibility == queue.PayloadRedacted) {
		model.Payload = recordPayloadModel(record.Payload)
	}
	if revealDiagnostics {
		model.Diagnostics = recordPayloadModel(record.Diagnostics)
	}

	return model
}

func hiddenRecordPayloadModel(payload queue.Payload) RecordPayload {
	return RecordPayload{Visibility: queue.PayloadHidden, Size: payload.Size}
}

func recordPayloadModel(payload queue.Payload) RecordPayload {
	return RecordPayload{
		Visibility: payload.Visibility, ContentType: payload.ContentType,
		Size: payload.Size, Data: append([]byte(nil), payload.Data...),
	}
}

func recordTimeModel(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()

	return &utc
}

func cloneRecordTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(tags))
	for key, value := range tags {
		cloned[key] = value
	}

	return cloned
}
