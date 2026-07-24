# Comparison with established libraries

`barcode` differs from image-first barcode packages by making immutable
logical modules and typed format options the public contract. It also exposes
machine-readable capability limitations and caller-provided decode budgets.

ZXing-family implementations remain valuable independent readers and sources
of interoperability evidence. Mature Go encoders are used only behind local
validation and tests; dependency success does not automatically make a format
advertised. A dependency writer that produced PDF417 checksum failures was
rejected in favor of an independently decodable path.

Choose an established general-purpose library directly when its API and
resource model already fit the application. Choose this package when exact
logical symbols, strict GS1 handling, explicit limits, and honest release gates
are more important than the broadest possible format list.
