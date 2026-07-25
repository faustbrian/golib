# Parsing and deterministic generation

`Parse` consumes only the supplied byte slice. It rejects DTDs and directives,
uses strict XML decoding, validates QName and NCName syntax, rejects duplicate
symbols, resolves only lexical `xml:base` identities, and enforces all parse
limits before constructing large component collections.

Imports and schema locations become resolved identity strings in the model;
parsing does not load them. Cancellation is observed through the supplied
context reader.

`Marshal` emits stable namespace prefixes, stable attribute and component
ordering, preserved operation direction, extension payloads, and optional XML
declaration/indentation. `MarshalOptions.MaxBytes` bounds output and returns
`ErrLimitExceeded` rather than a partial document.
