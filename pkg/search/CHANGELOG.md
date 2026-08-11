# Changelog

All notable changes to this module are documented here.

## Unreleased

- Make cursor time single-owner through `CursorCodec.Deadline` and `Remaining`,
  so adapters cannot create or extend backend state past signed cursor expiry.
- Require migration stores to provide durable exclusive `MigrationCoordinator`
  execution across `Run`, `Rollback`, and `Cleanup`, preventing duplicate
  dispatch and destructive lifecycle races across application instances.
- Reject invalid UTF-8 throughout documents, schemas, values, writes, query
  fields, projection events, migration and reconciliation identities, and
  backend hit identities; apply JSON depth/node limits to raw extensions.
- Reject invalid UTF-8 or unbounded bulk outcome codes and diagnostics so
  adapter classifications cannot bypass the shared result boundary.
- Replace backend-native `_id` sorting in shared requests with the virtual
  `DocumentIDSortField` stable tie-breaker that adapters translate explicitly.
- Charge range-aggregation buckets to the shared query-node budget before
  normalization can amplify work.
- Canonicalize reconciliation source digests and bound combined retained
  source/index/report/repair bytes by `MaxResultBytes`.
- Align the deterministic fake and shared conformance contract with OpenSearch
  multi-valued keyword semantics: exact-term and prefix queries now match any
  scalar element in a JSON array instead of silently treating the array as a
  non-match.
- Refuse orphan reconciliation deletes based only on source-snapshot absence;
  applications must provide an atomic source-side deletion guard that reserves
  a durable monotonic tombstone version before any orphan repair is dispatched.
- Make the module `check` contract fail closed on mutation, stress, leak, fault,
  soak, and vulnerability gates instead of leaving those required targets
  opt-in.
- Bind migration verification to the target `IndexDefinition` fingerprint so a
  resumable run cannot cut over after verifying a different live definition.
- Bound migration identifiers, tenant labels, and per-run reindex traversal
  before lifecycle authorization or external work.
- Require a retained source-definition fingerprint and a backend-verified,
  write-fenced `CutoverAlias` for both migration cutover and rollback; the core
  lifecycle contract no longer exposes an unverified raw alias swap.
- Reject ambiguous or malformed search results, including missing total
  relations and hit versions, invalid aggregation or suggestion payloads, and
  inconsistent diagnostics; bulk outcomes can now be bound back to their
  originating request.
- Gate update-existing writes with an explicit adapter capability so unsupported
  adapters cannot silently translate update semantics to index or upsert.
- Bound document, direct write-operation, and index-definition JSON nesting and
  aggregate object-field and array-element nodes, reject duplicate keys, and
  reject control-bearing physical index names without excluding valid lowercase
  Unicode.
- Bound highlight, aggregation, and suggestion result fanout during request
  validation in addition to the complete encoded-query byte limit.
- Reject non-scalar term and range values and preflight aggregate request input
  bytes and collection shapes before normalized-query allocation.
- Use non-overflowing bulk admission, preflight cursor encoding, bound
  reconciliation pages and totals, reject exhausted cursors before dispatch,
  enforce cumulative bounds on final cursor pages, and cap in-memory fake
  document capacity.
- Reject repair outcomes whose applied external version does not match the
  requested operation, and refuse to infer cutover or rollback completion from
  an already moved alias after an uncheckpointed lifecycle transition.
- Persist a reindex-dispatch marker so checkpoint loss cannot silently submit a
  duplicate backend task; interrupted dispatch now requires explicit recovery.
- Persist a create-dispatch marker so checkpoint loss cannot silently repeat
  physical index creation after an ambiguous backend outcome.
- Persist a cleanup-dispatch marker so checkpoint loss cannot repeat deletion
  of an inactive generation whose physical name may have been reused.
- Reject source reconciliation records whose declared digest does not match
  their bounded, validated document source.
- Bound reconciliation page count, cursors, record identifiers, and opaque
  digests; reject terminal cursors and unexpected index-side documents, and
  apply the total record budget across both source and index traversal.
- Require explicit limits when fingerprinting requests and reject oversized
  unvalidated input before canonical encoding.

### Added

- Backend-neutral document, value, typed query, result, bulk, cursor, schema,
  migration, projection, reconciliation, and deterministic fake contracts.
- Signed, expiring, query-bound cursor pagination with point-in-time state and
  explicit page, item, byte, request-byte, and non-sliding duration limits.
- OpenSearch production adapter with typed request encoding, external versions,
  per-item bulk outcomes, lifecycle authorization, resumable reindexing, and
  bounded transport handling.
- Capability-gated, adapter-bound raw extension queries for trusted application
  policy without an unrestricted query-string fallback.
- Validation for projection fields and positive finite full-text field boosts.
