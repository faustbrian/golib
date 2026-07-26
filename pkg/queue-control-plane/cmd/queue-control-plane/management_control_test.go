package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestManagementRuntimeDispatchesTenantCommandsThroughRemoteClient(t *testing.T) {
	t.Parallel()

	document := `{"tenants":[{"id":"tenant-1","base_url":"https://worker.example","token_file":"token"}]}`
	client := &managementControlStub{}
	runtime, err := loadManagementRuntime(
		"management.json", 1024,
		func(path string) (io.ReadCloser, error) {
			if path == "management.json" {
				return io.NopCloser(strings.NewReader(document)), nil
			}
			return io.NopCloser(strings.NewReader("transport-secret")), nil
		},
		func(string, string) (queue.StatusReader, error) { return client, nil },
	)
	if err != nil || runtime.Dispatcher == nil {
		t.Fatalf("loadManagementRuntime() = (%+v, %v)", runtime, err)
	}
	requestedAt := time.Now().UTC()
	resultDispatcher, ok := runtime.Dispatcher.(control.ResultDispatcher)
	if !ok {
		t.Fatalf("Dispatcher = %T, want control.ResultDispatcher", runtime.Dispatcher)
	}
	outcome, err := resultDispatcher.DispatchResult(context.Background(), controlplane.Command{
		CommandID:      "command-1-id",
		IdempotencyKey: "command-1", TenantID: "tenant-1", Actor: "operator-1",
		Reason: "drain before deployment", Action: controlplane.ActionDrain,
		Target:      controlplane.Target{Kind: controlplane.TargetWorker, Name: "worker-1"},
		RequestedAt: requestedAt,
	})
	if err != nil || outcome.Status != controlplane.CommandSucceeded {
		t.Fatalf("DispatchResult() = (%+v, %v)", outcome, err)
	}
	if client.command.ID != "command-1-id" || client.command.Protocol != (queue.ProtocolVersion{Major: 1}) ||
		client.command.Deadline.Sub(client.command.RequestedAt) != managementCommandTimeout {
		t.Fatalf("remote command = %+v", client.command)
	}
	_, err = resultDispatcher.DispatchResult(context.Background(), controlplane.Command{
		CommandID:      "command-2-id",
		IdempotencyKey: "command-2", TenantID: "tenant-2", Actor: "operator-1",
		Reason: "drain before deployment", Action: controlplane.ActionDrain,
		Target:      controlplane.Target{Kind: controlplane.TargetWorker, Name: "worker-1"},
		RequestedAt: requestedAt,
	})
	if !errors.Is(err, ErrManagementTenantUnavailable) {
		t.Fatalf("missing tenant dispatch error = %v", err)
	}
}

func TestManagementRuntimeRejectsStatusOnlyClient(t *testing.T) {
	t.Parallel()

	document := `{"tenants":[{"id":"tenant-1","base_url":"https://worker.example","token_file":"token"}]}`
	runtime, err := loadManagementRuntime(
		"management.json", 1024,
		func(path string) (io.ReadCloser, error) {
			if path == "management.json" {
				return io.NopCloser(strings.NewReader(document)), nil
			}
			return io.NopCloser(strings.NewReader("transport-secret")), nil
		},
		func(string, string) (queue.StatusReader, error) {
			return statusOnlyManagementClient{}, nil
		},
	)
	if runtime != (managementRuntime{}) || !errors.Is(err, ErrInvalidManagementRuntime) {
		t.Fatalf("loadManagementRuntime() = (%+v, %v)", runtime, err)
	}
}

func TestManagementRuntimeRejectsClientWithoutRecordReader(t *testing.T) {
	t.Parallel()

	document := `{"tenants":[{"id":"tenant-1","base_url":"https://worker.example","token_file":"token"}]}`
	runtime, err := loadManagementRuntime(
		"management.json", 1024,
		func(path string) (io.ReadCloser, error) {
			if path == "management.json" {
				return io.NopCloser(strings.NewReader(document)), nil
			}
			return io.NopCloser(strings.NewReader("transport-secret")), nil
		},
		func(string, string) (queue.StatusReader, error) {
			return statusControlOnlyManagementClient{}, nil
		},
	)
	if runtime != (managementRuntime{}) || !errors.Is(err, ErrInvalidManagementRuntime) {
		t.Fatalf("loadManagementRuntime() = (%+v, %v)", runtime, err)
	}
}

type managementControlStub struct {
	command queue.Command
}

func (s *managementControlStub) ListWorkers(
	context.Context,
	queue.StatusPageRequest,
) (queue.WorkerStatusPage, error) {
	return queue.WorkerStatusPage{}, nil
}

func (s *managementControlStub) ListQueues(
	context.Context,
	queue.StatusPageRequest,
) (queue.QueueStatusPage, error) {
	return queue.QueueStatusPage{}, nil
}

func (s *managementControlStub) Execute(
	_ context.Context,
	command queue.Command,
) (queue.CommandResult, error) {
	s.command = command
	return queue.CommandResult{
		CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
		WorkerID: "worker-1", Protocol: command.Protocol,
		Status: queue.CommandAcknowledged, CompletedAt: time.Now().UTC(),
	}, nil
}

func (*managementControlStub) ListFailures(
	context.Context,
	queue.PageRequest,
) (queue.RecordPage, error) {
	return queue.RecordPage{}, nil
}

func (*managementControlStub) ListDeadLetters(
	context.Context,
	queue.PageRequest,
) (queue.RecordPage, error) {
	return queue.RecordPage{}, nil
}

func (*managementControlStub) Inspect(
	context.Context,
	queue.InspectRequest,
) (queue.JobRecord, error) {
	return queue.JobRecord{}, nil
}

type statusOnlyManagementClient struct{}

type statusControlOnlyManagementClient struct {
	statusOnlyManagementClient
}

func (statusControlOnlyManagementClient) Execute(
	_ context.Context,
	command queue.Command,
) (queue.CommandResult, error) {
	return queue.CommandResult{
		CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
		WorkerID: "worker-1", Protocol: command.Protocol,
		Status: queue.CommandAcknowledged, CompletedAt: time.Now().UTC(),
	}, nil
}

func (statusOnlyManagementClient) ListWorkers(
	context.Context,
	queue.StatusPageRequest,
) (queue.WorkerStatusPage, error) {
	return queue.WorkerStatusPage{}, nil
}

func (statusOnlyManagementClient) ListQueues(
	context.Context,
	queue.StatusPageRequest,
) (queue.QueueStatusPage, error) {
	return queue.QueueStatusPage{}, nil
}
