# OpenSearch Specification Decisions

This register records observable choices where the OpenSearch REST API,
server implementations, official Go client, and adapter policy permit
different outcomes. The exact source archives are pinned in
[`specification/manifest.tsv`](../specification/manifest.tsv).

Each resolved entry names executable evidence. A change requires security,
resource, compatibility, wire-format, API, conformance, and changelog review.
Superseded decisions remain here and link to their replacements.

## OPENSEARCH-DEC-001: Supported server and client versions

**Authoritative reference:** [OpenSearch 3.8.0 source](https://github.com/opensearch-project/OpenSearch/tree/e5a3c5691be87af6c12dbe3e158c59c04ee72973).

- **Status, owner, and classification:** `resolved`; OpenSearch adapter
  maintainers; normative-source and interoperability policy.
- **Source and issue:** OpenSearch exposes a versioned REST API but plugins,
  patch releases, distributions, and the official Go client can differ. The
  project does not define one compatibility claim for every combination.
- **Interpretations and peer behavior:** Claim broad 2.x/3.x compatibility,
  follow only the client version, test one current server, or publish an exact
  server matrix. Peer adapters commonly accept wider ranges than they test.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. The supported matrix is exactly OpenSearch
  2.19.6, OpenSearch 3.8.0, and `opensearch-go/v4` 4.7.3. Other patches,
  plugins, analyzers, mappings, and managed-service profiles require their own
  evidence and are not implied compatible.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSupportedOpenSearchVersionsIncludesCurrentRelease` and
  `TestRealOpenSearchConformance` cover `SupportedOpenSearchVersions` and the
  real-server matrix. Reconsider on every server, client, plugin, or source pin
  upgrade; upstream records are the pinned release commits and client tag.

## OPENSEARCH-DEC-002: Endpoint, TLS, proxy, and credential trust

**Authoritative reference:** [OpenSearch Go client 4.7.3 source](https://github.com/opensearch-project/opensearch-go/tree/172ea95af6dfe30b612cc42ac736e7dd613154d9).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  transport and credential policy.
- **Source and issue:** The client permits caller transports and several
  endpoint configurations but does not define application trust boundaries,
  proxy policy, credential disclosure policy, or TLS minimums.
- **Interpretations and peer behavior:** Use environment defaults, permit URL
  userinfo, allow insecure TLS, or validate an explicit authority set. Generic
  clients often prioritize convenience over least privilege.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Endpoints are explicit and bounded; userinfo,
  unsafe schemes, insecure TLS, and implicit proxies fail. Basic credentials
  and AWS signing are mutually exclusive and refreshed for every request.
  Credentials cannot be sent over plaintext HTTP.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestNewRejectsUnsafeOrAmbiguousTransportConfiguration`,
  `TestExplicitInsecureHTTPCannotCarryCredentials`, and
  `TestOfficialAWSSignerRetrievesRotatedCredentialsForEveryRequest` cover
  `Config` and `New`. Reconsider if the official client exposes an equivalent
  complete trust contract; track upstream transport and signer changes.

## OPENSEARCH-DEC-003: Single-attempt transport and ambiguous receipt

**Authoritative reference:** [OpenSearch index API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/index.json).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  delivery and idempotency policy.
- **Source and issue:** REST specifications define requests and responses but
  cannot reveal whether a failed connection occurred before or after a write.
  Client, adapter, queue, and application retries can multiply side effects.
- **Interpretations and peer behavior:** Retry transport failures implicitly,
  retry only reads, expose retry counts, or make durable callers own retries.
  Clients differ in node retry and status retry defaults.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Every adapter operation performs one bounded
  transport attempt. Ambiguous writes and bulk items are reported as unknown,
  not silently retried. Durable callers reconcile ID and external version
  before retrying.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestClientSelectsConfiguredNodesWithoutImplicitRetries` and
  `TestBulkReturnsPerItemUnknownOutcomesWhenTransportOutcomeIsAmbiguous` cover
  request execution and result classification. Reconsider only with a
  versioned idempotency protocol; monitor upstream retry behavior.

## OPENSEARCH-DEC-004: Node selection and explicit discovery

**Authoritative reference:** [OpenSearch nodes API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/nodes.info.json).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  topology and authority policy.
- **Source and issue:** Node discovery returns publish addresses but does not
  establish that every returned address may receive the caller's credentials.
  DNS, proxy, and topology changes create an authority-confusion risk.
- **Interpretations and peer behavior:** Trust every discovered node, disable
  discovery, validate only seed hosts, or require an explicit allow policy.
  Peer clients commonly enable discovery or node retry independently.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Seed rotation is deterministic and bounded.
  Discovery is explicit, accepts only bounded data-node results matching a DNS
  suffix or IP prefix allow policy, and replaces the pool atomically only when
  the complete topology is trusted.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestPoolRotationAndEndpointOrder`,
  `TestDiscoverRequiresAnExplicitBoundedTrustPolicy`, and
  `TestDiscoverRejectsUntrustedOrMalformedTopologyWithoutChangingSeeds` cover
  discovery and selection. Reconsider if upstream supplies authenticated
  topology identities; track nodes API changes.

## OPENSEARCH-DEC-005: Bounded requests, responses, and diagnostics

**Authoritative reference:** [OpenSearch REST API specification at 3.8.0](https://github.com/opensearch-project/OpenSearch/tree/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  resource and disclosure policy.
- **Source and issue:** OpenSearch response bodies and error objects can be
  arbitrarily large or contain indexed data. REST definitions do not assign
  application memory limits or safe external diagnostics.
- **Interpretations and peer behavior:** Stream without a bound, trust content
  length, return provider errors, or enforce one adapter-wide budget. Clients
  vary in body ownership and truncation behavior.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Every response is read under an exact finite
  limit, closed on all paths, and structurally validated. Request collections,
  encoded bulk bodies, and configuration values are bounded. Public failures
  expose stable classifications and bounded metadata, never backend bodies.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestInfoRejectsMalformedAndOversizedResponsesWithoutLeakingBodies`,
  `TestBulkRejectsEncodedBodyAboveConfiguredByteLimit`, and
  `TestConfigurationCollectionsAndConcurrencyAreAbsolutelyBounded` cover the
  public operations and limits. Reconsider only with a separate streaming API;
  track upstream response-schema changes.

## OPENSEARCH-DEC-006: Typed query surface and raw extensions

**Authoritative reference:** [OpenSearch search API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/search.json).

- **Status, owner, and classification:** `resolved`; maintainers; application
  interoperability and extension policy.
- **Source and issue:** OpenSearch Query DSL is larger and less stable than the
  shared search contract. Unrestricted JSON would bypass validation, tenant
  policy, resource budgets, and backend portability.
- **Interpretations and peer behavior:** Expose raw DSL only, implement every
  query type, reject extensions, or provide a bounded explicit escape hatch.
  Search libraries vary between stringly typed and generated models.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. The adapter translates the shared typed query
  family. A raw extension must be a bounded JSON object explicitly bound to
  `opensearch` and constructed by a trusted caller. There is no query-string or
  automatic fallback.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSearchTranslatesTypedCapabilitiesWithoutCrossIndexLeakage`,
  `TestSearchEncodingWireContract`, and
  `TestSearchRejectsInvalidAnalyzerBeforeNetworkExecution` cover `Search` and
  capability translation. Reconsider per additive typed feature or separately
  authorized extension profile; track upstream Query DSL changes.

## OPENSEARCH-DEC-007: Tenant and physical-index resolution

**Authoritative reference:** [OpenSearch index APIs at 3.8.0](https://github.com/opensearch-project/OpenSearch/tree/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  multi-tenant application policy.
- **Source and issue:** OpenSearch accepts physical index names but does not
  know application tenants, logical indexes, read/write separation, or mapping
  generation ownership.
- **Interpretations and peer behavior:** Let callers pass physical names,
  prefix tenant IDs, use one shared alias, or inject an authorization-aware
  resolver. Generic clients expose raw paths.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Every search or write resolves tenant, logical
  index, and access mode through `IndexResolver`; returned names and mapping
  fingerprints are bounded and validated before network I/O. Read and write
  aliases remain distinct policy decisions.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSearchTranslatesTypedCapabilitiesWithoutCrossIndexLeakage`,
  `TestAddAliasCreatesAnAuthorizedReadWriteBoundary`, and
  `TestLifecycleRequiresAuthorizationBeforeNetworkAccess` cover resolver and
  lifecycle boundaries. Reconsider only with an equivalent application-facing
  authorization contract; no upstream issue can define tenant policy.

## OPENSEARCH-DEC-008: Query encoding and analyzer ownership

**Authoritative reference:** [OpenSearch search API 2.19.6](https://github.com/opensearch-project/OpenSearch/blob/97d3c13bf22a4a72ac11dc503fe44c97662b9161/rest-api-spec/src/main/resources/rest-api-spec/api/search.json).

- **Status, owner, and classification:** `resolved`; maintainers; REST mapping
  and application-policy decision.
- **Source and issue:** The REST API permits many equivalent JSON query shapes,
  analyzer names, sort forms, and omission defaults. Those choices affect
  ranking, cache identity, and cursor compatibility.
- **Interpretations and peer behavior:** Emit minimal JSON, preserve caller
  objects, rely on server defaults, or use a deterministic adapter mapping.
  Peers differ on zero values and analyzer handling.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Typed values map to deterministic package-owned
  JSON shapes, including explicit zero-valued boolean settings. Locale
  analyzers come only from a bounded allowlist. Applications retain ownership
  of mappings, analyzers, ranking, and relevance fixtures.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSearchEncodingWireContract`,
  `TestEncodeBoolPreservesZeroMinimumShouldMatchSemantics`, and
  `TestSearchRejectsInvalidAnalyzerBeforeNetworkExecution` cover encoding.
  Reconsider on mapping changes or server defaults; track search API changes.

## OPENSEARCH-DEC-009: Offset and PIT cursor pagination

**Authoritative reference:** [OpenSearch point-in-time API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/create_pit.json).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  pagination and resource-ownership policy.
- **Source and issue:** OpenSearch defines `from`, `search_after`, and PIT APIs
  but not application cursor signing, tenant binding, total traversal budgets,
  or who closes a PIT after every failure path.
- **Interpretations and peer behavior:** Return raw PIT IDs, use offset only,
  slide expiry on every page, or issue a signed application cursor. Libraries
  often leave PIT cleanup to callers.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Offset pagination is shallow and bounded.
  Cursor pagination requires stable sort ending in `_id`, binds a signed cursor
  to tenant, query, index fingerprint, PIT, totals, and absolute expiry, and
  transfers PIT ownership only with a valid continuation. Every terminal or
  failed owned path closes the PIT and exposes cleanup failure.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSearchUsesPITSearchAfterAndSignedQueryBoundCursor`,
  `TestCursorSearchRejectsPartialPagesAndClosesOwnedPIT`, and
  `TestSearchPreservesPrimaryAndPITCleanupFailures` cover cursor state and PIT
  lifecycle. Reconsider if OpenSearch adds equivalent opaque bounded cursors;
  track PIT and search-after changes.

## OPENSEARCH-DEC-010: Search response and partial-result policy

**Authoritative reference:** [OpenSearch search API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/search.json).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  response-validation policy.
- **Source and issue:** Search responses may contain shard failures, partial
  results, more hits than requested, changing PIT IDs, malformed sort values,
  or plugin-specific shapes. The REST schema does not choose application
  acceptance rules.
- **Interpretations and peer behavior:** Return every decodable response,
  expose partial results with warnings, silently truncate, or fail closed for
  cursor traversal. Peers vary in strictness.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Responses must fit all declared limits and
  internal invariants. Cursor traversal rejects partial shard results and
  excessive hits, validates sort/PIT state before issuing a continuation, and
  preserves bounded operational diagnostics.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSearchRejectsMoreHitsThanRequested`,
  `TestSearchCursorStateAndResponseBoundaries`, and
  `TestPITProjectionAndSearchResponsePredicatesAreIndependent` cover response
  acceptance. Reconsider only through an explicit partial-result API; track
  upstream response and PIT semantics.

## OPENSEARCH-DEC-011: External-version writes and deletes

**Authoritative reference:** [OpenSearch index and delete APIs at 3.8.0](https://github.com/opensearch-project/OpenSearch/tree/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api).

- **Status, owner, and classification:** `resolved`; maintainers; derived-state
  consistency and REST interoperability policy.
- **Source and issue:** OpenSearch supports several version modes and action
  APIs with different parameters. Its delete-version tombstones expire after
  `index.gc_deletes`, so an old external version can be accepted after garbage
  collection. Search projections must remain monotonic under replay without
  pretending the index is authoritative.
- **Interpretations and peer behavior:** Use internal versions, overwrite
  blindly, issue update scripts, or require external versions. Adapters differ
  on delete and alias safety parameters.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Index, upsert, and delete use external version
  semantics and validate the returned version. Every write additionally
  requires a pre-resolution application guard backed by durable authoritative
  current documents or tombstones; without one, write capabilities are absent
  and writes fail explicitly. Index/upsert require an alias; delete omits
  unsupported `require_alias` and depends on the authorized write alias returned
  by the resolver. The request alias and accepted physical response generation
  are distinct resolver outputs, updated atomically under the cutover write
  fence. Update-existing is rejected before I/O.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestWriteUsesExternalVersioningForSupportedDocumentActions`,
  `TestWriteDeleteOmitsUnsupportedRequireAliasParameter`, and
  `TestWriteRejectsSuccessfulResponseWithWrongExternalVersion`,
  `TestWritesFailClosedWithoutDurableGuard`, and
  `TestRealOpenSearchDurableGuardSurvivesDeleteVersionGC` cover `Write`.
  Reconsider on server support changes or a new projection protocol; track
  index, delete, and delete-version garbage-collection semantics.

## OPENSEARCH-DEC-012: Bulk attribution and unknown outcomes

**Authoritative reference:** [OpenSearch bulk API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/bulk.json).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  batch wire and durability policy.
- **Source and issue:** Bulk uses positional NDJSON requests and item results.
  Transport failure, missing items, reordered identities, malformed actions,
  and inconsistent success fields can make receipt ambiguous.
- **Interpretations and peer behavior:** Retry the whole batch, trust item
  order, match only IDs, discard malformed details, or preserve per-item known
  and unknown outcomes. Bulk helpers differ materially.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Final NDJSON bytes and item count are bounded.
  Every result must match action, ID, position, status, and version invariants.
  Partial failures remain per-item; ambiguous transport or malformed response
  produces attributed unknown outcomes rather than unsafe automatic retry.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestBulkEncodesExternalVersionsAndPreservesPartialOutcomes`,
  `TestBulkRejectsMisattributedResponseActions`, and
  `TestBulkWireContractAndResponseBoundaries` cover `Bulk`. Reconsider only
  with a stronger server receipt protocol; track upstream bulk changes.

## OPENSEARCH-DEC-013: Lifecycle, reindex, verification, and alias cutover

**Authoritative reference:** [OpenSearch reindex API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/reindex.json).

- **Status, owner, and classification:** `resolved`; maintainers; durable
  index-lifecycle and authorization policy.
- **Source and issue:** OpenSearch exposes create, reindex, task, count, alias,
  and delete APIs but does not define application rollout, rollback, source of
  truth, authorization, or safe handling of ambiguous mutations.
- **Interpretations and peer behavior:** Mutate aliases directly, block until
  reindex completes, trust count equality, or expose explicit resumable steps.
  Operational clients often mix these concerns.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Lifecycle operations require separate
  authorization and explicit versioned physical indexes. Reindex is resumable;
  verification precedes atomic alias cutover; rollback swaps the alias back;
  cleanup is a separate authorized step. Derived state is rebuilt from the
  application source of truth and durable replay log.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestLifecycleImplementsCreateResumableReindexVerifyCutoverAndCleanup`,
  `TestLifecycleRequiresAuthorizationBeforeNetworkAccess`, and
  `TestLifecycleMutationsReportAmbiguousTransportAndMalformedOutcomes` cover
  lifecycle APIs. Reconsider per new durable transition; track upstream index,
  task, reindex, and alias behavior.

## OPENSEARCH-DEC-014: Templates, mappings, analyzers, and fingerprints

**Authoritative reference:** [OpenSearch index-template API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/indices.put_index_template.json).

- **Status, owner, and classification:** `resolved`; maintainers; application
  schema-ownership policy.
- **Source and issue:** OpenSearch accepts mappings, settings, analyzers, and
  composable templates but does not identify the application's canonical
  schema or decide when a cursor crosses incompatible generations.
- **Interpretations and peer behavior:** Generate mappings in the adapter,
  infer field types, mutate templates in place, or require caller-owned
  canonical definitions. Framework integrations commonly hide this boundary.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Applications own canonical definitions and
  ranking. The adapter validates bounded structures and authorized template
  operations. A stable definition fingerprint accompanies each resolved index
  and binds cursors to one generation; changed mappings invalidate traversal.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestIndexTemplatesUseAuthorizedComposableTemplateAPI`,
  `TestSearchUsesPITSearchAfterAndSignedQueryBoundCursor`, and
  `TestAddAliasCreatesAnAuthorizedReadWriteBoundary` cover template and
  fingerprint behavior. Reconsider on a versioned schema registry contract;
  track upstream template and mapping changes.

## OPENSEARCH-DEC-015: Health, readiness, and capacity signals

**Authoritative reference:** [OpenSearch cluster health API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/cluster.health.json).

- **Status, owner, and classification:** `resolved`; maintainers; operational
  interpretation policy.
- **Source and issue:** Cluster health, stats, breakers, and thread pools expose
  many fields but do not define application readiness or safe telemetry
  cardinality. Missing fields can otherwise be mistaken for healthy zeros.
- **Interpretations and peer behavior:** Treat HTTP 200 as ready, expose raw
  responses, require green status, or compute a bounded operational view.
  Monitoring integrations use different thresholds.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Health requires structurally complete bounded
  responses and marks red or incomplete clusters unready. Capacity preserves
  explicit saturation signals without retaining node IDs, tenant labels,
  index names, or raw provider bodies. Threshold policy remains with callers.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestHealthMarksRedOrIncompleteClustersUnready`,
  `TestHealthAndCapacityPreserveOperationalSignals`, and
  `TestCapacityValidationContract` cover health and capacity APIs. Reconsider
  on an explicit service readiness profile; track upstream health/stats fields.

## OPENSEARCH-DEC-016: Failure and telemetry disclosure

**Authoritative reference:** [OpenSearch REST API specification at 2.19.6](https://github.com/opensearch-project/OpenSearch/tree/97d3c13bf22a4a72ac11dc503fe44c97662b9161/rest-api-spec/src/main/resources/rest-api-spec/api).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  diagnostics and privacy policy.
- **Source and issue:** Provider errors, queries, source documents, cursors,
  endpoints, and authorization failures may contain secrets or unbounded
  attacker data. REST specifications do not define an application log schema.
- **Interpretations and peer behavior:** Return raw errors, log request bodies,
  attach endpoint names, expose only sentinels, or use bounded classifications.
  Client libraries vary widely.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Public failures preserve stable operation,
  class, status, and outcome identity while excluding backend bodies and
  signer/provider details. Telemetry uses bounded attributes, contains observer
  panics, and does not record queries, sources, credentials, cursors, tenant
  labels, or physical index names.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestFailureDiagnosticAndClassificationContract`,
  `TestInfoClassifiesMalformedErrorBodiesWithoutEchoingThem`, and
  `TestTelemetryEmitsBoundedOperationOutcomesAndContainsObserverPanics` cover
  failures and telemetry. Reconsider only through a privacy review; track
  upstream error-shape changes.

## OPENSEARCH-DEC-017: Admission, circuit state, and shutdown ownership

**Authoritative reference:** [OpenSearch Go client transport at 4.7.3](https://github.com/opensearch-project/opensearch-go/tree/172ea95af6dfe30b612cc42ac736e7dd613154d9/opensearchtransport).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  concurrency and lifecycle policy.
- **Source and issue:** The client transport does not define this adapter's
  concurrency budget, queue wait, circuit behavior, observer containment, or
  ownership of caller-supplied idle connections.
- **Interpretations and peer behavior:** Allow unbounded concurrency, queue
  indefinitely, share a global breaker, always close transports, or expose
  explicit bounded ownership. Peer clients differ in hidden retry and pooling.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Admission and queue waits are finite, circuit
  state prevents overload amplification, cancellation releases capacity, and
  all state is instance-local. `Close` is idempotent and closes idle resources
  only when ownership was transferred; borrowed transports remain caller-owned.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestAdmissionRejectsExcessWorkWithoutReachingTransport`,
  `TestCircuitBreakerStopsOverloadAmplificationUntilCooldown`,
  `TestOwnedTransportIsUsedAndClosedExactlyOnce`, and
  `TestBorrowedTransportRemainsCallerOwned` cover resilience and lifecycle.
  Reconsider only with an explicit shared policy; track client transport changes.

## OPENSEARCH-DEC-018: Compatibility and reconsideration policy

**Authoritative reference:** [OpenSearch 2.19.6 source](https://github.com/opensearch-project/OpenSearch/tree/97d3c13bf22a4a72ac11dc503fe44c97662b9161).

- **Status, owner, and classification:** `resolved`; maintainers; SemVer,
  release, and conformance policy.
- **Source and issue:** OpenSearch releases can change accepted parameters,
  response fields, security behavior, and operational semantics without
  changing this adapter's Go API. Passing unit tests alone cannot prove server
  compatibility.
- **Interpretations and peer behavior:** Follow latest automatically, accept a
  broad version range, pin only production, or require an exact executable
  matrix. Integrations commonly conflate compile and runtime compatibility.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. REST paths, parameters, JSON shapes, result
  classifications, retry ownership, cursor binding, external versions, and
  lifecycle transitions are compatibility surfaces. Any source pin change
  requires real-server conformance, upgrade, security, benchmark, API, docs,
  and changelog review before the supported matrix changes.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestRealOpenSearchConformance`,
  `TestSupportedOpenSearchVersionsIncludesCurrentRelease`, and
  `TestContentTypeOwnershipAndWriteStatusBoundaries` cover the current matrix
  and wire surface. Reconsider when a superseding decision records migration,
  executable evidence, release impact, and upstream source changes; preserve
  this entry as history.

## OPENSEARCH-DEC-019: Search authorization precedes index resolution

**Authoritative reference:** [OpenSearch search API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/search.json).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  application authorization and disclosure policy.
- **Source and issue:** OpenSearch authorizes physical API requests but cannot
  decide whether an application principal may issue a logical query, use a raw
  extension, filter or sort by a field, aggregate data, or receive full source.
  Resolving a physical index first can disclose tenant topology to denied work.
- **Interpretations and peer behavior:** Authorize only the index, inspect the
  encoded backend JSON, permit typed queries by default, or authorize the
  complete logical intent before resolution. Generic clients leave this wholly
  to callers and often expose raw DSL unconditionally.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Every search requires `SearchAuthorizer`
  approval before resolver or transport access. The policy receives a
  caller-owned copy of the complete query and result-disclosure scope,
  including full-source and bounded pagination cost intent, without opaque
  cursor bytes. Denials collapse to `ErrSearchDenied` and raw extensions are
  unavailable when no authorizer is configured.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestSearchFailsClosedBeforeResolutionWithoutAuthorization`,
  `TestSearchAuthorizationReceivesExactRawQueryAndSourceScope`, and
  `TestSearchAuthorizationReceivesBoundedPaginationIntent` cover
  `SearchAuthorizer`, `SearchAuthorization`, `Capabilities`, and `Search`.
  Reconsider only with an equivalent application policy seam; no upstream
  OpenSearch issue can define business authorization.

## OPENSEARCH-DEC-020: Reindex continuation cursor

**Authoritative reference:** [OpenSearch tasks API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/tasks.get.json).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  lifecycle continuation and credential policy.
- **Source and issue:** Asynchronous reindex returns a backend task ID but does
  not bind it to an application tenant, source generation, target generation,
  expiry, or safe path grammar. Returning the raw ID permits cross-operation
  substitution and exposes provider topology.
- **Interpretations and peer behavior:** Return raw task IDs, store task state
  in the adapter, accept caller-provided paths, or issue an encrypted opaque
  continuation. Operational clients usually expose the backend task directly.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Reindex continuation is a separate bounded
  AES-256-GCM cursor binding task ID, tenant, physical source, physical target,
  and bounded expiry without disclosing those values to the token holder.
  Tampering, unsafe task syntax, cross-binding reuse, expiry, and over-limit
  tokens fail before polling. Every successful incomplete poll reseals the same
  task binding with a fresh bounded lease, and callers must replace their
  durable cursor checkpoint. It does not share search-cursor keys or semantics.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestReindexCursorIsEncryptedBoundAndExpiring`,
  `TestReindexFailsBeforeDispatchWithoutCursorCodec`, and
  `TestReindexCursorCodecRejectsUnboundedConfigurationAndClock` cover
  `ReindexCursorCodec`, `NewReindexCursorCodec`, and `Reindex`. Reconsider only
  if OpenSearch supplies an application-bound opaque task token; track upstream
  task and reindex changes.

## OPENSEARCH-DEC-021: Semantic verification gates alias cutover

**Authoritative reference:** [OpenSearch count API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/count.json).

- **Status, owner, and classification:** `resolved`; maintainers; defensive
  migration consistency and application-verification policy.
- **Source and issue:** Equal document counts do not prove that source and
  target generations contain the same IDs, external versions, source bytes,
  mappings, or settings. A caller-stored fingerprint does not attest live
  target state and sampling cannot safely authorize cutover.
- **Interpretations and peer behavior:** Trust count equality, sample records,
  compare only mappings, or require a full bounded semantic verifier. Reindex
  helpers commonly stop at task completion or count comparison.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. `VerifyIndex` performs count preflight and then
  requires a `LifecycleVerifier` to compare the complete bounded ID, external
  version, and canonical source-digest set from a stable point-in-time view and
  independently attest the live target definition fingerprint. Drift, missing
  attestation, impossible counts, or verifier failure prevents a verified
  report. `CutoverAlias`
  requires an application-owned `LifecycleCutoverGuard` that synchronously
  quiesces writes and retains that fence across a fresh complete verification
  and atomic alias mutation. Raw `SwapAlias` remains an explicitly unverified
  low-level primitive for bootstrap or already externally fenced recovery.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestVerifyIndexRequiresSemanticVerifierAfterCountPreflight`,
  `TestVerifyIndexUsesSemanticDriftWhenCountsAreEqual`,
  `TestVerifyIndexRejectsInvalidOrFailedSemanticVerification`, and
  `TestCutoverAliasHoldsApplicationFenceAcrossFinalVerificationAndSwap` plus
  `TestRealLifecycleVerifierUsesOnePointInTimeForBothGenerations` cover
  `LifecycleVerifier`, verification requests/results, `LifecycleCutoverGuard`,
  `VerifyIndex`, and `CutoverAlias`.
  Reconsider only with equivalent server-side proofs; track count, mapping,
  settings, PIT, and alias API changes.

## OPENSEARCH-DEC-022: Snapshot restore is deployment-owned evidence

**Authoritative reference:** [OpenSearch snapshot restore API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/snapshot.restore.json).

- **Status, owner, and classification:** `resolved`; maintainers; operational
  conformance and scope policy.
- **Source and issue:** Snapshot repositories require server-side filesystem or
  object-storage configuration, credentials, repository lifecycle, and restore
  isolation. A generic application adapter cannot safely own those deployment
  controls, yet backup compatibility still requires executable proof.
- **Interpretations and peer behavior:** Add snapshot methods to the adapter,
  omit restore testing, assume managed-service backups, or keep deployment
  ownership while testing the supported matrix. Clients commonly expose raw
  snapshot APIs without an operational ownership model.
- **Selected behavior and consequences:**
  Security, resource, compatibility, and wire consequences are included in
  the selected behavior below. Snapshot repository creation and restore remain
  deployment-owned, not public adapter APIs. The real-server matrix exercises
  an isolated filesystem repository and verifies that restored documents retain
  IDs, sources, and external versions. Source-of-truth rebuild remains a
  separate required recovery path.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestRealOpenSearchSnapshotRestore` and
  `TestRealOpenSearchConformanceRebuildReconciliationRollbackAndConcurrentApplications`
  cover deployment restore, application rebuild, and same-revision concurrent
  instance evidence. Mixed application releases remain explicitly unsupported
  by the compatibility contract. The public
  surface remains intentionally absent. Reconsider only when a portable
  repository lifecycle contract is defined; track upstream snapshot changes.

## OPENSEARCH-DEC-023: Destructive cleanup shares durable mutation exclusion

**Authoritative reference:** [OpenSearch aliases API 3.8.0](https://github.com/opensearch-project/OpenSearch/blob/e5a3c5691be87af6c12dbe3e158c59c04ee72973/rest-api-spec/src/main/resources/rest-api-spec/api/indices.update_aliases.json).

- **Status, owner, and classification:** `resolved`; maintainers; destructive
  lifecycle concurrency and application-coordination policy.
- **Source and issue:** Alias inventory, retained-reader, retention, backup, and
  generation-identity checks occur separately from physical index deletion.
  Another application instance can otherwise create or re-alias the candidate
  after the final check and before deletion.
- **Interpretations and peer behavior:** Treat the check as advisory, rely on a
  process lock, expose raw deletion, or require one durable coordinator shared
  by every lifecycle mutation. OpenSearch does not provide one transaction
  spanning application prerequisites, alias inspection, and index deletion.
- **Selected behavior and consequences:** `CreateIndex`, `AddAlias`,
  `SwapAlias`, `CutoverAlias`, and `CleanupIndex` require an application-owned
  `LifecycleMutationGuard`. It receives the tenant, operation, and complete
  resource set, invokes the mutation synchronously exactly once, and must hold
  cross-instance exclusion until the callback returns. Cleanup nests its final
  `LifecycleCleanupGuard` checks and deletion inside the same exclusion.
  Missing, repeated, asynchronous, or contradictory callbacks fail with a
  stable typed error and do not expose coordinator details.
- **Evidence, public surface, upstream, and reconsideration:**
  `TestAliasMutationCannotBypassActiveCleanupExclusion`,
  `TestSharedMutationGuardSerializesAliasMutationAfterCleanup`, and
  `TestRealOpenSearchConformancePersistsAndResumesMigratorLifecycle` exercise
  fail-closed omission, shared serialization, and a real backend with an OS
  lock shared across independent clients. Reconsider only if OpenSearch adds
  an atomic primitive covering all application deletion prerequisites.

## Unresolved and excluded behavior

No known material ambiguity inside the current public surface remains open.
Plugin APIs, unrestricted Query DSL, application relevance, mappings,
analyzers, managed-service extensions, cross-cluster search, vector search,
and server-side authorization policy remain outside the current claim. Adding
one requires a new decision before runtime implementation.
