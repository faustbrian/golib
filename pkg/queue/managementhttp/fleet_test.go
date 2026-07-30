package managementhttp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
)

func TestFleetClientForwardsSharedRecordsAndReportsPartialFanout(t *testing.T) {
	t.Parallel()

	record := validManagementRecord(management.RecordFailure, management.PayloadHidden)
	reader := &recordReaderHTTPStub{
		failures:    management.RecordPage{Items: []management.JobRecord{record}},
		deadLetters: management.RecordPage{},
		record:      record,
	}
	recordHandler, err := NewHandler(HandlerConfig{
		Token: "fleet-secret", Records: reader,
	})
	if err != nil {
		t.Fatalf("NewHandler(records) error = %v", err)
	}
	recordServer := httptest.NewServer(recordHandler)
	t.Cleanup(recordServer.Close)
	fleet, err := NewFleetClient(FleetClientConfig{
		Resolver: EndpointResolverFunc(func(context.Context) ([]Endpoint, error) {
			return []Endpoint{{ID: "records", BaseURL: recordServer.URL}}, nil
		}),
		Token: "fleet-secret",
	})
	if err != nil {
		t.Fatalf("NewFleetClient(records) error = %v", err)
	}
	pageRequest := validRecordPageRequest()
	failures, err := fleet.ListFailures(context.Background(), pageRequest)
	if err != nil || !reflect.DeepEqual(failures, reader.failures) {
		t.Fatalf("ListFailures() = (%+v, %v)", failures, err)
	}
	deadLetters, err := fleet.ListDeadLetters(context.Background(), pageRequest)
	if err != nil || len(deadLetters.Items) != 0 || deadLetters.NextCursor != "" {
		t.Fatalf("ListDeadLetters() = (%+v, %v)", deadLetters, err)
	}
	inspected, err := fleet.Inspect(context.Background(), management.InspectRequest{
		Kind: management.RecordFailure, ID: record.ID,
	})
	if err != nil || !reflect.DeepEqual(inspected, record) {
		t.Fatalf("Inspect() = (%+v, %v)", inspected, err)
	}

	workingController := &controllerStub{}
	failingController := &controllerStub{err: errors.New("remote failure")}
	working := newFleetWorkerServer(t, "worker-a", workingController)
	failing := newFleetWorkerServer(t, "worker-b", failingController)
	partial, err := NewFleetClient(FleetClientConfig{
		Resolver: EndpointResolverFunc(func(context.Context) ([]Endpoint, error) {
			return []Endpoint{
				{ID: "working", BaseURL: working.URL},
				{ID: "failing", BaseURL: failing.URL},
			}, nil
		}),
		Token: "fleet-secret",
	})
	if err != nil {
		t.Fatalf("NewFleetClient(partial) error = %v", err)
	}
	command := validCommand()
	command.Action = management.CommandPause
	command.Target = management.Target{Kind: management.TargetQueue, Name: "critical"}
	result, err := partial.Execute(context.Background(), command)
	if err != nil || result.Status != management.CommandPartial ||
		result.FailureCode != "fleet_partial" {
		t.Fatalf("Execute(partial) = (%+v, %v)", result, err)
	}
	workingController.err = errors.New("remote failure")
	if _, err = partial.Execute(context.Background(), command); !errors.Is(
		err,
		ErrFleetUnavailable,
	) {
		t.Fatalf("Execute(unavailable) error = %v", err)
	}
	workingController.err = nil
	failingController.err = nil
	failingController.execute = func(
		_ context.Context,
		command management.Command,
	) (management.CommandResult, error) {
		return management.CommandResult{
			CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
			WorkerID: "worker-b", Protocol: command.Protocol,
			Status: management.CommandRejected, FailureCode: "already_draining",
			CompletedAt: time.Date(2026, 7, 16, 10, 0, 2, 0, time.UTC),
		}, nil
	}
	result, err = partial.Execute(context.Background(), command)
	if err != nil || result.Status != management.CommandPartial ||
		result.FailureCode != "fleet_partial" {
		t.Fatalf("Execute(mixed fleet) = (%+v, %v)", result, err)
	}

	rejectedController := &controllerStub{}
	rejected := newFleetWorkerServer(t, "worker-c", rejectedController)
	rejectedController.execute = func(
		_ context.Context,
		command management.Command,
	) (management.CommandResult, error) {
		return management.CommandResult{
			CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
			WorkerID: "worker-c", Protocol: command.Protocol,
			Status: management.CommandRejected, FailureCode: "already_draining",
			CompletedAt: time.Date(2026, 7, 16, 10, 0, 2, 0, time.UTC),
		}, nil
	}
	rejectedFleet := mustFleetClient(t, []Endpoint{{ID: "rejected", BaseURL: rejected.URL}})
	result, err = rejectedFleet.Execute(context.Background(), command)
	if err != nil || result.Status != management.CommandRejected ||
		result.FailureCode != "already_draining" {
		t.Fatalf("Execute(rejected fleet) = (%+v, %v)", result, err)
	}
}

func TestFleetClientAggregatesStatusAndFansOutQueueLifecycle(t *testing.T) {
	t.Parallel()

	firstController := &controllerStub{}
	secondController := &controllerStub{}
	first := newFleetWorkerServer(t, "worker-b", firstController)
	second := newFleetWorkerServer(t, "worker-a", secondController)
	resolver := EndpointResolverFunc(func(context.Context) ([]Endpoint, error) {
		return []Endpoint{
			{ID: "endpoint-b", BaseURL: first.URL},
			{ID: "endpoint-a", BaseURL: second.URL},
		}, nil
	})
	fleet, err := NewFleetClient(FleetClientConfig{
		Resolver: resolver, Token: "fleet-secret",
	})
	if err != nil {
		t.Fatalf("NewFleetClient() error = %v", err)
	}
	workers, err := fleet.ListWorkers(
		context.Background(),
		management.StatusPageRequest{Limit: 10},
	)
	if err != nil || len(workers.Items) != 2 ||
		workers.Items[0].ID != "worker-a" || workers.Items[1].ID != "worker-b" {
		t.Fatalf("ListWorkers() = (%+v, %v)", workers, err)
	}
	queues, err := fleet.ListQueues(
		context.Background(),
		management.StatusPageRequest{Limit: 10},
	)
	if err != nil || len(queues.Items) != 1 || queues.Items[0].Queue != "critical" {
		t.Fatalf("ListQueues() = (%+v, %v)", queues, err)
	}

	command := validCommand()
	command.Action = management.CommandPause
	command.Target = management.Target{
		Kind: management.TargetQueue, Name: "critical",
	}
	result, err := fleet.Execute(context.Background(), command)
	if err != nil || result.Status != management.CommandAcknowledged ||
		result.WorkerID != "fleet" || firstController.calls != 1 ||
		secondController.calls != 1 {
		t.Fatalf(
			"Execute(queue pause) = (%+v, %v), calls = %d/%d",
			result,
			err,
			firstController.calls,
			secondController.calls,
		)
	}
	retry := validCommand()
	retry.Action = management.CommandRetry
	retry.Target = management.Target{Kind: management.TargetFailure, Name: "failure-1"}
	result, err = fleet.Execute(context.Background(), retry)
	if err != nil || result.WorkerID != "worker-a" {
		t.Fatalf("Execute(failure retry) = (%+v, %v)", result, err)
	}
}

func TestFleetClientRoutesWorkerLifecycleAndFailsClosed(t *testing.T) {
	t.Parallel()

	firstController := &controllerStub{}
	secondController := &controllerStub{}
	first := newFleetWorkerServer(t, "worker-a", firstController)
	second := newFleetWorkerServer(t, "worker-b", secondController)
	resolver := EndpointResolverFunc(func(context.Context) ([]Endpoint, error) {
		return []Endpoint{
			{ID: "endpoint-a", BaseURL: first.URL},
			{ID: "endpoint-b", BaseURL: second.URL},
		}, nil
	})
	fleet, err := NewFleetClient(FleetClientConfig{
		Resolver: resolver, Token: "fleet-secret",
	})
	if err != nil {
		t.Fatalf("NewFleetClient() error = %v", err)
	}
	command := validCommand()
	command.Target.Name = "worker-b"
	result, err := fleet.Execute(context.Background(), command)
	if err != nil || result.WorkerID != "worker-b" ||
		firstController.calls != 0 || secondController.calls != 1 {
		t.Fatalf(
			"Execute(worker drain) = (%+v, %v), calls = %d/%d",
			result,
			err,
			firstController.calls,
			secondController.calls,
		)
	}

	missing := command
	missing.ID = "command-2"
	missing.IdempotencyKey = "request-2"
	missing.Target.Name = "missing-worker"
	if _, err = fleet.Execute(context.Background(), missing); !errors.Is(
		err,
		ErrFleetTargetUnavailable,
	) {
		t.Fatalf("Execute(missing worker) error = %v", err)
	}

	invalid, err := NewFleetClient(FleetClientConfig{})
	if invalid != nil || !errors.Is(err, ErrInvalidFleetConfiguration) {
		t.Fatalf("NewFleetClient(empty) = (%v, %v)", invalid, err)
	}
	for name, token := range map[string]string{
		"control character": "fleet\nsecret",
		"oversized":         strings.Repeat("x", maxTokenBytes+1),
	} {
		invalid, err = NewFleetClient(FleetClientConfig{
			Resolver: EndpointResolverFunc(func(context.Context) ([]Endpoint, error) {
				return []Endpoint{{ID: "valid", BaseURL: first.URL}}, nil
			}),
			Token: token,
		})
		if invalid != nil || !errors.Is(err, ErrInvalidFleetConfiguration) {
			t.Fatalf("NewFleetClient(%s token) = (%v, %v)", name, invalid, err)
		}
	}
	duplicate := EndpointResolverFunc(func(context.Context) ([]Endpoint, error) {
		return []Endpoint{{ID: "same", BaseURL: first.URL}, {ID: "same", BaseURL: second.URL}}, nil
	})
	invalid, err = NewFleetClient(FleetClientConfig{
		Resolver: duplicate, Token: "fleet-secret",
	})
	if err != nil {
		t.Fatalf("NewFleetClient(duplicate resolver) error = %v", err)
	}
	if _, err = invalid.ListWorkers(
		context.Background(),
		management.StatusPageRequest{Limit: 1},
	); !errors.Is(err, ErrInvalidFleetEndpoints) {
		t.Fatalf("ListWorkers(duplicate endpoints) error = %v", err)
	}

	duplicateWorkerFirst := &controllerStub{}
	duplicateWorkerSecond := &controllerStub{}
	duplicateWorkerFleet := mustFleetClient(t, []Endpoint{
		{ID: "first", BaseURL: newFleetWorkerServer(t, "duplicate", duplicateWorkerFirst).URL},
		{ID: "second", BaseURL: newFleetWorkerServer(t, "duplicate", duplicateWorkerSecond).URL},
	})
	duplicateWorkerCommand := validCommand()
	duplicateWorkerCommand.Target.Name = "duplicate"
	if _, err = duplicateWorkerFleet.Execute(
		context.Background(),
		duplicateWorkerCommand,
	); !errors.Is(err, ErrInvalidFleetEndpoints) {
		t.Fatalf("Execute(duplicate worker) error = %v", err)
	}
	if duplicateWorkerFirst.calls != 0 || duplicateWorkerSecond.calls != 0 {
		t.Fatalf(
			"duplicate worker command calls = %d/%d, want 0/0",
			duplicateWorkerFirst.calls,
			duplicateWorkerSecond.calls,
		)
	}
}

func TestFleetClientPreservesCancellationDuringFanout(t *testing.T) {
	t.Parallel()

	requestStarted := make(chan struct{})
	httpClient := &http.Client{Transport: roundTripFunc(func(
		request *http.Request,
	) (*http.Response, error) {
		close(requestStarted)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	fleet, err := NewFleetClient(FleetClientConfig{
		Resolver: EndpointResolverFunc(func(context.Context) ([]Endpoint, error) {
			return []Endpoint{{ID: "blocked", BaseURL: "https://worker.example"}}, nil
		}),
		Token: "fleet-secret", HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatalf("NewFleetClient() error = %v", err)
	}
	command := validCommand()
	command.Action = management.CommandPause
	command.Target = management.Target{Kind: management.TargetQueue, Name: "critical"}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, executeErr := fleet.Execute(ctx, command)
		result <- executeErr
	}()
	<-requestStarted
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(canceled fanout) error = %v", err)
	}
}

func TestFleetClientContainsResolverAndEndpointFailures(t *testing.T) {
	t.Parallel()

	validController := &controllerStub{}
	validServer := newFleetWorkerServer(t, "worker-a", validController)
	resolverFailure := EndpointResolverFunc(func(context.Context) ([]Endpoint, error) {
		return nil, errors.New("resolver failure")
	})
	failedFleet, err := NewFleetClient(FleetClientConfig{
		Resolver: resolverFailure, Token: "fleet-secret",
	})
	if err != nil {
		t.Fatalf("NewFleetClient(failing resolver) error = %v", err)
	}
	for name, invoke := range map[string]func() error{
		"workers": func() error {
			_, invokeErr := failedFleet.ListWorkers(context.Background(), management.StatusPageRequest{Limit: 1})
			return invokeErr
		},
		"queues": func() error {
			_, invokeErr := failedFleet.ListQueues(context.Background(), management.StatusPageRequest{Limit: 1})
			return invokeErr
		},
		"execute": func() error {
			_, invokeErr := failedFleet.Execute(context.Background(), validCommand())
			return invokeErr
		},
		"failures": func() error {
			_, invokeErr := failedFleet.ListFailures(context.Background(), validRecordPageRequest())
			return invokeErr
		},
		"dead letters": func() error {
			_, invokeErr := failedFleet.ListDeadLetters(context.Background(), validRecordPageRequest())
			return invokeErr
		},
		"inspect": func() error {
			_, invokeErr := failedFleet.Inspect(context.Background(), management.InspectRequest{
				Kind: management.RecordFailure, ID: "record-1",
			})
			return invokeErr
		},
	} {
		if invokeErr := invoke(); !errors.Is(invokeErr, ErrFleetUnavailable) {
			t.Fatalf("%s error = %v", name, invokeErr)
		}
	}
	//lint:ignore SA1012 Public boundary must reject a nil context safely.
	//nolint:staticcheck // Public boundary must reject a nil context safely.
	if _, err = failedFleet.Execute(nil, validCommand()); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Execute(nil) error = %v", err)
	}
	invalidCommand := validCommand()
	invalidCommand.ID = ""
	if _, err = failedFleet.Execute(context.Background(), invalidCommand); !errors.Is(
		err,
		ErrInvalidRequest,
	) {
		t.Fatalf("Execute(invalid) error = %v", err)
	}
	resolverCalls := 0
	validationFleet, err := NewFleetClient(FleetClientConfig{
		Resolver: EndpointResolverFunc(func(context.Context) ([]Endpoint, error) {
			resolverCalls++
			return []Endpoint{{ID: "valid", BaseURL: validServer.URL}}, nil
		}),
		Token: "fleet-secret",
	})
	if err != nil {
		t.Fatalf("NewFleetClient(validation) error = %v", err)
	}
	for name, invoke := range map[string]func() error{
		"workers": func() error {
			_, invokeErr := validationFleet.ListWorkers(context.Background(), management.StatusPageRequest{})
			return invokeErr
		},
		"queues": func() error {
			_, invokeErr := validationFleet.ListQueues(context.Background(), management.StatusPageRequest{})
			return invokeErr
		},
		"failures": func() error {
			_, invokeErr := validationFleet.ListFailures(context.Background(), management.PageRequest{})
			return invokeErr
		},
		"dead letters": func() error {
			_, invokeErr := validationFleet.ListDeadLetters(context.Background(), management.PageRequest{})
			return invokeErr
		},
		"inspect": func() error {
			_, invokeErr := validationFleet.Inspect(context.Background(), management.InspectRequest{})
			return invokeErr
		},
		"execute": func() error {
			_, invokeErr := validationFleet.Execute(context.Background(), management.Command{})
			return invokeErr
		},
	} {
		if invokeErr := invoke(); !errors.Is(invokeErr, ErrInvalidRequest) {
			t.Fatalf("%s invalid request error = %v", name, invokeErr)
		}
	}
	if resolverCalls != 0 {
		t.Fatalf("invalid request resolver calls = %d, want 0", resolverCalls)
	}

	var nilFleet *FleetClient
	if _, err = nilFleet.ListWorkers(
		context.Background(),
		management.StatusPageRequest{Limit: 1},
	); !errors.Is(err, ErrInvalidFleetConfiguration) {
		t.Fatalf("nil ListWorkers() error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	validFleet, err := NewFleetClient(FleetClientConfig{
		Resolver: EndpointResolverFunc(func(context.Context) ([]Endpoint, error) {
			return []Endpoint{{ID: "valid", BaseURL: validServer.URL}}, nil
		}),
		Token: "fleet-secret",
	})
	if err != nil {
		t.Fatalf("NewFleetClient(valid) error = %v", err)
	}
	if _, err = validFleet.ListWorkers(canceled, management.StatusPageRequest{Limit: 1}); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("ListWorkers(canceled) error = %v", err)
	}
	for name, resolverError := range map[string]error{
		"canceled": context.Canceled,
		"deadline": context.DeadlineExceeded,
	} {
		resolverCanceled, createErr := NewFleetClient(FleetClientConfig{
			Resolver: EndpointResolverFunc(func(context.Context) ([]Endpoint, error) {
				return nil, resolverError
			}),
			Token: "fleet-secret",
		})
		if createErr != nil {
			t.Fatalf("NewFleetClient(%s resolver) error = %v", name, createErr)
		}
		if _, invokeErr := resolverCanceled.ListWorkers(
			context.Background(),
			management.StatusPageRequest{Limit: 1},
		); !errors.Is(invokeErr, resolverError) {
			t.Fatalf("ListWorkers(%s resolver) error = %v", name, invokeErr)
		}
	}

	tooMany := make([]Endpoint, MaxFleetEndpoints+1)
	for index := range tooMany {
		tooMany[index] = Endpoint{ID: "endpoint-" + string(rune(index+1)), BaseURL: validServer.URL}
	}
	for name, endpoints := range map[string][]Endpoint{
		"empty":        {},
		"too many":     tooMany,
		"empty id":     {{BaseURL: validServer.URL}},
		"spaced id":    {{ID: " endpoint", BaseURL: validServer.URL}},
		"oversized id": {{ID: strings.Repeat("x", 257), BaseURL: validServer.URL}},
		"duplicate base URL": {
			{ID: "first", BaseURL: validServer.URL},
			{ID: "second", BaseURL: validServer.URL + "/"},
		},
		"empty base URL": {{ID: "endpoint"}},
		"invalid URL":    {{ID: "endpoint", BaseURL: "://invalid"}},
	} {
		fleet, createErr := NewFleetClient(FleetClientConfig{
			Resolver: EndpointResolverFunc(func(context.Context) ([]Endpoint, error) {
				return endpoints, nil
			}),
			Token: "fleet-secret",
		})
		if createErr != nil {
			t.Fatalf("NewFleetClient(%s) error = %v", name, createErr)
		}
		if _, invokeErr := fleet.ListWorkers(
			context.Background(),
			management.StatusPageRequest{Limit: 1},
		); !errors.Is(invokeErr, ErrInvalidFleetEndpoints) {
			t.Fatalf("ListWorkers(%s) error = %v", name, invokeErr)
		}
	}
	duplicateBaseFleet := mustFleetClient(t, []Endpoint{
		{ID: "first", BaseURL: validServer.URL},
		{ID: "second", BaseURL: validServer.URL + "/"},
	})
	duplicateBaseCommand := validCommand()
	duplicateBaseCommand.Action = management.CommandPause
	duplicateBaseCommand.Target = management.Target{Kind: management.TargetQueue, Name: "critical"}
	if _, err = duplicateBaseFleet.Execute(
		context.Background(),
		duplicateBaseCommand,
	); !errors.Is(err, ErrInvalidFleetEndpoints) {
		t.Fatalf("Execute(duplicate base URL) error = %v", err)
	}
	if validController.calls != 0 {
		t.Fatalf("duplicate base URL command calls = %d, want 0", validController.calls)
	}
}

func TestFleetClientContainsRemoteStatusFailuresAndDuplicates(t *testing.T) {
	t.Parallel()

	failingStatus := &splitFleetStatusReader{
		queues:    management.QueueStatusPage{Items: []management.QueueStatus{validQueueStatus()}},
		workerErr: errors.New("worker failure"),
	}
	failingServer := newFleetStatusServer(t, failingStatus)
	queueFailure := &splitFleetStatusReader{
		workers:  management.WorkerStatusPage{Items: []management.WorkerStatus{validWorkerStatus()}},
		queueErr: errors.New("queue failure"),
	}
	queueFailureServer := newFleetStatusServer(t, queueFailure)
	emptyServer := newFleetStatusServer(t, &splitFleetStatusReader{})
	duplicateFirst := newFleetWorkerServer(t, "duplicate", &controllerStub{})
	duplicateSecond := newFleetWorkerServer(t, "duplicate", &controllerStub{})

	for name, endpoints := range map[string][]Endpoint{
		"worker failure": {{ID: "failure", BaseURL: failingServer.URL}},
		"duplicate worker": {
			{ID: "first", BaseURL: duplicateFirst.URL},
			{ID: "second", BaseURL: duplicateSecond.URL},
		},
	} {
		fleet := mustFleetClient(t, endpoints)
		if _, err := fleet.ListWorkers(
			context.Background(),
			management.StatusPageRequest{Limit: 10},
		); !errors.Is(err, map[string]error{
			"duplicate worker": ErrInvalidFleetEndpoints,
		}[name]) && !errors.Is(err, ErrFleetUnavailable) {
			t.Fatalf("ListWorkers(%s) error = %v", name, err)
		}
	}
	workerFailed := mustFleetClient(t, []Endpoint{{ID: "failure", BaseURL: failingServer.URL}})
	queues, err := workerFailed.ListQueues(
		context.Background(),
		management.StatusPageRequest{Limit: 1},
	)
	if err != nil || len(queues.Items) != 1 {
		t.Fatalf("ListQueues(worker status failure) = (%+v, %v)", queues, err)
	}
	queueFailed := mustFleetClient(t, []Endpoint{{ID: "failure", BaseURL: queueFailureServer.URL}})
	workers, err := queueFailed.ListWorkers(
		context.Background(),
		management.StatusPageRequest{Limit: 1},
	)
	if err != nil || len(workers.Items) != 1 {
		t.Fatalf("ListWorkers(queue status failure) = (%+v, %v)", workers, err)
	}
	empty := mustFleetClient(t, []Endpoint{{ID: "empty", BaseURL: emptyServer.URL}})
	if _, err := empty.ListWorkers(
		context.Background(),
		management.StatusPageRequest{Limit: 1},
	); !errors.Is(err, ErrFleetUnavailable) {
		t.Fatalf("ListWorkers(empty) error = %v", err)
	}
	if _, err := empty.ListQueues(
		context.Background(),
		management.StatusPageRequest{Limit: 1},
	); !errors.Is(err, ErrFleetUnavailable) {
		t.Fatalf("ListQueues(empty) error = %v", err)
	}
	if _, err := queueFailed.ListQueues(
		context.Background(),
		management.StatusPageRequest{Limit: 1},
	); !errors.Is(err, ErrFleetUnavailable) {
		t.Fatalf("ListQueues(remote failure) error = %v", err)
	}
	command := validCommand()
	if _, err := mustFleetClient(
		t,
		[]Endpoint{{ID: "failure", BaseURL: failingServer.URL}},
	).Execute(context.Background(), command); !errors.Is(err, ErrFleetUnavailable) {
		t.Fatalf("Execute(worker status failure) error = %v", err)
	}
}

func TestFleetClientBoundsRemoteStatusPagination(t *testing.T) {
	t.Parallel()

	for name, status := range map[string]*pagedFleetStatusReader{
		"workers paginate": {workerPages: 2},
		"workers repeat":   {workerPages: -1},
		"workers overflow": {workerPages: MaxFleetEndpoints + 1},
		"queues paginate":  {queuePages: 2},
		"queues repeat":    {queuePages: -1},
		"queues overflow":  {queuePages: MaxFleetEndpoints + 1},
	} {
		server := newFleetStatusServer(t, status)
		client, err := NewClient(ClientConfig{
			BaseURL: server.URL, Token: "fleet-secret",
		})
		if err != nil {
			t.Fatalf("NewClient(%s) error = %v", name, err)
		}
		switch {
		case strings.HasPrefix(name, "workers"):
			items, readErr := readAllFleetWorkers(context.Background(), client)
			if strings.HasSuffix(name, "paginate") {
				if readErr != nil || len(items) != 2 {
					t.Fatalf("readAllFleetWorkers(%s) = (%d, %v)", name, len(items), readErr)
				}
			} else if !errors.Is(readErr, ErrInvalidFleetEndpoints) {
				t.Fatalf("readAllFleetWorkers(%s) error = %v", name, readErr)
			}
		case strings.HasPrefix(name, "queues"):
			items, readErr := readAllFleetQueues(context.Background(), client)
			if strings.HasSuffix(name, "paginate") {
				if readErr != nil || len(items) != 2 {
					t.Fatalf("readAllFleetQueues(%s) = (%d, %v)", name, len(items), readErr)
				}
			} else if !errors.Is(readErr, ErrInvalidFleetEndpoints) {
				t.Fatalf("readAllFleetQueues(%s) error = %v", name, readErr)
			}
		}
	}
}

func newFleetWorkerServer(
	t *testing.T,
	workerID string,
	controller *controllerStub,
) *httptest.Server {
	t.Helper()
	worker := validWorkerStatus()
	worker.ID = workerID
	status := &statusReaderStub{
		workers: management.WorkerStatusPage{Items: []management.WorkerStatus{worker}},
		queues:  management.QueueStatusPage{Items: []management.QueueStatus{validQueueStatus()}},
	}
	controller.execute = func(
		_ context.Context,
		command management.Command,
	) (management.CommandResult, error) {
		if controller.err != nil {
			return management.CommandResult{}, controller.err
		}
		return management.CommandResult{
			CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
			WorkerID: workerID, Protocol: command.Protocol,
			Status:      management.CommandAcknowledged,
			CompletedAt: time.Date(2026, 7, 16, 10, 0, 1, 0, time.UTC),
		}, nil
	}
	handler, err := NewHandler(HandlerConfig{
		Token: "fleet-secret", Status: status, Controller: controller,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return server
}

type splitFleetStatusReader struct {
	workers   management.WorkerStatusPage
	queues    management.QueueStatusPage
	workerErr error
	queueErr  error
}

type pagedFleetStatusReader struct {
	workerPages int
	queuePages  int
}

func (reader *pagedFleetStatusReader) ListWorkers(
	_ context.Context,
	request management.StatusPageRequest,
) (management.WorkerStatusPage, error) {
	if reader.workerPages > MaxFleetEndpoints {
		items := make([]management.WorkerStatus, reader.workerPages)
		for index := range items {
			items[index] = validWorkerStatus()
			items[index].ID = fmt.Sprintf("worker-%03d", index)
		}
		return management.WorkerStatusPage{Items: items}, nil
	}
	if reader.workerPages == 0 {
		return management.WorkerStatusPage{}, nil
	}
	item := validWorkerStatus()
	item.ID = "worker-first"
	if request.Cursor == "" {
		return management.WorkerStatusPage{
			Items: []management.WorkerStatus{item}, NextCursor: "next",
		}, nil
	}
	if reader.workerPages < 0 {
		item.ID = "worker-repeated"
		return management.WorkerStatusPage{
			Items: []management.WorkerStatus{item}, NextCursor: "next",
		}, nil
	}
	item.ID = "worker-second"
	return management.WorkerStatusPage{Items: []management.WorkerStatus{item}}, nil
}

func (reader *pagedFleetStatusReader) ListQueues(
	_ context.Context,
	request management.StatusPageRequest,
) (management.QueueStatusPage, error) {
	if reader.queuePages > MaxFleetEndpoints {
		items := make([]management.QueueStatus, reader.queuePages)
		for index := range items {
			items[index] = validQueueStatus()
			items[index].Queue = fmt.Sprintf("queue-%03d", index)
		}
		return management.QueueStatusPage{Items: items}, nil
	}
	if reader.queuePages == 0 {
		return management.QueueStatusPage{}, nil
	}
	item := validQueueStatus()
	item.Queue = "queue-first"
	if request.Cursor == "" {
		return management.QueueStatusPage{
			Items: []management.QueueStatus{item}, NextCursor: "next",
		}, nil
	}
	if reader.queuePages < 0 {
		item.Queue = "queue-repeated"
		return management.QueueStatusPage{
			Items: []management.QueueStatus{item}, NextCursor: "next",
		}, nil
	}
	item.Queue = "queue-second"
	return management.QueueStatusPage{Items: []management.QueueStatus{item}}, nil
}

func (reader *splitFleetStatusReader) ListWorkers(
	context.Context,
	management.StatusPageRequest,
) (management.WorkerStatusPage, error) {
	return reader.workers, reader.workerErr
}

func (reader *splitFleetStatusReader) ListQueues(
	context.Context,
	management.StatusPageRequest,
) (management.QueueStatusPage, error) {
	return reader.queues, reader.queueErr
}

func newFleetStatusServer(
	t *testing.T,
	status management.StatusReader,
) *httptest.Server {
	t.Helper()
	handler, err := NewHandler(HandlerConfig{
		Token: "fleet-secret", Status: status,
	})
	if err != nil {
		t.Fatalf("NewHandler(status) error = %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return server
}

func mustFleetClient(t *testing.T, endpoints []Endpoint) *FleetClient {
	t.Helper()
	fleet, err := NewFleetClient(FleetClientConfig{
		Resolver: EndpointResolverFunc(func(context.Context) ([]Endpoint, error) {
			return endpoints, nil
		}),
		Token: "fleet-secret",
	})
	if err != nil {
		t.Fatalf("NewFleetClient() error = %v", err)
	}

	return fleet
}
