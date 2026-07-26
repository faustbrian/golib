# Transactional outbox integration

The core event store and outbox remain independently usable and releasable.
Neither imports the other. The optional
`github.com/faustbrian/golib/pkg/event-sourcing/adapters/gooutbox` nested module
is the only component that depends on both public contracts.

Use its committed `Store` as the ordinary aggregate repository store when
event rows and publishable outbox envelopes must commit together. Use its
lower-level `Stager` only for an already caller-owned `pgx.Tx`. The adapter
never publishes before commit and replay reads never enqueue records.

The complete API, envelope mapping, crash matrix, limits, recovery procedure,
and examples are documented in the
[gooutbox adapter guide](../adapters/gooutbox/README.md).

The production path is:

1. append event messages and outbox envelopes in one PostgreSQL transaction;
2. commit PostgreSQL before any external publication;
3. claim committed envelopes through the independently operated outbox relay;
4. publish through the outbox-owned Kafka publisher; and
5. settle Kafka consumption only after successful idempotent handling.

This is at-least-once delivery. PostgreSQL and Kafka do not share a transaction.
Producer idempotence and Kafka transactions do not change that boundary.

