package management

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"testing"
	"time"
)

func TestManagementExactBoundariesRemainAccepted(t *testing.T) {
	t.Parallel()

	worker := validWorkerStatus()
	worker.ID = stringOfLength(MaxIdentityBytes)
	worker.Queues = make([]string, MaxQueuesPerWorker)
	for index := range worker.Queues {
		worker.Queues[index] = "queue"
	}
	worker.Concurrency = MaxWorkerConcurrency
	worker.CurrentJobs = MaxWorkerConcurrency
	worker.Capabilities = make([]Capability, MaxCapabilitiesPerWorker)
	for index := range worker.Capabilities {
		worker.Capabilities[index] = CapabilityPause
	}
	if err := worker.Validate(); err != nil {
		t.Fatalf("WorkerStatus.Validate(exact bounds) error = %v", err)
	}

	metadata := StatusMetadata{
		ID: "worker", Version: "v1", Concurrency: MaxWorkerConcurrency,
		Protocol: ProtocolVersion{Major: 1},
	}
	if err := metadata.Validate(); err != nil {
		t.Fatalf("StatusMetadata.Validate(exact bounds) error = %v", err)
	}

	request := StatusPageRequest{Limit: MaxStatusPageSize, Cursor: stringOfLength(MaxCursorBytes)}
	if err := request.Validate(); err != nil {
		t.Fatalf("StatusPageRequest.Validate(exact bounds) error = %v", err)
	}
	workerPage := WorkerStatusPage{
		Items: make([]WorkerStatus, MaxStatusPageSize), NextCursor: stringOfLength(MaxCursorBytes),
	}
	for index := range workerPage.Items {
		workerPage.Items[index] = validWorkerStatus()
	}
	if err := workerPage.Validate(); err != nil {
		t.Fatalf("WorkerStatusPage.Validate(exact bounds) error = %v", err)
	}
	queuePage := QueueStatusPage{
		Items: make([]QueueStatus, MaxStatusPageSize), NextCursor: stringOfLength(MaxCursorBytes),
	}
	for index := range queuePage.Items {
		queuePage.Items[index] = providerQueue("queue")
	}
	if err := queuePage.Validate(); err != nil {
		t.Fatalf("QueueStatusPage.Validate(exact bounds) error = %v", err)
	}
}

func TestQueueStatusMeasurementSupportControlsValidation(t *testing.T) {
	t.Parallel()

	zero := QueueStatus{Backend: "backend", Queue: "queue", ObservedAt: time.Unix(1, 0)}
	zero.Metrics = QueueMetrics{
		Depth:      Measurement[int64]{Supported: true},
		Lag:        Measurement[int64]{Supported: true},
		Pending:    Measurement[int64]{Supported: true},
		OldestAge:  Measurement[time.Duration]{Supported: true},
		Throughput: Measurement[float64]{Supported: true},
		Runtime:    Measurement[time.Duration]{Supported: true},
	}
	if err := zero.Validate(); err != nil {
		t.Fatalf("QueueStatus.Validate(supported zeroes) error = %v", err)
	}

	unsupported := zero
	unsupported.Metrics = QueueMetrics{
		Depth:      Measurement[int64]{Value: -1},
		Lag:        Measurement[int64]{Value: -1},
		Pending:    Measurement[int64]{Value: -1},
		OldestAge:  Measurement[time.Duration]{Value: -1},
		Throughput: Measurement[float64]{Value: -1},
		Runtime:    Measurement[time.Duration]{Value: -1},
	}
	if err := unsupported.Validate(); err != nil {
		t.Fatalf("QueueStatus.Validate(unsupported measurements) error = %v", err)
	}
}

func TestRecordExactBoundsAndLegacyMetadataDetection(t *testing.T) {
	t.Parallel()

	legacy := JobRecord{
		Kind: RecordFailure, ID: "failure", Backend: "backend", Queue: "queue",
		OccurredAt: time.Unix(1, 0), Attempts: 1, FailureCode: "failed",
	}
	known := time.Unix(2, 0).UTC()
	metadata := map[string]func(*JobRecord){
		"payload schema": func(record *JobRecord) { record.PayloadSchemaVersion = "v1" },
		"original id":    func(record *JobRecord) { record.OriginalID = "original" },
		"topic":          func(record *JobRecord) { record.Topic = "topic" },
		"stream":         func(record *JobRecord) { record.Stream = "stream" },
		"routing key":    func(record *JobRecord) { record.RoutingKey = "route" },
		"consumer group": func(record *JobRecord) { record.ConsumerGroup = "group" },
		"source record":  func(record *JobRecord) { record.SourceRecordID = "source" },
		"enqueued":       func(record *JobRecord) { record.EnqueuedAt = &known },
		"first delivery": func(record *JobRecord) { record.FirstDeliveryAt = &known },
		"last delivery":  func(record *JobRecord) { record.LastDeliveryAt = &known },
		"dead lettered":  func(record *JobRecord) { record.DeadLetteredAt = &known },
		"retry policy":   func(record *JobRecord) { record.RetryPolicy = "policy" },
	}
	for name, mutate := range metadata {
		t.Run(name, func(t *testing.T) {
			record := legacy
			mutate(&record)
			assertValidationField(t, record.Validate(), "envelope_version")
		})
	}

	v1 := legacy
	v1.EnvelopeVersion = CurrentEnvelopeVersion
	v1.Classification = ClassificationPermanent
	v1.FailureSummary = stringOfLength(MaxFailureSummaryBytes)
	v1.Tags = make(map[string]string, MaxRecordTags)
	for index := 0; index < MaxRecordTags; index++ {
		v1.Tags[strconv.Itoa(index)] = "value"
	}
	if err := v1.Validate(); err != nil {
		t.Fatalf("JobRecord.Validate(exact metadata bounds) error = %v", err)
	}

	payload := Payload{Visibility: PayloadRevealed, Size: MaxAdministrativePayloadBytes}
	payload.Data = make([]byte, MaxAdministrativePayloadBytes)
	if err := payload.validate(); err != nil {
		t.Fatalf("Payload.validate(exact bound) error = %v", err)
	}

	pageRequest := PageRequest{
		Limit: MaxPageSize, Cursor: stringOfLength(MaxCursorBytes),
		Search: stringOfLength(MaxSearchBytes), Sort: SortOccurredAt, Direction: SortAscending,
	}
	if err := pageRequest.Validate(); err != nil {
		t.Fatalf("PageRequest.Validate(exact bounds) error = %v", err)
	}
	recordPage := RecordPage{
		Items: make([]JobRecord, MaxPageSize), NextCursor: stringOfLength(MaxCursorBytes),
	}
	for index := range recordPage.Items {
		recordPage.Items[index] = legacy
	}
	if err := recordPage.Validate(); err != nil {
		t.Fatalf("RecordPage.Validate(exact bounds) error = %v", err)
	}
}

func TestRecordChronologyAllowsIndependentlyKnownTimes(t *testing.T) {
	t.Parallel()

	known := time.Unix(2, 0).UTC()
	setters := map[string]func(*JobRecord){
		"enqueued":       func(record *JobRecord) { record.EnqueuedAt = &known },
		"first delivery": func(record *JobRecord) { record.FirstDeliveryAt = &known },
		"last delivery":  func(record *JobRecord) { record.LastDeliveryAt = &known },
		"dead lettered":  func(record *JobRecord) { record.DeadLetteredAt = &known },
		"retention":      func(record *JobRecord) { record.RetentionDeadline = &known },
	}
	for name, set := range setters {
		t.Run(name, func(t *testing.T) {
			record := JobRecord{
				Kind: RecordFailure, ID: "failure", Backend: "backend", Queue: "queue",
				OccurredAt: time.Unix(1, 0), Attempts: 1, FailureCode: "failed",
				EnvelopeVersion: CurrentEnvelopeVersion, Classification: ClassificationPermanent,
			}
			set(&record)
			if err := record.Validate(); err != nil {
				t.Fatalf("JobRecord.Validate() error = %v", err)
			}
		})
	}
}

func TestStatusReaderAcceptsExactProviderAndCursorBoundaries(t *testing.T) {
	t.Parallel()

	workers := make([]WorkerStatusProvider, MaxStatusProviders)
	queues := make([]QueueStatusProvider, MaxStatusProviders)
	for index := 0; index < MaxStatusProviders; index++ {
		workers[index] = valueStatusProvider{}
		queues[index] = valueStatusProvider{}
	}
	for name, config := range map[string]StatusReaderConfig{
		"workers": {Workers: workers},
		"queues":  {Queues: queues},
	} {
		t.Run(name, func(t *testing.T) {
			if reader, err := NewStatusReader(config); err != nil || reader == nil {
				t.Fatalf("NewStatusReader(exact %s) = (%v, %v)", name, reader, err)
			}
		})
	}

	cursor := base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(MaxStatusProviders)))
	start, err := statusPageStart(StatusPageRequest{Limit: 1, Cursor: cursor})
	if err != nil || start != MaxStatusProviders {
		t.Fatalf("statusPageStart(exact max) = (%d, %v)", start, err)
	}
	zeroCursor := base64.RawURLEncoding.EncodeToString([]byte("0"))
	start, err = statusPageStart(StatusPageRequest{Limit: 1, Cursor: zeroCursor})
	if err != nil || start != 0 {
		t.Fatalf("statusPageStart(zero) = (%d, %v)", start, err)
	}
	page, next, err := statusPage([]int{1}, 1, 1)
	if err != nil || len(page) != 0 || next != "" {
		t.Fatalf("statusPage(exact end) = (%v, %q, %v)", page, next, err)
	}
}

func TestLifecycleTargetAndSnapshotDistinctions(t *testing.T) {
	t.Parallel()

	lifecycle := newTestLifecycle(t, 8)
	for _, target := range []Target{
		{Kind: TargetQueue, Name: "critical"},
		{Kind: TargetWorker, Name: "worker-1"},
		{Kind: TargetWorkerGroup, Name: "payments"},
	} {
		if !lifecycle.matches(target) {
			t.Fatalf("matches(%+v) = false", target)
		}
	}
	for _, target := range []Target{
		{Kind: TargetWorkerGroup, Name: "critical"},
		{Kind: TargetQueue, Name: "payments"},
	} {
		if lifecycle.matches(target) {
			t.Fatalf("matches(%+v) = true", target)
		}
	}
	if lifecycle.validTargetState(Target{Kind: TargetQueue}, DesiredDraining) {
		t.Fatal("queue accepted draining")
	}
	if lifecycle.validTargetState(Target{Kind: TargetWorkerGroup}, DesiredState("unknown")) {
		t.Fatal("worker group accepted unknown state")
	}

	draining := newTestLifecycle(t, 8)
	if err := draining.ApplyDesiredState(
		context.Background(), lifecycleRecord(1, DesiredDraining, TargetWorkerGroup, "payments"),
	); err != nil {
		t.Fatalf("ApplyDesiredState(draining) error = %v", err)
	}
	if draining.Snapshot().Terminating {
		t.Fatal("draining snapshot reported terminating")
	}
	terminating := newTestLifecycle(t, 8)
	if err := terminating.ApplyDesiredState(
		context.Background(), lifecycleRecord(1, DesiredTerminating, TargetWorker, "worker-1"),
	); err != nil {
		t.Fatalf("ApplyDesiredState(terminating) error = %v", err)
	}
	if !terminating.Snapshot().Terminating {
		t.Fatal("terminating snapshot omitted terminating state")
	}
}

func TestProtocolAcceptsOneVersionRange(t *testing.T) {
	t.Parallel()

	version := ProtocolVersion{Major: 1, Minor: 2}
	if state := classifyCompatibility(ProtocolRange{Minimum: version, Maximum: version}, version); state != CompatibilityCompatible {
		t.Fatalf("classifyCompatibility(one version) = %q", state)
	}
}

func TestStatusPageStartRejectsOnlyOutOfRangeOffsets(t *testing.T) {
	t.Parallel()

	_, err := statusPageStart(StatusPageRequest{
		Limit:  1,
		Cursor: base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(MaxStatusProviders + 1))),
	})
	if !errors.Is(err, ErrInvalidStatusCursor) {
		t.Fatalf("statusPageStart(over max) error = %v", err)
	}
}
