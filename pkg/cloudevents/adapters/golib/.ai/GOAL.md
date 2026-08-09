# Goal: Golib CloudEvents adapters

Implement optional, explicit adapters for Golib event-sourcing, outbox, Kafka,
queue, workflow, correlation, tenancy, telemetry, audit, JSON Schema, and
schema-registry contracts without making any of them dependencies of the core
CloudEvents module. Every conversion must preserve canonical state outside the
CloudEvent, reject reserved-field collisions, and report representational loss.
