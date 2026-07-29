# Commitment Backend Audit

## Decision

`github.com/crate-crypto/go-ipa` is approved only for the current internal,
pre-v1 canonical-encoding research boundary. It is rejected as the production
commitment backend until the blockers below are resolved and re-audited.

The imported module version is
`v0.0.0-20240223125850-b1e8a79f509c`, corresponding to commit
`b1e8a79f509c5dd26b44d64c5f4aff67d7e69ed0`.

## Evidence

At the pinned revision:

- the upstream unit suite and race suite pass;
- `go vet ./...` passes;
- point decoding checks canonical base-field encoding, curve membership, and
  the Banderwagon subgroup condition;
- scalar decoding provides a canonical little-endian field decoder;
- the 256-point generator set is deterministically derived from
  `eth_verkle_oct_2021`; and
- upstream fixes at the pinned commit distinguish trusted uncompressed
  serialization from the checked compressed path.

The module-local dependency review also found:

- all resolved dependency licenses are Apache-2.0, BSD-3-Clause, MIT, or the
  package's recorded dual-license choice;
- the secret scan reports no findings; and
- `govulncheck` reports no reachable vulnerable symbols.

These results establish useful research behavior. They do not establish
production suitability, constant-time behavior, transcript soundness, or the
package-level hostile-input contract.

## Production Blockers

### Mutable cryptographic globals

The backend exports mutable `Generator`, `Identity`, curve parameters, and
related group values. Application code importing the same module can mutate
them before setup construction. The package cannot prove generator identity or
configuration immutability while those globals remain authoritative.

### Uncancellable and CPU-derived work

Setup generation, precomputation, multi-scalar multiplication, and multiproof
aggregation do not accept `context.Context`. Several paths derive goroutine
counts from `runtime.NumCPU`, and setup precomputation uses
`context.Background`. A wrapper cannot cancel or join this work after a caller
deadline without leaving backend work running.

### Unsafe public surface

The dependency publicly exposes unchecked or trusted decoding operations and
raw group, field, transcript, setup, and proof types. The future public
`verkletree` API must not re-export any of them.

### Initialization and mutable precomputation

Field and square-root packages initialize mutable lookup tables and parameters
through package initialization. The tree goal forbids hidden setup generation
and mutable global registries at package initialization.

### Verification robustness

Proof APIs accept pointer-rich inputs without package-level nil, size, work,
or cancellation budgets. The tree boundary must validate every count and
encoding before invoking proof verification and must convert malformed input
into typed errors without panic.

### Side-channel and maintenance evidence

The reviewed source does not state a complete constant-time contract. The
selected pseudo-version is untagged, and the upstream repository has not
published the maintenance, audit, vulnerability, or release evidence required
for a production cryptographic dependency.

### Vulnerable dependency versions

The pinned dependency graph contains `github.com/consensys/gnark-crypto`
`v0.12.1`, affected by `GO-2025-4087`, and `golang.org/x/sys` `v0.9.0`,
affected by the Windows-only `GO-2026-5024`. The module does not currently call
the reported vulnerable symbols, but a production cryptographic backend cannot
ship with these stale versions without a compatible upgrade and complete
revalidation.

## Accepted Internal Boundary

The current internal boundary may:

- decode exactly 32-byte compressed Banderwagon commitments;
- reject non-canonical, off-curve, wrong-subgroup, and identity encodings;
- decode exactly 32-byte canonical little-endian scalars;
- return one canonical encoding for accepted commitments and scalars; and
- defensively copy caller bytes before dependency decoding.

It must not yet construct setup, commit vectors, open positions, verify proofs,
or expose dependency values outside `internal/`.

## Reconsideration Gate

Production selection requires either an upstream revision or a separately
reviewable maintained backend that removes mutable cryptographic globals,
provides bounded context-aware work, narrows unsafe APIs, documents
side-channel behavior, and passes the complete differential, fuzz, mutation,
license, vulnerability, and provenance gates.
