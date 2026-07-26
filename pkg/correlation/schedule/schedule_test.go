package schedule_test

import (
	"errors"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
	queuecorrelation "github.com/faustbrian/golib/pkg/correlation/queue"
	schedulecorrelation "github.com/faustbrian/golib/pkg/correlation/schedule"
)

type generator struct{ values []string }

func (generator *generator) New() (string, error) {
	value := generator.values[0]
	generator.values = generator.values[1:]
	return value, nil
}

func TestScheduledMetadataAndInvalidBoundaries(t *testing.T) {
	if _, err := schedulecorrelation.New(nil, schedulecorrelation.Options{}); !errors.Is(err, schedulecorrelation.ErrInvalidOptions) {
		t.Fatalf("New(nil) error = %v", err)
	}
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{})
	if _, err := schedulecorrelation.New(factory, schedulecorrelation.Options{Queue: queuecorrelation.Options{Codec: correlation.CodecOptions{Policy: correlation.Policy{MaxLength: -1}}}}); err == nil {
		t.Fatal("invalid queue codec accepted")
	}
	if _, err := (*schedulecorrelation.Adapter)(nil).Start(); !errors.Is(err, schedulecorrelation.ErrInvalidOptions) {
		t.Fatalf("nil Start() error = %v", err)
	}
	if _, err := (*schedulecorrelation.Adapter)(nil).Enqueue(nil, correlation.Values{}); !errors.Is(err, schedulecorrelation.ErrInvalidOptions) {
		t.Fatalf("nil Enqueue() error = %v", err)
	}
	if _, err := (*schedulecorrelation.Adapter)(nil).Run(nil, false); !errors.Is(err, schedulecorrelation.ErrInvalidOptions) {
		t.Fatalf("nil Run() error = %v", err)
	}

	adapter, _ := schedulecorrelation.New(factory, schedulecorrelation.Options{})
	parent, _ := adapter.Start()
	metadata := map[string]string{}
	message, err := adapter.Enqueue(metadata, parent)
	if err != nil {
		t.Fatal(err)
	}
	run, err := adapter.Run(metadata, true)
	if err != nil || run.CorrelationID != parent.CorrelationID || run.CausationID.String() != message.RequestID.String() {
		t.Fatalf("scheduled metadata run = %#v, %v", run, err)
	}
}

func TestScheduledRunsDoNotDeriveCorrelationImplicitly(t *testing.T) {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &generator{values: []string{"run-one-flow", "run-one-request", "run-two-flow", "run-two-request"}},
	})
	adapter, err := schedulecorrelation.New(factory, schedulecorrelation.Options{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := adapter.Start()
	if err != nil {
		t.Fatal(err)
	}
	second, err := adapter.Start()
	if err != nil {
		t.Fatal(err)
	}
	if first.CorrelationID == second.CorrelationID || first.RequestID == second.RequestID || first.CausationID != "" || second.CausationID != "" {
		t.Fatalf("scheduled runs = %#v, %#v", first, second)
	}
}
