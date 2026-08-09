# Changelog

## Unreleased

### Fixed

- Reject whole-record truncation and trailing encrypted bytes instead of
  accepting a shortened or extended chunk as complete, while reporting
  underlying read failures as typed storage errors and sealing a store when
  failed temporary-file cleanup would otherwise permit artifact accumulation.

### Added

- Bounded lexicographic external sorting for caller-defined fixed-size records.
- Per-record AES-256-GCM temporary-storage encryption with authenticated chunk
  and ordinal binding.
- Explicit record, in-memory chunk, byte, and 64-file merge limits.
- Owner-only temporary storage, typed fail-closed errors, duplicate
  preservation, deterministic cleanup, fuzzing, benchmarks, and exact
  statement-coverage evidence.
- Exact boundary and mutation coverage for resource ceilings, merge
  termination, authenticated framing, cross-chunk substitution, nonce
  consumption, and ordering helpers.
