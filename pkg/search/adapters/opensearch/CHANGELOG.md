# Changelog

## Unreleased

### Changed

- Complete the destructive-cleanup decision record with explicit security,
  compatibility, and wire consequences for the required durable lifecycle
  mutation guard.

- Reject unsupported locale mappings and foreign raw-query extensions before
  creating a point-in-time search context.
- Add an auditable OpenSearch REST decision register, pinned source provenance,
  and fail-closed source integrity verification.
- Refresh real-cluster compatibility from OpenSearch 2.19.3/3.6.0 to the
  current 2.19.6/3.8.0 releases while retaining official client v4.7.3.
- Cap concurrency channels, queued callers, locale analyzer maps, and discovery
  trust rules before allocation or configuration cloning.
- Hold each in-flight admission permit until its response body reaches EOF or
  is closed, so slow or abandoned bodies remain inside the configured bound.
- Exercise a deployment-owned filesystem snapshot restore for each supported
  OpenSearch version while preserving external document versions, alongside
  full source-of-truth rebuild and reconciliation evidence.
- Require complete pre-resolution search authorization, including bounded
  pagination cost intent; AES-256-GCM-encrypted, expiring,
  tenant/source/target-bound reindex cursors; live target-definition
  fingerprint attestation; and an application-owned write fence across final
  verification and migration cutover.

- Require one application-owned durable lifecycle mutation coordinator across
  index creation, alias changes, cutover, and cleanup so final deletion
  eligibility cannot race another instance reactivating the generation.
- Run ten independent equivalent-semantics benchmark samples for each supported
  real OpenSearch version in the release matrix instead of a single sample.
- Make `CursorCodec` the sole cursor clock owner; `SearchConfig.Clock` remains
  source-compatible but is deprecated and ignored.
- Renew encrypted reindex cursors after every successful incomplete poll so a
  long-running backend task remains safely resumable without unbounded tokens.
- Disable write capabilities unless an application-owned durable `WriteGuard`
  authorizes immutable bounded operations against current documents or
  tombstones before target resolution, preventing stale replay after backend
  delete-version garbage collection.
- Split resolved request aliases from exact physical response generations and
  require resolver generation updates inside write-fenced alias cutover.
- Apply the 1,024-file-descriptor stress ceiling to disposable single-node
  clusters while using OpenSearch's required 65,536 minimum for multi-node
  rolling-upgrade proof.
- Reject `ActionUpdate` with `search.ErrUnsupported` before index resolution or
  transport because OpenSearch external-version writes cannot preserve shared
  update-existing semantics. Callers that intentionally accept create-or-replace
  semantics should use `ActionIndex` or `ActionUpsert`.
- Classify bulk HTTP 404 outcomes as `OutcomeNotFound` only for deletes;
  non-delete items now report `OutcomeFailed` when a safe backend failure is
  present and `OutcomeUnknown` otherwise.

### Fixed

- Copy validated explicit proxy URLs during client construction so later
  caller mutation cannot redirect requests or inject proxy credentials.
- Bound every returned cluster identity field to 1,024 bytes and reject
  control characters before the values can escape the adapter.
- Reject lifecycle verification of one generation against itself and cleanup
  bindings that identify a physical generation as the logical alias before
  authorization, guard callbacks, or backend requests.
- Reject invalid UTF-8 in bounded backend response bodies before JSON decoding
  can normalize poisoned identifiers or diagnostic text.
- Reject invalid UTF-8 locale configuration and lifecycle tenant or migration
  identities, and bound migration IDs before policy callbacks, resolver work,
  or backend requests.
- Isolate external-version ranges between benchmark samples and reject any
  release transcript with failed or missing samples before comparison.
- Replace the real security matrix's `all_access` runtime fixture with
  tenant-A-only read/write credentials and a separate bounded operator role;
  prove tenant-B and administrative denials on OpenSearch 2.19.6 and 3.8.0.
- Bind every accepted hit, bulk item, and successful single-write response to
  the resolver's exact physical generation while returning only the logical
  index to callers; reject response sources outside requested projections.
- Bound initial and rotated PIT identifiers, cap continuation keep-alive by the
  signed deadline's remaining lifetime, and classify a missing resource as PIT
  expiry only for cursor searches.
- Measure repeated real-network cleanup against explicit retained goroutine,
  file-descriptor, and heap budgets in the leak gate.
- Preserve the underlying known or unknown alias-mutation outcome when
  classifying cutover-guard contract failures, and wait for every started
  callback so an in-flight request cannot mutate after the caller returns;
  concurrent repeated callbacks can no longer displace or block the primary
  callback result.
- Reject exhausted cursor traversal before another search is dispatched and
  enforce cumulative item and response-byte limits on final short pages.
- Reject successful write and bulk items whose status/result pair does not
  match the requested index, upsert, or delete action.
- Classify transport loss and malformed success acknowledgements for PIT
  creation and deletion as unknown outcomes, and never repeat a failed PIT
  cleanup inside one search call.
- Start cursor expiry when the PIT is created, cap search response reads at the
  stricter result limit, and require OpenSearch's search, bulk, reindex, count,
  and health response metadata instead of accepting absent zero values.
- Redact lifecycle-verifier failures behind stable typed classifications while
  preserving cancellation and deadline semantics.
- Omit OpenSearch's unsupported `require_alias` parameter from single-document
  deletes while retaining external versions, so version-ordered deletes work
  through resolved write aliases on OpenSearch 2.19.6.

### Added

- Initial OpenSearch v4 client adapter for typed search, point-in-time cursor
  pagination, externally versioned writes and bulk operations, trusted node
  discovery, bounded transport decoding, and authorized index lifecycle flows.
- Explicit `opensearch`-bound raw query objects for trusted callers, with core
  size and JSON-object validation before transport execution.
- Fixed-deadline PIT cursors that cannot extend the configured total traversal
  duration on subsequent pages.
- Pre-network analyzer validation and final encoded bulk-byte enforcement.
- Authorized templates, process-local backpressure/circuit telemetry, health
  and capacity reports, migration/rebuild/reconciliation flows, AWS signing,
  and exact OpenSearch 2.19.6/3.8.0 compatibility evidence.
- Search authorization receives the complete query and result-disclosure scope;
  raw extensions are unavailable unless an authorizer is configured.
