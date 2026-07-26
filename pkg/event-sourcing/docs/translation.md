# Anti-corruption translation

`TranslatorChain` adapts deliveries at an explicit integration boundary. It is
not part of aggregate reconstitution and never rewrites stored history.

Each ordered `DeliveryTranslator` receives one delivery and returns:

- no deliveries to explicitly drop it;
- one delivery to filter or translate it; or
- multiple deliveries to split it while preserving order.

Every result must be a valid persisted delivery with the same live or replay
mode as its input. Changing replay intent is rejected so a translator cannot
silently route historical events into a live side-effect path. Output is
bounded by `MaxTranslatedDeliveries`, contexts are checked between calls, and
callback panics become redacted `TranslationError` values with the exact stage
and stage-input index.

```go
chain, err := eventsourcing.NewTranslatorChain(
	eventsourcing.TranslatorChainConfig{
		MaxDeliveries: 16,
		Translators: []eventsourcing.DeliveryTranslator{
			eventsourcing.DeliveryTranslatorFunc(
				func(
					ctx context.Context,
					delivery eventsourcing.Delivery,
				) ([]eventsourcing.Delivery, error) {
					if delivery.Message().Event().Name().String() ==
						"internal.debugged" {
						return nil, nil
					}
					return []eventsourcing.Delivery{delivery}, nil
				},
			),
		},
	},
)
```

Applications decide where the chain is applied before a custom dispatcher,
queue adapter, or other integration consumer. The library does not install a
global translator or automatically execute one during replay.

Use the narrower evolution mechanisms for persisted history:

- JSON aliases map historical names to registered canonical names while
  decoding;
- upcasters deterministically evolve payload, metadata, name, and schema
  version at the read boundary; and
- integration translators adapt deliveries after successful persistence.

Translator implementations own their determinism, thread safety, and any
application-specific identity mapping. Replay translation must remain
side-effect free.
