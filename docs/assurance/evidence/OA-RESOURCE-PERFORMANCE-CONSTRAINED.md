# OA-RESOURCE-PERFORMANCE Constrained Load Evidence

Observed at `2026-08-12T20:28:12Z` on Docker Engine `29.6.2` running a native
`linux/arm64` container, with Go `1.26.5`.

## Executed Proof

- A task-owned scratch container ran the public `service` reference under a
  64 MiB memory limit, 0.25 CPU limit, 64-process limit, 256-descriptor limit,
  non-root identity, read-only root filesystem, and dropped capabilities.
- A bounded driver completed 20,000 equivalent business requests at concurrency
  16 with zero request failures and zero process-sampling failures.
- The campaign measured 8,856.91 requests per second over 2.258 seconds, with
  0.689 ms p50, 1.430 ms p95, and 60.377 ms p99 response latency.
- Observed process maxima were 3,675,120 bytes allocated heap, 7,798,784 bytes
  reserved heap, 30 goroutines, and 28 open file descriptors.
- The executable gate required at least 500 requests per second, at most 250 ms
  p99, at most 32 MiB reserved heap, at most 128 goroutines and descriptors,
  exact request completion, zero errors, readiness after load, and graceful
  termination within the container deadline.
- The reference module's canonical verification gates passed after the load
  campaign. All task-owned containers, images, BuildKit state, fixtures, and Go
  caches were removed after evidence capture.

## Claim Boundary

This is a short constrained native-Linux service campaign. It does not prove a
multi-hour or multi-day soak, stress-to-failure behavior, realistic production
traffic distributions, database or broker load, ECS or Graviton behavior,
fleet-wide backpressure, dependency degradation, recovery after exhaustion, or
production capacity. `OA-RESOURCE-PERFORMANCE` therefore remains pending.
