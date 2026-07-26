package managementhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
)

const wirePayloadHidden = "hidden"

type recordPayload struct {
	Visibility  string `json:"visibility"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size"`
	Data        []byte `json:"data,omitempty"`
}

type jobRecord struct {
	Kind                 management.RecordKind     `json:"kind"`
	ID                   string                    `json:"id"`
	Backend              string                    `json:"backend"`
	Queue                string                    `json:"queue"`
	OccurredAt           time.Time                 `json:"occurred_at"`
	Attempts             uint32                    `json:"attempts"`
	FailureCode          string                    `json:"failure_code"`
	Payload              recordPayload             `json:"payload"`
	EnvelopeVersion      uint16                    `json:"envelope_version,omitempty"`
	PayloadSchemaVersion string                    `json:"payload_schema_version,omitempty"`
	OriginalID           string                    `json:"original_id,omitempty"`
	Topic                string                    `json:"topic,omitempty"`
	Stream               string                    `json:"stream,omitempty"`
	RoutingKey           string                    `json:"routing_key,omitempty"`
	ConsumerGroup        string                    `json:"consumer_group,omitempty"`
	SourceRecordID       string                    `json:"source_record_id,omitempty"`
	EnqueuedAt           *time.Time                `json:"enqueued_at,omitempty"`
	FirstDeliveryAt      *time.Time                `json:"first_delivery_at,omitempty"`
	LastDeliveryAt       *time.Time                `json:"last_delivery_at,omitempty"`
	DeadLetteredAt       *time.Time                `json:"dead_lettered_at,omitempty"`
	RetryPolicy          string                    `json:"retry_policy,omitempty"`
	Classification       management.Classification `json:"classification,omitempty"`
	FailureSummary       string                    `json:"failure_summary,omitempty"`
	Diagnostics          *recordPayload            `json:"diagnostics,omitempty"`
	HandlerType          string                    `json:"handler_type,omitempty"`
	JobType              string                    `json:"job_type,omitempty"`
	Tags                 map[string]string         `json:"tags,omitempty"`
	TraceID              string                    `json:"trace_id,omitempty"`
	TenantID             string                    `json:"tenant_id,omitempty"`
	ProducerVersion      string                    `json:"producer_version,omitempty"`
	WorkerVersion        string                    `json:"worker_version,omitempty"`
	OriginalDeadLetterID string                    `json:"original_dead_letter_id,omitempty"`
	PriorDeadLetterID    string                    `json:"prior_dead_letter_id,omitempty"`
	ReplayGeneration     uint32                    `json:"replay_generation,omitempty"`
	RetentionDeadline    *time.Time                `json:"retention_deadline,omitempty"`
}

type recordPage struct {
	Items      []jobRecord `json:"items"`
	NextCursor string      `json:"next_cursor,omitempty"`
}

// ListFailures returns one remote bounded failure page.
func (c *Client) ListFailures(
	ctx context.Context,
	request management.PageRequest,
) (management.RecordPage, error) {
	return c.listRecords(ctx, "failures", request)
}

// ListDeadLetters returns one remote bounded dead-letter page.
func (c *Client) ListDeadLetters(
	ctx context.Context,
	request management.PageRequest,
) (management.RecordPage, error) {
	return c.listRecords(ctx, "dead-letters", request)
}

func (c *Client) listRecords(
	ctx context.Context,
	collection string,
	request management.PageRequest,
) (management.RecordPage, error) {
	if request.Validate() != nil {
		return management.RecordPage{}, ErrInvalidRequest
	}
	endpoint := c.baseURL.JoinPath("v1", "records", collection)
	values := endpoint.Query()
	values.Set("limit", strconv.FormatUint(uint64(request.Limit), 10))
	values.Set("sort", string(request.Sort))
	values.Set("direction", string(request.Direction))
	if request.Cursor != "" {
		values.Set("cursor", request.Cursor)
	}
	if request.Search != "" {
		values.Set("search", request.Search)
	}
	endpoint.RawQuery = values.Encode()
	var wirePage recordPage
	if err := c.getRecords(ctx, endpoint, &wirePage); err != nil {
		return management.RecordPage{}, err
	}
	page := managementRecordPage(wirePage)
	if page.Validate() != nil || !hiddenRecordPage(page) {
		return management.RecordPage{}, ErrInvalidResponse
	}

	return page, nil
}

// Inspect returns one remote record at explicit payload visibility.
func (c *Client) Inspect(
	ctx context.Context,
	request management.InspectRequest,
) (management.JobRecord, error) {
	if request.Validate() != nil {
		return management.JobRecord{}, ErrInvalidRequest
	}
	collection := "failures"
	if request.Kind == management.RecordDeadLetter {
		collection = "dead-letters"
	}
	endpoint := c.baseURL.JoinPath("v1", "records", collection, request.ID)
	values := endpoint.Query()
	values.Set("visibility", wireVisibility(request.Visibility))
	endpoint.RawQuery = values.Encode()
	var wireRecord jobRecord
	if err := c.getRecords(ctx, endpoint, &wireRecord); err != nil {
		return management.JobRecord{}, err
	}
	record := managementRecord(wireRecord)
	if record.Validate() != nil || record.Kind != request.Kind ||
		!allowedVisibility(request.Visibility, record.Payload.Visibility) ||
		!allowedVisibility(request.Visibility, record.Diagnostics.Visibility) {
		return management.JobRecord{}, ErrInvalidResponse
	}

	return record, nil
}

func (c *Client) getRecords(ctx context.Context, endpoint *url.URL, output any) error {
	// #nosec G704 -- the configured management base URL and bounded contract
	// values are the only endpoint inputs.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return ErrInvalidRequest
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	// #nosec G704 -- management endpoints are explicit operator configuration.
	response, err := c.httpClient.Do(request)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrRemoteFailure
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, c.maxResponseBytes+1))
	if err != nil {
		return ErrRemoteFailure
	}
	if int64(len(data)) > c.maxResponseBytes {
		return ErrResponseTooLarge
	}
	if response.StatusCode != http.StatusOK {
		var wireProblem problem
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&wireProblem) != nil || ensureEOF(decoder) != nil {
			return ErrRemoteFailure
		}
		if target := managementProblem(wireProblem.Code); target != nil {
			return errors.Join(ErrRemoteFailure, target)
		}

		return ErrRemoteFailure
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(output) != nil || ensureEOF(decoder) != nil {
		return ErrInvalidResponse
	}

	return nil
}

func (h *handler) listFailures(writer http.ResponseWriter, request *http.Request) {
	h.listRecords(writer, request, h.records.ListFailures)
}

func (h *handler) listDeadLetters(writer http.ResponseWriter, request *http.Request) {
	h.listRecords(writer, request, h.records.ListDeadLetters)
}

func (h *handler) listRecords(
	writer http.ResponseWriter,
	request *http.Request,
	read func(context.Context, management.PageRequest) (management.RecordPage, error),
) {
	pageRequest, ok := recordPageRequest(request.URL.Query())
	if !ok {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	page, err := read(request.Context(), pageRequest)
	if err != nil {
		writeRecordProblem(writer, err)
		return
	}
	if page.Validate() != nil || !hiddenRecordPage(page) {
		writeProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(writer, transportRecordPage(page))
}

func (h *handler) inspectFailure(writer http.ResponseWriter, request *http.Request) {
	h.inspectRecord(writer, request, management.RecordFailure)
}

func (h *handler) inspectDeadLetter(writer http.ResponseWriter, request *http.Request) {
	h.inspectRecord(writer, request, management.RecordDeadLetter)
}

func (h *handler) inspectRecord(
	writer http.ResponseWriter,
	request *http.Request,
	kind management.RecordKind,
) {
	visibility, ok := inspectVisibility(request.URL.Query())
	inspect := management.InspectRequest{
		Kind: kind, ID: request.PathValue("id"), Visibility: visibility,
	}
	if !ok || inspect.Validate() != nil {
		writeProblem(writer, http.StatusBadRequest, "invalid_request")
		return
	}
	record, err := h.records.Inspect(request.Context(), inspect)
	if err != nil {
		writeRecordProblem(writer, err)
		return
	}
	if record.Validate() != nil || record.Kind != kind ||
		!allowedVisibility(visibility, record.Payload.Visibility) ||
		!allowedVisibility(visibility, record.Diagnostics.Visibility) {
		writeProblem(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(writer, transportRecord(record))
}

func writeRecordProblem(writer http.ResponseWriter, err error) {
	status, code := recordProblem(err)
	writeProblem(writer, status, code)
}

func recordProblem(err error) (int, string) {
	switch {
	case errors.Is(err, management.ErrRecordNotFound):
		return http.StatusNotFound, "record_not_found"
	case errors.Is(err, management.ErrUnsupportedCapability):
		return http.StatusNotImplemented, "unsupported_capability"
	case errors.Is(err, management.ErrManagementUnavailable):
		return http.StatusServiceUnavailable, "management_unavailable"
	case errors.Is(err, management.ErrMalformedCursor):
		return http.StatusBadRequest, "malformed_cursor"
	case errors.Is(err, management.ErrInvalidFilter):
		return http.StatusBadRequest, "invalid_filter"
	case errors.Is(err, management.ErrStaleRecord):
		return http.StatusConflict, "stale_record"
	case errors.Is(err, management.ErrMutationConflict):
		return http.StatusConflict, "mutation_conflict"
	case errors.Is(err, management.ErrPartialMutation):
		return http.StatusConflict, "partial_mutation"
	case errors.Is(err, management.ErrUnknownMutation):
		return http.StatusServiceUnavailable, "unknown_mutation"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func managementProblem(code string) error {
	switch code {
	case "record_not_found":
		return management.ErrRecordNotFound
	case "unsupported_capability":
		return management.ErrUnsupportedCapability
	case "management_unavailable":
		return management.ErrManagementUnavailable
	case "malformed_cursor":
		return management.ErrMalformedCursor
	case "invalid_filter":
		return management.ErrInvalidFilter
	case "stale_record":
		return management.ErrStaleRecord
	case "mutation_conflict":
		return management.ErrMutationConflict
	case "partial_mutation":
		return management.ErrPartialMutation
	case "unknown_mutation":
		return management.ErrUnknownMutation
	default:
		return nil
	}
}

func recordPageRequest(values url.Values) (management.PageRequest, bool) {
	allowed := map[string]bool{
		"cursor": true, "limit": true, "search": true,
		"sort": true, "direction": true,
	}
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 || entries[0] == "" {
			return management.PageRequest{}, false
		}
	}
	limit, err := strconv.ParseUint(values.Get("limit"), 10, 32)
	if err != nil {
		return management.PageRequest{}, false
	}
	request := management.PageRequest{
		Cursor: values.Get("cursor"), Limit: uint32(limit), Search: values.Get("search"),
		Sort:      management.SortField(values.Get("sort")),
		Direction: management.SortDirection(values.Get("direction")),
	}

	return request, request.Validate() == nil
}

func inspectVisibility(values url.Values) (management.PayloadVisibility, bool) {
	entries, exists := values["visibility"]
	if len(values) != 1 || !exists || len(entries) != 1 {
		return "", false
	}
	switch entries[0] {
	case wirePayloadHidden:
		return management.PayloadHidden, true
	case string(management.PayloadRedacted):
		return management.PayloadRedacted, true
	case string(management.PayloadRevealed):
		return management.PayloadRevealed, true
	default:
		return "", false
	}
}

func transportRecordPage(page management.RecordPage) recordPage {
	result := recordPage{Items: make([]jobRecord, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, item := range page.Items {
		result.Items = append(result.Items, transportRecord(item))
	}
	return result
}

func managementRecordPage(page recordPage) management.RecordPage {
	result := management.RecordPage{
		Items: make([]management.JobRecord, 0, len(page.Items)), NextCursor: page.NextCursor,
	}
	for _, item := range page.Items {
		result.Items = append(result.Items, managementRecord(item))
	}
	return result
}

func transportRecord(record management.JobRecord) jobRecord {
	return jobRecord{
		Kind: record.Kind, ID: record.ID, Backend: record.Backend, Queue: record.Queue,
		OccurredAt: record.OccurredAt, Attempts: record.Attempts,
		FailureCode: record.FailureCode,
		Payload: recordPayload{
			Visibility:  wireVisibility(record.Payload.Visibility),
			ContentType: record.Payload.ContentType, Size: record.Payload.Size,
			Data: append([]byte(nil), record.Payload.Data...),
		},
		EnvelopeVersion: record.EnvelopeVersion, PayloadSchemaVersion: record.PayloadSchemaVersion,
		OriginalID: record.OriginalID, Topic: record.Topic, Stream: record.Stream,
		RoutingKey: record.RoutingKey, ConsumerGroup: record.ConsumerGroup,
		SourceRecordID: record.SourceRecordID, EnqueuedAt: cloneTime(record.EnqueuedAt),
		FirstDeliveryAt: cloneTime(record.FirstDeliveryAt), LastDeliveryAt: cloneTime(record.LastDeliveryAt),
		DeadLetteredAt: cloneTime(record.DeadLetteredAt), RetryPolicy: record.RetryPolicy,
		Classification: record.Classification, FailureSummary: record.FailureSummary,
		Diagnostics: transportDiagnostics(record), HandlerType: record.HandlerType,
		JobType: record.JobType, Tags: cloneTags(record.Tags), TraceID: record.TraceID,
		TenantID: record.TenantID, ProducerVersion: record.ProducerVersion,
		WorkerVersion: record.WorkerVersion, OriginalDeadLetterID: record.OriginalDeadLetterID,
		PriorDeadLetterID: record.PriorDeadLetterID, ReplayGeneration: record.ReplayGeneration,
		RetentionDeadline: cloneTime(record.RetentionDeadline),
	}
}

func managementRecord(record jobRecord) management.JobRecord {
	return management.JobRecord{
		Kind: record.Kind, ID: record.ID, Backend: record.Backend, Queue: record.Queue,
		OccurredAt: record.OccurredAt, Attempts: record.Attempts,
		FailureCode: record.FailureCode,
		Payload: management.Payload{
			Visibility:  managementVisibility(record.Payload.Visibility),
			ContentType: record.Payload.ContentType, Size: record.Payload.Size,
			Data: append([]byte(nil), record.Payload.Data...),
		},
		EnvelopeVersion: record.EnvelopeVersion, PayloadSchemaVersion: record.PayloadSchemaVersion,
		OriginalID: record.OriginalID, Topic: record.Topic, Stream: record.Stream,
		RoutingKey: record.RoutingKey, ConsumerGroup: record.ConsumerGroup,
		SourceRecordID: record.SourceRecordID, EnqueuedAt: cloneTime(record.EnqueuedAt),
		FirstDeliveryAt: cloneTime(record.FirstDeliveryAt), LastDeliveryAt: cloneTime(record.LastDeliveryAt),
		DeadLetteredAt: cloneTime(record.DeadLetteredAt), RetryPolicy: record.RetryPolicy,
		Classification: record.Classification, FailureSummary: record.FailureSummary,
		Diagnostics: managementPayload(record.Diagnostics), HandlerType: record.HandlerType,
		JobType: record.JobType, Tags: cloneTags(record.Tags), TraceID: record.TraceID,
		TenantID: record.TenantID, ProducerVersion: record.ProducerVersion,
		WorkerVersion: record.WorkerVersion, OriginalDeadLetterID: record.OriginalDeadLetterID,
		PriorDeadLetterID: record.PriorDeadLetterID, ReplayGeneration: record.ReplayGeneration,
		RetentionDeadline: cloneTime(record.RetentionDeadline),
	}
}

func transportPayload(payload management.Payload) recordPayload {
	return recordPayload{
		Visibility: wireVisibility(payload.Visibility), ContentType: payload.ContentType,
		Size: payload.Size, Data: append([]byte(nil), payload.Data...),
	}
}

func transportDiagnostics(record management.JobRecord) *recordPayload {
	if record.EnvelopeVersion == 0 && record.Diagnostics.Visibility == management.PayloadHidden &&
		record.Diagnostics.ContentType == "" && record.Diagnostics.Size == 0 &&
		len(record.Diagnostics.Data) == 0 {
		return nil
	}
	payload := transportPayload(record.Diagnostics)

	return &payload
}

func managementPayload(payload *recordPayload) management.Payload {
	if payload == nil {
		return management.Payload{}
	}

	return management.Payload{
		Visibility: managementVisibility(payload.Visibility), ContentType: payload.ContentType,
		Size: payload.Size, Data: append([]byte(nil), payload.Data...),
	}
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value

	return &cloned
}

func cloneTags(tags map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	cloned := make(map[string]string, len(tags))
	for key, value := range tags {
		cloned[key] = value
	}

	return cloned
}

func wireVisibility(visibility management.PayloadVisibility) string {
	if visibility == management.PayloadHidden {
		return wirePayloadHidden
	}
	return string(visibility)
}

func managementVisibility(visibility string) management.PayloadVisibility {
	if visibility == wirePayloadHidden {
		return management.PayloadHidden
	}
	return management.PayloadVisibility(visibility)
}

func hiddenRecordPage(page management.RecordPage) bool {
	for _, record := range page.Items {
		if record.Payload.Visibility != management.PayloadHidden ||
			record.Diagnostics.Visibility != management.PayloadHidden {
			return false
		}
	}
	return true
}

func allowedVisibility(
	requested management.PayloadVisibility,
	actual management.PayloadVisibility,
) bool {
	switch requested {
	case management.PayloadHidden:
		return actual == management.PayloadHidden
	case management.PayloadRedacted:
		return actual == management.PayloadHidden || actual == management.PayloadRedacted
	case management.PayloadRevealed:
		return true
	default:
		return false
	}
}

var _ management.RecordReader = (*Client)(nil)
