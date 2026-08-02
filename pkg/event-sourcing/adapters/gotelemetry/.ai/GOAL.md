# Goal: Event Sourcing OpenTelemetry Adapter

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Objective

Build `event-sourcing/adapters/gotelemetry` as optional, failure-isolated
instrumentation for stores, serializers, snapshots, projections, process
managers, and Kafka propagation seams. It MUST observe completed operations
without changing event-sourcing behavior or leaking event data.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Wrap narrow core contracts without changing ordering, retries, settlement,
  transaction ownership, returned values, or error identity.
- Emit documented traces and metrics for operation kind, duration, outcome, and
  bounded low-cardinality state.
- Never record payloads, metadata values, aggregate IDs, message IDs, tenant
  IDs, stream names, arbitrary event names, errors, panic values, SQL, or
  credentials unless a separately bounded explicit allowlist permits a field.
- Define span/context propagation for Kafka headers separately from event wire
  identity with strict key/value/count limits and collision policy.
- Preserve cancellation and callback panic behavior while preventing telemetry
  failures from becoming event-store or handler failures.
- Start no hidden lifecycle and leave provider/exporter shutdown caller-owned.

## Documentation And Completion

Document every instrument and attribute, semantic-convention version, privacy
and cardinality policy, propagation, failure isolation, API, examples,
adoption, FAQ, compatibility, and migration. CI MUST enforce race, fuzz,
security, API, docs, benchmarks, exactly 100% statement coverage, and exactly
100% of viable mutants killed by meaningful tests.
