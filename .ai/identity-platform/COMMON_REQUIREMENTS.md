# Common Identity Goal Requirements

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

These requirements apply to every goal in `goals/`.

The following coordinator specifications are binding. Applicability MUST NOT be
left to worker discretion. `SHARED_CONTRACT_APPLICABILITY.json` is the sole
canonical per-unit selector manifest. Every inventory unit MUST have exactly
the five selectors `transaction`, `lifecycle`, `lifecycle_consumers`,
`configuration`, and `security_events`. Each selector is a bytewise-sorted,
duplicate-free array of exact catalog IDs; the sole no-applicability value is
`["none"]`, and `none` MUST NOT coexist with another value. Goal files MUST NOT
duplicate these selectors because such copies can drift when a goal moves.

The coordinator MUST run
`ruby .ai/identity-platform/render_shared_contracts.rb --check` before
assignment and MUST insert the output of
`ruby .ai/identity-platform/render_shared_contracts.rb <unit>` verbatim into the
generated worker assignment. The renderer supplies each selected ID, its
canonical source and the exact selected source row. The coordinator validator
MUST reject a missing unit or selector, an extra unit or selector, duplicates,
unsorted values, an unknown or stale ID, incomplete lifecycle-consumer capture,
or a non-canonical manifest encoding.

Selecting any `tx.*` ID MUST include `tx.foundation`; selecting any
`lifecycle.*` ID MUST include `lifecycle.foundation`. Configuration IDs use the
exact `ref.<template-path>` form defined by `REFERENCE_CONFIGURATION.md`.
`lifecycle_consumers` values are exact cascade row IDs from
`LIFECYCLE_CONSUMERS.md`; every owning or consuming inventory unit MUST select
the row, and its `lifecycle` selector MUST select the same cascade.

- `TRANSACTION_CONTRACT.md` for every PostgreSQL mutation, cross-module
  transition, one-time capability, reveal-once result, and unknown commit;
- `LIFECYCLE_CASCADES.md` for every identity, credential, session,
  organization, authorization, provider, client, and destructive lifecycle;
- `LIFECYCLE_CONSUMERS.md` for every unit that owns or consumes a cascade;
- `REFERENCE_CONFIGURATION.md`, the canonical machine-value authority, for
  every configurable package and for `identity/reference`; and
- `SECURITY_EVENTS.md` for every exact audit action emitted by a unit.

A package goal MUST NOT be interpreted to permit a weaker or different local
contract. If an existing primitive cannot implement a required shared
contract, the worker MUST stop and return the missing primitive/API as a graph
blocker; it MUST NOT add a private substitute or defer the behavior to
undocumented application glue.

## Start and ownership gates

1. A worker MUST NOT begin a unit unless the coordinator has marked it
   `in-progress`, recorded that worker, and verified every unit in its
   `Requires` field.
2. Only the coordinator MAY change inventory status, ownership, dependencies,
   planning-goal locations, or shared repository manifests. Workers MUST NOT
   edit coordinator-owned files.
3. The agent MUST implement only the named canonical module. A new dependency,
   shared contract, or ownership transfer MUST be approved and recorded in the
   graph before implementation continues.
4. `Consumes` entries identify existing primitives, not program start gates.
   Their public contracts MUST be inspected; private internals MUST NOT be
   imported or copied.
5. `Unlocks` is informative. A dependant remains blocked until all of its own
   `Requires` units are `verified`.
6. After integrating a scaffolded module, the coordinator MUST move its
   planning goal to the unit's declared canonical goal path and update
   the inventory link. The move MUST NOT weaken or silently rewrite the goal.
7. Every worker MUST satisfy the parity rows and end-state requirements that
   name its unit. A package goal is incomplete when those documents assign an
   unproved behavior to it.
8. Workers MUST limit writes to their canonical package directory. The
   coordinator owns root registration, catalogs, orchestration state, and
   final integrated verification.
9. Every worker MUST implement its enumerated `REFERENCE_CONFIGURATION.md` row
   IDs as explicit supported configuration. `REFERENCE_PROFILE.md` is
   explanatory and MUST match the canonical manifest; conflicts block
   assignment. Core zero values MUST remain unambiguous; the reference package
   owns deployment defaults.
10. Before assigning a unit, the coordinator MUST render the enumerated rows
    from `TRANSACTION_CONTRACT.md`, `LIFECYCLE_CASCADES.md`, and
    `REFERENCE_CONFIGURATION.md` into the worker acceptance contract. A unit
    MUST NOT become `verified` while an enumerated row lacks one public owner,
    executable proof, or an exact selected reference value.

## Module and API contract

- The module MUST live at its canonical `pkg/...` path, have an independent
  `go.mod`, and be declared in `modules.json` and `packages.json`.
- Public contracts for operations that own or perform those concerns MUST expose
  ownership, cancellation, resource limits, concurrency, retry and timeout
  policy, stable errors, and lifecycle.
- Core modules MUST define consumer-oriented interfaces. Persistence,
  protocols, transports, and providers MUST remain in named adapters.
- Cross-module dependencies MUST remain acyclic. Permanent `replace`
  directives and private sibling imports are forbidden.
- Every external operation MUST accept or derive a bounded
  `context.Context`. Callbacks MUST NOT run under locks or open transactions
  unless the public contract explicitly transfers that ownership.
- Public interfaces shared by two or more program modules MUST have one named
  upstream owner and a compile-time conformance test in every implementation.
  A downstream worker MUST NOT invent a parallel transaction, event, version,
  capability, locale, cookie, token, or authorization contract.

## Identity and authorization invariants

- Every subject, credential, session, organization, client, and provider MUST
  have a stable opaque identifier. User input MUST NOT become a storage key
  without canonicalization and collision analysis.
- Enumeration-sensitive operations MUST return indistinguishable public
  outcomes while preserving redacted internal reason codes.
- Authentication MUST NOT imply authorization. Every privileged operation MUST
  require an explicit decision and emit an audit record.
- Tenant and organization scope MUST be explicit and fail closed. Cross-scope
  reads, writes, linking, replay, and cache collisions MUST be tested.
- Secrets, credentials, tokens, codes, cookies, recovery material, provider
  tokens, challenges, and PII MUST NOT appear in errors, logs, traces, metric
  labels, fixtures, snapshots, or evidence.
- Bearer-equivalent values MUST be irreversibly digested when lookup permits.
  Recoverable secrets MUST use `secret-envelope` with authenticated context
  and a rotation policy.
- Security state changes MUST define atomicity, idempotency, replay,
  revocation propagation, audit semantics, and ambiguous-commit recovery.
- Security-sensitive versions and invalidations MUST use the dimensions and
  monotonic semantics in `LIFECYCLE_CASCADES.md`. A cache or stateless token
  MUST bind every dimension capable of invalidating the authority it carries.

## Persistence and migration contract

- Core modules MUST remain storage-neutral. Durable adapters own schemas,
  transactions, constraints, indexes, migrations, cleanup, and reconciliation.
- Migrations MUST be forward-only, interruption-safe, and safe with concurrent
  old/new binaries. Race-sensitive invariants MUST be database-enforced.
- Stores MUST specify consistency, pagination, locking, clock ownership,
  duplicates, retention, deletion, and not-committed/committed/unknown outcomes.
- Deletion and anonymization MUST account for audit, outbox, revocation,
  legal-hold, and referential-integrity boundaries without claiming deletion
  outside owned stores.
- PostgreSQL adapters participating in a shared mutation MUST use the common
  unit-of-work and command-result protocol in `TRANSACTION_CONTRACT.md`.
  Opening a private transaction inside an enlisted operation is forbidden.
- One-time capabilities MUST use the reservation/finalization/query protocol
  in `TRANSACTION_CONTRACT.md`. Consuming a capability in one commit and
  applying its domain transition in another without a durable recovery state
  is forbidden.

## Hostile boundaries and lifecycle

- Parsers and handlers MUST impose limits before allocation, conversion,
  recursion, decompression, or cryptographic work and MUST include fuzz targets
  with deterministic regression seeds.
- Concurrent code MUST document synchronization, cancellation, shutdown,
  channel closure, and goroutine lifetime and pass race, stress, and leak checks.
- Provider proof MUST cover timeouts, cancellation, partial and malformed
  responses, throttling, retries, duplicate callbacks, clock skew, key
  rotation, outage recovery, and redaction. Fakes are not interoperability proof.

## Required verification and release evidence

Each goal MUST produce:

1. Behavioral tests for success, denial, invalid transitions, replay,
   idempotency, cancellation, cleanup, and scope isolation.
2. Exact 100% statement coverage and exact 100% mutation efficacy and mutant
   coverage for every viable mutant.
3. Race, fuzz, hostile-input, leak, and benchmark evidence where applicable.
4. Pinned official fixtures and independent interoperability evidence for
   specification claims; untested profiles MUST be explicit.
5. Public API baseline, clean-consumer proof, dependency and license review,
   vulnerability scanning, secret scanning, SBOM and provenance gates, and
   manifests.
6. README quick start, API/adoption/security/operations/tradeoff/FAQ docs,
   compilable examples, and an Unreleased `CHANGELOG.md` entry.
7. The narrow module gate and reverse-dependant gates whose complete input
   fingerprints changed, using task-owned disposable Go caches.

A unit may move to `implemented-unverified` when implementation and docs are
complete. It may move to `verified` only after every goal requirement and
release gate passes against final inputs. Gaps, warning substitutions,
unavailable provider proof, or stale fingerprints block `verified`.
