package correlation_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	httpcorrelation "github.com/faustbrian/golib/pkg/correlation/http"
	jsonrpccorrelation "github.com/faustbrian/golib/pkg/correlation/jsonrpc"
	queuecorrelation "github.com/faustbrian/golib/pkg/correlation/queue"
	schedulecorrelation "github.com/faustbrian/golib/pkg/correlation/schedule"
	webhookcorrelation "github.com/faustbrian/golib/pkg/correlation/webhook"
)

func TestTrackHTTPQueueWebhookChainRetainsDistinctSemantics(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &sequenceGenerator{values: []string{
			"http-ingest", "queue-message", "queue-attempt",
			"webhook-delivery", "webhook-handler",
		}},
	})
	httpAdapter, _ := httpcorrelation.New(factory, httpcorrelation.Options{
		Trust: func(*http.Request) bool { return true },
	})
	queueAdapter, _ := queuecorrelation.New(factory, queuecorrelation.Options{})
	webhookAdapter, _ := webhookcorrelation.New(factory, webhookcorrelation.Options{
		Trust: func(*http.Request) bool { return true },
	})

	var queueMessage, queueAttempt, webhookDelivery, webhookHandler correlation.Values
	metadata := map[string]string{}
	webhookReceiver := webhookAdapter.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		webhookHandler, _ = correlation.FromContext(request.Context())
	}))
	ingestion := httpAdapter.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		httpValues, _ := correlation.FromContext(request.Context())
		var err error
		queueMessage, err = queueAdapter.Send(metadata, httpValues)
		if err != nil {
			t.Fatal(err)
		}
		queueAttempt, err = queueAdapter.Receive(metadata, true)
		if err != nil {
			t.Fatal(err)
		}
		outbound := httptest.NewRequest(http.MethodPost, "https://track.example/webhook", nil)
		webhookDelivery, err = webhookAdapter.Send(outbound, queueAttempt)
		if err != nil {
			t.Fatal(err)
		}
		webhookReceiver.ServeHTTP(httptest.NewRecorder(), outbound)
	}))

	request := httptest.NewRequest(http.MethodPost, "https://track.example/ingest", nil)
	request.Header.Set(httpcorrelation.CorrelationHeader, "track-workflow")
	request.Header.Set(httpcorrelation.RequestHeader, "client-request")
	ingestion.ServeHTTP(httptest.NewRecorder(), request)

	if queueMessage.CorrelationID.String() != "track-workflow" || queueMessage.RequestID.String() != "queue-message" || queueMessage.CausationID.String() != "http-ingest" {
		t.Fatalf("queue message = %#v", queueMessage)
	}
	if queueAttempt.RequestID.String() != "queue-attempt" || queueAttempt.CausationID.String() != "queue-message" {
		t.Fatalf("queue attempt = %#v", queueAttempt)
	}
	if webhookDelivery.RequestID.String() != "webhook-delivery" || webhookDelivery.CausationID.String() != "queue-attempt" {
		t.Fatalf("webhook delivery = %#v", webhookDelivery)
	}
	if webhookHandler.CorrelationID.String() != "track-workflow" || webhookHandler.RequestID.String() != "webhook-handler" || webhookHandler.CausationID.String() != "webhook-delivery" {
		t.Fatalf("webhook handler = %#v", webhookHandler)
	}
}

func TestEveryTransportPreservesWorkflowAndRotatesAttemptIdentity(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &sequenceGenerator{values: []string{
			"http-receive", "rpc-send", "rpc-receive", "queue-message",
			"queue-attempt-one", "queue-attempt-two", "retry-attempt",
			"scheduled-message", "scheduled-run", "webhook-send",
			"webhook-receive",
		}},
	})
	httpAdapter, _ := httpcorrelation.New(factory, httpcorrelation.Options{
		Trust: func(*http.Request) bool { return true },
	})
	rpcAdapter, _ := jsonrpccorrelation.New(factory, jsonrpccorrelation.Options{})
	queueAdapter, _ := queuecorrelation.New(factory, queuecorrelation.Options{})
	scheduleAdapter, _ := schedulecorrelation.New(factory, schedulecorrelation.Options{})
	webhookAdapter, _ := webhookcorrelation.New(factory, webhookcorrelation.Options{
		Trust: func(*http.Request) bool { return true },
	})

	var hops []correlation.Values
	webhookReceiver := webhookAdapter.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		values, _ := correlation.FromContext(request.Context())
		hops = append(hops, values)
	}))
	ingress := httpAdapter.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		httpValues, _ := correlation.FromContext(request.Context())
		hops = append(hops, httpValues)

		rpcMetadata := jsonrpccorrelation.Metadata{"application": {json.RawMessage(`"kept"`)}}
		rpcSent, err := rpcAdapter.Send(rpcMetadata, httpValues)
		if err != nil {
			t.Fatal(err)
		}
		hops = append(hops, rpcSent)
		rpcReceived, err := rpcAdapter.Receive(rpcMetadata, true)
		if err != nil {
			t.Fatal(err)
		}
		hops = append(hops, rpcReceived)

		queueMetadata := map[string]string{}
		message, err := queueAdapter.Send(queueMetadata, rpcReceived)
		if err != nil {
			t.Fatal(err)
		}
		hops = append(hops, message)
		firstAttempt, err := queueAdapter.Receive(queueMetadata, true)
		if err != nil {
			t.Fatal(err)
		}
		hops = append(hops, firstAttempt)
		redelivery, err := queueAdapter.Receive(queueMetadata, true)
		if err != nil {
			t.Fatal(err)
		}
		hops = append(hops, redelivery)
		retry, err := factory.Next(redelivery)
		if err != nil {
			t.Fatal(err)
		}
		hops = append(hops, retry)

		scheduledMetadata := map[string]string{}
		scheduledMessage, err := scheduleAdapter.Enqueue(scheduledMetadata, retry)
		if err != nil {
			t.Fatal(err)
		}
		hops = append(hops, scheduledMessage)
		scheduledRun, err := scheduleAdapter.Run(scheduledMetadata, true)
		if err != nil {
			t.Fatal(err)
		}
		hops = append(hops, scheduledRun)

		webhookRequest := httptest.NewRequest(http.MethodPost, "https://example.test/hook", nil)
		webhookSent, err := webhookAdapter.Send(webhookRequest, scheduledRun)
		if err != nil {
			t.Fatal(err)
		}
		hops = append(hops, webhookSent)
		webhookReceiver.ServeHTTP(httptest.NewRecorder(), webhookRequest)
	}))

	request := httptest.NewRequest(http.MethodPost, "https://example.test/ingress", nil)
	request.Header.Set(httpcorrelation.CorrelationHeader, "workflow")
	request.Header.Set(httpcorrelation.RequestHeader, "client-request")
	ingress.ServeHTTP(httptest.NewRecorder(), request)

	want := []struct {
		request   string
		causation string
	}{
		{"http-receive", "client-request"},
		{"rpc-send", "http-receive"},
		{"rpc-receive", "rpc-send"},
		{"queue-message", "rpc-receive"},
		{"queue-attempt-one", "queue-message"},
		{"queue-attempt-two", "queue-message"},
		{"retry-attempt", "queue-attempt-two"},
		{"scheduled-message", "retry-attempt"},
		{"scheduled-run", "scheduled-message"},
		{"webhook-send", "scheduled-run"},
		{"webhook-receive", "webhook-send"},
	}
	if len(hops) != len(want) {
		t.Fatalf("got %d hops, want %d", len(hops), len(want))
	}
	for index, expected := range want {
		if hops[index].CorrelationID.String() != "workflow" ||
			hops[index].RequestID.String() != expected.request ||
			hops[index].CausationID.String() != expected.causation {
			t.Fatalf("hop %d = %#v, want request %q and cause %q", index, hops[index], expected.request, expected.causation)
		}
	}
}
