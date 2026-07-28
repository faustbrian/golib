# Service integration

`kafkaservice` connects concrete Kafka resources to
`github.com/faustbrian/golib/pkg/service` without hiding the Kafka API or moving
topic, partition, retry, transaction, settlement, or business-handler policy
out of `kafka`.

## Producer ownership

`NewProducer` accepts a concrete resource plus explicit startup, readiness,
publish, and shutdown callbacks. The adapter returns that exact resource
through `Resource`.

- `Startup` is optional and performs one caller-bounded validation before
  publish admission.
- `Readiness` is optional. The caller decides whether to include the returned
  check in `service.Plan.Readiness`.
- omitting `Shutdown` keeps the resource shared;
- providing `Shutdown` transfers flush and close ownership after successful
  startup.

During service stop, the producer rejects new adapter publishes, waits for
every admitted publish callback, then invokes the owned shutdown callback.
If the service context expires while draining, the transport remains open and
a later stop may resume cleanup. Shutdown attempts are serialized. Concurrent
callers observe the same attempt, a failed attempt can be retried, and the
first successful attempt makes later calls idempotent. Startup failure begins
owned-resource cleanup and returns a secret-safe `StartupError` that preserves
both causes through `errors.Is` and `errors.As`; incomplete cleanup remains
retryable.

All application callbacks are panic-contained. Startup, readiness, publish,
handler, run, and shutdown panics return `CallbackPanicError`, whose
`Operation` identifies the boundary without retaining or formatting the panic
value. A startup panic still begins transferred-resource cleanup. A shutdown
panic completes that failed attempt rather than stranding waiters, so a later
bounded stop can retry.

Concrete Kafka composition remains explicit:

```go
adapter, err := kafkaservice.NewProducer(
    kafkaservice.ProducerOptions[*kafka.Producer]{
        Name:        "tracking-events-producer",
        Resource:    producer,
        Correlation: factory,
        Startup: func(ctx context.Context, producer *kafka.Producer) error {
            return producer.Health(ctx)
        },
        Readiness: func(ctx context.Context, producer *kafka.Producer) error {
            return producer.Health(ctx)
        },
        Publish: func(
            ctx context.Context,
            producer *kafka.Producer,
            record kafka.ProducerRecord,
        ) (kafka.DeliveryResult, error) {
            result := producer.PublishRecord(ctx, record)
            return result, result.Err
        },
        Shutdown: func(ctx context.Context, producer *kafka.Producer) error {
            return producer.Shutdown(ctx)
        },
    },
)
```

`Producer.Publish` requires `correlation.Values` in its context. It creates a
child record hop, copies the record and all bytes, and injects the correlation,
request, and causation fields through the correlation package's codec.
Application-supplied values for those reserved fields are rejected rather than
trusted or overwritten. The concrete publish callback receives the child
values in context for logging and telemetry.

`ProducerOptions.MessageLimits` is validated at construction. Its zero value
uses `kafka.DefaultMessageLimits`. Every record is validated before copying and
again after propagation adds headers, so oversized caller records do not cause
an adapter copy and metadata cannot bypass the Kafka record policy.

Set `TracePropagator` to an explicitly constructed OpenTelemetry
`propagation.TextMapPropagator` to inject trace context. The adapter removes
that propagator's fields from the owned record copy before injection, so stale
caller values cannot win. Nil disables trace propagation. No global provider
or propagator is read or installed.

The concrete publish callback must retain Kafka's own record validation,
delivery timeout, retry, and error-classification behavior. The adapter does
not replace those policies.

## Consumer ownership

`NewConsumer` requires an explicit handler, run callback, and shutdown callback.
Shutdown ownership always transfers because one Kafka consumer cannot support
competing active runners safely.

```go
adapter, err := kafkaservice.NewConsumer(
    kafkaservice.ConsumerOptions[*kafka.Consumer]{
        Name:            "tracking-events-consumer",
        Resource:        consumer,
        Correlation:     factory,
        TrustedMetadata: true,
        TracePropagator: propagation.TraceContext{},
        Handler:         applicationHandler,
        Run: func(
            ctx context.Context,
            consumer *kafka.Consumer,
            handler kafka.Handler,
        ) error {
            return consumer.Run(ctx, handler)
        },
        Shutdown: func(ctx context.Context, consumer *kafka.Consumer) error {
            return consumer.Shutdown(ctx)
        },
    },
)
plan := adapter.Plan()
```

For a long-running service command, merge the returned components, tasks, and
optional readiness check into the application plan. The service first cancels
the consumer task. `Consumer.Run` stops polling and joins admitted handlers.
Only after the task returns does reverse component shutdown call
`Consumer.Shutdown`, which leaves the group and closes the client within the
service shutdown context.

Every delivery creates a distinct request ID. Set `TrustedMetadata` only after
authenticating the immediate Kafka boundary through deployment ACLs and the
application's trust policy. Trusted valid metadata preserves the correlation
ID and turns the producer request ID into causation. The safe default ignores
inbound identity and starts a new local workflow.

Malformed or conflicting correlation fields are replaced with a new workflow
by default. `RejectInvalidMetadata` makes them fail before application code.
Trace extraction is independent and occurs only through the configured
caller-owned propagator. Correlation trust does not establish trace, baggage,
authentication, authorization, tenancy, or idempotency trust. The adapter
deep-copies bounded consumed headers before invoking a trace propagator, so
propagator mutation cannot change the borrowed record delivered to the
application.

## Ordering and failure policy

Declare facilities before adapters that depend on them:

```text
start: Kafka facilities -> producer -> consumer
stop:  consumer task join -> consumer close -> producer drain -> facilities
```

Actual ordering follows the application plan and reverse component shutdown.
Do not share one consumer across plans. Shared producers are allowed only when
their adapter omits shutdown and another explicit owner outlives all users.

Adapter construction failures use `ErrInvalidOptions`. Inactive and draining
resources return `ErrUnavailable`; outbound work without a parent workflow
returns `ErrMissingCorrelation`; recovered callback panics unwrap to
`ErrCallbackPanic`. Correlation and Kafka failures preserve their own
classifications. The adapter performs no implicit retry and never exposes
panic values or error causes in its own startup error text.
