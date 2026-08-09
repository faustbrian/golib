# Kafka OpenTelemetry adapter

`gotelemetry` is the independently versioned OpenTelemetry adapter for
[`github.com/faustbrian/golib/pkg/kafka`](../..). The root Kafka module remains
vendor-neutral and does not import OpenTelemetry.

Use this adapter when Kafka policy observations should become traces and
metrics. It translates only completed, copied, payload-free `kafka.Observation`
values. It does not wrap `franz-go`, reimplement Kafka instrumentation, inject
headers, extract message creation contexts, or claim producer-to-consumer trace
propagation.

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

The adapter emits OpenTelemetry messaging semantic conventions **1.43.0**,
whose messaging spans and metrics remain Development status. The selected
version is also available as `MessagingSemanticConventionVersion`. A future
semantic-convention change is reviewed and documented as a user-visible
adapter change; this pre-v1 adapter does not silently follow the latest
website content.

| Kafka observation | Span | Standard metric |
| --- | --- | --- |
| produce record, batch, async | `send [topic]`, `PRODUCER` | `messaging.client.operation.duration`, `messaging.client.sent.messages` |
| consume poll | `poll [topic]`, `CLIENT` | `messaging.client.operation.duration`, `messaging.client.consumed.messages` |
| consume record or batch | `process [topic]`, `CONSUMER` | `messaging.process.duration` |
| consume commit | `commit [topic]`, `CLIENT` | `messaging.client.operation.duration` |
| consume retry scheduled | `kafka consumer.retry_scheduled`, `INTERNAL` | adapter-owned policy metrics only |
| successfully processed replay record | `process [topic]`, `CONSUMER` | `messaging.process.duration` |
| skipped or failed replay record | `kafka replay.record`, `CLIENT` | adapter-owned policy metrics only |
| replay plan, run, shutdown | `kafka replay.*`, `CLIENT` | adapter-owned policy metrics only |
| inspector cluster, topics, consumer groups | `kafka inspector.*`, `CLIENT` | adapter-owned policy metrics only |
| dependency health, readiness, inspector shutdown | `kafka inspector.*`, `CLIENT` | adapter-owned policy metrics only |
| producer, consumer, transaction-processor shutdown | `kafka *.shutdown`, `CLIENT` | adapter-owned policy metrics only |

The optional `[topic]` suffix is present only for an allowlisted topic. Failed
operations set a generic error span status and the root package's stable,
low-cardinality `error.type`; they do not record an exception or error message.

Every root observation also emits:

- `kafka.client.operations`;
- `kafka.client.operation.duration`;
- `kafka.client.request.size` for request and response bytes;
- `kafka.client.request.queue.duration`; and
- `kafka.client.throttle.duration`.

These `kafka.*` metrics are adapter-owned policy metrics, not OpenTelemetry
messaging semantic conventions.
Retry-scheduled observations increment only the adapter-owned operation and
duration metrics. They do not increment semantic consumed-message or process
metrics because the event records a retry decision before backoff, not another
completed receive or processing operation.
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

This completion-only seam cannot inject a producer span context into a record
or extract one from consumed headers. It also cannot create the per-message
links recommended for batch receive and processing. Applications that require
cross-message propagation must use a separately reviewed record-header policy;
arbitrary header export is intentionally absent.

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

## Verification

```sh
make check
```

The module gate covers formatting, vet, unit tests, race detection, exact
statement coverage, fuzz smoke, benchmarks, and documentation. Repository
release gates additionally enforce mutation, API compatibility, security,
vulnerability, license, SBOM, provenance, and clean-consumer checks.
