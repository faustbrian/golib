package main

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	"github.com/faustbrian/golib/pkg/queue-control-plane/dataplane"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestLoadManagementRuntimeBuildsTenantStatusSource(t *testing.T) {
	t.Parallel()

	document := `{"tenants":[{"id":"tenant-1","base_url":"https://worker.example","token_file":"/run/secrets/tenant-1"}]}`
	worker := validManagementWorkerStatus()
	reader := &managementStatusStub{
		workers: queue.WorkerStatusPage{Items: []queue.WorkerStatus{worker}},
		queues: queue.QueueStatusPage{
			Items: []queue.QueueStatus{validManagementQueueStatus()},
		},
	}
	runtime, err := loadManagementRuntime(
		"/etc/control/management.json",
		1024,
		func(path string) (io.ReadCloser, error) {
			if path == "/etc/control/management.json" {
				return io.NopCloser(strings.NewReader(document)), nil
			}
			if path == "/run/secrets/tenant-1" {
				return io.NopCloser(strings.NewReader("transport-secret\n")), nil
			}
			return nil, errors.New("unexpected path")
		},
		func(baseURL, token string) (queue.StatusReader, error) {
			if baseURL != "https://worker.example" || token != "transport-secret" {
				t.Fatalf("client input = (%q, %q)", baseURL, token)
			}
			return reader, nil
		},
	)
	if err != nil || runtime.Queues == nil || runtime.Workers == nil || runtime.Records == nil {
		t.Fatalf("loadManagementRuntime() = (%+v, %v)", runtime, err)
	}
	page, err := runtime.Queues.ListQueues(
		context.Background(), "tenant-1", queue.StatusPageRequest{Limit: 1},
	)
	if err != nil || len(page.Items) != 1 || page.Items[0].Queue != "critical" {
		t.Fatalf("ListQueues() = (%+v, %v)", page, err)
	}
	if _, err := runtime.Queues.ListQueues(
		context.Background(), "tenant-2", queue.StatusPageRequest{Limit: 1},
	); !errors.Is(err, ErrManagementTenantUnavailable) {
		t.Fatalf("missing tenant error = %v", err)
	}
	if _, err := runtime.Records.ListFailures(
		context.Background(), "tenant-2", queue.PageRequest{
			Limit: 1, Sort: queue.SortOccurredAt, Direction: queue.SortDescending,
		},
	); !errors.Is(err, ErrManagementTenantUnavailable) {
		t.Fatalf("missing record tenant error = %v", err)
	}
	if _, ok := any(runtime.Queues).(*dataplane.StatusSource); !ok {
		t.Fatalf("Queues = %T, want *dataplane.StatusSource", runtime.Queues)
	}
	records, err := runtime.Records.ListFailures(context.Background(), "tenant-1", queue.PageRequest{
		Limit: 1, Sort: queue.SortOccurredAt, Direction: queue.SortDescending,
	})
	if err != nil || len(records.Items) != 0 {
		t.Fatalf("Records.ListFailures() = (%+v, %v)", records, err)
	}
	snapshot, err := runtime.Workers.SnapshotTenant(
		context.Background(), "tenant-1", worker.HeartbeatAt, time.Minute,
	)
	if err != nil || len(snapshot.Workers) != 1 || snapshot.Workers[0].WorkerID != worker.ID {
		t.Fatalf("Workers.SnapshotTenant() = (%+v, %v)", snapshot, err)
	}
}

func TestInvalidManagementTenantRejectsMissingParsedURL(t *testing.T) {
	t.Parallel()

	tenant := managementTenantEntry{
		ID: "tenant-1", BaseURL: "https://worker.example", TokenFile: "/run/secrets/token",
	}
	if !invalidManagementTenantWithParser(
		tenant,
		func(string) (*url.URL, error) { return nil, nil },
	) {
		t.Fatal("invalidManagementTenantWithParser() accepted a missing URL")
	}
}

func TestLoadManagementRuntimeRejectsMalformedOrUnsafeDocuments(t *testing.T) {
	t.Parallel()

	stageErr := errors.New("stage failed with token=secret")
	validDocument := `{"tenants":[{"id":"tenant-1","base_url":"https://worker.example","token_file":"/run/secrets/token"}]}`
	validOpen := func(path string) (io.ReadCloser, error) {
		if path == "management.json" {
			return io.NopCloser(strings.NewReader(validDocument)), nil
		}
		return io.NopCloser(strings.NewReader("token")), nil
	}
	validClient := func(string, string) (queue.StatusReader, error) {
		return &managementStatusStub{}, nil
	}
	tests := map[string]struct {
		maxBytes int64
		open     managementFileOpener
		client   managementStatusFactory
	}{
		"invalid bound": {
			maxBytes: defaultAccessDocumentSize + 1,
			open:     validOpen,
			client:   validClient,
		},
		"missing open": {
			maxBytes: 1024,
			client:   validClient,
		},
		"missing client": {
			maxBytes: 1024,
			open:     validOpen,
		},
		"open": {
			maxBytes: 1024,
			open:     func(string) (io.ReadCloser, error) { return nil, stageErr },
			client:   validClient,
		},
		"bound": {
			maxBytes: 8,
			open: func(string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(validDocument)), nil
			},
			client: validClient,
		},
		"unknown field": {
			maxBytes: 1024,
			open: func(string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(`{"tenants":[],"token":"secret"}`)), nil
			},
			client: validClient,
		},
		"trailing document": {
			maxBytes: 1024,
			open: func(string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(validDocument + `{}`)), nil
			},
			client: validClient,
		},
		"empty tenants": {
			maxBytes: 1024,
			open: func(string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(`{"tenants":[]}`)), nil
			},
			client: validClient,
		},
		"duplicate tenant": {
			maxBytes: 1024,
			open: func(path string) (io.ReadCloser, error) {
				if path == "management.json" {
					return io.NopCloser(strings.NewReader(`{"tenants":[{"id":"tenant-1","base_url":"https://one.example","token_file":"one"},{"id":"tenant-1","base_url":"https://two.example","token_file":"two"}]}`)), nil
				}
				return io.NopCloser(strings.NewReader("token")), nil
			},
			client: validClient,
		},
		"invalid tenant": {
			maxBytes: 1024,
			open: func(string) (io.ReadCloser, error) {
				return io.NopCloser(strings.NewReader(`{"tenants":[{"id":" ","base_url":"https://worker.example","token_file":"token"}]}`)), nil
			},
			client: validClient,
		},
		"token open": {
			maxBytes: 1024,
			open: func(path string) (io.ReadCloser, error) {
				if path == "management.json" {
					return io.NopCloser(strings.NewReader(validDocument)), nil
				}
				return nil, stageErr
			},
			client: validClient,
		},
		"token bound": {
			maxBytes: 1024,
			open: func(path string) (io.ReadCloser, error) {
				if path == "management.json" {
					return io.NopCloser(strings.NewReader(validDocument)), nil
				}
				return io.NopCloser(strings.NewReader(strings.Repeat("x", maxManagementTokenBytes+1))), nil
			},
			client: validClient,
		},
		"empty token": {
			maxBytes: 1024,
			open: func(path string) (io.ReadCloser, error) {
				if path == "management.json" {
					return io.NopCloser(strings.NewReader(validDocument)), nil
				}
				return io.NopCloser(strings.NewReader(" \n")), nil
			},
			client: validClient,
		},
		"client": {
			maxBytes: 1024,
			open:     validOpen,
			client: func(string, string) (queue.StatusReader, error) {
				return nil, stageErr
			},
		},
		"nil client": {
			maxBytes: 1024,
			open:     validOpen,
			client: func(string, string) (queue.StatusReader, error) {
				return nil, nil
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runtime, err := loadManagementRuntime(
				"management.json", tt.maxBytes, tt.open, tt.client,
			)
			if runtime != (managementRuntime{}) || !errors.Is(err, ErrInvalidManagementRuntime) ||
				strings.Contains(err.Error(), "secret") {
				t.Fatalf("loadManagementRuntime() = (%+v, %v)", runtime, err)
			}
		})
	}
	runtime, err := loadManagementRuntimeWithSource(
		"management.json", 1024, validOpen, validClient,
		func(dataplane.StatusReaderResolver) (*dataplane.StatusSource, error) {
			return nil, stageErr
		},
		dataplane.NewRecordSource, dataplane.NewFleetSource,
		newProductionManagementDispatcher,
	)
	if runtime != (managementRuntime{}) || !errors.Is(err, ErrInvalidManagementRuntime) {
		t.Fatalf("source failure = (%+v, %v)", runtime, err)
	}
	runtime, err = loadManagementRuntimeWithSource(
		"management.json", 1024, validOpen, validClient,
		dataplane.NewStatusSource, dataplane.NewRecordSource,
		func(dataplane.WorkerStatusSource) (*dataplane.FleetSource, error) {
			return nil, stageErr
		},
		newProductionManagementDispatcher,
	)
	if runtime != (managementRuntime{}) || !errors.Is(err, ErrInvalidManagementRuntime) {
		t.Fatalf("fleet failure = (%+v, %v)", runtime, err)
	}
	runtime, err = loadManagementRuntimeWithSource(
		"management.json", 1024, validOpen, validClient,
		dataplane.NewStatusSource, dataplane.NewRecordSource,
		dataplane.NewFleetSource,
		func(dataplane.ControllerResolver) (control.Dispatcher, error) {
			return nil, stageErr
		},
	)
	if runtime != (managementRuntime{}) || !errors.Is(err, ErrInvalidManagementRuntime) {
		t.Fatalf("dispatcher failure = (%+v, %v)", runtime, err)
	}
	runtime, err = loadManagementRuntimeWithSource(
		"management.json", 1024, validOpen, validClient,
		dataplane.NewStatusSource,
		func(dataplane.RecordReaderResolver) (*dataplane.RecordSource, error) {
			return nil, stageErr
		},
		dataplane.NewFleetSource, newProductionManagementDispatcher,
	)
	if runtime != (managementRuntime{}) || !errors.Is(err, ErrInvalidManagementRuntime) {
		t.Fatalf("record source failure = (%+v, %v)", runtime, err)
	}
	runtime, err = loadManagementRuntime("", 1024, validOpen, validClient)
	if runtime != (managementRuntime{}) || !errors.Is(err, ErrInvalidManagementRuntime) {
		t.Fatalf("invalid path = (%+v, %v)", runtime, err)
	}
}

func TestProductionManagementConstructorsLoadSecretFiles(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	tokenPath := filepath.Join(directory, "token")
	documentPath := filepath.Join(directory, "management.json")
	if err := os.WriteFile(tokenPath, []byte("transport-secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(token) error = %v", err)
	}
	document := `{"tenants":[{"id":"tenant-1","base_url":"https://worker.example","token_file":"` + tokenPath + `"}]}`
	if err := os.WriteFile(documentPath, []byte(document), 0o600); err != nil {
		t.Fatalf("WriteFile(document) error = %v", err)
	}
	file, err := openManagementFile(documentPath)
	if err != nil {
		t.Fatalf("openManagementFile() error = %v", err)
	}
	_ = file.Close()
	runtime, err := loadProductionManagement(documentPath, 1024)
	if err != nil || runtime.Queues == nil {
		t.Fatalf("loadProductionManagement() = (%+v, %v)", runtime, err)
	}
	reader, err := newProductionManagementStatus("https://worker.example", "token")
	if err != nil || reader == nil {
		t.Fatalf("newProductionManagementStatus() = (%v, %v)", reader, err)
	}
	if reader, err := newProductionManagementStatus("://invalid", "token"); !missingDependency(reader) || err == nil {
		t.Fatalf("invalid status client = (%v, %v)", reader, err)
	}
}

type managementStatusStub struct {
	workers queue.WorkerStatusPage
	queues  queue.QueueStatusPage
}

func validManagementWorkerStatus() queue.WorkerStatus {
	return queue.WorkerStatus{
		ID: "worker-1", Version: "v1.0.0", StartedAt: time.Unix(1, 0).UTC(),
		HeartbeatAt: time.Unix(2, 0).UTC(), Queues: []string{"critical"},
		Concurrency: 1, State: queue.WorkerRunning,
		DrainStatus: queue.DrainNotRequested, Backend: "valkey-streams",
		Protocol: queue.ProtocolVersion{Major: 1},
	}
}

func validManagementQueueStatus() queue.QueueStatus {
	return queue.QueueStatus{
		Backend: "valkey-streams", Queue: "critical", ObservedAt: time.Unix(1, 0).UTC(),
	}
}

func (s *managementStatusStub) ListWorkers(
	context.Context,
	queue.StatusPageRequest,
) (queue.WorkerStatusPage, error) {
	return s.workers, nil
}

func (s *managementStatusStub) ListQueues(
	context.Context,
	queue.StatusPageRequest,
) (queue.QueueStatusPage, error) {
	return s.queues, nil
}

func (s *managementStatusStub) Execute(
	_ context.Context,
	command queue.Command,
) (queue.CommandResult, error) {
	return queue.CommandResult{
		CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
		WorkerID: "worker-1", Protocol: command.Protocol,
		Status: queue.CommandAcknowledged, CompletedAt: time.Now().UTC(),
	}, nil
}

func (*managementStatusStub) ListFailures(
	context.Context,
	queue.PageRequest,
) (queue.RecordPage, error) {
	return queue.RecordPage{}, nil
}

func (*managementStatusStub) ListDeadLetters(
	context.Context,
	queue.PageRequest,
) (queue.RecordPage, error) {
	return queue.RecordPage{}, nil
}

func (*managementStatusStub) Inspect(
	context.Context,
	queue.InspectRequest,
) (queue.JobRecord, error) {
	return queue.JobRecord{}, nil
}
