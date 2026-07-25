# Performance and limits

Authorization is evaluated in memory against one immutable snapshot. Policy
evaluation performs no network or database I/O. Persistence and invalidation
belong on the reload path, outside request decisions.

## Default limits

Zero-valued limit fields retain these safe defaults:

| Surface | Default limit |
| --- | ---: |
| Engine policies | 1,000 |
| Engine batch requests | 1,000 |
| Engine trace entries | 100 |
| Engine matched policy IDs | 100 |
| Instrumentation matched policy IDs | 100 |
| ACL entries | 10,000 |
| ACL groups per subject | 100 |
| ACL matches per decision | 100 |
| RBAC roles | 1,000 |
| RBAC permissions or assignments | 10,000 each |
| RBAC inheritance depth | 32 |
| RBAC groups or matches per decision | 100 each |
| ABAC rules or named conditions | 1,000 each |
| ABAC condition depth | 32 |
| ABAC cost units or set values | 1,000 each |
| ABAC matches per decision | 100 |
| Model batch requests | 1,000 |
| Portable model document | 1 MiB |
| Portable manifest | 16 MiB |
| Compiler policies | 1,000 |
| Compiler aggregate model documents | 16 MiB |
| Advisory cached manifest | 1 MiB |
| Advisory cache entries and bytes | backend-configured hard limits |
| Synchronizer concurrent reloads | 1 |
| Synchronizer maximum staleness | 2 minutes |

Applications can lower model, compiler, and engine limits. Raising them requires
benchmark and memory evidence using representative policy distributions. Every
built-in model decoder rejects inputs above 1 MiB before JSON parsing. The
portable envelope rejects inputs above 16 MiB, and the compiler independently
bounds per-document bytes, aggregate document bytes, and policy count before
activation.

## Reference benchmark

Run the complete benchmark matrix with:

```sh
go test -run '^$' -bench '^Benchmark' -benchmem -benchtime=100ms ./...
```

On an Apple M4 Max with Go 1.26.5, a local reference run produced:

| Scenario | Time | Allocations |
| --- | ---: | ---: |
| Warm engine decision | 152 ns | 3 |
| Cold snapshot, engine, and decision | 238 ns | 6 |
| Batch of 100 engine decisions | 16.4 us | 301 |
| Snapshot construction and atomic reload | 66 ns | 2 |
| ACL with 10 / 1,000 / 10,000 indexed entries | 142 ns / 4.47 us / 40.0 us | 2 |
| RBAC inheritance depth 1 / 8 / 32 | 154 ns / 343 ns / 2.62 us | 3 / 3 / 10 |
| ABAC with 1 / 10 / 100 predicates | 83 ns / 419 ns / 3.76 us | 2 |
| Portable manifest compilation | 793 ns | 17 |

These numbers are orientation data, not cross-machine service-level
objectives. Record a stable baseline on deployment hardware, compare multiple
runs, and investigate changes in both time and allocations. Use end-to-end
budgets around the application mapper, instrumentation, and transport rather
than treating the core engine number as request latency.

## Scaling guidance

- Prefer batch evaluation when request mapping can be shared, but keep batches
  bounded to cap response size and cancellation latency.
- Keep ACL entries specific enough that one indexed principal/action/resource
  bucket does not contain an entire tenant's unrelated resources.
- Keep RBAC inheritance shallow and grant permissions to reusable parent roles.
- Reuse named ABAC conditions, order cheap selective predicates first, and set
  cost limits from the worst accepted policy rather than the global maximum.
- Reload complete immutable snapshots. Do not mutate active evaluator maps or
  slices to save an allocation; doing so breaks decision coherence.
