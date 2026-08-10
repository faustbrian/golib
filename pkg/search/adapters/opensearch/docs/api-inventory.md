# OpenSearch API and compatibility inventory

Implementation baseline: OpenSearch `2.19.6` and `3.8.0`, with
`github.com/opensearch-project/opensearch-go/v4` `v4.7.3`. These exact server
images form the release conformance matrix. A different patch, plugin set, or
managed-service feature profile is outside the proven matrix until added and
verified.

| Adapter operation | OpenSearch API | Compatibility boundary |
| --- | --- | --- |
| `Info` | `GET /` | requires node, cluster, UUID, and version fields |
| `Discover` | `GET /_nodes/http` | explicit invocation; only allowlisted data-node publish addresses replace seeds |
| `Write` | `PUT/DELETE /{alias}/_doc/{id}` | `version_type=external`; index/upsert use `require_alias=true`; delete omits the unsupported parameter and relies on resolver-authorized write-alias selection; no automatic retry |
| `Bulk` | `POST /_bulk` | bounded NDJSON; external version metadata; every response action, ID, status, and position is checked |
| `Search` | `POST /{index}/_search` or `POST /_search` | typed DSL plus explicitly authorized, adapter-bound raw extension objects; PIT requests use the global search endpoint |
| PIT create/delete | `POST /{index}/_search/point_in_time`, `DELETE /_search/point_in_time` | signed cursor owns PIT ID and cleanup; expiry is classified |
| health/capacity | cluster health/stats and bounded node thread-pool/breaker stats | no node IDs, index names, tenant labels, or query data are retained in reports |
| lifecycle | index create/delete, reindex tasks, count, aliases | separate tenant authorizer; reindex preserves external versions; cutover is atomic |
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
Direct analyzer names must match the adapter's bounded analyzer-name grammar,
and bulk admission checks the final encoded NDJSON size before transport.

Primary references:

- <https://docs.opensearch.org/latest/clients/go/>
- <https://github.com/opensearch-project/opensearch-go/tree/v4.7.3>
- <https://github.com/opensearch-project/OpenSearch/releases/tag/2.19.6>
- <https://github.com/opensearch-project/OpenSearch/releases/tag/3.8.0>
- <https://docs.opensearch.org/latest/api-reference/>
