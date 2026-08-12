# Repository Security Matrix

This matrix is the repository-level review surface. A row is release-ready only
when its affected modules have current mandatory gate evidence and its
remaining operational boundary is either proved or explicitly recorded in the
[residual-risk register](residual-risks.md).

| Threat class | Primary owners | Mandatory controls and evidence | Current repository boundary |
| --- | --- | --- | --- |
| Credential theft and authentication bypass | `authentication`, JWT, OIDC, password, keyphrase, secret-store adapters | Constant-time comparison, strict token/claim/key policy, bounded JWKS/discovery, hostile-input tests, fuzzing, rotation and redaction tests | Package evidence exists; composed live key and credential rotation remains operational assurance |
| Authorization or tenant escape | `authorization`, `tenancy`, capability, HTTP/RPC adapters | Fail-closed policy, explicit unscoped authority, cross-tenant tests, replay and revocation, cache isolation, race and mutation evidence | Application policy correctness and deployed datastore isolation remain consumer responsibilities |
| Replay, duplicate effects, and stale ownership | idempotency, lease, queue, outbox, scheduler, workflow, webhook | Stable identities, fencing, finite leases, at-least-once semantics, poison/dead-letter handling, reconciliation, crash and ambiguity tests | Composed process-death and backend-failover drills remain operational assurance |
| Injection and parser differential | wire, tabular, JSON Schema, JSON:API, JSON-RPC, OpenAPI, OpenRPC, XSD, WSDL, ECMA regexp | Explicit limits, no implicit resolution, official corpora, independent interoperability, fuzzing, hostile encodings, exact mutation evidence | Newly supported formats or dialects require a new decision and corpus entry |
| SSRF, redirects, proxy, and DNS abuse | HTTP client, filesystem/object storage, schema/document resolvers | Caller-owned transports and resolvers, redirect and target policy, bounded bodies, TLS and proxy tests, no ambient network access | Deployment DNS, egress, trust-store, and proxy controls remain operational assurance |
| Path traversal, symlink, archive, and temporary-data exposure | filesystem, tabular, external-sort | Capability roots, path validation, bounded archives, encrypted spill, cleanup and hostile-filesystem tests | Container filesystem and ephemeral-storage constraints remain operational assurance |
| Secret or personal-data disclosure | log, telemetry, audit, config, secret envelope, all adapters | Typed/redacted values, payload-free defaults, cardinality limits, scanner gates, disclosure-focused tests | Service-specific data classification, retention, erasure, and legal hold remain consumer and operational policy |
| Resource exhaustion and amplification | parsers, router/middleware, retry/hedge/rate-limit/bulkhead/concurrency packages, queue/workflow | Finite budgets, backpressure, cancellation, leak/race/fuzz tests, adversarial benchmarks, retry and hedge amplification policy | Controlled CPU/memory/descriptor load and soak evidence remains operational assurance |
| Cryptographic misuse and signature confusion | HTTP signature, capability, password, keyphrase, secret envelope, Merkle/trie packages | Maintained primitives, explicit algorithm profiles, canonical bytes, key identity, independent vectors, differential tests, no custom primitives | Production HSM/KMS lifecycle and compromise drills require deployment evidence |
| SQL, transaction, and migration corruption | postgres, migrations, outbox, audit, workflow, sequencer, settings | Parameterized operations, caller-owned transactions, checksums, locks/fencing, rollback and unknown-outcome recovery, supported-version integration | Backup/restore, failover, interrupted migration, and storage exhaustion remain operational assurance |
| Broker and schema trust failure | queue, Kafka, CloudEvents, schema registry, event sourcing | Bounded envelopes, explicit acknowledgement, schema identity, compatibility, poison handling, broker/provider conformance and interoperability | Active specialist evidence and composed broker outage/rebalance drills must be consumed before release |
| Search-derived-state corruption | search and OpenSearch adapter | Source-of-truth ownership, bounded cursors, partial bulk failure handling, migration, reconciliation, rebuild and version compatibility | Deployment snapshot/restore and full rebuild drills remain operational assurance |
| Supply-chain, CI, and release compromise | root tooling and workflow | Full action pins, centralized tool versions, isolated caches and modules, vulnerability/secret/license/SBOM gates, immutable tags, signed provenance, clean consumers | Signing, attestations, public-proxy verification, and final GitHub matrix remain open release work |
| Evidence spoofing or stale proof | root gate runner and catalogs | Complete input fingerprints, atomic logs/checkpoints, checksums, attributable module results, fail-closed missing evidence | Specialist scopes and affected reverse dependants remain independently attributable; history changes alone do not invalidate proof |

## Release Use

Scanner success alone does not close a row. Release review consumes current
package evidence, the threat model, this matrix, the risk register, and the
operational-assurance verdict. A missing row owner, unclassified boundary, or
unaccepted open risk fails release rather than becoming an implicit exception.
