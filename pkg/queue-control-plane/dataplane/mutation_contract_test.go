package dataplane

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	queue "github.com/faustbrian/golib/pkg/queue/management"
)

func TestCommandResultMatchesEveryEnvelopeFieldExactly(t *testing.T) {
	t.Parallel()

	requestedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	command := queue.Command{
		ID: "command-1", IdempotencyKey: "request-1",
		Protocol: queue.ProtocolVersion{Major: 1}, RequestedAt: requestedAt,
	}
	valid := queue.CommandResult{
		CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
		WorkerID: "worker-1", Protocol: command.Protocol,
		Status: queue.CommandAcknowledged, CompletedAt: requestedAt,
	}
	if !validResult(command, valid) {
		t.Fatal("validResult rejected an exact command acknowledgement")
	}
	for name, mutate := range map[string]func(*queue.CommandResult){
		"command id":      func(result *queue.CommandResult) { result.CommandID = "command-2" },
		"idempotency key": func(result *queue.CommandResult) { result.IdempotencyKey = "request-2" },
		"protocol":        func(result *queue.CommandResult) { result.Protocol.Minor++ },
		"completion time": func(result *queue.CommandResult) { result.CompletedAt = requestedAt.Add(-time.Nanosecond) },
	} {
		t.Run(name, func(t *testing.T) {
			result := valid
			mutate(&result)
			if validResult(command, result) {
				t.Fatalf("validResult accepted mismatched %s", name)
			}
		})
	}
}

func TestTenantBoundariesFailClosedIndependently(t *testing.T) {
	t.Parallel()

	exactTenant := strings.Repeat("t", controlplane.MaxIdentityBytes)
	oversizedTenant := exactTenant + "t"
	now := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)

	fleetReader := &pagedWorkerStatusStub{}
	fleetSource, err := NewFleetSource(fleetReader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fleetSource.SnapshotTenant(
		context.Background(), exactTenant, now, time.Nanosecond,
	); err != nil {
		t.Fatalf("exact fleet tenant boundary: %v", err)
	}
	for name, test := range map[string]struct {
		name       string
		tenant     string
		observedAt time.Time
		staleAfter time.Duration
	}{
		"blank tenant":      {tenant: " ", observedAt: now, staleAfter: time.Second},
		"oversized tenant":  {tenant: oversizedTenant, observedAt: now, staleAfter: time.Second},
		"zero observation":  {tenant: exactTenant, staleAfter: time.Second},
		"zero stale window": {tenant: exactTenant, observedAt: now},
		"negative stale window": {
			tenant: exactTenant, observedAt: now, staleAfter: -time.Nanosecond,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := fleetSource.SnapshotTenant(
				context.Background(), test.tenant, test.observedAt, test.staleAfter,
			); !errors.Is(err, ErrInvalidStatusRequest) {
				t.Fatalf("SnapshotTenant() error = %v", err)
			}
		})
	}

	statusResolver := &statusResolverStub{reader: &statusReaderStub{}}
	statusSource := mustStatusSource(t, statusResolver)
	statusRequest := queue.StatusPageRequest{Limit: 1}
	if _, err := statusSource.ListWorkers(
		context.Background(), exactTenant, statusRequest,
	); err != nil {
		t.Fatalf("exact status tenant boundary: %v", err)
	}
	statusCalls := statusResolver.calls
	if _, err := statusSource.ListWorkers(
		context.Background(), oversizedTenant, statusRequest,
	); !errors.Is(err, ErrInvalidStatusRequest) || statusResolver.calls != statusCalls {
		t.Fatalf("oversized status tenant error/calls = %v/%d", err, statusResolver.calls)
	}

	recordResolver := &recordResolverStub{reader: &recordReaderStub{}}
	recordSource := mustRecordSource(t, recordResolver)
	recordRequest := queue.PageRequest{
		Limit: 1, Sort: queue.SortOccurredAt, Direction: queue.SortDescending,
	}
	if _, err := recordSource.ListFailures(
		context.Background(), exactTenant, recordRequest,
	); err != nil {
		t.Fatalf("exact record tenant boundary: %v", err)
	}
	recordCalls := recordResolver.calls
	if _, err := recordSource.ListFailures(
		context.Background(), oversizedTenant, recordRequest,
	); !errors.Is(err, ErrInvalidRecordRequest) || recordResolver.calls != recordCalls {
		t.Fatalf("oversized record tenant error/calls = %v/%d", err, recordResolver.calls)
	}
}

func TestPayloadVisibilityPermitsOnlyEqualOrMoreRedactedOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		requested queue.PayloadVisibility
		actual    queue.PayloadVisibility
		want      bool
	}{
		{queue.PayloadHidden, queue.PayloadHidden, true},
		{queue.PayloadHidden, queue.PayloadRedacted, false},
		{queue.PayloadHidden, queue.PayloadRevealed, false},
		{queue.PayloadRedacted, queue.PayloadHidden, true},
		{queue.PayloadRedacted, queue.PayloadRedacted, true},
		{queue.PayloadRedacted, queue.PayloadRevealed, false},
		{queue.PayloadRevealed, queue.PayloadHidden, true},
		{queue.PayloadRevealed, queue.PayloadRedacted, true},
		{queue.PayloadRevealed, queue.PayloadRevealed, true},
	}
	for _, test := range tests {
		if got := visibilityPermitted(test.requested, test.actual); got != test.want {
			t.Errorf("visibilityPermitted(%q, %q) = %t, want %t",
				test.requested, test.actual, got, test.want)
		}
	}
}

func TestWorkerOrderingIsStrict(t *testing.T) {
	t.Parallel()

	if !workerIDLess("worker-a", "worker-b") {
		t.Fatal("workerIDLess rejected ascending worker IDs")
	}
	if workerIDLess("worker-a", "worker-a") || workerIDLess("worker-b", "worker-a") {
		t.Fatal("workerIDLess is not a strict ascending order")
	}
}
