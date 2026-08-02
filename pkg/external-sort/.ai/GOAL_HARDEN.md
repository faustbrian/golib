# Goal: Harden Encrypted External Sort

## Non-Negotiable Quality Gate

The module MUST maintain exactly 100% statement coverage and exactly 100% of
viable mutants killed by meaningful tests. Tests MUST prove behavior rather
than merely execute lines or preserve implementation structure.

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT",
"SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and
"OPTIONAL" in this document are to be interpreted as described in BCP 14
[RFC2119] [RFC8174] when, and only when, they appear in all capitals, as
shown here.

## Objective

Prove confidentiality, integrity, deterministic ordering, bounded resource use,
and complete cleanup under hostile filesystems, corruption, cancellation,
process interruption, and concurrent lifecycle calls.

## Required Campaigns

- Inject open/read/write/sync/rename/close/remove failures, short IO, disk full,
  quota exhaustion, inode exhaustion, permission changes, and disappearing
  directories at every boundary.
- Corrupt, truncate, reorder, duplicate, substitute, and cross-link encrypted
  records, chunks, metadata, nonces, and format versions.
- Prove nonce uniqueness under deterministic entropy tests and concurrent chunk
  creation without exposing key or plaintext material.
- Race iteration, cancellation, corruption detection, repeated `Close`, and
  caller misuse; define behavior for every operation after close.
- Verify cleanup after construction failure, sorting failure, merge failure,
  callback panic, cancellation, and successful exhaustion.
- Fuzz bounds, record sizes, counts, encrypted framing, and merge histories.
- Verify memory, file descriptors, temporary bytes, merge fan-in, recursion,
  diagnostics, and cleanup work remain bounded.

## Crash And Kubernetes Semantics

Abrupt process death cannot run in-process cleanup. Document a safe
caller-owned stale-directory janitor using ownership, age, root containment,
and non-following filesystem semantics. Test SIGTERM cleanup within a deadline,
termination-grace expiry, ephemeral-storage pressure, pod eviction, read-only
roots, and volume reuse without claiming crash cleanup the process cannot make.

## Completion Gates

Meaningful exact 100% statement coverage, exactly 100% viable mutation kills,
race, fuzz, fault, leak, permission, corruption, benchmark, API compatibility,
docs, security, and supply-chain gates MUST pass. Release is blocked by
plaintext temporary data, nonce reuse, accepted corruption, root escape,
unbounded disk/memory/descriptors, or undeclared crash residue.

[RFC2119]: https://www.rfc-editor.org/rfc/rfc2119
[RFC8174]: https://www.rfc-editor.org/rfc/rfc8174
