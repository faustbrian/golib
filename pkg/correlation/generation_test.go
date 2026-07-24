package correlation_test

import (
	"errors"
	"regexp"
	"testing"

	correlation "github.com/faustbrian/golib/pkg/correlation"
)

func TestFactoryDefaultsToSecureCanonicalUUIDs(t *testing.T) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	values, err := factory.Start()
	if err != nil {
		t.Fatal(err)
	}
	canonicalV4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !canonicalV4.MatchString(values.CorrelationID.String()) || !canonicalV4.MatchString(values.RequestID.String()) || values.CorrelationID.String() == values.RequestID.String() {
		t.Fatalf("default IDs = %#v", values)
	}
}

type sequenceGenerator struct {
	values []string
	index  int
}

func (generator *sequenceGenerator) New() (string, error) {
	if generator.index >= len(generator.values) {
		return "", errors.New("sequence exhausted")
	}
	value := generator.values[generator.index]
	generator.index++
	return value, nil
}

func TestFactoryStartsAndAdvancesDistinctHops(t *testing.T) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &sequenceGenerator{values: []string{"correlation-1", "request-1", "request-2"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := factory.Start()
	if err != nil {
		t.Fatal(err)
	}
	second, err := factory.Next(first)
	if err != nil {
		t.Fatal(err)
	}

	if first.CorrelationID.String() != "correlation-1" || first.RequestID.String() != "request-1" || first.CausationID.String() != "" {
		t.Fatalf("first hop = %#v", first)
	}
	if second.CorrelationID != first.CorrelationID || second.RequestID.String() != "request-2" || second.CausationID.String() != "request-1" {
		t.Fatalf("second hop = %#v", second)
	}
}

func TestFactoryPreservesOnlyExplicitlyTrustedInboundCorrelation(t *testing.T) {
	factory, err := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &sequenceGenerator{values: []string{"generated-correlation", "request-1", "request-2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	inbound := correlation.Values{CorrelationID: correlation.MustCorrelationID("inbound", correlation.Policy{})}

	untrusted, err := factory.Accept(inbound, correlation.InboundPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := factory.Accept(inbound, correlation.InboundPolicy{TrustCorrelation: true})
	if err != nil {
		t.Fatal(err)
	}

	if untrusted.CorrelationID.String() != "generated-correlation" || untrusted.RequestID.String() != "request-1" {
		t.Fatalf("untrusted inbound = %#v", untrusted)
	}
	if trusted.CorrelationID.String() != "inbound" || trusted.RequestID.String() != "request-2" {
		t.Fatalf("trusted inbound = %#v", trusted)
	}
}

func TestDeterministicStrategyUsesVersionedDomainSeparationAndKeying(t *testing.T) {
	first, err := correlation.NewDeterministic(correlation.DeterministicOptions{
		Domain: "shipment", Version: 1, Key: []byte("secret-a"), Length: 32,
	})
	if err != nil {
		t.Fatal(err)
	}
	same, _ := first.Derive([]byte("business-123"))
	again, _ := first.Derive([]byte("business-123"))
	if same != again || len(same.String()) != 32 {
		t.Fatalf("stable derivation = %q, %q", same, again)
	}

	for _, options := range []correlation.DeterministicOptions{
		{Domain: "shipment", Version: 2, Key: []byte("secret-a"), Length: 32},
		{Domain: "invoice", Version: 1, Key: []byte("secret-a"), Length: 32},
		{Domain: "shipment", Version: 1, Key: []byte("secret-b"), Length: 32},
	} {
		strategy, err := correlation.NewDeterministic(options)
		if err != nil {
			t.Fatal(err)
		}
		derived, err := strategy.Derive([]byte("business-123"))
		if err != nil {
			t.Fatal(err)
		}
		if derived == same {
			t.Fatalf("domain-separated derivation collided for %#v", options)
		}
	}

	if _, err := first.Derive(nil); !errors.Is(err, correlation.ErrInvalidDerivation) {
		t.Fatalf("Derive(nil) error = %v", err)
	}
}
