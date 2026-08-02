# Goal: Schema Registry Contracts

## Objective

Build `schema-registry` as a provider-neutral Go package for registering,
resolving, caching, validating, and enforcing compatibility of versioned data
schemas used by Kafka, CloudEvents, outbox, queue, workflow, HTTP, and stored
messages.

The package MUST expose provider capabilities and differences rather than
pretend Confluent-compatible registries, AWS Glue Schema Registry, and other
providers have identical identity, subject, version, compatibility, or wire
semantics.

Authoritative starting points for the first provider evaluations:

- https://docs.aws.amazon.com/glue/latest/dg/schema-registry.html
- https://docs.confluent.io/platform/current/schema-registry/index.html

## Core Model

Define stable contracts for schema content, format, fingerprint, registry ID,
subject/name, version, references, metadata, compatibility mode, lifecycle
state, lookup, registration, deletion policy, and diagnostics.

Supported schema formats MUST be explicit. JSON Schema MUST integrate with
`json-schema`; Avro and Protobuf MUST use maintained canonical implementations
or focused adapters rather than incomplete home-grown parsers.

Canonical fingerprints MUST define normalization and collision handling.
Provider-issued IDs MUST never be mistaken for portable schema identity.

## Operations

- Register idempotently and distinguish existing, newly created, incompatible,
  rejected, unauthorized, unavailable, and unknown outcomes.
- Resolve by provider ID, portable fingerprint, subject/version, and latest
  only when the provider can define those semantics safely.
- Check backward, forward, full, transitive, and provider-specific
  compatibility with explicit unsupported results.
- Support schema references and detect cycles, missing references, incompatible
  changes, and excessive graphs.
- Provide bounded listing and administrative lifecycle APIs without making
  destructive operations easy or implicit.

## Caching And Offline Behavior

Provide bounded positive and negative caches with explicit freshness,
staleness, invalidation, single-flight behavior, cancellation, and metrics.
Callers MUST choose fail-closed, stale-read, preloaded/offline, or unavailable
behavior. An outage MUST not silently cause unvalidated production or
registration storms.

Support immutable local bundles for startup and incident operation, with
provenance and fingerprints. Hidden network access during decode is forbidden.

## Providers And Wire Integration

Implement separately releasable adapters for providers selected by production,
including AWS Glue Schema Registry when AWS/MSK uses it and a
Confluent-compatible provider when required. Each adapter MUST document wire
format, authentication, region/endpoint behavior, quotas, compatibility,
deletion, eventual consistency, retries, and service-specific IDs.

Provide serializer/deserializer integration contracts that validate bounded
payloads and resolve schemas explicitly. Wire framing MUST be separately
versioned and tested; registry access MUST not be embedded invisibly into
business codecs.

## Security And Reliability

All I/O MUST use context, bounded retries, deadlines, concurrency, response
sizes, and one resilience budget. Use least-privilege credentials and safe
endpoint policies. Prevent SSRF, credential forwarding, schema bombs, reference
explosion, cache poisoning, downgrade, incompatible registration races, and
sensitive schema leakage.

## Verification

Run provider integration suites against supported versions/services and
independent clients. Test compatibility matrices, references, IDs,
fingerprints, caches, stale/offline modes, concurrent registration, quotas,
throttling, failover, malformed responses, and unknown outcomes. Fuzz schemas,
wire framing, and reference graphs. Exact 100% statement coverage and 100%
viable mutation kills are REQUIRED.

## Documentation And Delivery

Document provider differences, schema evolution, compatibility policy, wire
formats, caches, outages, authentication, migrations, incident operation,
security, FAQ, and end-to-end Kafka/CloudEvents examples. Add manifests, CI,
benchmarks, changelog, notices, and clean-consumer proof.

## Non-Goals

- inventing a new schema language or generic code generator;
- hiding incompatible provider semantics behind false portability;
- replacing application ownership of schema evolution;
- contacting a registry implicitly during ordinary value access.
