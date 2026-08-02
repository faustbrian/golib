# Goal: Kafka Service Lifecycle Adapter

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Objective

Build `kafkaservice` as the narrow lifecycle bridge between `kafka` clients
and `service` components. It MUST coordinate startup, readiness, run, drain,
and shutdown without owning Kafka producer, consumer, retry, transaction, or
broker policy.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Adapt producer and consumer resources into explicit service dependencies with
  deterministic names, health, readiness, and failure propagation.
- Validate all callbacks, typed nils, deadlines, and ownership before startup.
- Roll back partially started resources in reverse order.
- Mark consumers unready before stopping fetch, draining handlers, committing
  eligible offsets, leaving the group, and closing clients.
- Flush or abort producers under a caller-owned bounded shutdown budget with
  ambiguous delivery surfaced honestly.
- Make start, run, stop, duplicate stop, stop-before-start, and concurrent stop
  behavior explicit and panic-safe.
- Preserve Kafka errors and observations without exposing payload or credentials.

## Documentation And Completion

Document lifecycle sequence, dependency ownership, readiness, drain, Kubernetes
SIGTERM, duplicate windows, API, examples, adoption, FAQ, compatibility, and
migration. CI MUST enforce race, fuzz, lifecycle tests, API, docs, benchmarks,
exactly 100% statement coverage, and exactly 100% viable mutation kills.
