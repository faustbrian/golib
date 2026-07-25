package correlation_test

import (
	"errors"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
)

type memoryCarrier map[string][]string

func (carrier memoryCarrier) Values(key string) []string {
	return append([]string(nil), carrier[key]...)
}

func (carrier memoryCarrier) Set(key, value string) { carrier[key] = []string{value} }

func TestCarrierRoundTripRejectsConflictAndOverwrite(t *testing.T) {
	codec, err := correlation.NewCodec(correlation.CodecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("flow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("request", correlation.Policy{}),
		CausationID:   correlation.MustCausationID("parent", correlation.Policy{}),
	}
	carrier := memoryCarrier{}
	if err := codec.Inject(carrier, want); err != nil {
		t.Fatal(err)
	}
	got, err := codec.Extract(carrier)
	if err != nil || got != want {
		t.Fatalf("Extract() = %#v, %v", got, err)
	}

	carrier[correlation.DefaultCorrelationField] = []string{"flow", "other"}
	if _, err := codec.Extract(carrier); !errors.Is(err, correlation.ErrConflictingCarrier) {
		t.Fatalf("conflicting Extract() error = %v", err)
	}
	if err := codec.Inject(carrier, want); !errors.Is(err, correlation.ErrCarrierOverwrite) {
		t.Fatalf("overwriting Inject() error = %v", err)
	}
}

func TestCarrierBoundsAndMalformedValues(t *testing.T) {
	codec, err := correlation.NewCodec(correlation.CodecOptions{Policy: correlation.Policy{MaxLength: 8}})
	if err != nil {
		t.Fatal(err)
	}

	for name, carrier := range map[string]memoryCarrier{
		"control":  {correlation.DefaultCorrelationField: {"bad\nvalue"}},
		"oversize": {correlation.DefaultCorrelationField: {"123456789"}},
		"empty":    {correlation.DefaultCorrelationField: {""}},
		"unicode":  {correlation.DefaultCorrelationField: {"föö"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := codec.Extract(carrier); !errors.Is(err, correlation.ErrInvalidCarrier) {
				t.Fatalf("Extract() error = %v", err)
			}
		})
	}
}

func TestPropagatorCreatesHopBeforeInjectionAndOnReceipt(t *testing.T) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &sequenceGenerator{values: []string{"outbound-request", "received-request"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := correlation.NewCodec(correlation.CodecOptions{})
	if err != nil {
		t.Fatal(err)
	}
	propagator, err := correlation.NewPropagator(factory, codec)
	if err != nil {
		t.Fatal(err)
	}
	parent := correlation.Values{
		CorrelationID: correlation.MustCorrelationID("flow", correlation.Policy{}),
		RequestID:     correlation.MustRequestID("parent-request", correlation.Policy{}),
	}
	carrier := memoryCarrier{}
	outbound, err := propagator.Send(carrier, parent)
	if err != nil {
		t.Fatal(err)
	}
	received, err := propagator.Receive(carrier, correlation.InboundPolicy{
		TrustCorrelation: true, TrustRequestAsCausation: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if outbound.CorrelationID != parent.CorrelationID || outbound.RequestID.String() != "outbound-request" || outbound.CausationID.String() != "parent-request" {
		t.Fatalf("outbound = %#v", outbound)
	}
	if received.CorrelationID != parent.CorrelationID || received.RequestID.String() != "received-request" || received.CausationID.String() != "outbound-request" {
		t.Fatalf("received = %#v", received)
	}
}

func TestExternalIdentifierCarriesExplicitTrustAndSource(t *testing.T) {
	external, err := correlation.NewExternalID(correlation.ExternalIDOptions{
		Kind: "shopify_order", Value: "gid_123", Source: "shopify", Trusted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if external.Kind() != "shopify_order" || external.Value() != "gid_123" || external.Source() != "shopify" || !external.Trusted() {
		t.Fatalf("external ID = %#v", external)
	}
	if _, err := correlation.NewExternalID(correlation.ExternalIDOptions{Kind: "tenant", Value: "bad value", Source: "header"}); !errors.Is(err, correlation.ErrInvalidExternalID) {
		t.Fatalf("NewExternalID() error = %v", err)
	}
}
