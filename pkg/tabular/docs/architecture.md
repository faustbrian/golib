# Architecture

The public package owns configuration, stable errors, normalization, and the
common row contract. Each parser translates its format into ordered `Row`
values without schema inference.

Delimited input delegates RFC-style quoting and record boundaries to Go's
`encoding/csv`, then adds explicit header and normalization semantics.
Fixed-width input scans bounded newline records and slices byte ranges before
decoding. ZIP input validates the central directory before exposing exact
entry readers.

XLSX is a two-stage boundary: the package first validates ZIP limits and
worksheet XML well-formedness, then uses Excelize's row iterator with raw cell
values. XLS is intentionally separate under `internal/xls`; it reads OLE2
compound streams and a limited BIFF8 record set into bounded memory.

This split keeps a single public spreadsheet API while preserving honest
format-specific resource behavior. Internal XLS types are not compatibility
surface. Excelize is isolated behind narrow iterator interfaces so its errors
remain testable and callers do not depend on its types.
