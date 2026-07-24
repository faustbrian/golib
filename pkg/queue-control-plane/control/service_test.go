package control

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestInstrumentedServiceRecordsBoundedCommandOutcome(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	service, err := NewInstrumentedService(
		&authorizerStub{},
		&journalStub{created: true},
		&dispatcherStub{err: errors.New("worker disconnected")},
		time.Now,
		provider.Meter("test"),
	)
	if err != nil {
		t.Fatalf("NewInstrumentedService() error = %v", err)
	}
	if _, err := service.Execute(context.Background(), validCommand()); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var data metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &data); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(data.ScopeMetrics) != 1 || len(data.ScopeMetrics[0].Metrics) != 2 {
		t.Fatalf("metrics = %+v, want count and duration", data.ScopeMetrics)
	}
	for _, metric := range data.ScopeMetrics[0].Metrics {
		switch points := metric.Data.(type) {
		case metricdata.Sum[int64]:
			if len(points.DataPoints) != 1 {
				t.Fatalf("count = %+v, want one point", points)
			}
			outcome, ok := points.DataPoints[0].Attributes.Value(attribute.Key("outcome"))
			if points.DataPoints[0].Value != 1 || !ok ||
				outcome != attribute.StringValue("dispatch_failed") {
				t.Fatalf("count = %+v, want dispatch_failed", points)
			}
		case metricdata.Histogram[float64]:
			if len(points.DataPoints) != 1 || points.DataPoints[0].Count != 1 {
				t.Fatalf("duration = %+v, want one observation", points)
			}
		default:
			t.Fatalf("metric data = %T", metric.Data)
		}
	}
}

func TestNewInstrumentedServiceFailsOnInvalidInstruments(t *testing.T) {
	t.Parallel()

	instrumentErr := errors.New("instrument unavailable")
	base := metricnoop.NewMeterProvider().Meter("test")
	meters := []otelmetric.Meter{
		nil,
		failingMeter{Meter: base, counterErr: instrumentErr},
		failingMeter{Meter: base, histogramErr: instrumentErr},
	}
	for _, meter := range meters {
		service, err := NewInstrumentedService(
			&authorizerStub{}, &journalStub{}, &dispatcherStub{}, time.Now, meter,
		)
		if service != nil || err == nil {
			t.Fatalf("NewInstrumentedService() = (%v, %v), want nil error", service, err)
		}
	}
}

func TestInstrumentedServiceContinuesWhenMetricPipelineIsUnavailable(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	service, err := NewInstrumentedService(
		&authorizerStub{},
		&journalStub{created: true},
		&dispatcherStub{},
		time.Now,
		provider.Meter("test"),
	)
	if err != nil {
		t.Fatalf("NewInstrumentedService() error = %v", err)
	}
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("MeterProvider.Shutdown() error = %v", err)
	}

	result, err := service.Execute(context.Background(), validCommand())
	if err != nil || result.Status != controlplane.CommandSucceeded {
		t.Fatalf("Execute() after metric shutdown = (%+v, %v), want success", result, err)
	}
}

func TestTelemetryActionBoundsUnknownValues(t *testing.T) {
	t.Parallel()

	if got := telemetryAction(controlplane.Action("tenant-controlled")); got != "_OTHER" {
		t.Fatalf("telemetryAction() = %q, want _OTHER", got)
	}
	for status, want := range map[controlplane.CommandStatus]string{
		controlplane.CommandSucceeded:   "succeeded",
		controlplane.CommandFailed:      "failed",
		controlplane.CommandUnsupported: "unsupported",
		controlplane.CommandTimedOut:    "timed_out",
		controlplane.CommandPartial:     "partial",
		controlplane.CommandUnknown:     "unknown",
		controlplane.CommandAccepted:    "invalid",
		controlplane.CommandStatus(""):  "_OTHER",
	} {
		if got := dispatchTelemetryOutcome(status); got != want {
			t.Fatalf("dispatchTelemetryOutcome(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestServiceExecuteSequencesAuthorizedIdempotentMutation(t *testing.T) {
	t.Parallel()

	command := validCommand()
	completedAt := command.RequestedAt.Add(time.Second)
	journal := &journalStub{created: true}
	dispatcher := &dispatcherStub{}
	authorizer := &authorizerStub{}
	service := NewService(authorizer, journal, dispatcher, func() time.Time { return completedAt })

	result, err := service.Execute(context.Background(), command)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.TenantID != command.TenantID ||
		result.CommandID == "" || result.CommandID == command.IdempotencyKey ||
		result.Status != controlplane.CommandSucceeded ||
		result.CompletedAt != completedAt {
		t.Fatalf("Execute() result = %+v, want succeeded at %s", result, completedAt)
	}
	if journal.command.CommandID != result.CommandID ||
		dispatcher.command.CommandID != result.CommandID ||
		journal.completed.CommandID != result.CommandID {
		t.Fatalf(
			"command IDs = accept %q dispatch %q result %q complete %q",
			journal.command.CommandID,
			dispatcher.command.CommandID,
			result.CommandID,
			journal.completed.CommandID,
		)
	}
	if journal.command.Deadline != command.RequestedAt.Add(controlplane.DefaultCommandLifetime) ||
		dispatcher.command.Deadline != journal.command.Deadline {
		t.Fatalf(
			"deadlines = journal:%s dispatch:%s",
			journal.command.Deadline, dispatcher.command.Deadline,
		)
	}
	if authorizer.calls != 1 ||
		authorizer.tenant != command.TenantID ||
		authorizer.permission != controlplane.PermissionDrain {
		t.Fatalf(
			"authorization = %d calls for tenant %q permission %q",
			authorizer.calls,
			authorizer.tenant,
			authorizer.permission,
		)
	}
	if journal.acceptCalls != 1 || journal.dispatchCalls != 1 ||
		journal.acknowledgeCalls != 1 || journal.completeCalls != 1 || dispatcher.calls != 1 {
		t.Fatalf(
			"calls = accept:%d mark-dispatched:%d dispatch:%d acknowledge:%d complete:%d",
			journal.acceptCalls,
			journal.dispatchCalls,
			dispatcher.calls,
			journal.acknowledgeCalls,
			journal.completeCalls,
		)
	}
	if journal.dispatched.Status != controlplane.CommandDispatched ||
		journal.acknowledged.Status != controlplane.CommandAcknowledged {
		t.Fatalf(
			"transitions = dispatched %+v acknowledged %+v",
			journal.dispatched,
			journal.acknowledged,
		)
	}
}

func TestServiceExecuteFailsBeforeAuthorizationWhenCommandIDIsUnavailable(t *testing.T) {
	t.Parallel()

	want := errors.New("entropy unavailable")
	authorizer := &authorizerStub{}
	journal := &journalStub{}
	service := NewService(authorizer, journal, &dispatcherStub{}, time.Now)
	service.newCommandID = func() (string, error) { return "", want }

	result, err := service.Execute(context.Background(), validCommand())
	if result != (controlplane.CommandResult{}) ||
		!errors.Is(err, ErrCommandIDUnavailable) || !errors.Is(err, want) {
		t.Fatalf("Execute() = (%+v, %v), want command ID failure", result, err)
	}
	if authorizer.calls != 0 || journal.acceptCalls != 0 {
		t.Fatalf("calls = authorize %d accept %d, want 0:0", authorizer.calls, journal.acceptCalls)
	}
}

func TestServiceExecuteFailsClosedWithoutLifecycleJournal(t *testing.T) {
	t.Parallel()

	authorizer := &authorizerStub{}
	dispatcher := &dispatcherStub{}
	service := NewService(authorizer, legacyJournalStub{}, dispatcher, time.Now)

	result, err := service.Execute(context.Background(), validCommand())
	if result != (controlplane.CommandResult{}) || !errors.Is(err, ErrLifecycleJournalUnavailable) {
		t.Fatalf("Execute() = (%+v, %v), want lifecycle journal failure", result, err)
	}
	if authorizer.calls != 0 || dispatcher.calls != 0 {
		t.Fatalf("calls = authorize:%d dispatch:%d, want 0:0", authorizer.calls, dispatcher.calls)
	}
}

func TestServiceExecuteAuthorizesReplaySourceAndDestination(t *testing.T) {
	t.Parallel()

	replay := validCommand(func(command *controlplane.Command) {
		command.Action = controlplane.ActionReplay
		command.Target = controlplane.Target{
			Kind: controlplane.TargetFailure, Name: "failure-1",
		}
		command.Confirmed = true
		command.Replay = &controlplane.Replay{
			Destination:       "recovery",
			IdempotencyPolicy: controlplane.ReplayRejectDuplicate,
		}
	})
	destinationDenied := errors.New("destination denied")
	for name, test := range map[string]struct {
		authorizer *authorizerStub
		wantErr    error
		wantAccept int
	}{
		"both resources allowed": {
			authorizer: &authorizerStub{},
			wantAccept: 1,
		},
		"destination denied": {
			authorizer: &authorizerStub{errs: []error{nil, destinationDenied}},
			wantErr:    destinationDenied,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			journal := &journalStub{created: true}
			service := NewService(test.authorizer, journal, &dispatcherStub{}, time.Now)

			_, err := service.Execute(context.Background(), replay)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Execute() error = %v, want %v", err, test.wantErr)
			}
			wantTargets := []controlplane.Target{
				replay.Target,
				{Kind: controlplane.TargetQueue, Name: replay.Replay.Destination},
			}
			if !reflect.DeepEqual(test.authorizer.targets, wantTargets) {
				t.Fatalf("authorized targets = %+v, want %+v", test.authorizer.targets, wantTargets)
			}
			if journal.acceptCalls != test.wantAccept {
				t.Fatalf("Accept() calls = %d, want %d", journal.acceptCalls, test.wantAccept)
			}
		})
	}
}

func TestServiceExecuteAuthorizesEveryAdministrativeAction(t *testing.T) {
	t.Parallel()

	denied := errors.New("denied")
	tests := map[controlplane.Action]controlplane.Command{
		controlplane.ActionPause: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionPause
			command.Target.Kind = controlplane.TargetQueue
		}),
		controlplane.ActionResume: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionResume
			command.Target.Kind = controlplane.TargetQueue
		}),
		controlplane.ActionDrain: validCommand(),
		controlplane.ActionTerminate: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionTerminate
			command.Target.Kind = controlplane.TargetWorker
		}),
		controlplane.ActionRetry: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionRetry
			command.Target.Kind = controlplane.TargetFailure
		}),
		controlplane.ActionBulkRetry: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionBulkRetry
			command.Target.Kind = controlplane.TargetFailure
			command.Confirmed = true
			command.Selection = &controlplane.Selection{Limit: 100}
		}),
		controlplane.ActionDelete: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionDelete
			command.Target.Kind = controlplane.TargetFailure
		}),
		controlplane.ActionPurge: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionPurge
			command.Target.Kind = controlplane.TargetQueue
			command.Confirmed = true
		}),
		controlplane.ActionReplay: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionReplay
			command.Target.Kind = controlplane.TargetFailure
			command.Confirmed = true
			command.Replay = &controlplane.Replay{
				Destination:       "recovery",
				IdempotencyPolicy: controlplane.ReplayRejectDuplicate,
			}
		}),
		controlplane.ActionScale: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionScale
			command.Target.Kind = controlplane.TargetWorkload
			command.Scale = &controlplane.Scale{Replicas: 3}
		}),
	}
	for action, command := range tests {
		t.Run(string(action), func(t *testing.T) {
			t.Parallel()

			authorizer := &authorizerStub{err: denied}
			journal := &journalStub{created: true}
			dispatcher := &dispatcherStub{}
			service := NewService(authorizer, journal, dispatcher, time.Now)

			_, err := service.Execute(context.Background(), command)
			if !errors.Is(err, denied) {
				t.Fatalf("Execute() error = %v, want %v", err, denied)
			}
			if authorizer.calls != 1 ||
				authorizer.permission != controlplane.Permission(action) ||
				!reflect.DeepEqual(authorizer.targets, []controlplane.Target{command.Target}) {
				t.Fatalf(
					"authorization = %d calls, permission %q, targets %+v",
					authorizer.calls,
					authorizer.permission,
					authorizer.targets,
				)
			}
			if journal.acceptCalls != 0 || dispatcher.calls != 0 {
				t.Fatalf(
					"calls after denial = accept:%d dispatch:%d, want 0:0",
					journal.acceptCalls,
					dispatcher.calls,
				)
			}
		})
	}
}

func TestServiceExecuteReturnsDuplicateWithoutDispatch(t *testing.T) {
	t.Parallel()

	stored := controlplane.CommandResult{
		IdempotencyKey: "request-123",
		TenantID:       "tenant-1",
		Status:         controlplane.CommandSucceeded,
	}
	journal := &journalStub{accepted: stored, created: false}
	dispatcher := &dispatcherStub{}
	service := NewService(&authorizerStub{}, journal, dispatcher, time.Now)

	result, err := service.Execute(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result != stored {
		t.Fatalf("Execute() result = %+v, want stored %+v", result, stored)
	}
	if dispatcher.calls != 0 || journal.completeCalls != 0 {
		t.Fatalf("duplicate calls = dispatch:%d complete:%d, want 0:0", dispatcher.calls, journal.completeCalls)
	}
}

func TestServiceExecuteDeduplicatesEverySensitiveMutationBeforeDispatch(t *testing.T) {
	t.Parallel()

	tests := map[controlplane.Action]controlplane.Command{
		controlplane.ActionRetry: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionRetry
			command.Target = controlplane.Target{Kind: controlplane.TargetFailure, Name: "failure-1"}
		}),
		controlplane.ActionReplay: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionReplay
			command.Target = controlplane.Target{Kind: controlplane.TargetFailure, Name: "failure-1"}
			command.Confirmed = true
			command.Replay = &controlplane.Replay{
				Destination: "recovery", IdempotencyPolicy: controlplane.ReplayRejectDuplicate,
			}
		}),
		controlplane.ActionDelete: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionDelete
			command.Target = controlplane.Target{Kind: controlplane.TargetFailure, Name: "failure-1"}
		}),
		controlplane.ActionPurge: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionPurge
			command.Target = controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"}
			command.Confirmed = true
		}),
		controlplane.ActionPause: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionPause
			command.Target = controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"}
		}),
		controlplane.ActionResume: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionResume
			command.Target = controlplane.Target{Kind: controlplane.TargetQueue, Name: "critical"}
		}),
		controlplane.ActionDrain: validCommand(),
		controlplane.ActionScale: validCommand(func(command *controlplane.Command) {
			command.Action = controlplane.ActionScale
			command.Target = controlplane.Target{Kind: controlplane.TargetWorkload, Name: "workers"}
			command.Scale = &controlplane.Scale{Replicas: 3}
		}),
	}
	for action, command := range tests {
		t.Run(string(action), func(t *testing.T) {
			t.Parallel()

			stored := controlplane.CommandResult{
				IdempotencyKey: command.IdempotencyKey,
				TenantID:       command.TenantID,
				Status:         controlplane.CommandSucceeded,
			}
			journal := &journalStub{accepted: stored}
			dispatcher := &dispatcherStub{}
			service := NewService(&authorizerStub{}, journal, dispatcher, time.Now)

			result, err := service.Execute(context.Background(), command)
			if err != nil || result != stored {
				t.Fatalf("Execute(duplicate) = (%+v, %v), want stored result", result, err)
			}
			if journal.acceptCalls != 1 || journal.completeCalls != 0 || dispatcher.calls != 0 {
				t.Fatalf(
					"calls = accept:%d complete:%d dispatch:%d, want 1:0:0",
					journal.acceptCalls, journal.completeCalls, dispatcher.calls,
				)
			}
		})
	}
}

func TestServiceExecuteFailsClosedBeforeDispatch(t *testing.T) {
	t.Parallel()

	denied := errors.New("denied")
	auditUnavailable := errors.New("audit unavailable")

	tests := map[string]struct {
		command       controlplane.Command
		authorizerErr error
		journalErr    error
		wantErr       error
		wantAuthCalls int
		wantAccept    int
	}{
		"invalid command": {
			command:       validCommand(func(command *controlplane.Command) { command.Actor = "" }),
			wantAuthCalls: 0,
			wantAccept:    0,
		},
		"authorization denied": {
			command:       validCommand(),
			authorizerErr: denied,
			wantErr:       denied,
			wantAuthCalls: 1,
			wantAccept:    0,
		},
		"accept audit unavailable": {
			command:       validCommand(),
			journalErr:    auditUnavailable,
			wantErr:       auditUnavailable,
			wantAuthCalls: 1,
			wantAccept:    1,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			authorizer := &authorizerStub{err: tt.authorizerErr}
			journal := &journalStub{acceptErr: tt.journalErr}
			dispatcher := &dispatcherStub{}
			service := NewService(authorizer, journal, dispatcher, time.Now)

			_, err := service.Execute(context.Background(), tt.command)
			if tt.wantErr == nil {
				var validationError *controlplane.ValidationError
				if !errors.As(err, &validationError) {
					t.Fatalf("Execute() error = %v, want ValidationError", err)
				}
			} else if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Execute() error = %v, want %v", err, tt.wantErr)
			}
			if authorizer.calls != tt.wantAuthCalls || journal.acceptCalls != tt.wantAccept {
				t.Fatalf(
					"calls = authorize:%d accept:%d, want %d:%d",
					authorizer.calls,
					journal.acceptCalls,
					tt.wantAuthCalls,
					tt.wantAccept,
				)
			}
			if dispatcher.calls != 0 || journal.completeCalls != 0 {
				t.Fatalf("fail-closed calls = dispatch:%d complete:%d, want 0:0", dispatcher.calls, journal.completeCalls)
			}
		})
	}
}

func TestServiceExecuteRecordsDispatchFailure(t *testing.T) {
	t.Parallel()

	dispatchErr := errors.New("worker disconnected")
	journal := &journalStub{created: true}
	service := NewService(
		&authorizerStub{},
		journal,
		&dispatcherStub{err: dispatchErr},
		time.Now,
	)

	result, err := service.Execute(context.Background(), validCommand())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != controlplane.CommandFailed || result.Failure != controlplane.FailureDispatch {
		t.Fatalf("Execute() result = %+v, want recorded dispatch failure", result)
	}
	if journal.completed != result {
		t.Fatalf("Complete() result = %+v, want %+v", journal.completed, result)
	}
}

func TestServiceExecuteDoesNotDispatchWhenDispatchBoundaryCannotPersist(t *testing.T) {
	t.Parallel()

	want := errors.New("dispatch boundary unavailable")
	journal := &journalStub{created: true, dispatchErr: want}
	dispatcher := &dispatcherStub{}
	service := NewService(&authorizerStub{}, journal, dispatcher, time.Now)

	result, err := service.Execute(context.Background(), validCommand())
	if result != (controlplane.CommandResult{}) || !errors.Is(err, want) {
		t.Fatalf("Execute() = (%+v, %v), want pre-dispatch persistence failure", result, err)
	}
	if dispatcher.calls != 0 || journal.acknowledgeCalls != 0 || journal.completeCalls != 0 {
		t.Fatalf(
			"calls = dispatch:%d acknowledge:%d complete:%d, want 0:0:0",
			dispatcher.calls, journal.acknowledgeCalls, journal.completeCalls,
		)
	}
}

func TestServiceExecuteLeavesDispatchedWhenAcknowledgementCannotPersist(t *testing.T) {
	t.Parallel()

	want := errors.New("acknowledgement boundary unavailable")
	journal := &journalStub{created: true, acknowledgeErr: want}
	service := NewService(&authorizerStub{}, journal, &dispatcherStub{}, time.Now)

	result, err := service.Execute(context.Background(), validCommand())
	if !errors.Is(err, ErrOutcomeUnknown) || !errors.Is(err, want) ||
		result.Status != controlplane.CommandUnknown ||
		result.Failure != controlplane.FailureOutcomeUnknown {
		t.Fatalf("Execute() = (%+v, %v), want unknown acknowledgement", result, err)
	}
	if journal.dispatchCalls != 1 || journal.acknowledgeCalls != 1 || journal.completeCalls != 0 {
		t.Fatalf(
			"calls = dispatched:%d acknowledge:%d complete:%d, want 1:1:0",
			journal.dispatchCalls, journal.acknowledgeCalls, journal.completeCalls,
		)
	}
}

func TestServiceExecuteCancelsOnlyBeforeDispatch(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	journal := &journalStub{created: true}
	dispatcher := &dispatcherStub{}
	service := NewService(&authorizerStub{}, journal, dispatcher, time.Now)

	result, err := service.Execute(ctx, validCommand())
	if !errors.Is(err, context.Canceled) || result.Status != controlplane.CommandCanceled ||
		result.Failure != controlplane.FailureCanceled {
		t.Fatalf("Execute() = (%+v, %v), want pre-dispatch cancellation", result, err)
	}
	if dispatcher.calls != 0 || journal.dispatchCalls != 0 || journal.completeCalls != 1 {
		t.Fatalf(
			"calls = dispatch:%d mark-dispatched:%d complete:%d, want 0:0:1",
			dispatcher.calls, journal.dispatchCalls, journal.completeCalls,
		)
	}
}

func TestServiceExecuteReportsCancellationPersistenceFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("cancellation commit failed")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	journal := &journalStub{created: true, completeErr: want}
	service := NewService(&authorizerStub{}, journal, &dispatcherStub{}, time.Now)

	result, err := service.Execute(ctx, validCommand())
	if result.Status != controlplane.CommandCanceled ||
		!errors.Is(err, context.Canceled) || !errors.Is(err, want) {
		t.Fatalf("Execute() = (%+v, %v), want joined cancellation failure", result, err)
	}
}

func TestDispatchTelemetryRejectsNonterminalLifecycleStates(t *testing.T) {
	t.Parallel()

	for _, status := range []controlplane.CommandStatus{
		controlplane.CommandPending,
		controlplane.CommandDispatched,
		controlplane.CommandAcknowledged,
		controlplane.CommandCanceled,
	} {
		if got := dispatchTelemetryOutcome(status); got != "invalid" {
			t.Fatalf("dispatchTelemetryOutcome(%q) = %q, want invalid", status, got)
		}
	}
}

func TestServiceExecutePersistsStructuredDataPlaneOutcomes(t *testing.T) {
	t.Parallel()

	command := validCommand()
	completedAt := command.RequestedAt.Add(time.Second)
	tests := map[string]DispatchOutcome{
		"acknowledged": {
			Status:      controlplane.CommandSucceeded,
			CompletedAt: completedAt,
		},
		"rejected": {
			Status:      controlplane.CommandFailed,
			Failure:     "unsupported",
			CompletedAt: completedAt,
		},
		"unknown": {
			Status:      controlplane.CommandUnknown,
			Failure:     controlplane.FailureOutcomeUnknown,
			CompletedAt: completedAt,
		},
	}
	for name, outcome := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			journal := &journalStub{created: true}
			dispatcher := &resultDispatcherStub{outcome: outcome}
			service := NewService(&authorizerStub{}, journal, dispatcher, time.Now)
			result, err := service.Execute(context.Background(), command)
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if result.Status != outcome.Status || result.Failure != outcome.Failure ||
				result.CompletedAt != completedAt || journal.completed != result {
				t.Fatalf("Execute() result = %+v, outcome = %+v", result, outcome)
			}
			if dispatcher.resultCalls != 1 || dispatcher.legacyCalls != 0 {
				t.Fatalf("dispatch calls = result:%d legacy:%d", dispatcher.resultCalls, dispatcher.legacyCalls)
			}
		})
	}
}

func TestServiceExecuteFailsSafeForInvalidOrUnavailableStructuredOutcome(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("management transport unavailable")
	tests := map[string]struct {
		dispatcher *resultDispatcherStub
		status     controlplane.CommandStatus
		failure    string
	}{
		"invalid": {
			dispatcher: &resultDispatcherStub{outcome: DispatchOutcome{Status: controlplane.CommandSucceeded}},
			status:     controlplane.CommandUnknown,
			failure:    controlplane.FailureInvalidDispatchResult,
		},
		"unavailable": {
			dispatcher: &resultDispatcherStub{err: transportErr},
			status:     controlplane.CommandFailed,
			failure:    controlplane.FailureDispatch,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			journal := &journalStub{created: true}
			service := NewService(&authorizerStub{}, journal, tt.dispatcher, time.Now)
			result, err := service.Execute(context.Background(), validCommand())
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if result.Status != tt.status || result.Failure != tt.failure || journal.completed != result {
				t.Fatalf("Execute() result = %+v, want %q/%q", result, tt.status, tt.failure)
			}
		})
	}
}

func TestServiceExecuteExposesUnknownOutcomeWhenCompletionFails(t *testing.T) {
	t.Parallel()

	completionErr := errors.New("commit failed")
	journal := &journalStub{created: true, completeErr: completionErr}
	service := NewService(&authorizerStub{}, journal, &dispatcherStub{}, time.Now)

	result, err := service.Execute(context.Background(), validCommand())
	if !errors.Is(err, ErrOutcomeUnknown) || !errors.Is(err, completionErr) {
		t.Fatalf("Execute() error = %v, want outcome unknown and completion failure", err)
	}
	if result.Status != controlplane.CommandUnknown {
		t.Fatalf("Execute() status = %q, want %q", result.Status, controlplane.CommandUnknown)
	}
}

func validCommand(mutators ...func(*controlplane.Command)) controlplane.Command {
	command := controlplane.Command{
		IdempotencyKey: "request-123",
		TenantID:       "tenant-1",
		Actor:          "operator@example.test",
		Reason:         "Drain workers before the deployment",
		Action:         controlplane.ActionDrain,
		Target:         controlplane.Target{Kind: controlplane.TargetWorkerGroup, Name: "payments"},
		RequestedAt:    time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
	}
	for _, mutate := range mutators {
		mutate(&command)
	}

	return command
}

type authorizerStub struct {
	err        error
	errs       []error
	calls      int
	tenant     string
	permission controlplane.Permission
	targets    []controlplane.Target
}

func (s *authorizerStub) Authorize(
	_ context.Context,
	tenant string,
	_ string,
	permission controlplane.Permission,
	target controlplane.Target,
) error {
	s.calls++
	s.tenant = tenant
	s.permission = permission
	s.targets = append(s.targets, target)
	if len(s.errs) >= s.calls {
		return s.errs[s.calls-1]
	}

	return s.err
}

type journalStub struct {
	command          controlplane.Command
	accepted         controlplane.CommandResult
	completed        controlplane.CommandResult
	dispatched       controlplane.CommandResult
	acknowledged     controlplane.CommandResult
	created          bool
	acceptErr        error
	completeErr      error
	dispatchErr      error
	acknowledgeErr   error
	acceptCalls      int
	completeCalls    int
	dispatchCalls    int
	acknowledgeCalls int
}

type legacyJournalStub struct{}

func (legacyJournalStub) Accept(
	context.Context,
	controlplane.Command,
) (controlplane.CommandResult, bool, error) {
	return controlplane.CommandResult{}, false, nil
}

func (legacyJournalStub) Complete(context.Context, controlplane.CommandResult) error {
	return nil
}

func (s *journalStub) Accept(
	_ context.Context,
	command controlplane.Command,
) (controlplane.CommandResult, bool, error) {
	s.acceptCalls++
	s.command = command

	return s.accepted, s.created, s.acceptErr
}

func (s *journalStub) Complete(_ context.Context, result controlplane.CommandResult) error {
	s.completeCalls++
	s.completed = result

	return s.completeErr
}

func (s *journalStub) MarkDispatched(
	_ context.Context,
	result controlplane.CommandResult,
) error {
	s.dispatchCalls++
	s.dispatched = result

	return s.dispatchErr
}

func (s *journalStub) MarkAcknowledged(
	_ context.Context,
	result controlplane.CommandResult,
) error {
	s.acknowledgeCalls++
	s.acknowledged = result

	return s.acknowledgeErr
}

type dispatcherStub struct {
	err     error
	calls   int
	command controlplane.Command
}

type resultDispatcherStub struct {
	outcome     DispatchOutcome
	err         error
	resultCalls int
	legacyCalls int
}

func (s *resultDispatcherStub) Dispatch(context.Context, controlplane.Command) error {
	s.legacyCalls++

	return errors.New("legacy dispatch must not be used")
}

func (s *resultDispatcherStub) DispatchResult(
	context.Context,
	controlplane.Command,
) (DispatchOutcome, error) {
	s.resultCalls++

	return s.outcome, s.err
}

type failingMeter struct {
	otelmetric.Meter
	counterErr   error
	histogramErr error
}

func (m failingMeter) Int64Counter(name string, options ...otelmetric.Int64CounterOption) (otelmetric.Int64Counter, error) {
	if m.counterErr != nil {
		return nil, m.counterErr
	}
	return m.Meter.Int64Counter(name, options...)
}

func (m failingMeter) Float64Histogram(name string, options ...otelmetric.Float64HistogramOption) (otelmetric.Float64Histogram, error) {
	if m.histogramErr != nil {
		return nil, m.histogramErr
	}
	return m.Meter.Float64Histogram(name, options...)
}

func (s *dispatcherStub) Dispatch(_ context.Context, command controlplane.Command) error {
	s.calls++
	s.command = command

	return s.err
}
