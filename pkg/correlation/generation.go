package correlation

import (
	"errors"
	"fmt"

	identifieruuid "github.com/faustbrian/golib/pkg/identifier/uuid"
)

var (
	// ErrInvalidFactory reports invalid factory configuration.
	ErrInvalidFactory = errors.New("correlation: invalid factory")
	// ErrGeneration reports a generator failure or invalid generated value.
	ErrGeneration = errors.New("correlation: generation failed")
)

// Generator supplies canonical random identifier text. It is structurally
// compatible with identifier.Generator[string] from identifier.
type Generator interface {
	New() (string, error)
}

// GeneratorFunc adapts a function to Generator.
type GeneratorFunc func() (string, error)

// New calls the wrapped function.
func (function GeneratorFunc) New() (string, error) { return function() }

// FactoryOptions configure an immutable Factory.
type FactoryOptions struct {
	Policy    Policy
	Generator Generator
}

// InboundPolicy explicitly identifies which inbound semantics cross a trust
// boundary. Request IDs are never preserved because each hop gets a new one.
type InboundPolicy struct {
	TrustCorrelation        bool
	TrustRequestAsCausation bool
}

// Factory creates fresh hop identifiers without global mutable state.
type Factory struct {
	policy    Policy
	generator Generator
}

// NewFactory constructs a factory. The default generator uses crypto/rand.
func NewFactory(options FactoryOptions) (*Factory, error) {
	if err := validatePolicy(options.Policy); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFactory, err)
	}
	if options.Generator == nil {
		options.Generator = &uuidGenerator{generator: identifieruuid.NewV4Generator(nil)}
	}
	return &Factory{policy: options.Policy, generator: options.Generator}, nil
}

type uuidGenerator struct{ generator *identifieruuid.V4Generator }

func (generator *uuidGenerator) New() (string, error) {
	id, err := generator.generator.New()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// Start creates a new correlation and request identifier.
func (factory *Factory) Start() (Values, error) {
	correlationID, err := factory.newCorrelationID()
	if err != nil {
		return Values{}, err
	}
	requestID, err := factory.newRequestID()
	if err != nil {
		return Values{}, err
	}
	return Values{CorrelationID: correlationID, RequestID: requestID}, nil
}

// Next preserves correlation, creates a request ID, and makes the prior
// request the immediate cause.
func (factory *Factory) Next(parent Values) (Values, error) {
	correlationID, err := ParseCorrelationID(parent.CorrelationID.String(), factory.policy)
	if err != nil {
		return Values{}, fmt.Errorf("%w: parent correlation: %w", ErrGeneration, err)
	}
	causationID, err := ParseCausationID(parent.RequestID.String(), factory.policy)
	if err != nil {
		return Values{}, fmt.Errorf("%w: parent request: %w", ErrGeneration, err)
	}
	requestID, err := factory.newRequestID()
	if err != nil {
		return Values{}, err
	}
	return Values{
		CorrelationID: correlationID,
		RequestID:     requestID,
		CausationID:   causationID,
	}, nil
}

// Accept starts a receiving hop. No inbound value is used unless its exact
// semantic trust is enabled.
func (factory *Factory) Accept(inbound Values, policy InboundPolicy) (Values, error) {
	var correlationID CorrelationID
	var err error
	if policy.TrustCorrelation && inbound.CorrelationID != "" {
		correlationID, err = ParseCorrelationID(inbound.CorrelationID.String(), factory.policy)
		if err != nil {
			return Values{}, fmt.Errorf("%w: inbound correlation: %w", ErrGeneration, err)
		}
	} else {
		correlationID, err = factory.newCorrelationID()
		if err != nil {
			return Values{}, err
		}
	}

	requestID, err := factory.newRequestID()
	if err != nil {
		return Values{}, err
	}
	values := Values{CorrelationID: correlationID, RequestID: requestID}
	if policy.TrustRequestAsCausation && inbound.RequestID != "" {
		values.CausationID, err = ParseCausationID(inbound.RequestID.String(), factory.policy)
		if err != nil {
			return Values{}, fmt.Errorf("%w: inbound request: %w", ErrGeneration, err)
		}
	}
	return values, nil
}

func (factory *Factory) newCorrelationID() (CorrelationID, error) {
	value, err := factory.generate()
	if err != nil {
		return "", err
	}
	return ParseCorrelationID(value, factory.policy)
}

func (factory *Factory) newRequestID() (RequestID, error) {
	value, err := factory.generate()
	if err != nil {
		return "", err
	}
	return ParseRequestID(value, factory.policy)
}

func (factory *Factory) generate() (string, error) {
	if factory == nil || factory.generator == nil {
		return "", fmt.Errorf("%w: nil factory", ErrGeneration)
	}
	value, err := factory.generator.New()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrGeneration, err)
	}
	if err := validate(value, factory.policy); err != nil {
		return "", fmt.Errorf("%w: %w", ErrGeneration, err)
	}
	return value, nil
}

func validatePolicy(policy Policy) error {
	maximum := policy.MaxLength
	if maximum == 0 {
		maximum = defaultMaxLength
	}
	if maximum < 1 || maximum > 1024 {
		return fmt.Errorf("maximum length must be between 1 and 1024")
	}
	return nil
}
