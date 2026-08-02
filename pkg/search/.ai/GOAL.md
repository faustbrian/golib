# Goal: Search Contracts And Index Lifecycle

## Objective

Build `search` as a backend-neutral Go package for indexing, querying,
paginating, migrating, and operating full-text and structured search without
hiding backend capabilities or pretending all search engines are identical.

OpenSearch MUST be the first production adapter. The core MUST remain useful
without OpenSearch and MUST not reduce its API to a lowest-common-denominator
query string.

## Domain Model

Define explicit documents, stable document IDs, index names/aliases, document
versions, fields, values, queries, filters, sorting, pagination, highlights,
aggregations, facets, suggestions, bulk operations, results, hits, scores,
partial failures, and diagnostics.

The model MUST define exact numbers, timestamps, null/missing/empty fields,
Unicode, locale, analyzers, nested values, geo values, raw backend extensions,
ownership, and size limits. Backend-specific features MUST use capability
discovery or explicit adapter APIs rather than silent degradation.

## Query And Pagination

Support typed composition for boolean queries, exact terms, full text, prefix,
range, existence, geo, filters, sorting, source projection, aggregations,
highlights, and suggestions required by Track and Location. Invalid or
unsupported combinations MUST fail before network execution when possible.

Cursor/search-after pagination MUST be first-class and bounded. Define stable
sort requirements, point-in-time consistency, cursor encoding, expiry,
tampering, query binding, maximum pages/items/bytes/time, and changed-index
behavior. Offset pagination MAY exist with explicit cost and consistency limits.

## Indexing And Consistency

- Support single and bounded bulk index, update, upsert, and delete.
- Require explicit external/version semantics where supported.
- Report per-item outcomes and unknown bulk outcomes without flattening them
  into one success/failure boolean.
- Define refresh, visibility, read-your-write, retry, duplicate, and ordering
  behavior.
- Provide outbox and event-driven indexing seams with idempotent projection.
- Provide reconciliation and drift detection against the source of truth.

The search index MUST be treated as rebuildable derived state unless an
application explicitly documents otherwise.

## Schema And Index Lifecycle

Provide explicit index definitions, mappings/settings fingerprints, aliases,
templates where supported, compatibility checks, create, reindex, verify,
atomic alias cutover, rollback, cleanup, and resumable migration workflows.
Index lifecycle operations MUST be separately authorized and observable.

## Resilience And Resources

All I/O MUST accept context and bounded time, bytes, concurrency, retries, and
response decoding. Integrate explicitly with rate-limit, retry,
circuit-breaker, bulkhead, concurrency-limit, telemetry, tenancy, correlation,
and authentication. Retry policy MUST account for idempotency, bulk partial
results, overload, and unknown outcomes without amplification.

## Testing Support

Provide a deterministic bounded in-memory fake only for application contract
tests. It MUST not claim ranking or analyzer equivalence with OpenSearch.
Adapter conformance suites MUST prove shared semantics against every backend.

## Verification

Test query encoding, cursor binding, result ownership, bulk partial outcomes,
index migration, alias cutover, rollback, reconciliation, tenant isolation,
timeouts, cancellation, malformed responses, and resource bounds. Run fuzz,
race, leak, stress, soak, fault-injection, and real OpenSearch integration tests.
Exact 100% statement coverage and 100% viable mutation kills are REQUIRED.

## Documentation And Delivery

Document when to use PostgreSQL versus search, consistency, source-of-truth
rules, query APIs, pagination, schema migration, bulk ingestion, rebuild,
operations, security, capacity, FAQ, and Track/Location adoption examples. Add
manifests, CI, benchmarks, changelog, and clean-consumer proof.

## Non-Goals

- a relational query abstraction or source-of-truth datastore;
- pretending relevance behavior is portable across engines;
- silently exposing unrestricted raw query DSL to untrusted callers;
- owning application-specific document schemas or ranking decisions.
