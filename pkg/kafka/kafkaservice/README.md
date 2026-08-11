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

The producer component rejects new work during stop, joins admitted publish and
readiness callbacks, then invokes the transferred shutdown callback. Concurrent
shutdown callers share one attempt; a failed attempt can be retried, and the
first successful attempt makes later calls idempotent.

Startup is serialized with shutdown. A stop requested during a startup callback
marks the adapter unavailable but does not invoke shutdown until that callback
returns. The stop context bounds this wait; if it expires, a later stop must
resume cleanup. Concurrent start calls are rejected with `ErrUnavailable`, and
a repeated start after successful admission is idempotent.

Application callback panics are recovered at startup, readiness, publish,
handler, run, and shutdown boundaries and returned as secret-safe
`CallbackPanicError` values. Panic values are never retained or formatted.
Startup panic recovery still performs transferred-resource cleanup. A shutdown
panic completes the failed attempt so a later bounded stop can retry.

`NewConsumer` returns a plan containing one component, one supervised task,
and an optional readiness check. Service task cancellation stops intake and
joins handlers before reverse component shutdown closes the consumer. Component
stop also joins any admitted run or readiness callback, so direct concurrent
stop cannot close a resource still used by those callbacks.

See the root module's
[service integration guide](../docs/service-integration.md) for ownership,
ordering, correlation trust, failure behavior, and complete producer and
consumer examples.

## Lifecycle and shutdown sequence

The application constructs and configures every Kafka client before creating
an adapter. A service plan then starts Kafka facilities, producers, and
consumers in dependency order. Lifecycle readiness remains false until startup
succeeds; the service readiness endpoint also runs every optional dependency
check before reporting ready.

Shutdown reverses that order:

```text
withdraw readiness -> cancel consumer intake -> join admitted handlers
-> settle only work eligible under root Kafka cancellation policy
-> leave the group -> close consumers
-> caller policy flushes or aborts and closes owned producers
```

The returned consumer plan uses task cancellation, so only handler work
completed before cancellation remains committable. Applications that want
admitted work to settle during a planned graceful drain must explicitly invoke
the root consumer's drain boundary before canceling the plan task; the adapter
does not insert that application policy.

The adapter does not create a fresh timeout for any step. The caller-owned
service shutdown context is the single budget shared by drain, settlement, and
resource cleanup. If it expires, the returned context or Kafka error is
preserved. A direct caller may resume incomplete cleanup with another
`Component.Stop`; a completed `service.Service` shutdown does not rerun failed
component cleanup. Stop before start is safe, duplicate direct stop after
successful cleanup is a no-op, and concurrent direct stops join one shutdown
attempt.

Producer delivery can remain ambiguous when the shutdown budget expires after
the broker may have accepted a record. The adapter preserves the concrete
Kafka result and error; applications must use stable idempotency keys or
downstream deduplication instead of assuming that an error means "not sent."
Consumer records completed before cancellation may be committed. A record
finishing after cancellation remains eligible for redelivery, creating an
intentional duplicate-processing window.

## Kubernetes SIGTERM

On `SIGTERM`, use `service.Run`, or `service.RunWithSignals` with a
caller-owned channel registered for `SIGTERM`, and set a termination budget
shorter than the pod's `terminationGracePeriodSeconds`. Readiness must be
withdrawn before consumer intake is canceled. Readiness withdrawal begins
endpoint removal and local admission must still reject late traffic;
canceling the consumer task stops fetch and lets Kafka reassign group
partitions. Do not add an independent Kafka shutdown timeout: it would reset
the caller's budget and could outlive the pod. If the grace period expires,
let Kafka redeliver unsettled consumer work and treat unfinished producer
delivery as ambiguous.

## API and adoption

- `NewProducer` returns an inert adapter with `Resource`, `Component`, optional
  `Readiness`, and correlation-aware `Publish` methods.
- `NewConsumer` returns an inert adapter with `Resource` and `Plan`; the plan
  contains one component, one supervised run task, and optional readiness.
- `NewHandler` is available when only the correlation-aware handler boundary
  is needed.

Adopt the module by first constructing root `kafka` clients with application
policy, then supplying explicit callbacks, adding the returned components,
tasks, and checks to one `service.Plan`, and finally routing publication through
the adapter. Keep transaction, retry, offset, rebalance, and broker policy in
the root clients or application.

## Compatibility and migration

This module is pre-v1 and independently versioned. Its module path is
`github.com/faustbrian/golib/pkg/kafka/kafkaservice`; consumers must pin a
released module version and must not rely on the repository workspace or local
`replace` directives.

To migrate an existing service, retain the existing concrete clients and their
policies, move startup/readiness/run/shutdown calls into the matching adapter
callbacks, add the returned lifecycle entries to the service plan, and route
producer calls through `Publish`. Verify termination grace, consumer
redelivery, and producer ambiguity before removing the previous lifecycle
wiring. No payload or broker-policy migration is performed by this adapter.

## FAQ

**Does the adapter retry, commit, or manage transactions?** No. Those policies
remain owned by `kafka` clients and the application.

**Can a producer be shared?** Yes, only when `Shutdown` is omitted and another
explicit owner outlives every user. Consumers cannot be shared across plans.

**Can an application retry every failed publish?** No. A timeout or shutdown
error may have an ambiguous delivery result; retry only with an idempotency or
deduplication strategy.

**What happens when drain exceeds the shutdown deadline?** Admission stays
closed, the context error is returned, unsettled consumer work can be
redelivered, and a later direct `Component.Stop` may resume retryable cleanup.

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

The separate interoperability lane starts the immutable-digest Apache Kafka
4.3.1 fixture and exercises the concrete root producer and consumer through
this adapter. It proves producer startup/readiness/publication/shutdown,
consumer cancellation while a handler is admitted, handler completion before
resource shutdown, settlement completed before cancellation, redelivery of the
record whose handler finishes after cancellation, and rejection of publication
after stop. This fixture does not replace the root module's multi-broker,
rebalance, authentication, transaction, or fault-injection evidence.
