# Event sourcing OpenTelemetry adapter

`gotelemetry` is the independently versioned observability boundary for
`event-sourcing`. The event-sourcing core does not import OpenTelemetry or the
repository `telemetry` module.

This adapter instruments synchronous dispatch and consumer handling and adds
bounded Kafka context propagation plus event-store append, stream-read, and
global-read instrumentation. Snapshot-store instrumentation observes explicit
load, refresh, and deletion without exposing derived state or aggregate
identity. Projection-runner instrumentation observes bounded replay progress,
poison skips, durable checkpoint position, and terminal probes.

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
if err != nil {
	return err
}

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
if err != nil {
	return err
}

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

projectionRunner, err := instrumentation.WrapProjectionRunner(
	"account-summary",
	baseProjectionRunner,
)
if err != nil {
	return err
}

processManager, err := gotelemetry.WrapProcessManager(
	instrumentation,
	"welcome-email",
	baseProcessManager,
)
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
publishes. Invalid source records are rejected before copying. If propagation
injection panics or emits invalid propagation, its output is discarded and the
bounded source record is published without declared propagation fields. A
propagator that cannot declare its fields is rejected as invalid configuration
before wrapper creation. A zero propagation config selects
`kafka.DefaultMessageLimits`.

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
Every downstream return pair is preserved, including a nil iterator with a nil
error; that pair completes the read observation immediately.

`WrapSnapshotStore` implements `eventsourcing.SnapshotStore`. Load spans report
bounded `hit`, `miss`, `error`, or `panic` outcomes. Save spans treat
`ErrSnapshotStale` as the distinct `stale` outcome; successful saves represent
the application’s explicit snapshot refresh or rebuild. Delete spans report
`success`, `error`, or `panic`. Missing snapshots and every downstream error
remain unchanged, and panics are measured before the original value is
re-raised.

`WrapProjectionRunner` accepts the consumer-owned `ProjectionRunner` interface,
which is implemented by `*projection.Runner`. Each `RunBatch` span records
scanned, handled, filtered, skipped, and durably checkpointed counts plus the
resulting checkpoint and whether replay made progress or reached a terminal
empty batch. It preserves partial results, errors, panic values, cancellation,
and downstream context. The configured projection name is an operator-facing
attribute and must be a bounded static name, never a tenant or customer ID.
Successful and partial batch results also increment projection message
counters for scanned, handled, filtered, skipped, and checkpointed work.

`RecordProjectionLag` accepts the caller's exact durable checkpoint and high
watermark. It records their distance without reading a store, starting work, or
claiming the watermark remains current. The observation decorates the active
span and emits one histogram sample. Reversed positions or a distance outside
OpenTelemetry's signed 64-bit metric range fail explicitly.

`WrapProjectionCheckpointStore` observes durable checkpoint status and
optimistic saves through the standard `projection.CheckpointStore` contract.
It preserves the supplied projection name and exact unsigned positions for the
downstream store. Canonical projection names and positions are recorded as
strings so telemetry does not truncate them; an invalid name is reported only
as `invalid`. The wrapper does not validate returned status or alter conflict,
pause, error, or panic behavior. A failed status call records no returned state,
and checkpoint fields on a failed save describe the requested transition rather
than committed progress.

`WrapProjectionController` accepts the consumer-owned `ProjectionController`
interface implemented by `*projection.Controller`. It binds one canonical
static projection name and observes status, pause, resume, and checkpoint reset
calls. Returned state and checkpoint attributes are emitted only for successful
calls. Reset expectations are exact strings and describe the requested
transition. The wrapper neither starts runner work nor drains in-flight work;
those remain caller responsibilities.

`WrapProjectionHandler` decorates a standard `projection.Handler` with one span
per delivery. It records only the static projection name, delivery mode, bounded
outcome, and duration. The exact delivery and child span context reach the
downstream handler; returned errors and panic values are preserved but never
recorded. A handler wrapper can be passed directly to `projection.NewRunner`.

`WrapProcessManager` accepts the generic consumer-owned `ProcessManager`
interface implemented by `*processmanager.Manager`. It is a function because Go
does not permit methods with their own type parameters. Each planning call
records the static manager name, delivery mode, bounded outcome, duration, and
successful command count. It preserves the exact result, error, panic, context,
and delivery while neither executing nor inspecting planned commands.

`WrapPayloadCodec` implements `eventsourcing.ContextPayloadCodec`. It observes
payload encode and decode calls while preserving the pure two-method codec
contract. Repository calls propagate their operation context to a wrapped
context-aware codec; direct `Encode` and `Decode` calls use a background
context. `WrapUpcaster` provides the equivalent `eventsourcing.ContextUpcaster`
boundary and records only the successful output count. Neither wrapper
validates, copies, retries, or inspects application event data.

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
| span | `event_sourcing.projection.run_batch` | static projection name, bounded counts, checkpoint, and replay termination |
| span | `event_sourcing.projection.checkpoint.status` | canonical projection name, run state, and optional checkpoint |
| span | `event_sourcing.projection.checkpoint.save` | canonical projection name, expected checkpoint, and next checkpoint |
| span | `event_sourcing.projection.control.status` | static projection name, run state, and optional checkpoint |
| span | `event_sourcing.projection.control.pause` | static projection name, resulting state, and optional checkpoint |
| span | `event_sourcing.projection.control.resume` | static projection name, resulting state, and optional checkpoint |
| span | `event_sourcing.projection.control.reset` | static projection name, expected checkpoint, and resulting state |
| span | `event_sourcing.projection.handle` | static projection name and delivery mode |
| span | `event_sourcing.process_manager.plan` | static process-manager name, delivery mode, and successful command count |
| span | `event_sourcing.codec.encode` | no event attributes |
| span | `event_sourcing.codec.decode` | no event attributes |
| span | `event_sourcing.upcast` | successful output count |
| counter | `event_sourcing.operations` | operation and outcome |
| histogram | `event_sourcing.operation.duration` | operation and outcome |
| counter | `event_sourcing.deliveries` | delivery mode and outcome |
| counter | `event_sourcing.projection.messages` | result kind and batch outcome |
| histogram | `event_sourcing.projection.lag` | no attributes |

Operation values are `dispatch`, `consume`, `append`, `read_stream`,
`read_global`, `snapshot_load`, `snapshot_save`, `snapshot_delete`,
`projection_run_batch`, `projection_checkpoint_status`, or
`projection_checkpoint_save`; projection-control operations are
`projection_control_status`, `projection_control_pause`,
`projection_control_resume`, and `projection_control_reset`; projection
handling uses `projection_handle`, and process-manager planning uses
`process_manager_plan`; serialization operations use `codec_encode`,
`codec_decode`, and `upcast`.
Outcomes are the bounded values `success`, `error`, `panic`, `hit`, `miss`, or
`stale`; the snapshot operations use only the applicable subset. Delivery
modes are `live`, `replay`, or `unknown` in delivery counters; dispatch spans
may additionally report `mixed` or `empty`.
Projection replay termination is `progress`, `terminated`, `error`, or
`panic`. A successful terminal span means the wrapped runner returned an empty
batch; it does not promise that no later append can create more work.
Projection result kinds are `scanned`, `handled`, `filtered`, `skipped`, or
`checkpointed`. Zero counts are omitted. Failed batches report only work
present in the returned partial result.
Store message counts mean submitted messages for append spans and messages
yielded before termination for read spans; they do not claim a failed append
committed.
Delivery counters describe submitted inputs and the enclosing operation
outcome, not a claim that every item in a failed batch completed. These finite
values are the complete metric-cardinality policy.

## Semantic conventions

The adapter uses module-owned event-sourcing conventions, schema version 2.
It does not claim an upstream OpenTelemetry semantic-convention schema URL or
version. The span, metric, and attribute tables above are the complete schema;
changes to their names or meanings are compatibility changes recorded in this
module's changelog. Version 2 removes `event_sourcing.projection.name` from the
`event_sourcing.projection.messages` and `event_sourcing.projection.lag` metric
series; spans retain the bounded name.

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

Projection spans record only their explicitly configured bounded name,
aggregate counts, durable numeric checkpoint, and caller-supplied numeric lag.
Projection metrics omit projection names so their series cardinality remains
independent of runtime name diversity. Instrumentation does not record messages,
event identities, filters, handler or poison-policy diagnostics, or read-model
state.
Checkpoint-store instrumentation records canonical projection names plus exact
expected and resulting positions. Applications must keep the set of projection
names bounded and must not derive them from tenants or customers.
Projection-controller instrumentation records the same bounded static name,
operation, resulting state, and relevant checkpoint positions. It records no
read-model state, reset callback, drain status, or failure diagnostics.
Projection-handler instrumentation records the static projection name and
delivery mode but not message identity, event data, read-model mutations,
errors, or panic values.
Process-manager instrumentation records only its bounded static name, delivery
mode, and successful command count. It records no message identity, event data,
planned command data, process state, errors, or panic values. Applications must
not derive manager names from tenants or customers.
Payload codec instrumentation records no event identity, schema version,
content type, encoded payload, decoded value, error, or panic value. Upcaster
instrumentation records only a successful output count and never event
identity, schema version, payload, metadata, transformed values, errors, or
panic values.

Kafka propagation is limited to the W3C `traceparent` and `tracestate` fields
declared by the supplied propagator. Propagators declaring baggage,
authorization, application, or other fields are rejected before wrapper
creation. Existing trace-context fields are replaced rather than duplicated;
non-ASCII lookalikes remain unrelated application headers.

## Ownership and concurrency

`Instrumentation` is immutable and safe for concurrent use when the supplied
standard providers are. The `Runtime` contract requires tracer, meter, span,
instrument, and propagator API calls to return promptly. Wrappers hold no locks
across downstream calls and
start no goroutines. Kafka publisher inputs are fully copied before downstream
publication. Bounded consumed headers are copied before extraction; keys and
values retain `kafka.ConsumedMessage`'s handler-call lifetime. The application
owns provider flush and shutdown. Instrumented message iterators retain the
underlying iterator's single-caller ownership and must be closed.

OpenTelemetry runtime panics during span creation, span completion, metric
recording, and Kafka injection or extraction are isolated from wrapped calls.
A failed observation is dropped; downstream values, errors, and panic values
remain the operation result. The adapter does not start timeout goroutines and
therefore cannot preempt a provider that blocks inside the synchronous
OpenTelemetry API.
Applications must configure bounded, asynchronous SDK processors, exporter
timeouts, and shutdown deadlines; their flush and shutdown remain caller-owned.
A runtime that blocks inside a synchronous API call violates the supported
`Runtime` contract and cannot be preempted safely by this in-process adapter.

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

## Performance

The dispatcher benchmark compares one equivalent synchronous one-delivery call
through the direct downstream contract, no-op OpenTelemetry providers, an SDK
with tracing sampled out, and a recording SDK with a synchronous discard
exporter. On Apple M4 Max, Darwin arm64 27.0, Go 1.26.5, five independent
500 ms samples produced these medians:

| Path | Median latency | Bytes/op | Allocations/op |
| --- | ---: | ---: | ---: |
| direct | 9.825 ns | 0 | 0 |
| no-op | 7.459 µs | 904 | 16 |
| sampled out | 9.918 µs | 1,128 | 18 |
| recording | 14.624 µs | 2,072 | 21 |

The raw latency samples varied materially on the shared developer machine, so
these figures are a reproducible comparison baseline rather than a stable CI
threshold. The canonical benchmark gate publishes every adapter benchmark with
latency and allocation results.

## Compatibility and migration

The module is pre-v1 and follows the repository's directory-prefixed semantic
versions. It depends only on the narrow public event-sourcing and Kafka
contracts and standard OpenTelemetry provider interfaces. Provider and exporter
lifecycle remains outside the adapter.

Existing users do not need to migrate wire data: trace headers are separate
from event envelopes and event identity. After upgrading, invalid or panicking
propagator output is dropped instead of failing an otherwise bounded publish,
and store wrappers preserve a downstream nil iterator with a nil error. Remove
any caller logic that depended on the deprecated `ErrMessageIteratorRequired`
wrapper error. Kafka propagators must now declare only `traceparent` and
`tracestate`; replace baggage or application-header propagation with an
explicit application-owned transport boundary before upgrading.
Metric consumers must migrate from semantic schema version 1 to version 2:
remove projection-name grouping and filters from the
`event_sourcing.projection.messages` and `event_sourcing.projection.lag`
queries, dashboards, and alerts. Version 2 aggregates those series across all
projection names to keep cardinality bounded; use spans for per-projection
diagnosis.

## Development

From the repository root, run
`make check MODULES=pkg/event-sourcing/adapters/gotelemetry` for the canonical
module contract. The goal evidence also requires the module-scoped race, fuzz,
security, API, documentation, benchmark, exact statement-coverage, and exact
viable-mutation gates declared in `modules.json`.
