# Third-party notices

This module depends on permissively licensed barcode implementations for
selected encoding and decoding seams:

- `github.com/ericlevine/zxinggo`, Apache License 2.0;
- `github.com/makiuchi-d/gozxing`, Apache License 2.0;
- `github.com/boombuler/barcode`, MIT License;
- `github.com/ruudk/golang-pdf417`, MIT License;
- `github.com/speedata/barcode`, MIT License; and
- `github.com/unixdj/qr`, BSD 3-Clause License;
- `golang.org/x/text`, BSD 3-Clause License.

`internal/aztecencoder` derives from the Apache-2.0-licensed Aztec encoder in
`github.com/ericlevine/zxinggo` v0.1.0. It is maintained locally so GS1 FNC1
and ECI control prefixes can be emitted before the encoded payload.

`internal/pdf417encoder` derives from the Apache-2.0-licensed PDF417 encoder
in `github.com/ericlevine/zxinggo` v0.1.0. It includes a local correction for
calculating error-correction codewords over Unicode codeword values.

The module archives in `go.sum` pin the reviewed versions. Their license files
remain authoritative. No restricted standards text is copied into this
repository. The GS1 syntax dictionary is distributed under the license and
provenance documented in `specification/README.md`.

The GS1 content-linter algorithms and allocation tables in `gs1/linter.go` and
`gs1/linter_data.go` derive from the Apache-2.0-licensed GS1 Barcode Syntax
Dictionary reference implementation at commit
`2043848ad254657237f40aabef4ee66461fabc4b`.
