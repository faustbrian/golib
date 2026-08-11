# API and ownership

## Complete shared-semantics inventory

| Boundary | Declared shared surface |
| --- | --- |
| query nodes | `MatchAllQuery`, `BoolQuery` (`Must`, `Should`, `Filter`, `MustNot`, `MinimumShouldMatch`), `TermQuery`, `FullTextQuery`, `PrefixQuery`, `RangeQuery`, `ExistsQuery`, `GeoDistanceQuery`, `RawExtensionQuery` |
| result shaping | `Sort` with direction and missing placement, backend-neutral virtual `DocumentIDSortField`, `Projection`, `Highlight`, `TermsAggregation`, `RangeAggregation`/`RangeBucket`, `PrefixSuggestion` |
| document values | missing field by map absence; `KindNull`, `KindString`, `KindNumber`, `KindBool`, `KindTime`, `KindGeo`, `KindArray`, `KindObject` |
| pagination | `CursorPage`, `OffsetPage`, `CursorBinding`, `CursorState`, `CursorCodec`; cursor state binds PIT, sort values, page/item/byte totals, and absolute expiry; `CursorCodec.Deadline` and `Remaining` make the codec clock authoritative |
| write actions and outcomes | `WriteOperation` (`Action`, `Tenant`, `Index`, `ID`, `Version`, `Source`); `BulkRequest` (`Operations`, `Refresh`); `ItemOutcome` (`Position`, `ID`, `Action`, `State`, `Version`, `Code`, `Diagnostic`, `Retryable`); `BulkResult`; `ActionIndex`, `ActionUpdate`, `ActionUpsert`, `ActionDelete`; `OutcomeApplied`, `OutcomeNotFound`, `OutcomeVersionConflict`, `OutcomeRejected`, `OutcomeFailed`, `OutcomeUnknown`; `RefreshNone`, `RefreshWaitFor`, `RefreshImmediate` |
| totals and diagnostics | `TotalExact`, `TotalLowerBound`, `Hit`, `Failure`, `ShardDiagnostics`, `Diagnostics`, `Result` |
| capabilities | `Boolean`, `Term`, `FullText`, `Prefix`, `Range`, `Exists`, `Geo`, `Cursor`, `PointInTime`, `Offset`, `Projection`, `Highlight`, `Aggregation`, `Suggestion`, `ExternalVersion`, `UpdateExisting`, `BulkPartialOutcomes`, `Lifecycle`, `Templates`, `RawExtensions` |
| schema and compatibility | `IndexDefinition`, `Compatibility`, `Compatible`, `ReindexRequired` |
| migration operations | `LifecycleCreate`, `LifecycleReindex`, `LifecycleVerify`, `LifecycleCutover`, `LifecycleRollback`, `LifecycleCleanup` |
| migration phases | `MigrationPending`, `MigrationCreating`, `MigrationCreated`, `MigrationDispatching`, `MigrationReindexing`, `MigrationReindexed`, `MigrationVerified`, `MigrationComplete`, `MigrationRolledBack`, `MigrationCleaning`, `MigrationCleaned` |
| projection and reconciliation | `ProjectionUpsert`, `ProjectionDelete`, `ProjectionEvent`, `ProjectionConsumer`; `ReconciliationRequest`, `ReconciliationRecord`, `ReconciliationPage`, `ReconciliationReport`, `ReconciliationDeletion`, `ReconciliationDeletionGuard`, `Reconciler`; `NewReconciler`, `NewReconcilerWithDeletionGuard`; `DriftMissing`, `DriftStale`, `DriftDivergent`, `DriftOrphaned` |

`RawExtensionQuery` is the only shared backend-extension seam. Its adapter name
and bounded JSON object remain explicit; capability and application policy must
both approve it. The core declares no raw sort, aggregation, suggestion,
mapping, refresh, version, or pagination escape hatch.
`DocumentIDSortField` is the shared virtual stable-ID tiebreaker. Adapters map
it to their backend metadata field; callers do not use `_id` or another
backend-native spelling in shared requests.

### Limits

Every `Limits` field is mandatory and positive: `MaxTenantBytes`,
`MaxIndexBytes`, `MaxIDBytes`, `MaxSourceBytes`, `MaxQueryBytes`,
`MaxBulkItems`, `MaxBulkBytes`, `MaxPageItems`, `MaxPages`, `MaxResultBytes`,
`MaxCursorDuration`, `MaxQueryDepth`, `MaxQueryClauses`, `MaxJSONDepth`, and
`MaxJSONNodes`. Multiplication and accumulation boundaries are checked before
allocation or conversion. Adapter-specific limits may only be stricter.

### Adapter and orchestration contracts

| Contract | Responsibility |
| --- | --- |
| `Searcher` | capability discovery and bounded search |
| `Indexer` | one external-version write and attributed bulk writes |
| `LifecycleBackend` | create, resumable reindex, semantic verification, alias resolution, verified/write-fenced cutover, and deletion |
| `MigrationStore` | durable resumable migration state |
| `MigrationCoordinator` | one durable exclusive migration-ID boundary across every application instance; the store supplied to `NewMigrator` must implement it |
| `LifecycleAuthorizer` / `LifecycleObserver` | authorize and observe each lifecycle transition without owning backend semantics |
| `ProjectionOutbox` / `ProjectionConsumer` | durable event deduplication and write application |
| `ReconciliationReader` / `Reconciler` | bounded stable traversal, comparison, and repair; orphan repair fails before dispatch unless a source-owned deletion guard reserves a durable monotonic tombstone |
| `ReconciliationDeletionGuard` | atomically confirm authoritative deletion, reserve a tenant/index/ID-bound tombstone version above the observed index version, and fence every later source write above it |

### Exported error inventory

- document/value/JSON: `ErrTenantRequired`, `ErrTenantTooLarge`,
  `ErrIndexRequired`, `ErrIndexTooLarge`, `ErrIDRequired`, `ErrIDTooLarge`,
  `ErrVersionRequired`, `ErrSourceRequired`, `ErrSourceTooLarge`,
  `ErrInvalidSource`, `ErrJSONDepthLimit`, `ErrJSONNodeLimit`, and
  `ErrDuplicateJSONKey`;
- query/pagination/result: `ErrInvalidQuery`, `ErrUnsupported`,
  `ErrUnstableSort`, `ErrPageLimit`, `ErrInvalidCursorCodec`,
  `ErrInvalidCursor`, `ErrCursorBinding`, `ErrIndexChanged`,
  `ErrCursorExpired`, and `ErrInvalidResult`;
- write/projection: `ErrBulkLimit`, `ErrTenantMismatch`,
  `ErrInvalidOperation`, `ErrInvalidBulkResult`, and
  `ErrInvalidProjectionEvent`;
- schema/limits: `ErrInvalidLimits`, `ErrInvalidIndexDefinition`, and
  `ErrSchemaLimit`;
- lifecycle: `ErrInvalidMigrator`, `ErrInvalidMigrationPlan`,
  `ErrMigrationNotFound`, `ErrMigrationIncomplete`,
  `ErrMigrationPlanChanged`, `ErrMigrationVerification`,
  `ErrMigrationRecovery`, `ErrMigrationCoordination`, `ErrAliasChanged`, and
  `ErrInvalidMigrationPhase`;
- reconciliation: `ErrInvalidReconciler`, `ErrInvalidReconciliation`,
  `ErrReconciliationLimit`, `ErrMalformedReconciliation`,
  `ErrReconciliationDeletionGuard`, and `ErrRepairPartial`.

Errors identify stable semantic classes. Implementations may wrap them, but
must not replace an unsupported feature, ambiguous write, expired cursor,
partial repair, or failed verification with a success result.

## Documents and values

`Document` requires a tenant, logical index, stable non-empty ID, positive
external version, and bounded JSON-object source. `Value` distinguishes null,
boolean, exact decimal text, Unicode string,
UTC timestamp, geo point, object, and array. Missing means a field key is
absent; null is `search.NullValue()`; an empty string or array is present and
empty. Constructors validate applicable byte limits, JSON shape, coordinates,
and decimal grammar before any backend call. Documents and returned values own
their copied bytes; request slices and raw-extension payloads remain caller
owned and must not be mutated while validation or execution is in progress.
`Limits.MaxJSONDepth` bounds container nesting with the root object at depth
one. `Limits.MaxJSONNodes` bounds the total object fields and array elements;
for an index definition, settings and mappings share one combined node budget.
Document sources, settings, mappings, query text, identifiers, and field names
reject invalid UTF-8 instead of accepting replacement-character normalization.
Document sources, settings, and mappings reject duplicate object keys after
JSON escape decoding, so ambiguous input such as `"name"` and `"n\u0061me"`
cannot be silently overwritten. Both bounds must be positive.
`BulkRequest.Validate` applies the same source checks to direct
`WriteOperation` literals before adapter I/O.

## Queries

Queries compose through `BoolQuery` and include match-all, exact term, full
text, prefix, range, existence, geo distance, and trusted raw-extension nodes.
Nested document values are represented by object and array values; mappings
decide whether an engine interprets an object array as nested. Requests also
define filters, stable sorting, source projection,
aggregations, highlights, suggestions, locale, and pagination. `Validate`
rejects empty nodes, invalid field names, unstable cursor sorts, incompatible
bounds, unsupported raw extensions, malformed extension objects, and limit
violations. A raw extension binds its JSON object to one named adapter and is
bounded by `Limits.MaxQueryBytes`, `MaxJSONDepth`, and `MaxJSONNodes`; it is not
a general untrusted DSL endpoint.
The complete normalized request is bounded by `Limits.MaxQueryBytes`, including
full-text input, projections, highlight tags, aggregations, and suggestions.
Highlight fragment bytes and fragment counts, terms aggregation sizes, range
bucket counts, and suggestion sizes are also independently bounded by the
source, page-item, and query-clause limits before adapter execution.
Validation preflights the aggregate caller-owned input bytes and collection
shapes before constructing the normalized request, so oversized input is not
duplicated merely to discover that it exceeds the encoded-query limit.
`RequestFingerprint` requires the same explicit limits and performs its own
bounded preflight for safe use outside an adapter.
Projection fields use ordinary field names. Only full-text fields may carry a
positive finite `^boost` suffix; other query and result-shaping fields reject
boost syntax.

Core types describe intent, not portable relevance. An adapter publishes its
capabilities and may provide additional trusted APIs. Applications own document
schemas, analyzer selection, ranking, and authorization.
`UpdateDocument` means update an existing document and is available only when
an adapter declares `Capabilities.UpdateExisting`; unsupported adapters reject
it before network execution instead of translating it to index or upsert.

## Results and failures

`Result` owns hits, scores, sort values, source fields, highlights,
aggregations, suggestions, diagnostics, partial-failure details, and the next
cursor. Result construction requires an explicit exact or lower-bound total
relation, positive hit versions, valid named aggregation and suggestion JSON,
and internally consistent bounded diagnostic identifiers and text. A timeout or
failed shard cannot be reported as a complete result. `BulkResult` preserves
item order and per-item status, including an unknown outcome after ambiguous
transport failure. `BulkResult.ValidateRequest` verifies that each outcome's
position, document ID, and action match the originating operation and that an
applied outcome reports the requested external version. Callers must reconcile
an unknown write before retrying unless the same stable ID and external version
make replay safe.

`ItemOutcome.Code` and `ItemOutcome.Diagnostic` are adapter-facing stable,
bounded, redacted classifications. Adapters must never copy backend reason
text, documents, queries, endpoints, tenant labels, PITs, credentials, or
provider errors into either field. Production adapters should prefer a short
code and leave `Diagnostic` empty when a stable safe classification is not
available. `NewBulkResult` rejects invalid UTF-8, codes larger than
`MaxFieldNameBytes`, and diagnostics larger than `DefaultLimits().MaxSourceBytes`.

All I/O interfaces accept `context.Context`. Applications compose retry,
rate-limit, circuit-breaker, bulkhead, concurrency-limit, telemetry, tenancy,
correlation, and authentication around an adapter; the core does not hide or
duplicate those policies.

## Lifecycle and migration

`LifecycleBackend` keeps physical generation creation, resumable reindexing,
verification, verified alias cutover, and cleanup explicit. `VerifyIndex`
receives the expected target `IndexDefinition` fingerprint; an implementation
must bind its semantic comparison to that live definition rather than trusting
counts or a stored caller assertion. `MigrationPlan` also retains the source
definition fingerprint needed to verify rollback. `CutoverAlias` must hold the
backend/application write fence across a fresh complete verification and the
atomic alias mutation. `Migrator` persists each completed phase, refuses changed
plans, and uses verified cutover for both forward migration and rollback. The
supplied store must also implement `MigrationCoordinator`; `Run`, `Rollback`,
and `Cleanup` hold that durable migration-ID boundary across state access and
backend I/O so dispatch, cutover, rollback, and deletion cannot race across
instances. Reindex cursors are opaque backend state owned by the adapter.
Persist every cursor returned by a poll, because an adapter may renew its
authenticated continuation lease while retaining the same backend task.
