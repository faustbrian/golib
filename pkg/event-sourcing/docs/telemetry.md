# OpenTelemetry integration

The independently versioned
`github.com/faustbrian/golib/pkg/event-sourcing/adapters/gotelemetry` module
keeps OpenTelemetry dependencies outside the event-sourcing core.

The current adapter wraps synchronous dispatchers and consumer functions. It
preserves parent trace context and emits fixed-name spans plus
finite-cardinality operation, duration, and delivery metrics. It also wraps
Kafka publishers and handlers with bounded W3C context injection and
extraction. Event-store wrappers observe append and complete iterator
lifetimes for bounded stream and global reads. Snapshot-store wrappers observe
explicit load, refresh, and deletion with bounded hit, miss, stale, error, and
panic outcomes. Projection-runner wrappers observe bounded batch progress,
poison skips, durable checkpoint position, terminal empty reads, and bounded
throughput counters. Payload-codec and upcaster wrappers propagate repository
operation context through serialization and evolution while recording only
fixed operation names, bounded outcomes, duration, and successful upcast output
counts. The adapter never records
aggregate, message, correlation, causation, tenant, partition, event, payload,
metadata, error, panic, topic, event-store read position, database, or
credential data. Projection spans deliberately record the configured static
projection name and its resulting numeric checkpoint; applications must not
use tenant or customer identifiers as projection names.

The adapter accepts a narrow standard-provider runtime interface implemented by
the repository telemetry runtime. It creates no providers, exporters, global
registrations, goroutines, or shutdown ownership. Applications own provider
configuration, sampling, export, flush, retention, and access control.

Instrumentation observes the wrapped behavior only. It does not add
persistence, delivery, retries, transactions, offset settlement, or
exactly-once guarantees. Kafka propagation copies records, replaces stale
declared propagation fields, enforces Kafka message limits before publication,
and ignores inbound propagation outside those bounds or with duplicate
declared fields. Store wrappers preserve the caller-owned iterator lifecycle:
read spans end on iterator close, terminal error, or panic, and no hidden
cleanup goroutine is created.
Projection wrappers preserve partial batch results, errors, cancellation, and
panic values while distinguishing replay progress from a successful terminal
empty batch. A terminal batch is an observation at that read boundary, not a
claim that later appends cannot create more work.
Applications may report lag explicitly from a durable checkpoint and a
caller-owned high watermark. The adapter performs no hidden read and rejects
reversed or unrepresentable distances rather than truncating them.
Checkpoint-store wrappers observe status and optimistic saves without changing
conflict or pause semantics. Exact unsigned positions are recorded as strings;
invalid projection names are redacted instead of entering telemetry.
Projection-controller wrappers bind one canonical static name and observe
status, pause, resume, and checkpoint reset without starting runner work,
draining handlers, or resetting application read models.
Projection-handler wrappers create one child span per delivery while preserving
the exact delivery and downstream context. They never record event identity,
payload, read-model state, errors, or panic values.
Process-manager wrappers observe pure planning with a static bounded name,
delivery mode, and successful command count. They preserve typed results and
never inspect commands, execute effects, or record event or command data.
Payload-codec wrappers preserve both ordinary codecs and the optional
context-aware codec extension. Upcaster wrappers preserve the equivalent pure
and context-aware contracts. Direct pure calls use a background context;
repository operations supply their caller context. Neither boundary records
event identity, schema, content type, payload, metadata, decoded values,
transformed output, or failure diagnostics.

See the [adapter guide](../adapters/gotelemetry/README.md) for the quick start,
API, signal names, privacy contract, ownership rules, FAQ, and development
gate.
