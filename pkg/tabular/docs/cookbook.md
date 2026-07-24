# Scenario cookbook

## Finnish decimal CSV

Select `';'` as the delimiter. Values such as `12,50` remain strings; numeric
locale conversion belongs in application validation.

## Windows export

Wrap the source with `DecodeReader(source, EncodingWindows1252)` before
constructing a delimited reader. Fixed-width readers accept the encoding in
their own configuration.

## Strict headers

Use trimming, a consistent case, exact replacements, `RejectEmpty`, and
`RejectDuplicates`. Keep the original header separately if audit output needs
to show the source spelling.

## Preserve spreadsheet errors

Set `PreserveCellErrors` only when strings such as `#DIV/0!` are legitimate
application data. The default rejects them with row and field coordinates.

## Inspect archive contents safely

Use `Entries`; it returns a copy in original central-directory order. Open an
exact name. Never concatenate entry names into destination filesystem paths.
