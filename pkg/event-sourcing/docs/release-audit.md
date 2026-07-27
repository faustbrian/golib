# Release hardening findings

This report records the final source-level findings from the EventSauce 3.9.1
compatibility and production-hardening audit. The baseline is EventSauce
`3.9.1` at tag commit `33ea9b97ec3ac56991caad03b791fee418a43e41`.

There are no unresolved high- or medium-severity package findings. That is not
a release or deployment claim: a release still requires current
content-addressed repository and module evidence for the exact release inputs.

## Findings

| ID | Severity | Finding | Disposition | Primary evidence |
| --- | --- | --- | --- | --- |
| ES-001 | High | A caller-owned transaction could be mistaken for a committed aggregate save | Resolved | Explicit save plans, transaction staging, commit confirmation, ambiguous-commit poisoning, and transaction lifecycle tests |
| ES-002 | High | Event and outbox persistence could imply atomicity without sharing one transaction | Resolved | The `gooutbox` stager accepts one caller-owned PostgreSQL transaction; rollback, commit, ambiguity, duplicate, and replay-isolation tests cover the boundary |
| ES-003 | High | Replay could accidentally invoke process managers or external publication | Resolved | Live and replay deliveries are distinct; process managers and external adapters reject replay unless a separately named operation opts in |
| ES-004 | Medium | Payload serialization existed without a transport-neutral message-codec contract | Resolved | The core `MessageCodec` contract is separate from payload codecs and has ownership, validation, and round-trip tests |
| ES-005 | Medium | Release-facing package, queue, and adapter status documents contradicted implemented behavior | Resolved | Contract tests reject incomplete matrix states, an advertised first-release generator, stale Kafka status, and stale durable-queue status |
| ES-006 | Medium | Benchmark claims lacked one complete raw, reproducible evidence set | Resolved | Raw core, competitor, PostgreSQL, and outbox results include checksums, input fingerprints, environment provenance, and statistical summaries |
| ES-007 | Medium | Known event names at unsupported schema versions were reported as wholly unknown identities | Resolved | `ErrIncompatibleVersion` now distinguishes canonical and decode-alias version failures from unknown event names, with focused encode/decode regressions |
| ES-008 | Low | The pinned EventSauce matrix recorded 2026-04-25 as the package release date | Resolved | The matrix records the Packagist publication date and a separately dated source and documentation verification |
| ES-009 | High | Projection replay treated a restored event store behind its durable checkpoint as terminal | Resolved | The runner verifies the exact checkpoint position before terminal hooks, fails closed on missing history, and preserves terminal cancellation |
| ES-010 | Medium | A process manager could invoke its planner without declaring which stable events it accepts | Resolved | Construction requires a bounded unique event-name allowlist; accepted and ignored delivery scenarios prove planner isolation |
| ES-011 | High | Resumed projection replay had no required per-batch authorization or audit boundary | Resolved | Every runner requires a guard that runs before initial, resumed, and terminal hooks, reads, handlers, and checkpoint changes; rejection, panic, cancellation, and ordering tests fail closed |
| ES-012 | High | Applications had no storage-independent hook to authenticate event history before reconstitution, replay, or projection | Resolved | Verifying stream and global-reader decorators invoke one application verifier before message exposure, fail closed on rejection, panic, cancellation, or corrupt input, and retain store conformance |

Excluded EventSauce mechanisms and externally owned guarantees are decisions,
not open findings. They remain enumerated in the
[versioned compatibility matrix](compatibility/eventsauce-3.9.1.md).

## Residual risks

- Event-store and broker delivery is not end-to-end exactly once. Applications
  must make consumers idempotent and retain message IDs through retries.
- Direct PostgreSQL-to-Kafka dispatch is not atomic. Durable publication uses
  PostgreSQL event and outbox writes in one transaction, followed by
  at-least-once relay and explicit Kafka offset settlement.
- Applications own stable event names, payload schemas, codec registrations,
  upcasters, aggregate invariants, tenant authorization, and repair policy.
  The library cannot validate business meaning.
- Snapshots, projection checkpoints, and read models are derived state. They
  may be stale or unavailable and must remain deletable or rebuildable from
  authoritative history.
- Commit ambiguity, provider failover, backup restoration, retention,
  encryption, and legal deletion require application and operator procedures.
  The package exposes the relevant boundaries but cannot choose those policies.
- Kafka ordering is per partition. The aggregate-root key preserves
  per-aggregate order only while topic routing and partition counts follow the
  documented operational policy.
- EventSauce compatibility is conceptual and behavioral. PHP source, object
  hydration, framework wiring, and unspecified wire formats are not compatible.

## Evidence boundary

The source-controlled report records findings and decisions. It does not copy
mutable local or CI status into prose. Release readiness is derived from the
repository's content-addressed evidence for these independently releasable
modules:

- `pkg/event-sourcing`;
- `pkg/event-sourcing/postgres`;
- `pkg/event-sourcing/adapters/gokafka`;
- `pkg/event-sourcing/adapters/gooutbox`;
- `pkg/event-sourcing/adapters/goqueue`; and
- `pkg/event-sourcing/adapters/gotelemetry`.

For the exact release inputs, `make inventory`, every affected module check,
every affected release dry-run, the repository check, and the stable GitHub
Actions job must pass. A failed, cancelled, skipped, stale, advisory substitute,
or missing required result blocks release. NilAway remains advisory and visible;
its diagnostics are not a substitute for runtime evidence.

The compatibility matrix links each EventSauce capability to its expected
behavior, Go decision, focused tests, documentation, and status. Raw benchmark
evidence is published under [`benchmarks/results`](../benchmarks/results/), and
the [release evidence guide](release-notes.md) defines the SemVer-sensitive
surfaces and upgrade workflow.
