package management

import (
	"context"
	"fmt"
	"maps"
	"testing"
	"time"
)

func TestJobRecordValidateRequiresBoundedRedactedMetadata(t *testing.T) {
	t.Parallel()

	valid := JobRecord{
		Kind:        RecordFailure,
		ID:          "failure-1",
		Backend:     "valkey-streams",
		Queue:       "critical",
		OccurredAt:  time.Unix(1, 0),
		Attempts:    3,
		FailureCode: "handler_failed",
		Payload:     Payload{Visibility: PayloadHidden, Size: 128},
	}
	tests := map[string]struct {
		mutate func(*JobRecord)
		field  string
	}{
		"kind": {
			mutate: func(record *JobRecord) { record.Kind = RecordKind("retry") },
			field:  "kind",
		},
		"id": {
			mutate: func(record *JobRecord) { record.ID = "" },
			field:  "id",
		},
		"backend": {
			mutate: func(record *JobRecord) { record.Backend = "" },
			field:  "backend",
		},
		"queue": {
			mutate: func(record *JobRecord) { record.Queue = "" },
			field:  "queue",
		},
		"timestamp": {
			mutate: func(record *JobRecord) { record.OccurredAt = time.Time{} },
			field:  "occurred_at",
		},
		"attempts": {
			mutate: func(record *JobRecord) { record.Attempts = 0 },
			field:  "attempts",
		},
		"failure code": {
			mutate: func(record *JobRecord) { record.FailureCode = "" },
			field:  "failure_code",
		},
		"payload visibility": {
			mutate: func(record *JobRecord) { record.Payload.Visibility = PayloadVisibility("public") },
			field:  "payload.visibility",
		},
		"hidden payload bytes": {
			mutate: func(record *JobRecord) { record.Payload.Data = []byte("secret") },
			field:  "payload.data",
		},
		"negative payload size": {
			mutate: func(record *JobRecord) { record.Payload.Size = -1 },
			field:  "payload.size",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			record := valid
			tt.mutate(&record)
			assertValidationField(t, record.Validate(), tt.field)
		})
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestJobRecordValidateControlsPrivilegedPayloadDisclosure(t *testing.T) {
	t.Parallel()

	base := JobRecord{
		Kind:        RecordDeadLetter,
		ID:          "dead-1",
		Backend:     "valkey-streams",
		Queue:       "critical",
		OccurredAt:  time.Unix(1, 0),
		Attempts:    5,
		FailureCode: "attempts_exhausted",
	}

	redacted := base
	redacted.Payload = Payload{Visibility: PayloadRedacted, Size: 128}
	if err := redacted.Validate(); err != nil {
		t.Fatalf("redacted Validate() error = %v", err)
	}

	revealed := base
	revealed.Payload = Payload{
		Visibility:  PayloadRevealed,
		ContentType: "application/json",
		Size:        2,
		Data:        []byte("{}"),
	}
	if err := revealed.Validate(); err != nil {
		t.Fatalf("revealed Validate() error = %v", err)
	}

	oversized := revealed
	oversized.Payload.Data = make([]byte, MaxAdministrativePayloadBytes+1)
	assertValidationField(t, oversized.Validate(), "payload.data")

	invalidContentType := revealed
	invalidContentType.Payload.ContentType = stringOfLength(MaxIdentityBytes + 1)
	assertValidationField(t, invalidContentType.Validate(), "payload.content_type")

	inconsistent := revealed
	inconsistent.Payload.Size = 1
	assertValidationField(t, inconsistent.Validate(), "payload.size")

	redactedWithData := redacted
	redactedWithData.Payload.Data = []byte("secret")
	assertValidationField(t, redactedWithData.Validate(), "payload.data")
}

func TestJobRecordValidateV1DeadLetterEnvelope(t *testing.T) {
	t.Parallel()

	enqueuedAt := time.Unix(1, 0).UTC()
	firstDeliveryAt := time.Unix(2, 0).UTC()
	lastDeliveryAt := time.Unix(3, 0).UTC()
	deadLetteredAt := time.Unix(4, 0).UTC()
	retentionDeadline := time.Unix(5, 0).UTC()
	valid := JobRecord{
		Kind: RecordDeadLetter, ID: "dead-1", Backend: "valkey-streams", Queue: "critical",
		OccurredAt: deadLetteredAt, Attempts: 3, FailureCode: "attempts_exhausted",
		Payload:         Payload{Visibility: PayloadHidden, ContentType: "application/json", Size: 128},
		EnvelopeVersion: CurrentEnvelopeVersion, PayloadSchemaVersion: "order.v2",
		OriginalID: "job-1", Topic: "orders", Stream: "orders", RoutingKey: "orders.created",
		ConsumerGroup: "workers", SourceRecordID: "1720000000000-0",
		EnqueuedAt: &enqueuedAt, FirstDeliveryAt: &firstDeliveryAt,
		LastDeliveryAt: &lastDeliveryAt, DeadLetteredAt: &deadLetteredAt,
		RetryPolicy: "default-v1", Classification: ClassificationPermanent,
		FailureSummary: "handler rejected order", HandlerType: "CreateOrder",
		Tags: map[string]string{"region": "eu"}, TraceID: "trace-1", TenantID: "tenant-1",
		ProducerVersion: "1.2.0", WorkerVersion: "1.3.0",
		OriginalDeadLetterID: "dead-0", PriorDeadLetterID: "dead-0", ReplayGeneration: 1,
		RetentionDeadline: &retentionDeadline,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}

	tests := map[string]struct {
		mutate func(*JobRecord)
		field  string
	}{
		"envelope version": {
			mutate: func(record *JobRecord) { record.EnvelopeVersion = CurrentEnvelopeVersion + 1 },
			field:  "envelope_version",
		},
		"classification": {
			mutate: func(record *JobRecord) { record.Classification = Classification("fatal") },
			field:  "classification",
		},
		"dead-letter timestamp": {
			mutate: func(record *JobRecord) { record.DeadLetteredAt = nil },
			field:  "dead_lettered_at",
		},
		"delivery chronology": {
			mutate: func(record *JobRecord) {
				record.FirstDeliveryAt = &lastDeliveryAt
				record.LastDeliveryAt = &firstDeliveryAt
			},
			field: "last_delivery_at",
		},
		"failure summary": {
			mutate: func(record *JobRecord) { record.FailureSummary = stringOfLength(MaxFailureSummaryBytes + 1) },
			field:  "failure_summary",
		},
		"diagnostics": {
			mutate: func(record *JobRecord) {
				record.Diagnostics = Payload{
					Visibility: PayloadVisibility("visible"),
				}
			},
			field: "diagnostics.payload.visibility",
		},
		"tag count": {
			mutate: func(record *JobRecord) {
				record.Tags = make(map[string]string, MaxRecordTags+1)
				for index := 0; index <= MaxRecordTags; index++ {
					record.Tags[fmt.Sprintf("tag-%d", index)] = "value"
				}
			},
			field: "tags",
		},
		"tag value": {
			mutate: func(record *JobRecord) {
				record.Tags = map[string]string{"region": stringOfLength(MaxIdentityBytes + 1)}
			},
			field: "tags",
		},
		"bounded metadata": {
			mutate: func(record *JobRecord) {
				record.WorkerVersion = stringOfLength(MaxIdentityBytes + 1)
			},
			field: "worker_version",
		},
		"zero known time": {
			mutate: func(record *JobRecord) {
				zero := time.Time{}
				record.EnqueuedAt = &zero
			},
			field: "enqueued_at",
		},
		"first delivery chronology": {
			mutate: func(record *JobRecord) {
				record.EnqueuedAt = &lastDeliveryAt
				record.FirstDeliveryAt = &firstDeliveryAt
			},
			field: "first_delivery_at",
		},
		"dead-letter chronology": {
			mutate: func(record *JobRecord) {
				record.LastDeliveryAt = &deadLetteredAt
				record.DeadLetteredAt = &lastDeliveryAt
			},
			field: "dead_lettered_at",
		},
		"lineage": {
			mutate: func(record *JobRecord) { record.OriginalDeadLetterID = "" },
			field:  "original_dead_letter_id",
		},
		"prior lineage": {
			mutate: func(record *JobRecord) { record.PriorDeadLetterID = "" },
			field:  "prior_dead_letter_id",
		},
		"retention chronology": {
			mutate: func(record *JobRecord) { record.RetentionDeadline = &firstDeliveryAt },
			field:  "retention_deadline",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			record := valid
			record.Tags = maps.Clone(valid.Tags)
			tt.mutate(&record)
			assertValidationField(t, record.Validate(), tt.field)
		})
	}
}

func TestJobRecordValidateLegacyEnvelopeRejectsV1Metadata(t *testing.T) {
	t.Parallel()

	record := JobRecord{
		Kind: RecordFailure, ID: "failure-1", Backend: "valkey-streams", Queue: "critical",
		OccurredAt: time.Unix(1, 0), Attempts: 1, FailureCode: "handler_failed",
		Classification: ClassificationPermanent,
	}
	assertValidationField(t, record.Validate(), "envelope_version")
}

func TestPageRequestValidateBoundsListingAndSearch(t *testing.T) {
	t.Parallel()

	valid := PageRequest{Limit: MaxPageSize, Sort: SortOccurredAt, Direction: SortDescending}
	tests := map[string]struct {
		mutate func(*PageRequest)
		field  string
	}{
		"limit zero": {
			mutate: func(request *PageRequest) { request.Limit = 0 },
			field:  "limit",
		},
		"limit high": {
			mutate: func(request *PageRequest) { request.Limit = MaxPageSize + 1 },
			field:  "limit",
		},
		"cursor": {
			mutate: func(request *PageRequest) { request.Cursor = stringOfLength(MaxCursorBytes + 1) },
			field:  "cursor",
		},
		"search": {
			mutate: func(request *PageRequest) { request.Search = stringOfLength(MaxSearchBytes + 1) },
			field:  "search",
		},
		"sort": {
			mutate: func(request *PageRequest) { request.Sort = SortField("payload") },
			field:  "sort",
		},
		"direction": {
			mutate: func(request *PageRequest) { request.Direction = SortDirection("sideways") },
			field:  "direction",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			request := valid
			tt.mutate(&request)
			assertValidationField(t, request.Validate(), tt.field)
		})
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestRecordReaderContractRemainsBackendNeutral(t *testing.T) {
	t.Parallel()

	var reader RecordReader = recordReaderStub{}
	if _, err := reader.ListFailures(context.Background(), PageRequest{}); err != nil {
		t.Fatalf("ListFailures() error = %v", err)
	}
}

func TestInspectRequestValidateRequiresExplicitBoundedRecord(t *testing.T) {
	t.Parallel()

	valid := InspectRequest{Kind: RecordFailure, ID: "failure-1", Visibility: PayloadHidden}
	tests := map[string]struct {
		mutate func(*InspectRequest)
		field  string
	}{
		"kind": {
			mutate: func(request *InspectRequest) { request.Kind = RecordKind("job") },
			field:  "kind",
		},
		"id": {
			mutate: func(request *InspectRequest) { request.ID = "" },
			field:  "id",
		},
		"visibility": {
			mutate: func(request *InspectRequest) { request.Visibility = PayloadVisibility("public") },
			field:  "visibility",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			request := valid
			tt.mutate(&request)
			assertValidationField(t, request.Validate(), tt.field)
		})
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestRecordPageValidateBoundsAdapterOutput(t *testing.T) {
	t.Parallel()

	record := JobRecord{
		Kind:        RecordFailure,
		ID:          "failure-1",
		Backend:     "valkey-streams",
		Queue:       "critical",
		OccurredAt:  time.Unix(1, 0),
		Attempts:    1,
		FailureCode: "handler_failed",
	}
	valid := RecordPage{Items: []JobRecord{record}, NextCursor: "next"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	tooMany := valid
	tooMany.Items = make([]JobRecord, MaxPageSize+1)
	assertValidationField(t, tooMany.Validate(), "items")

	badItem := valid
	badItem.Items = append([]JobRecord(nil), valid.Items...)
	badItem.Items[0].ID = ""
	assertValidationField(t, badItem.Validate(), "items[0].id")

	badCursor := valid
	badCursor.NextCursor = stringOfLength(MaxCursorBytes + 1)
	assertValidationField(t, badCursor.Validate(), "next_cursor")
}

type recordReaderStub struct{}

func (recordReaderStub) ListFailures(context.Context, PageRequest) (RecordPage, error) {
	return RecordPage{}, nil
}

func (recordReaderStub) ListDeadLetters(context.Context, PageRequest) (RecordPage, error) {
	return RecordPage{}, nil
}

func (recordReaderStub) Inspect(context.Context, InspectRequest) (JobRecord, error) {
	return JobRecord{}, nil
}
