# Repository Threat Model

## Scope

This model covers the public modules, optional adapters, repository tooling,
verification evidence, and independent release process in `golib`. It does not
claim that a consuming service is secure merely because it uses these modules.
Applications still own business authorization, data classification, network
policy, credentials, deployment configuration, and incident response.

The model is reviewed whenever a module adds a new trust boundary, implicit
I/O, durable state, credential type, parser, cryptographic operation, release
path, or externally visible protocol behavior.

## Assets And Adversaries

Protected assets include credentials and signing keys, authentication and
authorization decisions, tenant identity, durable records, queue and event
position, audit integrity, schema identity, request and payload confidentiality,
release artifacts, provenance, and verification evidence.

Relevant adversaries include unauthenticated network callers, authenticated
cross-tenant callers, malicious webhook or broker producers, compromised
dependencies or CI actions, hostile files and protocol documents, compromised
operators or credentials, and accidental callers that trigger unsafe retry,
shutdown, migration, or ownership behavior.

## Trust Boundaries

| Boundary | Attacker control | Required ownership |
| --- | --- | --- |
| HTTP, JSON-RPC, JSON:API, webhook, and middleware ingress | Headers, paths, bodies, identifiers, ordering, cancellation, and timing | Transport packages bound input and preserve exact bytes where signatures require them; applications authenticate, authorize, and classify data |
| Parsers and document formats | Size, depth, references, encodings, compression ratios, and malformed structures | Format owners require explicit limits and prohibit implicit network or filesystem resolution |
| Authentication and authorization | Credentials, tokens, claims, keys, policy inputs, and cache timing | `authentication` establishes identity; `authorization` makes explicit fail-closed decisions; applications own membership and business policy |
| Tenant and correlation context | Missing, forged, stale, or cross-boundary identifiers | `tenancy` and `correlation` carry validated context; context is not itself authentication, authorization, or idempotency proof |
| PostgreSQL and durable workflows | Concurrent writers, transaction ambiguity, deadlocks, failover, stale ownership, and hostile persisted data | Storage adapters define transaction ownership, fencing, reconciliation, and unknown outcomes; callers own schema and business invariants |
| Valkey, Redis, queues, Kafka, and webhooks | Duplicate, delayed, reordered, poisoned, replayed, or ambiguously acknowledged work | Queue, outbox, idempotency, lease, scheduler, workflow, and webhook owners expose at-least-once and recovery semantics without claiming external exactly-once effects |
| External HTTP, filesystem, object storage, DNS, TLS, and proxies | Redirects, SSRF targets, partial responses, path traversal, symlinks, quotas, and credential rotation | Clients and adapters require caller-selected policy, bounded I/O, explicit capabilities, and redacted diagnostics |
| Logs, traces, metrics, audit, and evidence | Sensitive payloads, credentials, tenant identifiers, high-cardinality values, and forged metadata | Producers redact by default, bound cardinality, and keep immutable audit semantics distinct from telemetry |
| Build, CI, dependencies, and releases | Dependency substitution, action compromise, stale evidence, malicious fixtures, tag replacement, and artifact tampering | Pinned tools/actions, isolated module resolution, content-addressed evidence, SBOM/license/secret gates, immutable tags, signatures, and provenance own this boundary |

## Security Invariants

- Public library construction performs no implicit network, filesystem,
  environment, process, or global-registry access unless the API explicitly
  represents that effect.
- Input-controlled work has finite byte, item, depth, concurrency, retry, and
  time budgets at the owning boundary.
- Cancellation and deadlines reach blocking operations. Cleanup does not
  abandon owned goroutines, timers, files, response bodies, transactions,
  locks, or connections.
- Authentication comparisons use appropriate constant-time primitives;
  authorization and tenant checks fail closed.
- Secret values and payloads are absent from default errors, logs, traces,
  metrics, examples, fixtures, and CI evidence.
- Cryptography uses maintained standard or `x/crypto` primitives. Algorithms,
  key identity, rotation overlap, replay policy, and canonical bytes remain
  explicit.
- Durable effects define transaction ownership, duplicate handling, fencing,
  acknowledgement ambiguity, recovery, and reconciliation. External
  exactly-once behavior is never implied.
- Remote references, redirects, proxies, DNS, archive paths, and filesystem
  paths are caller controlled and constrained before I/O.
- Verification is attributable to the exact affected content. Missing tools,
  malformed reports, skipped boundaries, and stale evidence fail closed.

## Principal Threats And Controls

The [security matrix](security-matrix.md) maps each threat class to its owner,
required controls, and evidence. Package-specific threat models refine this
repository model where a module owns a security-sensitive boundary. A local
package model may strengthen but must not contradict these invariants.

The repository's strict gates provide prevention and regression evidence, not
an operational certification. Cross-package deployment, failover, rotation,
restore, and incident drills remain governed by
[`GOAL_OPERATIONAL_ASSURANCE.md`](../../.ai/GOAL_OPERATIONAL_ASSURANCE.md).

## Review Triggers

Re-review this model for a new module or adapter, new public protocol or
credential, new durable backend, changed parser or resolver behavior, new
release automation, dependency or action compromise, security incident,
accepted residual risk, or a change to supported Go, OS, architecture, or
deployment targets.

Open and accepted risks are recorded only in the
[residual-risk register](residual-risks.md). Silence is not acceptance.
