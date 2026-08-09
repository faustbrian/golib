# API and ownership

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
bounded by `Limits.MaxSourceBytes`; it is not a general untrusted DSL endpoint.
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

## Results and failures

`Result` owns hits, scores, sort values, source fields, highlights,
aggregations, suggestions, diagnostics, partial-failure details, and the next
cursor. `BulkResult` preserves item order and per-item status, including an
unknown outcome after ambiguous transport failure. Callers must reconcile an
unknown write before retrying unless the same stable ID and external version
make replay safe.

All I/O interfaces accept `context.Context`. Applications compose retry,
rate-limit, circuit-breaker, bulkhead, concurrency-limit, telemetry, tenancy,
correlation, and authentication around an adapter; the core does not hide or
duplicate those policies.
