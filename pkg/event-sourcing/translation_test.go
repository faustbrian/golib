package eventsourcing_test

import (
	"context"
	"errors"
	"testing"

	eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"
)

func TestTranslatorChainPreservesOrderedSplitAndDrop(t *testing.T) {
	_, message := persistedLifecycleMessage(
		t,
		"account.opened",
		1,
		1,
		[]byte("{}"),
	)
	first, err := eventsourcing.NewDelivery(
		message,
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatal(err)
	}
	_, secondMessage := persistedLifecycleMessage(
		t,
		"account.renamed",
		1,
		2,
		[]byte("{}"),
	)
	second, err := eventsourcing.NewDelivery(
		secondMessage,
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	chain, err := eventsourcing.NewTranslatorChain(
		eventsourcing.TranslatorChainConfig{
			Translators: []eventsourcing.DeliveryTranslator{
				eventsourcing.DeliveryTranslatorFunc(func(
					_ context.Context,
					delivery eventsourcing.Delivery,
				) ([]eventsourcing.Delivery, error) {
					order = append(order, delivery.Message().Event().Name().String())
					return []eventsourcing.Delivery{delivery, second}, nil
				}),
				eventsourcing.DeliveryTranslatorFunc(func(
					_ context.Context,
					delivery eventsourcing.Delivery,
				) ([]eventsourcing.Delivery, error) {
					name := delivery.Message().Event().Name().String()
					order = append(order, name)
					if name == "account.opened" {
						return nil, nil
					}
					return []eventsourcing.Delivery{delivery}, nil
				}),
			},
		},
	)
	if err != nil {
		t.Fatalf("NewTranslatorChain() error = %v", err)
	}
	translated, err := chain.Translate(context.Background(), first)
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if len(translated) != 1 ||
		translated[0].Message().ID() != second.Message().ID() ||
		translated[0].Mode() != eventsourcing.DeliveryLive {
		t.Fatalf("Translate() = %#v", translated)
	}
	wantOrder := []string{
		"account.opened",
		"account.opened",
		"account.renamed",
	}
	if len(order) != len(wantOrder) {
		t.Fatalf("translation order = %v", order)
	}
	for index := range wantOrder {
		if order[index] != wantOrder[index] {
			t.Fatalf("translation order = %v", order)
		}
	}
	translated[0] = eventsourcing.Delivery{}
	again, err := chain.Translate(context.Background(), first)
	if err != nil || again[0].IsZero() {
		t.Fatalf("Translate() retained result storage: %#v, %v", again, err)
	}
}

func TestTranslatorChainContainsAndRedactsFailures(t *testing.T) {
	delivery := translationDelivery(t)
	applicationErr := errors.New("translation failed with secret")
	tests := map[string]struct {
		translator eventsourcing.DeliveryTranslator
		want       error
	}{
		"error": {
			translator: eventsourcing.DeliveryTranslatorFunc(func(
				context.Context,
				eventsourcing.Delivery,
			) ([]eventsourcing.Delivery, error) {
				return nil, applicationErr
			}),
			want: applicationErr,
		},
		"panic": {
			translator: eventsourcing.DeliveryTranslatorFunc(func(
				context.Context,
				eventsourcing.Delivery,
			) ([]eventsourcing.Delivery, error) {
				panic("secret")
			}),
			want: eventsourcing.ErrTranslatorPanic,
		},
		"zero output": {
			translator: eventsourcing.DeliveryTranslatorFunc(func(
				context.Context,
				eventsourcing.Delivery,
			) ([]eventsourcing.Delivery, error) {
				return []eventsourcing.Delivery{{}}, nil
			}),
			want: eventsourcing.ErrInvalidTranslation,
		},
		"changed mode": {
			translator: eventsourcing.DeliveryTranslatorFunc(func(
				_ context.Context,
				delivery eventsourcing.Delivery,
			) ([]eventsourcing.Delivery, error) {
				changed, err := eventsourcing.NewDelivery(
					delivery.Message(),
					eventsourcing.DeliveryReplay,
				)
				return []eventsourcing.Delivery{changed}, err
			}),
			want: eventsourcing.ErrInvalidTranslation,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			chain, err := eventsourcing.NewTranslatorChain(
				eventsourcing.TranslatorChainConfig{
					Translators: []eventsourcing.DeliveryTranslator{
						translationIdentity(),
						test.translator,
					},
				},
			)
			if err != nil {
				t.Fatalf("NewTranslatorChain() error = %v", err)
			}
			_, translateErr := chain.Translate(
				context.Background(),
				delivery,
			)
			var failure *eventsourcing.TranslationError
			if !errors.Is(translateErr, eventsourcing.ErrTranslationFailed) ||
				!errors.Is(translateErr, test.want) ||
				!errors.As(translateErr, &failure) ||
				failure.Stage() != 1 ||
				failure.Input() != 0 ||
				translateErr.Error() != eventsourcing.ErrTranslationFailed.Error() {
				t.Fatalf("Translate() error = %#v", translateErr)
			}
		})
	}
}

func TestTranslatorChainValidatesConfigurationAndBounds(t *testing.T) {
	valid := []eventsourcing.DeliveryTranslator{translationIdentity()}
	for name, config := range map[string]eventsourcing.TranslatorChainConfig{
		"empty": {},
		"negative bound": {
			Translators:   valid,
			MaxDeliveries: -1,
		},
		"excessive bound": {
			Translators:   valid,
			MaxDeliveries: eventsourcing.MaxTranslatedDeliveries + 1,
		},
		"nil translator": {
			Translators: []eventsourcing.DeliveryTranslator{nil},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := eventsourcing.NewTranslatorChain(config); !errors.Is(
				err,
				eventsourcing.ErrInvalidArgument,
			) {
				t.Fatalf("NewTranslatorChain() error = %v", err)
			}
		})
	}
	translators := []eventsourcing.DeliveryTranslator{translationIdentity()}
	chain, err := eventsourcing.NewTranslatorChain(
		eventsourcing.TranslatorChainConfig{
			Translators:   translators,
			MaxDeliveries: 1,
		},
	)
	if err != nil {
		t.Fatalf("NewTranslatorChain() error = %v", err)
	}
	translators[0] = nil
	if _, err := chain.Translate(
		context.Background(),
		translationDelivery(t),
	); err != nil {
		t.Fatalf("Translate() after caller mutation error = %v", err)
	}

	chain, err = eventsourcing.NewTranslatorChain(
		eventsourcing.TranslatorChainConfig{
			Translators: []eventsourcing.DeliveryTranslator{
				eventsourcing.DeliveryTranslatorFunc(func(
					_ context.Context,
					delivery eventsourcing.Delivery,
				) ([]eventsourcing.Delivery, error) {
					return []eventsourcing.Delivery{delivery, delivery}, nil
				}),
			},
			MaxDeliveries: 1,
		},
	)
	if err != nil {
		t.Fatalf("NewTranslatorChain() error = %v", err)
	}
	_, err = chain.Translate(context.Background(), translationDelivery(t))
	if !errors.Is(err, eventsourcing.ErrTranslationLimit) {
		t.Fatalf("Translate(limit) error = %v", err)
	}
}

func TestTranslatorChainValidatesCallsAndCancellation(t *testing.T) {
	delivery := translationDelivery(t)
	zeroFailure := &eventsourcing.TranslationError{}
	if unwrapped := zeroFailure.Unwrap(); len(unwrapped) != 1 ||
		!errors.Is(zeroFailure, eventsourcing.ErrTranslationFailed) {
		t.Fatalf("zero TranslationError unwrap = %v", unwrapped)
	}
	if _, err := (eventsourcing.DeliveryTranslatorFunc(nil)).Translate(
		context.Background(),
		delivery,
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("nil translator function error = %v", err)
	}
	chain, err := eventsourcing.NewTranslatorChain(
		eventsourcing.TranslatorChainConfig{
			Translators: []eventsourcing.DeliveryTranslator{
				translationIdentity(),
			},
		},
	)
	if err != nil {
		t.Fatalf("NewTranslatorChain() error = %v", err)
	}
	var nilContext context.Context
	if _, err := chain.Translate(nilContext, delivery); !errors.Is(
		err,
		eventsourcing.ErrInvalidArgument,
	) {
		t.Fatalf("Translate(nil context) error = %v", err)
	}
	if _, err := chain.Translate(
		context.Background(),
		eventsourcing.Delivery{},
	); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
		t.Fatalf("Translate(zero) error = %v", err)
	}
	for _, invalid := range []*eventsourcing.TranslatorChain{
		nil,
		{},
	} {
		if _, err := invalid.Translate(
			context.Background(),
			delivery,
		); !errors.Is(err, eventsourcing.ErrInvalidArgument) {
			t.Fatalf("invalid chain Translate() error = %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := chain.Translate(ctx, delivery); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("Translate(cancelled) error = %v", err)
	}
}

func translationIdentity() eventsourcing.DeliveryTranslator {
	return eventsourcing.DeliveryTranslatorFunc(func(
		_ context.Context,
		delivery eventsourcing.Delivery,
	) ([]eventsourcing.Delivery, error) {
		return []eventsourcing.Delivery{delivery}, nil
	})
}

func translationDelivery(t *testing.T) eventsourcing.Delivery {
	t.Helper()
	_, message := persistedLifecycleMessage(
		t,
		"account.opened",
		1,
		1,
		[]byte("{}"),
	)
	delivery, err := eventsourcing.NewDelivery(
		message,
		eventsourcing.DeliveryLive,
	)
	if err != nil {
		t.Fatal(err)
	}
	return delivery
}
