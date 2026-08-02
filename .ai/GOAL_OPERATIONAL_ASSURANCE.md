# Goal: Repository Operational Assurance

## Objective

Certify the completed Golib ecosystem as a composed production platform rather
than a collection of independently passing modules. This goal executes after
all package implementation, hardening, compatibility, security, supply-chain,
performance, documentation, polish, monorepo, and repository gates and before
release authorization.

Passing package tests is necessary but insufficient. Completion requires
executable evidence for deployment, composition, durability, failure recovery,
resource behavior, upgrades, operations, and clean external adoption.

## Scope And Claim Model

Inventory every releasable module, adapter, supported Go/OS/architecture,
external service, deployment target, specification, cross-package contract,
and production claim. Build a requirement-to-evidence matrix. Every exclusion,
simulation, unavailable service, unsupported platform, and residual risk MUST
remain explicit.

Evidence MUST use content-identity rules from `AGENTS.md`; unrelated Git history
changes MUST not invalidate it. A changed package invalidates only affected
composition scenarios and reverse dependants.

## Reference Services

Build maintained non-production reference services exercising the recommended
stack through public APIs only:

- HTTP/JSON-RPC ingress, router, middleware, service lifecycle, configuration,
  authentication, authorization, tenancy, correlation, capabilities, HTTP
  signatures, validation, telemetry, and audit;
- PostgreSQL, migrations, Valkey cache/queue, Kafka, schema registry,
  CloudEvents, outbox, idempotency, scheduler, workflow, dead letters, and
  reconciliation;
- OpenSearch indexing, querying, migration, rebuild, and source-of-truth
  recovery;
- external HTTP clients, webhooks, filesystem/object storage, secrets, retries,
  rate limits, bulkheads, breakers, adaptive controls, hedges, and timeouts.

The reference services MUST prove startup, readiness, traffic, graceful
shutdown, restart, upgrade, rollback, and dependency recovery. They MUST not
introduce private framework magic unavailable to consumers.

## Platform Matrix

Verify the repository's supported Go version and Linux `amd64` and `arm64` at
minimum. Explicitly test ECS-compatible containers, Graviton, signal handling,
read-only filesystems, non-root execution, ephemeral storage, DNS, TLS trust,
task IAM credentials, `CGO_ENABLED` policy, CPU quotas, memory limits, file
descriptors, and network constraints.

Build and test artifacts MUST be reproducible from a clean checkout without
developer paths, sibling replacements, ambient credentials, or warm caches.

## Failure And Recovery Matrix

Inject failures before and after every durable or externally visible boundary:

- process kill, panic, cancellation, deadline, deploy replacement, and host
  loss;
- PostgreSQL deadlock, serialization failure, connection loss, failover,
  migration interruption, backup, restore, and storage exhaustion;
- Valkey failover, eviction, reconnect, script interruption, and stale lease;
- Kafka rebalance, duplicate, reorder, partition, leader loss, poison record,
  acknowledgement ambiguity, and schema-registry outage;
- OpenSearch overload, partial shard/bulk failure, point-in-time expiry,
  failover, migration interruption, snapshot/restore, and full rebuild;
- DNS, TLS, proxy, credential rotation, clock skew, quota, throttling, partial
  response, and malformed dependency behavior.

Prove idempotency, fencing, outbox publication, audit retention, workflow
recovery, compensation, dead letters, reconciliation, and truthful unknown
outcomes. Exactly-once behavior MUST not be claimed for external side effects.

## Deployment And Compatibility

Exercise mixed old/new versions, rolling ECS deployments, canaries, rollback,
schema-forward and schema-backward compatibility, old workers with new data,
new workers with old data, key rotation overlap, and interrupted migrations.

Prove versioned package releases in dependency order, clean external consumer
installation, minimal-version and current-version policy, upgrade, downgrade
where supported, deprecation, and removal. Pre-release local replacements MUST
not appear in final consumer evidence.

## Resource And Performance Assurance

Run reproducible load, stress, and multi-hour or multi-day soak campaigns under
realistic and adversarial traffic. Measure latency distributions, throughput,
allocations, heap growth, GC, goroutines, threads, descriptors, connections,
timers, queues, retries, telemetry volume, storage growth, and recovery time.

Define per-service and per-operation budgets. Test overload and recovery under
strict ECS CPU/memory limits. Prove backpressure and bounded degradation rather
than only peak throughput. Benchmarks MUST compare equivalent behavior and must
not substitute for service-level load tests.

## Security, Privacy, And Supply Chain

Perform composition-level threat modeling for identity, tenant isolation,
authorization, signed capabilities, HTTP signatures, replay, audit integrity,
event/schema trust, SSRF, injection, deserialization, secret exposure, and
dependency compromise.

Exercise credential and signing-key rotation, revocation, leaked-secret
response, PII redaction, retention, erasure, legal hold, forensic export, and
least privilege. Verify signed releases, immutable provenance, SBOMs,
reproducibility, vulnerability response, license policy, and clean artifacts.

## Observability And Operations

For every critical dependency and workflow, provide and verify SLOs, SLIs,
metrics, traces, structured logs, dashboards, alerts, runbooks, capacity limits,
dead-letter procedures, reconciliation, backup/restore, migration, rollback,
and security incident procedures.

Run operator drills for stuck queues/workflows, audit verification, database
restore, search rebuild, schema incompatibility, key compromise, dependency
outage, and failed deployment. Diagnostics MUST preserve correlation while
avoiding high-cardinality metrics and sensitive payloads.

## Cross-Package Consistency

Audit public composition for context propagation, cancellation, deadlines,
clock use, correlation, tenant scope, idempotency, error classification,
retryability, unknown outcomes, redaction, telemetry, resource ownership,
configuration, and shutdown. Contradictory defaults or overlapping ownership
MUST be corrected at the owning package rather than patched in the reference
service.

## Mandatory Evidence

- all affected repository and package gates with exact 100% meaningful
  statement coverage and 100% viable mutation kills;
- Linux `amd64`/`arm64`, container, ECS, clean-consumer, and release evidence;
- service-level load, soak, chaos, recovery, rolling-upgrade, and rollback
  reports with pinned environments;
- backup/restore, migration, reconciliation, OpenSearch rebuild, workflow
  recovery, and dead-letter drills;
- threat model, privacy review, supply-chain attestations, and dependency audit;
- verified dashboards, alerts, runbooks, SLOs, capacity limits, and incident
  exercises;
- a complete requirement-to-evidence matrix and residual-risk register.

## Completion Verdict

Produce one explicit verdict: `ready`, `not ready`, or `ready with named
accepted risks`. Only the user may accept residual production risks or authorize
release. Missing tools, credentials, services, platforms, evidence, or owner
decisions are blockers, not passes. This goal MUST NOT publish or deploy.
