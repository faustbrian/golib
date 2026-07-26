package management

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	// CurrentEnvelopeVersion is the first complete dead-letter record contract.
	CurrentEnvelopeVersion uint16 = 1
	// MaxAdministrativePayloadBytes bounds privileged payload inspection.
	MaxAdministrativePayloadBytes = 1 << 20
	// MaxFailureSummaryBytes bounds deliberately redacted operator diagnostics.
	MaxFailureSummaryBytes = 1_024
	// MaxRecordTags bounds user-supplied dead-letter record dimensions.
	MaxRecordTags = 32
	// MaxPageSize bounds one failure or dead-letter listing response.
	MaxPageSize uint32 = 200
	// MaxCursorBytes bounds opaque pagination state accepted from a client.
	MaxCursorBytes = 1_024
	// MaxSearchBytes bounds backend-neutral administrative search input.
	MaxSearchBytes = 256
)

// RecordKind distinguishes failed jobs from terminal dead letters.
type RecordKind string

const (
	RecordFailure    RecordKind = "failure"
	RecordDeadLetter RecordKind = "dead_letter"
)

// PayloadVisibility describes the disclosure applied by an authorized
// inspection. Its zero value is hidden so payloads are never exposed by
// default.
type PayloadVisibility string

const (
	PayloadHidden   PayloadVisibility = ""
	PayloadRedacted PayloadVisibility = "redacted"
	PayloadRevealed PayloadVisibility = "revealed"
)

// Payload is a bounded administrative representation. Data must remain empty
// unless privileged access explicitly requested revealed content.
type Payload struct {
	Visibility  PayloadVisibility
	ContentType string
	Size        int64
	Data        []byte
}

// JobRecord is the backend-neutral failure or dead-letter representation.
type JobRecord struct {
	Kind        RecordKind
	ID          string
	Backend     string
	Queue       string
	OccurredAt  time.Time
	Attempts    uint32
	FailureCode string
	Payload     Payload

	EnvelopeVersion      uint16
	PayloadSchemaVersion string
	OriginalID           string
	Topic                string
	Stream               string
	RoutingKey           string
	ConsumerGroup        string
	SourceRecordID       string
	EnqueuedAt           *time.Time
	FirstDeliveryAt      *time.Time
	LastDeliveryAt       *time.Time
	DeadLetteredAt       *time.Time
	RetryPolicy          string
	Classification       Classification
	FailureSummary       string
	Diagnostics          Payload
	HandlerType          string
	JobType              string
	Tags                 map[string]string
	TraceID              string
	TenantID             string
	ProducerVersion      string
	WorkerVersion        string
	OriginalDeadLetterID string
	PriorDeadLetterID    string
	ReplayGeneration     uint32
	RetentionDeadline    *time.Time
}

// Validate rejects malformed metadata and unsafe payload disclosure.
func (r JobRecord) Validate() error {
	if !r.Kind.valid() {
		return invalid("kind", "is unsupported")
	}
	if invalidIdentity(r.ID) {
		return invalid("id", "is required and must be bounded")
	}
	if invalidIdentity(r.Backend) {
		return invalid("backend", "is required and must be bounded")
	}
	if invalidIdentity(r.Queue) {
		return invalid("queue", "is required and must be bounded")
	}
	if r.OccurredAt.IsZero() {
		return invalid("occurred_at", "is required")
	}
	if r.Attempts == 0 {
		return invalid("attempts", "must be positive")
	}
	if invalidIdentity(r.FailureCode) {
		return invalid("failure_code", "is required and must be bounded")
	}

	if err := r.Payload.validate(); err != nil {
		return err
	}
	if r.EnvelopeVersion == 0 {
		if r.hasV1Metadata() {
			return invalid("envelope_version", "is required for v1 metadata")
		}
		return nil
	}
	if r.EnvelopeVersion != CurrentEnvelopeVersion {
		return invalid("envelope_version", "is unsupported")
	}
	if !r.Classification.valid() {
		return invalid("classification", "is unsupported")
	}
	if r.Kind == RecordDeadLetter && (r.DeadLetteredAt == nil || r.DeadLetteredAt.IsZero()) {
		return invalid("dead_lettered_at", "is required")
	}
	if len(r.FailureSummary) > MaxFailureSummaryBytes {
		return invalid("failure_summary", "exceeds the summary limit")
	}
	if err := r.Diagnostics.validate(); err != nil {
		var validationError *ValidationError
		_ = errors.As(err, &validationError)

		return invalid("diagnostics."+validationError.Field, validationError.Problem)
	}
	if len(r.Tags) > MaxRecordTags {
		return invalid("tags", "exceeds the tag limit")
	}
	for key, value := range r.Tags {
		if invalidIdentity(key) || invalidIdentity(value) {
			return invalid("tags", "keys and values must be bounded")
		}
	}
	for field, value := range map[string]string{
		"payload_schema_version":  r.PayloadSchemaVersion,
		"original_id":             r.OriginalID,
		"topic":                   r.Topic,
		"stream":                  r.Stream,
		"routing_key":             r.RoutingKey,
		"consumer_group":          r.ConsumerGroup,
		"source_record_id":        r.SourceRecordID,
		"retry_policy":            r.RetryPolicy,
		"handler_type":            r.HandlerType,
		"job_type":                r.JobType,
		"trace_id":                r.TraceID,
		"tenant_id":               r.TenantID,
		"producer_version":        r.ProducerVersion,
		"worker_version":          r.WorkerVersion,
		"original_dead_letter_id": r.OriginalDeadLetterID,
		"prior_dead_letter_id":    r.PriorDeadLetterID,
	} {
		if value != "" && invalidIdentity(value) {
			return invalid(field, "must be bounded when known")
		}
	}
	if field := invalidRecordChronology(r); field != "" {
		return invalid(field, "is inconsistent with record chronology")
	}
	if r.ReplayGeneration > 0 && r.OriginalDeadLetterID == "" {
		return invalid("original_dead_letter_id", "is required for replay lineage")
	}
	if r.ReplayGeneration > 0 && r.PriorDeadLetterID == "" {
		return invalid("prior_dead_letter_id", "is required for replay lineage")
	}

	return nil
}

func (r JobRecord) hasV1Metadata() bool {
	return r.PayloadSchemaVersion != "" || r.OriginalID != "" || r.Topic != "" ||
		r.Stream != "" || r.RoutingKey != "" || r.ConsumerGroup != "" ||
		r.SourceRecordID != "" || r.EnqueuedAt != nil || r.FirstDeliveryAt != nil ||
		r.LastDeliveryAt != nil || r.DeadLetteredAt != nil || r.RetryPolicy != "" ||
		r.Classification != "" || r.FailureSummary != "" ||
		r.Diagnostics.Visibility != PayloadHidden || r.Diagnostics.ContentType != "" ||
		r.Diagnostics.Size != 0 || len(r.Diagnostics.Data) != 0 || r.HandlerType != "" ||
		r.JobType != "" || len(r.Tags) != 0 || r.TraceID != "" || r.TenantID != "" ||
		r.ProducerVersion != "" || r.WorkerVersion != "" ||
		r.OriginalDeadLetterID != "" || r.PriorDeadLetterID != "" ||
		r.ReplayGeneration != 0 || r.RetentionDeadline != nil
}

func invalidRecordChronology(record JobRecord) string {
	knownTimes := map[string]*time.Time{
		"enqueued_at":        record.EnqueuedAt,
		"first_delivery_at":  record.FirstDeliveryAt,
		"last_delivery_at":   record.LastDeliveryAt,
		"dead_lettered_at":   record.DeadLetteredAt,
		"retention_deadline": record.RetentionDeadline,
	}
	for field, value := range knownTimes {
		if value != nil && value.IsZero() {
			return field
		}
	}
	if record.EnqueuedAt != nil && record.FirstDeliveryAt != nil &&
		record.FirstDeliveryAt.Before(*record.EnqueuedAt) {
		return "first_delivery_at"
	}
	if record.FirstDeliveryAt != nil && record.LastDeliveryAt != nil &&
		record.LastDeliveryAt.Before(*record.FirstDeliveryAt) {
		return "last_delivery_at"
	}
	if record.LastDeliveryAt != nil && record.DeadLetteredAt != nil &&
		record.DeadLetteredAt.Before(*record.LastDeliveryAt) {
		return "dead_lettered_at"
	}
	if record.DeadLetteredAt != nil && record.RetentionDeadline != nil &&
		!record.RetentionDeadline.After(*record.DeadLetteredAt) {
		return "retention_deadline"
	}

	return ""
}

func (r RecordKind) valid() bool {
	switch r {
	case RecordFailure, RecordDeadLetter:
		return true
	default:
		return false
	}
}

func (p Payload) validate() error {
	if !p.Visibility.valid() {
		return invalid("payload.visibility", "is unsupported")
	}
	if p.Size < 0 {
		return invalid("payload.size", "cannot be negative")
	}
	if p.ContentType != "" && invalidIdentity(p.ContentType) {
		return invalid("payload.content_type", "must be bounded")
	}
	if p.Visibility != PayloadRevealed && len(p.Data) != 0 {
		return invalid("payload.data", "must be hidden")
	}
	if len(p.Data) > MaxAdministrativePayloadBytes {
		return invalid("payload.data", "exceeds the inspection limit")
	}
	if int64(len(p.Data)) > p.Size {
		return invalid("payload.size", "cannot be smaller than revealed data")
	}

	return nil
}

func (v PayloadVisibility) valid() bool {
	switch v {
	case PayloadHidden, PayloadRedacted, PayloadRevealed:
		return true
	default:
		return false
	}
}

// SortField identifies bounded record-list ordering.
type SortField string

const (
	SortOccurredAt SortField = "occurred_at"
	SortQueue      SortField = "queue"
	SortAttempts   SortField = "attempts"
)

// SortDirection identifies ascending or descending ordering.
type SortDirection string

const (
	SortAscending  SortDirection = "asc"
	SortDescending SortDirection = "desc"
)

// PageRequest defines bounded cursor pagination, sorting, and search.
type PageRequest struct {
	Cursor    string
	Limit     uint32
	Search    string
	Sort      SortField
	Direction SortDirection
}

// Validate rejects unbounded or unsupported record-list requests.
func (r PageRequest) Validate() error {
	if r.Limit == 0 || r.Limit > MaxPageSize {
		return invalid("limit", "must be within the page limit")
	}
	if len(r.Cursor) > MaxCursorBytes {
		return invalid("cursor", "exceeds the cursor limit")
	}
	if len(r.Search) > MaxSearchBytes {
		return invalid("search", "exceeds the search limit")
	}
	if !r.Sort.valid() {
		return invalid("sort", "is unsupported")
	}
	if !r.Direction.valid() {
		return invalid("direction", "is unsupported")
	}

	return nil
}

func (s SortField) valid() bool {
	switch s {
	case SortOccurredAt, SortQueue, SortAttempts:
		return true
	default:
		return false
	}
}

func (d SortDirection) valid() bool {
	switch d {
	case SortAscending, SortDescending:
		return true
	default:
		return false
	}
}

// RecordPage is one bounded page of failure or dead-letter metadata.
type RecordPage struct {
	Items      []JobRecord
	NextCursor string
}

// Validate rejects oversized cursors, pages, and malformed adapter records.
func (p RecordPage) Validate() error {
	if len(p.Items) > int(MaxPageSize) {
		return invalid("items", "exceeds the page limit")
	}
	for index, item := range p.Items {
		if err := item.Validate(); err != nil {
			var validationError *ValidationError
			_ = errors.As(err, &validationError)

			return invalid(
				fmt.Sprintf("items[%d].%s", index, validationError.Field),
				validationError.Problem,
			)
		}
	}
	if len(p.NextCursor) > MaxCursorBytes {
		return invalid("next_cursor", "exceeds the cursor limit")
	}

	return nil
}

// InspectRequest asks for one record at an explicit payload visibility.
type InspectRequest struct {
	Kind       RecordKind
	ID         string
	Visibility PayloadVisibility
}

// Validate rejects ambiguous or unbounded inspection requests.
func (r InspectRequest) Validate() error {
	if !r.Kind.valid() {
		return invalid("kind", "is unsupported")
	}
	if invalidIdentity(r.ID) {
		return invalid("id", "is required and must be bounded")
	}
	if !r.Visibility.valid() {
		return invalid("visibility", "is unsupported")
	}

	return nil
}

// RecordReader is implemented by queue adapters that expose safe failure
// and dead-letter inspection without leaking native backend clients.
type RecordReader interface {
	ListFailures(context.Context, PageRequest) (RecordPage, error)
	ListDeadLetters(context.Context, PageRequest) (RecordPage, error)
	Inspect(context.Context, InspectRequest) (JobRecord, error)
}
