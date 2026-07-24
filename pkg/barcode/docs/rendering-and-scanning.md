# Rendering and image decoding

Render at an integer module scale. Do not resize the resulting bitmap with a
general image resampler: interpolation blurs edges and non-integer scaling
distorts module widths. SVG output uses integer coordinates and
`shape-rendering="crispEdges"`.

Quiet zones are part of the logical symbol. Encoder defaults are the minimum
safe values for their format, and shorter values are rejected. Add surrounding
layout whitespace outside the symbol when the output layout requires more
clearance.

Convert the required output dimensions to integer pixels per module and pass
that integer as the render scale. Linear barcode height is format/application
dependent and must be validated against the governing standard.

For decoding, crop only outside the quiet zone. `imagedecode` can try up to four
rotations and optionally inverted luminance. The automated QR acceptance
fixture uses high correction and eight pixels per module. It verifies decoding
after a one-pixel box blur, foreground/background luminance of 70/190, 0.05%
deterministic pixel noise, a two-module central erasure, 5% horizontal skew,
a one-module mostly opaque glare stripe across three quarters of the symbol,
and complete removal of the four-module quiet zone. These are deterministic
software regression thresholds rather than claims about external devices.

`Decode` returns one result. When an image contains multiple symbols, selection
is deliberately unspecified; callers that need exhaustive detection must crop
candidate regions and decode each region separately within their own budgets.
Checksum-failing candidates are never returned. Invalid GS1 data is rejected
by GS1-aware encoders before symbol construction. Controlled Data Matrix
decoding preserves raw bytes for unknown ECI assignments and marks the result
with `ECI_ASSIGNMENT` and `ECI_UNSUPPORTED` diagnostics.
