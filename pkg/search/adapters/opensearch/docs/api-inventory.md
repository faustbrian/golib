# OpenSearch API and compatibility inventory

Implementation baseline: OpenSearch `2.19.6` and `3.8.0`, with
`github.com/opensearch-project/opensearch-go/v4` `v4.7.3`. These exact server
images form the release conformance matrix. A different patch, plugin set, or
managed-service feature profile is outside the proven matrix until added and
verified.

| Adapter operation | OpenSearch API | Compatibility boundary |
| --- | --- | --- |
| `Info` | `GET /` | requires node, cluster, UUID, and version fields; each returned identity is limited to 1,024 bytes and rejects control characters |
| `Discover` | `GET /_nodes/http` | explicit invocation; only allowlisted data-node publish addresses replace seeds |
| `Write` | `PUT/DELETE /{alias}/_doc/{id}` | `version_type=external`; index/upsert use `require_alias=true`; delete omits the unsupported parameter and relies on resolver-authorized write-alias selection; no automatic retry |
| `Bulk` | `POST /_bulk` | bounded NDJSON; external version metadata; every response action, ID, status, and position is checked |
| `Search` | `POST /{index}/_search` or `POST /_search` | complete logical query, disclosure scope, and bounded pagination cost intent are authorized before resolution without exposing cursor bytes; typed DSL plus explicitly authorized, adapter-bound raw extension objects; PIT requests use the global search endpoint |
| PIT create/delete | `POST /{index}/_search/point_in_time`, `DELETE /_search/point_in_time` | signed cursor owns PIT ID and cleanup; expiry is classified |
| health/capacity | cluster health/stats and bounded node thread-pool/breaker stats | operator-wide, not tenant-scoped; no node IDs, index names, tenant labels, or query data are retained, but aggregate activity still requires a separate authorization boundary |
| lifecycle | index create/delete, reindex tasks, count, mappings/settings, aliases | separate tenant authorizer; reindex preserves external versions and returns an encrypted bound task cursor; `CutoverAlias` holds an application write fence across complete semantic verification and atomic alias mutation |
| templates | composable index-template put/delete | separate tenant authorizer; bounded patterns and canonical settings/mappings |

The adapter uses the official client's `Client.Stream` transport contract and
official `signer.Signer` interface. AWS signing uses the maintained
`signer/awsv2` implementation. The adapter deliberately does not use the
official default transport's environment discovery, retry, or node retry
layers: its single-attempt transport owns endpoint trust, proxy policy,
credential refresh, response bounds, admission, circuit state, and connection
shutdown so client retries cannot multiply package retries.

Typed translation covers boolean, term, multi-match full text, prefix, range,
exists, geo-distance, sort/missing placement, source projection, highlights,
terms/range aggregations, completion prefix suggestions, offset pages, and
PIT/search-after pages. Locale analyzers require an explicit allowlist. A raw
extension must be explicitly bound to `opensearch`, remains size-bounded, and
must be authorized by the application; there is no implicit query-string or
unrestricted JSON escape hatch.
`SearchAuthorization` includes the typed/raw query tree, sort, projection,
highlights, aggregations, suggestions, whether the complete source would be
returned, and cursor/offset size, offset, keep-alive, and continuation intent.
It never exposes opaque cursor bytes. Policy denial is collapsed to
`ErrSearchDenied` before index resolution or transport.
`Capabilities.RawExtensions` is false when no search authorizer is configured.
Direct analyzer names must match the adapter's bounded analyzer-name grammar,
and bulk admission checks the final encoded NDJSON size before transport.

## Public policy and ownership seams

| Surface | Ownership contract |
| --- | --- |
| `Config` / `Client` | explicit endpoints, TLS/proxy/credential mode, transport ownership, request/response bounds, resilience, telemetry, and optional search/lifecycle facilities; mutable TLS, proxy URL, discovery, search, and lifecycle configuration is copied during construction |
| `BasicCredentialsProvider` / official `signer.Signer` | resolve fresh credentials or sign every request; provider errors are redacted |
| `IndexResolver` / `IndexTarget` | map tenant/logical index plus `IndexRead` or `IndexWrite` to a safe least-privilege request alias (`Name`), exact tenant-owned backing generation accepted in response metadata (`PhysicalName`), and definition fingerprint; refresh the physical generation inside the cutover write fence |
| `SearchAuthorizer` | approve the complete logical query, field disclosure, and pagination cost intent before resolution; raw extensions require this seam |
| `WriteGuard` / `WriteAuthorization` | approve one complete cloned write or bounded single-tenant bulk unit against application-owned durable current state or tombstones before target resolution; omission makes the client explicitly read-only |
| `LifecycleAuthorizer` | approve tenant and every physical lifecycle resource before administration |
| `LifecycleVerifier` | compare the complete bounded stable snapshot and independently attest live target mappings/settings |
| `LifecycleCutoverGuard` | quiesce application writes synchronously across fresh verification and atomic alias mutation; guard failures are redacted |
| `LifecycleMutationGuard` / `LifecycleMutationRequest` | hold one application-owned durable cross-instance exclusion across index creation, alias mutation, cutover, and cleanup for overlapping tenant resources; omission makes those mutations fail explicitly |
| `LifecycleCleanupGuard` | prove complete deletion eligibility, including aliases, exact generation identity, retained readers/PITs, retention, and backup state, while the shared lifecycle mutation exclusion remains held |
| `ReindexCursorCodec` | AES-256-GCM confidentiality/integrity, tenant/source/target binding, bounded token/key retention, and a renewed bounded lease after each successful incomplete poll |
| `MaximumOpenPointInTimes` / `PointInTimeSnapshot` | configure and inspect the bounded PIT leases owned by one client process; the snapshot excludes cursors created by other instances and is operator-only aggregate state |
| `TelemetryObserver` | receive bounded request classifications plus `unknown_write_outcomes`, `partial_searches`, `pit_cleanup_failures`, and `cutover_failures` semantic signals without query, source, tenant, index, credential, or backend-body data |

`Info`, `Discover`, `Health`, `Capacity`, `ResilienceSnapshot`, and telemetry
are operational surfaces over the shared client or cluster. They require an
application-owned operator authorization boundary and must not be exposed by a
tenant-facing client, handler, cache, or metrics endpoint.

PIT continuation ownership is deliberately single-consumer for a given client:
concurrent use of the same cursor fails with `ErrPointInTimeInUse`. A cursor
continued by another application instance is adopted into that instance's
local budget after signature and binding validation, but process-local trackers
do not coordinate across instances. Applications that permit cross-instance
cursor routing must therefore enforce an aggregate distributed admission limit
outside this adapter. The in-memory fake declares PIT and cursor support absent
and does not emulate this per-client admission surface.

`Operation` values are `OperationInfo`, `OperationSearch`,
`OperationCreatePIT`, `OperationDeletePIT`, `OperationBulk`, `OperationWrite`,
`OperationLifecycle`, `OperationCreateIndex`, `OperationReindex`,
`OperationVerifyIndex`, `OperationResolveAlias`, `OperationSwapAlias`,
`OperationDeleteIndex`, `OperationHealth`, `OperationCapacity`, and
`OperationTemplate`. `FailureCategory` values are `FailureCancelled`,
`FailureTransport`, `FailureOverloaded`, `FailureClusterBlocked`,
`FailureRejected`, `FailureMalformed`, `FailureVersionConflict`,
`FailureMappingRejected`, `FailurePITExpired`, `FailureBackpressure`, and
`FailureCircuitOpen`. Every `Failure` states operation, category, bounded HTTP
status/code, retryability, and whether a mutation outcome is known.

## Exported error inventory

- configuration/transport: `ErrInvalidConfig`, `ErrUnsafeEndpoint`,
  `ErrUnsafeProxy`, `ErrInvalidTLS`, `ErrCredentials`, `ErrContextRequired`,
  `ErrClosed`, `ErrTransport`, `ErrMalformedResponse`, `ErrResponseTooLarge`,
  and `ErrUnexpectedStatus`;
- discovery/search/reconciliation: `ErrDiscoveryDisabled`,
  `ErrDiscoveryRejected`, `ErrSearchDisabled`, `ErrUnsafeIndexTarget`,
  `ErrSearchDenied`, `ErrWriteDisabled`, `ErrWriteDenied`,
  `ErrPartialResults`, `ErrPITExpired`, `ErrPointInTimeCapacity`, and
  `ErrPointInTimeInUse`;
- backend/admission classification: `ErrOverloaded`, `ErrClusterBlocked`,
  `ErrRejected`, `ErrVersionConflict`, `ErrMappingRejected`,
  `ErrBackpressure`, and `ErrCircuitOpen`;
- lifecycle: `ErrLifecycleRejected`, `ErrLifecycleDisabled`,
  `ErrLifecycleDenied`, `ErrLifecycleVerifierRequired`,
  `ErrLifecycleCutoverGuardRequired`, `ErrLifecycleCutoverGuardRejected`,
  `ErrLifecycleCutoverUnverified`, `ErrLifecycleMutationGuardRequired`,
  `ErrLifecycleMutationGuardRejected`, `ErrLifecycleCleanupGuardRequired`,
  `ErrLifecycleCleanupGuardRejected`, `ErrInvalidReindexCursorCodec`,
  `ErrInvalidReindexCursor`, `ErrReindexCursorBinding`,
  `ErrReindexCursorExpired`, and `ErrLifecycleCursorCodecRequired`.

All application-policy/provider failures collapse to the stable public class;
raw policy, credential, guard, transport, and backend reason text is not part of
the contract.

Primary references:

- <https://docs.opensearch.org/latest/clients/go/>
- <https://github.com/opensearch-project/opensearch-go/tree/v4.7.3>
- <https://github.com/opensearch-project/OpenSearch/releases/tag/2.19.6>
- <https://github.com/opensearch-project/OpenSearch/releases/tag/3.8.0>
- <https://docs.opensearch.org/latest/api-reference/>
