package eventsourcing

import (
	"context"
)

// MaxTranslatedDeliveries is the largest configured split output accepted at
// one translation stage.
const MaxTranslatedDeliveries = 1_000

// DeliveryTranslator explicitly adapts one integration delivery to zero, one,
// or many deliveries. Returning no deliveries is an intentional drop.
//
// Implementations must preserve DeliveryMode, be deterministic for equivalent
// input, retain no borrowed values without copying, and remain side-effect
// free when used for replay.
type DeliveryTranslator interface {
	Translate(context.Context, Delivery) ([]Delivery, error)
}

// DeliveryTranslatorFunc adapts a function to DeliveryTranslator.
type DeliveryTranslatorFunc func(
	context.Context,
	Delivery,
) ([]Delivery, error)

// Translate implements DeliveryTranslator.
func (translator DeliveryTranslatorFunc) Translate(
	ctx context.Context,
	delivery Delivery,
) ([]Delivery, error) {
	if translator == nil {
		return nil, invalid("translator", "must be assigned")
	}
	return translator(ctx, delivery)
}

// TranslatorChainConfig declares an ordered immutable anti-corruption chain.
// A zero MaxDeliveries selects MaxTranslatedDeliveries.
type TranslatorChainConfig struct {
	Translators   []DeliveryTranslator
	MaxDeliveries int
}

// TranslatorChain applies explicit integration translation outside aggregate
// replay. It starts no goroutines and is safe for concurrent use when its
// translators are safe.
type TranslatorChain struct {
	translators   []DeliveryTranslator
	maxDeliveries int
}

// TranslationError reports the exact failed stage and stage-input index
// without exposing event identity, payload, metadata, callback diagnostics, or
// panic values.
type TranslationError struct {
	cause error
	stage int
	input int
}

// Error implements error with a stable redacted diagnostic.
func (*TranslationError) Error() string {
	return ErrTranslationFailed.Error()
}

// Unwrap preserves stable categories and the original cause.
func (err *TranslationError) Unwrap() []error {
	if err.cause == nil {
		return []error{ErrTranslationFailed}
	}
	return []error{ErrTranslationFailed, err.cause}
}

// Stage returns the zero-based translator index.
func (err *TranslationError) Stage() int {
	return err.stage
}

// Input returns the zero-based delivery index within the stage.
func (err *TranslationError) Input() int {
	return err.input
}

// NewTranslatorChain validates and owns one ordered translator chain.
func NewTranslatorChain(
	config TranslatorChainConfig,
) (*TranslatorChain, error) {
	if len(config.Translators) == 0 {
		return nil, invalid("translators", "must not be empty")
	}
	if config.MaxDeliveries == 0 {
		config.MaxDeliveries = MaxTranslatedDeliveries
	}
	if config.MaxDeliveries < 1 ||
		config.MaxDeliveries > MaxTranslatedDeliveries {
		return nil, invalid(
			"max_deliveries",
			"must be within the supported bound",
		)
	}
	translators := append(
		[]DeliveryTranslator(nil),
		config.Translators...,
	)
	for _, translator := range translators {
		if translator == nil {
			return nil, invalid("translator", "must be assigned")
		}
	}
	return &TranslatorChain{
		translators:   translators,
		maxDeliveries: config.MaxDeliveries,
	}, nil
}

// Translate applies every stage in declaration order. Each stage receives the
// preceding stage's ordered output. An empty result is an explicit drop.
func (chain *TranslatorChain) Translate(
	ctx context.Context,
	delivery Delivery,
) ([]Delivery, error) {
	if ctx == nil {
		return nil, invalid("context", "must be assigned")
	}
	if chain == nil ||
		len(chain.translators) == 0 ||
		chain.maxDeliveries < 1 {
		return nil, invalid("translator_chain", "must be assigned")
	}
	if delivery.IsZero() {
		return nil, invalid("delivery", "must be assigned")
	}
	current := []Delivery{delivery}
	for stage, translator := range chain.translators {
		next := make([]Delivery, 0, len(current))
		for input, candidate := range current {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			output, err := callTranslator(ctx, translator, candidate)
			if err != nil {
				return nil, translationFailure(stage, input, err)
			}
			if len(output) > chain.maxDeliveries-len(next) {
				return nil, translationFailure(
					stage,
					input,
					ErrTranslationLimit,
				)
			}
			for _, translated := range output {
				if translated.IsZero() ||
					translated.Mode() != candidate.Mode() {
					return nil, translationFailure(
						stage,
						input,
						ErrInvalidTranslation,
					)
				}
			}
			next = append(next, output...)
		}
		current = next
	}
	return append([]Delivery(nil), current...), nil
}

func callTranslator(
	ctx context.Context,
	translator DeliveryTranslator,
	delivery Delivery,
) (translated []Delivery, err error) {
	defer func() {
		if recover() != nil {
			translated = nil
			err = ErrTranslatorPanic
		}
	}()
	return translator.Translate(ctx, delivery)
}

func translationFailure(stage int, input int, cause error) error {
	return &TranslationError{
		cause: cause,
		stage: stage,
		input: input,
	}
}
