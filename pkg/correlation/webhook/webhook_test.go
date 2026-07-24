package webhook_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	httpcorrelation "github.com/faustbrian/golib/pkg/correlation/http"
	webhookcorrelation "github.com/faustbrian/golib/pkg/correlation/webhook"
)

type generator struct{ values []string }

func (generator *generator) New() (string, error) {
	value := generator.values[0]
	generator.values = generator.values[1:]
	return value, nil
}

func TestNewRejectsInvalidHTTPOptions(t *testing.T) {
	if _, err := webhookcorrelation.New(nil, webhookcorrelation.Options{}); !errors.Is(err, httpcorrelation.ErrInvalidOptions) {
		t.Fatalf("New(nil) error = %v", err)
	}
}

func TestWebhookSendAndTrustedReceiptPreserveCausation(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &generator{values: []string{"webhook-request", "receiver-request"}},
	})
	adapter, err := webhookcorrelation.New(factory, webhookcorrelation.Options{
		Trust: func(*http.Request) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "https://receiver.example/hook", nil)
	parent := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("flow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("queue-request", correlation.Policy{}),
	}
	sent, err := adapter.Send(request, parent)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	adapter.Wrap(http.HandlerFunc(func(_ http.ResponseWriter, received *http.Request) {
		values, _ := correlation.FromContext(received.Context())
		if values.CorrelationID != parent.CorrelationID || values.CausationID.String() != "webhook-request" || values.RequestID.String() != "receiver-request" {
			t.Fatalf("received = %#v", values)
		}
	})).ServeHTTP(response, request)
	if sent.CausationID.String() != "queue-request" {
		t.Fatalf("sent = %#v", sent)
	}
}
