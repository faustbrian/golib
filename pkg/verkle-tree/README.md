# verkle-tree

`verkle-tree` is intended to become a storage-independent Go library for
authenticated key/value trees backed by vector commitments.

## Status

This module is **pre-v1 research only**. It does not yet expose a Go package,
implement a tree, or make proof/interoperability claims.

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
[`specification/sources.json`](specification/sources.json).

No dependency, fixture, generator table, setup material, or generated constant
has been imported.

## Development rule

Production implementation must not begin until the profile-freeze blockers are
resolved or a deliberately experimental profile is approved with a name,
version, complete transcript, canonical encoding, backend provenance, and
bounded compatibility claims.

The complete product requirements remain in [`.ai/GOAL.md`](.ai/GOAL.md).
