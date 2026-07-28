# Changelog

## Unreleased

### Added

- Bounded lexicographic external sorting for caller-defined fixed-size records.
- Per-record AES-256-GCM temporary-storage encryption with authenticated chunk
  and ordinal binding.
- Explicit record, in-memory chunk, byte, and 64-file merge limits.
- Owner-only temporary storage, typed fail-closed errors, duplicate
  preservation, deterministic cleanup, fuzzing, benchmarks, and exact
  statement-coverage evidence.
