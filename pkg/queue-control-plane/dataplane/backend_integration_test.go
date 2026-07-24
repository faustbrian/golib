//go:build integration

package dataplane

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	goqueue "github.com/faustbrian/golib/pkg/queue"
	controlplane "github.com/faustbrian/golib/pkg/queue-control-plane"
	"github.com/faustbrian/golib/pkg/queue-control-plane/control"
	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
	queue "github.com/faustbrian/golib/pkg/queue/management"
	"github.com/faustbrian/golib/pkg/queue/managementhttp"
	redisstream "github.com/faustbrian/golib/pkg/queue/redisstream"
	"github.com/faustbrian/golib/pkg/queue/valkeystream"
)

func TestRealGoQueueBackendsThroughManagementHTTP(t *testing.T) {
	redisAddress := os.Getenv("TEST_REDIS_ADDRESS")
	valkeyAddress := os.Getenv("TEST_VALKEY_ADDRESS")
	if redisAddress == "" || valkeyAddress == "" {
		t.Skip("real queue integration endpoints are not configured")
	}

	now := time.Now().UTC()
	metadata := queue.StatusMetadata{
		ID: "worker-1", Version: "v1.0.0", Concurrency: 1,
		Protocol: queue.ProtocolVersion{Major: 1},
	}
	t.Run("redis streams", func(t *testing.T) {
		queueName := "control-plane-integration"
		recoveryName := "control-plane-recovery"
		worker, err := redisstream.NewWorkerE(
			redisstream.WithAddr(redisAddress),
			redisstream.WithStreamName(queueName),
			redisstream.WithGroup(queueName),
			redisstream.WithConsumer("worker-1"),
			redisstream.WithConnectTimeout(5*time.Second),
			redisstream.WithRequestTimeout(2*time.Second),
			redisstream.WithBlockTime(10*time.Millisecond),
			redisstream.WithFailureStream(queueName+"-failures"),
			redisstream.WithDeadLetter(queueName+"-dead", 5),
			redisstream.WithReplayDestinations(recoveryName),
			redisstream.WithRecordRetention(100),
			redisstream.WithManagementStatus(metadata),
		)
		if err != nil {
			t.Fatalf("redisstream.NewWorkerE() error = %v", err)
		}
		runRealBackendManagement(
			t, worker, "redis-streams", now, metadata,
			func(client *managementhttp.Client, dispatcher *ControllerDispatcher) {
				verifyRealBackendRecordOperations(
					t, worker, client, dispatcher, recoveryName,
					func() (core.Worker, error) {
						return redisstream.NewWorkerE(
							redisstream.WithAddr(redisAddress),
							redisstream.WithStreamName(recoveryName),
							redisstream.WithGroup(recoveryName),
							redisstream.WithConsumer("recovery-worker"),
							redisstream.WithConnectTimeout(5*time.Second),
							redisstream.WithRequestTimeout(2*time.Second),
							redisstream.WithBlockTime(10*time.Millisecond),
						)
					},
				)
			},
		)
	})
	t.Run("valkey streams", func(t *testing.T) {
		queueName := "control-plane-integration"
		recoveryName := "control-plane-recovery"
		worker, err := valkeystream.NewWorkerE(
			valkeystream.WithAddress(valkeyAddress),
			valkeystream.WithStreamName(queueName),
			valkeystream.WithGroup(queueName),
			valkeystream.WithConsumer("worker-1"),
			valkeystream.WithDialTimeout(5*time.Second),
			valkeystream.WithRequestTimeout(2*time.Second),
			valkeystream.WithBlockTime(10*time.Millisecond),
			valkeystream.WithFailureStream(queueName+"-failures"),
			valkeystream.WithDeadLetter(queueName+"-dead", 5),
			valkeystream.WithManagementStatus(metadata),
			valkeystream.WithReplayDestinations(recoveryName),
			valkeystream.WithRecordRetention(100),
		)
		if err != nil {
			t.Fatalf("valkeystream.NewWorkerE() error = %v", err)
		}
		runRealBackendManagement(
			t, worker, "valkey-streams", now, metadata,
			func(client *managementhttp.Client, dispatcher *ControllerDispatcher) {
				verifyRealBackendRecordOperations(
					t, worker, client, dispatcher, recoveryName,
					func() (core.Worker, error) {
						return valkeystream.NewWorkerE(
							valkeystream.WithAddress(valkeyAddress),
							valkeystream.WithStreamName(recoveryName),
							valkeystream.WithGroup(recoveryName),
							valkeystream.WithConsumer("recovery-worker"),
							valkeystream.WithDialTimeout(5*time.Second),
							valkeystream.WithRequestTimeout(2*time.Second),
							valkeystream.WithBlockTime(10*time.Millisecond),
						)
					},
				)
			},
		)
	})
}

func runRealBackendManagement(
	t *testing.T,
	worker core.Worker,
	backend string,
	now time.Time,
	metadata queue.StatusMetadata,
	verify func(*managementhttp.Client, *ControllerDispatcher),
) {
	t.Helper()
	message := job.NewMessage(backendIntegrationPayload("seed"))
	if err := worker.Queue(&message); err != nil {
		t.Fatalf("worker.Queue() error = %v", err)
	}
	delivery, err := worker.Request()
	if err != nil {
		t.Fatalf("worker.Request() error = %v", err)
	}
	if settlement, required := delivery.(core.Acknowledger); required {
		if err := settlement.Ack(); err != nil {
			t.Fatalf("delivery.Ack() error = %v", err)
		}
	}
	lifecycle, err := queue.NewWorkerLifecycle(queue.WorkerLifecycleConfig{
		Metadata: metadata, WorkerGroup: "payments", Queue: "control-plane-integration",
		MaxCommandResults: 8, Now: func() time.Time { return now.Add(time.Second) },
	})
	if err != nil {
		t.Fatalf("NewWorkerLifecycle() error = %v", err)
	}
	managedQueue, err := goqueue.NewQueue(
		goqueue.WithWorker(worker), goqueue.WithWorkerLifecycle(lifecycle),
	)
	if err != nil {
		t.Fatalf("NewQueue() error = %v", err)
	}
	t.Cleanup(managedQueue.Release)
	statusReader, err := queue.NewStatusReader(queue.StatusReaderConfig{
		Workers: []queue.WorkerStatusProvider{managedQueue},
		Queues:  []queue.QueueStatusProvider{managedQueue},
	})
	if err != nil {
		t.Fatalf("NewStatusReader() error = %v", err)
	}
	recordReader, ok := worker.(queue.RecordReader)
	if !ok {
		t.Fatalf("%s worker does not implement management.RecordReader", backend)
	}
	directQueueStatus, err := managedQueue.ObserveQueue(t.Context())
	if err != nil || directQueueStatus.Backend != backend {
		t.Fatalf("ObserveQueue() = (%+v, %v), want backend %q", directQueueStatus, err, backend)
	}
	handler, err := managementhttp.NewHandler(managementhttp.HandlerConfig{
		Token: "integration-secret", Status: statusReader, Controller: managedQueue,
		Records: recordReader,
	})
	if err != nil {
		t.Fatalf("managementhttp.NewHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := managementhttp.NewClient(managementhttp.ClientConfig{
		BaseURL: server.URL, Token: "integration-secret", HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("managementhttp.NewClient() error = %v", err)
	}
	pageRequest := queue.PageRequest{
		Limit: 10, Sort: queue.SortOccurredAt, Direction: queue.SortDescending,
	}
	if page, listErr := client.ListFailures(t.Context(), pageRequest); listErr != nil || len(page.Items) != 0 {
		t.Fatalf("initial ListFailures() = (%+v, %v), want empty", page, listErr)
	}
	if page, listErr := client.ListDeadLetters(t.Context(), pageRequest); listErr != nil || len(page.Items) != 0 {
		t.Fatalf("initial ListDeadLetters() = (%+v, %v), want empty", page, listErr)
	}
	dispatcher, err := NewControllerDispatcher(
		&goQueueIntegrationResolver{client: client}, metadata.Protocol,
		time.Minute, func() time.Time { return now.Add(time.Second) },
	)
	if err != nil {
		t.Fatalf("NewControllerDispatcher() error = %v", err)
	}
	for index, action := range []controlplane.Action{
		controlplane.ActionPause, controlplane.ActionResume,
	} {
		command := lifecycleControlCommand(
			fmt.Sprintf("lifecycle-%d", index), action, now,
		)
		command.Target.Name = "control-plane-integration"
		outcome, dispatchErr := dispatcher.DispatchResult(context.Background(), command)
		if dispatchErr != nil || outcome.Status != controlplane.CommandSucceeded {
			t.Fatalf("%s DispatchResult() = (%+v, %v)", action, outcome, dispatchErr)
		}
	}
	workers, err := client.ListWorkers(t.Context(), queue.StatusPageRequest{Limit: 10})
	if err != nil || len(workers.Items) != 1 || workers.Items[0].State != queue.WorkerRunning {
		t.Fatalf("ListWorkers() = (%+v, %v)", workers, err)
	}
	for _, capability := range []queue.Capability{
		queue.CapabilityFailures,
		queue.CapabilityDeadLetters,
		queue.CapabilityRetry,
		queue.CapabilityBulkRetry,
		queue.CapabilityDelete,
		queue.CapabilityPurge,
		queue.CapabilityReplay,
		queue.CapabilityRetentionCount,
	} {
		if !containsQueueCapability(workers.Items[0].Capabilities, capability) {
			t.Fatalf("%s capabilities %v missing %q", backend, workers.Items[0].Capabilities, capability)
		}
	}
	queues, err := client.ListQueues(t.Context(), queue.StatusPageRequest{Limit: 10})
	if err != nil || len(queues.Items) != 1 || queues.Items[0].Backend != backend {
		t.Fatalf("ListQueues() = (%+v, %v), want backend %q", queues, err, backend)
	}
	if verify != nil {
		verify(client, dispatcher)
	}
}

func verifyRealBackendRecordOperations(
	t *testing.T,
	worker core.Worker,
	client *managementhttp.Client,
	dispatcher *ControllerDispatcher,
	recoveryName string,
	newRecovery func() (core.Worker, error),
) {
	t.Helper()

	seedFailureRecord(t, worker, "retryable payload", queue.ClassificationRetryable, "retryable_failure")
	failure := findRealRecord(t, client, queue.RecordFailure, "retryable_failure")
	assertRealRecordInspection(t, client, failure)
	dispatchRealRecordCommand(
		t, dispatcher, "retry-failure", controlplane.ActionRetry,
		controlplane.TargetFailure, failure.ID, nil,
	)
	ackRealDeliveries(t, worker, 1)

	seedFailureRecord(t, worker, "dead payload", queue.ClassificationPermanent, "permanent_failure")
	failure = findRealRecord(t, client, queue.RecordFailure, "permanent_failure")
	deadLetter := findRealRecord(t, client, queue.RecordDeadLetter, "permanent_failure")
	assertRealRecordInspection(t, client, failure)
	assertRealRecordInspection(t, client, deadLetter)

	replay := &controlplane.Replay{
		Destination: recoveryName, IdempotencyPolicy: controlplane.ReplayRejectDuplicate,
	}
	dispatchRealRecordCommand(
		t, dispatcher, "replay-dead", controlplane.ActionReplay,
		controlplane.TargetDeadLetter, deadLetter.ID, replay,
	)
	outcome := dispatchRealRecordCommand(
		t, dispatcher, "replay-duplicate", controlplane.ActionReplay,
		controlplane.TargetDeadLetter, deadLetter.ID, replay,
	)
	if outcome.Status != controlplane.CommandFailed || outcome.Failure != "replay_duplicate" {
		t.Fatalf("duplicate replay outcome = %+v", outcome)
	}
	recovery, err := newRecovery()
	if err != nil {
		t.Fatalf("create recovery worker: %v", err)
	}
	t.Cleanup(func() { _ = recovery.Shutdown() })
	replayed, err := recovery.Request()
	if err != nil || string(replayed.Payload()) != "dead payload" {
		t.Fatalf("recovery Request() = (%v, %v)", replayed, err)
	}
	if settlement, required := replayed.(core.Acknowledger); required {
		if err := settlement.Ack(); err != nil {
			t.Fatalf("replayed delivery.Ack() error = %v", err)
		}
	}
	dispatchRealRecordCommand(
		t, dispatcher, "retry-dead", controlplane.ActionRetry,
		controlplane.TargetDeadLetter, deadLetter.ID, nil,
	)
	ackRealDeliveries(t, worker, 1)

	seedFailureRecord(t, worker, "bulk one", queue.ClassificationPermanent, "bulk_one")
	seedFailureRecord(t, worker, "bulk two", queue.ClassificationPermanent, "bulk_two")
	selection := &controlplane.Selection{Limit: 2}
	dispatchRealRecordCommand(
		t, dispatcher, "bulk-dead", controlplane.ActionBulkRetry,
		controlplane.TargetDeadLetter, "dead-letter-selection", selection,
	)
	ackRealDeliveries(t, worker, 2)

	seedFailureRecord(t, worker, "delete payload", queue.ClassificationPermanent, "delete_failure")
	deleteRecord := findRealRecord(t, client, queue.RecordDeadLetter, "delete_failure")
	dispatchRealRecordCommand(
		t, dispatcher, "delete-dead", controlplane.ActionDelete,
		controlplane.TargetDeadLetter, deleteRecord.ID, nil,
	)
	if _, inspectErr := client.Inspect(t.Context(), queue.InspectRequest{
		Kind: queue.RecordDeadLetter, ID: deleteRecord.ID, Visibility: queue.PayloadHidden,
	}); !errors.Is(inspectErr, queue.ErrRecordNotFound) {
		t.Fatalf("Inspect() deleted record error = %v", inspectErr)
	}

	seedFailureRecord(t, worker, "race payload", queue.ClassificationPermanent, "race_failure")
	raceRecord := findRealRecord(t, client, queue.RecordDeadLetter, "race_failure")
	if verifyConcurrentRetryDelete(t, dispatcher, raceRecord.ID) == controlplane.ActionRetry {
		ackRealDeliveries(t, worker, 1)
	}

	seedFailureRecord(t, worker, "purge one", queue.ClassificationPermanent, "purge_one")
	seedFailureRecord(t, worker, "purge two", queue.ClassificationPermanent, "purge_two")
	dispatchRealRecordCommand(
		t, dispatcher, "purge-dead", controlplane.ActionPurge,
		controlplane.TargetDeadLetter, "dead-letter-selection", nil,
	)
	pageRequest := queue.PageRequest{
		Limit: 100, Sort: queue.SortOccurredAt, Direction: queue.SortDescending,
	}
	if page, listErr := client.ListDeadLetters(t.Context(), pageRequest); listErr != nil || len(page.Items) != 0 {
		t.Fatalf("ListDeadLetters() after purge = (%+v, %v)", page, listErr)
	}
	dispatchRealRecordCommand(
		t, dispatcher, "purge-failures", controlplane.ActionPurge,
		controlplane.TargetFailure, "failure-selection", nil,
	)
}

func verifyConcurrentRetryDelete(
	t *testing.T,
	dispatcher *ControllerDispatcher,
	recordID string,
) controlplane.Action {
	t.Helper()

	type result struct {
		action  controlplane.Action
		outcome control.DispatchOutcome
		err     error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for _, action := range []controlplane.Action{
		controlplane.ActionRetry,
		controlplane.ActionDelete,
	} {
		go func(action controlplane.Action) {
			ready.Done()
			<-start
			command := controlplane.Command{
				CommandID: string(action) + "-race-command", IdempotencyKey: string(action) + "-race",
				TenantID: "tenant-1", Actor: "operator-1", Reason: "prove concurrent mutation",
				Action: action, Target: controlplane.Target{Kind: controlplane.TargetDeadLetter, Name: recordID},
				RequestedAt: time.Now().UTC().Add(-time.Millisecond),
			}
			outcome, err := dispatcher.DispatchResult(t.Context(), command)
			results <- result{action: action, outcome: outcome, err: err}
		}(action)
	}
	ready.Wait()
	close(start)

	var winner controlplane.Action
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent %s transport error = %v", result.action, result.err)
		}
		if result.outcome.Status == controlplane.CommandSucceeded {
			if winner != "" {
				t.Fatalf("concurrent mutations both succeeded: %s and %s", winner, result.action)
			}
			winner = result.action
			continue
		}
		if result.outcome.Status != controlplane.CommandFailed || result.outcome.Failure == "" {
			t.Fatalf("concurrent %s outcome = %+v", result.action, result.outcome)
		}
	}
	if winner == "" {
		t.Fatal("concurrent retry/delete had no successful mutation")
	}

	return winner
}

func seedFailureRecord(
	t *testing.T,
	worker core.Worker,
	payload string,
	classification queue.Classification,
	code string,
) {
	t.Helper()
	message := job.NewMessage(backendIntegrationPayload(payload))
	if err := worker.Queue(&message); err != nil {
		t.Fatalf("worker.Queue(%q) error = %v", code, err)
	}
	delivery, err := worker.Request()
	if err != nil {
		t.Fatalf("worker.Request(%q) error = %v", code, err)
	}
	nacker, ok := delivery.(interface{ NackFailure(error) error })
	if !ok {
		t.Fatalf("delivery %q cannot record a classified failure", code)
	}
	failure := queue.NewFailure(classification, code, errors.New("integration failure"))
	if err := nacker.NackFailure(failure); err != nil {
		t.Fatalf("NackFailure(%q) error = %v", code, err)
	}
}

func findRealRecord(
	t *testing.T,
	client *managementhttp.Client,
	kind queue.RecordKind,
	failureCode string,
) queue.JobRecord {
	t.Helper()
	request := queue.PageRequest{
		Limit: 100, Sort: queue.SortOccurredAt, Direction: queue.SortDescending,
	}
	var page queue.RecordPage
	var err error
	if kind == queue.RecordFailure {
		page, err = client.ListFailures(t.Context(), request)
	} else {
		page, err = client.ListDeadLetters(t.Context(), request)
	}
	if err != nil {
		t.Fatalf("list %s records: %v", kind, err)
	}
	for _, record := range page.Items {
		if record.FailureCode == failureCode {
			return record
		}
	}
	t.Fatalf("%s record with failure code %q not found in %+v", kind, failureCode, page)

	return queue.JobRecord{}
}

func assertRealRecordInspection(
	t *testing.T,
	client *managementhttp.Client,
	record queue.JobRecord,
) {
	t.Helper()
	hidden, err := client.Inspect(t.Context(), queue.InspectRequest{
		Kind: record.Kind, ID: record.ID, Visibility: queue.PayloadHidden,
	})
	if err != nil || len(hidden.Payload.Data) != 0 || hidden.Payload.Visibility != queue.PayloadHidden {
		t.Fatalf("hidden Inspect() = (%+v, %v)", hidden, err)
	}
	revealed, err := client.Inspect(t.Context(), queue.InspectRequest{
		Kind: record.Kind, ID: record.ID, Visibility: queue.PayloadRevealed,
	})
	if err != nil || len(revealed.Payload.Data) == 0 ||
		revealed.Payload.Size != int64(len(revealed.Payload.Data)) ||
		revealed.EnvelopeVersion != queue.CurrentEnvelopeVersion {
		t.Fatalf("revealed Inspect() = (%+v, %v)", revealed, err)
	}
}

func dispatchRealRecordCommand(
	t *testing.T,
	dispatcher *ControllerDispatcher,
	id string,
	action controlplane.Action,
	kind controlplane.TargetKind,
	name string,
	options any,
) control.DispatchOutcome {
	t.Helper()
	command := controlplane.Command{
		CommandID:      id + "-command",
		IdempotencyKey: id, TenantID: "tenant-1", Actor: "operator-1",
		Reason: "real backend dead-letter integration", Action: action,
		Target:      controlplane.Target{Kind: kind, Name: name},
		RequestedAt: time.Now().UTC().Add(-time.Millisecond),
	}
	switch value := options.(type) {
	case *controlplane.Replay:
		command.Confirmed = true
		command.Replay = value
	case *controlplane.Selection:
		command.Confirmed = true
		command.Selection = value
	case nil:
		command.Confirmed = action == controlplane.ActionPurge
	default:
		t.Fatalf("unsupported integration command options %T", options)
	}
	outcome, err := dispatcher.DispatchResult(t.Context(), command)
	if err != nil {
		t.Fatalf("%s DispatchResult() error = %v", action, err)
	}
	if id != "replay-duplicate" && outcome.Status != controlplane.CommandSucceeded {
		t.Fatalf("%s DispatchResult() = %+v", action, outcome)
	}
	if outcome.WorkerID == "" || outcome.Protocol == nil ||
		outcome.CapabilityAvailable == nil || !*outcome.CapabilityAvailable {
		t.Fatalf("%s acknowledgement snapshot = %+v", action, outcome)
	}

	return outcome
}

func ackRealDeliveries(t *testing.T, worker core.Worker, count int) {
	t.Helper()
	for range count {
		delivery, err := worker.Request()
		if err != nil {
			t.Fatalf("Request() retried delivery: %v", err)
		}
		settlement, ok := delivery.(core.Acknowledger)
		if !ok {
			t.Fatal("retried delivery does not support acknowledgement")
		}
		if err := settlement.Ack(); err != nil {
			t.Fatalf("Ack() retried delivery: %v", err)
		}
	}
}

type backendIntegrationPayload string

func (m backendIntegrationPayload) Bytes() []byte { return []byte(m) }
