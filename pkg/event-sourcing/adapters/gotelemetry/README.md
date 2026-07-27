# Event sourcing OpenTelemetry adapter

`gotelemetry` is the independently versioned observability boundary for
`event-sourcing`. The event-sourcing core does not import OpenTelemetry or the
repository `telemetry` module.

This adapter instruments synchronous dispatch and consumer handling and adds
bounded Kafka context propagation plus event-store append, stream-read, and
global-read instrumentation. Snapshot-store instrumentation observes explicit
load, refresh, and deletion without exposing derived state or aggregate
identity.

## Quick start

```go
instrumentation, err := gotelemetry.New(runtime)
if err != nil {
	return err
}

dispatcher, err := instrumentation.WrapDispatcher(baseDispatcher)
if err != nil {
	return err
}

handler, err := instrumentation.WrapConsumer(projectEvent)
if err != nil {
	return err
}
consumer, err := eventsourcing.NewConsumer("account_projection", handler)

kafkaPublisher, err := instrumentation.WrapKafkaPublisher(
	producer,
	gotelemetry.KafkaPropagationConfig{},
)
if err != nil {
	return err
}

kafkaHandler, err := instrumentation.WrapKafkaHandler(
	recordHandler,
	gotelemetry.KafkaPropagationConfig{},
)

store, err := instrumentation.WrapEventStore(baseStore)
if err != nil {
	return err
}

globalReader, err := instrumentation.WrapGlobalReader(baseGlobalReader)
if err != nil {
	return err
}

snapshotStore, err := instrumentation.WrapSnapshotStore(baseSnapshotStore)
if err != nil {
	return err
}
```

`runtime` may be `*telemetry.Runtime` or any value exposing standard
OpenTelemetry tracer, meter, and propagator providers. Constructors create no
providers, exporters, global registrations, goroutines, or shutdown ownership.

## API and semantics

`WrapDispatcher` implements `eventsourcing.Dispatcher`. It preserves input
order, cancellation, reentrant calls, downstream errors, and panic values.
`WrapConsumer` returns an ordinary `eventsourcing.ConsumerFunc` with the same
error and panic behavior as the wrapped function.

Both wrappers require a non-nil `context.Context`. They start and end spans
synchronously and record metrics before returning or re-panicking. They do not
retry, recover, dispatch, persist, publish, or settle messages.

`WrapKafkaPublisher` implements the synchronous publisher contract used by the
Kafka producer and `gokafka.Dispatcher`. It deep-copies caller-owned record
storage, removes old propagation fields, injects the current context, validates
the complete result against explicit `kafka.MessageLimits`, and only then
publishes. A zero propagation config selects `kafka.DefaultMessageLimits`.

`WrapKafkaHandler` implements `kafka.Handler`. It copies bounded headers and
extracts a remote parent only when declared propagation fields are
unambiguous. Malformed, duplicate, or oversized trace context is ignored;
propagation that exceeds record limits never blocks downstream event handling.
Offset settlement remains owned by `kafka.Consumer`.

`WrapEventStore` instruments atomic appends and complete bounded stream reads.
`WrapGlobalReader` does the same for optional globally ordered reads. A read
span stays open until its returned iterator terminates with an error, panics,
or is closed. Callers retain the core requirement to close every iterator.
The wrapper starts no cleanup goroutine and does not hide leaked iterators.

`WrapSnapshotStore` implements `eventsourcing.SnapshotStore`. Load spans report
bounded `hit`, `miss`, `error`, or `panic` outcomes. Save spans treat
`ErrSnapshotStale` as the distinct `stale` outcome; successful saves represent
the application’s explicit snapshot refresh or rebuild. Delete spans report
`success`, `error`, or `panic`. Missing snapshots and every downstream error
remain unchanged, and panics are measured before the original value is
re-raised.

The adapter emits:

| Signal | Name | Bounded attributes |
| --- | --- | --- |
| span | `event_sourcing.dispatch` | delivery mode and batch count |
| span | `event_sourcing.consume` | delivery mode |
| span | `event_sourcing.store.append` | operation and message count |
| span | `event_sourcing.store.read_stream` | operation and message count |
| span | `event_sourcing.store.read_global` | operation and message count |
| span | `event_sourcing.snapshot.load` | operation and outcome |
| span | `event_sourcing.snapshot.save` | operation and outcome |
| span | `event_sourcing.snapshot.delete` | operation and outcome |
| counter | `event_sourcing.operations` | operation and outcome |
| histogram | `event_sourcing.operation.duration` | operation and outcome |
| counter | `event_sourcing.deliveries` | delivery mode and outcome |

Operation values are `dispatch`, `consume`, `append`, `read_stream`,
`read_global`, `snapshot_load`, `snapshot_save`, or `snapshot_delete`.
Outcomes are the bounded values `success`, `error`, `panic`, `hit`, `miss`, or
`stale`; the snapshot operations use only the applicable subset. Delivery
modes are `live`, `replay`, or `unknown` in delivery counters; dispatch spans
may additionally report `mixed` or `empty`.
Store message counts mean submitted messages for append spans and messages
yielded before termination for read spans; they do not claim a failed append
committed.
Delivery counters describe submitted inputs and the enclosing operation
outcome, not a claim that every item in a failed batch completed. These finite
values are the complete metric-cardinality policy.

## Privacy and security

The adapter never records aggregate IDs, message IDs, correlation or causation
IDs, tenant or partition values, event names, payloads, metadata, raw errors,
panic values, topics, database diagnostics, or credentials. Failures set a
fixed span status while returning the original error unchanged. Panics are
measured with a fixed status and re-raised with the original value.

OpenTelemetry provider-construction errors are available through `errors.Is`
and `errors.As`, while `InstrumentError.Error` remains redacted. Export,
sampling, retention, access control, and collector security remain application
and telemetry-runtime responsibilities.

Snapshot instrumentation does not record aggregate IDs or types, snapshot
state, metadata, schema or aggregate versions, timestamps, or failure
diagnostics.

Kafka propagation is limited to the explicit fields declared by the supplied
propagator. Declared fields must be lowercase Kafka-safe names and cannot use
the event adapter's reserved `es.` prefix. Existing declared fields are
replaced rather than duplicated. Baggage is propagated only when the supplied
runtime explicitly enables it; the repository telemetry runtime disables
baggage by default.

## Ownership and concurrency

`Instrumentation` is immutable and safe for concurrent use when the supplied
standard providers are. Wrappers hold no locks across downstream calls and
start no goroutines. Kafka publisher inputs are fully copied before downstream
publication. Bounded consumed headers are copied before extraction; keys and
values retain `kafka.ConsumedMessage`'s handler-call lifetime. The application
owns provider flush and shutdown. Instrumented message iterators retain the
underlying iterator's single-caller ownership and must be closed.

## Adoption

Use this adapter when traces and low-cardinality operational metrics are needed
at event-sourcing boundaries. Keep direct application logging separate and
redact it independently. Do not add high-cardinality event identities as
metric attributes.

Conventional persistence remains preferable when event history, replay, and
temporal auditability do not justify event-sourcing complexity; instrumentation
does not change that decision.

## FAQ

**Does this make delivery durable?** No. It only observes the wrapped contract.

**Does an error span contain the application error?** No. Status descriptions
are fixed and the original error is only returned to the caller.

**Does the adapter install global OpenTelemetry providers?** No. Pass an
explicit runtime and manage its lifecycle in the application.

**Does Kafka propagation change delivery guarantees?** No. Publication
acknowledgements, retries, duplicates, and offset settlement retain the
underlying Kafka contracts.

## Development

Run `make check`. It covers formatting, vet, unit tests, race detection, exact
100% production statement coverage, allocation-reporting benchmarks, and
documentation presence.
