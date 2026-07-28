# Kafka service integration

`kafkaservice` is the independently versioned integration between
[`github.com/faustbrian/golib/pkg/kafka`](..) and
`github.com/faustbrian/golib/pkg/service`. The root Kafka production package
does not import service, correlation, or OpenTelemetry APIs.

The adapter keeps concrete Kafka resources visible. It owns only service
lifecycle composition, correlation record boundaries, and optional
caller-owned trace propagation. Topic, partition, delivery, retry, settlement,
transaction, replay, and dead-letter policy remain in the root Kafka package
or the application.

Producer records are validated against `MessageLimits` before any byte slice is
copied and again after correlation and trace headers are added. The zero option
uses `kafka.DefaultMessageLimits`. Component, task, and readiness names are
valid UTF-8 and bounded by `MaxNameBytes`.

## Five-minute setup

```go
adapter, err := kafkaservice.NewProducer(
	kafkaservice.ProducerOptions[*kafka.Producer]{
		Name:        "orders-producer",
		Resource:    producer,
		Correlation: correlationFactory,
		Publish: func(
			ctx context.Context,
			producer *kafka.Producer,
			record kafka.ProducerRecord,
		) (kafka.DeliveryResult, error) {
			result := producer.PublishRecord(ctx, record)
			return result, result.Err
		},
		Shutdown: func(
			ctx context.Context,
			producer *kafka.Producer,
		) error {
			return producer.Shutdown(ctx)
		},
	},
)
if err != nil {
	return err
}

component := adapter.Component()
```

The producer component rejects new work during stop, joins admitted publish
callbacks, then invokes the transferred shutdown callback. Concurrent shutdown
callers share one attempt; a failed attempt can be retried, and the first
successful attempt makes later calls idempotent.

Application callback panics are recovered at startup, readiness, publish,
handler, run, and shutdown boundaries and returned as secret-safe
`CallbackPanicError` values. Panic values are never retained or formatted.
Startup panic recovery still performs transferred-resource cleanup. A shutdown
panic completes the failed attempt so a later bounded stop can retry.

`NewConsumer` returns a plan containing one component, one supervised task,
and an optional readiness check. Service task cancellation stops intake and
joins handlers before reverse component shutdown closes the consumer.

See the root module's
[service integration guide](../docs/service-integration.md) for ownership,
ordering, correlation trust, failure behavior, and complete producer and
consumer examples.

## Security and observability

Inbound correlation metadata is untrusted by default. Enabling trusted
metadata requires an application-owned ACL and trust decision. Malformed
metadata is replaced unless rejection is explicitly configured.

Trace propagation is disabled by default. A caller may provide an explicit
OpenTelemetry `TextMapPropagator`; the adapter never reads or installs a global
provider or propagator. Consumed headers are deep-copied before any caller-owned
propagator can inspect them, so a mutating propagator cannot alter the borrowed
record delivered to the application. Kafka keys, values, arbitrary headers,
credentials, broker endpoints, and callback error text are not logged or
exported by this module.

## Verification

```sh
make check
```

The module contract covers formatting, vet, unit tests, race detection, exact
statement coverage, fuzz smoke, allocation-reporting benchmarks, and
documentation. Repository gates additionally enforce mutation, API
compatibility, security, vulnerability, licenses, SBOM, and clean-consumer
checks.
