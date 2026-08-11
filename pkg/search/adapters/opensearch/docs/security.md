# Security and authentication

Basic credentials and AWS request signing are mutually exclusive. Credential
providers are consulted for every request so rotation does not require client
replacement. The adapter rejects credentials over plaintext HTTP, endpoint
userinfo, implicit environment proxies, insecure TLS, and untrusted discovery
addresses.

The index resolver must authorize tenant, logical index, and access mode before
returning a request alias and its exact tenant-owned backing generation.
`IndexTarget.Name` is used on the request path; response `_index` attribution
must match `IndexTarget.PhysicalName`. Update that physical generation inside
the application write fence that protects alias cutover, before writers resume.
Lifecycle methods additionally require the lifecycle authorizer. Give runtime
search credentials no destructive lifecycle privileges when deployments can
use separate clients or roles.

## Threat model

| Threat | Enforced boundary | Residual owner |
| --- | --- | --- |
| query/path injection | typed query encoding, strict field/analyzer/index grammar, adapter-bound bounded raw JSON, no query-string DSL | application must not construct raw extensions from unrestricted caller JSON |
| expensive queries/traversal | query byte/depth/clause limits, page/item/byte/deadline totals, pagination cost authorization, bounded aggregations/highlights/suggestions | application policy may impose stricter per-principal budgets |
| tenant leakage | tenant-aware read/write resolver, search and lifecycle authorizers, cursor tenant binding, single-tenant bulk validation; telemetry and cluster reports omit tenant labels | resolver/authorizer implementations and credentials must remain tenant-correct; `Info`, `Discover`, `Health`, `Capacity`, `ResilienceSnapshot`, and telemetry are operator-wide and must never be exposed through a tenant-facing client or endpoint |
| cursor/task tampering | HMAC-bound search cursors and AES-256-GCM reindex cursors, fixed expiry, query/index/source/target binding, strict token bounds | applications own key rotation and cursor persistence |
| response bombs/malformed success | transport and semantic response bounds, valid UTF-8 before JSON decoding, strict JSON shapes, shard/total/version/attribution checks, body closure on every path | deployment proxies must not bypass configured client transport |
| mapping explosion | bounded canonical `IndexDefinition`, explicit templates, live-definition fingerprint attestation, separately authorized lifecycle | schema owners must constrain dynamic mappings and field cardinality |
| field disclosure | complete query/projection/highlight/aggregation/suggestion/full-source authorization before resolution | application policy defines which principal may see each field |
| poisoned documents/stale replay | bounded JSON object/depth/node/duplicate-key validation, external versions, durable pre-resolution write guard, mapping failures classified without reason leakage | source-of-truth validation, authoritative current-document/tombstone storage, and schema policy remain application-owned |
| backend credential leakage | mutually exclusive secret providers/signing, TLS-only credentials, no URL userinfo, redacted provider/policy/guard/backend errors | operators own secret storage, rotation, and least-privilege roles |

Every valid search also requires `SearchAuthorizer` approval before the resolver
or network is reached. The policy sees the full query tree, sort, projection,
highlight, aggregation, suggestion, full-source scope, and bounded pagination
cost intent. Opaque cursor bytes are not disclosed to policy. An empty
projection means complete `_source` disclosure and must be approved explicitly.
Raw extensions are not advertised or executable without this authorizer.
Policy errors are hidden behind `ErrSearchDenied`.

OpenSearch retains a deleted document's external version only for the bounded
`index.gc_deletes` interval. After that tombstone is collected, the backend can
accept an older externally versioned replay and resurrect the projection.
Production writes therefore require `WriteGuard`: it receives an immutable,
bounded, single-tenant authorization snapshot before resolver or network access
and must reject any operation that does not match authoritative durable current
state or a durable tombstone. Guard errors are redacted; cancellation and
deadline classification remain observable. Omitting the guard disables write
capabilities and makes `Write` and `Bulk` fail with `ErrWriteDisabled`.

Reindex task cursors are AES-256-GCM encrypted, expire at a fixed deadline, and
are bound to tenant plus physical source and target. Treat the encryption key as a
credential, persist cursors unchanged, and never log either value. A lifecycle
verifier must compare all records from a stable point-in-time view within its
hard bound and attest the current live target definition; counts or
caller-stored fingerprints alone are not sufficient for alias cutover.

Backend bodies, queries, sources, signed cursors, credentials, and authorization
errors may contain secrets. Adapter failures expose bounded classifications and
metadata instead of echoing response bodies or provider errors.

Label-free does not mean tenant-scoped. `Info`, `Discover`, `Health`,
`Capacity`, `ResilienceSnapshot`, and configured telemetry describe the shared
client or cluster. Deploy them only behind an operator authorization boundary,
prefer a separate least-privilege operational client, and never cache or expose
their results in a tenant-keyed application surface.

The real security matrix uses separate credentials on both supported server
versions. Its runtime role can read and write only the tenant-A index/alias
pattern; tenant-B reads and writes, cluster health, and Security administration
must return HTTP 403. OpenSearch 3.8.0 additionally requires the top-level
`indices:data/write/bulk` cluster action before it resolves the destination;
the role retains tenant enforcement through its tenant-A-only index actions,
and the matrix proves that the transport admission does not permit tenant-B
writes. A distinct operator role has `cluster_monitor` plus management of only
the disposable recovery-fixture prefix, and proves `Info`, `Health`, and
`Capacity` without tenant data privileges.

The advisory review was refreshed on 2026-08-10. The OpenSearch repository's
published GitHub advisories list CVE-2023-31419 and CVE-2022-41917, both fixed
well before the supported 2.19.6 and 3.8.0 server versions. The 3.8.0 release
also upgrades dependencies for CVE-2026-8149, CVE-2026-54515, and
CVE-2026-2332, and hardens deserialization and filesystem path boundaries.
Operators must still review OpenSearch, plugin, JVM, image, and managed-service
advisories for their exact deployment before every release.

Primary references:

- <https://github.com/opensearch-project/OpenSearch/security/advisories>
- <https://github.com/opensearch-project/OpenSearch/releases/tag/3.8.0>
