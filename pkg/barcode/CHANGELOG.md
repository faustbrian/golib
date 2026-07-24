# Changelog

- Activate the complete barcode gate at the monorepo workflow root, with
  path filtering, scheduled fuzzing, benchmark artifacts, and manual dispatch.
- Shard hosted mutation testing by package directory while retaining the full
  local mutation command and thresholds.
- Scope capability advertising and interoperability evidence to independently
  tested software behavior, excluding hardware integration and certification.

- Add deterministic logical, PNG, and SVG rendering fixtures for every
  implemented format, with reproducible checksum validation.
- Preserve the GS1-128 AIM symbology identifier as a decode diagnostic instead
  of including it in the decoded GS1 payload.
- Contain panics from additive two-dimensional decoder dependencies at the
  candidate boundary.

- Document classified failures for unsupported ECI, invalid GS1, and
  checksum-failing image candidates.

All notable changes will be documented here. The project has not published a
stable release; current capability limitations and verification blockers are
listed in `README.md` and exposed through `barcode.CapabilityFor`.

## Unreleased

### Compatibility

- Added a pinned module export baseline so incompatible public API changes
  fail the canonical repository gate.

- Added immutable barcode core models and bounded raster/SVG rendering.
- Added complete phase-one linear formats and QR control surfaces.
- Added Data Matrix, PDF417, and Aztec encoding, decoding, controls, and
  reciprocal interoperability evidence.
- Added pinned GS1 parsing with component and AI association validation.
- Added offline GS1 allocation, check-pair, and coupon content validation.
- Added reciprocal GS1-128, UPC, supplement, and ITF-14 interoperability
  evidence, including explicit ITF-14 decode-format preservation.
- Preserved Data Matrix structured-append sequence and file identifiers in
  decode diagnostics.
- Added bounded raw and encoded image decoding, hostile-input fuzzing,
  deterministic render goldens, benchmarks, and reproducible CI gates.
- Added normative-to-executable evidence inventories for every format.
