# Production hardening audit

This audit is the release contract for the initial `v1` line. It distinguishes
portable cache semantics from server availability and native-client features.
Every supported result below is backed by a named unit, race, fuzz, or
integration test.

## Semantic truth table

`ExpiresAt` is the first instant at which a positive record is stale.
`StaleAt` is the first instant at which it is unusable.

| Stored state | `Get` result | Error | Loader behavior |
| --- | --- | --- | --- |
| No record | `Miss` | `nil` | A foreground flight may load |
| Fresh positive, including a zero or nil-like value | `Hit` with decoded value | `nil` | Not called |
| Stale positive | `Stale` with decoded value | `nil` | Policy table below |
| At or beyond `StaleAt` | `Miss` after deletion | Delete error only | Loads after successful cleanup |
| Live negative record | `Miss` with `Negative=true` | `nil` | Not called |
| Empty raw payload | No result | `ErrSchemaMismatch` | Not called |
| Malformed payload | No result | `ErrDecode` | Not called |
| Wrong codec version | No result | `ErrSchemaMismatch` | Not called |
| Structurally invalid record | No result | `ErrBackend` and `ErrInvalidRecord` | Not called |
| Oversized payload | No result | `ErrValueTooLarge` | Not called |
| Backend outage or timeout | No result | Preserved cause; outage matches `ErrBackend` | Not called |

There is no separate tombstone state. `Negative=true` is the only built-in
absence marker, must have an empty payload, and is valid only before its hard
deadline. Stored Go zero values and JSON `null` are values because `State`, not
the value, identifies a hit.

### Loading policy table

| Initial result | Plain | `StaleIfError` | `StaleWhileRevalidate` |
| --- | --- | --- | --- |
| `Hit` | Return hit | Return hit | Return hit |
| Negative miss | Return negative miss | Return negative miss | Return negative miss |
| Ordinary miss | Wait for one foreground flight | Same | Same |
| `Stale`, load succeeds | Wait; return fresh hit | Wait; return fresh hit | Return stale; refresh in background |
| `Stale`, load fails | Return loader error | Return stale plus loader error | Return stale; report failure through observer |

Enabling both stale policies is invalid. `Found=false` is an authoritative
absence; it returns a negative miss and is stored only when `NegativeTTL > 0`.
A loader error is never converted to absence. Recursive use of the same cache
with the loader-supplied context returns `ErrRecursiveLoad`.

### Mutation and bulk table

| Operation | Portable behavior |
| --- | --- |
| `Set` | Atomic unconditional replacement |
| `Add` | Atomic write only when no live record exists |
| `Replace` | Atomic write only when a live record exists |
| `Delete` | Idempotent at the cache API; expired records are absent |
| `GetMany` | Sequential input order; each result carries its own error |
| `SetMany` / `DeleteMany` | Sequential input order; continue after per-key failure |

Successful `Set`, `Add`, `Replace`, or `Delete` on the same `Cache` instance
wins over an active load. Failed and rejected conditional mutations do not
supersede it. This ordering is process-local; see remaining risks.

### TTL and clock rules

- `TTL` must be positive; `StaleFor`, `NegativeTTL`, and jitter must not be
  negative.
- Jitter subtracts from loaded-value freshness and must remain below `TTL`.
- Deadlines outside the portable signed Unix-nanosecond range are rejected.
- Process-local monotonic readings are stripped from stored deadlines. All
  backends therefore apply the injected wall clock consistently; a backward
  wall-clock adjustment can extend freshness until the clock catches up.
- Memory expiration is lazy and uses the injected clock without a janitor,
  goroutine, timer, or sleep.
- Redis and Valkey receive a relative `PX` duration to the hard deadline. A
  positive sub-millisecond duration is clamped to 1 ms; longer durations have
  millisecond server precision. The wire envelope retains the original Go
  deadline, so the semantic read still applies the injected clock.
- Backward observer-clock movement produces a zero duration, never a negative
  metric.

## Backend behavior matrix

| Capability | Memory | Redis 7.2/7.4/8.0 | Valkey 9.0 |
| --- | --- | --- | --- |
| Shared conformance contract | Unit | Integration matrix | Integration matrix |
| Standalone | Supported | Supported | Supported |
| Password authentication | N/A | Tested | Tested |
| Verified TLS | N/A | Tested | Tested |
| Atomic `NX` / `XX` | Mutex | Native `SET` | Native `SET` |
| Bounded read before transfer | In-process bound | Lua `STRLEN` guard | Lua `STRLEN` guard |
| Hard expiry | Lazy injected clock | Relative `PX` | Relative `PX` |
| Outage remains an error | Close test | Container termination | Container termination |
| Recovery at stable endpoint | N/A | Pause/unpause tested | Pause/unpause tested |
| Capacity eviction | Bounded LRU | Server policy | Server policy |
| Cluster, Sentinel, failover | N/A | Not release-tested or claimed | Not release-tested or claimed |
| Replica reads | N/A | Not claimed | Not claimed |
| Protocol batch/pipeline | N/A | Not used | Not used |

The release-supported network topology is standalone. Applications may supply
clients configured for other topologies, but that does not make them supported:
redirects, failover, replica staleness, script routing, and TLS between nodes
have not passed this release matrix. Semantic bulk operations deliberately use
one-key calls, so there is no hidden pipeline or transaction and no ambiguous
protocol-level partial batch.

## Ownership model

| Resource | Owner | Required action |
| --- | --- | --- |
| Native Redis/Valkey client, pool, credentials, and TLS | Application | Configure timeouts and close after cache loaders stop |
| Supplied backend | Application | Close it when its concrete type requires closure |
| Load context, semaphore, flight map, and load goroutines | `Cache` | Call `Cache.Close`; it cancels and waits |
| Loader I/O and child goroutines | Loader | Honor the supplied context and release all resources |
| Memory entries and LRU metadata | Memory backend | Call its `Close` to release retained state |
| Expiration cleanup | Reader/writer touching the key or server | No memory janitor exists |
| Observer/exporter lifecycle | Application | Keep callbacks bounded; monitor exporter health |

`Cache.Close` cannot safely abandon a loader that ignores cancellation, so
such a loader can delay shutdown indefinitely. Observer callbacks are
synchronous, best-effort, panic-isolated, and must not block.

## Threat model

| Threat | Control | Residual risk / operator duty |
| --- | --- | --- |
| Logical-key collision | SHA-256 over deterministic encoded bytes | Encoder must be injective for compound keys; cryptographic collision remains theoretical |
| Tenant escape | Versioned namespace plus hashed logical key | Include tenant identity in the logical key; never accept user-controlled namespace parts |
| Cache poisoning/type confusion | Strict version byte, record validation, bounded input, unknown/trailing JSON rejection | Use a distinct key-space and codec version for each value meaning |
| Stampede | Per-key flights, global loader bound, waiter bound, jitter | Coordination is per process, not distributed |
| Invalidation race | Same-instance mutations supersede active loads | Cross-process invalidation has no generation fence; use source versions or versioned keys when ordering is critical |
| Oversized key/value/batch | Configured hard limits and server-side length guard | Size native pools and server memory separately |
| Backend outage | Errors preserved; bounded client contexts; recovery tested | Choose fail-open only in use-case code |
| Stale authorization or pricing | Stale policies are opt-in and bounded | Do not enable stale serving for security-sensitive decisions |
| Secret or tenant leakage | Hashed keys; bundled events omit keys, values, causes, and credentials | Prefixes are visible; custom observers must preserve redaction |
| Untrusted backend bytes | Strict wire/codec validation and fuzzing | Cache data is not a trust or durability boundary |

## Findings and dispositions

| ID | Severity | Finding and reproduction | Disposition |
| --- | --- | --- | --- |
| H-01 | High | A loader could overwrite a concurrent `Set` or resurrect a concurrent `Delete`; reproduced by `TestMutationDuringLoadWinsWithoutResurrection` | Fixed with per-flight mutation precedence; background invalidation regression added |
| M-01 | Medium | Absolute server expiry used the injected application clock as server wall time; a year-2000 fake clock expired immediately | Fixed by relative hard-deadline TTLs |
| M-02 | Medium | Recursive loads could wait on their own flight or global slot forever | Fixed with `ErrRecursiveLoad` and loader-context ownership marker |
| M-03 | Medium | Memory `Delete` reported an expired retained entry as present, unlike Redis/Valkey; found by operation-model fuzzing | Fixed; minimized corpus retained |
| M-04 | Medium | Valkey encoded a positive sub-millisecond duration as invalid `PX 0` while Redis used 1 ms | Fixed by an adapter-owned 1 ms minimum |
| M-05 | Medium | Shared conformance did not prove that outages remain errors for all operations | Fixed with a required deterministic outage hook |
| M-06 | Medium | Memory retained process-local monotonic deadline data that serialized backends cannot preserve | Fixed by normalizing portable wall-clock deadlines in core and conformance |
| M-07 | Medium | Negative TTL overflow could reach and be accepted by a permissive third-party backend | Fixed by validating negative records before backend access |
| L-01 | Low | Authentication, verified TLS, duplicate OTel construction, and recovery were documented but not integration-tested | Tests and allocation benchmark added |
| L-02 | Low | The JSON fuzzer assumed every bounded decoded value must re-encode within the same limit, ignoring canonical escaping expansion | Corrected the invariant and retained the minimized corpus |

No high- or medium-severity finding remains open.

## Authoritative contracts

The audit uses the [Go memory model](https://go.dev/ref/mem),
[`context` contract](https://pkg.go.dev/context),
[`time` contract](https://pkg.go.dev/time), and
[`encoding/json` rules](https://pkg.go.dev/encoding/json). Backend behavior is
checked against Redis [`SET`](https://redis.io/docs/latest/commands/set/),
[Lua scripting](https://redis.io/docs/latest/develop/programmability/eval-intro/),
[TLS](https://redis.io/docs/latest/operate/oss_and_stack/management/security/encryption/),
and [ACL](https://redis.io/docs/latest/operate/oss_and_stack/management/security/acl/)
documentation, plus Valkey [transactions](https://valkey.io/topics/transactions/),
[TLS](https://valkey.io/topics/encryption/),
[security](https://valkey.io/topics/security/), and
[cluster](https://valkey.io/topics/cluster-spec/) documentation. Native-client
behavior is delegated only where documented by
[`go-redis/v9`](https://github.com/redis/go-redis) and
[`valkey-go`](https://github.com/valkey-io/valkey-go).

## Release verdict and gates

**Verdict: PASS for the audited standalone scope.** The following commands
passed on 2026-07-15:

```sh
make check
make integration
make fuzz FUZZ_TIME=10s
make benchmark
CACHE_REDIS_IMAGE=redis:7.2 make integration-redis
CACHE_REDIS_IMAGE=redis:7.4 make integration-redis
CACHE_REDIS_IMAGE=redis:8.0 make integration-redis
CACHE_VALKEY_IMAGE=valkey/valkey:9.0 make integration-valkey
go test -race ./... -count=50
```

Hard limits are the configured `MaxValue`, `MaxBatch`, backend key limit,
memory `MaxEntries`/`MaxBytes`, adapter `MaxRecordSize`, and load
`MaxConcurrent`/`MaxWaitersPerKey`. Zero load and batch limits select defaults
of 64 concurrent loaders, 1,024 waiters per key, and 1,000 bulk items. The JSON
codec defaults to 1 MiB when its own limit is unset.

Exact 100% coverage applies to production packages: core, memory, wire, OTel,
and slog in unit jobs, and each network adapter in its real-server integration
job. Test-support packages and generated fuzz corpus data are not presented as
production coverage.

Remaining accepted risks are the explicitly unsupported topology features,
process-local load/invalidation ordering, theoretical SHA-256 collision,
standard-library JSON cost within the input bound, native-server eviction, and
shutdown delay from a non-cooperative loader. Any red command, newly opened
high/medium finding, race, leak, sleep-based TTL assertion, or semantic matrix
contradiction changes the verdict to blocked.
