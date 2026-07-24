# Hardening evidence

The release gate maps the security contract to executable evidence rather than
using statement coverage as a substitute for behavior.

| Risk | Evidence |
| --- | --- |
| Absent, empty, duplicate, conflicting, malformed, oversized, non-canonical, Unicode, and control-bearing values | `TestEveryCarrierFieldRejectsHostileValueClasses` applies every class to correlation, request, and causation fields. HTTP and JSON-RPC tests cover their native representations and duplicate limits. |
| Trusted and untrusted boundaries | Factory, HTTP proxy, queue, scheduler, JSON-RPC, webhook, and request-ID bridge tests prove explicit trust and fresh request identity. |
| Cross-transport causation | `TestEveryTransportPreservesWorkflowAndRotatesAttemptIdentity` follows HTTP, JSON-RPC, queue delivery and redelivery, retry, scheduled work, and webhook send and receive. |
| Deterministic privacy | Vector, boundary, concurrency, and mutation tests prove versioned domain separation, key isolation, bounded input and output, and stable behavior. `privacy.md` records linkability and disclosure constraints. |
| Authority and cardinality separation | The public contract prohibits use for identity, authentication, authorization, tenancy, replay, billing, deduplication, or idempotency. Telemetry tests prove metrics contain only fixed-cardinality presence booleans. |
| Concurrency and aliasing | Race tests share the default factory, deterministic strategy, codec, and context propagation across workers while checking uniqueness, stable derivation, and copied key material. |
| Bounded parsing | Core codecs cap values at eight. HTTP stops collecting at the ninth value, and JSON-RPC rejects excess values and encoded identifiers before decoding. |
| Test sensitivity | Mutation gates invert precedence, trust, fresh generation, immediate causation, overwrite, transport bounds, deterministic versioning, and disclosure defaults. |

`make check-all` runs the ordinary suite, race detector, exact production
coverage gate, fuzz targets, mutation suite, benchmarks, sibling integration,
documentation and API checks, linters, vulnerability analysis, and NilAway.
