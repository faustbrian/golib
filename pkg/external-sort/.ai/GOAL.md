# Goal: Bounded Encrypted Fixed-Record External Sort

## Objective

Provide a dependency-free external sort for fixed-size opaque records that
keeps memory, record count, temporary files, and merge fan-in explicitly
bounded while encrypting every temporary record with AES-256-GCM.

## Required behavior

- Callers MUST declare the record size, records per in-memory chunk, maximum
  record count, and owner-only parent directory before opening storage.
- Invalid resource bounds and configurations requiring more than 64 merge
  files MUST fail before temporary storage opens.
- Every temporary record MUST use a fresh random AES-256-GCM nonce.
- Authentication MUST bind each record to its chunk, ordinal, record size, and
  format version so truncation, reordering, and cross-chunk substitution fail
  closed.
- Duplicate records MUST be preserved and yielded in ascending byte order.
- Temporary directories MUST be mode `0700`; chunk files MUST be mode `0600`.
- Cancellation, entropy failure, storage failure, corruption, and record-limit
  exhaustion MUST be typed and MUST NOT expose keys, records, or paths.
- `Close` MUST be idempotent and remove every temporary artifact after success.

## Boundaries

The module does not derive encryption keys, persist results, recover abandoned
crash artifacts, distribute work, compare semantic records, or provide an
unbounded multi-pass merge. Callers own key derivation, lifecycle cleanup, and
any stale-directory policy after process termination.

## Verification

Completion requires meaningful 100% production statement coverage, duplicate
preservation, corruption and reorder rejection, failure cleanup, exact
permission checks, fuzzing, race testing, an allocation benchmark, API
compatibility evidence, and security documentation.
