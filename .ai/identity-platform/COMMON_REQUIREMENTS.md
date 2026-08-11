# Common Identity Goal Requirements

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHALL**, **SHALL NOT**,
**SHOULD**, **SHOULD NOT**, **RECOMMENDED**, **NOT RECOMMENDED**, **MAY**, and
**OPTIONAL** in this document are to be interpreted as described in BCP 14
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in all capitals, as shown here.

These requirements apply to every goal in `goals/`.

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
   planning goal to the canonical `pkg/<module>/.ai/GOAL.md` path and update
   the inventory link. The move MUST NOT weaken or silently rewrite the goal.
7. Every worker MUST satisfy the parity rows and end-state requirements that
   name its unit. A package goal is incomplete when those documents assign an
   unproved behavior to it.
8. Workers MUST limit writes to their canonical package directory. The
   coordinator owns root registration, catalogs, orchestration state, and
   final integrated verification.

## Module and API contract

- The module MUST live at its canonical `pkg/...` path, have an independent
  `go.mod`, and be declared in `modules.json` and `packages.json`.
- Public contracts MUST expose ownership, cancellation, resource limits,
  concurrency, retry and timeout policy, stable errors, and lifecycle.
- Core modules MUST define consumer-oriented interfaces. Persistence,
  protocols, transports, and providers MUST remain in named adapters.
- Cross-module dependencies MUST remain acyclic. Permanent `replace`
  directives and private sibling imports are forbidden.
- Every external operation MUST accept or derive a bounded
  `context.Context`. Callbacks MUST NOT run under locks or open transactions
  unless the public contract explicitly transfers that ownership.

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
5. Public API baseline, clean-consumer proof, dependency/license review,
   vulnerability/secret scanning, SBOM/provenance gates, and manifests.
6. README quick start, API/adoption/security/operations/tradeoff/FAQ docs,
   compilable examples, and an Unreleased `CHANGELOG.md` entry.
7. The narrow module gate and reverse-dependant gates whose complete input
   fingerprints changed, using task-owned disposable Go caches.

A unit may move to `implemented-unverified` when implementation and docs are
complete. It may move to `verified` only after every goal requirement and
release gate passes against final inputs. Gaps, warning substitutions,
unavailable provider proof, or stale fingerprints block `verified`.
