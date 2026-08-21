# Comparisons And Tradeoffs

Golib comparisons evaluate equivalent behavior, not framework popularity or
minimal hello-world throughput. Package-owned benchmark reports and conformance
evidence remain authoritative for measured claims.

| Decision | Golib position | Prefer the alternative when |
| --- | --- | --- |
| `service`/`router`/middleware vs `net/http` | Adds explicit lifecycle, routing metadata, bounded middleware, probes, and shared service behavior without a container. | Plain `net/http` already provides all required lifecycle and routing semantics. |
| Router stack vs Chi, Gin, Echo, or Fiber | Keeps `net/http` handlers and standard request ownership; avoids framework context and hidden binding. | An existing service depends materially on the framework ecosystem or measured equivalent workloads justify its different transport model. |
| `log`/slog vs Zap or Zerolog | Keeps `*slog.Logger` as the application contract and adds focused handlers. | A measured logging workload needs a native API or feature that slog handlers cannot provide. |
| `json-schema` vs santhosh-tekuri/jsonschema | Targets the full official suite with owned bounded parsing and ecosystem integration. | A maintained external validator already satisfies the exact dialect, extension, error, and resource contracts. |
| `openapi` vs kin-openapi or libopenapi | Owns version-aware lossless documents, validation, conversion, compatibility, and repository protocol integration. | The application needs only the alternative's narrower mature surface and does not need Golib's shared model. |
| `jsonrpc` or `jsonapi` vs maintained peers | Targets complete protocol behavior, explicit ambiguity decisions, conformance, and shared service integration. | A peer proves the same required specification surface and materially lowers ownership cost. |
| `queue`, `cache`, or `postgres` vs direct clients | Adds durable semantics, lifecycle, classification, and consistent adapters while retaining explicit construction. | Native client behavior is the full application contract and another policy layer would add no reusable invariant. |
| `http-client` vs direct `net/http` | Standardizes bounded vendor integrations, pagination, rate policy, retries, middleware, cache, and diagnostics. | One simple endpoint needs no reusable remote API policy beyond `net/http`. |

Fasthttp is not a drop-in faster `net/http`: it changes handler, request,
response, streaming, and compatibility semantics. Compare it only on the same
application behavior and include adapters, allocations, cancellation, limits,
and operational features in both workloads.

See [performance methodology](../performance.md), the generated
[benchmark catalog](../benchmark-catalog.md), and
[limitations](../limitations.md).

Return to the [documentation index](../index.md).
