# OpenTelemetry integration

The independently versioned
`github.com/faustbrian/golib/pkg/event-sourcing/adapters/gotelemetry` module
keeps OpenTelemetry dependencies outside the event-sourcing core.

The current adapter wraps synchronous dispatchers and consumer functions. It
preserves parent trace context and emits fixed-name spans plus
finite-cardinality operation, duration, and delivery metrics. It also wraps
Kafka publishers and handlers with bounded W3C context injection and
extraction. Event-store wrappers observe append and complete iterator
lifetimes for bounded stream and global reads. The adapter never records
aggregate, message, correlation, causation, tenant, partition, event, payload,
metadata, error, panic, topic, position, database, or credential data.

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

See the [adapter guide](../adapters/gotelemetry/README.md) for the quick start,
API, signal names, privacy contract, ownership rules, FAQ, and development
gate.
