# verkle-tree

`verkle-tree` is intended to become a storage-independent Go library for
authenticated key/value trees backed by vector commitments.

## Status

This module is **pre-v1 research only**. Its root package intentionally exposes
no tree operations and it does not implement a production tree. Compatibility
claims are limited to the exact research corpora described below.

The initial source review did not find a profile that can honestly be frozen as
stable:

- the shared Python reference specification identifies itself as work in
  progress;
- `ethereum/go-verkle` says the implementation is no longer used and that
  responses may be delayed;
- `crate-crypto/rust-verkle` says it is unreviewed and unsafe outside research;
- the Ethereum Verkle state EIPs are draft or stagnant; and
- current Geth development is replacing its Verkle state work with a binary
  tree.

The exact evidence and consequences are recorded in
[`specification/profile-freeze.md`](specification/profile-freeze.md) and
[`specification/sources.json`](specification/sources.json). The pinned backend's
accepted seam and release blockers are in
[`docs/backend-audit.md`](docs/backend-audit.md).

Production code imports the pinned `go-ipa` dependency only behind an internal
canonical point/scalar encoding boundary. Test-only differential evidence also
exercises its deterministic setup, vector commitment, aggregate opening,
transcript, serialization, and verification operations. The encoding tests
include two pinned upstream point fixtures and the documented scalar-field
modulus; their provenance is recorded in
[`specification/sources.json`](specification/sources.json). No setup material,
generator table, or generated constant has been imported.
The preliminary encoding-only benchmark scope, method, and raw samples are in
[`docs/benchmarks.md`](docs/benchmarks.md).
An independently generated fixture from the pinned `rust-verkle` revision
proves that the accepted scalar and commitment bytes round-trip identically
across the Rust and Go encoding boundaries. This remains an encoding-only
research result, not tree or proof compatibility.
The same harness independently derives the ordered 256-point generator set for
`eth_verkle_oct_2021`; its canonical-encoding digest agrees with the pinned Go
reference. This establishes generator-set agreement under SHA-256 collision
resistance only for those exact revisions, width, seed, and encodings.
For one pinned three-opening corpus, both references also produce the same
canonical 576-byte aggregate proof, and the Go verifier accepts the Rust proof.
This narrow research result does not establish a stable transcript, hostile
proof-decoding safety, or tree compatibility.
A separate isolated harness pins `ethereum/go-verkle` at
`aa0a270c0ed03faa6c502e0d96bf26189d1d6542` and reproduces one deterministic
256-wide tree root plus an aggregate proof covering membership, an absent
suffix, and an absent stem. The reference verifier accepts the proof and
rejects a mutated proof commitment and replay against a different valid root.
The Go artifact alone records reference behavior and does not make `go-verkle`
a production dependency. The pinned Rust trie now reproduces the same root
commitment and every aggregate-proof element for that exact corpus, after the
documented conversion of Rust's final scalar encoding to the little-endian Go
JSON convention. Both reference verifiers accept their proof. This is
independent one-corpus tree evidence, not general compatibility, hostile-decoder
evidence, or a stable profile.

## Development rule

Production implementation must not begin until the profile-freeze blockers are
resolved or a deliberately experimental profile is approved with a name,
version, complete transcript, canonical encoding, backend provenance, and
bounded compatibility claims.

The complete product requirements remain in [`.ai/GOAL.md`](.ai/GOAL.md).
