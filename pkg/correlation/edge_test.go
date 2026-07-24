package correlation

import (
	"context"
	"errors"
	"strings"
	"testing"

	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
)

type edgeCarrier map[string][]string

type foreignContextKey string

func nilContext() context.Context { return nil }

func (carrier edgeCarrier) Values(key string) []string { return carrier[key] }
func (carrier edgeCarrier) Set(key, value string)      { carrier[key] = []string{value} }

type failingGenerator struct {
	values []string
	err    error
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("entropy") }

func (generator *failingGenerator) New() (string, error) {
	if generator.err != nil {
		return "", generator.err
	}
	value := generator.values[0]
	generator.values = generator.values[1:]
	return value, nil
}

func TestPolicyAndTypedIDFailureBoundaries(t *testing.T) {
	if _, err := ParseRequestID("bad value", Policy{}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("ParseRequestID() error = %v", err)
	}
	if _, err := ParseCausationID("bad value", Policy{}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("ParseCausationID() error = %v", err)
	}
	for name, call := range map[string]func(){
		"correlation": func() { MustCorrelationID("", Policy{}) },
		"request":     func() { MustRequestID("", Policy{}) },
		"causation":   func() { MustCausationID("", Policy{}) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			call()
		})
	}
	if _, ok := FromContext(nilContext()); ok {
		t.Fatal("nil context contained values")
	}
	if err := validatePolicy(Policy{MaxLength: -1}); err == nil {
		t.Fatal("negative policy accepted")
	}
}

func TestCodecRejectsInvalidConfigurationAndNilCarriers(t *testing.T) {
	for _, options := range []CodecOptions{
		{Policy: Policy{MaxLength: -1}},
		{CorrelationField: "bad field"},
		{CorrelationField: "same", RequestField: "same"},
	} {
		if _, err := NewCodec(options); !errors.Is(err, ErrInvalidCarrier) {
			t.Fatalf("NewCodec(%#v) error = %v", options, err)
		}
	}
	codec, _ := NewCodec(CodecOptions{})
	var nilCarrier edgeCarrier
	if _, err := codec.Extract(nilCarrier); !errors.Is(err, ErrInvalidCarrier) {
		t.Fatalf("Extract(nil) error = %v", err)
	}
	if err := codec.Inject(nilCarrier, Values{}); !errors.Is(err, ErrInvalidCarrier) {
		t.Fatalf("Inject(nil) error = %v", err)
	}
	if _, err := (*Codec)(nil).Extract(edgeCarrier{}); !errors.Is(err, ErrInvalidCarrier) {
		t.Fatalf("nil codec Extract() error = %v", err)
	}
	if err := (*Codec)(nil).Inject(edgeCarrier{}, Values{}); !errors.Is(err, ErrInvalidCarrier) {
		t.Fatalf("nil codec Inject() error = %v", err)
	}
}

func TestCodecExhaustsCarrierPrecedenceAndBounds(t *testing.T) {
	codec, _ := NewCodec(CodecOptions{})
	identical := edgeCarrier{DefaultCorrelationField: {"same", "same"}}
	values, err := codec.Extract(identical)
	if err != nil || values.CorrelationID.String() != "same" {
		t.Fatalf("identical duplicates = %#v, %v", values, err)
	}
	tooMany := edgeCarrier{DefaultCorrelationField: make([]string, 9)}
	if _, err := codec.Extract(tooMany); !errors.Is(err, ErrInvalidCarrier) {
		t.Fatalf("too many values error = %v", err)
	}
	for field, conflicting := range map[string]edgeCarrier{
		"request":   {DefaultRequestField: {"one", "two"}},
		"causation": {DefaultCausationField: {"one", "two"}},
	} {
		if _, err := codec.Extract(conflicting); !errors.Is(err, ErrConflictingCarrier) {
			t.Fatalf("conflicting %s error = %v", field, err)
		}
	}
	for field, invalid := range map[string]edgeCarrier{
		"request":   {DefaultRequestField: {"bad value"}},
		"causation": {DefaultCausationField: {"bad value"}},
	} {
		if _, err := codec.Extract(invalid); !errors.Is(err, ErrInvalidCarrier) {
			t.Fatalf("%s error = %v", field, err)
		}
	}
	invalid := Values{CorrelationID: CorrelationID("bad value")}
	if err := codec.Inject(edgeCarrier{}, invalid); !errors.Is(err, ErrInvalidCarrier) {
		t.Fatalf("invalid injection error = %v", err)
	}
	empty := edgeCarrier{}
	if err := codec.Inject(empty, Values{}); err != nil || len(empty) != 0 {
		t.Fatalf("empty injection = %v, %v", empty, err)
	}
	if !nilLike(nil) || nilLike(42) || !nilLike(([]string)(nil)) || nilLike([]string{}) {
		t.Fatal("nilLike classification is incorrect")
	}
}

func TestEveryCarrierFieldRejectsHostileValueClasses(t *testing.T) {
	codec, _ := NewCodec(CodecOptions{})
	if values, err := codec.Extract(edgeCarrier{}); err != nil || values != (Values{}) {
		t.Fatalf("absent carrier = %#v, %v", values, err)
	}

	fields := map[string]string{
		"correlation": DefaultCorrelationField,
		"request":     DefaultRequestField,
		"causation":   DefaultCausationField,
	}
	for semantic, field := range fields {
		t.Run(semantic, func(t *testing.T) {
			valid, err := codec.Extract(edgeCarrier{field: {"valid", "valid"}})
			if err != nil {
				t.Fatalf("identical duplicates: %v", err)
			}
			if valid.CorrelationID == "" && valid.RequestID == "" && valid.CausationID == "" {
				t.Fatal("identical duplicates were discarded")
			}

			for name, test := range map[string]struct {
				values []string
				want   error
			}{
				"empty":        {[]string{""}, ErrInvalidCarrier},
				"conflicting":  {[]string{"valid", "other"}, ErrConflictingCarrier},
				"malformed":    {[]string{"bad value"}, ErrInvalidCarrier},
				"oversized":    {[]string{strings.Repeat("a", 129)}, ErrInvalidCarrier},
				"noncanonical": {[]string{"value.with.dot"}, ErrInvalidCarrier},
				"unicode":      {[]string{"válid"}, ErrInvalidCarrier},
				"control":      {[]string{"valid\x00"}, ErrInvalidCarrier},
				"too-many":     {make([]string, 9), ErrInvalidCarrier},
			} {
				t.Run(name, func(t *testing.T) {
					if _, err := codec.Extract(edgeCarrier{field: test.values}); !errors.Is(err, test.want) {
						t.Fatalf("Extract() error = %v, want %v", err, test.want)
					}
				})
			}
		})
	}
}

func TestFactoryReportsEveryGenerationFailure(t *testing.T) {
	if _, err := NewFactory(FactoryOptions{Policy: Policy{MaxLength: -1}}); !errors.Is(err, ErrInvalidFactory) {
		t.Fatalf("invalid factory error = %v", err)
	}
	function := GeneratorFunc(func() (string, error) { return "value", nil })
	if value, err := function.New(); err != nil || value != "value" {
		t.Fatalf("GeneratorFunc.New() = %q, %v", value, err)
	}

	factory, _ := NewFactory(FactoryOptions{Generator: &failingGenerator{err: errors.New("entropy")}})
	if _, err := factory.Start(); !errors.Is(err, ErrGeneration) {
		t.Fatalf("Start entropy error = %v", err)
	}
	factory, _ = NewFactory(FactoryOptions{Generator: &failingGenerator{values: []string{"bad value"}}})
	if _, err := factory.Start(); !errors.Is(err, ErrGeneration) {
		t.Fatalf("Start invalid generated value error = %v", err)
	}
	count := 0
	factory, _ = NewFactory(FactoryOptions{Generator: GeneratorFunc(func() (string, error) {
		count++
		if count == 1 {
			return "flow", nil
		}
		return "", errors.New("entropy")
	})})
	if _, err := factory.Start(); !errors.Is(err, ErrGeneration) {
		t.Fatalf("partial Start error = %v", err)
	}
	if _, err := (*Factory)(nil).Start(); !errors.Is(err, ErrGeneration) {
		t.Fatalf("nil Start error = %v", err)
	}
	identifierGenerator := &uuidGenerator{generator: identifieruuid.NewV4Generator(errorReader{})}
	if _, err := identifierGenerator.New(); err == nil {
		t.Fatal("UUID entropy failure was hidden")
	}
}

func TestFactoryRejectsMalformedParentsAndTrustedInbound(t *testing.T) {
	factory, _ := NewFactory(FactoryOptions{Generator: &failingGenerator{values: []string{"request"}}})
	if _, err := factory.Next(Values{}); !errors.Is(err, ErrGeneration) {
		t.Fatalf("empty parent error = %v", err)
	}
	if _, err := factory.Next(Values{CorrelationID: "flow"}); !errors.Is(err, ErrGeneration) {
		t.Fatalf("empty parent request error = %v", err)
	}
	factory, _ = NewFactory(FactoryOptions{Generator: &failingGenerator{err: errors.New("entropy")}})
	if _, err := factory.Next(Values{CorrelationID: "flow", RequestID: "parent"}); !errors.Is(err, ErrGeneration) {
		t.Fatalf("next request generation error = %v", err)
	}
	if _, err := factory.Accept(Values{CorrelationID: "bad value"}, InboundPolicy{TrustCorrelation: true}); !errors.Is(err, ErrGeneration) {
		t.Fatalf("invalid trusted correlation error = %v", err)
	}
	factory, _ = NewFactory(FactoryOptions{Generator: &failingGenerator{err: errors.New("entropy")}})
	if _, err := factory.Accept(Values{}, InboundPolicy{}); !errors.Is(err, ErrGeneration) {
		t.Fatalf("accept correlation generation error = %v", err)
	}
	count := 0
	factory, _ = NewFactory(FactoryOptions{Generator: GeneratorFunc(func() (string, error) {
		count++
		if count == 1 {
			return "flow", nil
		}
		return "", errors.New("entropy")
	})})
	if _, err := factory.Accept(Values{}, InboundPolicy{}); !errors.Is(err, ErrGeneration) {
		t.Fatalf("accept request generation error = %v", err)
	}
	factory, _ = NewFactory(FactoryOptions{Generator: &failingGenerator{values: []string{"request"}}})
	if _, err := factory.Accept(Values{CorrelationID: "flow", RequestID: "bad value"}, InboundPolicy{TrustCorrelation: true, TrustRequestAsCausation: true}); !errors.Is(err, ErrGeneration) {
		t.Fatalf("invalid trusted request error = %v", err)
	}
}

func TestDeterministicAndDisclosureSecurityBoundaries(t *testing.T) {
	for _, options := range []DeterministicOptions{
		{},
		{Domain: strings.Repeat("a", 129), Version: 1, Length: 24},
		{Domain: "bad domain", Version: 1, Length: 24},
		{Domain: "domain", Version: 1, Length: 1},
		{Domain: "domain", Version: 1, Length: 100},
		{Domain: "domain", Version: 1, Length: 24, Key: make([]byte, 1025)},
	} {
		if _, err := NewDeterministic(options); !errors.Is(err, ErrInvalidDerivation) {
			t.Fatalf("NewDeterministic(%#v) error = %v", options, err)
		}
	}
	strategy, _ := NewDeterministic(DeterministicOptions{Domain: "domain", Version: 1, Length: 24})
	if _, err := (*Deterministic)(nil).Derive([]byte("value")); !errors.Is(err, ErrInvalidDerivation) {
		t.Fatalf("nil strategy error = %v", err)
	}
	if _, err := strategy.Derive(make([]byte, 1<<20+1)); !errors.Is(err, ErrInvalidDerivation) {
		t.Fatalf("oversized input error = %v", err)
	}

	for _, test := range []struct {
		policy DisclosurePolicy
		err    bool
	}{
		{DisclosurePolicy{}, false},
		{DisclosurePolicy{Mode: HashDisclosure}, true},
		{DisclosurePolicy{Mode: HashDisclosure, Key: []byte("key")}, false},
		{DisclosurePolicy{Mode: ExposeDisclosure}, false},
		{DisclosurePolicy{Mode: 99}, true},
		{DisclosurePolicy{Key: make([]byte, 1025)}, true},
	} {
		value, err := Disclose("correlation.id", "value", test.policy)
		if (err != nil) != test.err {
			t.Fatalf("Disclose(%#v) = %q, %v", test.policy, value, err)
		}
	}
	if value, err := Disclose("correlation.id", "", DisclosurePolicy{}); err != nil || value != "" {
		t.Fatalf("empty Disclose() = %q, %v", value, err)
	}
}

func TestExternalAndPropagatorInvalidBoundaries(t *testing.T) {
	for _, options := range []ExternalIDOptions{
		{},
		{Kind: "kind", Value: "value", Source: "bad source"},
		{Kind: "kind", Value: "bad value", Source: "source"},
	} {
		if _, err := NewExternalID(options); !errors.Is(err, ErrInvalidExternalID) {
			t.Fatalf("NewExternalID(%#v) error = %v", options, err)
		}
	}
	codec, _ := NewCodec(CodecOptions{})
	factory, _ := NewFactory(FactoryOptions{Generator: &failingGenerator{values: []string{"request"}}})
	if _, err := NewPropagator(nil, codec); !errors.Is(err, ErrInvalidPropagator) {
		t.Fatalf("nil factory error = %v", err)
	}
	if _, err := NewPropagator(factory, nil); !errors.Is(err, ErrInvalidPropagator) {
		t.Fatalf("nil codec error = %v", err)
	}
	if _, err := (*Propagator)(nil).Send(edgeCarrier{}, Values{}); !errors.Is(err, ErrInvalidPropagator) {
		t.Fatalf("nil Send error = %v", err)
	}
	if _, err := (*Propagator)(nil).Receive(edgeCarrier{}, InboundPolicy{}); !errors.Is(err, ErrInvalidPropagator) {
		t.Fatalf("nil Receive error = %v", err)
	}
	propagator, _ := NewPropagator(factory, codec)
	if _, err := propagator.Send(edgeCarrier{}, Values{}); !errors.Is(err, ErrGeneration) {
		t.Fatalf("invalid parent Send error = %v", err)
	}
	if _, err := propagator.Receive(edgeCarrier{DefaultCorrelationField: {"bad value"}}, InboundPolicy{}); !errors.Is(err, ErrInvalidCarrier) {
		t.Fatalf("invalid Receive error = %v", err)
	}
	factory, _ = NewFactory(FactoryOptions{Generator: &failingGenerator{values: []string{"child"}}})
	propagator, _ = NewPropagator(factory, codec)
	occupied := edgeCarrier{DefaultCorrelationField: {"occupied"}}
	if _, err := propagator.Send(occupied, Values{CorrelationID: "flow", RequestID: "parent"}); !errors.Is(err, ErrCarrierOverwrite) {
		t.Fatalf("Send overwrite error = %v", err)
	}
}

func TestContextForeignValueCannotCollide(t *testing.T) {
	ctx := context.WithValue(context.Background(), foreignContextKey("correlation"), Values{CorrelationID: "foreign"})
	if _, ok := FromContext(ctx); ok {
		t.Fatal("foreign context key collided")
	}
}
