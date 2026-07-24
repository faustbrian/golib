# FAQ

## Why is a format implemented but not advertised?

Implementation is only one gate. Independent fixtures, metadata, hostile-input
limits, meaningful 100% coverage, and reciprocal software interoperability
must also pass.

## Can I remove or shrink the quiet zone?

No. The encoder default is standards-safe and undersized requests are rejected.
Extra surrounding whitespace is fine.

## Why not resize the PNG to the exact CSS or label size?

Non-integer resampling distorts module widths. Select an integer module scale or
use SVG with a layout that preserves integer device pixels at rasterization.

## Does a high correction level fix a tiny or blurry code?

No. Correction handles bounded symbol damage, not inadequate module size,
missing quiet zones, perspective distortion, or poor contrast.

## Will the decoder open a URL it finds?

Never. It returns untrusted payload data only.

## Is support for multiple symbols complete?

QR and Data Matrix structured-append encoding and Macro PDF417 are available.
General multiple-symbol detection and automatic sequence assembly remain
incomplete.
