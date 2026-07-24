# API and format options

The `barcode` package owns typed format identifiers, immutable `Matrix` and
`Bars` values, `Symbol`, `DecodeResult`, checksum state, diagnostics, and the
capability registry. Constructors copy caller slices; getters return defensive
copies where a slice could otherwise alias internal state.

Each encoder has a focused package and typed options:

- `qr`: mode, version, mask, ECI, FNC1, structured append, correction, and
  quiet zone;
- `code128`: automatic or forced code sets and GS1-128;
- `code39` and `code93`: full-ASCII/checksum behavior supported by the format;
- `ean` and `upc`: check digits and two- or five-digit supplements;
- `itf`: ITF and ITF-14 dimension/bearer controls;
- `codabar`: explicit validated guards;
- `datamatrix`: ECC 200 shape/dimensions, GS1, ECI, Macro 05/06, structured
  append, and controlled Base 256;
- `pdf417`: compaction, correction, compact layout, row/column constraints,
  ECI, and complete Macro PDF417 control blocks;
- `aztec`: compact/full layers, correction percentage, GS1, and ECI.

Encoders return logical symbols with standards-safe quiet zones. `render`
converts them to raster or SVG at exact integer scale. `imagedecode.Decode`
accepts an `image.Image`, requested formats, behavior flags, and explicit
resource limits, including an optional elapsed-time budget. `DecodeEncoded`
also bounds compressed PNG, JPEG, or GIF bytes before full decoding. Content is
returned as untrusted bytes and text; the library never executes or follows
it.

Errors are package-level sentinels wrapped with diagnostic causes. Use
`errors.Is`, not string matching.

Unsupported ECI assignments are rejected by the encoder's classified error
sentinel. When controlled Data Matrix decoding encounters a syntactically
valid but unknown ECI, it preserves the raw payload bytes and reports both
`ECI_ASSIGNMENT` and `ECI_UNSUPPORTED` diagnostics instead of guessing a
character set. GS1 encoders validate the complete element string before
allocating a symbol and wrap `gs1.ErrInvalidElement`. Image candidates whose
mandatory checksum fails are discarded. If no valid candidate remains,
`Decode` returns `imagedecode.ErrNotFound` rather than exposing
checksum-failing payload data.
