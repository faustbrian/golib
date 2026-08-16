# Operational Assurance

Operational assurance certifies Golib as a composed production platform rather
than treating independently passing modules as deployment proof. The full
contract is [`.ai/GOAL_OPERATIONAL_ASSURANCE.md`](../.ai/GOAL_OPERATIONAL_ASSURANCE.md).

## Current Verdict

The current verdict is **not ready**. Two of the eleven required composition
scenarios have final evidence, nine remain pending, and six named residual risks
remain open. This is an honest release boundary, not a package-quality
regression and not a reason to rerun content-identical package campaigns.

The [complete requirement-to-evidence matrix](assurance/requirement-matrix.md)
separates proved, partial, external, specialist-owned, and consumer-owned work
and names the exact remaining proof for every goal requirement.

`OA-PLATFORM-MATRIX` now retains passing local Linux amd64/arm64 container
evidence, but remains pending for native Graviton, live ECS task IAM and
lifecycle, bit-for-bit artifact reproducibility, and production network
boundaries.

`OA-REFERENCE-DURABILITY` now retains passing PostgreSQL and Valkey evidence
for transaction rollback, atomic business/idempotency/outbox commit, relay,
unacknowledged-task reclamation, acknowledgement, and command replay. It
also retains queue-service lifecycle evidence for timeout redelivery,
dead-letter recovery, Redis/Valkey settlement, scheduler lease fencing,
migration interruption recovery, and audit backup/restore with retained legal
holds. A real OpenSearch matrix adds snapshot/restore, durable stale-write
rejection, rebuild, reconciliation, rollback, and mixed application-protocol
evidence on both supported engine versions. A separate reference campaign now
proves the same PostgreSQL/Valkey composition on every supported PostgreSQL
major from 14 through 18. It remains pending for the other required durability
systems and production recovery cases.

`OA-RELEASE-CONSUMER` now retains a deterministic local `v1.0.0` source-proxy
and clean-consumer proof for all 107 releasable modules. Every module resolved
at exact `v1.0.0` with `GOWORK=off`, no replacements, and one listed public
package. A separate clean-clone campaign also passed the dependency-ordered
release dry-run for every module, including isolated tidy, test, API, local
packaging, tag-shape, and per-module consumer checks. It remains pending for
public proxy and checksum resolution, signatures, attestations, upgrade policy,
and release authorization.

`OA-RESOURCE-PERFORMANCE` now retains a passing constrained native-Linux
service campaign with explicit throughput, latency, heap, goroutine,
descriptor, and error budgets. The OpenSearch matrix adds bounded real-engine
load and equivalent adapter/direct-client comparisons on both supported
versions. It remains pending for multi-hour soak, stress-to-failure, broader
realistic dependency load, fleet behavior, exhaustion recovery, and production
capacity proof.

`OA-FAILURE-RECOVERY` now retains passing local process-death and
PostgreSQL/Valkey replacement evidence, plus focused workflow evidence for
deadlocks, process death, unknown activity outcomes, snapshot restore, replica
promotion, fencing, audited dead-letter resolution, scheduler ambiguity,
migration interruption, and local audit backup/restore reconciliation. It
also retains local JWT/OIDC outage, retired-key rollback, revocation-store
outage, fail-closed key-lifecycle evidence, and real Collector outage recovery
without interrupting business traffic. A real two-node OpenSearch campaign
also proves bounded endpoint and complete-cluster outages, unknown-write
reconciliation, mixed-version operation, and recovery through a rolling
2.19.6-to-3.8.0 replacement. A secured-engine matrix adds TLS trust failure,
credential rotation, DNS target replacement, degraded-health detection, and
recovery on both supported versions. It remains pending for managed database
and OpenSearch failover, storage exhaustion, network partitions, Kafka, live
credential providers, prolonged telemetry outages, full search rebuild, and
broader external-side-effect reconciliation.

`OA-DEPLOYMENT-COMPATIBILITY` now retains bounded queue-worker rolling
replacement and scale-up evidence on Redis and Valkey, plus migration history,
Laravel baseline, interrupted-migration recovery, and audit protocol-compatible
writer behavior across a durability migration on PostgreSQL 18.4. It remains
pending for released mixed application binary and data versions, ECS rolling
deployment and canary behavior, complete application rollback, and live key
rotation. The reference durability campaign proves the public composition on
PostgreSQL 14 through 18, while the OpenSearch campaign proves a real engine
rolling upgrade with old/new nodes serving the same fixture throughout. Its
security matrix proves runtime credential rotation without client
reconstruction. Local bearer, API-key, JWT, OIDC, capability, HTTP-signature,
and cursor tests now prove bounded overlap and retirement under the race
detector.

`OA-SECURITY-PRIVACY-SUPPLY-CHAIN` and
`OA-OBSERVABILITY-OPERATIONS` reuse the unchanged reference HTTP campaign as
bounded evidence for signed requests, fail-closed authorization and tenancy,
correlation, in-memory telemetry and audit, readiness recovery, and graceful
shutdown. Both also retain a local PostgreSQL audit campaign proving immutable
history, privacy validation, least privilege, legal holds, retention, and
backup/restore reconciliation. A real Collector campaign now proves local
OTLP/gRPC trace and metric export, bounded operation during a short Collector
outage, recovery, graceful flush, and omission of an injected sensitive marker.
The OpenSearch operations contract also validates bounded-cardinality dashboard
signals, alert-to-runbook links, and procedures for every declared search
incident drill, backed by the separate real-engine campaigns. They remain
pending for complete privacy lifecycle, dependency response, signed
provenance, production Better Stack export, production SLO installation,
dashboard and alert delivery, and human operator drills.

`OA-SECURITY-PRIVACY-SUPPLY-CHAIN` additionally retains a bounded cross-package
key-lifecycle campaign covering atomic replacement, overlap, refresh,
retirement, revocation, compromise response, rollback rejection, and fail-closed
provider outages. Live provider, KMS, secret-distribution, and incident drills
remain unproved. A separate secured OpenSearch matrix proves peer-verified TLS,
fail-closed trust, least-privilege tenant isolation, credential rotation, and
denial of cross-tenant and operator surfaces on both supported engine versions.

`OA-CROSS-PACKAGE-CONSISTENCY` retains the representative Track, Postal, and
Location adoption fixture for public package construction, role isolation,
correlation, bounded resilience, fleet behavior, and lifecycle consistency. A
separate race-enabled key campaign proves aligned overlap, retirement,
revocation, and outage behavior across six security-facing modules. The
OpenSearch matrix additionally proves that the search core and production
adapter preserve shared semantics across rebuild, reconciliation, rollback,
mixed application versions, and both supported engine versions. The scenario
also retains real adapter-level tenant isolation and operator/runtime role
separation. It remains pending for the other durable dependencies, tenant and
idempotency propagation through a composed service, redaction, exported
telemetry, and real service business paths.

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

Validation succeeds when the register is structurally complete, every stored
artifact still matches its content digest, and evidence supporting a passed or
accepted-risk scenario matches current inputs. Evidence retained under a
pending, failed, or unavailable scenario is historical partial evidence: its
recorded input scope remains validated, but it does not block repository
maintenance and cannot support readiness. `--require-ready` additionally fails
unless the verdict is `ready` or `ready with named accepted risks`.
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
module and releasable reverse dependant. Evidence produced by a non-releasable
harness records that harness as an explicit input module, so harness changes
also invalidate current proof. A current input change invalidates current
evidence whose expanded scope contains that module; a Git-history-only change
does not. Evidence under a non-ready scenario remains attributable historical
context until that scenario is rerun and promoted, but it is not current proof.
When a broad fingerprint changes solely because an audited transitive input is
irrelevant to an earlier campaign, the register may retain the original
observation through an exact one-way input-digest migration. Each migration is
bound to one module, one previous digest, one current digest, a reviewed
repository artifact and its SHA-256 digest, and a rationale. Migrations may
form an explicit chain but cannot match another module or an unlisted digest.
Absolute paths, traversal, symlink escapes, stale artifacts, stale current
evidence, unknown modules, missing scenarios, duplicate records, and
unsupported statuses fail validation.

Only the user may accept a residual production risk. A `ready with named
accepted risks` verdict requires a complete acceptance naming the decision
maker, UTC time, rationale, and affected scenarios for every residual risk.
The validator does not infer acceptance from prose, issue state, package tests,
or the existence of a report.

See the [threat model](security/threat-model.md),
[security matrix](security/security-matrix.md), and
[residual-risk register](security/residual-risks.md).
