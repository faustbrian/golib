# Kafka integration

The independently versioned
`github.com/faustbrian/golib/pkg/event-sourcing/adapters/gokafka` module owns
event-message mapping and event-specific producer and consumer composition.
The core module does not import Kafka or franz-go.

The stable record codec, topic allowlist, aggregate-root key, complete identity
headers, canonical metadata, live/replay round trip, and ordered synchronous
producer acknowledgement are implemented. The event record handler composes
with bounded cooperative group polling and manual post-success settlement.
An explicit failure policy leaves records unsettled by default and permits
settlement only after synchronous durable poison quarantine or dead-letter
handling. A digest-pinned real-broker suite covers synchronous Zstandard
dispatch, stable envelope reconstruction, per-aggregate order, consumer
handling, acknowledged dead-letter publication, dead-letter reconstruction,
replay rejection without offset settlement, redelivery after explicit replay
opt-in, and committed offsets. See the
[adapter guide](../adapters/gokafka/README.md).

Direct event-store-to-Kafka dispatch is not atomic. The recommended durable
path remains:

1. stage event rows and outbox envelopes in one PostgreSQL transaction;
2. commit PostgreSQL;
3. publish claimed outbox envelopes through the outbox Kafka adapter; and
4. handle Kafka records idempotently before settling offsets.

Kafka producer idempotence and transactions do not create atomicity with
PostgreSQL and do not provide end-to-end exactly-once processing.
