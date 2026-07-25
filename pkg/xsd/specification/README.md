# Specification provenance

`manifest.tsv` pins the XML Schema 1.0 Second Edition baseline. Dated W3C
Recommendation and XSTS URLs are immutable pins. Namespace URLs are snapshots:
their recorded digest and byte count are the accepted input, and drift fails
verification instead of silently changing the implementation target.

The normative language is in XSD Structures and XSD Datatypes. The primer,
built-in datatype schema, and examples are supporting material. The W3C schema
for schemas is normative only for the syntactic constraints it expresses.

`requirements/xsd-1.0.tsv` is the live implementation and evidence matrix.
Rows remain `missing` or `partial` until evidence directly covers the stated
requirement. `decisions.md` records scope and interpretation decisions.

Run `make provenance` to validate the local records. Set `VERIFY_REMOTE=1` to
download every pinned resource and verify its current bytes. Remote checking
is deliberately opt-in and is never part of parsing or compilation.
