# Operational Assurance

Operational assurance certifies Golib as a composed production platform rather
than treating independently passing modules as deployment proof. The full
contract is [`.ai/GOAL_OPERATIONAL_ASSURANCE.md`](../.ai/GOAL_OPERATIONAL_ASSURANCE.md).

## Current Verdict

The current verdict is **not ready**. Two of the eleven required composition
scenarios have final evidence, nine remain pending, and six named residual risks
remain open. This is an honest release boundary, not a package-quality
regression and not a reason to rerun content-identical package campaigns.

`OA-PLATFORM-MATRIX` now retains passing local Linux amd64/arm64 container
evidence, but remains pending for native Graviton, live ECS task IAM and
lifecycle, bit-for-bit artifact reproducibility, and production network
boundaries.

`OA-REFERENCE-DURABILITY` now retains passing PostgreSQL and Valkey evidence
for transaction rollback, atomic business/idempotency/outbox commit, relay,
unacknowledged-task reclamation, acknowledgement, and command replay. It
remains pending for the other required durability systems and recovery cases.

`operational-assurance.json` is the machine-readable authority. It catalogs
every releasable module, every mandatory scenario, evidence paths and SHA-256
digests, complete current-input fingerprints, environments, UTC observation
times, residual risks, and explicit risk acceptances. Git commits are
navigation metadata and are not evidence identity.

Run:

```text
make operational-assurance
go run ./cmd/golib assurance --format json
go run ./cmd/golib assurance --require-ready
```

Validation succeeds when the register is structurally complete and all stored
evidence still matches its content digest. `--require-ready` additionally
fails unless the verdict is `ready` or `ready with named accepted risks`.
Release planning reports the current verdict; any future mutating release path
must pass the ready check before package gates or publication begin.

## Mandatory Scenarios

| Identifier | Required proof |
| --- | --- |
| `OA-REFERENCE-HTTP` | Public-API composition of ingress, lifecycle, configuration, identity, authorization, tenancy, correlation, capabilities, HTTP signatures, validation, telemetry, and audit |
| `OA-REFERENCE-DURABILITY` | PostgreSQL, migrations, cache, queue, Kafka, schema, outbox, idempotency, scheduler, workflow, dead-letter, and reconciliation composition |
| `OA-REFERENCE-EXTERNAL` | HTTP clients, webhooks, filesystem/object storage, secrets, retries, rate limits, bulkheads, breakers, hedges, and timeout composition |
| `OA-PLATFORM-MATRIX` | Supported Go, Linux amd64/arm64, ECS-compatible container, Graviton, non-root/read-only runtime, quotas, descriptors, TLS, DNS, and credential behavior |
| `OA-FAILURE-RECOVERY` | Process and dependency failure, ambiguity, failover, restore, poison work, reconciliation, and truthful unknown outcomes |
| `OA-DEPLOYMENT-COMPATIBILITY` | Rolling deploy, canary, rollback, mixed binary/data versions, key overlap, and interrupted migration |
| `OA-RESOURCE-PERFORMANCE` | Equivalent service load, stress, soak, backpressure, budgets, leak detection, growth, and recovery under finite limits |
| `OA-SECURITY-PRIVACY-SUPPLY-CHAIN` | Composition threat model, rotation/revocation, redaction, retention/erasure, least privilege, signed provenance, and dependency response |
| `OA-OBSERVABILITY-OPERATIONS` | SLOs, metrics, traces, logs, dashboards, alerts, runbooks, capacity limits, backup/restore, dead-letter, and operator drills |
| `OA-CROSS-PACKAGE-CONSISTENCY` | Context, deadlines, clock, tenant, correlation, idempotency, retryability, redaction, telemetry, ownership, and shutdown consistency |
| `OA-RELEASE-CONSUMER` | Dependency-ordered `v1.0.0` release proof, clean external consumers, public resolution, signatures, attestations, and checksums |

## Evidence Rules

A `passed` scenario requires at least one repository-relative regular evidence
file, an exact SHA-256 digest, a UTC RFC 3339 observation time, an environment
description, and coverage of every affected module. Each evidence item also
records the canonical gate-input fingerprint for every scoped releasable
module and releasable reverse dependant. A current input change invalidates
only evidence whose expanded scope contains that module; a Git-history-only
change does not. Absolute paths, traversal, symlink escapes, stale artifact or
input digests, unknown modules, missing scenarios, duplicate records, and
unsupported statuses fail validation.

Only the user may accept a residual production risk. A `ready with named
accepted risks` verdict requires a complete acceptance naming the decision
maker, UTC time, rationale, and affected scenarios for every residual risk.
The validator does not infer acceptance from prose, issue state, package tests,
or the existence of a report.

See the [threat model](security/threat-model.md),
[security matrix](security/security-matrix.md), and
[residual-risk register](security/residual-risks.md).
