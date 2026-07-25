package control

import (
	"context"
	"errors"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
)

func TestRoutingDispatcherRequiresBothBoundaries(t *testing.T) {
	t.Parallel()

	valid := &dispatcherStub{}
	var typedNil *dispatcherStub
	for _, input := range [][2]Dispatcher{
		{nil, valid}, {valid, nil}, {typedNil, valid}, {valid, typedNil},
	} {
		dispatcher, err := NewRoutingDispatcher(input[0], input[1])
		if dispatcher != nil || !errors.Is(err, ErrInvalidDispatcherConfiguration) {
			t.Fatalf("NewRoutingDispatcher() = (%v, %v)", dispatcher, err)
		}
	}
}

func TestRoutingDispatcherSeparatesDataPlaneAndWorkloadCommands(t *testing.T) {
	t.Parallel()

	dataPlane := &dispatcherStub{}
	workloads := &dispatcherStub{}
	dispatcher, err := NewRoutingDispatcher(dataPlane, workloads)
	if err != nil {
		t.Fatalf("NewRoutingDispatcher() error = %v", err)
	}

	drain := validCommand()
	if err := dispatcher.Dispatch(context.Background(), drain); err != nil {
		t.Fatalf("Dispatch(drain) error = %v", err)
	}
	scale := drain
	scale.Action = controlplane.ActionScale
	scale.Target = controlplane.Target{Kind: controlplane.TargetWorkload, Name: "billing-workers"}
	scale.Scale = &controlplane.Scale{Replicas: 5}
	if err := dispatcher.Dispatch(context.Background(), scale); err != nil {
		t.Fatalf("Dispatch(scale) error = %v", err)
	}

	if dataPlane.calls != 1 || dataPlane.command.Action != controlplane.ActionDrain {
		t.Fatalf("data-plane dispatch = %d calls with %q", dataPlane.calls, dataPlane.command.Action)
	}
	if workloads.calls != 1 || workloads.command.Action != controlplane.ActionScale {
		t.Fatalf("workload dispatch = %d calls with %q", workloads.calls, workloads.command.Action)
	}
}

func TestUnavailableDispatcherFailsClosed(t *testing.T) {
	t.Parallel()

	err := (UnavailableDispatcher{}).Dispatch(context.Background(), controlplane.Command{})
	if !errors.Is(err, ErrDataPlaneUnavailable) {
		t.Fatalf("Dispatch() error = %v, want ErrDataPlaneUnavailable", err)
	}
}

func TestRoutingDispatcherPreservesStructuredDataPlaneResults(t *testing.T) {
	t.Parallel()

	completedAt := time.Date(2026, time.July, 16, 12, 0, 1, 0, time.UTC)
	dataPlane := &resultDispatcherStub{outcome: DispatchOutcome{
		Status:      controlplane.CommandUnknown,
		Failure:     controlplane.FailureOutcomeUnknown,
		CompletedAt: completedAt,
	}}
	workloads := &dispatcherStub{}
	dispatcher, err := NewRoutingDispatcher(dataPlane, workloads)
	if err != nil {
		t.Fatalf("NewRoutingDispatcher() error = %v", err)
	}
	outcome, err := dispatcher.DispatchResult(context.Background(), validCommand())
	if err != nil || outcome != dataPlane.outcome {
		t.Fatalf("DispatchResult() = (%+v, %v), want %+v", outcome, err, dataPlane.outcome)
	}
	if dataPlane.resultCalls != 1 || dataPlane.legacyCalls != 0 || workloads.calls != 0 {
		t.Fatalf(
			"calls = result:%d legacy:%d workload:%d",
			dataPlane.resultCalls,
			dataPlane.legacyCalls,
			workloads.calls,
		)
	}
}

func TestRoutingDispatcherWrapsLegacyResultsWithCompletionTime(t *testing.T) {
	t.Parallel()

	dataPlane := &dispatcherStub{}
	workloads := &dispatcherStub{}
	dispatcher, err := NewRoutingDispatcher(dataPlane, workloads)
	if err != nil {
		t.Fatalf("NewRoutingDispatcher() error = %v", err)
	}

	command := validCommand()
	startedAt := time.Now()
	outcome, err := dispatcher.DispatchResult(context.Background(), command)
	finishedAt := time.Now()
	if err != nil || outcome.Status != controlplane.CommandSucceeded ||
		outcome.CompletedAt.Before(startedAt) || outcome.CompletedAt.After(finishedAt) {
		t.Fatalf("data-plane DispatchResult() = (%+v, %v)", outcome, err)
	}
	command.Action = controlplane.ActionScale
	command.Target = controlplane.Target{Kind: controlplane.TargetWorkload, Name: "workers"}
	command.Scale = &controlplane.Scale{Replicas: 2}
	startedAt = time.Now()
	outcome, err = dispatcher.DispatchResult(context.Background(), command)
	finishedAt = time.Now()
	if err != nil || outcome.Status != controlplane.CommandSucceeded ||
		outcome.CompletedAt.Before(startedAt) || outcome.CompletedAt.After(finishedAt) {
		t.Fatalf("workload DispatchResult() = (%+v, %v)", outcome, err)
	}
	if dataPlane.calls != 1 || workloads.calls != 1 {
		t.Fatalf("legacy calls = data plane:%d workloads:%d", dataPlane.calls, workloads.calls)
	}

	dataPlane.err = errors.New("unavailable")
	if _, err := dispatcher.DispatchResult(context.Background(), validCommand()); !errors.Is(err, dataPlane.err) {
		t.Fatalf("failed DispatchResult() error = %v", err)
	}
}
