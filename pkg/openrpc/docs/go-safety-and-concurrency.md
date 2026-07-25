# Go safety and concurrency

Parsed and constructed values own their input bytes, slices, maps, and fields.
Public collection and byte getters return copies. Concurrent read-only use is
therefore supported after construction.

`builder.MethodRegistry` and `discovery.Cache` synchronize their mutable state.
Resolver caches are per call, so authorization and fetched resources do not
leak across requests. Cancellation of one cache waiter does not cancel shared
discovery work owned by other waiters.

No constructor starts a goroutine. No package installs a global resolver,
schema loader, registry, cache, or telemetry exporter. Callers own lifecycle,
shutdown, transport, and exporter behavior.

Run `make race` after concurrency changes. `make leak` runs package-wide
goroutine leak detection around the registry, discovery cache, observer hooks,
resolver, HTTP store, and all cancellation tests. Both gates are blocking.
Ownership claims also require mutation tests that modify caller inputs and
returned snapshots, not only race-detector silence.
