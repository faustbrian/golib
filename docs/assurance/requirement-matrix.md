# Operational-Assurance Requirement Matrix

This matrix maps every requirement in
[the operational-assurance goal](../../.ai/GOAL_OPERATIONAL_ASSURANCE.md) to
current evidence and the exact remaining proof. The machine-readable scenario
status remains authoritative in [`operational-assurance.json`](../../operational-assurance.json).

Statuses mean:

- **Proved:** current evidence covers the complete stated requirement.
- **Partial:** retained evidence covers only the named subset.
- **External:** proof requires a live deployment, provider, public release, or
  maintainer decision outside this repository.
- **Specialist-owned:** another active specialist owns the remaining campaign;
  its evidence may be consumed without rerunning unaffected Golib campaigns.
- **Consumer-owned:** each consuming service must prove its own policy or
  business behavior.

Pending and externally owned work does not invalidate content-identical passed
evidence and is not a reason to restart unrelated package verification.

Reviewed evidence-identity migrations are retained as part of the audit trail:

- [OpenSearch alias observer migration](evidence/OA-INPUT-DIGEST-MIGRATION-OPENSEARCH-ALIAS-OBSERVER.md)
- [JSON-RPC URL contract migration](evidence/OA-INPUT-DIGEST-MIGRATION-JSONRPC-URL-CONTRACT.md)
- [Reference durability matrix migration](evidence/OA-INPUT-DIGEST-MIGRATION-REFERENCE-DURABILITY-MATRIX.md)
- [Telemetry endpoint migration](evidence/OA-INPUT-DIGEST-MIGRATION-TELEMETRY-ENDPOINT.md)

## Reference Services

| Requirement | Status | Current evidence | Exact remaining proof and owner |
| --- | --- | --- | --- |
| HTTP/JSON-RPC ingress, routing, middleware, lifecycle, configuration, identity, authorization, tenancy, correlation, capabilities, signatures, validation, telemetry, and audit through public APIs | Proved | [`OA-REFERENCE-HTTP`](evidence/OA-REFERENCE-HTTP.md) | None for the reference scenario; production-service adoption remains consumer-owned. |
| PostgreSQL transaction, migration, outbox, idempotency, scheduler, workflow, audit, dead-letter, and reconciliation composition | Partial | [`OA-REFERENCE-DURABILITY-POSTGRES-VALKEY`](evidence/OA-REFERENCE-DURABILITY-POSTGRES-VALKEY.md), [`OA-REFERENCE-DURABILITY-POSTGRES-MATRIX`](evidence/OA-REFERENCE-DURABILITY-POSTGRES-MATRIX.md), [`OA-SCHEDULER-POSTGRES-VALKEY`](evidence/OA-SCHEDULER-POSTGRES-VALKEY.md), [`OA-MIGRATIONS-POSTGRES-RECOVERY`](evidence/OA-MIGRATIONS-POSTGRES-RECOVERY.md), [`OA-AUDIT-POSTGRES-OPERATIONS`](evidence/OA-AUDIT-POSTGRES-OPERATIONS.md), [`OA-FAILURE-RECOVERY-WORKFLOW`](evidence/OA-FAILURE-RECOVERY-WORKFLOW.md) | Complete managed failover/restore, storage exhaustion, and composed service business-path campaigns beyond the now-proved PostgreSQL 14-18 version matrix. Operational-assurance owner. |
| Valkey cache and queue composition, redelivery, settlement, leases, and dead letters | Partial | [`OA-REFERENCE-DURABILITY-POSTGRES-VALKEY`](evidence/OA-REFERENCE-DURABILITY-POSTGRES-VALKEY.md), [`OA-QUEUE-LIFECYCLE-REDIS-VALKEY`](evidence/OA-QUEUE-LIFECYCLE-REDIS-VALKEY.md), [`OA-SCHEDULER-POSTGRES-VALKEY`](evidence/OA-SCHEDULER-POSTGRES-VALKEY.md) | Prove managed Valkey failover, eviction, reconnect, stale leases, script interruption, cluster topology, and prolonged partition recovery. Operational-assurance owner. |
| Kafka, schema registry, CloudEvents, durable publication, poison records, and reconciliation | Specialist-owned | Existing package and specialist evidence is intentionally outside this lane. | Kafka specialist supplies final broker, schema-registry, failure, recovery, compatibility, and specification evidence. Do not rerun or modify Kafka here. |
| OpenSearch indexing, querying, migration, rebuild, rollback, and source-of-truth recovery | Partial | [`OA-OPENSEARCH-VERSION-MATRIX`](evidence/OA-OPENSEARCH-VERSION-MATRIX.md), [`OA-OPENSEARCH-ROLLING-RECOVERY`](evidence/OA-OPENSEARCH-ROLLING-RECOVERY.md), [`OA-OPENSEARCH-SECURITY-MATRIX`](evidence/OA-OPENSEARCH-SECURITY-MATRIX.md) | Prove managed failover, storage exhaustion, network partitions, production-sized full rebuild, and service-owned source-of-truth reconciliation. Operational-assurance and consumer owners. |
| External HTTP, webhooks, files/object storage, secrets, retries, limits, bulkheads, breakers, hedges, and timeouts through public APIs | Proved | [`OA-REFERENCE-EXTERNAL`](evidence/OA-REFERENCE-EXTERNAL.md) | Live providers, credentials, and production network behavior remain separate external deployment proof. |
| Reference startup, readiness, traffic, graceful shutdown, restart, and dependency recovery | Partial | [`OA-REFERENCE-HTTP`](evidence/OA-REFERENCE-HTTP.md), [`OA-REFERENCE-EXTERNAL`](evidence/OA-REFERENCE-EXTERNAL.md), [`OA-FAILURE-RECOVERY-POSTGRES-VALKEY`](evidence/OA-FAILURE-RECOVERY-POSTGRES-VALKEY.md), [`OA-TELEMETRY-COLLECTOR-OPERATIONS`](evidence/OA-TELEMETRY-COLLECTOR-OPERATIONS.md) | Complete composed durability startup/recovery, released rolling upgrade, rollback, and ECS lifecycle proof. |
| No private framework magic unavailable to consumers | Proved | [`OA-REFERENCE-HTTP`](evidence/OA-REFERENCE-HTTP.md), [`OA-REFERENCE-EXTERNAL`](evidence/OA-REFERENCE-EXTERNAL.md), [`OA-CROSS-PACKAGE-ADOPTION`](evidence/OA-CROSS-PACKAGE-ADOPTION.md) | None for maintained reference construction. |

## Platform Matrix

| Requirement | Status | Current evidence | Exact remaining proof and owner |
| --- | --- | --- | --- |
| Supported Go version and Linux `amd64`/`arm64` | Partial | [`OA-PLATFORM-MATRIX-LOCAL`](evidence/OA-PLATFORM-MATRIX-LOCAL.md) | Run native Graviton rather than emulated arm64 and retain pinned CI or ECS evidence. External platform owner. |
| ECS-compatible image, signals, non-root user, read-only filesystem, and ephemeral storage | Partial | [`OA-PLATFORM-MATRIX-LOCAL`](evidence/OA-PLATFORM-MATRIX-LOCAL.md) | Exercise live ECS task replacement, drain, writable-volume boundaries, and rollback. External platform owner. |
| DNS, TLS trust, task IAM credentials, and network constraints | Partial | [`OA-PLATFORM-MATRIX-LOCAL`](evidence/OA-PLATFORM-MATRIX-LOCAL.md), [`OA-OPENSEARCH-SECURITY-MATRIX`](evidence/OA-OPENSEARCH-SECURITY-MATRIX.md) | Prove live ECS DNS, egress policy, task IAM credential refresh, provider TLS roots, and constrained networks. External platform owner. |
| `CGO_ENABLED` policy, CPU/memory quotas, descriptors, and connection constraints | Partial | [`OA-PLATFORM-MATRIX-LOCAL`](evidence/OA-PLATFORM-MATRIX-LOCAL.md), [`OA-RESOURCE-PERFORMANCE-CONSTRAINED`](evidence/OA-RESOURCE-PERFORMANCE-CONSTRAINED.md) | Prove representative services under final ECS task limits and exhaustion/recovery thresholds. External platform and service owners. |
| Clean reproducible artifacts without developer paths, sibling replacements, ambient credentials, or warm caches | Partial | [`OA-PLATFORM-MATRIX-LOCAL`](evidence/OA-PLATFORM-MATRIX-LOCAL.md), [`OA-RELEASE-CONSUMER-ALL-LOCAL`](evidence/OA-RELEASE-CONSUMER-ALL-LOCAL.md) | Produce bit-for-bit reproducible release artifacts with signed provenance from clean CI. Release owner. |

## Failure And Recovery

| Requirement | Status | Current evidence | Exact remaining proof and owner |
| --- | --- | --- | --- |
| Process kill, panic, cancellation, deadlines, deploy replacement, and host loss | Partial | [`OA-FAILURE-RECOVERY-POSTGRES-VALKEY`](evidence/OA-FAILURE-RECOVERY-POSTGRES-VALKEY.md), [`OA-FAILURE-RECOVERY-WORKFLOW`](evidence/OA-FAILURE-RECOVERY-WORKFLOW.md), [`OA-QUEUE-LIFECYCLE-REDIS-VALKEY`](evidence/OA-QUEUE-LIFECYCLE-REDIS-VALKEY.md) | Add panic and host-loss campaigns across the composed reference plus ECS replacement. |
| PostgreSQL deadlock, serialization, connection loss, failover, interrupted migration, backup/restore, and storage exhaustion | Partial | [`OA-REFERENCE-DURABILITY-POSTGRES-MATRIX`](evidence/OA-REFERENCE-DURABILITY-POSTGRES-MATRIX.md), [`OA-FAILURE-RECOVERY-WORKFLOW`](evidence/OA-FAILURE-RECOVERY-WORKFLOW.md), [`OA-MIGRATIONS-POSTGRES-RECOVERY`](evidence/OA-MIGRATIONS-POSTGRES-RECOVERY.md), [`OA-AUDIT-POSTGRES-OPERATIONS`](evidence/OA-AUDIT-POSTGRES-OPERATIONS.md) | Supported PostgreSQL composition is proved; add serialization failures, managed failover, managed backup/restore, storage exhaustion, and recovery across the supported matrix. |
| Valkey failover, eviction, reconnect, script interruption, and stale leases | Partial | [`OA-FAILURE-RECOVERY-POSTGRES-VALKEY`](evidence/OA-FAILURE-RECOVERY-POSTGRES-VALKEY.md), [`OA-SCHEDULER-POSTGRES-VALKEY`](evidence/OA-SCHEDULER-POSTGRES-VALKEY.md), [`OA-QUEUE-LIFECYCLE-REDIS-VALKEY`](evidence/OA-QUEUE-LIFECYCLE-REDIS-VALKEY.md) | Prove managed failover, eviction pressure, interrupted scripts, stale lease fencing, and network partition recovery. |
| Kafka rebalance, duplicates, reorder, partitions, leader loss, poison records, acknowledgement ambiguity, and registry outage | Specialist-owned | Kafka specialist evidence is consumed at the final boundary. | Kafka specialist completes and supplies the exact matrix. |
| OpenSearch overload, partial shard/bulk failure, PIT expiry, failover, interrupted migration, snapshot/restore, and full rebuild | Partial | [`OA-OPENSEARCH-ROLLING-RECOVERY`](evidence/OA-OPENSEARCH-ROLLING-RECOVERY.md), [`OA-OPENSEARCH-VERSION-MATRIX`](evidence/OA-OPENSEARCH-VERSION-MATRIX.md) | Partial shards, partial bulk outcomes, PIT expiry, snapshot/restore, interruption, and bounded outage recovery are proved. Add prolonged overload, managed failover, network partitions, storage exhaustion, and production-sized full rebuild. |
| DNS, TLS, proxy, credential rotation, clock skew, quota, throttling, partial response, and malformed dependency behavior | Partial | [`OA-REFERENCE-EXTERNAL`](evidence/OA-REFERENCE-EXTERNAL.md), [`OA-KEY-LIFECYCLE`](evidence/OA-KEY-LIFECYCLE.md), [`OA-OPENSEARCH-SECURITY-MATRIX`](evidence/OA-OPENSEARCH-SECURITY-MATRIX.md) | Add live provider/proxy rotation, clock-skew, quota, malformed-provider, prolonged outage, and ECS network campaigns. |
| Idempotency, fencing, outbox, audit retention, workflow recovery, compensation, dead letters, reconciliation, and truthful unknown outcomes | Partial | [`OA-REFERENCE-DURABILITY-POSTGRES-VALKEY`](evidence/OA-REFERENCE-DURABILITY-POSTGRES-VALKEY.md), [`OA-FAILURE-RECOVERY-WORKFLOW`](evidence/OA-FAILURE-RECOVERY-WORKFLOW.md), [`OA-AUDIT-POSTGRES-OPERATIONS`](evidence/OA-AUDIT-POSTGRES-OPERATIONS.md) | Prove these guarantees through complete composed service business paths and external-side-effect reconciliation; retain at-least-once semantics and never claim external exactly-once. Consumer and operational-assurance owners. |

## Deployment And Compatibility

| Requirement | Status | Current evidence | Exact remaining proof and owner |
| --- | --- | --- | --- |
| Mixed old/new binaries, rolling ECS deployment, canary, and rollback | Partial | [`OA-QUEUE-LIFECYCLE-REDIS-VALKEY`](evidence/OA-QUEUE-LIFECYCLE-REDIS-VALKEY.md), [`OA-OPENSEARCH-ROLLING-RECOVERY`](evidence/OA-OPENSEARCH-ROLLING-RECOVERY.md) | Exercise released service binaries in live ECS canary, rolling replacement, failed deployment, and full rollback. External platform and service owners. |
| Schema forward/backward compatibility, old workers/new data, new workers/old data, and interrupted migrations | Partial | [`OA-REFERENCE-DURABILITY-POSTGRES-MATRIX`](evidence/OA-REFERENCE-DURABILITY-POSTGRES-MATRIX.md), [`OA-MIGRATIONS-POSTGRES-RECOVERY`](evidence/OA-MIGRATIONS-POSTGRES-RECOVERY.md), [`OA-AUDIT-POSTGRES-OPERATIONS`](evidence/OA-AUDIT-POSTGRES-OPERATIONS.md), [`OA-OPENSEARCH-VERSION-MATRIX`](evidence/OA-OPENSEARCH-VERSION-MATRIX.md) | Supported PostgreSQL composition and engine versions are proved; complete released mixed-binary/data and in-place database upgrade campaigns. |
| Key rotation overlap and retirement | Partial | [`OA-KEY-LIFECYCLE`](evidence/OA-KEY-LIFECYCLE.md), [`OA-OPENSEARCH-SECURITY-MATRIX`](evidence/OA-OPENSEARCH-SECURITY-MATRIX.md) | Prove live Infisical/provider distribution, KMS/HSM where used, task population drain, and incident rollback. External platform and service owners. |
| Dependency-ordered `v1.0.0` releases, clean external install, upgrade, downgrade, deprecation, and removal | Partial | [`OA-RELEASE-CONSUMER-LOCAL`](evidence/OA-RELEASE-CONSUMER-LOCAL.md), [`OA-RELEASE-CONSUMER-ALL-LOCAL`](evidence/OA-RELEASE-CONSUMER-ALL-LOCAL.md) | Publish signed immutable `v1.0.0` tags, resolve via the public proxy/checksum database, run final package gates, and exercise documented upgrades. Release owner; only the user authorizes release. |

## Resource And Performance

| Requirement | Status | Current evidence | Exact remaining proof and owner |
| --- | --- | --- | --- |
| Reproducible realistic and adversarial load with latency, throughput, allocation, heap, GC, goroutine, thread, descriptor, connection, timer, queue, retry, telemetry, storage, and recovery measurements | Partial | [`OA-RESOURCE-PERFORMANCE-CONSTRAINED`](evidence/OA-RESOURCE-PERFORMANCE-CONSTRAINED.md), [`OA-OPENSEARCH-VERSION-MATRIX`](evidence/OA-OPENSEARCH-VERSION-MATRIX.md) | Extend metrics to threads, connections, timers, queues, retries, telemetry and storage; run realistic dependency traffic and multi-hour soak. |
| Per-service/per-operation budgets under strict ECS CPU and memory limits | Partial | [`OA-RESOURCE-PERFORMANCE-CONSTRAINED`](evidence/OA-RESOURCE-PERFORMANCE-CONSTRAINED.md) | Define and prove budgets for each consuming service on final ECS task sizes. Consumer and platform owners. |
| Stress-to-failure, backpressure, bounded degradation, and recovery | Partial | [`OA-RESOURCE-PERFORMANCE-CONSTRAINED`](evidence/OA-RESOURCE-PERFORMANCE-CONSTRAINED.md), [`OA-OPENSEARCH-ROLLING-RECOVERY`](evidence/OA-OPENSEARCH-ROLLING-RECOVERY.md) | Run stress-to-failure, queue growth, retry-storm, dependency saturation, and post-exhaustion recovery campaigns. |
| Equivalent competitor comparisons without substituting benchmarks for service load | Partial | [`OA-OPENSEARCH-VERSION-MATRIX`](evidence/OA-OPENSEARCH-VERSION-MATRIX.md), [benchmark catalog](../benchmark-catalog.md) | Complete fair comparison tracks where cataloged and retain separate service-level load evidence. Performance owner. |

## Security, Privacy, And Supply Chain

| Requirement | Status | Current evidence | Exact remaining proof and owner |
| --- | --- | --- | --- |
| Composition threat model for identity, tenancy, authorization, capabilities, signatures, replay, audit, event/schema trust, SSRF, injection, deserialization, secrets, and dependency compromise | Partial | [threat model](../security/threat-model.md), [security matrix](../security/security-matrix.md), [`OA-REFERENCE-HTTP`](evidence/OA-REFERENCE-HTTP.md), [`OA-REFERENCE-EXTERNAL`](evidence/OA-REFERENCE-EXTERNAL.md) | Add final Kafka/schema trust evidence and review each deployed service's data flows, providers, and business authorization. Specialist and consumer owners. |
| Credential/signing-key rotation, revocation, compromise response, and least privilege | Partial | [`OA-KEY-LIFECYCLE`](evidence/OA-KEY-LIFECYCLE.md), [`OA-OPENSEARCH-SECURITY-MATRIX`](evidence/OA-OPENSEARCH-SECURITY-MATRIX.md), [`OA-AUDIT-POSTGRES-OPERATIONS`](evidence/OA-AUDIT-POSTGRES-OPERATIONS.md) | Run live Infisical/provider/KMS credential drills with deployed workloads and verified alerts/runbooks. External platform and service owners. |
| PII redaction, retention, erasure, legal hold, and forensic export | Consumer-owned | [`OA-AUDIT-POSTGRES-OPERATIONS`](evidence/OA-AUDIT-POSTGRES-OPERATIONS.md), [`OA-TELEMETRY-COLLECTOR-OPERATIONS`](evidence/OA-TELEMETRY-COLLECTOR-OPERATIONS.md), [`GL-RISK-006`](../security/residual-risks.md#gl-risk-006-privacy-operations-remain-service-specific) | Each service must classify data and exercise lawful retention, erasure, hold, export, and sampling policy. Generic library proof cannot close this. |
| Signed releases, immutable provenance, SBOMs, reproducibility, vulnerability response, license policy, and clean artifacts | External | [`OA-RELEASE-CONSUMER-ALL-LOCAL`](evidence/OA-RELEASE-CONSUMER-ALL-LOCAL.md), dependency and license repository gates | Produce signed `v1.0.0` releases, attestations, SBOMs, checksums, reproducibility evidence, and final dependency dispositions in CI. Release/security owners. |

## Observability And Operations

| Requirement | Status | Current evidence | Exact remaining proof and owner |
| --- | --- | --- | --- |
| SLOs, SLIs, metrics, traces, structured logs, correlation, and bounded cardinality | Partial | [`OA-REFERENCE-HTTP`](evidence/OA-REFERENCE-HTTP.md), [`OA-TELEMETRY-COLLECTOR-OPERATIONS`](evidence/OA-TELEMETRY-COLLECTOR-OPERATIONS.md), [`OA-OPENSEARCH-OPERATIONS`](evidence/OA-OPENSEARCH-OPERATIONS.md) | Install service-specific SLOs and prove Better Stack export, retention, cardinality, sampling, and alert delivery under production traffic. Consumer/operations owners. |
| Dashboards, alerts, runbooks, capacity limits, dead-letter and reconciliation procedures | Partial | [`OA-OPENSEARCH-OPERATIONS`](evidence/OA-OPENSEARCH-OPERATIONS.md), [`OA-AUDIT-POSTGRES-OPERATIONS`](evidence/OA-AUDIT-POSTGRES-OPERATIONS.md), [`OA-QUEUE-LIFECYCLE-REDIS-VALKEY`](evidence/OA-QUEUE-LIFECYCLE-REDIS-VALKEY.md) | Verify installed dashboards and alert routing, then complete procedures for every critical composed dependency and workflow. Operations and consumer owners. |
| Backup/restore, migration, rollback, and security incident procedures | Partial | [`OA-MIGRATIONS-POSTGRES-RECOVERY`](evidence/OA-MIGRATIONS-POSTGRES-RECOVERY.md), [`OA-AUDIT-POSTGRES-OPERATIONS`](evidence/OA-AUDIT-POSTGRES-OPERATIONS.md), [`OA-KEY-LIFECYCLE`](evidence/OA-KEY-LIFECYCLE.md) | Exercise managed backups, full service rollback, live key compromise, and provider incident procedures. |
| Human drills for stuck work, audit verification, restore, search rebuild, schema incompatibility, key compromise, outages, and failed deployment | External | Repository runbooks and [`OA-OPENSEARCH-OPERATIONS`](evidence/OA-OPENSEARCH-OPERATIONS.md) structurally validate declared procedures. | Execute and record human operator drills in the deployment environment. Operations owner. |

## Cross-Package Consistency

| Requirement | Status | Current evidence | Exact remaining proof and owner |
| --- | --- | --- | --- |
| Context, cancellation, deadlines, clock, correlation, tenant, idempotency, errors, retryability, unknown outcomes, redaction, telemetry, ownership, configuration, and shutdown | Partial | [`OA-CROSS-PACKAGE-ADOPTION`](evidence/OA-CROSS-PACKAGE-ADOPTION.md), [`OA-KEY-LIFECYCLE`](evidence/OA-KEY-LIFECYCLE.md), [`OA-OPENSEARCH-VERSION-MATRIX`](evidence/OA-OPENSEARCH-VERSION-MATRIX.md), [`OA-OPENSEARCH-SECURITY-MATRIX`](evidence/OA-OPENSEARCH-SECURITY-MATRIX.md) | Prove tenant/idempotency propagation, redaction, exported telemetry, durable dependencies, and real business paths in composed services. Correct contradictions at owning packages. |
| Representative Track, Postal, and Location public adoption | Partial | [`OA-CROSS-PACKAGE-ADOPTION`](evidence/OA-CROSS-PACKAGE-ADOPTION.md) | Replace fixtures with consumer-owned service evidence as ports become deployable; fixtures remain valid for generic public construction. |

## Mandatory Evidence And Verdict

| Requirement | Status | Current evidence | Exact remaining proof and owner |
| --- | --- | --- | --- |
| Exact 100% meaningful coverage and viable mutation kills for affected repository/package gates | Partial | Package-attributable gate evidence and [hardening report](../hardening-report.md) | Consume current specialist evidence, resolve remaining aggregate goal-audit findings, and rerun only changed packages and actual reverse dependants. Repository hardening owner. |
| Linux architectures, container, ECS, clean consumer, and release evidence | Partial | [`OA-PLATFORM-MATRIX-LOCAL`](evidence/OA-PLATFORM-MATRIX-LOCAL.md), [`OA-RELEASE-CONSUMER-ALL-LOCAL`](evidence/OA-RELEASE-CONSUMER-ALL-LOCAL.md) | Native Graviton, live ECS, signed release, public proxy, and checksum evidence. External platform/release owners. |
| Load, soak, chaos, recovery, rolling upgrade, and rollback reports | Partial | [`OA-RESOURCE-PERFORMANCE-CONSTRAINED`](evidence/OA-RESOURCE-PERFORMANCE-CONSTRAINED.md), [`OA-OPENSEARCH-ROLLING-RECOVERY`](evidence/OA-OPENSEARCH-ROLLING-RECOVERY.md) | Multi-hour soak, stress-to-failure, broader chaos, service rollback, and final-capacity proof. |
| Backup/restore, migrations, reconciliation, search rebuild, workflow recovery, and dead-letter drills | Partial | [`OA-MIGRATIONS-POSTGRES-RECOVERY`](evidence/OA-MIGRATIONS-POSTGRES-RECOVERY.md), [`OA-AUDIT-POSTGRES-OPERATIONS`](evidence/OA-AUDIT-POSTGRES-OPERATIONS.md), [`OA-FAILURE-RECOVERY-WORKFLOW`](evidence/OA-FAILURE-RECOVERY-WORKFLOW.md), [`OA-OPENSEARCH-VERSION-MATRIX`](evidence/OA-OPENSEARCH-VERSION-MATRIX.md) | Managed provider and production-sized drills plus human operations evidence. |
| Threat model, privacy review, supply-chain attestations, and dependency audit | Partial | [threat model](../security/threat-model.md), [security matrix](../security/security-matrix.md), [residual risks](../security/residual-risks.md) | Service privacy reviews, final dependency dispositions, signed provenance, SBOMs, and release attestations. |
| Verified operations artifacts and incident exercises | Partial | [`OA-OPENSEARCH-OPERATIONS`](evidence/OA-OPENSEARCH-OPERATIONS.md), [`OA-TELEMETRY-COLLECTOR-OPERATIONS`](evidence/OA-TELEMETRY-COLLECTOR-OPERATIONS.md) | Installed production artifacts, alert delivery, capacity limits, and human drills for every critical dependency/workflow. |
| Complete matrix and residual-risk register | Proved | This matrix and [residual-risk register](../security/residual-risks.md) | Keep both current as evidence changes; only the user may accept risks. |
| Explicit completion verdict | Proved | [`operational-assurance.json`](../../operational-assurance.json) and [operational assurance](../operational-assurance.md) | Current verdict is `not ready`; changing it requires all scenarios passed or named user-accepted risks. No release or deployment is authorized. |
