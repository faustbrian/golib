package managementhttp_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"time"

	"github.com/faustbrian/golib/pkg/queue/management"
	"github.com/faustbrian/golib/pkg/queue/managementhttp"
)

func ExampleClient_Execute() {
	controller := exampleController{}
	handler, err := managementhttp.NewHandler(managementhttp.HandlerConfig{
		Token: "replace-with-a-secret", Controller: controller,
	})
	if err != nil {
		panic(err)
	}
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	client, err := managementhttp.NewClient(managementhttp.ClientConfig{
		BaseURL: server.URL, Token: "replace-with-a-secret",
		HTTPClient: server.Client(),
	})
	if err != nil {
		panic(err)
	}
	requestedAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	result, err := client.Execute(context.Background(), management.Command{
		ID: "command-1", IdempotencyKey: "deployment-1", Actor: "operator-1",
		Reason:      "drain before deployment",
		Protocol:    management.ProtocolVersion{Major: 1},
		Action:      management.CommandDrain,
		Target:      management.Target{Kind: management.TargetWorker, Name: "worker-1"},
		RequestedAt: requestedAt, Deadline: requestedAt.Add(time.Minute),
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Status)
	// Output: acknowledged
}

func ExampleClient_ListFailures() {
	reader := exampleRecordReader{}
	handler, err := managementhttp.NewHandler(managementhttp.HandlerConfig{
		Token: "replace-with-a-secret", Records: reader,
	})
	if err != nil {
		panic(err)
	}
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	client, err := managementhttp.NewClient(managementhttp.ClientConfig{
		BaseURL: server.URL, Token: "replace-with-a-secret",
		HTTPClient: server.Client(),
	})
	if err != nil {
		panic(err)
	}
	page, err := client.ListFailures(context.Background(), management.PageRequest{
		Limit: 25, Sort: management.SortOccurredAt,
		Direction: management.SortDescending,
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(page.Items[0].ID, page.Items[0].Payload.Visibility == management.PayloadHidden)
	// Output: failure-1 true
}

type exampleController struct{}

func (exampleController) Execute(
	_ context.Context,
	command management.Command,
) (management.CommandResult, error) {
	return management.CommandResult{
		CommandID: command.ID, IdempotencyKey: command.IdempotencyKey,
		WorkerID: "worker-1", Protocol: command.Protocol,
		Status:      management.CommandAcknowledged,
		CompletedAt: command.RequestedAt.Add(time.Second),
	}, nil
}

type exampleRecordReader struct{}

func (exampleRecordReader) ListFailures(
	context.Context,
	management.PageRequest,
) (management.RecordPage, error) {
	return management.RecordPage{Items: []management.JobRecord{{
		Kind: management.RecordFailure, ID: "failure-1", Backend: "example",
		Queue: "critical", OccurredAt: time.Unix(2, 0).UTC(), Attempts: 1,
		FailureCode: "handler_failed", Payload: management.Payload{Size: 7},
	}}}, nil
}

func (exampleRecordReader) ListDeadLetters(
	context.Context,
	management.PageRequest,
) (management.RecordPage, error) {
	return management.RecordPage{}, nil
}

func (exampleRecordReader) Inspect(
	context.Context,
	management.InspectRequest,
) (management.JobRecord, error) {
	return management.JobRecord{}, nil
}
