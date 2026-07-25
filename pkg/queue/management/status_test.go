package management

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestWorkerStatusValidateRejectsMalformedOrUnboundedReports(t *testing.T) {
	t.Parallel()

	valid := WorkerStatus{
		ID:           "worker-1",
		Version:      "v1.2.3",
		StartedAt:    time.Unix(1, 0),
		HeartbeatAt:  time.Unix(2, 0),
		Queues:       []string{"critical"},
		Concurrency:  10,
		State:        WorkerRunning,
		CurrentJobs:  2,
		DrainStatus:  DrainNotRequested,
		Backend:      "valkey-streams",
		Protocol:     ProtocolVersion{Major: 1},
		Capabilities: []Capability{CapabilityWorkerStatus, CapabilityDrain},
	}

	tests := map[string]struct {
		mutate func(*WorkerStatus)
		field  string
	}{
		"identity": {
			mutate: func(status *WorkerStatus) { status.ID = "" },
			field:  "id",
		},
		"identity length": {
			mutate: func(status *WorkerStatus) { status.ID = stringOfLength(MaxIdentityBytes + 1) },
			field:  "id",
		},
		"version": {
			mutate: func(status *WorkerStatus) { status.Version = "" },
			field:  "version",
		},
		"started timestamp": {
			mutate: func(status *WorkerStatus) { status.StartedAt = time.Time{} },
			field:  "started_at",
		},
		"heartbeat timestamp": {
			mutate: func(status *WorkerStatus) { status.HeartbeatAt = time.Time{} },
			field:  "heartbeat_at",
		},
		"heartbeat before start": {
			mutate: func(status *WorkerStatus) { status.HeartbeatAt = status.StartedAt.Add(-time.Second) },
			field:  "heartbeat_at",
		},
		"queue count": {
			mutate: func(status *WorkerStatus) {
				status.Queues = make([]string, MaxQueuesPerWorker+1)
			},
			field: "queues",
		},
		"queue name": {
			mutate: func(status *WorkerStatus) { status.Queues = []string{" "} },
			field:  "queues[0]",
		},
		"concurrency": {
			mutate: func(status *WorkerStatus) { status.Concurrency = 0 },
			field:  "concurrency",
		},
		"current jobs": {
			mutate: func(status *WorkerStatus) { status.CurrentJobs = status.Concurrency + 1 },
			field:  "current_jobs",
		},
		"state": {
			mutate: func(status *WorkerStatus) { status.State = WorkerState("wedged") },
			field:  "state",
		},
		"drain status": {
			mutate: func(status *WorkerStatus) { status.DrainStatus = DrainState("maybe") },
			field:  "drain_status",
		},
		"backend": {
			mutate: func(status *WorkerStatus) { status.Backend = "" },
			field:  "backend",
		},
		"protocol": {
			mutate: func(status *WorkerStatus) { status.Protocol = ProtocolVersion{} },
			field:  "protocol",
		},
		"capability count": {
			mutate: func(status *WorkerStatus) {
				status.Capabilities = make([]Capability, MaxCapabilitiesPerWorker+1)
			},
			field: "capabilities",
		},
		"capability value": {
			mutate: func(status *WorkerStatus) { status.Capabilities = []Capability{""} },
			field:  "capabilities[0]",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := valid
			tt.mutate(&status)

			err := status.Validate()
			var validationError *ValidationError
			if !errors.As(err, &validationError) {
				t.Fatalf("Validate() error = %v, want ValidationError", err)
			}
			if validationError.Field != tt.field {
				t.Fatalf("ValidationError.Field = %q, want %q", validationError.Field, tt.field)
			}
		})
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestStatusMetadataValidatesNativeReporterIdentity(t *testing.T) {
	t.Parallel()

	valid := StatusMetadata{
		ID: "worker-1", Version: "v1.0.0", Concurrency: 4,
		Protocol: ProtocolVersion{Major: 1},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	for name, mutate := range map[string]func(*StatusMetadata){
		"id":          func(value *StatusMetadata) { value.ID = "" },
		"version":     func(value *StatusMetadata) { value.Version = "" },
		"concurrency": func(value *StatusMetadata) { value.Concurrency = 0 },
		"protocol":    func(value *StatusMetadata) { value.Protocol = ProtocolVersion{} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			candidate := valid
			mutate(&candidate)
			if err := candidate.Validate(); err == nil {
				t.Fatalf("Validate(%+v) error = nil", candidate)
			}
		})
	}
}

func TestQueueStatusValidateRequiresIdentityAndTimestamp(t *testing.T) {
	t.Parallel()

	valid := QueueStatus{Backend: "valkey-streams", Queue: "critical", ObservedAt: time.Unix(1, 0)}
	tests := map[string]struct {
		mutate func(*QueueStatus)
		field  string
	}{
		"backend": {
			mutate: func(status *QueueStatus) { status.Backend = "" },
			field:  "backend",
		},
		"queue": {
			mutate: func(status *QueueStatus) { status.Queue = " " },
			field:  "queue",
		},
		"timestamp": {
			mutate: func(status *QueueStatus) { status.ObservedAt = time.Time{} },
			field:  "observed_at",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status := valid
			tt.mutate(&status)
			var validationError *ValidationError
			if !errors.As(status.Validate(), &validationError) || validationError.Field != tt.field {
				t.Fatalf("Validate() = %v, want field %q", validationError, tt.field)
			}
		})
	}

	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

func TestQueueStatusValidateRejectsDishonestSupportedMeasurements(t *testing.T) {
	t.Parallel()

	valid := QueueStatus{Backend: "valkey-streams", Queue: "critical", ObservedAt: time.Unix(1, 0)}
	tests := map[string]struct {
		mutate func(*QueueMetrics)
		field  string
	}{
		"depth": {
			mutate: func(metrics *QueueMetrics) { metrics.Depth = Measurement[int64]{Value: -1, Supported: true} },
			field:  "metrics.depth",
		},
		"lag": {
			mutate: func(metrics *QueueMetrics) { metrics.Lag = Measurement[int64]{Value: -1, Supported: true} },
			field:  "metrics.lag",
		},
		"pending": {
			mutate: func(metrics *QueueMetrics) { metrics.Pending = Measurement[int64]{Value: -1, Supported: true} },
			field:  "metrics.pending",
		},
		"oldest age": {
			mutate: func(metrics *QueueMetrics) {
				metrics.OldestAge = Measurement[time.Duration]{Value: -1, Supported: true}
			},
			field: "metrics.oldest_age",
		},
		"throughput negative": {
			mutate: func(metrics *QueueMetrics) { metrics.Throughput = Measurement[float64]{Value: -1, Supported: true} },
			field:  "metrics.throughput",
		},
		"throughput NaN": {
			mutate: func(metrics *QueueMetrics) {
				metrics.Throughput = Measurement[float64]{Value: math.NaN(), Supported: true}
			},
			field: "metrics.throughput",
		},
		"throughput infinity": {
			mutate: func(metrics *QueueMetrics) {
				metrics.Throughput = Measurement[float64]{Value: math.Inf(1), Supported: true}
			},
			field: "metrics.throughput",
		},
		"runtime": {
			mutate: func(metrics *QueueMetrics) { metrics.Runtime = Measurement[time.Duration]{Value: -1, Supported: true} },
			field:  "metrics.runtime",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			status := valid
			tt.mutate(&status.Metrics)
			assertValidationField(t, status.Validate(), tt.field)
		})
	}

	valid.Metrics = QueueMetrics{
		Depth:      Measurement[int64]{Value: 1, Supported: true},
		Throughput: Measurement[float64]{Value: 1.5, Supported: true},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	valid.Metrics = QueueMetrics{
		Depth:      Measurement[int64]{Value: -1},
		Throughput: Measurement[float64]{Value: math.NaN()},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("unsupported measurements error = %v, want nil", err)
	}
}

func TestStatusPageRequestAndPagesRemainBounded(t *testing.T) {
	t.Parallel()

	request := StatusPageRequest{Limit: MaxStatusPageSize}
	if err := request.Validate(); err != nil {
		t.Fatalf("StatusPageRequest.Validate() error = %v", err)
	}
	request.Limit = 0
	assertValidationField(t, request.Validate(), "limit")
	request = StatusPageRequest{Limit: 1, Cursor: stringOfLength(MaxCursorBytes + 1)}
	assertValidationField(t, request.Validate(), "cursor")

	worker := validWorkerStatus()
	workerPage := WorkerStatusPage{Items: []WorkerStatus{worker}, NextCursor: "next"}
	if err := workerPage.Validate(); err != nil {
		t.Fatalf("WorkerStatusPage.Validate() error = %v", err)
	}
	workerPage.Items[0].ID = ""
	assertValidationField(t, workerPage.Validate(), "items[0].id")
	workerPage = WorkerStatusPage{Items: make([]WorkerStatus, MaxStatusPageSize+1)}
	assertValidationField(t, workerPage.Validate(), "items")
	workerPage = WorkerStatusPage{NextCursor: stringOfLength(MaxCursorBytes + 1)}
	assertValidationField(t, workerPage.Validate(), "next_cursor")

	queue := QueueStatus{Backend: "valkey-streams", Queue: "critical", ObservedAt: time.Unix(1, 0)}
	queuePage := QueueStatusPage{Items: []QueueStatus{queue}}
	if err := queuePage.Validate(); err != nil {
		t.Fatalf("QueueStatusPage.Validate() error = %v", err)
	}
	queuePage.Items[0].Queue = ""
	assertValidationField(t, queuePage.Validate(), "items[0].queue")
	queuePage = QueueStatusPage{Items: make([]QueueStatus, MaxStatusPageSize+1)}
	assertValidationField(t, queuePage.Validate(), "items")
	queuePage = QueueStatusPage{NextCursor: stringOfLength(MaxCursorBytes + 1)}
	assertValidationField(t, queuePage.Validate(), "next_cursor")

	var reader StatusReader = statusReaderStub{}
	if _, err := reader.ListWorkers(context.Background(), StatusPageRequest{}); err != nil {
		t.Fatalf("ListWorkers() error = %v", err)
	}
	if _, err := reader.ListQueues(context.Background(), StatusPageRequest{}); err != nil {
		t.Fatalf("ListQueues() error = %v", err)
	}
}

func TestValidationErrorIncludesField(t *testing.T) {
	t.Parallel()

	got := (&ValidationError{Field: "id", Problem: "is required"}).Error()
	if got != "id: is required" {
		t.Fatalf("Error() = %q, want %q", got, "id: is required")
	}
}

func stringOfLength(length int) string {
	value := make([]byte, length)
	for index := range value {
		value[index] = 'a'
	}

	return string(value)
}

func validWorkerStatus() WorkerStatus {
	return WorkerStatus{
		ID:           "worker-1",
		Version:      "v1.2.3",
		StartedAt:    time.Unix(1, 0),
		HeartbeatAt:  time.Unix(2, 0),
		Queues:       []string{"critical"},
		Concurrency:  10,
		State:        WorkerRunning,
		CurrentJobs:  2,
		DrainStatus:  DrainNotRequested,
		Backend:      "valkey-streams",
		Protocol:     ProtocolVersion{Major: 1},
		Capabilities: []Capability{CapabilityWorkerStatus},
	}
}

type statusReaderStub struct{}

func (statusReaderStub) ListWorkers(context.Context, StatusPageRequest) (WorkerStatusPage, error) {
	return WorkerStatusPage{}, nil
}

func (statusReaderStub) ListQueues(context.Context, StatusPageRequest) (QueueStatusPage, error) {
	return QueueStatusPage{}, nil
}
