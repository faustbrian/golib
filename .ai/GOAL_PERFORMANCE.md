# Goal: Establish Performance Discipline

## Mission

Create reproducible performance engineering across `golib` without
trading correctness, bounded behavior, or maintainability for synthetic
benchmark wins.

Comparative performance work MUST also satisfy `.ai/GOAL_BENCHMARKS.md`.
Package-local benchmarks establish regression evidence; the comparative suite
establishes how equivalent behavior performs against maintained alternatives.
Neither replaces the other.

## Baseline

Inventory performance-sensitive paths and existing benchmarks. Record the Go
version, OS, architecture, CPU, power mode, benchmark flags, fixture sizes,
dependency versions, and repository revision for every baseline.

Classify packages by performance risk instead of imposing meaningless budgets
on all modules. Prioritize parsers, serializers, schema evaluators, HTTP and
RPC paths, queues, caches, database adapters, authorization evaluation,
schedulers, temporal calculations, and bulk tabular processing.

## Benchmark Requirements

- Separate construction, compilation, steady-state operation, and cleanup.
- Report latency, throughput, allocations, bytes allocated, and peak memory.
- Cover small, representative, large, and adversarial inputs.
- Benchmark concurrent and sequential use where both are supported.
- Include cancellation, failure, retry, cache miss, and degraded dependency
  paths where operationally relevant.
- Avoid setup, random fixture generation, logging, or network startup inside
  timed regions unless those are the behavior under test.
- Use deterministic fixtures and stable benchmark names.
- Compare statistically with `benchstat` or an equivalent maintained tool.
- Retain raw results for significant regressions and release baselines.

## Resource Budgets

Document evidence-based limits for input size, nesting, concurrency, queue
depth, retries, references, diagnostic output, cache size, database batches,
and goroutine ownership. Performance tests MUST prove graceful bounded failure
when limits are reached.

No package may create input-proportional goroutines without an enforced bound.
No cache, registry, queue, or retained diagnostic set may grow indefinitely.

## Regression Policy

- Define budgets only after representative measurements exist.
- Require investigation for statistically significant latency, allocation,
  memory, or throughput regressions.
- Permit regressions when correctness or security requires them, but document
  impact and alternatives.
- Never weaken validation, precision, cancellation, or safety to restore a
  benchmark number.
- Track improvements and regressions in package changelogs when users can
  observe them.

## CI Strategy

Run compilation and smoke benchmarks on pull requests, stable comparative
benchmarks on controlled runners or scheduled jobs, and full adversarial
benchmarks before significant releases. Do not fail builds on noisy shared
runner measurements without statistical controls.

## Required Deliverables

Provide a benchmark catalog, methodology, reproducible commands, baselines,
resource-limit matrix, regression policy, comparative competitor matrix, and
per-module performance verdict.

## Completion Criteria

This goal is complete when every hot path has representative and adversarial
benchmarks, resource growth is bounded, regression decisions are reproducible,
and no performance claim exceeds measured evidence.
