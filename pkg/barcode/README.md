# barcode

`barcode` is a standards-driven Go library for validating, encoding,
rendering, and decoding common one-dimensional and two-dimensional barcodes.
Its core values are immutable logical modules; PNG, SVG, and `image.Image`
output are derived views rather than the source of truth.

```go
symbol, err := qr.Encode([]byte("https://example.invalid/parcel/42"), qr.Options{
    ErrorCorrection: qr.Quartile,
})
if err != nil {
    return err
}
return render.PNG(output, symbol.Logical(), render.Options{Scale: 4})
```

Import paths use `github.com/faustbrian/golib/pkg/barcode`. The image decoder is an
additive package; encoders and logical rendering do not require callers to use
image detection.

## Capability status

No format is marked advertised until its software encoding, decoding,
validation, metadata, and independent interoperability evidence are complete.
Call `barcode.Formats()` and `barcode.CapabilityFor(format)` for the same
machine-readable status. Hardware APIs, device control, and physical device
certification are outside this library's scope.

| Format | Encode | Image decode | GS1 | Advertised | Current software limitation |
|---|---:|---:|---:|---:|---|
| QR Code | yes | yes | yes | yes | none |
| Code 128 / GS1-128 | yes | yes | yes | yes | none |
| Code 39 / Code 93 | yes | yes | no | yes | none |
| EAN-8 / EAN-13 | yes | yes | yes | yes | none |
| UPC-A / UPC-E | yes | yes | yes | yes | none |
| ITF / ITF-14 | yes | yes | ITF-14 | yes | none |
| Codabar | yes | yes | no | yes | optional checksum profiles unavailable |
| Data Matrix ECC 200 | yes | yes | yes | no | structured-append sequence assembly incomplete |
| PDF417 | yes | yes | no | no | macro sequence assembly incomplete |
| Aztec | yes | yes | yes | yes | none |

## Documentation

- [API and format options](docs/api.md)
- [GS1 recipes](docs/gs1.md)
- [Rendering and image decoding](docs/rendering-and-scanning.md)
- [Error-correction tradeoffs](docs/error-correction.md)
- [Software interoperability](docs/interoperability.md)
- [Security and resource limits](docs/security.md)
- [Adoption guide](docs/adoption.md)
- [Comparison with established libraries](docs/comparison.md)
- [FAQ](docs/faq.md)
- [Standards and licensing](specification/README.md)

Run `make check` for every blocking local gate. Coverage below meaningful 100%,
missing conformance evidence, or an unsupported advertised control remains a
release blocker.
