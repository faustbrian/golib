# Standards and fixture inventory

This repository identifies standards but does not redistribute restricted ISO
or AIM publications. Implementers need licensed access to the listed editions
for normative review. Public metadata links are recorded in the `barcode`
capability registry.

The GS1 Barcode Syntax Dictionary is redistributed under Apache-2.0 from the
GS1 repository. It is pinned to release `2026-01-27` and SHA-256
`6461ece7c03420fb27a3f18163ef9a08694aa4bb2491dea25f63bcdf22451a99`.
Run `scripts/sync-gs1-dictionary.sh` to reproduce it.

`normative.tsv` maps each required format behavior to its pinned standard.
`evidence.tsv` maps the same stable IDs to executable or explicitly partial
evidence. They contain descriptive metadata only and do not reproduce
restricted standards text. Every normative ID must have exactly one evidence
row; the documentation gate verifies that relationship.

`render-fixtures.tsv` inventories deterministic logical, PNG, and SVG goldens
for every implemented format. The checked-in files in `render-fixtures/` are
canonical software rendering fixtures. `make docs` verifies their hashes.

The independent writer tests use `github.com/speedata/barcode` v1.1.1 under
the MIT license. Its module archive hash is recorded in `manifest.json` and its
module checksum is enforced by `go.sum`. The tests generate QR, Code 128,
Code 39, Code 93, EAN-8, EAN-13, ITF, Codabar, Data Matrix, and Aztec symbols
through that separate implementation before decoding them through this
library. They also generate GS1-128 and ITF-14 payloads through the independent
Code 128 and ITF writers. UPC-A and UPC-E use the Apache-2.0 ZXing writer.
PDF417 uses `github.com/ruudk/golang-pdf417` at commit `a7e3863a1245` under the
MIT license because speedata/barcode v1.1.1 emits an invalid PDF417
error-correction checksum for this fixture.

The reciprocal reader tests use `github.com/ericlevine/zxinggo` v0.1.0 under
Apache-2.0. They render every format from this library and decode it through
that separate reader implementation. Its module archive hash is also recorded
in `manifest.json` and its module checksum is enforced by `go.sum`.

No ISO or AIM example is represented as an official conformance fixture unless
its redistribution terms independently permit that use. Independently derived
logical vectors identify their derivation in the corresponding test.
