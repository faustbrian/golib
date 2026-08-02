# Changelog

## Unreleased

### Added

- Add bounded standalone permit admission and typed execution helpers with
  exactly-once success, dependency-failure, local-drop, ignored, and overload
  outcomes.
- Add fixed, AIMD, Vegas-style, and Gradient2 algorithms with deterministic
  equations, bounded sampling, throughput correlation, reset state, and
  immutable diagnostics.
- Add optional bounded FIFO queueing, configured metadata cardinality,
  abandoned-permit reaping, graceful drain, pod-local reset, observers,
  simulations, fuzzing, race tests, benchmarks, and operational guidance.
- Reject admission explicitly if the non-wrapping process-local permit
  identifier sequence is exhausted.
- Emit admission/rejection events for queued grants and validate algorithm
  tuning against portable arithmetic bounds.
- Serialize lifecycle reset with algorithm decisions so a pre-reset window
  cannot overwrite cold-start state.
- Match Netflix Gradient2 warm-up averaging and preserve its fractional limit
  between updates instead of truncating adaptation on every window.
- Contain caller-supplied timer cleanup panics so queued admission still
  returns its terminal permit or error without corrupting limiter state.
- Add per-update reference equations, reproducible adversarial workload
  campaigns, metadata fairness checks, and lifecycle race stress coverage.
- Publish pinned comparative workload, convergence, CPU, memory, and allocation
  evidence, plus cross-package retry/hedge and pod lifecycle simulations.
