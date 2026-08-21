# Kafka OpenTelemetry adapter

`gotelemetry` is the independently versioned OpenTelemetry adapter for
[`github.com/faustbrian/golib/pkg/kafka`](../..). The root Kafka module remains
vendor-neutral and does not import OpenTelemetry.

Use this adapter when Kafka policy observations should become traces and
metrics. `Instrumentation` translates only completed, copied, payload-free
`kafka.Observation` values. The separate `TraceContextPropagation` policy can
copy bounded W3C Trace Context fields between explicit Kafka records and
contexts. Neither surface wraps `franz-go`, reimplements Kafka instrumentation,
or installs global OpenTelemetry state.

## Five-minute setup

```go
instrumentation, err := gotelemetry.New(gotelemetry.Config{
    Runtime: telemetryRuntime,
    Attributes: gotelemetry.AttributePolicy{
        AllowedClientIDs:      []string{"orders-producer", "orders-consumer"},
        AllowedTopics:         []string{"orders"},
        AllowedConsumerGroups: []string{"fulfillment"},
    },
})
if err != nil {
    return err
}

observerPolicy := kafka.ObserverPolicy{
    Observers: []kafka.ObserverFunc{instrumentation.Observer()},
    FailureHandler: func(_ context.Context, failure kafka.ObservationFailure) {
        // Report a stable local failure category. Do not export failure.Cause()
        // without application-owned redaction.
    },
    Timeout: 100 * time.Millisecond,
}

producer, err := kafka.NewProducer(kafka.ProducerConfig{
    // Normal producer policy omitted.
    Observers: observerPolicy,
})
```

The same policy can be supplied to `ConsumerConfig`,
`TransactionProcessorConfig`, `ReplayConfig`, and `InspectorConfig`. One
`Instrumentation` is concurrency-safe and starts no goroutines.

Optional record propagation is explicit and independent of observation:

```go
tracePolicy, err := gotelemetry.NewTraceContextPropagation(messageLimits)
if err != nil {
    return err
}

outbound, err := tracePolicy.Inject(ctx, kafka.ProducerRecord{
    Topic: "orders.v1",
    Key:   orderID,
    Value: encodedOrder,
})
if err != nil {
    return err
}
delivery := producer.PublishRecord(ctx, outbound)
```

At a consumer boundary, pass the returned context to the application handler:

```go
handlerContext, err := tracePolicy.Extract(ctx, consumedRecord)
if err != nil {
    return err
}
return applicationHandler.Handle(handlerContext, consumedRecord)
```

Use the same `kafka.MessageLimits` as the producer and consumer. This policy
does not publish, consume, settle, retry, or create spans; those Kafka and
application lifecycles remain explicit.

## Cardinality and data policy

All identity dimensions are denied by default:

- client IDs appear only when exactly allowlisted;
- topics appear only when exactly allowlisted;
- consumer-group IDs appear only when exactly allowlisted; and
- each allowlist is copied, duplicate-free, and limited to 128 valid Kafka
  identities.

The adapter never receives or records keys, values, headers, credentials,
broker endpoints, application error text, or panic values. Partition and
offset coordinates are span-only numeric diagnostics. They are not metric
dimensions. Kafka broker IDs and protocol API keys are bounded numeric
diagnostics.

## Semantic conventions

The adapter maps its standard spans and metrics to the reviewed OpenTelemetry
messaging semantic conventions **1.44.0**, whose messaging signals remain
Development status. The selected version is also available as
`MessagingSemanticConventionVersion`. A future semantic-convention change is
reviewed and documented as a user-visible adapter change; this pre-v1 adapter
does not silently follow the latest website content.

Version 1.44.0 defines separate create, producer-send, client-send, receive,
process, and settle span models. The completion observer can implement only the
settle shape shown below. Produce completion does not prove that a
record reached the broker-send attempt, expose the creation context injected
before publication, or support the required creation-context link. The root
poll observation covers polling, handlers, and settlement rather than
receive-only timing and status. Record and batch observations can also be
emitted before a handler starts, and failed observations do not distinguish
pre-handler rejection from handler failure. Those observations therefore use
adapter-owned spans and do not emit unproved standard send, receive, or process
spans or durations.

The adapter cannot authoritatively emit `messaging.kafka.cluster.id`: root
operation observations do not carry cluster metadata, and the adapter does not
perform a hidden administrative lookup or trust a caller assertion. Cluster
inspection remains available through the root inspector.

This is a policy-controlled mapping, not a claim that every recommended or
conditionally required attribute is always present. In particular,
`messaging.destination.name`, `messaging.client.id`, and
`messaging.consumer.group.name` remain absent unless explicitly allowlisted;
bounded cardinality and data minimization take precedence over automatic
identity export.

| Kafka observation | Span | Standard metric |
| --- | --- | --- |
| produce record | `kafka producer.publish_completion`, `INTERNAL` | adapter-owned policy metrics only |
| produce batch | `kafka producer.publish_batch_completion`, `INTERNAL` | adapter-owned policy metrics only |
| produce async | `kafka producer.publish_async_completion`, `INTERNAL` | adapter-owned policy metrics only |
| consume poll | `kafka consumer.poll_cycle`, `INTERNAL` | `messaging.client.consumed.messages` plus adapter-owned policy metrics |
| consume record | `kafka consumer.record_completion`, `INTERNAL` | adapter-owned policy metrics only |
| consume batch | `kafka consumer.batch_completion`, `INTERNAL` | adapter-owned policy metrics only |
| consume commit | `commit [topic]`, `CLIENT` | `messaging.client.operation.duration` |
| broker connect | `kafka broker.connect`, `CLIENT` | adapter-owned policy metrics only |
| broker request | `kafka broker.request`, `CLIENT` | adapter-owned policy and request metrics |
| broker throttle | `kafka broker.throttle`, `INTERNAL` | adapter-owned policy and throttle metrics |
| broker disconnect | `kafka broker.disconnect`, `INTERNAL` | adapter-owned policy metrics only |
| consume assigned | `kafka consumer.assigned`, `INTERNAL` | adapter-owned policy metrics only |
| consume revoked | `kafka consumer.revoked`, `INTERNAL` | adapter-owned policy metrics only |
| consume lost | `kafka consumer.lost`, `INTERNAL` | adapter-owned policy metrics only |
| consume blocked | `kafka consumer.rebalance_blocked`, `INTERNAL` | adapter-owned policy metrics only |
| consume group error | `kafka consumer.group_error`, `INTERNAL` | adapter-owned policy metrics only |
| consume retry scheduled | `kafka consumer.retry_scheduled`, `INTERNAL` | adapter-owned policy metrics only |
| consume rebalance wait | `kafka consumer.rebalance_wait`, `INTERNAL` | adapter-owned policy metrics only |
| transaction begin | `kafka transaction.begin`, `INTERNAL` | adapter-owned policy metrics only |
| transaction commit | `kafka transaction.commit`, `CLIENT` | adapter-owned policy metrics only |
| transaction abort | `kafka transaction.abort`, `CLIENT` | adapter-owned policy metrics only |
| replay plan | `kafka replay.plan`, `CLIENT` | adapter-owned policy metrics only |
| replay record processed | `process [topic]`, `CONSUMER` | `messaging.process.duration` |
| replay record skipped or failed | `kafka replay.record`, `INTERNAL` | adapter-owned policy metrics only |
| replay run | `kafka replay.run`, `INTERNAL` | adapter-owned policy metrics only |
| replay shutdown | `kafka replay.shutdown`, `INTERNAL` | adapter-owned policy metrics only |
| inspector cluster | `kafka inspector.cluster`, `CLIENT` | adapter-owned policy metrics only |
| inspector topics | `kafka inspector.topics`, `CLIENT` | adapter-owned policy metrics only |
| inspector consumer groups | `kafka inspector.consumer_groups`, `CLIENT` | adapter-owned policy metrics only |
| dependency health | `kafka inspector.dependency_health`, `CLIENT` | adapter-owned policy metrics only |
| readiness | `kafka inspector.readiness`, `INTERNAL` | adapter-owned policy metrics only |
| inspector shutdown | `kafka inspector.shutdown`, `INTERNAL` | adapter-owned policy metrics only |
| producer shutdown | `kafka producer.shutdown`, `INTERNAL` | adapter-owned policy metrics only |
| consumer shutdown | `kafka consumer.shutdown`, `INTERNAL` | adapter-owned policy metrics only |
| transaction-processor shutdown | `kafka transaction_processor.shutdown`, `INTERNAL` | adapter-owned policy metrics only |

The optional `[topic]` suffix on standard messaging spans is present only for
an allowlisted topic. Failed operations set a generic error span status and the
root package's stable, low-cardinality `error.type`; they do not record an
exception or error message.

Every root observation emits:

- `kafka.client.operations`;
- `kafka.client.operation.duration`.

Applicable broker-request or throttle observations additionally emit:

- `kafka.client.request.size` for request and response bytes;
- `kafka.client.request.queue.duration`; and
- `kafka.client.throttle.duration`.

These `kafka.*` metrics are adapter-owned policy metrics, not OpenTelemetry
messaging semantic conventions. They never carry client, topic, or consumer
group identities.
Retry-scheduled observations increment only the adapter-owned operation and
duration metrics. They do not increment semantic consumed-message or process
metrics because the event records a retry decision before backoff, not another
completed receive or processing operation.
Rebalance-wait observations likewise use only adapter-owned operation and
duration metrics. Their interval ends at local poll-gate release, callback
cancellation, or timeout and does not represent complete broker rebalance
duration.
Replay plan, record, and run spans also carry fixed
`kafka.replay.processed`, `kafka.replay.skipped`, `kafka.replay.failed`, and
`kafka.replay.remaining` signed-64-bit attributes. Source topic is exported
only when explicitly allowlisted; partition and offset remain span-only.
The adapter does not emit `messaging.client.consumed.messages` from replay
outcomes because replay currently has no separate observation that proves
delivery to the application exactly once.
Inspector spans use only adapter-owned `kafka.*` attributes. Bounded broker,
topic, consumer-group, member, and partition counts are present when non-zero;
dependency and readiness spans include the fixed health, decision, and
hysteresis fields. Inspected identities and broker-controlled descriptive
metadata are never attributes.
Broker-connect spans add the bounded adapter-owned
`kafka.authentication.method` attribute. It identifies only the configured
SASL method; no credential, token, username, certificate, endpoint, or distinct
authentication latency is exported.

## Trace timing and propagation boundary

The root observer runs after a Kafka policy operation completes. The adapter
therefore creates and ends the span synchronously during the callback while
using `Observation.StartedAt` and `Observation.Duration` as the recorded span
interval. The callback context remains the span parent when it contains an
ambient trace context.

The completion-only observer seam cannot inject a producer span context into a
record, extract one from consumed headers, or create the per-message links
recommended for batch receive and processing. `TraceContextPropagation` is the
separate reviewed record-header policy: it uses a fixed W3C Trace Context
propagator, never a process-global or caller-supplied propagator, and excludes
baggage and arbitrary headers.

Injection validates before copying, deep-copies the record, removes stale
`traceparent` and `tracestate` keys under ASCII case-insensitive comparison,
injects current fields, and validates the result again. Non-ASCII Kafka header
keys are never treated as W3C fields. If the context has no valid span context,
stale W3C fields remain removed and none are added. Extraction validates the
borrowed record and does not mutate or retain it. An invalid `traceparent`, or
duplicate W3C fields under the same comparison, preserves the supplied parent
context; an invalid `tracestate` is ignored according to the OpenTelemetry W3C
propagator contract. Added fields remain subject to header count, key, value,
and aggregate byte limits. Trace state is transported as Kafka metadata and is
never made a telemetry attribute by this adapter; applications must still
ensure vendor trace-state values are appropriate for every broker and
downstream trust boundary.

The interoperability gate publishes an injected record through the root
producer, consumes and settles it through the root consumer, and extracts the
same remote span context after a pinned Apache Kafka 4.3.1 broker preserves the
headers. This proves the Kafka-to-Kafka propagation boundary only; it does not
prove tracing for external side effects or make the completion observer
propagate.

## Failure and lifecycle behavior

`Config.Validate` and `AttributePolicy.Validate` perform validation without
constructing instruments. `New` creates all instruments synchronously and
returns a redacted `InstrumentError` if a provider rejects any instrument.
The provider error remains available through `errors.Is`/`errors.As` for
intentional local handling.

`Observer` rejects nil or canceled contexts and delegates public observation
validation to `kafka.Observation.Validate`. Recording is synchronous at the
OpenTelemetry API boundary. Export and queue limits remain the responsibility
of the configured OpenTelemetry SDK and exporter. Configure the root
`ObserverPolicy` deadline and SDK queues so telemetry cannot become an
unbounded Kafka delivery or rebalance delay.

No-op providers accept observations and emit nothing. A sampled-out tracer
emits no span while metric recording remains independent. After SDK shutdown,
provider instruments follow OpenTelemetry's no-op behavior and the adapter
still preserves the Kafka result. Provider callbacks are synchronous and can
consume the observer deadline, so providers must cooperate with cancellation
and exporters should use bounded queues. A provider panic is contained as the
stable `ErrProviderPanic` category without retaining its panic value; signals
recorded before that panic may already have been accepted. Provider shutdown
remains caller-owned.

An in-process Go callback cannot be forcibly preempted. A provider that ignores
cancellation can exceed producer, consumer, rebalance, or shutdown deadlines;
isolating such a provider requires a process boundary. Applications must use
trusted, cancellation-cooperative providers and bounded SDK/exporter queues.
The adapter starts no goroutine to race or abandon a blocked provider call.

## Metric reference

All counters are monotonic. Durations use seconds, sizes use bytes, and counts
use brace units. Partition, offset, record/count/size, broker, health, and
replay diagnostics never become metric dimensions. Broker-request metrics may
add the bounded numeric protocol API key and fixed request direction.

| Metric | Unit | Value and dimensions |
| --- | --- | --- |
| `messaging.client.operation.duration` | `s` | settle duration with operation type plus allowlisted destination and consumer group |
| `messaging.process.duration` | `s` | successful replay processing duration with allowlisted destination and consumer group |
| `messaging.client.consumed.messages` | `{message}` | records delivered by a poll with allowlisted destination and consumer group |
| `kafka.client.operations` | `{operation}` | one per observation with bounded operation, outcome, and error category |
| `kafka.client.operation.duration` | `s` | stable observation duration with the same bounded dimensions as the operation counter |
| `kafka.client.request.size` | `By` | broker request and response bytes with fixed direction and optional numeric API key |
| `kafka.client.request.queue.duration` | `s` | broker-request queue duration with optional numeric API key |
| `kafka.client.throttle.duration` | `s` | broker throttle duration with a bounded after-response boolean |

Histogram boundaries are explicit and versioned in `New`. Contract tests
assert every unit, counter monotonicity flag, and complete boundary list.

## Attribute reference and privacy

Every span has `kafka.operation` and `kafka.outcome`. Failed operations add the
root package's bounded `error.type`; error messages and exception events are
never emitted. Standard messaging spans add `messaging.system`,
`messaging.operation.name`, and `messaging.operation.type`.

| Attribute | Surface and cardinality |
| --- | --- |
| `messaging.client.id`, `kafka.client.id` | span-only exact client-ID allowlist |
| `messaging.destination.name` | span and applicable standard-metric exact topic allowlist |
| `kafka.topic` | span-only exact topic allowlist |
| `messaging.consumer.group.name` | span and applicable standard-metric exact consumer-group allowlist |
| `kafka.consumer.group` | span-only exact consumer-group allowlist |
| `messaging.destination.partition.id` | standard-span-only numeric partition coordinate |
| `messaging.kafka.offset` | single-record standard-span numeric offset |
| `kafka.partition` | adapter-span-only numeric partition coordinate |
| `kafka.offset` | single-record adapter-span numeric offset |
| `messaging.batch.message_count` | standard-span-only bounded count for batch settlement |
| `kafka.broker.id` | span-only bounded numeric broker diagnostic |
| `kafka.protocol.api_key` | span-only and broker-request metric bounded numeric protocol key |
| `kafka.authentication.method` | span-only fixed configured SASL method |
| `kafka.request.bytes`, `kafka.response.bytes`, `kafka.request.queue.duration`, `kafka.throttle.duration` | span-only protocol timing and size diagnostics |
| `kafka.throttled_after_response` | span-only and throttle-metric fixed boolean |
| `kafka.record.count`, `kafka.partition.count`, `kafka.record.processed_count`, `kafka.record.committed_count`, `kafka.record.size` | span-only bounded record diagnostics |
| `kafka.broker.count`, `kafka.topic.count`, `kafka.consumer_group.count`, `kafka.consumer_group.member.count` | span-only bounded inspector counts |
| `kafka.replay.processed`, `kafka.replay.skipped`, `kafka.replay.failed`, `kafka.replay.remaining` | span-only signed-64-bit replay progress |
| `kafka.dependency.healthy`, `kafka.readiness.ready`, `kafka.readiness.consecutive_failures`, `kafka.readiness.consecutive_successes` | span-only bounded health and hysteresis state |
| `kafka.observation.truncated` | span-only fixed boolean when root diagnostics were clipped |
| `kafka.request.direction` | request-size metric only; fixed `request` or `response` |

Keys, values, record headers, credentials, usernames, tokens, certificates,
broker endpoints, application error text, and panic values are outside the
observation contract. W3C trace header values are used only by
`TraceContextPropagation`; they never become telemetry attributes.
Adapter-owned metrics are identity-free. Standard-metric identity tuples are
bounded by the applicable topic and consumer-group allowlist sizes; client IDs
remain span-only because the pinned standard metric schemas do not include
client identity.

## API reference

- `New(Config)` validates the runtime and copied policy, constructs every
  instrument synchronously, and returns immutable instrumentation.
- `Instrumentation.Observer()` returns the synchronous `kafka.ObserverFunc`.
- `Config.Validate()` and `AttributePolicy.Validate()` validate without
  constructing instruments.
- `InstrumentError` exposes `ErrInstrumentCreation` and preserves the provider
  cause through `errors.Is` and `errors.As` without rendering it.
- `NewTraceContextPropagation(kafka.MessageLimits)` constructs the immutable
  record-header policy. `Inject` returns an owned producer record and `Extract`
  returns a context containing only valid remote W3C Trace Context.
- `MessagingSemanticConventionVersion` exposes the pinned convention version.

## Migration

This module is pre-v1. Span names, kinds, attributes, metric names, units,
boundaries, propagation validation, and the convention pin are user-visible
migration surfaces. Upgrade OpenTelemetry and this adapter together, review
the changelog, update dashboards and sampling rules, and revalidate allowlists.
When migrating from direct franz-go hooks, remove duplicate client
instrumentation first. Existing applications gain no propagation until they
explicitly inject and extract records with `TraceContextPropagation`.

## FAQ

### Does the observer propagate trace context?

No. Completion occurs after the record boundary. Use the explicit header
policy before publication and at the consumer handler boundary.

### Does the adapter install or shut down global OpenTelemetry state?

No. Providers and their shutdown remain caller-owned.

### Can an exporter delay Kafka work?

Yes. Provider API callbacks are synchronous. Use a bounded root observer
deadline, cancellation-cooperative providers, and bounded exporter queues.

### Are identities or payloads exported by default?

No. Identity allowlists default to empty, and payload-bearing data is absent
from `kafka.Observation`.

### What happens after SDK shutdown or when a trace is sampled out?

The provider determines emission. Sampled-out spans do not suppress metrics,
and shut-down or no-op providers do not turn a Kafka success into a failure.

## Verification

Observable interpretation choices are recorded in the
[specification decision register](docs/specification-decisions.md), with exact
upstream pins in `specification/manifest.json`.

```sh
make check
```

The module gate covers formatting, vet, unit tests, race detection, exact
statement coverage, fuzz smoke, benchmarks, and documentation. Repository
release gates additionally enforce mutation, API compatibility, security,
vulnerability, license, SBOM, provenance, and clean-consumer checks.
