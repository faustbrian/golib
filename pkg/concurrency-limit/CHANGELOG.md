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
