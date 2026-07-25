# Goal: Standards-Compliant Barcode and QR Support for Go

## Objective

Build `barcode` as an open-source, standards-driven library for generating,
reading, validating, and rendering common one-dimensional and two-dimensional
barcodes. QR codes belong in this repository as a focused package, not in a
separate foundational repository.

The implementation MUST prioritize standards compliance and interoperability
over merely producing images that appear barcode-like.

Hardware APIs, camera capture, printer control, scanner control, and physical
device certification are out of scope. Interoperability in this repository
means software-to-software compatibility with independent implementations.

## Required Symbologies

Phase 1 MUST fully implement and test the formats needed by the services:

- QR Code, including numeric, alphanumeric, byte, Kanji, ECI, structured
  append, FNC1, mask selection, and all error-correction levels;
- Code 128, including code-set switching, checksums, and GS1-128/FNC1;
- Code 39 and Code 93;
- EAN-8, EAN-13, UPC-A, UPC-E, and supplements;
- Interleaved 2 of 5 and ITF-14; and
- Codabar.

Phase 2 SHOULD add complete Data Matrix, PDF417, and Aztec support. A format MUST
NOT be advertised as supported until its required encoding, decoding,
validation, metadata, and interoperability fixtures are complete.

Each implementation MUST identify and pin the governing specification or
edition. Licensing constraints around standards and test vectors MUST be
documented without copying restricted material into the repository.

## Core Model

The public API MUST provide:

- typed symbology identifiers and format-specific options;
- validated payloads and structured data such as GS1 application identifiers;
- an immutable logical symbol independent of output pixels;
- encode and decode results with format, payload, raw bytes, orientation,
  confidence/diagnostics where meaningful, and checksum status;
- matrix and bar primitives with exact module dimensions;
- quiet-zone, scale, aspect, color, and error-correction controls;
- checksums and check-digit helpers with strict validation; and
- explicit capability and limitation reporting.

Defaults MUST be standards-safe. Renderers MUST NOT silently shrink quiet zones,
distort modules, truncate payloads, or choose an incompatible encoding mode.

## Rendering And Decoding

Provide renderers for `image.Image`, PNG, SVG, and a device-neutral vector or
module representation. EPS and PDF adapters MAY be separate optional packages
to avoid heavy root dependencies.

Decoding MUST support caller-provided bounds for image dimensions, pixels,
rotations, candidates, corrections, payload size, time, and memory. Image and
advanced decoder dependencies MUST remain additive.

The package MUST define behavior for inverted images, rotation, skew, blur,
noise, damaged modules, multiple symbols, unsupported ECI values, invalid GS1
data, and checksum failures.

## Package Structure

Prefer focused packages such as:

- `barcode` for shared symbols, metadata, and errors;
- `qr`, `code128`, `code39`, `code93`, `ean`, `upc`, `itf`, and `codabar`;
- later `datamatrix`, `pdf417`, and `aztec` packages;
- `render` and optional output adapters;
- `imagedecode` for image processing; and
- `barcodetest` for common conformance and interoperability helpers.

## Interoperability And Testing

- Meaningful 100% statement coverage is REQUIRED.
- Official or independently licensed conformance vectors MUST be inventoried
  with provenance.
- Golden tests MUST validate logical modules, not only rendered snapshots.
- Round-trip tests MUST be supplemented by independent decoder/encoder tests so
  matching defects cannot validate each other.
- Differential tests SHOULD compare against ZXing and mature Go libraries using
  identical payloads, formats, dimensions, and correction settings.
- Software interoperability tests MUST cover independently maintained encoders
  and decoders for release candidates.
- Fuzz tests MUST cover payloads, option parsers, symbol decoders, image
  decoders, and GS1 parsing.
- Race tests MUST cover concurrent encoders, decoders, and renderers.
- Mutation tests MUST prove checksum, bounds, and correction assertions.
- Benchmarks MUST separate logical encoding, rendering, image detection,
  decoding, allocations, and malformed-input rejection.

## Security

- Enforce explicit limits before allocating images, matrices, correction
  buffers, or candidate sets.
- Reject integer overflows in dimensions, scaling, and capacity calculations.
- Bound decoder CPU and memory against adversarial images and symbols.
- Never execute, fetch, or automatically follow content embedded in a barcode.
- Treat decoded text and URLs as untrusted data.

## Documentation And Delivery

Documentation MUST include a complete API reference, format matrix, quick
starts, GS1 recipes, rendering and image-decoding guidance, output sizing,
interoperability notes, error-correction tradeoffs, security limits, examples,
adoption guide, FAQ, and comparisons with established libraries.

CI MUST enforce formatting, vetting, strict linting, unit/conformance/race/fuzz
tests, meaningful coverage, mutation, vulnerability and dependency review,
golden fixture integrity, examples, docs, and benchmark tracking. All checks
MUST be locally reproducible.

## Completion Criteria

Completion requires the promised formats, standards inventory, encoding,
decoding, rendering, checksums, GS1 support, independent interoperability
evidence, hostile-input limits, documentation, CI, benchmarks, and meaningful
100% coverage. QR-only image generation is not completion.
