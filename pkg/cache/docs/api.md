# API reference

The Go package documentation is the source of truth for signatures. This guide
defines the behavioral contract that those signatures carry.

## Construction

`New(Config[K, V])` requires a backend, key space, codec, clock, positive value
limit, and valid positive TTL. Zero loader and batch limits select bounded
defaults. Negative limits, contradictory stale policies, and jitter at or above
the positive TTL are rejected.

Construction errors match `ErrInvalidConfig`, `ErrInvalidTTL`, or
`ErrInvalidPolicy`.

## Results

`Result[V]` contains:

- `State`: `Hit`, `Miss`, or `Stale`;
- `Value`: decoded value for hits and stale results;
- `Negative`: true when a miss came from negative caching.

`Get` returns `Miss, nil` for absence. It never converts backend, record,
schema, or decode failures into misses. A record past `ExpiresAt` but before
`StaleAt` returns `Stale`. A record at or beyond `StaleAt` is deleted and
returns `Miss` unless cleanup fails.

Stored zero values and nil-like values remain hits. Empty raw bytes are not a
tombstone and fail schema validation. Negative records are the only built-in
absence marker.

## Mutations

- `Set` writes unconditionally.
- `Add` atomically writes only if no live record exists.
- `Replace` atomically writes only if a live record exists.
- `Delete` is idempotent at the semantic API.

Conditional false results are not errors. Backends must implement the
conditions atomically.

## Cache-aside loading

`GetOrLoad` first performs `Get`, then joins or creates one in-process flight
per backend key. The loader returns `LoadResult[V]{Found:false}` for an
authoritative absence. A loader error is not absence and matches `ErrLoader`.

Flights are bounded globally by `MaxConcurrent` and per key by
`MaxWaitersPerKey`. Caller cancellation detaches that caller; it does not cancel
a load still needed by other callers. `Close` cancels the shared load context,
waits for all flight cleanup, and makes subsequent operations return
`ErrClosed`.

Successful same-instance mutations supersede an active load for that key, so a
load cannot overwrite a `Set` or resurrect a `Delete`. Loaders must use their
supplied context; recursive same-cache calls with that context return
`ErrRecursiveLoad`. These guarantees are process-local.

## Bulk operations

`GetMany`, `SetMany`, and `DeleteMany` preserve input order. A request beyond
`MaxBatch` fails before backend access with `ErrBatchTooLarge`. Individual
operation failures are stored beside each key and do not stop the remaining
items.

## Keys and codecs

`NewKeySpace` accepts lowercase ASCII namespace/name parts containing digits,
hyphen, or underscore, a nonzero version, a deterministic encoder, and a
positive backend-key limit. Backend keys contain the versioned prefix and a
SHA-256 digest, never raw logical bytes.

`JSONCodec` prefixes strict JSON with a nonzero one-byte version. It rejects
unknown fields, malformed or trailing JSON, wrong versions, and size excesses.
Custom codecs implement `Codec[V]` and must provide the same deterministic,
bounded behavior expected by the application.

## Errors

Use `errors.Is`, not string matching. Important sentinels are:

- `ErrBackend`, `ErrDecode`, and `ErrSchemaMismatch`;
- `ErrInvalidKey`, `ErrKeyTooLarge`, and `ErrValueTooLarge`;
- `ErrInvalidTTL`, `ErrInvalidPolicy`, `ErrInvalidConfig`, and
  `ErrInvalidRecord`;
- `ErrLoader`, `ErrLoaderPanic`, `ErrRecursiveLoad`, and `ErrWaiterLimit`;
- `ErrCapacity`, `ErrBatchTooLarge`, and `ErrClosed`.

`Error` also exposes `Kind`, `Operation`, and the original cause. Context
cancellation and deadlines are returned directly so `errors.Is` remains useful.

## Observers

`Observer.Observe` receives redacted `Event` values with operation, outcome,
duration, and size only. Observer errors and panics are isolated from cache
behavior. Implementations must keep labels bounded and must not infer or attach
keys or values.

## Backend conformance

Third-party backends should invoke:

```go
cachetest.RunBackendConformance(t, cachetest.BackendHarness{
    Backend: backend,
    MakeUnavailable: func(t *testing.T) {
        // Deterministically stop or disconnect the backend.
    },
})
```

The suite checks misses, cloned payloads, positive and negative round trips,
atomic conditions, idempotent deletion, cancellation, invalid records and
conditions, expiration semantics, and that outages remain errors for reads,
writes, and deletes. The outage hook runs last. Backends may expose additional
native operations outside the semantic API.
