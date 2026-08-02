# Goal: Queue Service Lifecycle Adapter

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

## Objective

Build `queueservice` as the narrow lifecycle integration between `queue`
producers/workers and `service`. It MUST standardize startup, readiness,
drain, and shutdown without owning backend transport, retry, scheduling,
dead-letter, or worker-management policy.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals.

## Required Scope

- Adapt typed producers, handlers, and workers through explicit callbacks with
  validated ownership and stable service identity.
- Roll back partial construction/startup and close resources exactly once.
- Remove readiness before stopping intake, drain in-flight handlers within one
  caller-owned deadline, settle only completed work, and release uncompleted
  work for backend redelivery.
- Define publish cancellation and unknown-acceptance behavior without unsafe
  application retry.
- Handle stop-before-start, repeated stop, concurrent run/stop, worker exit,
  callback panic, and shutdown timeout deterministically.
- Preserve backend errors and observations while redacting task payloads,
  credentials, endpoints, and panic values.

## Documentation And Completion

Document lifecycle sequence, ownership, readiness, Kubernetes SIGTERM/scaling,
duplicate windows, backend differences, API, examples, adoption, FAQ, and
migration. CI MUST enforce race, fuzz, backend integration, API, docs,
benchmarks, exactly 100% statement coverage, and exactly 100% of viable mutants
killed by meaningful tests.
