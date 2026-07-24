package queue_test

import (
	"errors"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	queuecorrelation "github.com/faustbrian/golib/pkg/correlation/queue"
)

type generator struct{ values []string }

func (generator *generator) New() (string, error) {
	value := generator.values[0]
	generator.values = generator.values[1:]
	return value, nil
}

func TestAdapterRejectsInvalidOptionsAndHandlesAbsentMetadata(t *testing.T) {
	if _, err := queuecorrelation.New(nil, queuecorrelation.Options{}); !errors.Is(err, queuecorrelation.ErrInvalidOptions) {
		t.Fatalf("New(nil) error = %v", err)
	}
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{})
	if _, err := queuecorrelation.New(factory, queuecorrelation.Options{Codec: correlation.CodecOptions{Policy: correlation.Policy{MaxLength: -1}}}); err == nil {
		t.Fatal("invalid codec accepted")
	}
	adapter, _ := queuecorrelation.New(factory, queuecorrelation.Options{})
	if _, err := adapter.Send(nil, correlation.Values{}); !errors.Is(err, queuecorrelation.ErrInvalidOptions) {
		t.Fatalf("Send(nil) error = %v", err)
	}
	if _, err := (*queuecorrelation.Adapter)(nil).Send(map[string]string{}, correlation.Values{}); !errors.Is(err, queuecorrelation.ErrInvalidOptions) {
		t.Fatalf("nil adapter Send() error = %v", err)
	}
	if _, err := (*queuecorrelation.Adapter)(nil).Receive(nil, false); !errors.Is(err, queuecorrelation.ErrInvalidOptions) {
		t.Fatalf("nil adapter Receive() error = %v", err)
	}
	if values, err := adapter.Receive(nil, false); err != nil || values.CorrelationID == "" || values.RequestID == "" {
		t.Fatalf("Receive(nil) = %#v, %v", values, err)
	}
}

func TestRedeliveryPreservesWorkflowAndCreatesAttemptIDs(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &generator{values: []string{"message-request", "attempt-one", "attempt-two"}},
	})
	adapter, err := queuecorrelation.New(factory, queuecorrelation.Options{})
	if err != nil {
		t.Fatal(err)
	}
	producer := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("flow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("producer-request", correlation.Policy{}),
	}
	metadata := map[string]string{}
	message, err := adapter.Send(metadata, producer)
	if err != nil {
		t.Fatal(err)
	}
	first, err := adapter.Receive(metadata, true)
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Receive(metadata, true)
	if err != nil {
		t.Fatal(err)
	}

	if message.RequestID.String() != "message-request" || message.CausationID.String() != "producer-request" {
		t.Fatalf("message = %#v", message)
	}
	if first.CorrelationID != producer.CorrelationID || first.RequestID.String() != "attempt-one" || first.CausationID.String() != "message-request" {
		t.Fatalf("first delivery = %#v", first)
	}
	if second.CorrelationID != producer.CorrelationID || second.RequestID.String() != "attempt-two" || second.CausationID.String() != "message-request" {
		t.Fatalf("redelivery = %#v", second)
	}
}
