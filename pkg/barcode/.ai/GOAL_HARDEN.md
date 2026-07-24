# Goal: Harden barcode for Production

## Objective

Demonstrate standards compliance, independent software interoperability, and
bounded behavior for malformed payloads and adversarial images across every
advertised barcode format.

Hardware APIs, camera capture, printer control, scanner control, and physical
device certification are out of scope.

## Standards And Format Correctness

- Maintain a normative requirement and fixture inventory per symbology and
  pinned specification edition.
- Test capacity boundaries, mode switches, checksums, quiet zones, dimensions,
  masks, correction levels, GS1/FNC1, ECI, and format-specific reserved areas.
- Verify logical module patterns before testing raster output.
- Reject unsupported options and invalid data instead of silently normalizing
  them into a different symbol.
- Require independent-reader and independent-writer interoperability for every
  advertised format.

## Rendering And Software Interoperability

- Golden-test PNG, SVG, and logical/vector output with deterministic metadata.
- Test scaling, colors, transparency, non-square requests, integer overflow,
  and minimum readable dimensions.
- Exercise rotation, inversion, skew, blur, noise, glare, cropping, damaged
  modules, and low contrast with documented acceptance thresholds.
- Maintain reproducible reciprocal tests with independent software encoders
  and decoders without claiming compatibility beyond the tested contracts.

## Decoder Security

- Fuzz all binary, textual, GS1, symbol, and image decoding entry points.
- Enforce image-pixel, candidate, correction, payload, recursion, CPU, and
  memory budgets before expensive work.
- Test decompression bombs, oversized dimensions, malformed palettes, truncated
  images, integer overflows, pathological candidate fields, and cancellation.
- Ensure malformed data cannot panic, hang, leak goroutines, or trigger remote
  access.

## Concurrency And Reliability

- Race-test reused immutable symbols, encoders, decoders, and renderers.
- Prove caller buffers and options are not aliased or mutated unexpectedly.
- Test deterministic output under parallel execution.
- Verify errors preserve machine-readable categories without exposing entire
  sensitive payloads by default.

## Verification Gates

- Meaningful 100% statement coverage for every advertised format.
- Passing official/independent vectors, differential tests, fuzzing, race tests,
  mutation tests, and reciprocal software interoperability checks.
- Stable benchmarks for encode, render, detect, decode, malformed rejection,
  allocations, and peak memory at representative sizes.
- Static analysis, vulnerability scanning, dependency review, fixture license
  review, and reproducible fixture checksums.

## Release Blockers

Release MUST be blocked by missing conformance evidence, self-round-trip-only
validation, wrong checksums or module layouts, unreadable safe defaults,
unbounded image processing, integer overflow, panic/hang findings, race
findings, undocumented format gaps, or meaningful coverage below 100%.
